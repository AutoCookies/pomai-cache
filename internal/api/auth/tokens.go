package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// Helpers for parsing duration strings (supports "15m","1h","7d", etc).
func parseDurOrDefault(s string, d time.Duration) time.Duration {
	if s == "" {
		return d
	}
	// support days suffix "Nd"
	if len(s) > 1 && s[len(s)-1] == 'd' {
		var n int
		_, err := fmt.Sscanf(s, "%dd", &n)
		if err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour
		}
	}
	t, err := time.ParseDuration(s)
	if err != nil {
		return d
	}
	return t
}

// GenerateAccessToken signs an access JWT using HS256 with accessSecret.
// extraClaims can include roles or other claims; expires uses cfg.AccessExpiry if expiresIn empty.
func GenerateAccessToken(cfg *Config, userID string, extraClaims map[string]any, expiresIn time.Duration) (string, error) {
	if cfg == nil || cfg.JWTAccessSecret == "" {
		return "", errors.New("access secret not configured")
	}
	if userID == "" {
		return "", errors.New("userID required")
	}
	if expiresIn <= 0 {
		expiresIn = cfg.AccessExpiry
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": userID,
		"iat": now.Unix(),
		"exp": now.Add(expiresIn).Unix(),
	}
	for k, v := range extraClaims {
		claims[k] = v
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString([]byte(cfg.JWTAccessSecret))
}

// VerifyAccessToken verifies the JWT and returns the claims.
func VerifyAccessToken(cfg *Config, tokenStr string) (jwt.MapClaims, error) {
	if cfg == nil || cfg.JWTAccessSecret == "" {
		return nil, errors.New("access secret not configured")
	}
	p := func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(cfg.JWTAccessSecret), nil
	}
	tok, err := jwt.Parse(tokenStr, p)
	if err != nil {
		return nil, err
	}
	if !tok.Valid {
		return nil, errors.New("token invalid")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims")
	}
	return claims, nil
}

// GenerateRefreshToken creates a refresh JWT signed with refresh secret.
func GenerateRefreshToken(cfg *Config, userID string, extraClaims map[string]any, expiresIn time.Duration) (string, error) {
	if cfg == nil || cfg.JWTRefreshSecret == "" {
		return "", errors.New("refresh secret not configured")
	}
	if userID == "" {
		return "", errors.New("userID required")
	}
	if expiresIn <= 0 {
		expiresIn = cfg.RefreshExpiry
	}
	// create jti
	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		return "", err
	}
	jti := base64.RawURLEncoding.EncodeToString(jtiBytes)

	now := time.Now()
	claims := jwt.MapClaims{
		"sub": userID,
		"iat": now.Unix(),
		"exp": now.Add(expiresIn).Unix(),
		"jti": jti,
	}
	for k, v := range extraClaims {
		claims[k] = v
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString([]byte(cfg.JWTRefreshSecret))
}

// VerifyRefreshToken verifies refresh JWT and returns claims.
func VerifyRefreshToken(cfg *Config, tokenStr string) (jwt.MapClaims, error) {
	if cfg == nil || cfg.JWTRefreshSecret == "" {
		return nil, errors.New("refresh secret not configured")
	}
	p := func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(cfg.JWTRefreshSecret), nil
	}
	tok, err := jwt.Parse(tokenStr, p)
	if err != nil {
		return nil, err
	}
	if !tok.Valid {
		return nil, errors.New("token invalid")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims")
	}
	return claims, nil
}
