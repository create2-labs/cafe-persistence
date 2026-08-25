package domain

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	CryptoPolicyStatusPersisted = "persisted"
)

// JSONMap is a JSONB object column (crypto policy payloads).
type JSONMap map[string]any

func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}
	return b, err
}

func (j *JSONMap) Scan(value any) error {
	if value == nil {
		*j = JSONMap{}
		return nil
	}
	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("JSONMap: unsupported type %T", value)
	}
	if len(raw) == 0 {
		*j = JSONMap{}
		return nil
	}
	out := make(JSONMap)
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*j = out
	return nil
}

// CryptoPolicyEntity is a persisted crypto policy row (ADR §8.4 / ADR_20260824 remove drafts).
type CryptoPolicyEntity struct {
	ID                      uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	UserID                  string         `gorm:"type:text;not null;index" json:"user_id"`
	TenantID                string         `gorm:"type:text" json:"tenant_id,omitempty"`
	ScanID                  uuid.UUID      `gorm:"type:uuid;not null" json:"scan_id"`
	WalletAddress           string         `gorm:"type:text;not null" json:"wallet_address"`
	ChainID                 int64          `gorm:"not null" json:"chain_id"`
	Payload                 JSONMap        `gorm:"type:jsonb;not null" json:"payload"`
	PayloadSHA256           string         `gorm:"type:text;not null" json:"payload_sha256"`
	SignedMessageHash       string         `gorm:"type:text" json:"signed_message_hash,omitempty"`
	OwnershipStatus         string         `gorm:"type:text" json:"ownership_status,omitempty"`
	WalletControlMethod     string         `gorm:"type:text" json:"wallet_control_method,omitempty"`
	WalletControlVerifiedAt *time.Time     `json:"wallet_control_verified_at,omitempty"`
	ChallengeIssuedAt       *time.Time     `json:"challenge_issued_at,omitempty"`
	ChallengeExpiresAt      *time.Time     `json:"challenge_expires_at,omitempty"`
	Status                  string         `gorm:"type:text;not null;index" json:"status"`
	PersistedAt             time.Time      `gorm:"not null" json:"persisted_at"`
	CreatedAt               time.Time      `json:"created_at"`
	UpdatedAt               time.Time      `json:"updated_at"`
	DeletedAt               gorm.DeletedAt `gorm:"index" json:"-"`
}

func (CryptoPolicyEntity) TableName() string { return "crypto_policies" }

// CloneJSONMap returns a shallow copy of a payload map.
func CloneJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// NormalizePolicyPayload builds the durable payload projection (no raw signature material).
// Client-supplied payload_sha256 is stripped; the column is the server authority.
func NormalizePolicyPayload(payload map[string]any, scanID, wallet string, chainID int64, verifiedAt time.Time) map[string]any {
	out := CloneJSONMap(payload)
	if out == nil {
		out = make(map[string]any)
	}
	out["scan_id"] = scanID
	out["wallet_address"] = wallet
	out["chain_id"] = chainID
	out["ownership_status"] = "verified"
	out["wallet_control_method"] = "eoa_signature"
	out["wallet_control_verified_at"] = verifiedAt.UTC().Format(time.RFC3339)
	out["persisted_at"] = verifiedAt.UTC().Format(time.RFC3339)
	delete(out, "signed_message")
	delete(out, "signature")
	delete(out, "payload_sha256")
	delete(out, "draft_id")
	return out
}

// ValidateOwnerScope ensures user_id is present for owner-scoped CP operations.
func ValidateOwnerScope(userID, tenantID string) error {
	if userID == "" {
		return errors.New("user_id is required")
	}
	_ = tenantID
	return nil
}
