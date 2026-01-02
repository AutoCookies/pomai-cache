// File: cmd/server/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
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
	"github.com/AutoCookies/pomai-cache/internal/engine/tenants"
)

const (
	Version     = "1.3.0-gnet"
	ServiceName = "Pomai Cache (Gnet Edition)"
)

type Config struct {
	HTTPPort string
	TCPPort  string

	CacheShards   int
	CapacityBytes int64

	PersistenceType string
	DataDir         string
	WriteBufferSize int
	FlushInterval   time.Duration

	HTTPReadTimeout  time.Duration
	HTTPWriteTimeout time.Duration
	HTTPIdleTimeout  time.Duration
	ShutdownTimeout  time.Duration

	EnableCORS    bool
	EnableMetrics bool
	EnableDebug   bool

	GCPercent       int
	MaxProcs        int
	MemoryLimit     int64
	EnableProfiling bool
}

func init() {
	applyRuntimeTuning()
}

func main() {
	_ = godotenv.Load()

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	applyConfigTuning(cfg)

	printBanner(cfg)

	log.Println("Initializing components...")

	log.Printf("Creating tenant manager (shards=%d, capacity=%s)...",
		cfg.CacheShards, formatBytes(cfg.CapacityBytes))
	tm := tenants.NewManager(cfg.CacheShards, cfg.CapacityBytes)

	pers, persImpl := setupPersistence(cfg, tm)
	defer closePersistence(pers, persImpl, tm)

	wb := setupWriteBehind(cfg, pers)
	defer closeWriteBehind(wb)

	httpSrv, tcpSrv := startServers(cfg, tm)

	waitForReady(cfg.HTTPPort)

	log.Println("")
	log.Println("========================================")
	log.Println("All services ready!")
	log.Println("Pomai Cache is running!")
	log.Println("========================================")
	log.Println("")

	if cfg.EnableProfiling {
		log.Printf("Profiling:   http://localhost:%s/debug/pprof/", cfg.HTTPPort)
	}

	gracefulShutdown(cfg, httpSrv, tcpSrv, wb, pers, persImpl, tm)
}

func applyRuntimeTuning() {
	numCPU := runtime.NumCPU()
	runtime.GOMAXPROCS(numCPU)

	debug.SetGCPercent(-1)

	debug.SetMemoryLimit(math.MaxInt64)
}

func applyConfigTuning(cfg *Config) {
	if cfg.MaxProcs > 0 {
		runtime.GOMAXPROCS(cfg.MaxProcs)
		log.Printf("GOMAXPROCS set to %d", cfg.MaxProcs)
	}

	if cfg.GCPercent >= 0 {
		old := debug.SetGCPercent(cfg.GCPercent)
		log.Printf("GC percent changed from %d to %d", old, cfg.GCPercent)
	} else {
		log.Println("Go GC disabled")
	}

	if cfg.MemoryLimit > 0 {
		debug.SetMemoryLimit(cfg.MemoryLimit)
		log.Printf("Memory limit set to %s", formatBytes(cfg.MemoryLimit))
	}

	if cfg.EnableDebug {
		runtime.SetBlockProfileRate(1)
		runtime.SetMutexProfileFraction(1)
		log.Println("Block and mutex profiling enabled")
	}
}

func loadConfig() (*Config, error) {
	cfg := &Config{
		HTTPPort: getenv("PORT", "8080"),
		TCPPort:  getenv("TCP_PORT", "7600"),

		CacheShards:   getenvInt("CACHE_SHARDS", 2048),
		CapacityBytes: int64(getenvInt("PER_TENANT_CAPACITY_BYTES", 0)),

		PersistenceType: getenv("PERSISTENCE_TYPE", "none"),
		DataDir:         getenv("DATA_DIR", "./data"),
		WriteBufferSize: getenvInt("WRITE_BUFFER_SIZE", 1000),
		FlushInterval:   getenvDuration("FLUSH_INTERVAL", 5*time.Second),

		HTTPReadTimeout:  getenvDuration("HTTP_READ_TIMEOUT", 30*time.Second),
		HTTPWriteTimeout: getenvDuration("HTTP_WRITE_TIMEOUT", 30*time.Second),
		HTTPIdleTimeout:  getenvDuration("HTTP_IDLE_TIMEOUT", 120*time.Second),
		ShutdownTimeout:  getenvDuration("SHUTDOWN_TIMEOUT", 30*time.Second),

		EnableCORS:    getenvBool("ENABLE_CORS", false),
		EnableMetrics: getenvBool("ENABLE_METRICS", false),
		EnableDebug:   getenvBool("ENABLE_DEBUG", false),

		GCPercent:       getenvInt("GOGC", -1),
		MaxProcs:        getenvInt("GOMAXPROCS", 0),
		MemoryLimit:     int64(getenvInt("GOMEMLIMIT", 0)),
		EnableProfiling: getenvBool("ENABLE_PROFILING", false),
	}

	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data dir: %w", err)
	}

	return cfg, nil
}

