package storage

import (
	"testing"

	"cafe-persistence/internal/domain"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupWalletWriterTestDB(t *testing.T) *WalletWriter {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.ScanResultEntity{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewWalletWriter(db)
}

func setupTLSWriterTestDB(t *testing.T) *TLSWriter {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.TLSScanResultEntity{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewTLSWriter(db)
}

func TestWalletWriter_TwoScanIDsSameAddressPreservesTerminalA(t *testing.T) {
	w := setupWalletWriterTestDB(t)
	userID := uuid.New()
	address := "0xabc"
	scanA := uuid.New()
	scanB := uuid.New()

	if err := w.OnStarted(scanA, userID, address); err != nil {
		t.Fatalf("OnStarted A: %v", err)
	}
	entityA := domain.FromScanResult(userID, &domain.ScanResult{
		Address: address, Type: domain.AccountTypeEOA,
		Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1,
		IsEOA: true, RiskScore: 1.0,
	})
	if err := w.OnCompleted(scanA, entityA); err != nil {
		t.Fatalf("OnCompleted A: %v", err)
	}

	if err := w.OnStarted(scanB, userID, address); err != nil {
		t.Fatalf("OnStarted B: %v", err)
	}
	entityB := domain.FromScanResult(userID, &domain.ScanResult{
		Address: address, Type: domain.AccountTypeEOA,
		Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1,
		IsEOA: true, RiskScore: 9.0,
	})
	if err := w.OnCompleted(scanB, entityB); err != nil {
		t.Fatalf("OnCompleted B: %v", err)
	}

	var rows []domain.ScanResultEntity
	if err := w.db.Where("user_id = ? AND address = ?", userID, address).Find(&rows).Error; err != nil {
		t.Fatalf("query rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows for same address, got %d", len(rows))
	}

	statusA, err := w.GetStatus(scanA)
	if err != nil {
		t.Fatalf("GetStatus A: %v", err)
	}
	if statusA != scan.StateSUCCESS {
		t.Fatalf("scan A status: want %s, got %s", scan.StateSUCCESS, statusA)
	}

	var storedA domain.ScanResultEntity
	if err := w.db.Where("id = ?", scanA).First(&storedA).Error; err != nil {
		t.Fatalf("load A: %v", err)
	}
	if storedA.RiskScore != 1.0 {
		t.Fatalf("scan A risk_score: want 1.0, got %v", storedA.RiskScore)
	}
	if storedA.CreatedAt.IsZero() {
		t.Fatal("scan A created_at was reset to zero after completion")
	}

	var storedB domain.ScanResultEntity
	if err := w.db.Where("id = ?", scanB).First(&storedB).Error; err != nil {
		t.Fatalf("load B: %v", err)
	}
	if storedB.RiskScore != 9.0 {
		t.Fatalf("scan B risk_score: want 9.0, got %v", storedB.RiskScore)
	}
	if storedB.CreatedAt.IsZero() {
		t.Fatal("scan B created_at was reset to zero after completion")
	}
}

func TestWalletWriter_OnCompleted_StoresPublicKeyRecovery(t *testing.T) {
	w := setupWalletWriterTestDB(t)
	userID := uuid.New()
	scanID := uuid.New()
	address := "0xrecover"

	if err := w.OnStarted(scanID, userID, address); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}
	entity := domain.FromScanResult(userID, &domain.ScanResult{
		Address:           address,
		Type:              domain.AccountTypeEOA,
		PublicKeyRecovery: domain.PublicKeyRecoveryUnresolved,
	})
	if err := w.OnCompleted(scanID, entity); err != nil {
		t.Fatalf("OnCompleted: %v", err)
	}

	var stored domain.ScanResultEntity
	if err := w.db.Where("id = ?", scanID).First(&stored).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if stored.PublicKeyRecovery != domain.PublicKeyRecoveryUnresolved {
		t.Fatalf("public_key_recovery = %q", stored.PublicKeyRecovery)
	}
	if got := stored.ToScanResult().PublicKeyRecovery; got != domain.PublicKeyRecoveryUnresolved {
		t.Fatalf("dto public_key_recovery = %q", got)
	}
}

func TestWalletWriter_OnStartedIdempotentByScanID(t *testing.T) {
	w := setupWalletWriterTestDB(t)
	userID := uuid.New()
	scanID := uuid.New()
	address := "0xdef"

	if err := w.OnStarted(scanID, userID, address); err != nil {
		t.Fatalf("first OnStarted: %v", err)
	}
	if err := w.OnStarted(scanID, userID, address); err != nil {
		t.Fatalf("second OnStarted: %v", err)
	}

	var count int64
	if err := w.db.Model(&domain.ScanResultEntity{}).Where("id = ?", scanID).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("want 1 row after duplicate start, got %d", count)
	}
}

func TestWalletWriter_OnStartedInsertsLifecycleFieldsOnly(t *testing.T) {
	w := setupWalletWriterTestDB(t)
	userID := uuid.New()
	scanID := uuid.New()
	address := "0xlifecycle"

	if err := w.OnStarted(scanID, userID, address); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	var stored domain.ScanResultEntity
	if err := w.db.Where("id = ?", scanID).First(&stored).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if stored.UserID != userID {
		t.Fatalf("user_id = %v, want %v", stored.UserID, userID)
	}
	if stored.Address != address {
		t.Fatalf("address = %q, want %q", stored.Address, address)
	}
	if stored.Status != scan.StateRUNNING {
		t.Fatalf("status = %q, want %q", stored.Status, scan.StateRUNNING)
	}
	if stored.Type != "" {
		t.Fatalf("type = %q, want empty before scan.completed", stored.Type)
	}
	if stored.Algorithm != "" {
		t.Fatalf("algorithm = %q, want empty before scan.completed", stored.Algorithm)
	}
	if stored.IsEOA {
		t.Fatal("is_eoa must not be true before scan.completed")
	}
	if stored.NISTLevel != 0 {
		t.Fatalf("nist_level = %v, want zero before scan.completed", stored.NISTLevel)
	}
	if stored.RiskScore != 0 {
		t.Fatalf("risk_score = %v, want zero before scan.completed", stored.RiskScore)
	}
	if stored.Networks != "" {
		t.Fatalf("networks = %q, want empty before scan.completed", stored.Networks)
	}
	if stored.PublicKeyRecovery != "" {
		t.Fatalf("public_key_recovery = %q, want empty before scan.completed", stored.PublicKeyRecovery)
	}
}

func TestTLSWriter_OnStartedInsertsLifecycleFieldsOnly(t *testing.T) {
	w := setupTLSWriterTestDB(t)
	userID := uuid.New()
	scanID := uuid.New()
	url := "https://lifecycle.example"

	if err := w.OnStarted(scanID, &userID, url); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	var stored domain.TLSScanResultEntity
	if err := w.db.Where("id = ?", scanID).First(&stored).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if stored.UserID == nil || *stored.UserID != userID {
		t.Fatalf("user_id = %v, want %v", stored.UserID, userID)
	}
	if stored.URL != url {
		t.Fatalf("url = %q, want %q", stored.URL, url)
	}
	if stored.Status != scan.StateRUNNING {
		t.Fatalf("status = %q, want %q", stored.Status, scan.StateRUNNING)
	}
	if stored.ProtocolVersion != "" {
		t.Fatalf("protocol_version = %q, want empty before scan.completed", stored.ProtocolVersion)
	}
	if stored.NISTLevel != 0 {
		t.Fatalf("nist_level = %v, want zero before scan.completed", stored.NISTLevel)
	}
	if stored.PQCRisk != "" {
		t.Fatalf("pqc_risk = %q, want empty before scan.completed", stored.PQCRisk)
	}
	if stored.RiskScore != 0 {
		t.Fatalf("risk_score = %v, want zero before scan.completed", stored.RiskScore)
	}
}

func TestWalletWriter_AccidentalInsertWithoutStatusNotRunning(t *testing.T) {
	w := setupWalletWriterTestDB(t)
	userID := uuid.New()
	scanID := uuid.New()
	address := "0xaccidental"

	ent := &domain.ScanResultEntity{ID: scanID, UserID: userID, Address: address}
	if err := w.db.Create(ent).Error; err != nil {
		t.Fatalf("Create without status: %v", err)
	}

	status, err := w.GetStatus(scanID)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status == scan.StateRUNNING {
		t.Fatalf("accidental insert must not default to RUNNING, got %q", status)
	}
}

func TestTLSWriter_AccidentalInsertWithoutStatusNotRunning(t *testing.T) {
	w := setupTLSWriterTestDB(t)
	userID := uuid.New()
	scanID := uuid.New()
	url := "https://accidental.example"

	ent := &domain.TLSScanResultEntity{ID: scanID, UserID: &userID, URL: url}
	if err := w.db.Create(ent).Error; err != nil {
		t.Fatalf("Create without status: %v", err)
	}

	status, err := w.GetStatus(scanID)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status == scan.StateRUNNING {
		t.Fatalf("accidental insert must not default to RUNNING, got %q", status)
	}
}

func TestTLSWriter_OnCompletedPreservesCreatedAt(t *testing.T) {
	w := setupTLSWriterTestDB(t)
	userID := uuid.New()
	scanID := uuid.New()
	url := "https://example.com"

	if err := w.OnStarted(scanID, &userID, url); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	entity := &domain.TLSScanResultEntity{
		UserID: &userID, URL: url, Host: "example.com", Port: 443,
		ProtocolVersion: "TLS 1.3", NISTLevel: domain.NISTLevel1,
		RiskScore: 1, PQCRisk: "medium",
	}
	if err := w.OnCompleted(scanID, entity); err != nil {
		t.Fatalf("OnCompleted: %v", err)
	}

	var stored domain.TLSScanResultEntity
	if err := w.db.Where("id = ?", scanID).First(&stored).Error; err != nil {
		t.Fatalf("load TLS scan: %v", err)
	}
	if stored.CreatedAt.IsZero() {
		t.Fatal("TLS created_at was reset to zero after completion")
	}
}

func TestTLSWriter_DefaultRescanReplacesNilUserRows(t *testing.T) {
	w := setupTLSWriterTestDB(t)
	url := "https://nginx"
	otherURL := "https://other.example"
	oldID := uuid.New()
	failedID := uuid.New()
	otherID := uuid.New()
	userID := uuid.New()
	userScanID := uuid.New()
	newID := uuid.New()

	seed := []domain.TLSScanResultEntity{
		{ID: oldID, URL: url, Default: true, Status: scan.StateSUCCESS, Host: "nginx", ProtocolVersion: "TLS 1.3", PQCRisk: "low"},
		{ID: failedID, URL: url, Default: false, Status: scan.StateFAILED, Host: "nginx", ProtocolVersion: "unknown", PQCRisk: "unknown"},
		{ID: otherID, URL: otherURL, Default: true, Status: scan.StateSUCCESS, Host: "other", ProtocolVersion: "TLS 1.3", PQCRisk: "low"},
		{ID: userScanID, UserID: &userID, URL: url, Default: false, Status: scan.StateSUCCESS, Host: "nginx", ProtocolVersion: "TLS 1.3", PQCRisk: "low"},
	}
	for i := range seed {
		if err := w.db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if err := w.OnStarted(newID, nil, url); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	var live []domain.TLSScanResultEntity
	if err := w.db.Where("url = ?", url).Order("id").Find(&live).Error; err != nil {
		t.Fatalf("live rows: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("want the new default row and the user row, got %d", len(live))
	}
	var replaced domain.TLSScanResultEntity
	if err := w.db.Where("id = ?", newID).First(&replaced).Error; err != nil {
		t.Fatalf("replacement: %v", err)
	}
	if !replaced.Default || replaced.Status != scan.StateRUNNING || replaced.UserID != nil {
		t.Fatalf("replacement default=%v status=%s user=%v", replaced.Default, replaced.Status, replaced.UserID)
	}
	if err := w.db.Where("id = ?", otherID).First(&domain.TLSScanResultEntity{}).Error; err != nil {
		t.Fatalf("other default URL was removed: %v", err)
	}
	if err := w.db.Where("id = ?", userScanID).First(&domain.TLSScanResultEntity{}).Error; err != nil {
		t.Fatalf("user scan was removed: %v", err)
	}
	var gone int64
	if err := w.db.Unscoped().Model(&domain.TLSScanResultEntity{}).Where("id IN ? AND deleted_at IS NOT NULL", []uuid.UUID{oldID, failedID}).Count(&gone).Error; err != nil {
		t.Fatalf("count replaced: %v", err)
	}
	if gone != 2 {
		t.Fatalf("soft-deleted %d previous nil-user rows, want 2", gone)
	}

	if err := w.OnStarted(newID, nil, url); err != nil {
		t.Fatalf("redelivered OnStarted: %v", err)
	}
	if err := w.db.Where("id = ?", newID).First(&replaced).Error; err != nil {
		t.Fatalf("replacement removed on redelivery: %v", err)
	}

	if err := w.OnStarted(oldID, nil, url); err != nil {
		t.Fatalf("replaced scan OnStarted: %v", err)
	}
	if err := w.db.Where("id = ?", newID).First(&replaced).Error; err != nil {
		t.Fatalf("newer default removed by the replaced scan: %v", err)
	}
	var resurrected int64
	if err := w.db.Model(&domain.TLSScanResultEntity{}).Where("id = ?", oldID).Count(&resurrected).Error; err != nil {
		t.Fatalf("count resurrected: %v", err)
	}
	if resurrected != 0 {
		t.Fatalf("replaced scan was resurrected")
	}
}
