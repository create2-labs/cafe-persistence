package repository

import (
	"cafe-persistence/internal/domain"
	"errors"

	"gorm.io/gorm"
)

// UserRepository defines the interface for user data access operations
type UserRepository interface {
	Create(user *domain.User) error
	FindByEmail(email string) (*domain.User, error)
	FindByID(id string) (*domain.User, error)
	ExistsByEmail(email string) (bool, error)
}

// userRepository implements UserRepository interface
type userRepository struct {
	db *gorm.DB
}

// NewUserRepository creates a new user repository
func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{
		db: db,
	}
}

// Create creates a new user in the database
func (r *userRepository) Create(user *domain.User) error {
	if err := r.db.Create(user).Error; err != nil {
		return err
	}
	return nil
}

// FindByEmail finds a user by email address
func (r *userRepository) FindByEmail(email string) (*domain.User, error) {
	var user domain.User
	result := r.db.Where("email = ?", email).First(&user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, result.Error
	}
	return &user, nil
}

// FindByID finds a user by ID
func (r *userRepository) FindByID(id string) (*domain.User, error) {
	var user domain.User
	result := r.db.Where("id = ?", id).First(&user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, result.Error
	}
	return &user, nil
}

// ExistsByEmail checks if a user with the given email exists
func (r *userRepository) ExistsByEmail(email string) (bool, error) {
	var count int64
	result := r.db.Model(&domain.User{}).Where("email = ?", email).Count(&count)
	if result.Error != nil {
		return false, result.Error
	}
	return count > 0, nil
}
