package scanapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cafe-persistence/internal/config"
	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/repository"
	"cafe-persistence/internal/scanroutes"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const transitionAddr = "0xabc0000000000000000000000000000000000001"

func startTransitionAPI(t *testing.T, db *gorm.DB) *httptest.Server {
	t.Helper()
	h := NewHandler(
		newMemoryPendingRepo(),
		repository.NewScanResultRepository(db),
		repository.NewTLSScanResultRepository(db),
		repository.NewScanUsageLedgerRepository(db),
		nil,
		&config.ChainConfig{},
	)
	h.EnableWalletTransition(db)
	srv := NewServer("127.0.0.1", "0", testServiceToken, h)
	return httptest.NewServer(srv.HTTPHandler())
}

func transitionURL(base string, scanID uuid.UUID) string {
	url := base + scanroutes.Join(scanroutes.WalletScanTransition)
	return strings.Replace(url, "{scan_id}", scanID.String(), 1)
}

func postTransition(t *testing.T, url string, header uuid.UUID, body any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp := doAuthRequest(t, http.MethodPost, url, header, raw)
	defer func() { _ = resp.Body.Close() }()
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.StatusCode, got
}

func runningBody(id string, user uuid.UUID) map[string]any {
	return map[string]any{
		"transition_id": id,
		"user_id":       user.String(),
		"address":       transitionAddr,
		"source_states": []string{scan.StatePENDING},
		"target_state":  scan.StateRUNNING,
		"quota_action":  "none",
	}
}

func successBody(id string, user uuid.UUID, resultAddr string) map[string]any {
	return map[string]any{
		"transition_id": id,
		"user_id":       user.String(),
		"address":       transitionAddr,
		"source_states": []string{scan.StatePENDING, scan.StateRUNNING},
		"target_state":  scan.StateSUCCESS,
		"quota_action":  "keep",
		"result": map[string]any{
			"address": resultAddr, "type": "EOA", "algorithm": "ECDSA-secp256k1",
			"nist_level": 1, "is_eoa": true, "risk_score": 4,
		},
	}
}

func failedBody(id string, user uuid.UUID) map[string]any {
	return map[string]any{
		"transition_id": id,
		"user_id":       user.String(),
		"address":       transitionAddr,
		"source_states": []string{scan.StatePENDING, scan.StateRUNNING},
		"target_state":  scan.StateFAILED,
		"quota_action":  "release",
		"error":         "scan failed",
	}
}

func assertHTTPOutcome(t *testing.T, code int, got map[string]any, outcome, status string, withStatus bool) {
	t.Helper()
	if code != http.StatusOK {
		t.Fatalf("status %d body %#v", code, got)
	}
	if got["outcome"] != outcome || got["projection_ready"] != false || got["ready_published"] != false {
		t.Fatalf("body %#v", got)
	}
	raw, present := got["status"]
	if withStatus {
		if !present || raw != status {
			t.Fatalf("status %#v, want %s", raw, status)
		}
		return
	}
	if present {
		t.Fatalf("status should be absent, got %#v", raw)
	}
}

func ledgerRows(t *testing.T, db *gorm.DB, scanID uuid.UUID) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&domain.ScanUsageEventEntity{}).Where("scan_id = ?", scanID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func loadWalletRow(t *testing.T, db *gorm.DB, scanID uuid.UUID) (domain.ScanResultEntity, bool) {
	t.Helper()
	var ent domain.ScanResultEntity
	err := db.Where("id = ?", scanID).First(&ent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ScanResultEntity{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return ent, true
}

func TestApplyWalletTransition_ProtocolErrorsAreNotOutcomes(t *testing.T) {
	db := setupScanAPITestDB(t)
	ts := startTransitionAPI(t, db)
	defer ts.Close()
	user := uuid.New()
	url := transitionURL(ts.URL, uuid.New())
	raw, err := json.Marshal(runningBody("t-proto", user))
	if err != nil {
		t.Fatal(err)
	}

	missing, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	missing.Header.Set(headerAuthorization, "Bearer "+testServiceToken)
	missing.Header.Set("Content-Type", "application/json")
	invalid, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	invalid.Header.Set(headerAuthorization, "Bearer "+testServiceToken)
	invalid.Header.Set(headerUserID, "not-a-uuid")
	broken, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte("{")))
	if err != nil {
		t.Fatal(err)
	}
	broken.Header.Set(headerAuthorization, "Bearer "+testServiceToken)
	broken.Header.Set(headerUserID, user.String())
	broken.Header.Set("Content-Type", "application/json")

	for _, req := range []*http.Request{missing, invalid, broken} {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		decodeErr := json.NewDecoder(resp.Body).Decode(&got)
		_ = resp.Body.Close()
		if decodeErr != nil || resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status %d body %#v err %v", resp.StatusCode, got, decodeErr)
		}
		if _, ok := got["outcome"]; ok || got["error"] == nil {
			t.Fatalf("protocol body %#v", got)
		}
	}
}

