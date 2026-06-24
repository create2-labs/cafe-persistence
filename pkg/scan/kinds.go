package scan

// Kind constants for discovery scan types (stable; used for registry and billing).
const (
	KindTLS    = "tls"
	KindWallet = "wallet"
)

// Plan limit keys (used by PlanService and plugin descriptors; may differ from Kind).
// TLS scans are limited by "endpoint" (EndpointScanLimit), wallet by "wallet" (WalletScanLimit).
const (
	PlanLimitKeyEndpoint = "endpoint"
	PlanLimitKeyWallet   = "wallet" // same as KindWallet
)
