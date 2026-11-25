package repository

import (
	"testing"
	"time"

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

func seedOpenEnrichmentRow(t *testing.T, db *gorm.DB, id uuid.UUID, status string, createdAt time.Time) domain.ScanEnrichmentEntity {
	t.Helper()
	e := domain.ScanEnrichmentEntity{
		EnrichmentID: id, ScanID: uuid.New(), Family: "history", UserID: uuid.New(),
		Status: status, Chains: "[]", TransitionID: "t1", CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	if err := db.Create(&e).Error; err != nil {
		t.Fatalf("seed enrichment: %v", err)
	}
	return e
}

func enrichmentIDs(rows []domain.ScanEnrichmentEntity) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.EnrichmentID)
	}
	return ids
}

// CA4.4, ET7: open status, strictly before the bound, no join on the scan, soft-deleted excluded.
func TestListOpenEnrichments_FiltersStatusBoundAndSoftDelete(t *testing.T) {
	db, repo := setupScanResultTestDB(t)
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	idPending := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	idRunning := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	seedOpenEnrichmentRow(t, db, idPending, "PENDING", base.Add(-2*time.Hour))
	seedOpenEnrichmentRow(t, db, idRunning, "RUNNING", base.Add(-time.Hour))
	seedOpenEnrichmentRow(t, db, uuid.New(), "DONE", base.Add(-3*time.Hour))
	seedOpenEnrichmentRow(t, db, uuid.New(), "ERROR", base.Add(-3*time.Hour))
	seedOpenEnrichmentRow(t, db, uuid.New(), "PENDING", base) // equal to the bound
	gone := seedOpenEnrichmentRow(t, db, uuid.New(), "PENDING", base.Add(-4*time.Hour))
	if err := db.Delete(&gone).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	rows, err := repo.ListOpenEnrichments([]string{"PENDING", "RUNNING"}, base, 100)
	if err != nil {
		t.Fatalf("ListOpenEnrichments: %v", err)
	}
	got := enrichmentIDs(rows)
	if len(got) != 2 || got[0] != idPending || got[1] != idRunning {
		t.Fatalf("ids = %v, want [%s %s]", got, idPending, idRunning)
	}

	rows, err = repo.ListOpenEnrichments([]string{"RUNNING"}, base, 100)
	if err != nil || len(rows) != 1 || rows[0].EnrichmentID != idRunning {
		t.Fatalf("RUNNING only: rows %v, err %v", enrichmentIDs(rows), err)
	}
}

// ET7: order by created_at then enrichment_id; the limit truncates after the order.
func TestListOpenEnrichments_OrderAndLimit(t *testing.T) {
	db, repo := setupScanResultTestDB(t)
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	same := base.Add(-time.Hour)
	id1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	id3 := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	seedOpenEnrichmentRow(t, db, id3, "PENDING", same)
	seedOpenEnrichmentRow(t, db, id1, "PENDING", same)
	seedOpenEnrichmentRow(t, db, id2, "PENDING", same.Add(-time.Minute))

	rows, err := repo.ListOpenEnrichments([]string{"PENDING"}, base, 100)
	if err != nil {
		t.Fatalf("ListOpenEnrichments: %v", err)
	}
	got := enrichmentIDs(rows)
	if len(got) != 3 || got[0] != id2 || got[1] != id1 || got[2] != id3 {
		t.Fatalf("ids = %v, want [%s %s %s]", got, id2, id1, id3)
	}
	rows, err = repo.ListOpenEnrichments([]string{"PENDING"}, base, 2)
	if err != nil || len(rows) != 2 || rows[0].EnrichmentID != id2 || rows[1].EnrichmentID != id1 {
		t.Fatalf("limit 2: rows %v, err %v", enrichmentIDs(rows), err)
	}
}

// CA4.4: a store failure is returned, never an empty result.
func TestListOpenEnrichments_StoreError(t *testing.T) {
	db, repo := setupScanResultTestDB(t)
	if err := db.Migrator().DropTable(&domain.ScanEnrichmentEntity{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	rows, err := repo.ListOpenEnrichments([]string{"PENDING"}, time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC), 100)
	if err == nil || len(rows) != 0 {
		t.Fatalf("rows %v, err %v, want an error", rows, err)
	}
}
