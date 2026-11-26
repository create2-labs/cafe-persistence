package scanapi

import (
	"bytes"
	"encoding/json"
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

func startEnrichmentAPI(t *testing.T, db *gorm.DB) *httptest.Server {
	t.Helper()
	h := NewHandler(
		newMemoryPendingRepo(),
		repository.NewScanResultRepository(db),
		repository.NewTLSScanResultRepository(db),
		repository.NewScanUsageLedgerRepository(db),
		nil,
		&config.ChainConfig{},
	)
	if db != nil {
		h.EnableWalletTransition(db)
	}
	return httptest.NewServer(authedRoutes(h))
}

func enrichmentURL(base, scanID, enrichmentID string) string {
	url := base + scanroutes.V1Base + scanroutes.WalletScanEnrichmentTransition
	url = strings.Replace(url, "{scan_id}", scanID, 1)
	return strings.Replace(url, "{enrichment_id}", enrichmentID, 1)
}

func seedSuccessScan(t *testing.T, db *gorm.DB, user uuid.UUID, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := db.Create(&domain.ScanResultEntity{ID: id, UserID: user, Address: transitionAddr, Status: status}).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func pendingChains() []map[string]any {
	return []map[string]any{{"chain_id": 1, "outcome": "pending"}, {"chain_id": 10, "outcome": "not_observed"}}
}

// pendingEnrichmentBody is a valid creation body; mutate edits it per case.
func pendingEnrichmentBody(id string, user uuid.UUID, mutate func(map[string]any)) map[string]any {
	b := map[string]any{
		"transition_id": id,
		"user_id":       user.String(),
		"family":        "history",
		"source_states": []string{"PENDING"},
		"target_state":  "PENDING",
		"quota_action":  "none",
		"result":        map[string]any{"chains": pendingChains()},
	}
	if mutate != nil {
		mutate(b)
	}
	return b
}

func postRaw(t *testing.T, url string, header uuid.UUID, raw []byte) (int, map[string]any) {
	t.Helper()
	resp := doAuthRequest(t, http.MethodPost, url, header, raw)
	defer func() { _ = resp.Body.Close() }()
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.StatusCode, got
}

func postEnrichment(t *testing.T, url string, header uuid.UUID, body any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return postRaw(t, url, header, raw)
}

func assertEnrichmentOutcome(t *testing.T, label string, code int, got map[string]any, outcome, status string) {
	t.Helper()
	if code != http.StatusOK || got["outcome"] != outcome {
		t.Fatalf("%s: status %d body %#v, want outcome %s", label, code, got, outcome)
	}
	if _, has := got["projection_ready"]; has {
		t.Fatalf("%s: projection_ready must be absent: %#v", label, got)
	}
	if _, has := got["ready_published"]; has {
		t.Fatalf("%s: ready_published must be absent: %#v", label, got)
	}
	raw, present := got["status"]
	if status == "" && present || status != "" && raw != status {
		t.Fatalf("%s: status %#v, want %q", label, raw, status)
	}
}

func enrichmentRows(t *testing.T, db *gorm.DB, scanID uuid.UUID) []domain.ScanEnrichmentEntity {
	t.Helper()
	var rows []domain.ScanEnrichmentEntity
	if err := db.Unscoped().Where("scan_id = ?", scanID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

// CA4.1 (record), design §5.8: a valid PENDING command creates the row with the chains in body order.
func TestApplyEnrichmentTransition_ProtocolAndRejections(t *testing.T) {
	db := setupScanAPITestDB(t)
	ts := startEnrichmentAPI(t, db)
	defer ts.Close()
	user := uuid.New()
	scanID := seedSuccessScan(t, db, user, scan.StateSUCCESS)
	enrichmentID := uuid.New()
	url := enrichmentURL(ts.URL, scanID.String(), enrichmentID.String())

	// CA4.1: created, stored status PENDING, chains in body order.
	code, got := postEnrichment(t, url, user, pendingEnrichmentBody("t-ok", user, nil))
	assertEnrichmentOutcome(t, "valid pending", code, got, "applied", "PENDING")
	rows := enrichmentRows(t, db, scanID)
	if len(rows) != 1 || rows[0].EnrichmentID != enrichmentID || rows[0].Status != "PENDING" || rows[0].TransitionID != "t-ok" {
		t.Fatalf("rows %+v", rows)
	}
	chains, err := rows[0].DecodeChains()
	if err != nil || len(chains) != 2 || chains[0].ChainID != 1 || chains[0].Outcome != "pending" || chains[1].ChainID != 10 {
		t.Fatalf("chains %+v err %v", chains, err)
	}

	// Protocol errors are HTTP 400 without an outcome.
	valid, err := json.Marshal(pendingEnrichmentBody("t-p", user, nil))
	if err != nil {
		t.Fatal(err)
	}
	withUnknown := pendingEnrichmentBody("t-p", user, func(b map[string]any) { b["extra"] = 1 })
	withUnknownResult := pendingEnrichmentBody("t-p", user, func(b map[string]any) { b["result"].(map[string]any)["extra"] = 1 })
	rawUnknown, _ := json.Marshal(withUnknown)
	rawUnknownResult, _ := json.Marshal(withUnknownResult)
	other := uuid.NewString()
	protocol := map[string]struct {
		url    string
		header string
		body   []byte
	}{
		"missing X-User-Id":     {url, "", valid},
		"invalid X-User-Id":     {url, "not-a-uuid", valid},
		"scan_id not a UUID":    {enrichmentURL(ts.URL, "nope", other), user.String(), valid},
		"enrichment_id no UUID": {enrichmentURL(ts.URL, other, "nope"), user.String(), valid},
		"unreadable JSON":       {url, user.String(), []byte("{")},
		"unknown field":         {url, user.String(), rawUnknown},
		"unknown result field":  {url, user.String(), rawUnknownResult},
	}
	for name, c := range protocol {
		req, err := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(c.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(headerAuthorization, "Bearer "+testServiceToken)
		req.Header.Set("Content-Type", "application/json")
		if c.header != "" {
			req.Header.Set(headerUserID, c.header)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		decodeErr := json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if decodeErr != nil || resp.StatusCode != http.StatusBadRequest || body["error"] == nil {
			t.Fatalf("%s: status %d body %#v err %v", name, resp.StatusCode, body, decodeErr)
		}
		if _, has := body["outcome"]; has {
			t.Fatalf("%s: protocol error carries an outcome: %#v", name, body)
		}
	}

	// Rejections: HTTP 200 rejected, no row written (table of design §5.8; OP-5.13 for the three empty chains).
	chain := func(m map[string]any) []map[string]any { return []map[string]any{m} }
	setChains := func(c any) func(map[string]any) {
		return func(b map[string]any) { b["result"] = map[string]any{"chains": c} }
	}
	cases := map[string]func(map[string]any){
		"blank transition_id":         func(b map[string]any) { b["transition_id"] = "  " },
		"body user differs":           func(b map[string]any) { b["user_id"] = uuid.NewString() },
		"nil user":                    func(b map[string]any) { b["user_id"] = uuid.Nil.String() },
		"deployer family":             func(b map[string]any) { b["family"] = "deployer" },
		"unknown target":              func(b map[string]any) { b["target_state"] = "SUCCESS" },
		"two sources for PENDING":     func(b map[string]any) { b["source_states"] = []string{"PENDING", "RUNNING"} },
		"empty sources":               func(b map[string]any) { b["source_states"] = []string{} },
		"quota keep":                  func(b map[string]any) { b["quota_action"] = "keep" },
		"error text on PENDING":       func(b map[string]any) { b["error"] = "boom" },
		"result absent":               func(b map[string]any) { delete(b, "result") },
		"result without chains":       func(b map[string]any) { b["result"] = map[string]any{} },
		"chains null":                 setChains(nil),
		"chains empty":                setChains([]map[string]any{}),
		"chain_id zero":               setChains(chain(map[string]any{"chain_id": 0, "outcome": "pending"})),
		"duplicate chain_id":          setChains([]map[string]any{{"chain_id": 1, "outcome": "pending"}, {"chain_id": 1, "outcome": "pending"}}),
		"no pending chain":            setChains(chain(map[string]any{"chain_id": 1, "outcome": "not_observed"})),
		"observed outcome on PENDING": setChains([]map[string]any{{"chain_id": 1, "outcome": "pending"}, {"chain_id": 10, "outcome": "observed"}}),
		"error chain without code":    setChains([]map[string]any{{"chain_id": 1, "outcome": "pending"}, {"chain_id": 10, "outcome": "error"}}),
		"code on a pending chain":     setChains(chain(map[string]any{"chain_id": 1, "outcome": "pending", "code": "x"})),
		"hash on a pending chain":     setChains(chain(map[string]any{"chain_id": 1, "outcome": "pending", "transaction_hash": "0x01"})),
		"recovery field on PENDING": func(b map[string]any) {
			b["result"].(map[string]any)["public_key_recovery"] = "unresolved"
		},
	}
	for name, mutate := range cases {
		id := uuid.New()
		code, got := postEnrichment(t, enrichmentURL(ts.URL, scanID.String(), id.String()), user, pendingEnrichmentBody("t-r", user, mutate))
		assertEnrichmentOutcome(t, name, code, got, "rejected", "")
		if n := len(enrichmentRows(t, db, scanID)); n != 1 {
			t.Fatalf("%s: wrote a row (%d rows)", name, n)
		}
	}

	// The scan check: not SUCCESS, owned by another user, or unknown.
	failed := seedSuccessScan(t, db, user, scan.StateFAILED)
	foreign := seedSuccessScan(t, db, uuid.New(), scan.StateSUCCESS)
	for name, id := range map[string]uuid.UUID{"scan not SUCCESS": failed, "scan of another user": foreign, "unknown scan": uuid.New()} {
		code, got := postEnrichment(t, enrichmentURL(ts.URL, id.String(), uuid.NewString()), user, pendingEnrichmentBody("t-s", user, nil))
		assertEnrichmentOutcome(t, name, code, got, "rejected", "")
		if n := len(enrichmentRows(t, db, id)); n != 0 {
			t.Fatalf("%s: wrote a row", name)
		}
	}
}

func TestApplyEnrichmentTransition_RetryableWithoutDatabase(t *testing.T) {
	ts := startEnrichmentAPI(t, nil)
	defer ts.Close()
	user := uuid.New()
	code, got := postEnrichment(t, enrichmentURL(ts.URL, uuid.NewString(), uuid.NewString()), user, pendingEnrichmentBody("t-n", user, nil))
	assertEnrichmentOutcome(t, "no database", code, got, "retryable_error", "")
}

// CA4.1, design §5.8 (Existing enrichment): duplicates and out-of-order commands never write on an existing row.
func TestApplyEnrichmentTransition_OutOfOrderAndDuplicates(t *testing.T) {
	db := setupScanAPITestDB(t)
	ts := startEnrichmentAPI(t, db)
	defer ts.Close()
	user := uuid.New()
	scanID := seedSuccessScan(t, db, user, scan.StateSUCCESS)
	enrichmentID := uuid.New()
	url := enrichmentURL(ts.URL, scanID.String(), enrichmentID.String())

	code, got := postEnrichment(t, url, user, pendingEnrichmentBody("t-1", user, nil))
	assertEnrichmentOutcome(t, "first", code, got, "applied", "PENDING")

	// Redelivery with the same transition_id.
	code, got = postEnrichment(t, url, user, pendingEnrichmentBody("t-1", user, nil))
	assertEnrichmentOutcome(t, "redelivery", code, got, "already_applied", "PENDING")

	// Another transition_id for the same PENDING record is a defect: rejected, stored status reported.
	code, got = postEnrichment(t, url, user, pendingEnrichmentBody("t-2", user, nil))
	assertEnrichmentOutcome(t, "second id", code, got, "rejected", "PENDING")

	// Until OC-P43, any other command on an existing row is rejected.
	done := pendingEnrichmentBody("t-3", user, func(b map[string]any) { b["target_state"] = "RUNNING"; delete(b, "result") })
	code, got = postEnrichment(t, url, user, done)
	assertEnrichmentOutcome(t, "running on existing", code, got, "rejected", "PENDING")

	// A (scan_id, family) conflict under another enrichment_id is rejected and writes nothing.
	code, got = postEnrichment(t, enrichmentURL(ts.URL, scanID.String(), uuid.NewString()), user, pendingEnrichmentBody("t-4", user, nil))
	assertEnrichmentOutcome(t, "family conflict", code, got, "rejected", "")

	// A row of another scan or user under the same enrichment_id is rejected.
	otherScan := seedSuccessScan(t, db, user, scan.StateSUCCESS)
	code, got = postEnrichment(t, enrichmentURL(ts.URL, otherScan.String(), enrichmentID.String()), user, pendingEnrichmentBody("t-1", user, nil))
	assertEnrichmentOutcome(t, "foreign enrichment_id", code, got, "rejected", "PENDING")

	for status, outcome := range map[string]string{"RUNNING": "already_running", "DONE": "already_terminal", "ERROR": "already_terminal"} {
		sid := seedSuccessScan(t, db, user, scan.StateSUCCESS)
		row := domain.ScanEnrichmentEntity{
			EnrichmentID: uuid.New(), ScanID: sid, UserID: user, Family: "history", Status: status,
			Chains: `[{"chain_id":1,"outcome":"not_observed"}]`, TransitionID: "t-seed",
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		code, got = postEnrichment(t, enrichmentURL(ts.URL, sid.String(), row.EnrichmentID.String()), user, pendingEnrichmentBody("t-late", user, nil))
		assertEnrichmentOutcome(t, "late pending on "+status, code, got, outcome, status)
		var after domain.ScanEnrichmentEntity
		if err := db.Where("enrichment_id = ?", row.EnrichmentID).First(&after).Error; err != nil {
			t.Fatal(err)
		}
		if after.Status != status || after.Chains != row.Chains || after.TransitionID != "t-seed" {
			t.Fatalf("late pending wrote on %s row: %+v", status, after)
		}
	}
	if n := len(enrichmentRows(t, db, scanID)); n != 1 {
		t.Fatalf("scan has %d enrichment rows, want 1", n)
	}
}
