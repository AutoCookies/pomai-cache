package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

// HandleHealth checks server health
func (h *HTTPHandlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	stats := h.Tenants.StatsAll()
	health := map[string]interface{}{
		"status":      "ok",
		"timestamp":   time.Now().Unix(),
		"total_users": len(stats),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(health)
}

// HandleStats returns detailed statistics
func (h *HTTPHandlers) HandleStats(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	w.Header().Set("Content-Type", "application/json")

	if userID != "" {
		if st, ok := h.Tenants.StatsForTenant(userID); ok {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"userId": userID,
				"stats":  st,
			})
			return
		}
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	statsMap := h.Tenants.StatsAll()
	perUser := make(map[string]interface{}, len(statsMap))
	for k, v := range statsMap {
		perUser[k] = v
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"total_users": len(statsMap),
		"per_user":    perUser,
	})
}
