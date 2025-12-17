package auth

import (
	"os"
	"strings"
	"time"
)

// Config contains runtime configuration for auth handlers/middleware.
type Config struct {
	JWTAccessSecret   string
	JWTRefreshSecret  string
	AccessExpiry      time.Duration
	RefreshExpiry     time.Duration
	CookieDomain      string // optional
	ProductionCookies bool
}

// NewConfigFromEnv reads env vars and returns a Config.
// ENVs used:
// - JWT_ACCESS_SECRET (required)
// - JWT_REFRESH_SECRET (required)
// - ACCESS_TOKEN_EXPIRES_IN (default "15m")
// - REFRESH_TOKEN_EXPIRES_IN (default "7d")
// - COOKIE_DOMAIN (optional)
// - NODE_ENV == "production" sets ProductionCookies = true
func NewConfigFromEnv() *Config {
	access := os.Getenv("JWT_ACCESS_SECRET")
	refresh := os.Getenv("JWT_REFRESH_SECRET")
	ae := os.Getenv("ACCESS_TOKEN_EXPIRES_IN")
	if ae == "" {
		ae = "15m"
	}
	re := os.Getenv("REFRESH_TOKEN_EXPIRES_IN")
	if re == "" {
		re = "7d"
	}
	return &Config{
		JWTAccessSecret:   strings.TrimSpace(access),
		JWTRefreshSecret:  strings.TrimSpace(refresh),
		AccessExpiry:      parseDurOrDefault(ae, 15*time.Minute),
		RefreshExpiry:     parseDurOrDefault(re, 7*24*time.Hour),
		CookieDomain:      os.Getenv("COOKIE_DOMAIN"),
		ProductionCookies: strings.ToLower(os.Getenv("NODE_ENV")) == "production",
	}
}