func TestApplyWalletTransition_RejectedWritesNothing(t *testing.T) {
	db := setupScanAPITestDB(t)
	ts := startTransitionAPI(t, db)
	defer ts.Close()
	header := uuid.New()
	ledger := repository.NewScanUsageLedgerRepository(db)

	other := uuid.New()
	scanID := uuid.New()
	body := runningBody("t-user", other)
	code, got := postTransition(t, transitionURL(ts.URL, scanID), header, body)
	assertHTTPOutcome(t, code, got, "rejected", "", false)
	if _, ok := loadWalletRow(t, db, scanID); ok {
		t.Fatal("user mismatch wrote a row")
	}

	scanID = uuid.New()
	if err := ledger.RecordSuccessUsage(header, scanID, domain.ScanUsageKindWallet); err != nil {
		t.Fatal(err)
	}
	code, got = postTransition(t, transitionURL(ts.URL, scanID), header, successBody("t-addr", header, "0xdef0000000000000000000000000000000000002"))
	assertHTTPOutcome(t, code, got, "rejected", "", false)
	if _, ok := loadWalletRow(t, db, scanID); ok || ledgerRows(t, db, scanID) != 1 {
		t.Fatal("address mismatch wrote or changed the ledger")
	}

	scanID = uuid.New()
	bad := successBody("t-err", header, transitionAddr)
	bad["error"] = "must not be set"
	code, got = postTransition(t, transitionURL(ts.URL, scanID), header, bad)
	assertHTTPOutcome(t, code, got, "rejected", "", false)
	if _, ok := loadWalletRow(t, db, scanID); ok {
		t.Fatal("SUCCESS with error wrote a row")
	}
}

func TestApplyWalletTransition_QuotaAndOutcomes(t *testing.T) {
	db := setupScanAPITestDB(t)
	ts := startTransitionAPI(t, db)
	defer ts.Close()
	user := uuid.New()
	ledger := repository.NewScanUsageLedgerRepository(db)

	runningID := uuid.New()
	code, got := postTransition(t, transitionURL(ts.URL, runningID), user, runningBody("t-run", user))
	assertHTTPOutcome(t, code, got, "applied", scan.StateRUNNING, true)
	row, ok := loadWalletRow(t, db, runningID)
	if !ok || row.Status != scan.StateRUNNING || row.TransitionID == nil || *row.TransitionID != "t-run" || ledgerRows(t, db, runningID) != 0 {
		t.Fatalf("none quota row %+v ok %v", row, ok)
	}
	code, got = postTransition(t, transitionURL(ts.URL, runningID), user, runningBody("t-run", user))
	assertHTTPOutcome(t, code, got, "already_applied", scan.StateRUNNING, true)
	code, got = postTransition(t, transitionURL(ts.URL, runningID), user, runningBody("t-run-2", user))
	assertHTTPOutcome(t, code, got, "already_running", scan.StateRUNNING, true)

	keptID := uuid.New()
	if err := ledger.RecordSuccessUsage(user, keptID, domain.ScanUsageKindWallet); err != nil {
		t.Fatal(err)
	}
	code, got = postTransition(t, transitionURL(ts.URL, keptID), user, successBody("t-keep", user, transitionAddr))
	assertHTTPOutcome(t, code, got, "applied", scan.StateSUCCESS, true)
	if ledgerRows(t, db, keptID) != 1 {
		t.Fatal("keep changed the ledger")
	}
	code, got = postTransition(t, transitionURL(ts.URL, keptID), user, failedBody("t-late", user))
	assertHTTPOutcome(t, code, got, "already_terminal", scan.StateSUCCESS, true)
	if ledgerRows(t, db, keptID) != 1 {
		t.Fatal("already_terminal released the ledger")
	}

	releasedID := uuid.New()
	if err := ledger.RecordSuccessUsage(user, releasedID, domain.ScanUsageKindWallet); err != nil {
		t.Fatal(err)
	}
	code, got = postTransition(t, transitionURL(ts.URL, releasedID), user, failedBody("t-release", user))
	assertHTTPOutcome(t, code, got, "applied", scan.StateFAILED, true)
	if ledgerRows(t, db, releasedID) != 0 {
		t.Fatal("release kept the ledger")
	}

	retryID := uuid.New()
	code, got = postTransition(t, transitionURL(ts.URL, retryID), user, successBody("t-retry", user, transitionAddr))
	assertHTTPOutcome(t, code, got, "retryable_error", scan.StatePENDING, true)
	if _, ok := loadWalletRow(t, db, retryID); ok {
		t.Fatal("retryable keep inserted a row")
	}
}

func TestApplyWalletTransition_StatusAbsentWhenUnread(t *testing.T) {
	db := setupScanAPITestDB(t)
	ts := startTransitionAPI(t, db)
	defer ts.Close()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	user := uuid.New()
	code, got := postTransition(t, transitionURL(ts.URL, uuid.New()), user, runningBody("t-closed", user))
	assertHTTPOutcome(t, code, got, "retryable_error", "", false)
}
