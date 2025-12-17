package ports

import (
	"context"
	"time"

	"github.com/AutoCookies/pomai-cache/internal/core/models"
)

// TokenPayload chứa thông tin payload trong Access Token
type TokenPayload struct {
	UserID    string    `json:"sub"`
	IssuedAt  time.Time `json:"iat"`
	ExpiredAt time.Time `json:"exp"`
}

// TokenMaker quy định cách tạo và verify token
type TokenMaker interface {
	CreateToken(userId string, duration time.Duration) (string, error)
	VerifyToken(token string) (*TokenPayload, error)
	CreateRefreshToken() (string, error)
}

// AuthRepository quy định các thao tác DB User và Token
type AuthRepository interface {
	// User Ops
	CreateUser(ctx context.Context, params models.CreateUserParams) (*models.User, error)
	FindUserByEmail(ctx context.Context, email string) (*models.User, error)
	FindUserByID(ctx context.Context, userID string) (*models.User, error)
	UpdateUserStatus(ctx context.Context, userID string, disabled bool) error

	// Token Ops
	CreateRefreshToken(ctx context.Context, token *models.RefreshToken) error
	FindValidRefreshToken(ctx context.Context, tokenString string) (*models.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenID string) error
}
