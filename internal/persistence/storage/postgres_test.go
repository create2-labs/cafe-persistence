package storage

import (
	"testing"

	"cafe-persistence/internal/domain"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupScanResultTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.ScanResultEntity{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
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

func TestScanResultEntity_AccidentalInsertWithoutStatusNotRunning(t *testing.T) {
	db := setupScanResultTestDB(t)
	userID := uuid.New()
	scanID := uuid.New()
	address := "0xaccidental"

	ent := &domain.ScanResultEntity{ID: scanID, UserID: userID, Address: address}
	if err := db.Create(ent).Error; err != nil {
		t.Fatalf("Create without status: %v", err)
	}

	var stored domain.ScanResultEntity
	if err := db.Select("status").First(&stored, "id = ?", scanID).Error; err != nil {
		t.Fatalf("read status: %v", err)
	}
	if stored.Status == scan.StateRUNNING {
		t.Fatalf("accidental insert must not default to RUNNING, got %q", stored.Status)
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
