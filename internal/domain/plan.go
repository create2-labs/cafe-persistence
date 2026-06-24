package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PlanType represents the type of plan
type PlanType string

const (
	PlanTypeFree    PlanType = "FREE"
	PlanTypePremium PlanType = "PREMIUM"
)

// Plan represents a subscription plan
type Plan struct {
	ID                uuid.UUID      `gorm:"type:char(36);primary_key" json:"id"`
	Name              string         `gorm:"type:varchar(100);not null;uniqueIndex" json:"name"`
	Type              PlanType       `gorm:"type:varchar(50);not null" json:"type"`
	WalletScanLimit   int            `gorm:"not null;default:0" json:"wallet_scan_limit"`   // 0 = unlimited
	EndpointScanLimit int            `gorm:"not null;default:0" json:"endpoint_scan_limit"` // 0 = unlimited
	Price             float64        `gorm:"type:decimal(10,2);default:0" json:"price"`
	IsActive          bool           `gorm:"default:true" json:"is_active"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for GORM
func (Plan) TableName() string {
	return "plans"
}

// BeforeCreate hook to generate UUID before creating a plan
func (p *Plan) BeforeCreate(tx *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}

// IsUnlimited checks if the plan has unlimited scans for a given type
func (p *Plan) IsUnlimited(scanType string) bool {
	if scanType == "wallet" {
		return p.WalletScanLimit == 0
	}
	if scanType == "endpoint" {
		return p.EndpointScanLimit == 0
	}
	return false
}
