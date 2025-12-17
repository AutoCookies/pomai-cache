package httpadapter

import (
	"net/http"

	"github.com/AutoCookies/pomai-cache/internal/core/ports" // Import ports
	"github.com/AutoCookies/pomai-cache/internal/engine"
	"github.com/gorilla/mux"
)

// Server wraps handlers for cache engine and auth
type Server struct {
	tenants     *engine.TenantManager
	authHandler *AuthHandler
	tokenMaker  ports.TokenMaker // [MỚI] Cần cái này để verify token
	requireAuth bool
	router      *mux.Router
}

// NewServer creates a new API Server instance
// [CẬP NHẬT] Thêm tham số tokenMaker
func NewServer(
	tenants *engine.TenantManager,
	authHandler *AuthHandler,
	tokenMaker ports.TokenMaker, // [MỚI]
	requireAuth bool,
) *Server {
	s := &Server{
		tenants:     tenants,
		authHandler: authHandler,
		tokenMaker:  tokenMaker, // [MỚI]
		requireAuth: requireAuth,
		router:      mux.NewRouter(),
	}
	s.setupRoutes()
	return s
}

func (s *Server) Router() http.Handler {
	return s.router
}
