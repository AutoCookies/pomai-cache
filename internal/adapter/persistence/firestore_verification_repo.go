package persistence

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/AutoCookies/pomai-cache/internal/core/ports"
	"github.com/AutoCookies/pomai-cache/pkg/utils"
)

const (
	verificationColl = "email_verifications"
	plaintextColl    = "email_verification_plaintext"
)

type FirestoreVerificationRepo struct {
	client *firestore.Client
}

// Đảm bảo struct implement interface
var _ ports.VerificationRepository = (*FirestoreVerificationRepo)(nil)

func NewFirestoreVerificationRepo(client *firestore.Client) *FirestoreVerificationRepo {
	return &FirestoreVerificationRepo{client: client}
}

// Create tương ứng với createVerificationRecord trong JS
func (r *FirestoreVerificationRepo) Create(ctx context.Context, userId, email, code string, ttl time.Duration) error {
	now := time.Now()
	expiresAt := now.Add(ttl)
	codeHash := utils.HashCode(code)

	payload := map[string]interface{}{
		"userId":    userId, // Có thể là empty string
		"email":     email,
		"codeHash":  codeHash,
		"attempts":  0,
		"used":      false,
		"createdAt": now,
		"expiresAt": expiresAt,
	}

	docRef, _, err := r.client.Collection(verificationColl).Add(ctx, payload)
	if err != nil {
		return err
	}

	// Logic TEST_MODE: Lưu plaintext code nếu đang chạy test
	if os.Getenv("TEST_MODE") == "true" {
		go func() {
			// Chạy background để không block request chính
			_, _, _ = r.client.Collection(plaintextColl).Add(context.Background(), map[string]interface{}{
				"email":             email,
				"code":              code,
				"verificationDocId": docRef.ID,
				"createdAt":         now,
			})
		}()
	}

	return nil
}

// Validate tương ứng với validateVerificationCode trong JS
func (r *FirestoreVerificationRepo) Validate(ctx context.Context, userId, email, code string) (bool, error) {
	if code == "" {
		return false, fmt.Errorf("code required")
	}
	codeHash := utils.HashCode(code)
	maxAttempts := 5

	// 1. Query tìm record (limit 5 giống JS)
	coll := r.client.Collection(verificationColl)
	var q firestore.Query

	if userId != "" {
		q = coll.Where("userId", "==", userId).Where("used", "==", false).OrderBy("createdAt", firestore.Desc).Limit(5)
	} else if email != "" {
		q = coll.Where("email", "==", email).Where("used", "==", false).OrderBy("createdAt", firestore.Desc).Limit(5)
	} else {
		return false, fmt.Errorf("userId or email required")
	}

	iter := q.Documents(ctx)
	docs, err := iter.GetAll()
	if err != nil {
		return false, err
	}

	if len(docs) == 0 {
		return false, nil // Không tìm thấy
	}

	// 2. Tìm doc hợp lệ đầu tiên (chưa hết hạn, khớp hash)
	now := time.Now()
	var foundDoc *firestore.DocumentSnapshot

	for _, doc := range docs {
		data := doc.Data()

		// Check expired
		expiresAt, ok := data["expiresAt"].(time.Time)
		if !ok {
			continue // Skip bad data
		}
		if expiresAt.Before(now) {
			continue
		}

		// Check hash
		storedHash, _ := data["codeHash"].(string)
		if storedHash == codeHash {
			foundDoc = doc
			break
		}
	}

	// 3. Xử lý logic Attempts (nếu không tìm thấy khớp hash)
	if foundDoc == nil {
		// Logic JS: Increment attempts on the *most recent* doc
		mostRecent := docs[0] // Docs đã sort Desc nên phần tử 0 là mới nhất

		// Transaction an toàn để tăng attempts
		err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
			docRef := mostRecent.Ref
			snap, err := tx.Get(docRef)
			if err != nil {
				return err
			}

			currentAttempts, _ := snap.Data()["attempts"].(int64) // Firestore int thường là int64
			newAttempts := currentAttempts + 1

			updates := []firestore.Update{
				{Path: "attempts", Value: newAttempts},
			}

			// Nếu quá số lần thử -> khóa luôn (mark used)
			if newAttempts >= int64(maxAttempts) {
				updates = append(updates, firestore.Update{Path: "used", Value: true})
			}

			return tx.Update(docRef, updates)
		})

		if err != nil {
			log.Printf("Failed to update attempts: %v", err)
		}

		return false, nil // Mã sai
	}

	// 4. Nếu tìm thấy và đúng mã -> Mark used
	_, err = foundDoc.Ref.Update(ctx, []firestore.Update{
		{Path: "used", Value: true},
		{Path: "usedAt", Value: now},
	})
	if err != nil {
		return false, err
	}

	return true, nil
}
