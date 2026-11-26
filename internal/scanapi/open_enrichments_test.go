package scanapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"testing"
	"time"

	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/scanroutes"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var openEnrichmentsBase = time.Date(2026, 1, 1, 10, 0, 0, 123456789, time.UTC)

const openEnrichmentsBound = "2026-01-02T00:00:00Z"

func seedEnrichment(t *testing.T, db *gorm.DB, status string, created time.Time, chains string) domain.ScanEnrichmentEntity {
	t.Helper()
	e := domain.ScanEnrichmentEntity{
		EnrichmentID: uuid.New(), ScanID: uuid.New(), UserID: uuid.New(), Family: "history",
		Status: status, Chains: chains, TransitionID: "t-" + uuid.NewString(), CreatedAt: created, UpdatedAt: created,
	}
	if err := db.Create(&e).Error; err != nil {
		t.Fatalf("seed enrichment: %v", err)
	}
	return e
}

// getOpenEnrichments calls the route with the service token and no X-User-Id.
func getOpenEnrichments(t *testing.T, base, query string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+scanroutes.V1Base+scanroutes.Enrichments+query, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set(headerAuthorization, "Bearer "+testServiceToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.StatusCode, out
}

func openEnrichmentIDs(t *testing.T, body map[string]any) []string {
	t.Helper()
	items, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("items = %#v, want an array", body["items"])
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.(map[string]any)["enrichment_id"].(string))
	}
	return ids
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// CA4.4, ET7: default statuses are PENDING and RUNNING; only rows created before the bound are listed.
func TestScanAPI_ListOpenEnrichments_StatusAndBound(t *testing.T) {
	db := setupScanAPITestDB(t)
	pending := seedEnrichment(t, db, "PENDING", openEnrichmentsBase, "[]")
	running := seedEnrichment(t, db, "RUNNING", openEnrichmentsBase.Add(time.Second), "[]")
	seedEnrichment(t, db, "DONE", openEnrichmentsBase, "[]")
	seedEnrichment(t, db, "ERROR", openEnrichmentsBase, "[]")
	seedEnrichment(t, db, "PENDING", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), "[]") // equal to the bound: excluded
	seedEnrichment(t, db, "PENDING", time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), "[]")
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	code, body := getOpenEnrichments(t, ts.URL, "?created_before="+openEnrichmentsBound)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%#v)", code, body)
	}
	if got, want := openEnrichmentIDs(t, body), []string{pending.EnrichmentID.String(), running.EnrichmentID.String()}; !sameStrings(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}

	code, body = getOpenEnrichments(t, ts.URL, "?status=RUNNING&created_before="+openEnrichmentsBound)
	if got, want := openEnrichmentIDs(t, body), []string{running.EnrichmentID.String()}; code != http.StatusOK || !sameStrings(got, want) {
		t.Fatalf("status=RUNNING = %d %v, want 200 %v", code, got, want)
	}
	code, body = getOpenEnrichments(t, ts.URL, "?status=PENDING,RUNNING&created_before="+openEnrichmentsBound)
	if code != http.StatusOK || len(openEnrichmentIDs(t, body)) != 2 {
		t.Fatalf("status=PENDING,RUNNING = %d %#v, want 200 and 2 items", code, body)
	}
}

// CA4.4, ET7: an empty result is an empty array, not null.
func TestScanAPI_ListOpenEnrichments_EmptyIsArray(t *testing.T) {
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), setupScanAPITestDB(t))
	defer ts.Close()
	code, body := getOpenEnrichments(t, ts.URL, "?created_before="+openEnrichmentsBound)
	items, ok := body["items"].([]any)
	if code != http.StatusOK || !ok || len(items) != 0 {
		t.Fatalf("got %d %#v, want 200 and items []", code, body)
	}
}

// CA4.4, ET7: order is created_at then enrichment_id.
func TestScanAPI_ListOpenEnrichments_Order(t *testing.T) {
	db := setupScanAPITestDB(t)
	late := seedEnrichment(t, db, "PENDING", openEnrichmentsBase.Add(time.Minute), "[]")
	tied := []domain.ScanEnrichmentEntity{
		seedEnrichment(t, db, "PENDING", openEnrichmentsBase, "[]"),
		seedEnrichment(t, db, "RUNNING", openEnrichmentsBase, "[]"),
		seedEnrichment(t, db, "PENDING", openEnrichmentsBase, "[]"),
	}
	sort.Slice(tied, func(i, j int) bool { return tied[i].EnrichmentID.String() < tied[j].EnrichmentID.String() })
	want := []string{tied[0].EnrichmentID.String(), tied[1].EnrichmentID.String(), tied[2].EnrichmentID.String(), late.EnrichmentID.String()}
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	code, body := getOpenEnrichments(t, ts.URL, "?created_before="+openEnrichmentsBound)
	if got := openEnrichmentIDs(t, body); code != http.StatusOK || !sameStrings(got, want) {
		t.Fatalf("ids = %v (status %d), want %v", got, code, want)
	}
}

