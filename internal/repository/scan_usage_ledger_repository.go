package repository

import (
	"errors"
	"time"

	"cafe-persistence/internal/domain"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ScanUsageLedgerRepository implements success-only plan quota ledger queries (IMM-6b P1).
type ScanUsageLedgerRepository interface {
	RecordSuccessUsage(userID, scanID uuid.UUID, kind domain.ScanUsageKind) error
	CountSuccessUsage(userID uuid.UUID, kind domain.ScanUsageKind) (int64, error)
	CountInFlightScans(userID uuid.UUID, kind domain.ScanUsageKind) (int64, error)
	CountVisibleSuccessScans(userID uuid.UUID, kind domain.ScanUsageKind) (int64, error)
	// TryAcquireSuccessSlot returns true when ledger success count is strictly below limit.
	// limit <= 0 means unlimited (always true). Uses a per-user/kind lock on Postgres.
	TryAcquireSuccessSlot(userID uuid.UUID, kind domain.ScanUsageKind, limit int) (bool, error)

	// InTx variants for atomic completion handling (IMM-6b-4).
	RecordSuccessUsageInTx(tx *gorm.DB, userID, scanID uuid.UUID, kind domain.ScanUsageKind) error
	TryAcquireSuccessSlotInTx(tx *gorm.DB, userID uuid.UUID, kind domain.ScanUsageKind, limit int) (bool, error)
	// RecordSuccessUsageIfUnderLimitInTx atomically inserts when ledger count < limit (portable).
	RecordSuccessUsageIfUnderLimitInTx(tx *gorm.DB, userID, scanID uuid.UUID, kind domain.ScanUsageKind, limit int) (bool, error)
}

type scanUsageLedgerRepository struct {
	db *gorm.DB
}

// NewScanUsageLedgerRepository creates a ledger repository backed by Postgres (or test DB).
func NewScanUsageLedgerRepository(db *gorm.DB) ScanUsageLedgerRepository {
	return &scanUsageLedgerRepository{db: db}
}

var scanUsageInFlightStatuses = []string{scan.StatePENDING, scan.StateRUNNING, ""}

func (r *scanUsageLedgerRepository) RecordSuccessUsage(userID, scanID uuid.UUID, kind domain.ScanUsageKind) error {
	return r.RecordSuccessUsageInTx(r.db, userID, scanID, kind)
}

func (r *scanUsageLedgerRepository) RecordSuccessUsageInTx(tx *gorm.DB, userID, scanID uuid.UUID, kind domain.ScanUsageKind) error {
	if err := validateScanUsageKind(kind); err != nil {
		return err
	}
	event := &domain.ScanUsageEventEntity{
		UserID:     userID,
		ScanID:     scanID,
		ScanKind:   kind,
		ConsumedAt: time.Now().UTC(),
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "scan_id"}},
		DoNothing: true,
	}).Create(event).Error
}

func (r *scanUsageLedgerRepository) CountSuccessUsage(userID uuid.UUID, kind domain.ScanUsageKind) (int64, error) {
	if err := validateScanUsageKind(kind); err != nil {
		return 0, err
	}
	var count int64
	err := r.db.Model(&domain.ScanUsageEventEntity{}).
		Where("user_id = ? AND scan_kind = ?", userID, kind).
		Count(&count).Error
	return count, err
}

func (r *scanUsageLedgerRepository) CountInFlightScans(userID uuid.UUID, kind domain.ScanUsageKind) (int64, error) {
	if err := validateScanUsageKind(kind); err != nil {
		return 0, err
	}
	switch kind {
	case domain.ScanUsageKindWallet:
		var count int64
		err := r.db.Model(&domain.ScanResultEntity{}).
			Where("user_id = ? AND status IN ?", userID, scanUsageInFlightStatuses).
			Count(&count).Error
		return count, err
	case domain.ScanUsageKindEndpoint:
		var count int64
		err := r.db.Model(&domain.TLSScanResultEntity{}).
			Where("user_id = ? AND \"default\" = ? AND status IN ?", userID, false, scanUsageInFlightStatuses).
			Count(&count).Error
		return count, err
	default:
		return 0, errInvalidScanUsageKind
	}
}

