package scanapi

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// ServiceAuthMiddleware protects internal scan routes with a static bearer token.
// Rejects every request when expectedToken is empty (fail closed).
func ServiceAuthMiddleware(expectedToken string, next http.Handler) http.Handler {
	expected := strings.TrimSpace(expectedToken)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get(headerAuthorization))
		if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
			writeServiceAuthError(w, r)
			return
		}
		raw := strings.TrimSpace(header[len("Bearer "):])
		if raw == "" || expected == "" {
			writeServiceAuthError(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(raw), []byte(expected)) != 1 {
			writeServiceAuthError(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeServiceAuthError(w http.ResponseWriter, r *http.Request) {
	if rid := strings.TrimSpace(r.Header.Get(headerRequestID)); rid != "" {
		w.Header().Set(headerRequestID, rid)
	}
	writeJSON(w, http.StatusUnauthorized, map[string]string{
		"error":   "service_auth_required",
		"message": "Valid service bearer token is required.",
	})
}
