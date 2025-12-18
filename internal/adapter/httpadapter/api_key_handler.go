package httpadapter

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/AutoCookies/pomai-cache/internal/core/services"
)

type APIKeyHandler struct {
	service services.APIKeyService
}

func NewAPIKeyHandler(service services.APIKeyService) *APIKeyHandler {
	return &APIKeyHandler{service: service}
}

// HandleGenerate xử lý tạo API_KEY mới
func (h *APIKeyHandler) HandleGenerate(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	expiryDaysStr := r.URL.Query().Get("expiry_days")

	expiryDays, err := strconv.Atoi(expiryDaysStr)
	if err != nil {
		http.Error(w, "Invalid expiry_days parameter", http.StatusBadRequest)
		return
	}

	apiKey, err := h.service.GenerateAPIKey(tenantID, expiryDays)
	if err != nil {
		http.Error(w, "Could not generate API Key: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apiKey)
}

// HandleValidate xử lý xác thực API_KEY
func (h *APIKeyHandler) HandleValidate(w http.ResponseWriter, r *http.Request) {
	apiKey := r.URL.Query().Get("key")

	isValid, err := h.service.ValidateAPIKey(apiKey)
	if err != nil {
		http.Error(w, "API Key is invalid: "+err.Error(), http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"is_valid": isValid})
}