func (r *scanUsageLedgerRepository) CountVisibleSuccessScans(userID uuid.UUID, kind domain.ScanUsageKind) (int64, error) {
	if err := validateScanUsageKind(kind); err != nil {
		return 0, err
	}
	switch kind {
	case domain.ScanUsageKindWallet:
		var count int64
		err := r.db.Model(&domain.ScanResultEntity{}).
			Where("user_id = ? AND status = ?", userID, scan.StateSUCCESS).
			Count(&count).Error
		return count, err
	case domain.ScanUsageKindEndpoint:
		var count int64
		err := r.db.Model(&domain.TLSScanResultEntity{}).
			Where("user_id = ? AND \"default\" = ? AND status = ?", userID, false, scan.StateSUCCESS).
			Count(&count).Error
		return count, err
	default:
		return 0, errInvalidScanUsageKind
	}
}

func (r *scanUsageLedgerRepository) TryAcquireSuccessSlot(userID uuid.UUID, kind domain.ScanUsageKind, limit int) (bool, error) {
	if limit <= 0 {
		return true, nil
	}
	var acquired bool
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var err error
		acquired, err = r.TryAcquireSuccessSlotInTx(tx, userID, kind, limit)
		return err
	})
	return acquired, err
}

func (r *scanUsageLedgerRepository) TryAcquireSuccessSlotInTx(tx *gorm.DB, userID uuid.UUID, kind domain.ScanUsageKind, limit int) (bool, error) {
	if limit <= 0 {
		return true, nil
	}
	if err := validateScanUsageKind(kind); err != nil {
		return false, err
	}
	if err := lockUserKindQuota(tx, userID, kind); err != nil {
		return false, err
	}
	var count int64
	if err := tx.Model(&domain.ScanUsageEventEntity{}).
		Where("user_id = ? AND scan_kind = ?", userID, kind).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count < int64(limit), nil
}

func (r *scanUsageLedgerRepository) RecordSuccessUsageIfUnderLimitInTx(
	tx *gorm.DB,
	userID, scanID uuid.UUID,
	kind domain.ScanUsageKind,
	limit int,
) (bool, error) {
	if limit <= 0 {
		if err := r.RecordSuccessUsageInTx(tx, userID, scanID, kind); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := validateScanUsageKind(kind); err != nil {
		return false, err
	}
	now := time.Now().UTC()
	eventID := uuid.New()
	res := tx.Exec(`
INSERT INTO scan_usage_events (id, user_id, scan_id, scan_kind, consumed_at)
SELECT ?, ?, ?, ?, ?
WHERE (SELECT COUNT(*) FROM scan_usage_events WHERE user_id = ? AND scan_kind = ?) < ?
ON CONFLICT (scan_id) DO NOTHING`,
		eventID, userID, scanID, kind, now,
		userID, kind, limit,
	)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

var errInvalidScanUsageKind = errors.New("invalid scan usage kind")

func validateScanUsageKind(kind domain.ScanUsageKind) error {
	switch kind {
	case domain.ScanUsageKindWallet, domain.ScanUsageKindEndpoint:
		return nil
	default:
		return errInvalidScanUsageKind
	}
}

func lockUserKindQuota(tx *gorm.DB, userID uuid.UUID, kind domain.ScanUsageKind) error {
	switch tx.Name() {
	case "postgres":
		k1, k2 := userKindAdvisoryLockKey(userID, kind)
		return tx.Exec("SELECT pg_advisory_xact_lock(?, ?)", k1, k2).Error
	default:
		return nil
	}
}

// userKindAdvisoryLockKey returns two int4 keys for pg_advisory_xact_lock(int, int).
func userKindAdvisoryLockKey(userID uuid.UUID, kind domain.ScanUsageKind) (int32, int32) {
	k1 := uuidBytesToLockKeyInt32(userID[0:4])
	k2 := uuidBytesToLockKeyInt32(userID[4:8])
	var mix int32
	switch kind {
	case domain.ScanUsageKindWallet:
		mix = 1
	case domain.ScanUsageKindEndpoint:
		mix = 2
	default:
		mix = 3
	}
	return k1, k2 ^ mix
}

// uuidBytesToLockKeyInt32 folds four UUID bytes into a stable int32 without uint32 casts (gosec G115).
func uuidBytesToLockKeyInt32(b []byte) int32 {
	return int32(b[0])<<24 | int32(b[1])<<16 | int32(b[2])<<8 | int32(b[3])
}
