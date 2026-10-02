package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ScanUsageKind identifies which plan quota bucket a ledger event consumes.
// Values match plan limit keys (wallet / endpoint), not NATS scan KindTLS ("tls").
type ScanUsageKind string

const (
	ScanUsageKindWallet   ScanUsageKind = "wallet"
	ScanUsageKindEndpoint ScanUsageKind = "endpoint"
)

// ScanUsageEventEntity is the plan-quota ledger row for one scan_id (IMM-6b P1).
// A row reserves the credit before the scan runs and counts as a success.
// Completion keeps it when the scan has a result, including wallet type unknown.
// A failure without a result deletes it. The scan_id stays unique.
type ScanUsageEventEntity struct {
	ID         uuid.UUID     `gorm:"type:char(36);primary_key" json:"id"`
	UserID     uuid.UUID     `gorm:"type:char(36);not null" json:"user_id"`
	ScanID     uuid.UUID     `gorm:"type:char(36);not null;uniqueIndex" json:"scan_id"`
	ScanKind   ScanUsageKind `gorm:"type:varchar(20);not null" json:"scan_kind"`
	ConsumedAt time.Time     `gorm:"not null" json:"consumed_at"`
}

func (ScanUsageEventEntity) TableName() string {
	return "scan_usage_events"
}

func (e *ScanUsageEventEntity) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}
