package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// State tracks persistenceStorage scan migrations and NATS subscriptions are ready.
type State struct {
	ready atomic.Bool
}

// SetReady marks the service as ready to accept scan traffic.
func (s *State) SetReady(ready bool) {
	s.ready.Store(ready)
}

// IsReady reports whether scan migrations and NATS are operational.
func (s *State) IsReady() bool {
	return s.ready.Load()
}

// Server exposes liveness (/health) and readiness (/ready) probes for orchestrators.
type Server struct {
	state *State
	srv   *http.Server
}

// NewServer binds health endpoints on host:port.
func NewServer(host, port string, state *State) *Server {
	mux := http.NewServeMux()
	s := &Server{state: state}

	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)

	addr := fmt.Sprintf("%s:%s", host, port)
	s.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return s
}

// Start serves HTTP in a background goroutine.
func (s *Server) Start() {
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("health server failed")
		}
	}()
}

// Shutdown stops the HTTP server gracefully.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"app_name":  "Cafe Persistence Service",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if !s.state.IsReady() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":    "not_ready",
			"app_name":  "Cafe Persistence Service",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"checks": map[string]any{
				"scan_migrations": false,
				"nats":            false,
			},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"app_name":  "Cafe Persistence Service",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"checks": map[string]any{
			"scan_migrations": true,
			"nats":            true,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
