package handlers

import (
	"testing"

	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/persistence/planlimit"
	"cafe-persistence/internal/persistence/storage"
	"cafe-persistence/internal/repository"
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
		&domain.TLSScanResultEntity{},
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

func TestApplyCompleted_WalletDoesNotWriteScanOrLedger(t *testing.T) {
	handler, db, userID, _ := setupQuotaCompletionTest(t)
	address := "0xignored"
	missingID := uuid.New()
	runningID := uuid.New()
	if err := storage.NewWalletWriter(db).OnStarted(runningID, userID, address); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}
	seedUsage(t, db, userID, runningID, domain.ScanUsageKindWallet)
	before, err := usageForScan(db, runningID)
	if err != nil {
		t.Fatalf("seeded ledger: %v", err)
	}

	for _, scanID := range []uuid.UUID{missingID, runningID} {
		if err := handler.applyCompleted(&scanCompleted{
			ScanID: scanID, Kind: "wallet", UserID: userID, Address: address,
			Result: walletResult(address, domain.AccountTypeEOA),
		}); err != nil {
			t.Fatalf("applyCompleted %s: %v", scanID, err)
		}
	}

	var n int64
	if err := db.Model(&domain.ScanResultEntity{}).Where("id = ?", missingID).Count(&n).Error; err != nil {
		t.Fatalf("count missing scan: %v", err)
	}
	if n != 0 {
		t.Fatal("wallet scan.completed inserted a row")
	}
	var row domain.ScanResultEntity
	if err := db.Where("id = ?", runningID).First(&row).Error; err != nil {
		t.Fatalf("load seeded scan: %v", err)
	}
	if row.Status != scan.StateRUNNING || row.Type != "" || row.Error != "" || row.PublicKey != "" {
		t.Fatalf("seeded RUNNING row changed: %+v", row)
	}
	assertUsageCount(t, db, userID, domain.ScanUsageKindWallet, 1)
	after, err := usageForScan(db, runningID)
	if err != nil {
		t.Fatalf("seeded ledger after completion: %v", err)
	}
	if after.ID != before.ID || !after.ConsumedAt.Equal(before.ConsumedAt) {
		t.Fatalf("ledger row changed: before=%+v after=%+v", before, after)
	}
	if _, err := usageForScan(db, missingID); err == nil {
		t.Fatal("wallet scan.completed acquired a ledger row")
	}
}

func TestHandleWalletFailed_DoesNotReleaseReservation(t *testing.T) {
	handler, db, userID, planID := setupQuotaCompletionTest(t)
	setPlanLimit(t, db, planID, "wallet_scan_limit", 1)
	cache := &memRedis{values: map[string]string{}}
	bus := &recordingNATS{}
	handler.redisCache = storage.NewRedisCache(cache)
	handler.natsConn = bus

	scanID := uuid.New()
	otherID := uuid.New()
	address := "0xnoreult"
	seedUsage(t, db, userID, scanID, domain.ScanUsageKindWallet)
	seedUsage(t, db, userID, otherID, domain.ScanUsageKindWallet)
	if err := storage.NewWalletWriter(db).OnStarted(scanID, userID, address); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	err := handler.applyFailed(&scanFailed{
		ScanID: scanID, Kind: "wallet", UserID: userID, Address: address, Error: "rpc down",
	})
	if err != nil {
		t.Fatalf("applyFailed: %v", err)
	}
	assertUsageCount(t, db, userID, domain.ScanUsageKindWallet, 2)
	if _, err := usageForScan(db, scanID); err != nil {
		t.Fatalf("wallet scan.failed must keep the reservation: %v", err)
	}
	if _, err := usageForScan(db, otherID); err != nil {
		t.Fatalf("other reservation must stay: %v", err)
	}
	assertWalletStatus(t, db, scanID, scan.StateRUNNING, "")
	if len(cache.values) != 0 || len(bus.subjects) != 0 {
		t.Fatalf("wallet projection redis=%v nats=%v", cache.values, bus.subjects)
	}
}

func TestHandleWalletFailed_AfterSuccessKeepsUsage(t *testing.T) {
	handler, db, userID, _ := setupQuotaCompletionTest(t)
	handler.redisCache = storage.NewRedisCache(&memRedis{values: map[string]string{}})

	scanID := uuid.New()
	address := "0xdone"
	seedUsage(t, db, userID, scanID, domain.ScanUsageKindWallet)
	rich := walletResult(address, domain.AccountTypeEOA)
	entity := domain.FromScanResult(userID, rich)
	entity.ID = scanID
	entity.Status = scan.StateSUCCESS
	if err := db.Create(entity).Error; err != nil {
		t.Fatalf("seed success: %v", err)
	}

	err := handler.applyFailed(&scanFailed{
		ScanID: scanID, Kind: "wallet", UserID: userID, Address: address, Error: "late",
	})
	if err != nil {
		t.Fatalf("applyFailed: %v", err)
	}
	assertUsageCount(t, db, userID, domain.ScanUsageKindWallet, 1)
	assertWalletStatus(t, db, scanID, scan.StateSUCCESS, "")
}

func TestCommitTLSCompletion_ExistingReservationConfirmsAtLimit(t *testing.T) {
	handler, db, userID, planID := setupQuotaCompletionTest(t)
	setPlanLimit(t, db, planID, "endpoint_scan_limit", 1)

	scanID := uuid.New()
	endpoint := "https://reserved.example"
	seedUsage(t, db, userID, scanID, domain.ScanUsageKindEndpoint)
	if err := storage.NewTLSWriter(db).OnStarted(scanID, &userID, endpoint); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	result := tlsResult(endpoint)
	acquired := commitTLS(t, handler, userID, scanID, endpoint, result)
	if !acquired {
		t.Fatal("existing TLS reservation must confirm the scan")
	}
	assertUsageCount(t, db, userID, domain.ScanUsageKindEndpoint, 1)
	assertTLSStatus(t, db, scanID, scan.StateSUCCESS, "")
}

