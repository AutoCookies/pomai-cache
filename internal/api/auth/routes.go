package auth

import (
	"net/http"
	"strings"
)

// RegisterRoutes attaches auth routes under given base path (e.g. "/auth").
// Example usage:
//
//	cfg := NewConfigFromEnv()
//	RegisterAuthRoutes(mux, "/auth", cfg)
func RegisterAuthRoutes(mux *http.ServeMux, base string, cfg *Config) {
	base = strings.TrimRight(base, "/")
	// /auth/me requires authentication
	mux.Handle(base+"/me", AuthenticateWrapper(cfg))
	// /auth/refresh is public
	mux.HandleFunc(base+"/refresh", RefreshHandler(cfg))
}

// AuthenticateWrapper returns a handler that applies Authenticate middleware and then MeHandler.
func AuthenticateWrapper(cfg *Config) http.Handler {
	authMiddleware := Authenticate(cfg)
	return authMiddleware(http.HandlerFunc(MeHandler(cfg)))
}
