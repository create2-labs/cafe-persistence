package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ScanRelatedAddressEntity is an address related to a wallet scan, such as an EIP-7702 delegation target.
// The primary key is (scan_id, chain_id, role, address); there is no foreign key to scan_results.
type ScanRelatedAddressEntity struct {
	ScanID         uuid.UUID      `gorm:"type:char(36);primaryKey" json:"-"`
	ChainID        int64          `gorm:"type:bigint;primaryKey" json:"-"`
	Role           string         `gorm:"type:varchar(32);primaryKey" json:"-"`
	Address        string         `gorm:"type:text;primaryKey" json:"-"` // lowercased
	EnrichmentID   *uuid.UUID     `gorm:"type:char(36)" json:"-"`        // NULL for delegation_target, set for deployer
	CreationTxHash string         `gorm:"type:text;not null;default:''" json:"-"`
	CreatedAt      time.Time      `json:"-"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for GORM.
func (ScanRelatedAddressEntity) TableName() string {
	return "scan_related_addresses"
}
