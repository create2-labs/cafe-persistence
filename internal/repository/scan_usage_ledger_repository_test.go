package repository

import (
	"sync"
	"testing"

	"cafe-persistence/internal/domain"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupScanUsageLedgerTestDB(t *testing.T) (*gorm.DB, ScanUsageLedgerRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.New().String()+"?mode=memory&cache=shared&_txlock=immediate"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&domain.ScanUsageEventEntity{},
		&domain.ScanResultEntity{},
		&domain.TLSScanResultEntity{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	return db, NewScanUsageLedgerRepository(db)
}

func TestScanUsageLedger_RecordSuccessUsage_Idempotent(t *testing.T) {
	_, repo := setupScanUsageLedgerTestDB(t)
	userID := uuid.New()
	scanID := uuid.New()

	if err := repo.RecordSuccessUsage(userID, scanID, domain.ScanUsageKindWallet); err != nil {
		t.Fatalf("first record: %v", err)
	}
	if err := repo.RecordSuccessUsage(userID, scanID, domain.ScanUsageKindWallet); err != nil {
		t.Fatalf("second record: %v", err)
	}

	count, err := repo.CountSuccessUsage(userID, domain.ScanUsageKindWallet)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("want 1 ledger row, got %d", count)
	}
}

func TestScanUsageLedger_CountInFlightScans(t *testing.T) {
	db, repo := setupScanUsageLedgerTestDB(t)
	userID := uuid.New()

	rows := []domain.ScanResultEntity{
		{ID: uuid.New(), UserID: userID, Address: "0x1", Status: scan.StateRUNNING, Type: domain.AccountTypeEOA, Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1},
		{ID: uuid.New(), UserID: userID, Address: "0x2", Status: scan.StateSUCCESS, Type: domain.AccountTypeEOA, Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1},
		{ID: uuid.New(), UserID: userID, Address: "0x3", Status: scan.StateFAILED, Type: domain.AccountTypeEOA, Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	inFlight, err := repo.CountInFlightScans(userID, domain.ScanUsageKindWallet)
	if err != nil {
		t.Fatalf("CountInFlightScans: %v", err)
	}
	if inFlight != 1 {
		t.Fatalf("want 1 in-flight, got %d", inFlight)
	}

	visible, err := repo.CountVisibleSuccessScans(userID, domain.ScanUsageKindWallet)
	if err != nil {
		t.Fatalf("CountVisibleSuccessScans: %v", err)
	}
	if visible != 1 {
		t.Fatalf("want 1 visible success, got %d", visible)
	}
}

func TestScanUsageLedger_TryAcquireSuccessSlot_AtLimit(t *testing.T) {
	_, repo := setupScanUsageLedgerTestDB(t)
	userID := uuid.New()
	limit := 2

	for i := 0; i < limit; i++ {
		if err := repo.RecordSuccessUsage(userID, uuid.New(), domain.ScanUsageKindWallet); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	ok, err := repo.TryAcquireSuccessSlot(userID, domain.ScanUsageKindWallet, limit)
	if err != nil {
		t.Fatalf("TryAcquireSuccessSlot: %v", err)
	}
	if ok {
		t.Fatal("expected no slot when count == limit")
	}

	ok, err = repo.TryAcquireSuccessSlot(userID, domain.ScanUsageKindWallet, 0)
	if err != nil || !ok {
		t.Fatalf("unlimited limit: ok=%v err=%v", ok, err)
	}
}

func TestScanUsageLedger_CountVisibleSuccessScans_ExcludesSoftDeleted(t *testing.T) {
	db, repo := setupScanUsageLedgerTestDB(t)
	userID := uuid.New()
	scanID := uuid.New()

	row := domain.ScanResultEntity{
		ID: scanID, UserID: userID, Address: "0xdel", Status: scan.StateSUCCESS,
		Type: domain.AccountTypeEOA, Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.RecordSuccessUsage(userID, scanID, domain.ScanUsageKindWallet); err != nil {
		t.Fatalf("ledger: %v", err)
	}

	used, err := repo.CountSuccessUsage(userID, domain.ScanUsageKindWallet)
	if err != nil {
		t.Fatalf("CountSuccessUsage: %v", err)
	}
	visible, err := repo.CountVisibleSuccessScans(userID, domain.ScanUsageKindWallet)
	if err != nil {
		t.Fatalf("CountVisibleSuccessScans before delete: %v", err)
	}
	if used != 1 || visible != 1 {
		t.Fatalf("before delete: used=%d visible=%d, want 1/1", used, visible)
	}

	if err := db.Delete(&domain.ScanResultEntity{}, "id = ?", scanID).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	usedAfter, err := repo.CountSuccessUsage(userID, domain.ScanUsageKindWallet)
	if err != nil {
		t.Fatalf("CountSuccessUsage after delete: %v", err)
	}
	visibleAfter, err := repo.CountVisibleSuccessScans(userID, domain.ScanUsageKindWallet)
	if err != nil {
		t.Fatalf("CountVisibleSuccessScans after delete: %v", err)
	}
	if usedAfter != 1 {
		t.Fatalf("used after delete = %d, want 1 (ledger unchanged)", usedAfter)
	}
	if visibleAfter != 0 {
		t.Fatalf("visible after delete = %d, want 0", visibleAfter)
	}
}

func TestScanUsageLedger_RecordSuccessUsageIfUnderLimit_Concurrent(t *testing.T) {
	db, repo := setupScanUsageLedgerTestDB(t)
	userID := uuid.New()
	limit := 2

	if err := repo.RecordSuccessUsage(userID, uuid.New(), domain.ScanUsageKindWallet); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	const workers = 8
	var wg sync.WaitGroup
	var recorded int64
	var mu sync.Mutex

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scanID := uuid.New()
			var inserted bool
			err := db.Transaction(func(tx *gorm.DB) error {
				var err error
				inserted, err = repo.RecordSuccessUsageIfUnderLimitInTx(tx, userID, scanID, domain.ScanUsageKindWallet, limit)
				return err
			})
			if err != nil {
				t.Errorf("transaction: %v", err)
				return
			}
			if inserted {
				mu.Lock()
				recorded++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	final, err := repo.CountSuccessUsage(userID, domain.ScanUsageKindWallet)
	if err != nil {
		t.Fatalf("final count: %v", err)
	}
	if final != int64(limit) {
		t.Fatalf("want ledger count %d after concurrent insert, got %d (recorded goroutines=%d)", limit, final, recorded)
	}
	if recorded != 1 {
		t.Fatalf("want exactly 1 concurrent slot taker, got %d", recorded)
	}
}
