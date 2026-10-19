package scanapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cafe-persistence/internal/config"
	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/persistence/storage"
	"cafe-persistence/internal/repository"
	"cafe-persistence/internal/scanroutes"
	"cafe-persistence/internal/walletobservation"
	"cafe-persistence/pkg/scan"

	"github.com/create2-labs/cafe-contracts/cafenatsv01"
	lifecycle "github.com/create2-labs/cafe-contracts/discovery/scanlifecycle/v1"
	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"
	goredis "github.com/redis/go-redis/v9"
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

type transitionRedis struct {
	mu     sync.Mutex
	values map[string]string
	setErr error
}

func (m *transitionRedis) Get(context.Context, string) *goredis.StringCmd {
	return goredis.NewStringCmd(context.Background())
}
func (m *transitionRedis) Set(_ context.Context, key string, value interface{}, _ time.Duration) *goredis.StatusCmd {
	cmd := goredis.NewStatusCmd(context.Background())
	if m.setErr != nil {
		cmd.SetErr(m.setErr)
		return cmd
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch v := value.(type) {
	case []byte:
		m.values[key] = string(v)
	case string:
		m.values[key] = v
	default:
		raw, _ := json.Marshal(v)
		m.values[key] = string(raw)
	}
	cmd.SetVal("OK")
	return cmd
}
func (m *transitionRedis) SetArgs(context.Context, string, interface{}, goredis.SetArgs) *goredis.StatusCmd {
	return goredis.NewStatusCmd(context.Background())
}
func (m *transitionRedis) Del(context.Context, ...string) *goredis.IntCmd {
	return goredis.NewIntCmd(context.Background())
}
func (m *transitionRedis) Eval(context.Context, string, []string, ...interface{}) *goredis.Cmd {
	return goredis.NewCmd(context.Background())
}
func (m *transitionRedis) Keys(context.Context, string) *goredis.StringSliceCmd {
	return goredis.NewStringSliceCmd(context.Background())
}
func (m *transitionRedis) Close() error { return nil }
func (m *transitionRedis) Ping(context.Context) *goredis.StatusCmd {
	cmd := goredis.NewStatusCmd(context.Background())
	cmd.SetVal("PONG")
	return cmd
}
func (m *transitionRedis) get(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values[key]
}

type transitionBus struct {
	mu       sync.Mutex
	subjects []string
	payloads [][]byte
	fail     map[string]error
}

func (b *transitionBus) Publish(subject string, data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.fail[subject]; err != nil {
		return err
	}
	b.subjects = append(b.subjects, subject)
	b.payloads = append(b.payloads, append([]byte(nil), data...))
	return nil
}
func (b *transitionBus) Subscribe(string, func(*natslib.Msg)) (*natslib.Subscription, error) {
	return nil, nil
}
func (b *transitionBus) QueueSubscribe(string, string, func(*natslib.Msg)) (*natslib.Subscription, error) {
	return nil, nil
}
func (b *transitionBus) Close()            {}
func (b *transitionBus) IsConnected() bool { return true }
func (b *transitionBus) count(subject string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, s := range b.subjects {
		if s == subject {
			n++
		}
	}
	return n
}
func (b *transitionBus) body(subject string, nth int) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for i, s := range b.subjects {
		if s != subject {
			continue
		}
		if n == nth {
			return b.payloads[i]
		}
		n++
	}
	return nil
}

func startProjectedAPI(t *testing.T, cache *transitionRedis, bus *transitionBus) (*httptest.Server, *gorm.DB) {
	t.Helper()
	db := setupScanAPITestDB(t)
	h := NewHandler(
		newMemoryPendingRepo(),
		repository.NewScanResultRepository(db),
		repository.NewTLSScanResultRepository(db),
		repository.NewScanUsageLedgerRepository(db),
		storage.NewRedisCache(cache),
		&config.ChainConfig{},
	)
	h.EnableWalletTransition(db)
	h.EnableWalletEvents(bus)
	return httptest.NewServer(NewServer("127.0.0.1", "0", testServiceToken, h).HTTPHandler()), db
}

func walletCacheKey(user uuid.UUID) string {
	return "wallet:user:" + user.String() + ":" + transitionAddr
}

func assertProjected(t *testing.T, code int, got map[string]any, outcome, status string, ready, published bool) {
	t.Helper()
	if code != http.StatusOK || got["outcome"] != outcome || got["status"] != status ||
		got["projection_ready"] != ready || got["ready_published"] != published {
		t.Fatalf("status %d body %#v", code, got)
	}
}

func observedEventID(t *testing.T, raw []byte) string {
	t.Helper()
	var ev struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil || ev.EventID == "" {
		t.Fatalf("wallet.observed %s: %v", raw, err)
	}
	return ev.EventID
}

