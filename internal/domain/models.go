package domain

import "time"

// AccountType represents the type of Ethereum account
type AccountType string

const (
	AccountTypeEOA      AccountType = "EOA"
	AccountTypeAA       AccountType = "AA" // Abstract Account (ERC-4337)
	AccountTypeContract AccountType = "Contract"
)

// Algorithm represents the cryptographic algorithm used
type Algorithm string

const (
	AlgorithmECDSAsecp256k1 Algorithm = "ECDSA-secp256k1"
)

// NISTLevel represents the NIST quantum-security level
type NISTLevel int

const (
	NISTLevel1 NISTLevel = 1 // Quantum-broken
	NISTLevel2 NISTLevel = 2
	NISTLevel3 NISTLevel = 3
	NISTLevel4 NISTLevel = 4
	NISTLevel5 NISTLevel = 5 // PQC-ready
)

// RiskCategory represents the risk category
type RiskCategory string

const (
	RiskHigh     RiskCategory = "High"
	RiskMedium   RiskCategory = "Medium"
	RiskPQCReady RiskCategory = "PQC-ready"
)

// ScanResult represents the result of a wallet discovery scan
type ScanResult struct {
	Address         string      `json:"address"`
	Type            AccountType `json:"type"`
	Algorithm       Algorithm   `json:"algorithm"`
	NISTLevel       NISTLevel   `json:"nist_level"`
	KeyExposed      bool        `json:"key_exposed"`
	PublicKey       string      `json:"public_key,omitempty"`       // Recovered public key if available
	TransactionHash string      `json:"transaction_hash,omitempty"` // Hash of transaction that exposed the key
	ExposedNetwork  string      `json:"exposed_network,omitempty"`  // Network where key was exposed
	IsEOA           bool        `json:"is_eoa"`
	IsERC4337       bool        `json:"is_erc4337"`
	RiskScore       float64     `json:"risk_score"`
	FirstSeen       *time.Time  `json:"first_seen,omitempty"`
	LastSeen        *time.Time  `json:"last_seen,omitempty"`
	ScannedAt       time.Time   `json:"scanned_at"`
	Networks        []string    `json:"networks"`
	Connections     []string    `json:"connections"`
}

// NetworkResult represents the scan result for a specific network
type NetworkResult struct {
	Network          string
	IsEOA            bool
	IsERC4337        bool // True if contract implements ERC-4337 (Account Abstraction)
	IsKeyExposed     bool
	TransactionCount uint64
	PublicKey        string // Recovered public key if available
	TransactionHash  string // Hash of transaction that exposed the key (if available)
}

// TLSScanResult represents the result of a TLS certificate scan
type TLSScanResult struct {
	URL             string            `json:"url"`
	Host            string            `json:"host"`
	Port            int               `json:"port"`
	Certificate     CertificateInfo   `json:"certificate"`
	CipherSuites    []CipherSuiteInfo `json:"cipher_suites"`
	ProtocolVersion string            `json:"protocol_version"`
	NISTLevel       NISTLevel         `json:"nist_level"`
	RiskScore       float64           `json:"risk_score"`
	PQCRisk         string            `json:"pqc_risk"`                // "safe", "warning", "critical"
	SupportedPQCs   []string          `json:"supported_pqc,omitempty"` // List of supported PQC algorithms
	Recommendations []string          `json:"recommendations,omitempty"`
	ScannedAt       time.Time         `json:"scanned_at"`
	// PQC-specific information from OQS/OpenSSL scan
	KexAlgorithm string         `json:"kex_algorithm,omitempty"` // Key exchange algorithm (group)
	KexPQCReady  bool           `json:"kex_pqc_ready,omitempty"` // Whether KEX uses PQC
	PQCMode      string         `json:"pqc_mode,omitempty"`      // "classical", "hybrid", "pure"
	PFS          bool           `json:"pfs,omitempty"`           // Perfect Forward Secrecy
	ALPN         string         `json:"alpn,omitempty"`          // Application-Layer Protocol Negotiation
	OCSPStapled  bool           `json:"ocsp_stapled,omitempty"`  // OCSP stapling
	Curve        string         `json:"curve,omitempty"`         // ECDSA curve name
	NISTLevels   map[string]int `json:"nist_levels,omitempty"`   // Detailed NIST levels (kex, sig, cipher, hkdf, session)
	Default      bool           `json:"default,omitempty"`       // Whether this is a default endpoint
}

// CertificateInfo represents TLS certificate information
type CertificateInfo struct {
	Subject            string    `json:"subject"`
	Issuer             string    `json:"issuer"`
	SignatureAlgorithm string    `json:"signature_algorithm"`
	PublicKeyAlgorithm string    `json:"public_key_algorithm"`
	KeySize            int       `json:"key_size"`
	NotBefore          time.Time `json:"not_before"`
	NotAfter           time.Time `json:"not_after"`
	SerialNumber       string    `json:"serial_number"`
	NISTLevel          NISTLevel `json:"nist_level"`
	IsPQCReady         bool      `json:"is_pqc_ready"`
}

// CipherSuiteInfo represents TLS cipher suite information
type CipherSuiteInfo struct {
	ID          uint16    `json:"id"`
	Name        string    `json:"name"`
	KeyExchange string    `json:"key_exchange"`
	Encryption  string    `json:"encryption"`
	MAC         string    `json:"mac"`
	NISTLevel   NISTLevel `json:"nist_level"`
	IsPQCReady  bool      `json:"is_pqc_ready"`
}
