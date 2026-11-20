package scanddl

import (
	"fmt"

	"cafe-persistence/internal/domain"

	"gorm.io/gorm"
)

// scanIndexDDL matches cmd/persistence startup in cafe-discovery (IMM-2, IMM-6b-1).
var scanIndexDDL = []string{
	`DROP INDEX IF EXISTS idx_scan_results_user_address`,
	`DROP INDEX IF EXISTS idx_tls_scan_results_user_url`,
	`CREATE INDEX IF NOT EXISTS idx_scan_results_user_address_created_at ON scan_results (user_id, address, created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_tls_scan_results_user_url_created_at ON tls_scan_results (user_id, url, created_at DESC) NULLS NOT DISTINCT`,
	`CREATE INDEX IF NOT EXISTS idx_scan_usage_events_user_kind ON scan_usage_events (user_id, scan_kind)`,
	// IMM-D2: status must not default to RUNNING; OnStarted sets RUNNING on scan.started.
	`ALTER TABLE scan_results ALTER COLUMN status DROP DEFAULT`,
	`ALTER TABLE tls_scan_results ALTER COLUMN status DROP DEFAULT`,
}

// walletResultDDL removes activity dates and the replaced columns from
// scan_results and constrains public_key_recovery and account_type.
// Previous first_seen/last_seen values are not translated.
var walletResultDDL = []string{
	`ALTER TABLE scan_results DROP COLUMN IF EXISTS first_seen`,
	`ALTER TABLE scan_results DROP COLUMN IF EXISTS last_seen`,
	`ALTER TABLE scan_results DROP CONSTRAINT IF EXISTS chk_scan_results_public_key_recovery`,
	`ALTER TABLE scan_results ADD CONSTRAINT chk_scan_results_public_key_recovery CHECK (public_key_recovery IN ('', 'not_required', 'recovered', 'unresolved'))`,
	`ALTER TABLE scan_results DROP CONSTRAINT IF EXISTS chk_scan_results_account_type`,
	`ALTER TABLE scan_results ADD CONSTRAINT chk_scan_results_account_type CHECK (account_type IN ('', 'eoa', 'contract', 'blank'))`,
	// The replaced columns (design §5.6): the qualification columns carry their meaning.
	`ALTER TABLE scan_results DROP COLUMN IF EXISTS type`,
	`ALTER TABLE scan_results DROP COLUMN IF EXISTS is_eoa`,
	`ALTER TABLE scan_results DROP COLUMN IF EXISTS is_erc4337`,
	`ALTER TABLE scan_results DROP COLUMN IF EXISTS networks`,
	`ALTER TABLE scan_results DROP COLUMN IF EXISTS delegations`,
	`ALTER TABLE scan_results DROP COLUMN IF EXISTS connections`,
}

// relatedAddressDDL holds the constraints of scan_related_addresses (design §5.2).
var relatedAddressDDL = []string{
	`ALTER TABLE scan_related_addresses DROP CONSTRAINT IF EXISTS chk_scan_related_addresses_role`,
	`ALTER TABLE scan_related_addresses ADD CONSTRAINT chk_scan_related_addresses_role CHECK (role IN ('delegation_target', 'deployer'))`,
	`ALTER TABLE scan_related_addresses DROP CONSTRAINT IF EXISTS chk_scan_related_addresses_enrichment`,
	`ALTER TABLE scan_related_addresses ADD CONSTRAINT chk_scan_related_addresses_enrichment CHECK ((role = 'delegation_target' AND enrichment_id IS NULL) OR (role = 'deployer' AND enrichment_id IS NOT NULL))`,
}

// RequiredIndexNames are the IMM list/history indexes applied at persistence boot (ADR §14.5).
var RequiredIndexNames = []string{
	"idx_scan_results_user_address_created_at",
	"idx_tls_scan_results_user_url_created_at",
	"idx_scan_usage_events_user_kind",
}

// LegacyIndexNames must be absent after MigrateScanSchema (IMM-2 drop).
var LegacyIndexNames = []string{
	"idx_scan_results_user_address",
	"idx_tls_scan_results_user_url",
}

// ScanTableNames are the scan-owned tables migrated by cafe-persistence.
var ScanTableNames = []string{
	"scan_results",
	"tls_scan_results",
	"scan_usage_events",
	"scan_related_addresses",
}

// MigrateScanSchema runs AutoMigrate on scan tables and applies IMM index DDL.
// AutoMigrate adds nullable scan_results.transition_id from domain.ScanResultEntity.
// tls_scan_results does not receive that identity.
func MigrateScanSchema(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&domain.TLSScanResultEntity{},
		&domain.ScanResultEntity{},
		&domain.ScanUsageEventEntity{},
		&domain.ScanRelatedAddressEntity{},
	); err != nil {
		return fmt.Errorf("scan tables AutoMigrate: %w", err)
	}
	for _, q := range scanIndexDDL {
		if err := db.Exec(q).Error; err != nil {
			return fmt.Errorf("scan DDL %q: %w", q, err)
		}
	}
	for _, q := range walletResultDDL {
		if err := db.Exec(q).Error; err != nil {
			return fmt.Errorf("scan DDL %q: %w", q, err)
		}
	}
	for _, q := range relatedAddressDDL {
		if err := db.Exec(q).Error; err != nil {
			return fmt.Errorf("scan DDL %q: %w", q, err)
		}
	}
	return nil
}
