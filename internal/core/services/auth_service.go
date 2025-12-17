package services

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/AutoCookies/pomai-cache/internal/core/models"
	"github.com/AutoCookies/pomai-cache/internal/core/ports"
	"github.com/AutoCookies/pomai-cache/pkg/utils"
)

// AuthService chứa toàn bộ logic nghiệp vụ về xác thực
type AuthService struct {
	repo        ports.AuthRepository         // Giao tiếp với DB (User & Token)
	verifyRepo  ports.VerificationRepository // Giao tiếp với DB (OTP)
	tokenMaker  ports.TokenMaker             // Tạo JWT/PASETO & UUID
	emailSender ports.EmailSender            // Gửi Email
}

// NewAuthService khởi tạo service với các dependency được inject vào
func NewAuthService(
	repo ports.AuthRepository,
	verifyRepo ports.VerificationRepository,
	tokenMaker ports.TokenMaker,
	emailSender ports.EmailSender,
) *AuthService {
	return &AuthService{
		repo:        repo,
		verifyRepo:  verifyRepo,
		tokenMaker:  tokenMaker,
		emailSender: emailSender,
	}
}

// Register (Tương đương createPendingUser): Tạo user ở trạng thái Disabled và gửi OTP
func (s *AuthService) Register(ctx context.Context, params models.CreateUserParams) (*models.User, error) {
	if !models.ValidateEmail(params.Email) {
		return nil, errors.New("invalid email format")
	}

	// 1. Hash Password
	hashed, err := models.HashPassword(params.Password)
	if err != nil {
		return nil, err
	}

	// 2. Gán hash vào params để Repo sử dụng (FIX LỖI DECLARED NOT USED)
	params.PasswordHash = hashed
	params.Password = "" // Xóa plain text để an toàn (optional)

	// Force disabled
	params.Disabled = true

	// 3. Gọi Repo
	user, err := s.repo.CreateUser(ctx, params)
	if err != nil {
		return nil, err
	}

	// 4. Gửi OTP
	if err := s.sendVerificationFlow(ctx, user.ID, user.Email); err != nil {
		log.Printf("Failed to send verification email: %v", err)
	}

	return user, nil
}

// VerifyEmail (Tương đương completeSignup): Kiểm tra OTP -> Enable User -> Trả về Token
func (s *AuthService) VerifyEmail(ctx context.Context, email, code string) (*models.AuthResponse, error) {
	// 1. Validate OTP
	isValid, err := s.verifyRepo.Validate(ctx, "", email, code)
	if err != nil {
		return nil, err
	}
	if !isValid {
		return nil, errors.New("invalid or expired verification code")
	}

	// 2. Tìm User
	user, err := s.repo.FindUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("user not found")
	}

	// 3. Kích hoạt User (nếu đang disabled)
	if user.Disabled {
		if err := s.repo.UpdateUserStatus(ctx, user.ID, false); err != nil {
			return nil, err
		}
		user.Disabled = false
	}

	// 4. Sinh Token để đăng nhập luôn
	return s.generateTokenPair(ctx, user)
}

// Login (Tương đương signinUser)
func (s *AuthService) Login(ctx context.Context, email, password string) (*models.AuthResponse, error) {
	// 1. Tìm User
	user, err := s.repo.FindUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid credentials")
	}

	// 2. Kiểm tra trạng thái
	if user.Disabled {
		return nil, errors.New("account is disabled or not verified")
	}

	// 3. Kiểm tra mật khẩu
	if !models.CheckPassword(password, user.PasswordHash) {
		return nil, errors.New("invalid credentials")
	}

	// 4. Sinh Token
	return s.generateTokenPair(ctx, user)
}

