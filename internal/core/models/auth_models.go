package models

import "time"

// RefreshToken đại diện cho token lưu trong DB
type RefreshToken struct {
	ID        string    `firestore:"-" json:"id"`
	Token     string    `firestore:"token" json:"token"`
	UserID    string    `firestore:"userId" json:"userId"`
	ExpiresAt time.Time `firestore:"expiresAt" json:"expiresAt"`
	Revoked   bool      `firestore:"revoked" json:"revoked"`
	CreatedAt time.Time `firestore:"createdAt" json:"createdAt"`
	RevokedAt time.Time `firestore:"revokedAt,omitempty" json:"revokedAt,omitempty"`
}

// AuthResponse trả về cho Client
type AuthResponse struct {
	User         *User  `json:"user"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}
