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
			if row.PublicKey != "" || row.KeyExposed || row.Networks != "" || row.Delegations != "" || row.Connections != "" {
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

func TestCommitWalletCompletion_ExistingReservationConfirmsAtLimit(t *testing.T) {
	handler, db, userID, planID := setupQuotaCompletionTest(t)
	setPlanLimit(t, db, planID, "wallet_scan_limit", 1)

	scanID := uuid.New()
	address := "0xreserved"
	seedUsage(t, db, userID, scanID, domain.ScanUsageKindWallet)
	if err := storage.NewWalletWriter(db).OnStarted(scanID, userID, address); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	rich := walletResult(address, domain.AccountTypeEOA)
	acquired := commitWallet(t, handler, userID, scanID, address, rich)
	if !acquired {
		t.Fatal("existing reservation must confirm the scan, not reject it as over limit")
	}
	assertUsageCount(t, db, userID, domain.ScanUsageKindWallet, 1)
	assertWalletStatus(t, db, scanID, scan.StateSUCCESS, "")
}

func TestCommitWalletCompletion_UnknownResultKeepsReservation(t *testing.T) {
	handler, db, userID, planID := setupQuotaCompletionTest(t)
	setPlanLimit(t, db, planID, "wallet_scan_limit", 1)

	scanID := uuid.New()
	address := "0xunknown"
	seedUsage(t, db, userID, scanID, domain.ScanUsageKindWallet)
	if err := storage.NewWalletWriter(db).OnStarted(scanID, userID, address); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	unknown := walletResult(address, domain.AccountTypeUnknown)
	if !commitWallet(t, handler, userID, scanID, address, unknown) {
		t.Fatal("unknown result must keep the reserved credit")
	}
	if !commitWallet(t, handler, userID, scanID, address, unknown) {
		t.Fatal("a second unknown completion must not take another credit")
	}
	assertUsageCount(t, db, userID, domain.ScanUsageKindWallet, 1)

	var row domain.ScanResultEntity
	if err := db.Where("id = ?", scanID).First(&row).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if row.Status != scan.StateSUCCESS {
		t.Fatalf("status: want %s, got %s", scan.StateSUCCESS, row.Status)
	}
	if row.Type != domain.AccountTypeUnknown {
		t.Fatalf("type: want %s, got %s", domain.AccountTypeUnknown, row.Type)
	}
}

func TestCommitWalletCompletion_TwoCompletionsOneLedgerRow(t *testing.T) {
	handler, db, userID, planID := setupQuotaCompletionTest(t)
	setPlanLimit(t, db, planID, "wallet_scan_limit", 1)

	scanID := uuid.New()
	address := "0xtwice"
	if err := storage.NewWalletWriter(db).OnStarted(scanID, userID, address); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}
	rich := walletResult(address, domain.AccountTypeEOA)

	if !commitWallet(t, handler, userID, scanID, address, rich) {
		t.Fatal("first completion must acquire the only credit")
	}
	if !commitWallet(t, handler, userID, scanID, address, rich) {
		t.Fatal("second completion of the same scan_id must confirm the existing row")
	}
	assertUsageCount(t, db, userID, domain.ScanUsageKindWallet, 1)
	assertWalletStatus(t, db, scanID, scan.StateSUCCESS, "")
}

func TestHandleWalletFailed_ReleasesReservation(t *testing.T) {
	handler, db, userID, planID := setupQuotaCompletionTest(t)
	setPlanLimit(t, db, planID, "wallet_scan_limit", 1)
	handler.redisCache = storage.NewRedisCache(&memRedis{values: map[string]string{}})

	scanID := uuid.New()
	otherID := uuid.New()
	address := "0xnoreult"
	seedUsage(t, db, userID, scanID, domain.ScanUsageKindWallet)
	seedUsage(t, db, userID, otherID, domain.ScanUsageKindWallet)
	if err := storage.NewWalletWriter(db).OnStarted(scanID, userID, address); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	err := handler.HandleFailed(&nats.ScanFailedMessage{
		ScanID: scanID, Kind: "wallet", UserID: userID, Address: address, Error: "rpc down",
	})
	if err != nil {
		t.Fatalf("HandleFailed: %v", err)
	}
	assertUsageCount(t, db, userID, domain.ScanUsageKindWallet, 1)
	if _, err := usageForScan(db, scanID); err == nil {
		t.Fatal("failed scan must drop its ledger row")
	}
	if _, err := usageForScan(db, otherID); err != nil {
		t.Fatalf("other reservation must stay: %v", err)
	}
	assertWalletStatus(t, db, scanID, scan.StateFAILED, "rpc down")
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

	err := handler.HandleFailed(&nats.ScanFailedMessage{
		ScanID: scanID, Kind: "wallet", UserID: userID, Address: address, Error: "late",
	})
	if err != nil {
		t.Fatalf("HandleFailed: %v", err)
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

	err := handler.HandleFailed(&nats.ScanFailedMessage{
		ScanID: scanID, Kind: "tls", UserID: userID, Endpoint: endpoint, Error: "dial timeout",
	})
	if err != nil {
		t.Fatalf("HandleFailed: %v", err)
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

func commitWallet(t *testing.T, handler *ScanEventHandler, userID, scanID uuid.UUID, address string, result *domain.ScanResult) bool {
	t.Helper()
	msg := &nats.ScanCompletedMessage{
		ScanID: scanID, Kind: "wallet", UserID: userID, Address: address, Result: result,
	}
	entity := domain.FromScanResult(userID, result)
	entity.ID = scanID
	acquired, err := handler.commitWalletCompletion(msg, entity, result)
	if err != nil {
		t.Fatalf("commitWalletCompletion: %v", err)
	}
	return acquired
}

func commitTLS(t *testing.T, handler *ScanEventHandler, userID, scanID uuid.UUID, endpoint string, result *domain.TLSScanResult) bool {
	t.Helper()
	msg := &nats.ScanCompletedMessage{
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
