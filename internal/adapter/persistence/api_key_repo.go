package persistence

import (
	"errors"

	"github.com/AutoCookies/pomai-cache/internal/core/models"
	"github.com/AutoCookies/pomai-cache/internal/core/ports"
)

type APIKeyRepo struct {
	db map[string]models.APIKeyModel // Simplified in-memory database for demo purposes
}

func NewAPIKeyRepo() ports.APIKeyRepository {
	return &APIKeyRepo{db: make(map[string]models.APIKeyModel)}
}

func (r *APIKeyRepo) CreateAPIKey(apiKey models.APIKeyModel) error {
	r.db[apiKey.ID] = apiKey
	return nil
}

func (r *APIKeyRepo) GetAPIKeyByID(apiKeyID string) (models.APIKeyModel, error) {
	if apiKey, exists := r.db[apiKeyID]; exists {
		return apiKey, nil
	}
	return models.APIKeyModel{}, errors.New("API Key not found")
}

func (r *APIKeyRepo) GetAPIKeyByKey(key string) (models.APIKeyModel, error) {
	for _, apiKey := range r.db {
		if apiKey.Key == key {
			return apiKey, nil
		}
	}
	return models.APIKeyModel{}, errors.New("API Key not found")
}

func (r *APIKeyRepo) DeactivateAPIKey(apiKeyID string) error {
	apiKey, err := r.GetAPIKeyByID(apiKeyID)
	if err != nil {
		return err
	}
	apiKey.IsActive = false
	r.db[apiKeyID] = apiKey
	return nil
}
