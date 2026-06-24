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
}

// MigrateScanSchema runs AutoMigrate on scan tables and applies IMM index DDL.
func MigrateScanSchema(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&domain.TLSScanResultEntity{},
		&domain.ScanResultEntity{},
		&domain.ScanUsageEventEntity{},
	); err != nil {
		return fmt.Errorf("scan tables AutoMigrate: %w", err)
	}
	for _, q := range scanIndexDDL {
		if err := db.Exec(q).Error; err != nil {
			return fmt.Errorf("scan DDL %q: %w", q, err)
		}
	}
	return nil
}
