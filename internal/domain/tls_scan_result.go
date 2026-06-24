package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TLSScanResultEntity represents a TLS scan result stored in the database
type TLSScanResultEntity struct {
	ID              uuid.UUID  `gorm:"type:char(36);primary_key" json:"id"`
	UserID          *uuid.UUID `gorm:"type:char(36);index" json:"user_id,omitempty"` // NULL for default endpoints
	URL             string     `gorm:"type:varchar(500);not null;index" json:"url"`
	Host            string    `gorm:"type:varchar(255);not null" json:"host"`
	Port            int       `gorm:"not null" json:"port"`
	ProtocolVersion string    `gorm:"type:varchar(20);not null" json:"protocol_version"`
	NISTLevel       NISTLevel `gorm:"not null" json:"nist_level"`
	RiskScore       float64   `gorm:"not null" json:"risk_score"`
	PQCRisk         string    `gorm:"type:varchar(20);not null" json:"pqc_risk"`
	Certificate     string    `gorm:"type:text" json:"-"` // JSON stored as text
	CipherSuites    string    `gorm:"type:text" json:"-"` // JSON array stored as text
	SupportedPQCs   string    `gorm:"type:text" json:"-"` // JSON array stored as text
	Recommendations string    `gorm:"type:text" json:"-"` // JSON array stored as text
	// PQC-specific fields
	KexAlgorithm string         `gorm:"type:varchar(100)" json:"kex_algorithm,omitempty"`
	KexPQCReady  bool           `gorm:"default:false" json:"kex_pqc_ready,omitempty"`
	PQCMode      string         `gorm:"type:varchar(20)" json:"pqc_mode,omitempty"` // classical, hybrid, pure
	PFS          bool           `gorm:"default:false" json:"pfs,omitempty"`
	ALPN         string         `gorm:"type:varchar(100)" json:"alpn,omitempty"`
	OCSPStapled  bool           `gorm:"default:false" json:"ocsp_stapled,omitempty"`
	Curve        string         `gorm:"type:varchar(50)" json:"curve,omitempty"`
	NISTLevels   string         `gorm:"type:text" json:"-"`                                    // JSON map stored as text
	Default      bool           `gorm:"column:default;default:false" json:"default,omitempty"` // Whether this is a default endpoint
	Status       string         `gorm:"type:varchar(20);index" json:"status"` // empty/PENDING until scan.started (RUNNING); then SUCCESS, FAILED, TIMEOUT, UNREACHABLE
	Error        string         `gorm:"type:text" json:"error,omitempty"`                     // Error message when status is FAILED
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for GORM
func (TLSScanResultEntity) TableName() string {
	return "tls_scan_results"
}

// BeforeCreate hook to generate UUID before creating a TLS scan result
func (t *TLSScanResultEntity) BeforeCreate(tx *gorm.DB) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return nil
}

// ToTLSScanResult converts the entity to the domain TLSScanResult DTO
func (t *TLSScanResultEntity) ToTLSScanResult() *TLSScanResult {
	var certInfo CertificateInfo
	var cipherSuites []CipherSuiteInfo
	var supportedPQCs []string
	var recommendations []string

	// Parse JSON fields
	if t.Certificate != "" {
		_ = json.Unmarshal([]byte(t.Certificate), &certInfo)
	}
	if t.CipherSuites != "" {
		_ = json.Unmarshal([]byte(t.CipherSuites), &cipherSuites)
	}
	if t.SupportedPQCs != "" {
		_ = json.Unmarshal([]byte(t.SupportedPQCs), &supportedPQCs)
	}
	if t.Recommendations != "" {
		_ = json.Unmarshal([]byte(t.Recommendations), &recommendations)
	}

	var nistLevels map[string]int
	if t.NISTLevels != "" {
		_ = json.Unmarshal([]byte(t.NISTLevels), &nistLevels)
	}

	return &TLSScanResult{
		URL:             t.URL,
		Host:            t.Host,
		Port:            t.Port,
		Certificate:     certInfo,
		CipherSuites:    cipherSuites,
		ProtocolVersion: t.ProtocolVersion,
		NISTLevel:       t.NISTLevel,
		RiskScore:       t.RiskScore,
		PQCRisk:         t.PQCRisk,
		SupportedPQCs:   supportedPQCs,
		Recommendations: recommendations,
		ScannedAt:       t.CreatedAt,
		KexAlgorithm:    t.KexAlgorithm,
		KexPQCReady:     t.KexPQCReady,
		PQCMode:         t.PQCMode,
		PFS:             t.PFS,
		ALPN:            t.ALPN,
		OCSPStapled:     t.OCSPStapled,
		Curve:           t.Curve,
		NISTLevels:      nistLevels,
		Default:         t.Default,
	}
}

// FromTLSScanResult creates an entity from a domain TLSScanResult DTO
// userID can be nil for default endpoints (isDefault=true)
func FromTLSScanResult(userID *uuid.UUID, result *TLSScanResult, isDefault bool) *TLSScanResultEntity {
	certJSON, _ := json.Marshal(result.Certificate)
	cipherSuitesJSON, _ := json.Marshal(result.CipherSuites)
	supportedPQCsJSON, _ := json.Marshal(result.SupportedPQCs)
	recommendationsJSON, _ := json.Marshal(result.Recommendations)
	nistLevelsJSON, _ := json.Marshal(result.NISTLevels)

	return &TLSScanResultEntity{
		UserID:          userID, // nil for default endpoints
		URL:             result.URL,
		Host:            result.Host,
		Port:            result.Port,
		ProtocolVersion: result.ProtocolVersion,
		NISTLevel:       result.NISTLevel,
		RiskScore:       result.RiskScore,
		PQCRisk:         result.PQCRisk,
		Certificate:     string(certJSON),
		CipherSuites:    string(cipherSuitesJSON),
		SupportedPQCs:   string(supportedPQCsJSON),
		Recommendations: string(recommendationsJSON),
		KexAlgorithm:    result.KexAlgorithm,
		KexPQCReady:     result.KexPQCReady,
		PQCMode:         result.PQCMode,
		PFS:             result.PFS,
		ALPN:            result.ALPN,
		OCSPStapled:     result.OCSPStapled,
		Curve:           result.Curve,
		NISTLevels:      string(nistLevelsJSON),
		Default:         isDefault,
	}
}
