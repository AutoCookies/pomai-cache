package firebase_setup

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
)

var (
	app             *firebase.App
	firestoreClient *firestore.Client
	authClient      *auth.Client
	once            sync.Once
)

// ServiceAccountConfig mô phỏng cấu trúc JSON của Service Account Google
type ServiceAccountConfig struct {
	Type                    string `json:"type"`
	ProjectID               string `json:"project_id"`
	PrivateKeyID            string `json:"private_key_id"`
	PrivateKey              string `json:"private_key"`
	ClientEmail             string `json:"client_email"`
	ClientID                string `json:"client_id"`
	AuthURI                 string `json:"auth_uri"`
	TokenURI                string `json:"token_uri"`
	AuthProviderX509CertURL string `json:"auth_provider_x509_cert_url"`
	ClientX509CertURL       string `json:"client_x509_cert_url"`
}

// buildServiceAccountFromEnv đọc biến môi trường và tạo JSON config
func buildServiceAccountFromEnv() []byte {
	// Ưu tiên 1: Chuỗi JSON đầy đủ
	if jsonStr := os.Getenv("FIREBASE_SERVICE_ACCOUNT_JSON"); jsonStr != "" {
		// Xử lý xuống dòng trong private key nếu cần
		if strings.Contains(jsonStr, "\\n") {
			jsonStr = strings.ReplaceAll(jsonStr, "\\n", "\n")
		}
		return []byte(jsonStr)
	}

	// Ưu tiên 2: Các biến môi trường rời rạc
	projectID := os.Getenv("FIREBASE_PROJECT_ID")
	privateKey := os.Getenv("FIREBASE_PRIVATE_KEY")
	clientEmail := os.Getenv("FIREBASE_CLIENT_EMAIL")

	if projectID == "" || privateKey == "" || clientEmail == "" {
		return nil
	}

	// Fix lỗi xuống dòng trong Private Key (rất phổ biến khi lưu trong .env)
	privateKey = strings.ReplaceAll(privateKey, "\\n", "\n")

	config := ServiceAccountConfig{
		Type:                    "service_account",
		ProjectID:               projectID,
		PrivateKeyID:            os.Getenv("FIREBASE_PRIVATE_KEY_ID"),
		PrivateKey:              privateKey,
		ClientEmail:             clientEmail,
		ClientID:                os.Getenv("FIREBASE_CLIENT_ID"),
		AuthURI:                 os.Getenv("FIREBASE_AUTH_URI"),
		TokenURI:                os.Getenv("FIREBASE_TOKEN_URI"),
		AuthProviderX509CertURL: os.Getenv("FIREBASE_AUTH_PROVIDER_X509_CERT_URL"),
		ClientX509CertURL:       os.Getenv("FIREBASE_CLIENT_X509_CERT_URL"),
	}

	jsonBytes, err := json.Marshal(config)
	if err != nil {
		log.Printf("Error marshalling service account config: %v", err)
		return nil
	}
	return jsonBytes
}

// Initialize khởi tạo kết nối (Singleton)
func Initialize() {
	once.Do(func() {
		ctx := context.Background()
		var opts []option.ClientOption

		// 1. Cố gắng tạo credential từ biến môi trường
		saBytes := buildServiceAccountFromEnv()
		projectID := os.Getenv("FIREBASE_PROJECT_ID")
		if projectID == "" {
			projectID = os.Getenv("GCLOUD_PROJECT")
		}

		if saBytes != nil {
			opts = append(opts, option.WithCredentialsJSON(saBytes))
		} else {
			// Nếu không có credential explicit, SDK sẽ tự tìm GOOGLE_APPLICATION_CREDENTIALS (ADC)
			// Không cần làm gì thêm
		}

		// Config cho Firebase App
		conf := &firebase.Config{ProjectID: projectID}

		// 2. Khởi tạo App
		var err error
		app, err = firebase.NewApp(ctx, conf, opts...)
		if err != nil {
			log.Fatalf("error initializing firebase app: %v", err)
		}

		// 3. Khởi tạo Firestore Client
		// Lưu ý: Go SDK tự động check biến môi trường FIRESTORE_EMULATOR_HOST
		// nên không cần setup thủ công như Node.js
		firestoreClient, err = app.Firestore(ctx)
		if err != nil {
			log.Fatalf("error initializing firestore client: %v", err)
		}

		// 4. Khởi tạo Auth Client
		authClient, err = app.Auth(ctx)
		if err != nil {
			log.Fatalf("error initializing auth client: %v", err)
		}

		fmt.Println("[Firebase] Initialized successfully")
	})
}

// GetDB trả về Firestore Client instance
func GetDB() *firestore.Client {
	if firestoreClient == nil {
		Initialize()
	}
	return firestoreClient
}

// GetAuthClient trả về Auth Client instance
func GetAuthClient() *auth.Client {
	if authClient == nil {
		Initialize()
	}
	return authClient
}
