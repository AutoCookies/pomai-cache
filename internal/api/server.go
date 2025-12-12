package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AutoCookies/pomai-cache/internal/cache"
	"github.com/gorilla/mux"
)

type contextKey string

const ctxTenantKey contextKey = "tenantID"

// Server wraps handlers for cache.
type Server struct {
	tenants     *cache.TenantManager
	requireAuth bool
	router      *mux.Router
}

// NewServer creates a new API Server.
// If requireAuth is true the server expects Authorization: Bearer <tenantID> and treats the token value as tenant ID.
func NewServer(tenants *cache.TenantManager, requireAuth bool) *Server {
	s := &Server{
		tenants:     tenants,
		requireAuth: requireAuth,
		router:      mux.NewRouter(),
	}
	s.routes()
	return s
}

// Router returns http.Handler to be used by http.Server
func (s *Server) Router() http.Handler {
	return s.router
}

func (s *Server) routes() {
	api := s.router.PathPrefix("/v1").Subrouter()

	api.HandleFunc("/cache/{key}", s.authMiddleware(s.handlePut())).Methods("PUT")
	api.HandleFunc("/cache/{key}", s.authMiddleware(s.handleGet())).Methods("GET")
	api.HandleFunc("/cache/{key}", s.authMiddleware(s.handleDelete())).Methods("DELETE")
	api.HandleFunc("/cache/{key}", s.authMiddleware(s.handleHead())).Methods("HEAD")

	api.HandleFunc("/stats", s.authMiddleware(s.handleStats())).Methods("GET")

	// health
	s.router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}).Methods("GET")
}

// authMiddleware extracts tenant from Authorization header (if requireAuth) and stores tenantID in request context.
// If requireAuth=false, it injects tenantID "default" so single-tenant mode still works.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := "default"
		if s.requireAuth {
			// 1) Try Authorization header "Bearer <token>"
			auth := r.Header.Get("Authorization")
			var token string
			if auth != "" {
				parts := strings.Fields(auth)
				if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
					token = parts[1]
				} else {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
			}

			// 2) If no header token, try cookie "accessToken"
			if token == "" {
				if c, err := r.Cookie("accessToken"); err == nil {
					token = c.Value
				}
			}

			// 3) If still empty, try query param (least secure)
			if token == "" {
				token = r.URL.Query().Get("accessToken")
			}

			if token == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// Map/validate token -> tenantID.
			// Option A: treat token itself as tenantID (simple)
			// tenantID = token

			// Option B: use a mapping or validate JWT and extract tenant claim.
			// Example uses a token->tenant map stored in TenantManager or another AuthManager:
			if t := s.tenants.LookupTenantForToken(token); t != "" {
				tenantID = t
			} else {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		ctx := context.WithValue(r.Context(), ctxTenantKey, tenantID)
		next(w, r.WithContext(ctx))
	}
}

func tenantFromContext(ctx context.Context) string {
	if v := ctx.Value(ctxTenantKey); v != nil {
		if t, ok := v.(string); ok {
			return t
		}
	}
	return "default"
}

func (s *Server) handlePut() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := tenantFromContext(r.Context())
		vars := mux.Vars(r)
		key := vars["key"]
		if key == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}
		ttl := time.Duration(0)
		if v := r.URL.Query().Get("ttl"); v != "" {
			if secs, err := strconv.ParseInt(v, 10, 64); err == nil && secs > 0 {
				ttl = time.Duration(secs) * time.Second
			}
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}
		store := s.tenants.GetStore(tenant)
		store.Put(key, body, ttl)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

func (s *Server) handleGet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := tenantFromContext(r.Context())
		key := mux.Vars(r)["key"]
		if key == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}
		store := s.tenants.GetStore(tenant)
		v, ok := store.Get(key)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(v)
	}
}

func (s *Server) handleDelete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := tenantFromContext(r.Context())
		key := mux.Vars(r)["key"]
		if key == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}
		store := s.tenants.GetStore(tenant)
		store.Delete(key)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleHead() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := tenantFromContext(r.Context())
		key := mux.Vars(r)["key"]
		if key == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}
		store := s.tenants.GetStore(tenant)
		remain, ok := store.TTLRemaining(key)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if remain > 0 {
			w.Header().Set("X-Cache-TTL-Remaining", fmt.Sprintf("%d", int64(remain.Seconds())))
		} else {
			w.Header().Set("X-Cache-TTL-Remaining", "0")
		}
		w.WriteHeader(http.StatusOK)
	}
}

func (s *Server) handleStats() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := r.URL.Query().Get("tenant")
		if tenant != "" {
			// per-tenant stats if requested
			if st, ok := s.tenants.StatsForTenant(tenant); ok {
				_ = json.NewEncoder(w).Encode(st)
				return
			}
			http.Error(w, "tenant not found", http.StatusNotFound)
			return
		}
		// otherwise, return aggregated stats across tenants
		type AggStats struct {
			TotalTenants int                    `json:"total_tenants"`
			PerTenant    map[string]cache.Stats `json:"per_tenant"`
		}
		out := AggStats{
			PerTenant: make(map[string]cache.Stats),
		}

		statsMap := s.tenants.StatsAll()
		out.TotalTenants = len(statsMap)
		out.PerTenant = statsMap

		_ = json.NewEncoder(w).Encode(out)
	}
}
