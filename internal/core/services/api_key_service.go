package services

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/AutoCookies/pomai-cache/internal/core/models"
	"github.com/AutoCookies/pomai-cache/internal/core/ports"
)

type APIKeyService struct {
	repo ports.APIKeyRepository
}

func NewAPIKeyService(repo ports.APIKeyRepository) ports.APIKeyService {
	return &APIKeyService{repo: repo}
}

func (s *APIKeyService) GenerateAPIKey(tenantID string, expiryDays int) (models.APIKeyModel, error) {
	bytes := make([]byte, 16)
	rand.Read(bytes)

	apiKey := models.APIKeyModel{
		ID:        hex.EncodeToString(bytes),
		Key:       hex.EncodeToString(bytes),
		TenantID:  tenantID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Duration(expiryDays) * 24 * time.Hour),
		IsActive:  true,
	}

	err := s.repo.CreateAPIKey(apiKey)
	return apiKey, err
}

func (s *APIKeyService) ValidateAPIKey(key string) (bool, error) {
	apiKey, err := s.repo.GetAPIKeyByKey(key)
	if err != nil {
		return false, err
	}

	if !apiKey.IsActive || time.Now().After(apiKey.ExpiresAt) {
		return false, nil
	}
	return true, nil
}
