package repository

import (
	"testing"

	"cafe-persistence/internal/domain"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupScanResultTestDB(t *testing.T) (*gorm.DB, ScanResultRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.New().String()+"?mode=memory&cache=shared&_txlock=immediate"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&domain.ScanResultEntity{},
		&domain.ScanRelatedAddressEntity{},
		&domain.ScanEnrichmentEntity{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	return db, NewScanResultRepository(db)
}

// seedScanWithChildren inserts one scan, two related rows and one enrichment.
func seedScanWithChildren(t *testing.T, db *gorm.DB, userID uuid.UUID) uuid.UUID {
	t.Helper()
	scanID := uuid.New()
	if err := db.Create(&domain.ScanResultEntity{ID: scanID, UserID: userID, Address: "0x" + scanID.String(), Status: "SUCCESS"}).Error; err != nil {
		t.Fatalf("seed scan: %v", err)
	}
	for _, role := range []string{"delegation_target", "deployer"} {
		if err := db.Create(&domain.ScanRelatedAddressEntity{ScanID: scanID, ChainID: 1, Role: role, Address: "0xabc"}).Error; err != nil {
			t.Fatalf("seed related: %v", err)
		}
	}
	if err := db.Create(&domain.ScanEnrichmentEntity{
		EnrichmentID: uuid.New(), ScanID: scanID, Family: "history", UserID: userID,
		Status: "DONE", Chains: "[]", TransitionID: "t1",
	}).Error; err != nil {
		t.Fatalf("seed enrichment: %v", err)
	}
	return scanID
}

type scanRowCounts struct{ live, all, deleted int64 }

// countScanRows counts the rows of one scan through the default scope, through Unscoped, and with deleted_at set.
// The enrichment counts stay zero when the table is absent.
func countScanRows(t *testing.T, db *gorm.DB, scanID uuid.UUID) (scan, related, enrich scanRowCounts) {
	t.Helper()
	count := func(model any, col string) scanRowCounts {
		var c scanRowCounts
		if err := db.Model(model).Where(col+" = ?", scanID).Count(&c.live).Error; err != nil {
			t.Fatalf("count live: %v", err)
		}
		if err := db.Unscoped().Model(model).Where(col+" = ?", scanID).Count(&c.all).Error; err != nil {
			t.Fatalf("count all: %v", err)
		}
		if err := db.Unscoped().Model(model).Where(col+" = ? AND deleted_at IS NOT NULL", scanID).Count(&c.deleted).Error; err != nil {
			t.Fatalf("count deleted: %v", err)
		}
		return c
	}
	scan = count(&domain.ScanResultEntity{}, "id")
	related = count(&domain.ScanRelatedAddressEntity{}, "scan_id")
	if db.Migrator().HasTable(&domain.ScanEnrichmentEntity{}) {
		enrich = count(&domain.ScanEnrichmentEntity{}, "scan_id")
	}
	return scan, related, enrich
}

// Design §5.16 (OC-P73): a deleted scan is never read again.
func TestDeleteOwnedWalletScan_SoftDeletesRelatedAndEnrichments(t *testing.T) {
	db, repo := setupScanResultTestDB(t)
	userID := uuid.New()
	first := seedScanWithChildren(t, db, userID)
	second := seedScanWithChildren(t, db, userID)

	deleted, err := repo.DeleteOwnedWalletScan(userID, first)
	if err != nil || !deleted {
		t.Fatalf("delete first: deleted=%v err=%v", deleted, err)
	}

	scan, related, enrich := countScanRows(t, db, first)
	if scan != (scanRowCounts{live: 0, all: 1, deleted: 1}) {
		t.Errorf("first scan counts = %+v", scan)
	}
	if related != (scanRowCounts{live: 0, all: 2, deleted: 2}) {
		t.Errorf("first related counts = %+v", related)
	}
	if enrich != (scanRowCounts{live: 0, all: 1, deleted: 1}) {
		t.Errorf("first enrichment counts = %+v", enrich)
	}

	scan, related, enrich = countScanRows(t, db, second)
	if scan != (scanRowCounts{live: 1, all: 1}) || related != (scanRowCounts{live: 2, all: 2}) || enrich != (scanRowCounts{live: 1, all: 1}) {
		t.Errorf("second scan touched: %+v %+v %+v", scan, related, enrich)
	}

	// Second delete of the same scan: false, and deleted_at values do not change.
	var before domain.ScanRelatedAddressEntity
	if err := db.Unscoped().Where("scan_id = ?", first).First(&before).Error; err != nil {
		t.Fatalf("read related: %v", err)
	}
	deleted, err = repo.DeleteOwnedWalletScan(userID, first)
	if err != nil || deleted {
		t.Fatalf("second delete: deleted=%v err=%v", deleted, err)
	}
	var after domain.ScanRelatedAddressEntity
	if err := db.Unscoped().Where("scan_id = ? AND role = ?", first, before.Role).First(&after).Error; err != nil {
		t.Fatalf("re-read related: %v", err)
	}
	if !before.DeletedAt.Time.Equal(after.DeletedAt.Time) {
		t.Errorf("deleted_at changed on second delete: %v -> %v", before.DeletedAt.Time, after.DeletedAt.Time)
	}
}

// Design §5.16 (OC-P73): a foreign or unknown scan writes nothing.
func TestDeleteOwnedWalletScan_ForeignOrMissingDeletesNothing(t *testing.T) {
	db, repo := setupScanResultTestDB(t)
	owner := uuid.New()
	scanID := seedScanWithChildren(t, db, owner)

	for name, tc := range map[string]struct{ user, scan uuid.UUID }{
		"foreign user": {uuid.New(), scanID},
		"unknown scan": {owner, uuid.New()},
	} {
		deleted, err := repo.DeleteOwnedWalletScan(tc.user, tc.scan)
		if err != nil || deleted {
			t.Fatalf("%s: deleted=%v err=%v", name, deleted, err)
		}
	}

	scan, related, enrich := countScanRows(t, db, scanID)
	if scan.deleted != 0 || related.deleted != 0 || enrich.deleted != 0 {
		t.Errorf("rows soft-deleted: %+v %+v %+v", scan, related, enrich)
	}
	if scan.live != 1 || related.live != 2 || enrich.live != 1 {
		t.Errorf("live rows changed: %+v %+v %+v", scan, related, enrich)
	}
}

// Design §5.16 (OC-P73): any error rolls back all three writes.
func TestDeleteOwnedWalletScan_RollsBackOnError(t *testing.T) {
	db, repo := setupScanResultTestDB(t)
	userID := uuid.New()
	scanID := seedScanWithChildren(t, db, userID)

	if err := db.Migrator().DropTable(&domain.ScanEnrichmentEntity{}); err != nil {
		t.Fatalf("drop scan_enrichments: %v", err)
	}

	deleted, err := repo.DeleteOwnedWalletScan(userID, scanID)
	if err == nil {
		t.Fatalf("expected an error when scan_enrichments is missing")
	}
	if deleted {
		t.Errorf("deleted = true on error")
	}

	scan, related, _ := countScanRows(t, db, scanID)
	if scan.deleted != 0 || scan.live != 1 {
		t.Errorf("scan not rolled back: %+v", scan)
	}
	if related.deleted != 0 || related.live != 2 {
		t.Errorf("related rows not rolled back: %+v", related)
	}
}
