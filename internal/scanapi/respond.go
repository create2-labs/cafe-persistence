package scanapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	if rid := strings.TrimSpace(r.Header.Get(headerRequestID)); rid != "" {
		w.Header().Set(headerRequestID, rid)
	}
	writeJSON(w, status, map[string]string{
		"error":   code,
		"message": message,
	})
}

func writeServiceUnavailable(w http.ResponseWriter, r *http.Request, message string) {
	writeError(w, r, http.StatusServiceUnavailable, "service_unavailable", message)
}

func writeBadRequest(w http.ResponseWriter, r *http.Request, message string) {
	writeError(w, r, http.StatusBadRequest, "invalid_request", message)
}

func writeNotFound(w http.ResponseWriter, r *http.Request, message string) {
	writeError(w, r, http.StatusNotFound, "not_found", message)
}
