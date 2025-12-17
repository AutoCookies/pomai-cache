package persistence

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/AutoCookies/pomai-cache/internal/core/models"
	"github.com/AutoCookies/pomai-cache/internal/core/ports"
	"google.golang.org/api/iterator"
)

type FirestoreAuthRepo struct {
	client *firestore.Client
}

// Đảm bảo struct implement interface
var _ ports.AuthRepository = (*FirestoreAuthRepo)(nil)

func NewFirestoreAuthRepo(client *firestore.Client) *FirestoreAuthRepo {
	return &FirestoreAuthRepo{client: client}
}

// --- USER OPERATIONS ---

func (r *FirestoreAuthRepo) CreateUser(ctx context.Context, params models.CreateUserParams) (*models.User, error) {
	// 1. Check Email Exist
	iter := r.client.Collection("users").Where("email", "==", params.Email).Limit(1).Documents(ctx)
	existing, err := iter.Next()
	if err == nil && existing != nil {
		return nil, errors.New("email already in use")
	}

	now := time.Now()

	// 2. Prepare Payload
	user := models.User{
		Email:        params.Email,
		PasswordHash: params.PasswordHash, // Lấy hash đã được gán từ Service
		DisplayName:  params.DisplayName,
		Roles:        []string{"owner"}, // Default role
		Disabled:     params.Disabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// 3. Save to Firestore
	docRef, _, err := r.client.Collection("users").Add(ctx, user)
	if err != nil {
		return nil, err
	}

	user.ID = docRef.ID
	return &user, nil
}

func (r *FirestoreAuthRepo) FindUserByEmail(ctx context.Context, email string) (*models.User, error) {
	iter := r.client.Collection("users").Where("email", "==", email).Limit(1).Documents(ctx)
	doc, err := iter.Next()
	if err == iterator.Done {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, err
	}

	var user models.User
	if err := doc.DataTo(&user); err != nil {
		return nil, err
	}
	user.ID = doc.Ref.ID
	return &user, nil
}

func (r *FirestoreAuthRepo) FindUserByID(ctx context.Context, userID string) (*models.User, error) {
	doc, err := r.client.Collection("users").Doc(userID).Get(ctx)
	if err != nil {
		if !doc.Exists() {
			return nil, nil
		}
		return nil, err
	}
	var user models.User
	if err := doc.DataTo(&user); err != nil {
		return nil, err
	}
	user.ID = doc.Ref.ID
	return &user, nil
}

func (r *FirestoreAuthRepo) UpdateUserStatus(ctx context.Context, userID string, disabled bool) error {
	_, err := r.client.Collection("users").Doc(userID).Update(ctx, []firestore.Update{
		{Path: "disabled", Value: disabled},
		{Path: "updatedAt", Value: time.Now()},
	})
	return err
}

// --- TOKEN OPERATIONS ---

func (r *FirestoreAuthRepo) CreateRefreshToken(ctx context.Context, token *models.RefreshToken) error {
	// Firestore cho phép tự gen ID nếu dùng .Add(), hoặc ta tự set ID bằng .NewDoc()
	ref := r.client.Collection("refresh_tokens").NewDoc()
	token.CreatedAt = time.Now()
	token.Revoked = false

	// Lưu ý: struct RefreshToken cần có tags `firestore:"..."`
	_, err := ref.Set(ctx, token)
	if err != nil {
		return err
	}
	token.ID = ref.ID
	return nil
}

func (r *FirestoreAuthRepo) FindValidRefreshToken(ctx context.Context, tokenString string) (*models.RefreshToken, error) {
	iter := r.client.Collection("refresh_tokens").
		Where("token", "==", tokenString).
		Where("revoked", "==", false).
		Limit(1).
		Documents(ctx)

	doc, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var rt models.RefreshToken
	if err := doc.DataTo(&rt); err != nil {
		return nil, err
	}
	rt.ID = doc.Ref.ID

	// Check expiry ở tầng Application cho chắc chắn (dù có thể query)
	if time.Now().After(rt.ExpiresAt) {
		return nil, nil
	}

	return &rt, nil
}

func (r *FirestoreAuthRepo) RevokeRefreshToken(ctx context.Context, tokenID string) error {
	_, err := r.client.Collection("refresh_tokens").Doc(tokenID).Update(ctx, []firestore.Update{
		{Path: "revoked", Value: true},
		{Path: "revokedAt", Value: time.Now()},
	})
	return err
}
