package models

import (
	"context"
	"errors"
	"regexp"
	"time"

	"cloud.google.com/go/firestore"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/api/iterator"
)

const (
	UserCollection = "users"
	SaltRounds     = 12
)

// User Struct
type User struct {
	ID           string    `firestore:"-" json:"id"`
	Email        string    `firestore:"email" json:"email"`
	PasswordHash string    `firestore:"passwordHash" json:"-"`
	DisplayName  string    `firestore:"displayName" json:"displayName"`
	Roles        []string  `firestore:"roles" json:"roles"`
	Disabled     bool      `firestore:"disabled" json:"disabled"`
	CreatedAt    time.Time `firestore:"createdAt" json:"createdAt"`
	UpdatedAt    time.Time `firestore:"updatedAt" json:"updatedAt"`
}

// [SỬA] Thêm PasswordHash vào đây
type CreateUserParams struct {
	Email        string
	Password     string // Plain text
	PasswordHash string // Hash (Service sẽ điền cái này)
	DisplayName  string
	Roles        []string
	WorkspaceID  string
	InitialRole  string
	Disabled     bool
}

// ... (Giữ nguyên các hàm validate, HashPassword, VerifyPassword, FindUserByEmail...)
// Lưu ý: Các hàm helper này giữ nguyên như file bạn đã gửi
func validateEmail(email string) bool {
	re := regexp.MustCompile(`\S+@\S+\.\S+`)
	return re.MatchString(email)
}

func validatePassword(password string) bool {
	return len(password) >= 8
}

func HashPassword(password string) (string, error) {
	if !validatePassword(password) {
		return "", errors.New("invalid password (min 8 chars)")
	}
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(bytes), err
}

func VerifyPassword(plainPassword, passwordHash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(plainPassword))
	return err == nil
}

func FindUserByEmail(ctx context.Context, client *firestore.Client, email string) (*User, error) {
	// ... (Code cũ của bạn ok)
	if !validateEmail(email) {
		return nil, errors.New("invalid email")
	}
	iter := client.Collection(UserCollection).Where("email", "==", email).Limit(1).Documents(ctx)
	doc, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var user User
	doc.DataTo(&user)
	user.ID = doc.Ref.ID
	return &user, nil
}

// Validate Email Helper Exported (để AuthService dùng)
func ValidateEmail(email string) bool {
	return validateEmail(email)
}

func CheckPassword(password, hash string) bool {
	return VerifyPassword(password, hash)
}
