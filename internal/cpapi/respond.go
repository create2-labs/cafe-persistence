package cpapi

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

func writeServiceUnavailable(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusServiceUnavailable, "PERSISTENCE_UNAVAILABLE", "Persistence is temporarily unavailable.")
}

func writeBadRequest(w http.ResponseWriter, r *http.Request, message string) {
	writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", message)
}

func writeInvalidWalletAddress(w http.ResponseWriter, r *http.Request, message string) {
	writeError(w, r, http.StatusBadRequest, "INVALID_WALLET_ADDRESS", message)
}

func writePolicyNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusNotFound, "POLICY_NOT_FOUND", "Policy not found.")
}

func writeForbidden(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusForbidden, "FORBIDDEN", "Resource is not accessible for the scoped owner.")
}

func writePolicyAlreadyExists(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusConflict, "POLICY_ALREADY_EXISTS", "An active crypto policy already exists for this wallet.")
}
