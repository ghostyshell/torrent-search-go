package handlers

import (
	"encoding/json"
	"net/http"
)

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, `{"success":false,"error":"Failed to encode response"}`, http.StatusInternalServerError)
	}
}

// writeError writes an error response
func writeError(w http.ResponseWriter, message string, code string, status int) {
	writeJSON(w, status, map[string]interface{}{
		"success": false,
		"error":   message,
		"code":    code,
	})
}
