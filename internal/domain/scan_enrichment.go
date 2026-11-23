package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// errInvalidEnrichmentChains is the only error of DecodeChains. It does not wrap the encoding/json error.
var errInvalidEnrichmentChains = errors.New("decode scan_enrichments.chains: invalid JSON array")

// Enrichment families, statuses, and chain outcomes (design §5.7).
const (
	EnrichmentFamilyHistory  = "history"
	EnrichmentFamilyDeployer = "deployer"

	EnrichmentStatusPending = "PENDING"
	EnrichmentStatusRunning = "RUNNING"
	EnrichmentStatusDone    = "DONE"
	EnrichmentStatusError   = "ERROR"

	// EnrichmentOutcomePending is a stored value only, not a contract value.
	EnrichmentOutcomePending     = "pending"
	EnrichmentOutcomeObserved    = "observed"
	EnrichmentOutcomeNotObserved = "not_observed"
	EnrichmentOutcomeError       = "error"
)

// ScanEnrichmentEntity is one enrichment of a wallet scan (design §5.7).
// There is no foreign key to scan_results; the (scan_id, family) pair is unique.
type ScanEnrichmentEntity struct {
	EnrichmentID uuid.UUID      `gorm:"type:char(36);primaryKey" json:"-"`
	ScanID       uuid.UUID      `gorm:"type:char(36);not null;uniqueIndex:idx_scan_enrichments_scan_family,priority:1" json:"-"`
	Family       string         `gorm:"type:varchar(20);not null;uniqueIndex:idx_scan_enrichments_scan_family,priority:2" json:"-"`
	UserID       uuid.UUID      `gorm:"type:char(36);not null" json:"-"`
	Status       string         `gorm:"type:varchar(20);not null;index:idx_scan_enrichments_status_created_at,priority:1" json:"-"`
	Chains       string         `gorm:"type:text;not null" json:"-"` // JSON array of EnrichmentChain
	Error        string         `gorm:"type:text;not null;default:''" json:"-"`
	TransitionID string         `gorm:"type:text;not null" json:"-"` // the last applied transition
	CreatedAt    time.Time      `gorm:"index:idx_scan_enrichments_status_created_at,priority:2" json:"-"`
	UpdatedAt    time.Time      `json:"-"`
	DeletedAt    gorm.DeletedAt `gorm:"index:idx_scan_enrichments_deleted_at" json:"-"`
}

// TableName specifies the table name for GORM.
func (ScanEnrichmentEntity) TableName() string {
	return "scan_enrichments"
}

// EnrichmentChain is the stored state of one chain of an enrichment.
type EnrichmentChain struct {
	ChainID         int64  `json:"chain_id"`
	Outcome         string `json:"outcome"` // pending | observed | not_observed | error
	Code            string `json:"code,omitempty"`
	TransactionHash string `json:"transaction_hash,omitempty"`
	Deployer        string `json:"deployer,omitempty"`
	CreationTxHash  string `json:"creation_tx_hash,omitempty"`
}

// DecodeChains decodes the chains column (design §5.7, Decoding). A JSON array gives its elements, never a nil
// list. An empty column, JSON null, any other JSON value, and unreadable JSON are errors. Only the JSON form is
// checked: the non-empty rule and the outcome values belong to the writer.
func (e ScanEnrichmentEntity) DecodeChains() ([]EnrichmentChain, error) {
	var chains []EnrichmentChain
	if err := json.Unmarshal([]byte(e.Chains), &chains); err != nil || chains == nil {
		return nil, errInvalidEnrichmentChains
	}
	return chains, nil
}