// CA4.4, ET7: limit bounds the page; an absent limit means 100.
func TestScanAPI_ListOpenEnrichments_Limit(t *testing.T) {
	db := setupScanAPITestDB(t)
	for i := 0; i < 101; i++ {
		seedEnrichment(t, db, "PENDING", openEnrichmentsBase.Add(time.Duration(i)*time.Second), "[]")
	}
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	for _, tc := range []struct {
		query string
		want  int
	}{{"&limit=1", 1}, {"&limit=5", 5}, {"&limit=100", 100}, {"", 100}} {
		code, body := getOpenEnrichments(t, ts.URL, "?created_before="+openEnrichmentsBound+tc.query)
		if code != http.StatusOK || len(openEnrichmentIDs(t, body)) != tc.want {
			t.Fatalf("query %q = %d with %d items, want 200 with %d", tc.query, code, len(openEnrichmentIDs(t, body)), tc.want)
		}
	}
}

// CA4.4, ET7: a soft-deleted enrichment is never listed.
func TestScanAPI_ListOpenEnrichments_SoftDeletedExcluded(t *testing.T) {
	db := setupScanAPITestDB(t)
	live := seedEnrichment(t, db, "PENDING", openEnrichmentsBase, "[]")
	gone := seedEnrichment(t, db, "PENDING", openEnrichmentsBase, "[]")
	if err := db.Delete(&gone).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	code, body := getOpenEnrichments(t, ts.URL, "?created_before="+openEnrichmentsBound)
	if got, want := openEnrichmentIDs(t, body), []string{live.EnrichmentID.String()}; code != http.StatusOK || !sameStrings(got, want) {
		t.Fatalf("ids = %v (status %d), want %v", got, code, want)
	}
}

// CA4.4, ET7: each item has exactly the six fields, created_at is RFC 3339 nanoseconds UTC, and chains is absent.
func TestScanAPI_ListOpenEnrichments_ItemFields(t *testing.T) {
	db := setupScanAPITestDB(t)
	e := seedEnrichment(t, db, "RUNNING", openEnrichmentsBase, `[{"chain_id":1,"outcome":"pending"}]`)
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	code, body := getOpenEnrichments(t, ts.URL, "?created_before="+openEnrichmentsBound)
	items, _ := body["items"].([]any)
	if code != http.StatusOK || len(items) != 1 {
		t.Fatalf("got %d %#v, want 200 and one item", code, body)
	}
	item := items[0].(map[string]any)
	want := map[string]string{
		"enrichment_id": e.EnrichmentID.String(), "scan_id": e.ScanID.String(), "user_id": e.UserID.String(),
		"family": "history", "status": "RUNNING", "created_at": "2026-01-01T10:00:00.123456789Z",
	}
	if len(item) != len(want) {
		t.Fatalf("item has %d keys %#v, want exactly %d", len(item), item, len(want))
	}
	for k, v := range want {
		if item[k] != v {
			t.Fatalf("item[%s] = %#v, want %q", k, item[k], v)
		}
	}
	if _, has := item["chains"]; has {
		t.Fatal("item carries chains")
	}
}

// CA4.4, ET7, OP-5.14: a stored chains value that is not JSON does not fail the list.
func TestScanAPI_ListOpenEnrichments_UndecodableChainsListed(t *testing.T) {
	db := setupScanAPITestDB(t)
	bad := seedEnrichment(t, db, "PENDING", openEnrichmentsBase, "not json")
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	code, body := getOpenEnrichments(t, ts.URL, "?created_before="+openEnrichmentsBound)
	if got, want := openEnrichmentIDs(t, body), []string{bad.EnrichmentID.String()}; code != http.StatusOK || !sameStrings(got, want) {
		t.Fatalf("got %d %v, want 200 %v", code, got, want)
	}
}

