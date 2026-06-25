package repository

import (
	"errors"

	"cafe-persistence/internal/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TLSScanResultRepository defines owner TLS scan read/delete operations.
type TLSScanResultRepository interface {
	FindDefaultTLSScanByID(scanID uuid.UUID) (*domain.TLSScanResultEntity, error)
	FindOwnedUserTLSScanByID(userID, scanID uuid.UUID) (*domain.TLSScanResultEntity, error)
	DeleteOwnedUserTLSScan(userID, scanID uuid.UUID) (deleted bool, err error)
	ListOwnerUserTLSScansDiscoveryV1(userID uuid.UUID, limit, offset int) ([]*domain.TLSScanResultEntity, int64, error)
	FindAllDefault() ([]*domain.TLSScanResultEntity, error)
}

type tlsScanResultRepository struct {
	db *gorm.DB
}

// NewTLSScanResultRepository creates a TLS scan result repository.
func NewTLSScanResultRepository(db *gorm.DB) TLSScanResultRepository {
	return &tlsScanResultRepository{db: db}
}

func (r *tlsScanResultRepository) FindDefaultTLSScanByID(scanID uuid.UUID) (*domain.TLSScanResultEntity, error) {
	var ent domain.TLSScanResultEntity
	err := r.db.Where("id = ? AND \"default\" = ?", scanID, true).First(&ent).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ent, nil
}

func (r *tlsScanResultRepository) FindOwnedUserTLSScanByID(userID, scanID uuid.UUID) (*domain.TLSScanResultEntity, error) {
	var ent domain.TLSScanResultEntity
	err := r.db.Where("id = ? AND user_id = ? AND \"default\" = ?", scanID, userID, false).First(&ent).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ent, nil
}

func (r *tlsScanResultRepository) DeleteOwnedUserTLSScan(userID, scanID uuid.UUID) (bool, error) {
	res := r.db.Where("id = ? AND user_id = ? AND \"default\" = ?", scanID, userID, false).Delete(&domain.TLSScanResultEntity{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *tlsScanResultRepository) ListOwnerUserTLSScansDiscoveryV1(userID uuid.UUID, limit, offset int) ([]*domain.TLSScanResultEntity, int64, error) {
	q := r.db.Model(&domain.TLSScanResultEntity{}).Where("user_id = ? AND \"default\" = ?", userID, false)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []*domain.TLSScanResultEntity
	err := r.db.Where("user_id = ? AND \"default\" = ?", userID, false).
		Order("created_at DESC, id DESC").
		Limit(limit).Offset(offset).
		Find(&out).Error
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (r *tlsScanResultRepository) FindAllDefault() ([]*domain.TLSScanResultEntity, error) {
	var results []*domain.TLSScanResultEntity
	if err := r.db.Where("\"default\" = ?", true).Order("created_at DESC").Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}
