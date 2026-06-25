package repository

import (
	"errors"
	"strings"

	"cafe-persistence/internal/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ScanResultRepository defines owner-scoped wallet scan read/delete operations.
type ScanResultRepository interface {
	FindOwnedWalletScanByID(userID, scanID uuid.UUID) (*domain.ScanResultEntity, error)
	DeleteOwnedWalletScan(userID, scanID uuid.UUID) (deleted bool, err error)
	ListOwnerWalletScansDiscoveryV1(userID uuid.UUID, address string, limit, offset int) ([]*domain.ScanResultEntity, int64, error)
	ListOwnerWalletScansByAddress(userID uuid.UUID, address string) ([]*domain.ScanResultEntity, error)
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

func (r *scanResultRepository) DeleteOwnedWalletScan(userID, scanID uuid.UUID) (bool, error) {
	res := r.db.Where("id = ? AND user_id = ?", scanID, userID).Delete(&domain.ScanResultEntity{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
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