func TestApplyWalletTransition_ProjectsTerminalSuccess(t *testing.T) {
	cache := &transitionRedis{values: map[string]string{}}
	bus := &transitionBus{}
	ts, db := startProjectedAPI(t, cache, bus)
	defer ts.Close()
	user := uuid.New()
	ledger := repository.NewScanUsageLedgerRepository(db)
	scanID := uuid.New()
	if err := ledger.RecordSuccessUsage(user, scanID, domain.ScanUsageKindWallet); err != nil {
		t.Fatal(err)
	}
	url := transitionURL(ts.URL, scanID)

	code, got := postTransition(t, url, user, successBody("t-ok", user, transitionAddr))
	assertProjected(t, code, got, "applied", scan.StateSUCCESS, true, true)
	if !strings.Contains(cache.get(walletCacheKey(user)), `"risk_score":4`) {
		t.Fatalf("redis %s", cache.get(walletCacheKey(user)))
	}
	readyRaw := bus.body(lifecycle.SubjectScanReady, 0)
	var ready lifecycle.Ready
	if err := json.Unmarshal(readyRaw, &ready); err != nil || ready.Status != "success" || ready.Address != transitionAddr || ready.Kind != "wallet" {
		t.Fatalf("scan.ready %s", readyRaw)
	}
	wantID := walletobservation.ExportMetaForScanJob(scanID).EventID
	if bus.count(cafenatsv01.NATSSubjectDiscoveryWalletObservedV01) != 1 || observedEventID(t, bus.body(cafenatsv01.NATSSubjectDiscoveryWalletObservedV01, 0)) != wantID {
		t.Fatal("wallet.observed")
	}

	code, got = postTransition(t, url, user, successBody("t-ok", user, transitionAddr))
	assertProjected(t, code, got, "already_applied", scan.StateSUCCESS, true, true)
	if bus.count(lifecycle.SubjectScanReady) != 2 || bus.count(cafenatsv01.NATSSubjectDiscoveryWalletObservedV01) != 2 {
		t.Fatalf("replay subjects ready=%d observed=%d", bus.count(lifecycle.SubjectScanReady), bus.count(cafenatsv01.NATSSubjectDiscoveryWalletObservedV01))
	}
	if observedEventID(t, bus.body(cafenatsv01.NATSSubjectDiscoveryWalletObservedV01, 1)) != wantID {
		t.Fatal("replay event id changed")
	}

	loser := successBody("t-lose", user, transitionAddr)
	loser["result"].(map[string]any)["risk_score"] = 9
	code, got = postTransition(t, url, user, loser)
	assertProjected(t, code, got, "already_terminal", scan.StateSUCCESS, false, false)
	if !strings.Contains(cache.get(walletCacheKey(user)), `"risk_score":4`) || strings.Contains(cache.get(walletCacheKey(user)), `"risk_score":9`) {
		t.Fatalf("loser rewrote redis %s", cache.get(walletCacheKey(user)))
	}
	if bus.count(lifecycle.SubjectScanReady) != 2 || bus.count(cafenatsv01.NATSSubjectDiscoveryWalletObservedV01) != 2 || ledgerRows(t, db, scanID) != 1 {
		t.Fatal("loser published or changed the ledger")
	}
	runURL := transitionURL(ts.URL, uuid.New())
	code, got = postTransition(t, runURL, user, runningBody("t-run", user))
	assertProjected(t, code, got, "applied", scan.StateRUNNING, false, false)
	code, got = postTransition(t, runURL, user, runningBody("t-other", user))
	assertProjected(t, code, got, "already_running", scan.StateRUNNING, false, false)
	if !strings.Contains(cache.get(walletCacheKey(user)), `"risk_score":4`) || bus.count(lifecycle.SubjectScanReady) != 2 || bus.count(cafenatsv01.NATSSubjectDiscoveryWalletObservedV01) != 2 {
		t.Fatal("RUNNING published")
	}
}

func TestApplyWalletTransition_FailedPublishesReadyOnly(t *testing.T) {
	cache := &transitionRedis{values: map[string]string{}}
	bus := &transitionBus{}
	ts, db := startProjectedAPI(t, cache, bus)
	defer ts.Close()
	user := uuid.New()
	scanID := uuid.New()
	if err := repository.NewScanUsageLedgerRepository(db).RecordSuccessUsage(user, scanID, domain.ScanUsageKindWallet); err != nil {
		t.Fatal(err)
	}
	code, got := postTransition(t, transitionURL(ts.URL, scanID), user, failedBody("t-fail", user))
	assertProjected(t, code, got, "applied", scan.StateFAILED, true, true)
	if !strings.Contains(cache.get(walletCacheKey(user)), "scan failed") {
		t.Fatalf("redis %s", cache.get(walletCacheKey(user)))
	}
	var ready lifecycle.Ready
	if err := json.Unmarshal(bus.body(lifecycle.SubjectScanReady, 0), &ready); err != nil || ready.Status != "failed" {
		t.Fatalf("scan.ready %#v", ready)
	}
	if bus.count(cafenatsv01.NATSSubjectDiscoveryWalletObservedV01) != 0 || ledgerRows(t, db, scanID) != 0 {
		t.Fatal("FAILED published wallet.observed or kept the ledger")
	}
}

