package auth

import (
	"net/http"
)

// setAuthCookies sets accessToken and refreshToken cookies per config.
func setAuthCookies(w http.ResponseWriter, cfg *Config, accessToken, refreshToken string) {
	isProd := cfg != nil && cfg.ProductionCookies
	accessMax := int(cfg.AccessExpiry.Seconds())
	refreshMax := int(cfg.RefreshExpiry.Seconds())

	accessCookie := &http.Cookie{
		Name:     "accessToken",
		Value:    accessToken,
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteNoneMode,
		Path:     "/",
		MaxAge:   accessMax,
	}
	if cfg != nil && cfg.CookieDomain != "" {
		accessCookie.Domain = cfg.CookieDomain
	}
	http.SetCookie(w, accessCookie)

	refreshCookie := &http.Cookie{
		Name:     "refreshToken",
		Value:    refreshToken,
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteNoneMode,
		Path:     "/",
		MaxAge:   refreshMax,
	}
	if cfg != nil && cfg.CookieDomain != "" {
		refreshCookie.Domain = cfg.CookieDomain
	}
	http.SetCookie(w, refreshCookie)
}
