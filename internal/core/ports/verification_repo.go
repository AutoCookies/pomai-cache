package ports

import (
	"context"
	"time"
)

// VerificationRepository xử lý việc lưu và kiểm tra mã xác thực
type VerificationRepository interface {
	// Create lưu mã xác thực mới vào DB
	Create(ctx context.Context, userId, email, code string, ttl time.Duration) error

	// Validate kiểm tra mã, đánh dấu đã dùng, và quản lý số lần thử (attempts)
	// Trả về true nếu hợp lệ, false nếu sai/hết hạn
	Validate(ctx context.Context, userId, email, code string) (bool, error)
}
