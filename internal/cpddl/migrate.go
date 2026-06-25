package cpddl

import (
	"fmt"

	"cafe-persistence/internal/domain"

	"gorm.io/gorm"
)

// cpIndexDDL applies partial indexes for owner lookups and W1/W3 guards (ADR §8.4).
var cpIndexDDL = []string{
	`CREATE INDEX IF NOT EXISTS idx_crypto_policy_drafts_user_scan_active ON crypto_policy_drafts (user_id, scan_id) WHERE deleted_at IS NULL`,
	`CREATE INDEX IF NOT EXISTS idx_crypto_policy_drafts_user_id ON crypto_policy_drafts (user_id, id)`,
	`CREATE INDEX IF NOT EXISTS idx_crypto_policies_user_scan_persisted ON crypto_policies (user_id, scan_id) WHERE status = 'persisted' AND deleted_at IS NULL`,
	`CREATE INDEX IF NOT EXISTS idx_crypto_policies_user_wallet_persisted ON crypto_policies (user_id, wallet_address) WHERE status = 'persisted' AND deleted_at IS NULL`,
	`CREATE INDEX IF NOT EXISTS idx_crypto_policies_user_id ON crypto_policies (user_id, id)`,
}

// RequiredIndexNames are CP indexes applied at persistence boot (ADR §8.4).
var RequiredIndexNames = []string{
	"idx_crypto_policy_drafts_user_scan_active",
	"idx_crypto_policy_drafts_user_id",
	"idx_crypto_policies_user_scan_persisted",
	"idx_crypto_policies_user_wallet_persisted",
	"idx_crypto_policies_user_id",
}

// CPTableNames are the CP-owned tables migrated by cafe-persistence (PERS-D4).
var CPTableNames = []string{
	"crypto_policy_drafts",
	"crypto_policies",
	"draft_persist_state",
}

// MigrateCPSchema runs AutoMigrate on CP tables and applies index DDL.
func MigrateCPSchema(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&domain.CryptoPolicyDraftEntity{},
		&domain.CryptoPolicyEntity{},
		&domain.DraftPersistStateEntity{},
	); err != nil {
		return fmt.Errorf("cp tables AutoMigrate: %w", err)
	}
	for _, q := range cpIndexDDL {
		if err := db.Exec(q).Error; err != nil {
			return fmt.Errorf("cp DDL %q: %w", q, err)
		}
	}
	return nil
}
