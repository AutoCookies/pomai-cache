package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	httpAdapter "github.com/AutoCookies/pomai-cache/internal/adapter/http"
	"github.com/AutoCookies/pomai-cache/internal/adapter/persistence"
	"github.com/AutoCookies/pomai-cache/internal/adapter/persistence/file"
	"github.com/AutoCookies/pomai-cache/internal/adapter/persistence/wal"
	tcpAdapter "github.com/AutoCookies/pomai-cache/internal/adapter/tcp"

	"github.com/AutoCookies/pomai-cache/internal/core/ports"
	"github.com/AutoCookies/pomai-cache/internal/engine"
)

// Helper đọc Int từ Env
func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// Helper đọc Duration từ Env
func getenvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// Helper đọc String từ Env
func getenv(key string, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	// 1. Load .env file (nếu có)
	_ = godotenv.Load() // Bỏ qua lỗi nếu không có file .env (chạy bằng biến môi trường hệ thống)

	// 2. Config từ ENV
	httpPort := getenv("PORT", "8080")
	tcpPort := getenv("TCP_PORT", "9090") // Mặc định 9090 cho Pomai Protocol
	dataDir := getenv("DATA_DIR", "./data")

	// Cấu hình Cache
	shardCount := getenvInt("CACHE_SHARDS", 256)
	perTenantCapacity := int64(getenvInt("PER_TENANT_CAPACITY_BYTES", 0)) // 0 = Unlimited

	// Cấu hình Persistence
	persistType := getenv("PERSISTENCE_TYPE", "noop") // file, wal, noop

	// Cấu hình Write-Behind
	wbMax := getenvInt("WRITE_BUFFER_SIZE", 1000)
	wbFlushInterval := getenvDuration("FLUSH_INTERVAL", 5*time.Second)

	log.Printf("CONFIG: Shards=%d, Cap=%d bytes, Persist=%s, WB=%d/%v",
		shardCount, perTenantCapacity, persistType, wbMax, wbFlushInterval)

	// 3. Init Engine
	// Bloom Filter & Sketch config (Hardcode hoặc thêm env nếu muốn)
	engine.InitGlobalFreq(65536, 4)

	tm := engine.NewTenantManager(shardCount, perTenantCapacity)

	// 4. Setup Persistence Strategy
	var pers ports.Persister
	var persImpl interface{} // Dùng để check interface Snapshotter

	// Tạo thư mục data nếu chưa có
	_ = os.MkdirAll(dataDir, 0755)

	switch persistType {
	case "file":
		fp, err := file.NewFilePersister(dataDir)
		if err != nil {
			log.Fatalf("failed to create file persister: %v", err)
		}
		pers = fp
		persImpl = fp
	case "wal":
		walPath := fmt.Sprintf("%s/wal.log", dataDir)
		wp, err := wal.NewWALPersister(walPath)
		if err != nil {
			log.Fatalf("failed to create wal persister: %v", err)
		}
		// Restore dữ liệu từ WAL khi khởi động
		log.Println("Restoring from WAL...")
		defaultStore := tm.GetStore("default")
		if err := wp.Restore(defaultStore); err != nil {
			log.Printf("WAL Restore warning: %v", err)
		}
		pers = wp
		persImpl = wp
	default:
		np := persistence.NewNoOpPersister()
		pers = np
		persImpl = np
	}

	// 5. Start Write-Behind (Background Worker)
	wb := persistence.NewWriteBehindBuffer(wbMax, wbFlushInterval, pers)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wb.Start(ctx)

	// 6. Init HTTP Server (Admin/Stats)
	srv := httpAdapter.NewServer(tm)
	httpSrv := &http.Server{
		Addr:    ":" + httpPort,
		Handler: srv.Router(),
	}

	// Chạy HTTP trong Goroutine
	errCh := make(chan error, 1)
	go func() {
		log.Printf("HTTP Admin Server starting on :%s", httpPort)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// 7. [QUAN TRỌNG] Init TCP Server (Pomai Binary Protocol)
	pomaiSrv := tcpAdapter.NewPomaiServer(tm)
	go func() {
		log.Printf("Pomai Binary Data Server starting on :%s", tcpPort)
		if err := pomaiSrv.ListenAndServe(":" + tcpPort); err != nil {
			// TCP lỗi là lỗi critical, cho sập luôn để restart
			errCh <- fmt.Errorf("TCP Server failed: %w", err)
		}
	}()

	// 8. Graceful Shutdown Handler
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("Signal received: %v, starting graceful shutdown...", sig)
	case err := <-errCh:
		log.Fatalf("Server crash: %v", err)
	}

	// Shutdown HTTP
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}

	// Stop Write-Behind
	if err := wb.Close(); err != nil {
		log.Printf("Write-behind close error: %v", err)
	}

	// Snapshot khi tắt (Nếu persistence hỗ trợ Snapshot)
	if sp, ok := persImpl.(ports.Snapshotter); ok {
		tenantIDs := tm.ListTenants()
		log.Printf("Snapshotting %d tenants before exit...", len(tenantIDs))
		for _, id := range tenantIDs {
			store := tm.GetStore(id)
			if err := sp.Snapshot(store); err != nil {
				log.Printf("Snapshot error tenant=%s: %v", id, err)
			}
		}
		log.Println("Snapshot complete")
	}

	// Close Persister (File/WAL)
	if pers != nil {
		if err := pers.Close(); err != nil {
			log.Printf("Persister close error: %v", err)
		}
	}

	log.Println("Shutdown complete. Bye!")
}
