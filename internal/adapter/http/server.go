package http

import (
	"net/http"

	"github.com/AutoCookies/pomai-cache/internal/engine"
	"github.com/gorilla/mux"
)

type Server struct {
	tenants *engine.TenantManager
	router  *mux.Router
}

func NewServer(tenants *engine.TenantManager) *Server {
	s := &Server{
		tenants: tenants,
		router:  mux.NewRouter(),
	}
	s.setupRoutes()
	return s
}

func (s *Server) Router() http.Handler {
	return CorsMiddleware(s.router)
}
