package auth

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// MeHandler returns the JWT claims extracted by middleware (fast).
// If query param ?full=true and AUTH_SERVICE_URL is set, the handler can optionally fetch remote profile (not covered here).
func MeHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaimsFromContext(r)
		if ok {
			writeJSON(w, http.StatusOK, map[string]any{"claims": claims})
			return
		}
		// fallback - no claims found
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "no auth claims"})
	}
}

// RefreshHandler verifies the incoming refresh JWT with cfg.JWTRefreshSecret, then issues
// a new access token and a new refresh token (both signed locally).
func RefreshHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// parse refresh token from JSON body or cookie
		var body struct {
			RefreshToken string `json:"refreshToken"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		token := strings.TrimSpace(body.RefreshToken)
		if token == "" {
			if c, err := r.Cookie("refreshToken"); err == nil {
				token = c.Value
			}
		}
		if token == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing refresh token"})
			return
		}

		claims, err := VerifyRefreshToken(cfg, token)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid or expired refresh token"})
			return
		}

		userID, _ := claims["sub"].(string)
		if userID == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid refresh token payload"})
			return
		}

		// propagate roles or other claims if present on refresh
		extra := map[string]any{}
		if roles, ok := claims["roles"]; ok {
			extra["roles"] = roles
		}

		accessTok, err := GenerateAccessToken(cfg, userID, extra, 0)
		if err != nil {
			log.Printf("[auth] GenerateAccessToken error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal error"})
			return
		}
		refreshTok, err := GenerateRefreshToken(cfg, userID, nil, 0)
		if err != nil {
			log.Printf("[auth] GenerateRefreshToken error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal error"})
			return
		}

		setAuthCookies(w, cfg, accessTok, refreshTok)

		writeJSON(w, http.StatusOK, map[string]any{"accessToken": accessTok, "refreshToken": refreshTok})
	}
}
