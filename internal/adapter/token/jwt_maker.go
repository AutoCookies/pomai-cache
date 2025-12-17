package token

import (
	"errors"
	"time"

	"github.com/AutoCookies/pomai-cache/internal/core/ports"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const minSecretLen = 32

type JWTMaker struct {
	secretKey string
}

func NewJWTMaker(secretKey string) (ports.TokenMaker, error) {
	if len(secretKey) < minSecretLen {
		return nil, errors.New("invalid key size: must be at least 32 characters")
	}
	return &JWTMaker{secretKey}, nil
}

// CreateToken tạo Access Token (JWT)
func (maker *JWTMaker) CreateToken(userId string, duration time.Duration) (string, error) {
	payload := jwt.MapClaims{
		"sub": userId,
		"iat": jwt.NewNumericDate(time.Now()),
		"exp": jwt.NewNumericDate(time.Now().Add(duration)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, payload)
	return token.SignedString([]byte(maker.secretKey))
}

// VerifyToken xác thực Access Token
func (maker *JWTMaker) VerifyToken(tokenString string) (*ports.TokenPayload, error) {
	keyFunc := func(token *jwt.Token) (interface{}, error) {
		_, ok := token.Method.(*jwt.SigningMethodHMAC)
		if !ok {
			return nil, errors.New("token is invalid")
		}
		return []byte(maker.secretKey), nil
	}

	jwtToken, err := jwt.Parse(tokenString, keyFunc)
	if err != nil {
		return nil, err
	}

	claims, ok := jwtToken.Claims.(jwt.MapClaims)
	if !ok || !jwtToken.Valid {
		return nil, errors.New("token is invalid")
	}

	payload := &ports.TokenPayload{
		UserID: claims["sub"].(string),
	}
	if exp, err := claims.GetExpirationTime(); err == nil {
		payload.ExpiredAt = exp.Time
	}

	return payload, nil
}

// CreateRefreshToken tạo Refresh Token (UUID)
// Đây là logic thay thế cho generateRefreshToken() trong tokens.js
func (maker *JWTMaker) CreateRefreshToken() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
