package storage

import (
	"testing"

	"cafe-persistence/internal/domain"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
)

func TestTLSWriter_OnPlanLimitExceededInTx_StripsResult(t *testing.T) {
	w := setupTLSWriterTestDB(t)
	userID := uuid.New()
	scanID := uuid.New()
	url := "https://quota.example"

	if err := w.OnStarted(scanID, &userID, url); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	rich := &domain.TLSScanResultEntity{
		UserID: &userID, URL: url, Host: "quota.example", Port: 443,
		ProtocolVersion: "TLS 1.3", NISTLevel: domain.NISTLevel1,
		RiskScore: 8, PQCRisk: "high", Certificate: `{"subject":"x"}`,
		CipherSuites: `["TLS_AES_128"]`, SupportedPQCs: `["kyber"]`,
	}
	if err := w.OnCompletedInTx(w.db, scanID, rich); err != nil {
		t.Fatalf("OnCompletedInTx: %v", err)
	}

	if err := w.OnPlanLimitExceededInTx(w.db, scanID, &userID, url); err != nil {
		t.Fatalf("OnPlanLimitExceededInTx: %v", err)
	}

	var stored domain.TLSScanResultEntity
	if err := w.db.Where("id = ?", scanID).First(&stored).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if stored.Status != scan.StateFAILED {
		t.Fatalf("status: want %s, got %s", scan.StateFAILED, stored.Status)
	}
	if stored.Error != scan.ErrPlanLimitExceeded {
		t.Fatalf("error: want %s, got %q", scan.ErrPlanLimitExceeded, stored.Error)
	}
	if stored.URL != url {
		t.Fatalf("url: want %s, got %s", url, stored.URL)
	}
	assertTLSPlanLimitStubNoCryptoPosture(t, stored)
}

func TestTLSWriter_OnPlanLimitExceededInTx_LifecycleOnlyRow(t *testing.T) {
	w := setupTLSWriterTestDB(t)
	userID := uuid.New()
	scanID := uuid.New()
	url := "https://quota-lifecycle.example"

	if err := w.OnStarted(scanID, &userID, url); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}
	if err := w.OnPlanLimitExceededInTx(w.db, scanID, &userID, url); err != nil {
		t.Fatalf("OnPlanLimitExceededInTx: %v", err)
	}

	var stored domain.TLSScanResultEntity
	if err := w.db.Where("id = ?", scanID).First(&stored).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if stored.Status != scan.StateFAILED {
		t.Fatalf("status: want %s, got %s", scan.StateFAILED, stored.Status)
	}
	if stored.Error != scan.ErrPlanLimitExceeded {
		t.Fatalf("error: want %s, got %q", scan.ErrPlanLimitExceeded, stored.Error)
	}
	if stored.URL != url {
		t.Fatalf("url: want %s, got %s", url, stored.URL)
	}
	assertTLSPlanLimitStubNoCryptoPosture(t, stored)
}

func TestTLSWriter_OnPlanLimitExceededInTx_InsertOnReplay(t *testing.T) {
	w := setupTLSWriterTestDB(t)
	userID := uuid.New()
	scanID := uuid.New()
	url := "https://quota-replay.example"

	if err := w.OnPlanLimitExceededInTx(w.db, scanID, &userID, url); err != nil {
		t.Fatalf("OnPlanLimitExceededInTx: %v", err)
	}

	var stored domain.TLSScanResultEntity
	if err := w.db.Where("id = ?", scanID).First(&stored).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if stored.UserID == nil || *stored.UserID != userID {
		t.Fatalf("user_id = %v, want %v", stored.UserID, userID)
	}
	if stored.URL != url {
		t.Fatalf("url: want %s, got %s", url, stored.URL)
	}
	if stored.Status != scan.StateFAILED {
		t.Fatalf("status: want %s, got %s", scan.StateFAILED, stored.Status)
	}
	if stored.Error != scan.ErrPlanLimitExceeded {
		t.Fatalf("error: want %s, got %q", scan.ErrPlanLimitExceeded, stored.Error)
	}
	assertTLSPlanLimitStubNoCryptoPosture(t, stored)
}

func assertTLSPlanLimitStubNoCryptoPosture(t *testing.T, stored domain.TLSScanResultEntity) {
	t.Helper()
	if stored.Host != "" {
		t.Fatalf("host must be empty on plan limit stub, got %q", stored.Host)
	}
	if stored.Port != 0 {
		t.Fatalf("port must be zero on plan limit stub, got %d", stored.Port)
	}
	if stored.ProtocolVersion != "" {
		t.Fatalf("protocol_version must be empty on plan limit stub, got %q", stored.ProtocolVersion)
	}
	if stored.NISTLevel != 0 {
		t.Fatalf("nist_level must be zero on plan limit stub, got %v", stored.NISTLevel)
	}
	if stored.PQCRisk != "" {
		t.Fatalf("pqc_risk must be empty on plan limit stub, got %q", stored.PQCRisk)
	}
	if stored.Certificate != "" {
		t.Fatalf("certificate must be empty, got %q", stored.Certificate)
	}
	if stored.CipherSuites != "" {
		t.Fatalf("cipher_suites must be empty, got %q", stored.CipherSuites)
	}
	if stored.SupportedPQCs != "" {
		t.Fatalf("supported_pqcs must be empty, got %q", stored.SupportedPQCs)
	}
	if stored.Recommendations != "" {
		t.Fatalf("recommendations must be empty, got %q", stored.Recommendations)
	}
	if stored.NISTLevels != "" {
		t.Fatalf("nist_levels must be empty, got %q", stored.NISTLevels)
	}
	if stored.RiskScore != 0 {
		t.Fatalf("risk_score must be 0, got %v", stored.RiskScore)
	}
}