// RefreshToken (Tương đương refreshTokens)
func (s *AuthService) RefreshToken(ctx context.Context, oldRefreshToken string) (*models.AuthResponse, error) {
	// 1. Tìm và validate Refresh Token trong DB
	storedToken, err := s.repo.FindValidRefreshToken(ctx, oldRefreshToken)
	if err != nil || storedToken == nil {
		return nil, errors.New("invalid or expired refresh token")
	}

	// 2. Bảo mật: Token Rotation
	// Thu hồi token cũ ngay lập tức để chống Replay Attack
	if err := s.repo.RevokeRefreshToken(ctx, storedToken.ID); err != nil {
		return nil, err
	}

	// 3. Lấy thông tin User
	user, err := s.repo.FindUserByID(ctx, storedToken.UserID)
	if err != nil || user == nil {
		return nil, errors.New("user not found associated with token")
	}

	// 4. Kiểm tra xem user có bị khóa trong lúc refresh không
	if user.Disabled {
		return nil, errors.New("user account is disabled")
	}

	// 5. Cấp cặp token mới
	return s.generateTokenPair(ctx, user)
}

// ResendVerification (Tương đương resendVerification)
func (s *AuthService) ResendVerification(ctx context.Context, email string) error {
	user, err := s.repo.FindUserByEmail(ctx, email)
	if err != nil || user == nil {
		return errors.New("user not found")
	}

	if !user.Disabled {
		return errors.New("user is already verified")
	}

	return s.sendVerificationFlow(ctx, user.ID, user.Email)
}

// SignOut (Tương đương signoutUser)
func (s *AuthService) SignOut(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	// Tìm token (nếu tồn tại)
	storedToken, err := s.repo.FindValidRefreshToken(ctx, refreshToken)
	if err != nil || storedToken == nil {
		return nil // Coi như đã đăng xuất
	}

	// Thu hồi
	return s.repo.RevokeRefreshToken(ctx, storedToken.ID)
}

// GetMe (Lấy thông tin profile)
func (s *AuthService) GetMe(ctx context.Context, userID string) (*models.User, error) {
	return s.repo.FindUserByID(ctx, userID)
}

// --- Private Helpers ---

// generateTokenPair sinh Access Token (JWT) và Refresh Token (UUID)
func (s *AuthService) generateTokenPair(ctx context.Context, user *models.User) (*models.AuthResponse, error) {
	// 1. Create Access Token (15 phút)
	accessToken, err := s.tokenMaker.CreateToken(user.ID, 15*time.Minute)
	if err != nil {
		return nil, err
	}

	// 2. Create Refresh Token (UUID - 7 ngày)
	refreshTokenStr, err := s.tokenMaker.CreateRefreshToken()
	if err != nil {
		return nil, err
	}

	// 3. Lưu Refresh Token vào DB
	refreshTokenModel := &models.RefreshToken{
		Token:     refreshTokenStr,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(24 * 7 * time.Hour),
		Revoked:   false,
	}

	if err := s.repo.CreateRefreshToken(ctx, refreshTokenModel); err != nil {
		return nil, err
	}

	return &models.AuthResponse{
		User:         user,
		AccessToken:  accessToken,
		RefreshToken: refreshTokenStr,
	}, nil
}

// sendVerificationFlow tạo mã OTP và gửi mail (helper nội bộ)
func (s *AuthService) sendVerificationFlow(ctx context.Context, userID, email string) error {
	// 1. Sinh mã 6 số
	code, err := utils.Gen6DigitCode()
	if err != nil {
		return err
	}

	// 2. Lưu vào DB (TTL 15 phút)
	if err := s.verifyRepo.Create(ctx, userID, email, code, 15*time.Minute); err != nil {
		return err
	}

	// 3. Gửi email (Chạy Async để response nhanh)
	go func() {
		// Tạo context riêng hoặc background context để tránh bị cancel khi request chính kết thúc
		if err := s.emailSender.SendVerificationEmail(email, code); err != nil {
			log.Printf("ERROR: Failed to send email to %s: %v", email, err)
		}
	}()

	return nil
}
