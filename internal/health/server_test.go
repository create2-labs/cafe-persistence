package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthAlwaysOK(t *testing.T) {
	state := &State{}
	srv := NewServer("127.0.0.1", "0", state)

	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status = %v, want ok", body["status"])
	}
}

func TestReadyNotReadyUntilSet(t *testing.T) {
	state := &State{}
	srv := NewServer("127.0.0.1", "0", state)

	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /ready before ready status = %d, want 503", rec.Code)
	}

	state.SetReady(true)
	rec2 := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET /ready after ready status = %d, want 200", rec2.Code)
	}
}
