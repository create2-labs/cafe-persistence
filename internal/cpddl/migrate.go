package cpddl

import (
	"fmt"
	"strings"
	"sync"

	"cafe-persistence/internal/domain"

	"gorm.io/gorm"
)

// migrateMu serializes MigrateCPSchema within a process (integration packages share one Postgres).
var migrateMu sync.Mutex

// cpDropLegacyDDL removes draft-era tables (ADR_20260824 RD-P3 — voluntary RAZ / no dual-run).
var cpDropLegacyDDL = []string{
	`DROP TABLE IF EXISTS draft_persist_state`,
	`DROP TABLE IF EXISTS crypto_policy_drafts`,
}

// cpIndexDDL applies W1/W3 indexes for policy-only storage (ADR §8.4 / ADR_20260824).
var cpIndexDDL = []string{
	`DROP INDEX IF EXISTS idx_crypto_policies_user_wallet_persisted`,
	`CREATE INDEX IF NOT EXISTS idx_crypto_policies_user_scan_persisted ON crypto_policies (user_id, scan_id) WHERE status = 'persisted' AND deleted_at IS NULL`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uidx_crypto_policies_user_wallet_active ON crypto_policies (user_id, wallet_address) WHERE status = 'persisted' AND deleted_at IS NULL`,
	`CREATE INDEX IF NOT EXISTS idx_crypto_policies_user_id ON crypto_policies (user_id, id)`,
}

// RequiredIndexNames are CP indexes applied at persistence boot.
var RequiredIndexNames = []string{
	"idx_crypto_policies_user_scan_persisted",
	"uidx_crypto_policies_user_wallet_active",
	"idx_crypto_policies_user_id",
}

// CPTableNames are the CP-owned tables migrated by cafe-persistence (policy-only after RD-P3).
var CPTableNames = []string{
	"crypto_policies",
}

// MigrateCPSchema drops legacy draft tables, migrates crypto_policies, and applies index DDL.
//
// RAZ note (ADR_20260824): stacks with old CPM draft clients break voluntarily after this
// migration; when upgrading from the draft-era schema, existing `crypto_policies` rows are
// dropped once so NOT NULL audit columns can be applied (no production CP data).
func MigrateCPSchema(db *gorm.DB) error {
	migrateMu.Lock()
	defer migrateMu.Unlock()

	for _, q := range cpDropLegacyDDL {
		if err := db.Exec(q).Error; err != nil {
			return fmt.Errorf("cp drop legacy %q: %w", q, err)
		}
	}
	if err := resetCryptoPoliciesIfLegacy(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(&domain.CryptoPolicyEntity{}); err != nil {
		return fmt.Errorf("cp tables AutoMigrate: %w", err)
	}
	if err := dropLegacyDraftIDColumn(db); err != nil {
		return err
	}
	for _, q := range cpIndexDDL {
		if err := db.Exec(q).Error; err != nil {
			return fmt.Errorf("cp DDL %q: %w", q, err)
		}
	}
	return nil
}

func resetCryptoPoliciesIfLegacy(db *gorm.DB) error {
	exists, err := tableExists(db, "crypto_policies")
	if err != nil || !exists {
		return err
	}
	hasDraftID, err := columnExists(db, "crypto_policies", "draft_id")
	if err != nil {
		return err
	}
	hasPayloadSHA, err := columnExists(db, "crypto_policies", "payload_sha256")
	if err != nil {
		return err
	}
	if !hasDraftID && hasPayloadSHA {
		return nil
	}
	// One-shot RAZ from draft-era schema → recreate via AutoMigrate.
	if err := db.Exec(`DROP TABLE IF EXISTS crypto_policies CASCADE`).Error; err != nil {
		return fmt.Errorf("cp RAZ crypto_policies: %w", err)
	}
	return nil
}

func tableExists(db *gorm.DB, table string) (bool, error) {
	switch strings.ToLower(db.Name()) {
	case "postgres":
		var exists bool
		err := db.Raw(`
SELECT EXISTS (
  SELECT 1 FROM information_schema.tables
  WHERE table_schema = 'public' AND table_name = ?
)`, table).Scan(&exists).Error
		return exists, err
	default:
		return db.Migrator().HasTable(table), nil
	}
}

func columnExists(db *gorm.DB, table, column string) (bool, error) {
	switch strings.ToLower(db.Name()) {
	case "postgres":
		var exists bool
		err := db.Raw(`
SELECT EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema = 'public' AND table_name = ? AND column_name = ?
)`, table, column).Scan(&exists).Error
		return exists, err
	default:
		return db.Migrator().HasColumn(table, column), nil
	}
}

func dropLegacyDraftIDColumn(db *gorm.DB) error {
	has, err := columnExists(db, "crypto_policies", "draft_id")
	if err != nil || !has {
		return err
	}
	switch strings.ToLower(db.Name()) {
	case "postgres":
		if err := db.Exec(`ALTER TABLE crypto_policies DROP COLUMN IF EXISTS draft_id`).Error; err != nil {
			return fmt.Errorf("cp column cleanup draft_id: %w", err)
		}
	case "sqlite":
		if err := db.Exec(`ALTER TABLE crypto_policies DROP COLUMN draft_id`).Error; err != nil {
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "no such column") || strings.Contains(msg, "not found") {
				return nil
			}
			return fmt.Errorf("cp column cleanup draft_id: %w", err)
		}
	default:
		_ = db.Exec(`ALTER TABLE crypto_policies DROP COLUMN draft_id`)
	}
	return nil
}
