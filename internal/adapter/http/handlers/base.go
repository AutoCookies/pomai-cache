package handlers

import (
	"context"
	"net/http"
	"os"
	"strconv"

	"github.com/AutoCookies/pomai-cache/internal/engine"
	"github.com/golang/snappy"
)

// HTTPHandlers chứa các dependencies cần thiết cho việc xử lý request
type HTTPHandlers struct {
	Tenants *engine.TenantManager
}

// NewHTTPHandlers khởi tạo
func NewHTTPHandlers(tenants *engine.TenantManager) *HTTPHandlers {
	return &HTTPHandlers{
		Tenants: tenants,
	}
}

// tenantFromContext helper lấy tenantID từ context (được inject từ middleware bên ngoài)
func tenantFromContext(ctx context.Context) string {
	// Sử dụng string key để tránh import cycle với package middleware
	if v := ctx.Value("tenantID"); v != nil {
		if t, ok := v.(string); ok {
			return t
		}
	}
	return "default"
}

// getenvInt64 helper đọc biến môi trường
func getenvInt64(name string, def int64) int64 {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

// serveValue helper trả về dữ liệu binary (có giải nén snappy nếu cần)
func serveValue(w http.ResponseWriter, v []byte) {
	if len(v) == 0 {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(v)
		return
	}
	magicByte := v[0]
	payload := v[1:]
	if magicByte == 1 {
		decoded, err := snappy.Decode(nil, payload)
		if err != nil {
			http.Error(w, "decompression error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(decoded)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(payload)
}
