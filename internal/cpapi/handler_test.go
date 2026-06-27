package cpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cafe-persistence/internal/cpddl"
	"cafe-persistence/internal/cpstore"
	"cafe-persistence/internal/cproutes"
	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/scanapi"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const testServiceToken = "test-persistence-service-token"

var (
	testUserID = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	testScanID = uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	testWallet = "0x742d35cc6634c0532925a3b844bc454e4438f44e"
)

func setupCPAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.New().String()+"?mode=memory&cache=shared&_txlock=immediate"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&domain.CryptoPolicyDraftEntity{},
		&domain.CryptoPolicyEntity{},
		&domain.DraftPersistStateEntity{},
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

func newTestCPAPIServer(t *testing.T, db *gorm.DB) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	cpapiHandler := NewHandler(cpstore.NewPostgresStore(db))
	RegisterRoutes(mux, cpapiHandler)
	srv := scanapi.NewServerFromMux("127.0.0.1", "0", testServiceToken, mux)
	return httptest.NewServer(srv.HTTPHandler())
}

func cpURL(base, rel string) string {
	return base + cproutes.V1Base + rel
}

func cpURLWithDraft(base, draftID string) string {
	rel := strings.ReplaceAll(cproutes.DraftByID, "{draft_id}", draftID)
	return base + cproutes.V1Base + rel
}

func cpURLWithDraftPersist(base, draftID string) string {
	rel := strings.ReplaceAll(cproutes.DraftPersist, "{draft_id}", draftID)
	return base + cproutes.V1Base + rel
}

