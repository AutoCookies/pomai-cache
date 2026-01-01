package http

import (
	"github.com/AutoCookies/pomai-cache/internal/adapter/http/handlers"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (s *Server) setupRoutes() {
	h := handlers.NewHTTPHandlers(s.tenants)
	api := s.router.PathPrefix("/v1").Subrouter()

	// --- SYSTEM ROUTES ---
	api.HandleFunc("/stats", TenantMiddleware(s.tenants, h.HandleStats)).Methods("GET")

	// Health check (Không cần Auth)
	s.router.HandleFunc("/health", h.HandleHealth).Methods("GET")

	// Prometheus Metrics Endpoint (Không cần Auth để Prometheus scrape)
	s.router.Handle("/metrics", promhttp.Handler()).Methods("GET")
}