func TestCommitTLSCompletion_TwoCompletionsOneLedgerRow(t *testing.T) {
	handler, db, userID, planID := setupQuotaCompletionTest(t)
	setPlanLimit(t, db, planID, "endpoint_scan_limit", 1)

	scanID := uuid.New()
	endpoint := "https://twice.example"
	if err := storage.NewTLSWriter(db).OnStarted(scanID, &userID, endpoint); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}
	result := tlsResult(endpoint)
	if !commitTLS(t, handler, userID, scanID, endpoint, result) {
		t.Fatal("first TLS completion must acquire the only credit")
	}
	if !commitTLS(t, handler, userID, scanID, endpoint, result) {
		t.Fatal("second TLS completion must confirm the existing row")
	}
	assertUsageCount(t, db, userID, domain.ScanUsageKindEndpoint, 1)
	assertTLSStatus(t, db, scanID, scan.StateSUCCESS, "")
}

func TestHandleTLSFailed_ReleasesReservation(t *testing.T) {
	handler, db, userID, planID := setupQuotaCompletionTest(t)
	setPlanLimit(t, db, planID, "endpoint_scan_limit", 1)
	handler.redisCache = storage.NewRedisCache(&memRedis{values: map[string]string{}})

	scanID := uuid.New()
	endpoint := "https://failed.example"
	seedUsage(t, db, userID, scanID, domain.ScanUsageKindEndpoint)
	if err := storage.NewTLSWriter(db).OnStarted(scanID, &userID, endpoint); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	err := handler.applyFailed(&scanFailed{
		ScanID: scanID, Kind: "tls", UserID: userID, Endpoint: endpoint, Error: "dial timeout",
	})
	if err != nil {
		t.Fatalf("applyFailed: %v", err)
	}
	assertUsageCount(t, db, userID, domain.ScanUsageKindEndpoint, 0)
	assertTLSStatus(t, db, scanID, scan.StateFAILED, "dial timeout")
}

func setPlanLimit(t *testing.T, db *gorm.DB, planID uuid.UUID, column string, limit int) {
	t.Helper()
	if err := db.Model(&domain.Plan{}).Where("id = ?", planID).Update(column, limit).Error; err != nil {
		t.Fatalf("set %s: %v", column, err)
	}
}

func seedUsage(t *testing.T, db *gorm.DB, userID, scanID uuid.UUID, kind domain.ScanUsageKind) {
	t.Helper()
	if err := db.Create(&domain.ScanUsageEventEntity{
		ID: uuid.New(), UserID: userID, ScanID: scanID, ScanKind: kind,
	}).Error; err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
}

func walletResult(address string, accountType domain.AccountType) *domain.ScanResult {
	return &domain.ScanResult{
		Address: address, Type: accountType,
		Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1,
		RiskScore: 1, Networks: []string{"ethereum"},
	}
}

func tlsResult(url string) *domain.TLSScanResult {
	return &domain.TLSScanResult{
		URL: url, Host: "reserved.example", Port: 443,
		ProtocolVersion: "TLS 1.3", NISTLevel: domain.NISTLevel1,
		RiskScore: 1, PQCRisk: "high",
	}
}

func commitTLS(t *testing.T, handler *ScanEventHandler, userID, scanID uuid.UUID, endpoint string, result *domain.TLSScanResult) bool {
	t.Helper()
	msg := &scanCompleted{
		ScanID: scanID, Kind: "tls", UserID: userID, Endpoint: endpoint, Result: result,
	}
	user := &userID
	entity := domain.FromTLSScanResult(user, result, false)
	entity.ID = scanID
	acquired, err := handler.commitTLSCompletion(msg, entity, result)
	if err != nil {
		t.Fatalf("commitTLSCompletion: %v", err)
	}
	return acquired
}

func assertUsageCount(t *testing.T, db *gorm.DB, userID uuid.UUID, kind domain.ScanUsageKind, want int64) {
	t.Helper()
	count, err := repository.NewScanUsageLedgerRepository(db).CountSuccessUsage(userID, kind)
	if err != nil {
		t.Fatalf("ledger count: %v", err)
	}
	if count != want {
		t.Fatalf("ledger count: want %d, got %d", want, count)
	}
}

func usageForScan(db *gorm.DB, scanID uuid.UUID) (domain.ScanUsageEventEntity, error) {
	var row domain.ScanUsageEventEntity
	err := db.Where("scan_id = ?", scanID).First(&row).Error
	return row, err
}

func assertWalletStatus(t *testing.T, db *gorm.DB, scanID uuid.UUID, status, errMsg string) {
	t.Helper()
	var row domain.ScanResultEntity
	if err := db.Where("id = ?", scanID).First(&row).Error; err != nil {
		t.Fatalf("load wallet: %v", err)
	}
	if row.Status != status {
		t.Fatalf("wallet status: want %s, got %s", status, row.Status)
	}
	if row.Error != errMsg {
		t.Fatalf("wallet error: want %q, got %q", errMsg, row.Error)
	}
}

func assertTLSStatus(t *testing.T, db *gorm.DB, scanID uuid.UUID, status, errMsg string) {
	t.Helper()
	var row domain.TLSScanResultEntity
	if err := db.Where("id = ?", scanID).First(&row).Error; err != nil {
		t.Fatalf("load tls: %v", err)
	}
	if row.Status != status {
		t.Fatalf("tls status: want %s, got %s", status, row.Status)
	}
	if row.Error != errMsg {
		t.Fatalf("tls error: want %q, got %q", errMsg, row.Error)
	}
}
