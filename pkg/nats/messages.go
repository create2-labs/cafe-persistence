package nats

import "github.com/google/uuid"

// WalletScanMessage represents a wallet scan request message
type WalletScanMessage struct {
	ScanID  uuid.UUID `json:"scan_id"`  // Backend generates; used for idempotency and event correlation
	UserID  uuid.UUID `json:"user_id"`
	Address string    `json:"address"`
}

// TLSScanMessage represents a TLS scan request message
type TLSScanMessage struct {
	ScanID    uuid.UUID `json:"scan_id"`    // Backend generates; used for idempotency and event correlation
	UserID    uuid.UUID `json:"user_id"`     // uuid.Nil for default endpoints
	Endpoint  string    `json:"endpoint"`
	IsDefault bool      `json:"is_default"`  // true when scanning default endpoints at startup
}

// ScannerPresenceEvent is the event type for scanner presence messages.
const (
	ScannerPresenceJoined = "joined"
	ScannerPresenceLeft   = "left"
)

// ScannerPresenceMessage is published by scanners on start (joined), periodically (joined heartbeat), and on shutdown (left).
type ScannerPresenceMessage struct {
	Event     string `json:"event"`     // "joined" or "left"
	ScannerID string `json:"scanner_id"`
	Type      string `json:"type"`      // "tls" or "wallet"
}

// ScanStartedMessage is published by scanners when a scan begins (consumed by persistence-service).
type ScanStartedMessage struct {
	ScanID    uuid.UUID `json:"scan_id"`
	Kind      string    `json:"kind"` // "tls" or "wallet"
	UserID    uuid.UUID `json:"user_id"`
	StartedAt string    `json:"started_at"` // RFC3339
	// Target identifies the scanned resource (url for TLS, address for wallet)
	Endpoint string `json:"endpoint,omitempty"`
	Address  string `json:"address,omitempty"`
}

// ScanCompletedMessage is published by scanners when a scan succeeds (consumed by persistence-service).
type ScanCompletedMessage struct {
	ScanID      uuid.UUID   `json:"scan_id"`
	Kind        string      `json:"kind"`
	UserID      uuid.UUID   `json:"user_id"`
	Findings    interface{} `json:"findings,omitempty"`
	Metadata    interface{} `json:"metadata,omitempty"`
	CompletedAt string      `json:"completed_at"`
	Endpoint    string      `json:"endpoint,omitempty"`
	Address     string      `json:"address,omitempty"`
	// Result is the full domain result (TLSScanResult or ScanResult) for Redis write-through
	Result interface{} `json:"result,omitempty"`
}

// ScanFailedMessage is published by scanners when a scan fails (consumed by persistence-service).
type ScanFailedMessage struct {
	ScanID      uuid.UUID `json:"scan_id"`
	Kind        string    `json:"kind"`
	UserID      uuid.UUID `json:"user_id"`
	Error       string    `json:"error"`
	CompletedAt string    `json:"completed_at"`
	Endpoint   string    `json:"endpoint,omitempty"`
	Address    string    `json:"address,omitempty"`
}

// ScanReadyMessage is published by persistence-service after writing a scan result (success or failure) to Redis and Postgres.
// API backend can subscribe when a scan result is persisted (e.g. logging or metrics).
type ScanReadyMessage struct {
	UserID   uuid.UUID `json:"user_id"`
	Kind     string    `json:"kind"`     // "tls" or "wallet"
	Endpoint string    `json:"endpoint,omitempty"`
	Address  string    `json:"address,omitempty"`
	Status   string    `json:"status"`   // "success" or "failed"
}

// ScannerHeartbeatMessage is published by scanners every 5s (backend tracks last_seen in Redis).
type ScannerHeartbeatMessage struct {
	ScannerID string    `json:"scanner_id"`
	Kind      string    `json:"kind"` // "tls" or "wallet"
	Timestamp string    `json:"timestamp"` // RFC3339
}
