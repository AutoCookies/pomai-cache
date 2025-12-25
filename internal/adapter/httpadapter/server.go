package httpadapter

import (
	"net/http"

	"github.com/AutoCookies/pomai-cache/internal/core/ports"
	"github.com/AutoCookies/pomai-cache/internal/engine"
	"github.com/gorilla/mux"
)

// Server wraps handlers for cache engine and auth
type Server struct {
	tenants       *engine.TenantManager
	authHandler   *AuthHandler
	tokenMaker    ports.TokenMaker
	requireAuth   bool
	router        *mux.Router
	apiKeyHandler *APIKeyHandler
	apiKeyService ports.APIKeyService // expose service for middleware use
}

// NewServer creates a new API Server instance
func NewServer(
	tenants *engine.TenantManager,
	authHandler *AuthHandler,
	tokenMaker ports.TokenMaker,
	requireAuth bool,
	apiKeyService ports.APIKeyService,
) *Server {
	apiKeyHandler := NewAPIKeyHandler(apiKeyService)

	s := &Server{
		tenants:       tenants,
		authHandler:   authHandler,
		tokenMaker:    tokenMaker,
		requireAuth:   requireAuth,
		router:        mux.NewRouter(),
		apiKeyHandler: apiKeyHandler,
		apiKeyService: apiKeyService,
	}

	s.setupRoutes()
	return s
}

// Router trả về handler đã được bọc middleware CORS
// Đây là chốt chặn quan trọng nhất để fix lỗi trên trình duyệt
func (s *Server) Router() http.Handler {
	return enableCORS(s.router)
}

// enableCORS là Middleware cho phép truy cập từ mọi nguồn (Allow All)
// Phục vụ cho SDK public và Web App (Vercel)
func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Cho phép tất cả các domain (Client nào cũng gọi được)
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// 2. Cho phép đầy đủ các phương thức HTTP
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD")

		// 3. Cho phép các Header quan trọng (Authorization, X-API-Key dùng để xác thực)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, Origin, Accept, X-Requested-With")

		// 4. Xử lý Preflight Request (OPTIONS)
		// Trình duyệt luôn gửi request này trước khi gửi POST/PUT để "hỏi đường"
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Nếu không phải OPTIONS, chuyển tiếp cho Router xử lý logic chính
		next.ServeHTTP(w, r)
	})
}