func validateConfig(cfg *Config) error {
	if cfg.CacheShards < 1 || cfg.CacheShards > 8192 {
		return fmt.Errorf("CACHE_SHARDS must be 1-8192, got %d", cfg.CacheShards)
	}

	if cfg.CapacityBytes < 0 {
		return fmt.Errorf("capacity cannot be negative")
	}

	if cfg.WriteBufferSize < 1 {
		return fmt.Errorf("write buffer size must be >= 1")
	}

	validPersistence := map[string]bool{
		"wal":  true,
		"file": true,
		"none": true,
	}

	if !validPersistence[cfg.PersistenceType] {
		return fmt.Errorf("invalid persistence type: %s", cfg.PersistenceType)
	}

	if cfg.GCPercent < -1 {
		return fmt.Errorf("GOGC must be >= -1, got %d", cfg.GCPercent)
	}

	return nil
}

func printBanner(cfg *Config) {
	banner := `
========================================
   POMAI CACHE v%s
========================================
  High Performance In-Memory Cache
    Powered by Gnet Event-Driven I/O
========================================

System: 
  Go:               %s
  CPU:            %d cores
  GOMAXPROCS:     %d
  Platform:       %s/%s
  GC:              Disabled
  Memory Limit:   %s

Config:
  HTTP:            :%s
  TCP:            :%s (Gnet)
  Shards:         %d
  Capacity:       %s
  Persistence:    %s

Mode:
  Gnet:           Enabled
  Auto-Tuning:    Enabled
  Stats:           Disabled
  Profiling:      %v

Endpoints:
  Health:          http://localhost:%s/health
  Stats:          http://localhost:%s/v1/stats

========================================
`

	capacityStr := "Unlimited"
	if cfg.CapacityBytes > 0 {
		capacityStr = formatBytes(cfg.CapacityBytes)
	}

	memLimitStr := "Unlimited"
	if cfg.MemoryLimit > 0 {
		memLimitStr = formatBytes(cfg.MemoryLimit)
	}

	fmt.Printf(banner,
		Version,
		runtime.Version(),
		runtime.NumCPU(),
		runtime.GOMAXPROCS(0),
		runtime.GOOS,
		runtime.GOARCH,
		memLimitStr,
		cfg.HTTPPort,
		cfg.TCPPort,
		cfg.CacheShards,
		capacityStr,
		cfg.PersistenceType,
		cfg.EnableProfiling,
		cfg.HTTPPort,
		cfg.HTTPPort,
	)
}

func setupPersistence(cfg *Config, tm *tenants.Manager) (ports.Persister, interface{}) {
	var pers ports.Persister
	var persImpl interface{}

	log.Printf("Setting up persistence (type=%s)...", cfg.PersistenceType)

	switch cfg.PersistenceType {
	case "file":
		fp, err := file.NewFilePersister(cfg.DataDir)
		if err != nil {
			log.Fatalf("Failed to create file persister: %v", err)
		}
		log.Println("File persistence initialized")
		pers = fp
		persImpl = fp

	case "wal":
		walPath := fmt.Sprintf("%s/wal. log", cfg.DataDir)
		wp, err := wal.NewWALPersister(walPath)
		if err != nil {
			log.Fatalf("Failed to create WAL persister:  %v", err)
		}

		log.Println("Restoring from WAL...")
		defaultStore := tm.GetStore("default")

		if err := wp.RestoreFrom(defaultStore); err != nil {
			log.Printf("WAL restore warning: %v", err)
		} else {
			stats := defaultStore.Stats()
			log.Printf("WAL restored: %d items, %s",
				stats.Items, formatBytes(stats.Bytes))
		}

		pers = wp
		persImpl = wp

	case "none":
		log.Println("No persistence (in-memory only)")
		np := persistence.NewNoOpPersister()
		pers = np
		persImpl = np

	default:
		log.Printf("Unknown persistence type '%s', using none", cfg.PersistenceType)
		np := persistence.NewNoOpPersister()
		pers = np
		persImpl = np
	}

	return pers, persImpl
}

func setupWriteBehind(cfg *Config, pers ports.Persister) *persistence.WriteBehindBuffer {
	if cfg.PersistenceType == "none" {
		log.Println("Write-behind buffer skipped")
		return nil
	}

	log.Printf("Setting up write-behind buffer (size=%d, interval=%v)...",
		cfg.WriteBufferSize, cfg.FlushInterval)

	wb := persistence.NewWriteBehindBuffer(cfg.WriteBufferSize, cfg.FlushInterval, pers)

	ctx := context.Background()
	wb.Start(ctx)

	log.Println("Write-behind buffer started")
	return wb
}

