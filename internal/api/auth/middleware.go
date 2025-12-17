package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// ctxKey type to avoid collisions
type ctxKey string

const (
	claimsCtxKey ctxKey = "authClaims"
)

// parseBearer extracts a Bearer token from an Authorization header value.
func parseBearer(h string) string {
	const p = "Bearer "
	if h == "" {
		return ""
	}
	h = strings.TrimSpace(h)
	if strings.HasPrefix(h, p) {
		return strings.TrimSpace(h[len(p):])
	}
	return ""
}

// tokenFromCookie tries to read cookie named "accessToken".
func tokenFromCookie(r *http.Request) string {
	if c, err := r.Cookie("accessToken"); err == nil {
		return c.Value
	}
	return ""
}

// Authenticate is middleware verifying access token (JWT) using provided cfg.
// On success it stores jwt.MapClaims into request context under "authClaims".
func Authenticate(cfg *Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := parseBearer(r.Header.Get("Authorization"))
			if token == "" {
				token = tokenFromCookie(r)
			}
			if token == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing access token"})
				return
			}
			claims, err := VerifyAccessToken(cfg, token)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid or expired access token"})
				return
			}
			ctx := context.WithValue(r.Context(), claimsCtxKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetClaimsFromContext(r *http.Request) (map[string]any, bool) {
	v := r.Context().Value(claimsCtxKey)
	if v == nil {
		return nil, false
	}
	switch c := v.(type) {
	case map[string]any:
		return c, true
	case jwt.MapClaims:
		out := map[string]any{}
		for k, vv := range c {
			out[k] = vv
		}
		return out, true
	default:
		return nil, false
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
