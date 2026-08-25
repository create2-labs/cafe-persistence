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
	testSHA    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func setupCPAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.New().String()+"?mode=memory&cache=shared&_txlock=immediate"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := cpddl.MigrateCPSchema(db); err != nil {
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

func createPolicyBody(scanID uuid.UUID, wallet, sha string) []byte {
	verifiedAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	issued := verifiedAt.Add(-time.Minute)
	expires := verifiedAt.Add(9 * time.Minute)
	body, _ := json.Marshal(map[string]any{
		"scan_id":                     scanID.String(),
		"wallet_address":              wallet,
		"chain_id":                    1,
		"payload":                     map[string]any{"mode": "strict", "signature": "raw"},
		"payload_sha256":              sha,
		"signed_message_hash":         "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"wallet_control_method":       "eoa_signature",
		"wallet_control_verified_at":  verifiedAt.Format(time.RFC3339Nano),
		"challenge_issued_at":         issued.Format(time.RFC3339Nano),
		"challenge_expires_at":        expires.Format(time.RFC3339Nano),
	})
	return body
}

func TestCpAPI_CreateGetPolicyLifecycle(t *testing.T) {
	db := setupCPAPITestDB(t)
	ts := newTestCPAPIServer(t, db)
	defer ts.Close()

	resp := doAuthRequest(t, http.MethodPost, cpURL(ts.URL, cproutes.Policies), testUserID, createPolicyBody(testScanID, testWallet, testSHA))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create policy: status = %d", resp.StatusCode)
	}
	var createResp map[string]any
	decodeJSONBody(t, resp, &createResp)
	policyID, _ := createResp["policy_id"].(string)
	if policyID == "" || createResp["payload_sha256"] != testSHA {
		t.Fatalf("unexpected create response: %#v", createResp)
	}

	resp = doAuthRequest(t, http.MethodGet, cpURLWithPolicy(ts.URL, policyID), testUserID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get policy: status = %d", resp.StatusCode)
	}
	var policy map[string]any
	decodeJSONBody(t, resp, &policy)
	if policy["status"] != "persisted" || policy["payload_sha256"] != testSHA {
		t.Fatalf("policy = %#v", policy)
	}
	payload, _ := policy["payload"].(map[string]any)
	if _, ok := payload["signature"]; ok {
		t.Fatal("policy payload must not expose signature at HTTP layer")
	}
	if _, ok := policy["draft_id"]; ok {
		t.Fatal("draft_id must not appear on policy rows")
	}
}

func TestCpAPI_CreateDuplicateReturns409(t *testing.T) {
	db := setupCPAPITestDB(t)
	ts := newTestCPAPIServer(t, db)
	defer ts.Close()

	body := createPolicyBody(testScanID, testWallet, testSHA)
	resp := doAuthRequest(t, http.MethodPost, cpURL(ts.URL, cproutes.Policies), testUserID, body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first create: status = %d", resp.StatusCode)
	}

	body2 := createPolicyBody(uuid.New(), testWallet, "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	resp = doAuthRequest(t, http.MethodPost, cpURL(ts.URL, cproutes.Policies), testUserID, body2)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate create: status = %d, want 409", resp.StatusCode)
	}
	var errBody map[string]string
	decodeJSONBody(t, resp, &errBody)
	if errBody["error"] != "POLICY_ALREADY_EXISTS" {
		t.Fatalf("error = %q", errBody["error"])
	}
}

func TestCpAPI_CountPoliciesByScan(t *testing.T) {
	db := setupCPAPITestDB(t)
	ts := newTestCPAPIServer(t, db)
	defer ts.Close()

	countURL := cpURL(ts.URL, cproutes.ReferenceScan) + "?scan_id=" + testScanID.String()
	resp := doAuthRequest(t, http.MethodGet, countURL, testUserID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("count before create: status = %d", resp.StatusCode)
	}
	var before map[string]any
	decodeJSONBody(t, resp, &before)
	if before["referenced"] != false || before["count"].(float64) != 0 {
		t.Fatalf("before create: %#v", before)
	}

	resp = doAuthRequest(t, http.MethodPost, cpURL(ts.URL, cproutes.Policies), testUserID, createPolicyBody(testScanID, testWallet, testSHA))
	_ = resp.Body.Close()

	resp = doAuthRequest(t, http.MethodGet, countURL, testUserID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("count after create: status = %d", resp.StatusCode)
	}
	var after map[string]any
	decodeJSONBody(t, resp, &after)
	if after["referenced"] != true || after["count"].(float64) != 1 {
		t.Fatalf("after create: %#v", after)
	}
}

func TestCpAPI_CountPoliciesByWallet(t *testing.T) {
	db := setupCPAPITestDB(t)
	ts := newTestCPAPIServer(t, db)
	defer ts.Close()

	countURL := cpURL(ts.URL, cproutes.ReferenceWallet) + "?wallet_address=" + testWallet
	resp := doAuthRequest(t, http.MethodGet, countURL, testUserID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("count wallet empty: status = %d", resp.StatusCode)
	}
	var empty map[string]any
	decodeJSONBody(t, resp, &empty)
	if empty["exists"] != false || empty["policy_count"].(float64) != 0 {
		t.Fatalf("empty wallet refs: %#v", empty)
	}
	if _, ok := empty["draft_count"]; ok {
		t.Fatal("draft_count must not be returned after RD-P3")
	}

	resp = doAuthRequest(t, http.MethodPost, cpURL(ts.URL, cproutes.Policies), testUserID, createPolicyBody(testScanID, testWallet, testSHA))
	_ = resp.Body.Close()

	resp = doAuthRequest(t, http.MethodGet, countURL, testUserID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("count wallet: status = %d", resp.StatusCode)
	}
	var body map[string]any
	decodeJSONBody(t, resp, &body)
	if body["exists"] != true || body["policy_count"].(float64) != 1 {
		t.Fatalf("wallet refs: %#v", body)
	}
}

func TestCpAPI_ListPoliciesByScan(t *testing.T) {
	db := setupCPAPITestDB(t)
	ts := newTestCPAPIServer(t, db)
	defer ts.Close()

	resp := doAuthRequest(t, http.MethodPost, cpURL(ts.URL, cproutes.Policies), testUserID, createPolicyBody(testScanID, testWallet, testSHA))
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

func TestCpAPI_DeletePolicy(t *testing.T) {
	db := setupCPAPITestDB(t)
	ts := newTestCPAPIServer(t, db)
	defer ts.Close()

	resp := doAuthRequest(t, http.MethodPost, cpURL(ts.URL, cproutes.Policies), testUserID, createPolicyBody(testScanID, testWallet, testSHA))
	var createResp map[string]any
	decodeJSONBody(t, resp, &createResp)
	policyID := createResp["policy_id"].(string)

	resp = doAuthRequest(t, http.MethodDelete, cpURLWithPolicy(ts.URL, policyID), testUserID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete policy: status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Soft-delete frees W1 unique slot.
	resp = doAuthRequest(t, http.MethodPost, cpURL(ts.URL, cproutes.Policies), testUserID, createPolicyBody(uuid.New(), testWallet, "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("recreate after delete: status = %d", resp.StatusCode)
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
	_ = domain.CryptoPolicyStatusPersisted
}