func cpURLWithPolicy(base, policyID string) string {
	rel := strings.ReplaceAll(cproutes.PolicyByID, "{policy_id}", policyID)
	return base + cproutes.V1Base + rel
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

func decodeJSONBody(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

func TestCpAPI_DraftPersistGetPolicyLifecycle(t *testing.T) {
	db := setupCPAPITestDB(t)
	ts := newTestCPAPIServer(t, db)
	defer ts.Close()

	draftID := uuid.New()
	upsertBody, _ := json.Marshal(map[string]any{
		"scan_id": testScanID.String(),
		"payload": map[string]any{
			"policy_context": map[string]any{
				"wallet_address": testWallet,
				"wallet_type":    "eoa",
			},
			"mode": "strict",
		},
	})
	resp := doAuthRequest(t, http.MethodPut, cpURLWithDraft(ts.URL, draftID.String()), testUserID, upsertBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upsert draft: status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	verifiedAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	persistBody, _ := json.Marshal(map[string]any{
		"wallet_address":             testWallet,
		"chain_id":                   1,
		"scan_id":                    testScanID.String(),
		"wallet_control_verified_at": verifiedAt.Format(time.RFC3339Nano),
	})
	resp = doAuthRequest(t, http.MethodPost, cpURLWithDraftPersist(ts.URL, draftID.String()), testUserID, persistBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("persist draft: status = %d", resp.StatusCode)
	}
	var persistResp map[string]any
	decodeJSONBody(t, resp, &persistResp)
	policyID, _ := persistResp["policy_id"].(string)
	if policyID == "" {
		t.Fatalf("missing policy_id: %#v", persistResp)
	}

	resp = doAuthRequest(t, http.MethodGet, cpURLWithPolicy(ts.URL, policyID), testUserID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get policy: status = %d", resp.StatusCode)
	}
	var policy map[string]any
	decodeJSONBody(t, resp, &policy)
	if policy["status"] != "persisted" {
		t.Fatalf("policy status = %#v", policy["status"])
	}
	if _, ok := policy["signature"]; ok {
		t.Fatal("policy payload must not expose signature at HTTP layer")
	}
}

func TestCpAPI_PersistReplayReturns409(t *testing.T) {
	db := setupCPAPITestDB(t)
	ts := newTestCPAPIServer(t, db)
	defer ts.Close()

	draftID := uuid.New()
	upsertBody, _ := json.Marshal(map[string]any{
		"scan_id": testScanID.String(),
		"payload": map[string]any{
			"policy_context": map[string]any{"wallet_address": testWallet, "wallet_type": "eoa"},
		},
	})
	resp := doAuthRequest(t, http.MethodPut, cpURLWithDraft(ts.URL, draftID.String()), testUserID, upsertBody)
	_ = resp.Body.Close()

	persistBody, _ := json.Marshal(map[string]any{
		"wallet_address":             testWallet,
		"chain_id":                   1,
		"scan_id":                    testScanID.String(),
		"wallet_control_verified_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	resp = doAuthRequest(t, http.MethodPost, cpURLWithDraftPersist(ts.URL, draftID.String()), testUserID, persistBody)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first persist: status = %d", resp.StatusCode)
	}

	resp = doAuthRequest(t, http.MethodPost, cpURLWithDraftPersist(ts.URL, draftID.String()), testUserID, persistBody)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("replay persist: status = %d, want 409", resp.StatusCode)
	}
	var errBody map[string]string
	decodeJSONBody(t, resp, &errBody)
	if errBody["error"] != "DRAFT_ALREADY_PERSISTED" {
		t.Fatalf("error = %q", errBody["error"])
	}
}

func TestCpAPI_CountPoliciesByScan(t *testing.T) {
	db := setupCPAPITestDB(t)
	ts := newTestCPAPIServer(t, db)
	defer ts.Close()

	draftID := uuid.New()
	upsertBody, _ := json.Marshal(map[string]any{
		"scan_id": testScanID.String(),
		"payload": map[string]any{
			"policy_context": map[string]any{"wallet_address": testWallet, "wallet_type": "eoa"},
		},
	})
	resp := doAuthRequest(t, http.MethodPut, cpURLWithDraft(ts.URL, draftID.String()), testUserID, upsertBody)
	_ = resp.Body.Close()

	countURL := cpURL(ts.URL, cproutes.ReferenceScan) + "?scan_id=" + testScanID.String()
	resp = doAuthRequest(t, http.MethodGet, countURL, testUserID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("count before persist: status = %d", resp.StatusCode)
	}
	var before map[string]any
	decodeJSONBody(t, resp, &before)
	if before["referenced"] != false || before["count"].(float64) != 0 {
		t.Fatalf("before persist: %#v", before)
	}

	persistBody, _ := json.Marshal(map[string]any{
		"wallet_address":             testWallet,
		"chain_id":                   1,
		"scan_id":                    testScanID.String(),
		"wallet_control_verified_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	resp = doAuthRequest(t, http.MethodPost, cpURLWithDraftPersist(ts.URL, draftID.String()), testUserID, persistBody)
	_ = resp.Body.Close()

	resp = doAuthRequest(t, http.MethodGet, countURL, testUserID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("count after persist: status = %d", resp.StatusCode)
	}
	var after map[string]any
	decodeJSONBody(t, resp, &after)
	if after["referenced"] != true || after["count"].(float64) != 1 {
		t.Fatalf("after persist: %#v", after)
	}
}

func TestCpAPI_CountPoliciesByWallet(t *testing.T) {
	db := setupCPAPITestDB(t)
	ts := newTestCPAPIServer(t, db)
	defer ts.Close()

	draftID := uuid.New()
	upsertBody, _ := json.Marshal(map[string]any{
		"payload": map[string]any{
			"policy_context": map[string]any{"wallet_address": testWallet, "wallet_type": "eoa"},
		},
	})
	resp := doAuthRequest(t, http.MethodPut, cpURLWithDraft(ts.URL, draftID.String()), testUserID, upsertBody)
	_ = resp.Body.Close()

	countURL := cpURL(ts.URL, cproutes.ReferenceWallet) + "?wallet_address=" + testWallet
	resp = doAuthRequest(t, http.MethodGet, countURL, testUserID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("count wallet: status = %d", resp.StatusCode)
	}
	var body map[string]any
	decodeJSONBody(t, resp, &body)
	if body["exists"] != true || body["draft_count"].(float64) != 1 {
		t.Fatalf("wallet refs: %#v", body)
	}
	if body["platform_draft_id"] != draftID.String() {
		t.Fatalf("platform_draft_id = %v, want %s", body["platform_draft_id"], draftID.String())
	}
}

func TestCpAPI_ListPoliciesByScan(t *testing.T) {
	db := setupCPAPITestDB(t)
	ts := newTestCPAPIServer(t, db)
	defer ts.Close()

	draftID := uuid.New()
	upsertBody, _ := json.Marshal(map[string]any{
		"scan_id": testScanID.String(),
		"payload": map[string]any{
			"policy_context": map[string]any{"wallet_address": testWallet, "wallet_type": "eoa"},
		},
	})
	resp := doAuthRequest(t, http.MethodPut, cpURLWithDraft(ts.URL, draftID.String()), testUserID, upsertBody)
	_ = resp.Body.Close()

	persistBody, _ := json.Marshal(map[string]any{
		"wallet_address":             testWallet,
		"chain_id":                   1,
		"scan_id":                    testScanID.String(),
		"wallet_control_verified_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	resp = doAuthRequest(t, http.MethodPost, cpURLWithDraftPersist(ts.URL, draftID.String()), testUserID, persistBody)
	_ = resp.Body.Close()

	listURL := cpURL(ts.URL, cproutes.Policies) + "?scan_id=" + testScanID.String()
	resp = doAuthRequest(t, http.MethodGet, listURL, testUserID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list policies: status = %d", resp.StatusCode)
	}
	var list map[string]any
	decodeJSONBody(t, resp, &list)
	items, _ := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %#v", list["items"])
	}
}

func TestCpAPI_DeleteDraftAndPolicy(t *testing.T) {
	db := setupCPAPITestDB(t)
	ts := newTestCPAPIServer(t, db)
	defer ts.Close()

	draftID := uuid.New()
	upsertBody, _ := json.Marshal(map[string]any{
		"scan_id": testScanID.String(),
		"payload": map[string]any{
			"policy_context": map[string]any{"wallet_address": testWallet, "wallet_type": "eoa"},
		},
	})
	resp := doAuthRequest(t, http.MethodPut, cpURLWithDraft(ts.URL, draftID.String()), testUserID, upsertBody)
	_ = resp.Body.Close()

	resp = doAuthRequest(t, http.MethodDelete, cpURLWithDraft(ts.URL, draftID.String()), testUserID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete draft: status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = doAuthRequest(t, http.MethodGet, cpURLWithDraft(ts.URL, draftID.String()), testUserID, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get deleted draft: status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Persist a policy then delete it.
	draftID = uuid.New()
	resp = doAuthRequest(t, http.MethodPut, cpURLWithDraft(ts.URL, draftID.String()), testUserID, upsertBody)
	_ = resp.Body.Close()
	persistBody, _ := json.Marshal(map[string]any{
		"wallet_address":             testWallet,
		"chain_id":                   1,
		"scan_id":                    testScanID.String(),
		"wallet_control_verified_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	resp = doAuthRequest(t, http.MethodPost, cpURLWithDraftPersist(ts.URL, draftID.String()), testUserID, persistBody)
	var persistResp map[string]any
	decodeJSONBody(t, resp, &persistResp)
	policyID := persistResp["policy_id"].(string)

	resp = doAuthRequest(t, http.MethodDelete, cpURLWithPolicy(ts.URL, policyID), testUserID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete policy: status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestCpAPI_RequiresServiceAuth(t *testing.T) {
	db := setupCPAPITestDB(t)
	ts := newTestCPAPIServer(t, db)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, cpURL(ts.URL, cproutes.Policies)+"?scan_id="+testScanID.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(headerUserID, testUserID.String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestCpAPI_RequiresUserHeader(t *testing.T) {
	db := setupCPAPITestDB(t)
	ts := newTestCPAPIServer(t, db)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, cpURL(ts.URL, cproutes.ReferenceScan)+"?scan_id="+testScanID.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(headerAuthorization, "Bearer "+testServiceToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCpAPI_NoPublicAPIPrefix(t *testing.T) {
	for _, route := range []string{
		cproutes.DraftByID,
		cproutes.DraftPersist,
		cproutes.PolicyByID,
		cproutes.Policies,
		cproutes.ReferenceWallet,
		cproutes.ReferenceScan,
	} {
		if strings.HasPrefix(route, "/api/") {
			t.Fatalf("route %q must not use public prefix", route)
		}
	}
}

func TestCpDDL_MigrateCompatibleWithHandlerDB(t *testing.T) {
	db := setupCPAPITestDB(t)
	if err := cpddl.MigrateCPSchema(db); err != nil {
		t.Fatalf("MigrateCPSchema on sqlite test db: %v", err)
	}
}
