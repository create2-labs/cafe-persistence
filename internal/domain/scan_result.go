package domain

import (
	"encoding/json"
	"fmt"
	"time"

	addressv1 "github.com/create2-labs/cafe-contracts/observation/address/v1"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ScanResultEntity represents a scan result stored in the database
type ScanResultEntity struct {
	ID                uuid.UUID         `gorm:"type:char(36);primary_key" json:"id"`
	UserID            uuid.UUID         `gorm:"type:char(36);not null;index" json:"user_id"`
	Address           string            `gorm:"not null;index" json:"address"`
	Type              AccountType       `gorm:"type:varchar(20);not null" json:"type"`
	Algorithm         Algorithm         `gorm:"type:varchar(50);not null" json:"algorithm"`
	NISTLevel         NISTLevel         `gorm:"not null" json:"nist_level"`
	KeyExposed        bool              `gorm:"not null" json:"key_exposed"`
	PublicKey         string            `gorm:"type:text" json:"public_key,omitempty"`              // Recovered public key
	TransactionHash   string            `gorm:"type:varchar(66)" json:"transaction_hash,omitempty"` // Hash of transaction that exposed the key
	ExposedNetwork    string            `gorm:"type:varchar(50)" json:"exposed_network,omitempty"`  // Network where key was exposed
	PublicKeyRecovery PublicKeyRecovery `gorm:"type:varchar(20);not null;default:''" json:"public_key_recovery"`
	IsEOA             bool              `gorm:"not null" json:"is_eoa"`
	IsERC4337         bool              `gorm:"not null" json:"is_erc4337"`
	RiskScore         float64           `gorm:"not null" json:"risk_score"`
	Networks          string            `gorm:"type:text" json:"-"`                   // JSON array stored as text
	Delegations       string            `gorm:"type:text" json:"-"`                   // JSON array stored as text
	Connections       string            `gorm:"type:text" json:"-"`                   // JSON array stored as text
	Status            string            `gorm:"type:varchar(20);index" json:"status"` // empty/PENDING until scan.started (RUNNING); then SUCCESS, FAILED, TIMEOUT, UNREACHABLE
	Error             string            `gorm:"type:text" json:"error,omitempty"`     // Error message when status is FAILED
	TransitionID      *string           `gorm:"type:text" json:"-"`                   // last applied wallet transition; NULL until written; absent on TLS
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	DeletedAt         gorm.DeletedAt    `gorm:"index" json:"-"`

	// Address qualification (observation/address/v1), written by the wallet transition on SUCCESS; '' until then.
	AccountType        string     `gorm:"type:varchar(20);not null;default:''" json:"-"` // '', eoa, contract, blank
	Chains             string     `gorm:"type:text;not null;default:''" json:"-"`        // JSON array of addressv1.Chain
	UnobservedChainIDs string     `gorm:"type:text;not null;default:''" json:"-"`        // JSON array of int64
	Anomalies          string     `gorm:"type:text;not null;default:''" json:"-"`        // JSON array of addressv1.Anomaly
	ObservedAt         *time.Time `json:"-"`                                             // result observed_at, UTC
}

// TableName specifies the table name for GORM
func (ScanResultEntity) TableName() string {
	return "scan_results"
}

// BeforeCreate hook to generate UUID before creating a scan result
func (s *ScanResultEntity) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}

// DecodeChains returns the stored chains; an empty list for an empty column and an error for any content that is not a JSON array.
func (s *ScanResultEntity) DecodeChains() ([]addressv1.Chain, error) {
	return decodeJSONList[addressv1.Chain]("chains", s.Chains)
}

// DecodeUnobservedChainIDs returns the stored unobserved chain ids; an empty list for an empty column and an error for any content that is not a JSON array.
func (s *ScanResultEntity) DecodeUnobservedChainIDs() ([]int64, error) {
	return decodeJSONList[int64]("unobserved_chain_ids", s.UnobservedChainIDs)
}

// DecodeAnomalies returns the stored anomalies; an empty list for an empty column and an error for any content that is not a JSON array.
func (s *ScanResultEntity) DecodeAnomalies() ([]addressv1.Anomaly, error) {
	return decodeJSONList[addressv1.Anomaly]("anomalies", s.Anomalies)
}

// decodeJSONList decodes a JSON array column. An empty column gives an empty list and an array gives its
// elements, never a nil list. Any other content, including JSON null, is an error.
func decodeJSONList[T any](column, raw string) ([]T, error) {
	if raw == "" {
		return []T{}, nil
	}
	var list []T
	if err := json.Unmarshal([]byte(raw), &list); err != nil || list == nil {
		return nil, fmt.Errorf("decode scan_results.%s: invalid JSON array", column)
	}
	return list, nil
}
