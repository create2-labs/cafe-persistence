package scanapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cafe-persistence/internal/config"
	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/repository"
	"cafe-persistence/internal/scanroutes"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const testServiceToken = "test-persistence-service-token"

func setupScanAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.New().String()+"?mode=memory&cache=shared&_txlock=immediate"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&domain.ScanResultEntity{},
		&domain.TLSScanResultEntity{},
		&domain.ScanUsageEventEntity{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	return db
}

func newTestScanAPIServer(t *testing.T, pending repository.PendingV1ScanRepository, db *gorm.DB) *httptest.Server {
	t.Helper()
	chainCfg := &config.ChainConfig{
		Blockchains: []config.Blockchain{
			{Name: "ethereum", ChainID: 1},
		},
	}
	h := NewHandler(
		pending,
		repository.NewScanResultRepository(db),
		repository.NewTLSScanResultRepository(db),
		repository.NewScanUsageLedgerRepository(db),
		nil,
		chainCfg,
	)
	srv := NewServer("127.0.0.1", "0", testServiceToken, h)
	return httptest.NewServer(srv.HTTPHandler())
}

func authRequest(method, url string, userID uuid.UUID, body []byte) (*http.Request, error) {
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(method, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequest(method, url, nil)
		if err != nil {
			return nil, err
		}
	}
	req.Header.Set(headerAuthorization, "Bearer "+testServiceToken)
	req.Header.Set(headerUserID, userID.String())
	return req, nil
}

func doAuthRequest(t *testing.T, method, url string, userID uuid.UUID, body []byte) *http.Response {
	t.Helper()
	req, err := authRequest(method, url, userID, body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

type memoryPendingRepo struct {
	mu       sync.Mutex
	byID     map[uuid.UUID]*repository.PendingV1ScanRecord
	byWallet map[string]uuid.UUID
}

func newMemoryPendingRepo() *memoryPendingRepo {
	return &memoryPendingRepo{
		byID:     make(map[uuid.UUID]*repository.PendingV1ScanRecord),
		byWallet: make(map[string]uuid.UUID),
	}
}

func walletKey(userID uuid.UUID, address string) string {
	return userID.String() + ":" + strings.ToLower(strings.TrimSpace(address))
}

func (r *memoryPendingRepo) Put(_ context.Context, rec *repository.PendingV1ScanRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *rec
	r.byID[rec.ScanID] = &cp
	return nil
}

func (r *memoryPendingRepo) PutWallet(_ context.Context, rec *repository.PendingV1ScanRecord) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := walletKey(rec.UserID, rec.Address)
	if _, ok := r.byWallet[key]; ok {
		return false, nil
	}
	cp := *rec
	cp.Family = "wallet"
	r.byWallet[key] = rec.ScanID
	r.byID[rec.ScanID] = &cp
	return true, nil
}

func (r *memoryPendingRepo) Get(_ context.Context, scanID uuid.UUID) (*repository.PendingV1ScanRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.byID[scanID]
	if rec == nil {
		return nil, nil
	}
	cp := *rec
	return &cp, nil
}

func (r *memoryPendingRepo) GetWalletByOwnerAddress(_ context.Context, userID uuid.UUID, address string) (*repository.PendingV1ScanRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	scanID, ok := r.byWallet[walletKey(userID, address)]
	if !ok {
		return nil, nil
	}
	rec := r.byID[scanID]
	if rec == nil {
		return &repository.PendingV1ScanRecord{ScanID: scanID, UserID: userID, Family: "wallet", Address: address}, nil
	}
	cp := *rec
	return &cp, nil
}

func (r *memoryPendingRepo) Delete(_ context.Context, scanID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec := r.byID[scanID]; rec != nil && rec.Family == "wallet" {
		delete(r.byWallet, walletKey(rec.UserID, rec.Address))
	}
	delete(r.byID, scanID)
	return nil
}

func (r *memoryPendingRepo) DeleteWalletReservation(_ context.Context, userID uuid.UUID, address string, scanID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := walletKey(userID, address)
	if r.byWallet[key] == scanID {
		delete(r.byWallet, key)
	}
	return nil
}

func TestScanAPI_ReserveWalletPending_Conflict(t *testing.T) {
	db := setupScanAPITestDB(t)
	pending := newMemoryPendingRepo()
	ts := newTestScanAPIServer(t, pending, db)
	defer ts.Close()

	userID := uuid.New()
	addr := "0xabc"
	scanID1 := uuid.New()
	scanID2 := uuid.New()

	body1, _ := json.Marshal(map[string]any{
		"scan_id": scanID1.String(),
		"address": addr,
	})
	resp1 := doAuthRequest(t, http.MethodPost, ts.URL+scanroutes.Join(scanroutes.PendingWallet), userID, body1)
	_ = resp1.Body.Close()
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("first reserve status = %d, want 201", resp1.StatusCode)
	}

	body2, _ := json.Marshal(map[string]any{
		"scan_id": scanID2.String(),
		"address": addr,
	})
	resp2 := doAuthRequest(t, http.MethodPost, ts.URL+scanroutes.Join(scanroutes.PendingWallet), userID, body2)
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("second reserve status = %d, want 409", resp2.StatusCode)
	}
	var errBody map[string]string
	if err := json.NewDecoder(resp2.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode conflict body: %v", err)
	}
	if errBody["error"] != "SCAN_IN_PROGRESS" {
		t.Fatalf("error = %q, want SCAN_IN_PROGRESS", errBody["error"])
	}
}

