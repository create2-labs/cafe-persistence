package domain

import "time"

// AccountType represents the type of Ethereum account
type AccountType string

const (
	AccountTypeEOA      AccountType = "EOA"
	AccountTypeAA       AccountType = "AA" // Abstract Account (ERC-4337)
	AccountTypeContract AccountType = "Contract"
	AccountTypeUnknown  AccountType = "unknown"
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

// PublicKeyRecovery is the outcome of ECDSA public-key recovery for one wallet scan.
type PublicKeyRecovery string

const (
	PublicKeyRecoveryNotRequired PublicKeyRecovery = "not_required"
	PublicKeyRecoveryRecovered   PublicKeyRecovery = "recovered"
	PublicKeyRecoveryUnresolved  PublicKeyRecovery = "unresolved"
)

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