// CA4.4, ET7: the route takes no X-User-Id; a header that is sent is ignored.
func TestScanAPI_ListOpenEnrichments_IgnoresUserIDHeader(t *testing.T) {
	db := setupScanAPITestDB(t)
	e := seedEnrichment(t, db, "PENDING", openEnrichmentsBase, "[]")
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	resp := doAuthRequest(t, http.MethodGet, ts.URL+scanroutes.V1Base+scanroutes.Enrichments+"?created_before="+openEnrichmentsBound, uuid.New(), nil)
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, want := openEnrichmentIDs(t, body), []string{e.EnrichmentID.String()}; resp.StatusCode != http.StatusOK || !sameStrings(got, want) {
		t.Fatalf("got %d %v, want 200 %v (other user's row listed)", resp.StatusCode, got, want)
	}
}

// OC-P70b S3: created_before is converted to UTC before the query, so an offset bound compares by instant.
func TestScanAPI_ListOpenEnrichments_OffsetBoundComparedAsInstant(t *testing.T) {
	db := setupScanAPITestDB(t)
	before := seedEnrichment(t, db, "PENDING", time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), "[]")
	seedEnrichment(t, db, "PENDING", time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC), "[]")
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	// 11:30+02:00 is 09:30Z: only the 09:00Z row is before it.
	code, body := getOpenEnrichments(t, ts.URL, "?created_before="+url.QueryEscape("2026-01-01T11:30:00+02:00"))
	if got, want := openEnrichmentIDs(t, body), []string{before.EnrichmentID.String()}; code != http.StatusOK || !sameStrings(got, want) {
		t.Fatalf("got %d %v, want 200 %v", code, got, want)
	}
	// Fractional seconds are accepted.
	code, _ = getOpenEnrichments(t, ts.URL, "?created_before="+url.QueryEscape("2026-01-01T09:30:00.5Z"))
	if code != http.StatusOK {
		t.Fatalf("fractional bound status = %d, want 200", code)
	}
}

// CA4.4, ET7: a store error is 503 service_unavailable.
func TestScanAPI_ListOpenEnrichments_StoreErrorIs503(t *testing.T) {
	db := setupScanAPITestDB(t)
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	code, body := getOpenEnrichments(t, ts.URL, "?created_before="+openEnrichmentsBound)
	if code != http.StatusServiceUnavailable || body["error"] != "service_unavailable" {
		t.Fatalf("got %d %#v, want 503 service_unavailable", code, body)
	}
}

// CA4.4, ET7: the route needs the service-level access.
func TestScanAPI_ListOpenEnrichments_RequiresServiceAuth(t *testing.T) {
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), setupScanAPITestDB(t))
	defer ts.Close()

	for name, header := range map[string]string{"missing": "", "wrong": "Bearer wrong-token"} {
		req, err := http.NewRequest(http.MethodGet, ts.URL+scanroutes.V1Base+scanroutes.Enrichments+"?created_before="+openEnrichmentsBound, nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		if header != "" {
			req.Header.Set(headerAuthorization, header)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s token: status = %d, want 401", name, resp.StatusCode)
		}
	}
}

// CA4.4, ET7 and OC-P70b S1: each parameter rule violation is 400 invalid_request.
func TestScanAPI_ListOpenEnrichments_InvalidParameters(t *testing.T) {
	db := setupScanAPITestDB(t)
	seedEnrichment(t, db, "PENDING", openEnrichmentsBase, "[]")
	ts := newTestScanAPIServer(t, newMemoryPendingRepo(), db)
	defer ts.Close()

	cb := "created_before=" + openEnrichmentsBound
	for name, query := range map[string]string{
		"created_before missing":    "",
		"created_before empty":      "?created_before=",
		"created_before no offset":  "?created_before=2026-01-02T00:00:00",
		"created_before date only":  "?created_before=2026-01-02",
		"created_before garbage":    "?created_before=yesterday",
		"status empty":              "?status=&" + cb,
		"status empty element":      "?status=PENDING,&" + cb,
		"status leading empty":      "?status=,RUNNING&" + cb,
		"status unknown value":      "?status=DONE&" + cb,
		"status lower case":         "?status=pending&" + cb,
		"status duplicate":          "?status=PENDING,PENDING&" + cb,
		"status mixed with unknown": "?status=PENDING,ERROR&" + cb,
		"limit zero":                "?limit=0&" + cb,
		"limit negative":            "?limit=-1&" + cb,
		"limit over max":            "?limit=101&" + cb,
		"limit not an integer":      "?limit=abc&" + cb,
		"limit decimal":             "?limit=1.5&" + cb,
		"limit_empty":               "?limit=&" + cb,
	} {
		t.Run(name, func(t *testing.T) {
			code, body := getOpenEnrichments(t, ts.URL, query)
			if code != http.StatusBadRequest || body["error"] != "invalid_request" {
				t.Fatalf("%s: got %d %s, want 400 invalid_request", query, code, fmt.Sprint(body))
			}
		})
	}
}
