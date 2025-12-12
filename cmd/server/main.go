package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/AutoCookies/pomai-cache/internal/api"
	"github.com/AutoCookies/pomai-cache/internal/cache"
)

func main() {
	// Config via env or flags
	var (
		portEnv           = getEnv("PORT", "8080")
		shardsEnv         = getEnv("CACHE_SHARDS", "32")
		perTenantCapacity = getEnv("PER_TENANT_CAPACITY_BYTES", "0") // quota per tenant
		gracefulSec       = getEnv("GRACEFUL_SHUTDOWN_SEC", "10")
		addrFlag          = flag.String("addr", ":"+portEnv, "listen address")
		shardsFlag        = flag.Int("shards", atoiDefault(shardsEnv, 32), "shard count")
		perTenantFlag     = flag.Int64("perTenantCapacity", atoi64Default(perTenantCapacity, 0), "per-tenant capacity bytes (0 = unlimited)")
		gracefulFlag      = flag.Int("graceful", atoiDefault(gracefulSec, 10), "graceful shutdown seconds")
	)
	flag.Parse()

	tenants := cache.NewTenantManager(*shardsFlag, *perTenantFlag)

	// Server requires auth tokens (Bearer <tenantID>) to identify tenant.
	srv := api.NewServer(tenants, true)

	httpSrv := &http.Server{
		Addr:    *addrFlag,
		Handler: srv.Router(),
	}

	log.Printf("starting cache server on %s (per-tenant shards=%d perTenantCap=%d)", *addrFlag, *shardsFlag, *perTenantFlag)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server ListenAndServe: %v", err)
		}
	}()

	// graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Printf("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*gracefulFlag)*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("server shutdown error: %v", err)
	} else {
		log.Printf("server stopped")
	}
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoiDefault(s string, d int) int {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return d
}

func atoi64Default(s string, d int64) int64 {
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}
	return d
}
