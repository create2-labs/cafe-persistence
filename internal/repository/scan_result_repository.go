package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"cafe-persistence/internal/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ScanResultRepository defines wallet scan read/delete operations. Methods that take a userID are
// scoped to that owner; ListOpenEnrichments is cross-user.
type ScanResultRepository interface {
	FindOwnedWalletScanByID(userID, scanID uuid.UUID) (*domain.ScanResultEntity, error)
	DeleteOwnedWalletScan(userID, scanID uuid.UUID) (deleted bool, err error)
	ListOwnerWalletScansDiscoveryV1(userID uuid.UUID, address string, limit, offset int) ([]*domain.ScanResultEntity, int64, error)
	ListOwnerWalletScansByAddress(userID uuid.UUID, address string) ([]*domain.ScanResultEntity, error)
	ListRelatedAddresses(scanID uuid.UUID) ([]domain.ScanRelatedAddressEntity, error)
	// ListOpenEnrichments is cross-user: it has no user filter and returns the user_id of every row. It is
	// only for the internal open-enrichment route. It returns the live enrichments whose status is in statuses
	// and whose created_at is before the bound, ordered by created_at then enrichment_id, at most limit rows (design §5.11).
	ListOpenEnrichments(statuses []string, before time.Time, limit int) ([]domain.ScanEnrichmentEntity, error)
}

type scanResultRepository struct {
	db *gorm.DB
}

// NewScanResultRepository creates a wallet scan result repository.
func NewScanResultRepository(db *gorm.DB) ScanResultRepository {
	return &scanResultRepository{db: db}
}

func (r *scanResultRepository) FindOwnedWalletScanByID(userID, scanID uuid.UUID) (*domain.ScanResultEntity, error) {
	var ent domain.ScanResultEntity
	err := r.db.Where("id = ? AND user_id = ?", scanID, userID).First(&ent).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ent, nil
}

// DeleteOwnedWalletScan soft-deletes an owned scan with its related-address and enrichment rows
// in one transaction. The scan row is written first: this is the lock order the enrichment
// transition relies on. It returns false, and writes nothing else, when no owned live scan matches.
func (r *scanResultRepository) DeleteOwnedWalletScan(userID, scanID uuid.UUID) (bool, error) {
	var deleted bool
	err := r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND user_id = ?", scanID, userID).Delete(&domain.ScanResultEntity{})
		if res.Error != nil {
			return fmt.Errorf("delete scan: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return nil
		}
		if err := tx.Where("scan_id = ?", scanID).Delete(&domain.ScanRelatedAddressEntity{}).Error; err != nil {
			return fmt.Errorf("delete related addresses: %w", err)
		}
		if err := tx.Where("scan_id = ?", scanID).Delete(&domain.ScanEnrichmentEntity{}).Error; err != nil {
			return fmt.Errorf("delete enrichments: %w", err)
		}
		deleted = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}

func (r *scanResultRepository) ListOwnerWalletScansDiscoveryV1(userID uuid.UUID, address string, limit, offset int) ([]*domain.ScanResultEntity, int64, error) {
	q := r.db.Model(&domain.ScanResultEntity{}).Where("user_id = ?", userID)
	if strings.TrimSpace(address) != "" {
		q = q.Where("LOWER(address) = ?", strings.ToLower(strings.TrimSpace(address)))
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []*domain.ScanResultEntity
	tx := r.db.Where("user_id = ?", userID)
	if strings.TrimSpace(address) != "" {
		tx = tx.Where("LOWER(address) = ?", strings.ToLower(strings.TrimSpace(address)))
	}
	err := tx.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&out).Error
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (r *scanResultRepository) ListOwnerWalletScansByAddress(userID uuid.UUID, address string) ([]*domain.ScanResultEntity, error) {
	tx := r.db.Where("user_id = ? AND LOWER(address) = ?", userID, strings.ToLower(strings.TrimSpace(address)))
	var out []*domain.ScanResultEntity
	if err := tx.Order("created_at DESC, id DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ListRelatedAddresses returns the related addresses of a wallet scan, ordered by chain_id, role, then address.
// GORM's default scope excludes soft-deleted rows.
func (r *scanResultRepository) ListRelatedAddresses(scanID uuid.UUID) ([]domain.ScanRelatedAddressEntity, error) {
	var out []domain.ScanRelatedAddressEntity
	err := r.db.Where("scan_id = ?", scanID).Order("chain_id ASC, role ASC, address ASC").Find(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListOpenEnrichments returns the live enrichments in the given statuses created strictly before the bound.
// It is cross-user: there is no user filter and every row carries its user_id. Only the internal
// open-enrichment route may call it.
// GORM's default scope excludes soft-deleted rows; no join on scan_results is needed because a scan deletion
// soft-deletes its enrichments in the same transaction (design §5.11).
func (r *scanResultRepository) ListOpenEnrichments(statuses []string, before time.Time, limit int) ([]domain.ScanEnrichmentEntity, error) {
	var out []domain.ScanEnrichmentEntity
	err := r.db.Where("status IN ? AND created_at < ?", statuses, before).
		Order("created_at ASC, enrichment_id ASC").
		Limit(limit).
		Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("list open enrichments: %w", err)
	}
	return out, nil
}
