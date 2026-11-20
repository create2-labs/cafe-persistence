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
		&domain.ScanRelatedAddressEntity{},
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
	return httptest.NewServer(authedRoutes(h))
}

// authedRoutes serves the scan routes behind the service-token middleware,
// the same handler chain the internal HTTP server uses.
func authedRoutes(h *Handler) http.Handler {
	mux := http.NewServeMux()
	RegisterRoutes(mux, h)
	return ServiceAuthMiddleware(testServiceToken, mux)
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
	resp1 := doAuthRequest(t, http.MethodPost, ts.URL+scanroutes.V1Base+scanroutes.PendingWallet, userID, body1)
	_ = resp1.Body.Close()
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("first reserve status = %d, want 201", resp1.StatusCode)
	}

	body2, _ := json.Marshal(map[string]any{
		"scan_id": scanID2.String(),
		"address": addr,
	})
	resp2 := doAuthRequest(t, http.MethodPost, ts.URL+scanroutes.V1Base+scanroutes.PendingWallet, userID, body2)
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
	url := ts.URL + scanroutes.V1Base + scanroutes.WalletScanByID
	url = strings.Replace(url, "{scan_id}", scanID.String(), 1)
	resp := doAuthRequest(t, http.MethodGet, url, userID, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// getWalletScanRow reads the by-id route and decodes a 200 body.
func getWalletScanRow(t *testing.T, ts *httptest.Server, userID, scanID uuid.UUID) (int, map[string]any) {
	t.Helper()
	url := strings.Replace(ts.URL+scanroutes.V1Base+scanroutes.WalletScanByID, "{scan_id}", scanID.String(), 1)
	resp := doAuthRequest(t, http.MethodGet, url, userID, nil)
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.StatusCode, body
}

func seedWalletRow(t *testing.T, db *gorm.DB, row domain.ScanResultEntity) {
	t.Helper()
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func assertRowKeys(t *testing.T, body map[string]any) {
	t.Helper()
	for _, k := range []string{"type", "is_eoa", "is_erc4337", "networks", "delegations", "connections"} {
		if _, ok := body[k]; ok {
			t.Errorf("removed key %q present", k)
		}
	}
	for _, k := range []string{"id", "user_id", "address", "account_type", "chains", "unobserved_chain_ids", "anomalies",
		"key_exposed", "algorithm", "nist_level", "risk_score", "status", "created_at", "updated_at"} {
		if _, ok := body[k]; !ok {
			t.Errorf("key %q missing", k)
		}
	}
}

func TestScanAPI_GetWalletScan_OK(t *testing.T) {
	db := setupScanAPITestDB(t)
	userID, scanID := uuid.New(), uuid.New()
	seedWalletRow(t, db, domain.ScanResultEntity{
		ID: scanID, UserID: userID, Address: "0xabc", Status: scan.StateSUCCESS,
		Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1,
		AccountType: "eoa", Chains: "[]", UnobservedChainIDs: "[]", Anomalies: "[]",
	})
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	code, body := getWalletScanRow(t, ts, userID, scanID)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if body["id"] != scanID.String() || body["status"] != scan.StateSUCCESS || body["account_type"] != "eoa" {
		t.Fatalf("body = %#v", body)
	}
	assertRowKeys(t, body)
}

func TestScanAPI_GetWalletScan_PublicKeyRecovery(t *testing.T) {
	db := setupScanAPITestDB(t)
	userID, scanID := uuid.New(), uuid.New()
	seedWalletRow(t, db, domain.ScanResultEntity{
		ID: scanID, UserID: userID, Address: "0xabc", Status: scan.StateSUCCESS,
		Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1,
		PublicKeyRecovery: domain.PublicKeyRecoveryNotRequired, AccountType: "eoa",
	})
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	code, body := getWalletScanRow(t, ts, userID, scanID)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if body["public_key_recovery"] != string(domain.PublicKeyRecoveryNotRequired) {
		t.Fatalf("public_key_recovery = %#v", body["public_key_recovery"])
	}
	for _, k := range []string{"first_seen", "last_seen"} {
		if _, ok := body[k]; ok {
			t.Fatalf("%s = %#v", k, body[k])
		}
	}
	assertRowKeys(t, body)
}

// CA1.1 to CA1.4: a row with no SUCCESS yet has account_type ” and no replaced key.
func TestScanAPI_GetWalletScan_RunningRowHasEmptyQualification(t *testing.T) {
	db := setupScanAPITestDB(t)
	userID, scanID := uuid.New(), uuid.New()
	seedWalletRow(t, db, domain.ScanResultEntity{ID: scanID, UserID: userID, Address: "0xabc", Status: scan.StateRUNNING})
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	code, body := getWalletScanRow(t, ts, userID, scanID)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if body["account_type"] != "" {
		t.Fatalf("account_type = %#v, want empty", body["account_type"])
	}
	assertRowKeys(t, body)
	// CA1.5: observed_at is absent while NULL.
	if _, ok := body["observed_at"]; ok {
		t.Fatalf("observed_at = %#v, want absent", body["observed_at"])
	}
	// The empty column gives an empty array for each of the three lists.
	for _, k := range []string{"chains", "unobserved_chain_ids", "anomalies"} {
		list, ok := body[k].([]any)
		if !ok || len(list) != 0 {
			t.Errorf("%s = %#v, want []", k, body[k])
		}
	}
}

// CA2.1, CA2.6, CA1.5: the stored qualification is returned as JSON arrays, never as strings.
func TestScanAPI_GetWalletScan_QualificationArrays(t *testing.T) {
	db := setupScanAPITestDB(t)
	userID, scanID := uuid.New(), uuid.New()
	seedWalletRow(t, db, domain.ScanResultEntity{
		ID: scanID, UserID: userID, Address: "0xabc", Status: scan.StateSUCCESS, KeyExposed: true,
		AccountType:        "eoa",
		Chains:             `[{"chain_id":1,"qualification":"eoa","nonce":3,"key_exposed":true},{"chain_id":10,"qualification":"eoa","key_exposed":true}]`,
		UnobservedChainIDs: `[137]`,
		Anomalies:          `["contract_with_eoa_activity"]`, // CA1.7
		ObservedAt:         func() *time.Time { v := time.Date(2026, 10, 23, 10, 0, 0, 123456789, time.UTC); return &v }(),
	})
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	code, body := getWalletScanRow(t, ts, userID, scanID)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	chains, ok := body["chains"].([]any)
	if !ok || len(chains) != 2 {
		t.Fatalf("chains = %#v, want 2 entries as an array", body["chains"])
	}
	designator := chains[1].(map[string]any)
	if designator["key_exposed"] != true {
		t.Fatalf("designator chain = %#v, want key_exposed true", designator)
	}
	if _, ok := designator["nonce"]; ok {
		t.Fatalf("designator chain = %#v, want no nonce", designator)
	}
	if got, ok := body["unobserved_chain_ids"].([]any); !ok || len(got) != 1 || got[0] != float64(137) {
		t.Fatalf("unobserved_chain_ids = %#v", body["unobserved_chain_ids"])
	}
	if got, ok := body["anomalies"].([]any); !ok || len(got) != 1 || got[0] != "contract_with_eoa_activity" {
		t.Fatalf("anomalies = %#v", body["anomalies"])
	}
	if body["observed_at"] != "2026-10-23T10:00:00.123456789Z" {
		t.Fatalf("observed_at = %#v, want RFC 3339 nanoseconds UTC", body["observed_at"])
	}
}

// An unreadable stored list is a defect: 503 on both routes, while a valid row answers 200.
func TestScanAPI_WalletScan_UnreadableChainsIs503(t *testing.T) {
	db := setupScanAPITestDB(t)
	goodUser, badUser := uuid.New(), uuid.New()
	goodID, badID := uuid.New(), uuid.New()
	seedWalletRow(t, db, domain.ScanResultEntity{ID: goodID, UserID: goodUser, Address: "0xabc", Status: scan.StateSUCCESS, AccountType: "eoa", Chains: "[]"})
	seedWalletRow(t, db, domain.ScanResultEntity{ID: badID, UserID: badUser, Address: "0xdef", Status: scan.StateSUCCESS, AccountType: "eoa", Chains: "not json"})
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	if code, _ := getWalletScanRow(t, ts, goodUser, goodID); code != http.StatusOK {
		t.Fatalf("valid by-id status = %d, want 200", code)
	}
	code, body := getWalletScanRow(t, ts, badUser, badID)
	if code != http.StatusServiceUnavailable || body["error"] != "service_unavailable" {
		t.Fatalf("bad by-id = %d %#v, want 503 service_unavailable", code, body)
	}

	list := func(user uuid.UUID) (int, map[string]any) {
		resp := doAuthRequest(t, http.MethodGet, ts.URL+scanroutes.V1Base+scanroutes.WalletScans, user, nil)
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp.StatusCode, out
	}
	if code, _ := list(goodUser); code != http.StatusOK {
		t.Fatalf("valid list status = %d, want 200", code)
	}
	code, body = list(badUser)
	if code != http.StatusServiceUnavailable || body["error"] != "service_unavailable" {
		t.Fatalf("bad list = %d %#v, want 503 service_unavailable", code, body)
	}
}

func TestScanAPI_DeleteWalletScan_OK(t *testing.T) {
	db := setupScanAPITestDB(t)
	userID := uuid.New()
	scanID := uuid.New()
	row := domain.ScanResultEntity{
		ID: scanID, UserID: userID, Address: "0xabc", Status: scan.StateSUCCESS,
		Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	url := ts.URL + scanroutes.V1Base + scanroutes.WalletScanByID
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
		Algorithm: domain.AlgorithmECDSAsecp256k1, NISTLevel: domain.NISTLevel1,
	}).Error; err != nil {
		t.Fatalf("seed in-flight: %v", err)
	}
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	url := ts.URL + scanroutes.V1Base + scanroutes.LedgerUsage + "?kind=wallet"
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

	req, err := http.NewRequest(http.MethodGet, ts.URL+scanroutes.V1Base+scanroutes.LedgerUsage+"?kind=wallet", nil)
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

	req, err := http.NewRequest(http.MethodGet, ts.URL+scanroutes.V1Base+scanroutes.LedgerUsage+"?kind=wallet", nil)
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

	url := ts.URL + scanroutes.V1Base + scanroutes.WalletScans + "?latest=true"
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

	url := ts.URL + scanroutes.V1Base + scanroutes.TLSScanByID
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
	resp := doAuthRequest(t, http.MethodPost, ts.URL+scanroutes.V1Base+scanroutes.PendingTLS, userID, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
}

// CA1.3: the by-id route returns the related addresses, ordered by chain_id, role, address;
// the list route does not; an empty set is []; a soft-deleted row is excluded.
func TestScanAPI_GetWalletScan_RelatedAddresses(t *testing.T) {
	db := setupScanAPITestDB(t)
	userID, otherScan, enrichmentID := uuid.New(), uuid.New(), uuid.New()
	scanID, emptyID := uuid.New(), uuid.New()
	seedWalletRow(t, db, domain.ScanResultEntity{ID: scanID, UserID: userID, Address: "0xabc", Status: scan.StateSUCCESS, AccountType: "contract"})
	seedWalletRow(t, db, domain.ScanResultEntity{ID: emptyID, UserID: userID, Address: "0xdef", Status: scan.StateSUCCESS, AccountType: "eoa"})
	rows := []domain.ScanRelatedAddressEntity{
		{ScanID: scanID, ChainID: 10, Role: "delegation_target", Address: "0xbb"},
		{ScanID: scanID, ChainID: 1, Role: "delegation_target", Address: "0xcc"},
		{ScanID: scanID, ChainID: 1, Role: "delegation_target", Address: "0xaa"},
		{ScanID: scanID, ChainID: 1, Role: "deployer", Address: "0xdd", CreationTxHash: "0xtx", EnrichmentID: &enrichmentID},
		{ScanID: scanID, ChainID: 5, Role: "delegation_target", Address: "0xee"},
		{ScanID: otherScan, ChainID: 1, Role: "deployer", Address: "0xff"},
	}
	for _, r := range rows {
		if err := db.Create(&r).Error; err != nil {
			t.Fatalf("seed related: %v", err)
		}
	}
	// A soft-deleted row is never returned.
	if err := db.Where("address = ?", "0xee").Delete(&domain.ScanRelatedAddressEntity{}).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	code, body := getWalletScanRow(t, ts, userID, scanID)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	list, ok := body["related_addresses"].([]any)
	if !ok {
		t.Fatalf("related_addresses = %#v, want array", body["related_addresses"])
	}
	var got []string
	for _, it := range list {
		m := it.(map[string]any)
		got = append(got, m["role"].(string)+":"+m["address"].(string))
	}
	want := []string{"delegation_target:0xaa", "delegation_target:0xcc", "deployer:0xdd", "delegation_target:0xbb"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
	// Optional fields: absent when empty or NULL, present otherwise.
	first, deployer := list[0].(map[string]any), list[2].(map[string]any)
	for _, k := range []string{"creation_tx_hash", "enrichment_id"} {
		if _, ok := first[k]; ok {
			t.Errorf("delegation_target %s = %#v, want absent", k, first[k])
		}
	}
	if first["chain_id"] != float64(1) {
		t.Errorf("chain_id = %#v, want 1", first["chain_id"])
	}
	if deployer["creation_tx_hash"] != "0xtx" || deployer["enrichment_id"] != enrichmentID.String() {
		t.Errorf("deployer = %#v", deployer)
	}

	// No row: [].
	_, empty := getWalletScanRow(t, ts, userID, emptyID)
	if l, ok := empty["related_addresses"].([]any); !ok || len(l) != 0 {
		t.Errorf("related_addresses = %#v, want []", empty["related_addresses"])
	}

	// The list route returns the row keys only.
	resp := doAuthRequest(t, http.MethodGet, ts.URL+scanroutes.V1Base+scanroutes.WalletScans, userID, nil)
	defer func() { _ = resp.Body.Close() }()
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("list items = %d, want 2", len(page.Items))
	}
	for _, it := range page.Items {
		if _, ok := it["related_addresses"]; ok {
			t.Errorf("list item has related_addresses")
		}
	}
}

// CA1.3: a contract result stores and returns its not_deployed designator row (OP-3.1 (a)).
func TestScanAPI_GetWalletScan_RelatedAddresses_ContractDesignator(t *testing.T) {
	db := setupScanAPITestDB(t)
	userID, scanID := uuid.New(), uuid.New()
	seedWalletRow(t, db, domain.ScanResultEntity{
		ID: scanID, UserID: userID, Address: "0xabc", Status: scan.StateSUCCESS, AccountType: "contract",
		Chains: `[{"chain_id":1,"qualification":"contract"},{"chain_id":10,"qualification":"not_deployed"}]`,
	})
	if err := db.Create(&domain.ScanRelatedAddressEntity{ScanID: scanID, ChainID: 10, Role: "delegation_target", Address: "0xd0"}).Error; err != nil {
		t.Fatalf("seed related: %v", err)
	}
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	code, body := getWalletScanRow(t, ts, userID, scanID)
	list, _ := body["related_addresses"].([]any)
	if code != http.StatusOK || len(list) != 1 {
		t.Fatalf("status = %d, related_addresses = %#v, want 1 row", code, body["related_addresses"])
	}
	if m := list[0].(map[string]any); m["chain_id"] != float64(10) || m["address"] != "0xd0" {
		t.Errorf("row = %#v", m)
	}
}
