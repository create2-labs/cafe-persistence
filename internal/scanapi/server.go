// Package scanapi implements the internal scan HTTP API (PERS-D3a-impl).
package scanapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"cafe-persistence/internal/scanroutes"

	"github.com/rs/zerolog/log"
)

// Server exposes the internal scan API on a dedicated HTTP listener.
type Server struct {
	srv *http.Server
}

// NewServer registers routes from openapi/internal/scan/v1.yaml on host:port.
func NewServer(host, port string, serviceToken string, h *Handler) *Server {
	mux := http.NewServeMux()
	base := scanroutes.V1Base

	register := func(method, rel string, fn http.HandlerFunc) {
		pattern := method + " " + base + rel
		mux.HandleFunc(pattern, fn)
	}

	register("POST", scanroutes.PendingWallet, h.ReserveWalletPending)
	register("GET", scanroutes.PendingWallet, h.GetWalletPendingByAddress)
	register("DELETE", scanroutes.PendingWallet, h.ReleaseWalletPendingReservation)
	register("POST", scanroutes.PendingTLS, h.PutTLSPending)
	register("GET", scanroutes.PendingByScanID, h.GetPendingScan)
	register("DELETE", scanroutes.PendingByScanID, h.ReleasePendingScan)
	register("GET", scanroutes.WalletScans, h.ListWalletScans)
	register("GET", scanroutes.WalletScanByID, h.GetWalletScan)
	register("DELETE", scanroutes.WalletScanByID, h.DeleteWalletScan)
	register("GET", scanroutes.TLSScans, h.ListTLSScans)
	register("GET", scanroutes.TLSScansDefaults, h.ListTLSDefaultScans)
	register("GET", scanroutes.TLSScanByID, h.GetTLSScan)
	register("DELETE", scanroutes.TLSScanByID, h.DeleteTLSScan)
	register("GET", scanroutes.LedgerUsage, h.GetScanLedgerUsage)

	addr := fmt.Sprintf("%s:%s", host, port)
	s := &Server{
		srv: &http.Server{
			Addr:              addr,
			Handler:           ServiceAuthMiddleware(serviceToken, mux),
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
	return s
}

// Start serves HTTP in a background goroutine.
func (s *Server) Start() {
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("internal scan API server failed")
		}
	}()
}

// Shutdown stops the HTTP server gracefully.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// HTTPHandler exposes the root handler (auth + routes) for contract tests.
func (s *Server) HTTPHandler() http.Handler {
	return s.srv.Handler
}