func startServers(cfg *Config, tm *tenants.Manager) (*httpAdapter.Server, *tcpAdapter.PomaiServer) {
	log.Printf("Starting HTTP server on :%s.. .", cfg.HTTPPort)

	httpConfig := httpAdapter.DefaultServerConfig()
	httpConfig.Port, _ = strconv.Atoi(cfg.HTTPPort)
	httpConfig.ReadTimeout = cfg.HTTPReadTimeout
	httpConfig.WriteTimeout = cfg.HTTPWriteTimeout
	httpConfig.IdleTimeout = cfg.HTTPIdleTimeout
	httpConfig.EnableCORS = cfg.EnableCORS
	httpConfig.EnableMetrics = cfg.EnableMetrics
	httpConfig.EnableDebug = cfg.EnableDebug

	httpSrv := httpAdapter.NewServerWithConfig(tm, httpConfig)

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	log.Printf("HTTP server started on :%s", cfg.HTTPPort)

	log.Printf("Starting Gnet TCP server on :%s.. .", cfg.TCPPort)

	tcpSrv := tcpAdapter.NewPomaiServer(tm)

	go func() {
		addr := ":" + cfg.TCPPort
		if err := tcpSrv.ListenAndServe(addr); err != nil {
			log.Fatalf("TCP server error: %v", err)
		}
	}()

	log.Println("Gnet TCP server started")

	return httpSrv, tcpSrv
}

func waitForReady(port string) {
	log.Println("Waiting for services...")

	maxRetries := 50
	retryDelay := 100 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		resp, err := http.Get("http://localhost:" + port + "/health")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			log.Println("Services ready")
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(retryDelay)
	}

	log.Println("Health check timeout")
}

func gracefulShutdown(cfg *Config, httpSrv *httpAdapter.Server, tcpSrv *tcpAdapter.PomaiServer,
	wb *persistence.WriteBehindBuffer, pers ports.Persister,
	persImpl interface{}, tm *tenants.Manager) {

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)

	sig := <-sigCh
	log.Printf("\nSignal received: %v", sig)
	log.Println("Starting graceful shutdown...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	log.Println("Stopping HTTP server...")
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	} else {
		log.Println("HTTP server stopped")
	}

	log.Println("Stopping TCP server...")
	if err := tcpSrv.Shutdown(cfg.ShutdownTimeout); err != nil {
		log.Printf("TCP shutdown error: %v", err)
	} else {
		log.Println("TCP server stopped")
	}

	if wb != nil {
		log.Println("Flushing write-behind buffer...")
		closeWriteBehind(wb)
	}

	if sp, ok := persImpl.(ports.Snapshotter); ok {
		log.Println("Creating snapshots...")
		createSnapshots(sp, tm)
	}

	log.Println("Closing persistence...")
	closePersistence(pers, persImpl, tm)

	printFinalStats(tm)

	log.Println("\nShutdown complete.  Goodbye!")
}

func closeWriteBehind(wb *persistence.WriteBehindBuffer) {
	if wb == nil {
		return
	}

	if err := wb.Close(); err != nil {
		log.Printf("Write-behind close error: %v", err)
	} else {
		log.Println("Write-behind buffer flushed")
	}
}

func createSnapshots(sp ports.Snapshotter, tm *tenants.Manager) {
	tenantIDs := tm.ListTenants()
	log.Printf("Creating snapshots for %d tenant(s)...", len(tenantIDs))

	successCount := 0
	totalItems := int64(0)
	totalBytes := int64(0)

	for _, id := range tenantIDs {
		store := tm.GetStore(id)
		if store == nil {
			continue
		}

		stats := store.Stats()
		totalItems += stats.Items
		totalBytes += stats.Bytes

		if err := sp.Snapshot(store); err != nil {
			log.Printf("Snapshot error for tenant '%s': %v", id, err)
		} else {
			successCount++
		}
	}

	log.Printf("Snapshots created: %d/%d (items:  %d, size: %s)",
		successCount, len(tenantIDs), totalItems, formatBytes(totalBytes))
}

func closePersistence(pers ports.Persister, persImpl interface{}, tm *tenants.Manager) {
	if pers == nil {
		return
	}

	if err := pers.Close(); err != nil {
		log.Printf("Persistence close error: %v", err)
	} else {
		log.Println("Persistence closed")
	}
}

func printFinalStats(tm *tenants.Manager) {
	log.Println("\nFinal Statistics:")

	tenantIDs := tm.ListTenants()
	for _, id := range tenantIDs {
		store := tm.GetStore(id)
		if store == nil {
			continue
		}

		stats := store.Stats()
		log.Printf("  Tenant '%s': %d items, %s",
			id, stats.Items, formatBytes(stats.Bytes))
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	log.Printf("  Memory: Alloc=%s, TotalAlloc=%s, Sys=%s, NumGC=%d",
		formatBytes(int64(m.Alloc)),
		formatBytes(int64(m.TotalAlloc)),
		formatBytes(int64(m.Sys)),
		m.NumGC)
}

func getenv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getenvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getenvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getenvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func formatBytes(bytes int64) string {
	if bytes == 0 {
		return "0 B"
	}

	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
