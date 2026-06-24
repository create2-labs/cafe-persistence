package repository

import (
	"cafe-persistence/internal/domain"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PlanRepository defines the interface for plan data access operations
type PlanRepository interface {
	Create(plan *domain.Plan) error
	FindByID(id uuid.UUID) (*domain.Plan, error)
	FindByType(planType domain.PlanType) (*domain.Plan, error)
	FindAll() ([]*domain.Plan, error)
	FindActive() ([]*domain.Plan, error)
}

// planRepository implements PlanRepository interface
type planRepository struct {
	db *gorm.DB
}

// NewPlanRepository creates a new plan repository
func NewPlanRepository(db *gorm.DB) PlanRepository {
	return &planRepository{
		db: db,
	}
}

// Create creates a new plan in the database
func (r *planRepository) Create(plan *domain.Plan) error {
	if err := r.db.Create(plan).Error; err != nil {
		return err
	}
	return nil
}

// FindByID finds a plan by ID
func (r *planRepository) FindByID(id uuid.UUID) (*domain.Plan, error) {
	var plan domain.Plan
	result := r.db.Where("id = ?", id).First(&plan)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, result.Error
	}
	return &plan, nil
}

// FindByType finds a plan by type
func (r *planRepository) FindByType(planType domain.PlanType) (*domain.Plan, error) {
	var plan domain.Plan
	result := r.db.Where("type = ? AND is_active = ?", planType, true).First(&plan)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, result.Error
	}
	return &plan, nil
}

// FindAll finds all plans
func (r *planRepository) FindAll() ([]*domain.Plan, error) {
	var plans []*domain.Plan
	result := r.db.Order("price ASC").Find(&plans)
	if result.Error != nil {
		return nil, result.Error
	}
	return plans, nil
}

// FindActive finds all active plans
func (r *planRepository) FindActive() ([]*domain.Plan, error) {
	var plans []*domain.Plan
	result := r.db.Where("is_active = ?", true).Order("price ASC").Find(&plans)
	if result.Error != nil {
		return nil, result.Error
	}
	return plans, nil
}
