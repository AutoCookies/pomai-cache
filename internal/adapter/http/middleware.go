package http

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/AutoCookies/pomai-cache/internal/engine"
)

// CorsMiddleware cho phép requests từ mọi nguồn (Dev mode)
func CorsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE, HEAD")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, X-Requested-With, X-Tenant-ID")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// TenantMiddleware inject TenantID vào context và giới hạn concurrency
func TenantMiddleware(tenants *engine.TenantManager, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get("X-Tenant-ID")
		if strings.TrimSpace(tenantID) == "" {
			tenantID = "default"
		}

		// Giới hạn concurrency (timeout 3s)
		if !tenants.AcquireTenant(tenantID, 3*time.Second) {
			http.Error(w, "tenant too many concurrent requests", http.StatusTooManyRequests)
			return
		}
		defer tenants.ReleaseTenant(tenantID)

		// Inject vào context với key string "tenantID" để package handlers đọc được
		ctx := context.WithValue(r.Context(), "tenantID", tenantID)
		next(w, r.WithContext(ctx))
	}
}
