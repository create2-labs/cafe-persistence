package handlers

import (
	"sync"
	"testing"

	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/persistence/planlimit"
	"cafe-persistence/internal/persistence/storage"
	"cafe-persistence/internal/repository"
	"cafe-persistence/pkg/nats"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupQuotaCompletionTest(t *testing.T) (*ScanEventHandler, *gorm.DB, uuid.UUID, uuid.UUID) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.New().String()+"?mode=memory&cache=shared&_txlock=immediate"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&domain.ScanUsageEventEntity{},
		&domain.ScanResultEntity{},
		&domain.User{},
		&domain.Plan{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	planID := uuid.New()
	userID := uuid.New()
	if err := db.Create(&domain.Plan{
		ID: planID, Name: "quota-test", Type: domain.PlanTypeFree,
		WalletScanLimit: 2, EndpointScanLimit: 2, IsActive: true,
	}).Error; err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	if err := db.Create(&domain.User{
		ID: userID, Email: "quota@test.local", Password: "x", PlanID: planID,
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	ledger := repository.NewScanUsageLedgerRepository(db)
	limits := planlimit.NewResolver(repository.NewUserRepository(db), repository.NewPlanRepository(db))
	walletWriter := storage.NewWalletWriter(db)
	handler := NewScanEventHandler(
		storage.NewTLSWriter(db),
		walletWriter,
		nil,
		nil,
		nil,
		db,
		ledger,
		limits,
	)
	return handler, db, userID, planID
}

func TestCommitWalletCompletion_AtLimitOneRichOneStub(t *testing.T) {
	handler, db, userID, _ := setupQuotaCompletionTest(t)
	address := "0xlimitrace"

	seedScan := uuid.New()
	if err := db.Create(&domain.ScanUsageEventEntity{
		ID: uuid.New(), UserID: userID, ScanID: seedScan,
		ScanKind: domain.ScanUsageKindWallet,
	}).Error; err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	scanA := uuid.New()
	scanB := uuid.New()
	for _, id := range []uuid.UUID{scanA, scanB} {
		if err := storage.NewWalletWriter(db).OnStarted(id, userID, address); err != nil {
			t.Fatalf("OnStarted %s: %v", id, err)
		}
	}

	richResult := &domain.ScanResult{
		Address: address, Type: domain.AccountTypeEOA,
		Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1,
		KeyExposed: true, PublicKey: "0xsecret", RiskScore: 7.5,
		Networks: []string{"ethereum"}, Connections: []string{"peer"},
	}

	const workers = 2
	var wg sync.WaitGroup
	results := make([]bool, workers)
	scanIDs := []uuid.UUID{scanA, scanB}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			msg := &nats.ScanCompletedMessage{
				ScanID: scanIDs[idx], Kind: "wallet", UserID: userID, Address: address,
				Result: richResult,
			}
			entity := domain.FromScanResult(userID, richResult)
			entity.ID = scanIDs[idx]
			acquired, err := handler.commitWalletCompletion(msg, entity, richResult)
			if err != nil {
				t.Errorf("commitWalletCompletion: %v", err)
				return
			}
			results[idx] = acquired
		}(i)
	}
	wg.Wait()

	var successCount int
	for _, ok := range results {
		if ok {
			successCount++
		}
	}
	if successCount != 1 {
		t.Fatalf("want exactly 1 success slot, got %d (results=%v)", successCount, results)
	}

	ledgerCount, err := repository.NewScanUsageLedgerRepository(db).CountSuccessUsage(userID, domain.ScanUsageKindWallet)
	if err != nil {
		t.Fatalf("ledger count: %v", err)
	}
	if ledgerCount != 2 {
		t.Fatalf("want ledger count 2 (seed + one success), got %d", ledgerCount)
	}

	var rows []domain.ScanResultEntity
	if err := db.Where("user_id = ? AND id IN ?", userID, []uuid.UUID{scanA, scanB}).Find(&rows).Error; err != nil {
		t.Fatalf("load rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 scan rows, got %d", len(rows))
	}

	var rich, stub int
	for _, row := range rows {
		switch row.Status {
		case scan.StateSUCCESS:
			rich++
			if row.PublicKey == "" || row.Networks == "" || row.Networks == "[]" {
				t.Fatalf("success row must keep rich result: %+v", row)
			}
		case scan.StateFAILED:
			stub++
			if row.Error != scan.ErrPlanLimitExceeded {
				t.Fatalf("stub error: want %s, got %q", scan.ErrPlanLimitExceeded, row.Error)
			}
			if row.Address != address {
				t.Fatalf("stub must keep address, got %q", row.Address)
			}
			if row.PublicKey != "" || row.KeyExposed || row.Networks != "" || row.Connections != "" {
				t.Fatalf("stub must strip exploitable fields: %+v", row)
			}
		default:
			t.Fatalf("unexpected status %s for scan %s", row.Status, row.ID)
		}
	}
	if rich != 1 || stub != 1 {
		t.Fatalf("want 1 rich success + 1 stub, got rich=%d stub=%d", rich, stub)
	}
}

func TestCommitWalletCompletion_UnlimitedAlwaysRecordsLedger(t *testing.T) {
	handler, db, userID, planID := setupQuotaCompletionTest(t)
	if err := db.Model(&domain.Plan{}).Where("id = ?", planID).Update("wallet_scan_limit", 0).Error; err != nil {
		t.Fatalf("set unlimited: %v", err)
	}

	scanID := uuid.New()
	address := "0xunlimited"
	if err := storage.NewWalletWriter(db).OnStarted(scanID, userID, address); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	rich := &domain.ScanResult{
		Address: address, Type: domain.AccountTypeEOA,
		Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1,
		RiskScore: 1.0, Networks: []string{"ethereum"},
	}
	msg := &nats.ScanCompletedMessage{
		ScanID: scanID, Kind: "wallet", UserID: userID, Address: address, Result: rich,
	}
	entity := domain.FromScanResult(userID, rich)
	entity.ID = scanID

	acquired, err := handler.commitWalletCompletion(msg, entity, rich)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if !acquired {
		t.Fatal("unlimited plan must accept completion")
	}

	count, err := repository.NewScanUsageLedgerRepository(db).CountSuccessUsage(userID, domain.ScanUsageKindWallet)
	if err != nil {
		t.Fatalf("ledger count: %v", err)
	}
	if count != 1 {
		t.Fatalf("want 1 ledger row, got %d", count)
	}
}