func TestScanAPI_GetWalletScan_NotFound(t *testing.T) {
	db := setupScanAPITestDB(t)
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	userID := uuid.New()
	scanID := uuid.New()
	url := ts.URL + scanroutes.Join(scanroutes.WalletScanByID)
	url = strings.Replace(url, "{scan_id}", scanID.String(), 1)
	resp := doAuthRequest(t, http.MethodGet, url, userID, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestScanAPI_GetWalletScan_OK(t *testing.T) {
	db := setupScanAPITestDB(t)
	userID := uuid.New()
	scanID := uuid.New()
	row := domain.ScanResultEntity{
		ID: scanID, UserID: userID, Address: "0xabc", Status: scan.StateSUCCESS,
		Type: domain.AccountTypeEOA, Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	url := ts.URL + scanroutes.Join(scanroutes.WalletScanByID)
	url = strings.Replace(url, "{scan_id}", scanID.String(), 1)
	resp := doAuthRequest(t, http.MethodGet, url, userID, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["id"] != scanID.String() {
		t.Fatalf("id = %v", body["id"])
	}
	if body["status"] != scan.StateSUCCESS {
		t.Fatalf("status = %v", body["status"])
	}
}

func TestScanAPI_DeleteWalletScan_OK(t *testing.T) {
	db := setupScanAPITestDB(t)
	userID := uuid.New()
	scanID := uuid.New()
	row := domain.ScanResultEntity{
		ID: scanID, UserID: userID, Address: "0xabc", Status: scan.StateSUCCESS,
		Type: domain.AccountTypeEOA, Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	url := ts.URL + scanroutes.Join(scanroutes.WalletScanByID)
	url = strings.Replace(url, "{scan_id}", scanID.String(), 1)
	resp := doAuthRequest(t, http.MethodDelete, url, userID, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}

func TestScanAPI_GetScanLedgerUsage_OK(t *testing.T) {
	db := setupScanAPITestDB(t)
	userID := uuid.New()
	scanID := uuid.New()
	if err := db.Create(&domain.ScanUsageEventEntity{
		ID: uuid.New(), UserID: userID, ScanID: scanID, ScanKind: domain.ScanUsageKindWallet, ConsumedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	if err := db.Create(&domain.ScanResultEntity{
		ID: uuid.New(), UserID: userID, Address: "0x1", Status: scan.StateRUNNING,
		Type: domain.AccountTypeEOA, Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1,
	}).Error; err != nil {
		t.Fatalf("seed in-flight: %v", err)
	}
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	url := ts.URL + scanroutes.Join(scanroutes.LedgerUsage) + "?kind=wallet"
	resp := doAuthRequest(t, http.MethodGet, url, userID, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success_count"].(float64) != 1 {
		t.Fatalf("success_count = %v", body["success_count"])
	}
	if body["in_flight_count"].(float64) != 1 {
		t.Fatalf("in_flight_count = %v", body["in_flight_count"])
	}
}

func TestScanAPI_ServiceAuthRequired(t *testing.T) {
	db := setupScanAPITestDB(t)
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+scanroutes.Join(scanroutes.LedgerUsage)+"?kind=wallet", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set(headerUserID, uuid.New().String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestScanAPI_MissingUserIDHeader(t *testing.T) {
	db := setupScanAPITestDB(t)
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+scanroutes.Join(scanroutes.LedgerUsage)+"?kind=wallet", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set(headerAuthorization, "Bearer "+testServiceToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestScanAPI_ListWalletScans_LatestRequiresAddress(t *testing.T) {
	db := setupScanAPITestDB(t)
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	url := ts.URL + scanroutes.Join(scanroutes.WalletScans) + "?latest=true"
	resp := doAuthRequest(t, http.MethodGet, url, uuid.New(), nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestScanAPI_GetTLSScan_DefaultCatalog(t *testing.T) {
	db := setupScanAPITestDB(t)
	scanID := uuid.New()
	row := domain.TLSScanResultEntity{
		ID: scanID, URL: "https://example.com", Host: "example.com", Port: 443,
		ProtocolVersion: "TLS1.3", NISTLevel: domain.NISTLevel1, RiskScore: 0, PQCRisk: "low",
		Default: true, Status: scan.StateSUCCESS,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	url := ts.URL + scanroutes.Join(scanroutes.TLSScanByID)
	url = strings.Replace(url, "{scan_id}", scanID.String(), 1)
	resp := doAuthRequest(t, http.MethodGet, url, uuid.New(), nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestScanAPI_PutTLSPending_Created(t *testing.T) {
	db := setupScanAPITestDB(t)
	pending := newMemoryPendingRepo()
	ts := newTestScanAPIServer(t, pending, db)
	defer ts.Close()

	userID := uuid.New()
	scanID := uuid.New()
	body, _ := json.Marshal(map[string]any{
		"scan_id":  scanID.String(),
		"endpoint": "https://example.com",
	})
	resp := doAuthRequest(t, http.MethodPost, ts.URL+scanroutes.Join(scanroutes.PendingTLS), userID, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
}
