// Package scanroutes defines canonical path constants for the internal scan API (PERS-D3a-spec).
// Handlers, contract tests, and Discovery HTTP clients import these literals.
package scanroutes

// V1Base is the internal scan API prefix on the persistence HTTP listener (not public edge).
const V1Base = "/internal/scan/v1"

// OpenAPI-relative paths (see openapi/internal/scan/v1.yaml paths block; server url = V1Base).
const (
	PendingWallet    = "/pending/wallet"
	PendingTLS       = "/pending/tls"
	PendingByScanID  = "/pending/{scan_id}"
	WalletScans      = "/wallets/scans"
	WalletScanByID   = "/wallets/scans/{scan_id}"
	TLSScans         = "/tls/scans"
	TLSScansDefaults = "/tls/scans/defaults"
	TLSScanByID      = "/tls/scans/{scan_id}"
	LedgerUsage      = "/ledger/usage"
)

// Join returns the absolute mux path for a V1-relative route.
func Join(relative string) string {
	return V1Base + relative
}