func TestApplyWalletTransition_RedisFailureKeepsCommit(t *testing.T) {
	cache := &transitionRedis{values: map[string]string{}, setErr: errors.New("redis down")}
	bus := &transitionBus{}
	ts, db := startProjectedAPI(t, cache, bus)
	defer ts.Close()
	user := uuid.New()
	scanID := uuid.New()
	if err := repository.NewScanUsageLedgerRepository(db).RecordSuccessUsage(user, scanID, domain.ScanUsageKindWallet); err != nil {
		t.Fatal(err)
	}
	code, got := postTransition(t, transitionURL(ts.URL, scanID), user, successBody("t-redis", user, transitionAddr))
	assertProjected(t, code, got, "applied", scan.StateSUCCESS, false, false)
	row, ok := loadWalletRow(t, db, scanID)
	if !ok || row.Status != scan.StateSUCCESS || ledgerRows(t, db, scanID) != 1 || bus.count(lifecycle.SubjectScanReady) != 0 {
		t.Fatalf("row %+v ok %v bus %d", row, ok, bus.count(lifecycle.SubjectScanReady))
	}
}

func TestApplyWalletTransition_ReadyFailureStillObserves(t *testing.T) {
	cache := &transitionRedis{values: map[string]string{}}
	bus := &transitionBus{fail: map[string]error{lifecycle.SubjectScanReady: errors.New("nats down")}}
	ts, db := startProjectedAPI(t, cache, bus)
	defer ts.Close()
	user := uuid.New()
	scanID := uuid.New()
	if err := repository.NewScanUsageLedgerRepository(db).RecordSuccessUsage(user, scanID, domain.ScanUsageKindWallet); err != nil {
		t.Fatal(err)
	}
	code, got := postTransition(t, transitionURL(ts.URL, scanID), user, successBody("t-ready", user, transitionAddr))
	assertProjected(t, code, got, "applied", scan.StateSUCCESS, true, false)
	if bus.count(lifecycle.SubjectScanReady) != 0 || bus.count(cafenatsv01.NATSSubjectDiscoveryWalletObservedV01) != 1 {
		t.Fatalf("ready=%d observed=%d", bus.count(lifecycle.SubjectScanReady), bus.count(cafenatsv01.NATSSubjectDiscoveryWalletObservedV01))
	}
	if ledgerRows(t, db, scanID) != 1 || cache.get(walletCacheKey(user)) == "" {
		t.Fatal("ready failure rolled back the commit or skipped redis")
	}
}

func TestApplyWalletTransition_ConcurrentCreatesOneRow(t *testing.T) {
	cache := &transitionRedis{values: map[string]string{}}
	bus := &transitionBus{}
	ts, db := startProjectedAPI(t, cache, bus)
	defer ts.Close()
	user := uuid.New()
	scanID := uuid.New()
	if err := repository.NewScanUsageLedgerRepository(db).RecordSuccessUsage(user, scanID, domain.ScanUsageKindWallet); err != nil {
		t.Fatal(err)
	}
	url := transitionURL(ts.URL, scanID)
	type result struct {
		code int
		body map[string]any
		err  error
	}
	out := make(chan result, 2)
	var wg sync.WaitGroup
	for _, id := range []string{"c-a", "c-b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			raw, err := json.Marshal(successBody(id, user, transitionAddr))
			if err != nil {
				out <- result{err: err}
				return
			}
			req, err := authRequest(http.MethodPost, url, user, raw)
			if err != nil {
				out <- result{err: err}
				return
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				out <- result{err: err}
				return
			}
			defer func() { _ = resp.Body.Close() }()
			var got map[string]any
			err = json.NewDecoder(resp.Body).Decode(&got)
			out <- result{code: resp.StatusCode, body: got, err: err}
		}(id)
	}
	wg.Wait()
	close(out)
	var applied, terminal int
	for res := range out {
		if res.err != nil || res.code != http.StatusOK {
			t.Fatalf("concurrent response %d %#v %v", res.code, res.body, res.err)
		}
		switch res.body["outcome"] {
		case "applied":
			applied++
			if res.body["projection_ready"] != true || res.body["ready_published"] != true {
				t.Fatalf("winner %#v", res.body)
			}
		case "already_terminal":
			terminal++
			if res.body["projection_ready"] != false || res.body["ready_published"] != false {
				t.Fatalf("loser %#v", res.body)
			}
		default:
			t.Fatalf("outcome %#v", res.body)
		}
	}
	rows := int64(0)
	if err := db.Model(&domain.ScanResultEntity{}).Where("id = ?", scanID).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if applied != 1 || terminal != 1 || rows != 1 || ledgerRows(t, db, scanID) != 1 {
		t.Fatalf("applied %d terminal %d rows %d ledger %d", applied, terminal, rows, ledgerRows(t, db, scanID))
	}
	if bus.count(lifecycle.SubjectScanReady) != 1 || bus.count(cafenatsv01.NATSSubjectDiscoveryWalletObservedV01) != 1 {
		t.Fatal("concurrent loser published")
	}
}
