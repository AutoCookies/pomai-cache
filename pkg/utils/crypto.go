package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
)

// Gen6DigitCode tạo chuỗi số ngẫu nhiên 6 chữ số (000000 - 999999)
func Gen6DigitCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	// Pad thêm số 0 ở đầu nếu cần để đủ 6 ký tự
	return fmt.Sprintf("%06d", n), nil
}

// HashCode băm chuỗi bằng SHA-256 (giống logic JS cũ)
func HashCode(code string) string {
	hash := sha256.Sum256([]byte(code))
	return hex.EncodeToString(hash[:])
}
