package handlers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/persistence/storage"
	"cafe-persistence/pkg/nats"
	redisconn "cafe-persistence/pkg/redis"

	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"
	goredis "github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type memRedis struct {
	values map[string]string
}

func (m *memRedis) Get(context.Context, string) *goredis.StringCmd {
	return goredis.NewStringCmd(context.Background())
}
func (m *memRedis) Set(_ context.Context, key string, value interface{}, _ time.Duration) *goredis.StatusCmd {
	switch v := value.(type) {
	case []byte:
		m.values[key] = string(v)
	case string:
		m.values[key] = v
	default:
		b, _ := json.Marshal(v)
		m.values[key] = string(b)
	}
	cmd := goredis.NewStatusCmd(context.Background())
	cmd.SetVal("OK")
	return cmd
}
func (m *memRedis) SetArgs(context.Context, string, interface{}, goredis.SetArgs) *goredis.StatusCmd {
	return goredis.NewStatusCmd(context.Background())
}
func (m *memRedis) Del(context.Context, ...string) *goredis.IntCmd {
	return goredis.NewIntCmd(context.Background())
}
func (m *memRedis) Eval(context.Context, string, []string, ...interface{}) *goredis.Cmd {
	return goredis.NewCmd(context.Background())
}
func (m *memRedis) Keys(context.Context, string) *goredis.StringSliceCmd {
	return goredis.NewStringSliceCmd(context.Background())
}
func (m *memRedis) Close() error { return nil }
func (m *memRedis) Ping(context.Context) *goredis.StatusCmd {
	cmd := goredis.NewStatusCmd(context.Background())
	cmd.SetVal("PONG")
	return cmd
}

type recordingNATS struct {
	subjects []string
	payloads [][]byte
}

func (r *recordingNATS) Publish(subject string, data []byte) error {
	r.subjects = append(r.subjects, subject)
	r.payloads = append(r.payloads, append([]byte(nil), data...))
	return nil
}
func (r *recordingNATS) Subscribe(string, func(msg *natslib.Msg)) (*natslib.Subscription, error) {
	return nil, nil
}
func (r *recordingNATS) QueueSubscribe(string, string, func(msg *natslib.Msg)) (*natslib.Subscription, error) {
	return nil, nil
}
func (r *recordingNATS) Close()            {}
func (r *recordingNATS) IsConnected() bool { return true }

func setupDelegationHandler(t *testing.T) (*ScanEventHandler, *gorm.DB, *memRedis, *recordingNATS) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.New().String()+"?mode=memory&cache=shared&_txlock=immediate"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.ScanResultEntity{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	cache := &memRedis{values: map[string]string{}}
	bus := &recordingNATS{}
	h := NewScanEventHandler(
		nil,
		storage.NewWalletWriter(db),
		storage.NewRedisCache(cache),
		bus,
		map[string]int64{"ethereum-mainnet": 1},
		nil, nil, nil,
	)
	return h, db, cache, bus
}

func walletCacheKey(userID uuid.UUID, address string) string {
	return "wallet:user:" + userID.String() + ":" + address
}

func publishedSubjects(bus *recordingNATS, subject string) int {
	n := 0
	for _, s := range bus.subjects {
		if s == subject {
			n++
		}
	}
	return n
}

func TestHandleWalletCompleted_MissingDelegationsUnknownSkipsObserved(t *testing.T) {
	h, db, cache, bus := setupDelegationHandler(t)
	userID := uuid.New()
	scanID := uuid.New()
	address := "0xabc0000000000000000000000000000000000001"
	if err := storage.NewWalletWriter(db).OnStarted(scanID, userID, address); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	err := h.HandleCompleted(&nats.ScanCompletedMessage{
		ScanID:  scanID,
		Kind:    "wallet",
		UserID:  userID,
		Address: address,
		Result: map[string]any{
			"address":     address,
			"type":        "unknown",
			"algorithm":   "",
			"nist_level":  0,
			"is_eoa":      false,
			"key_exposed": false,
			"risk_score":  0,
			"networks":    []any{},
			"connections": []any{},
		},
	})
	if err != nil {
		t.Fatalf("HandleCompleted: %v", err)
	}

	var stored domain.ScanResultEntity
	if err := db.Where("id = ?", scanID).First(&stored).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if stored.Type != domain.AccountTypeUnknown {
		t.Fatalf("type = %q", stored.Type)
	}
	if stored.Delegations != "[]" {
		t.Fatalf("postgres delegations = %q", stored.Delegations)
	}
	if stored.Algorithm != "" || stored.IsEOA || stored.NISTLevel != 0 {
		t.Fatalf("posture stored = %+v", stored)
	}

	raw, ok := cache.values[walletCacheKey(userID, address)]
	if !ok {
		t.Fatal("redis wallet result missing")
	}
	var cached domain.ScanResult
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		t.Fatalf("redis json: %v", err)
	}
	if cached.Delegations == nil || len(cached.Delegations) != 0 {
		t.Fatalf("redis delegations = %#v", cached.Delegations)
	}
	if cached.Type != domain.AccountTypeUnknown {
		t.Fatalf("redis type = %q", cached.Type)
	}
	if publishedSubjects(bus, nats.SubjectDiscoveryWalletObserved) != 0 {
		t.Fatalf("wallet.observed published: %v", bus.subjects)
	}
	if publishedSubjects(bus, nats.SubjectScanReady) != 1 {
		t.Fatalf("scan.ready count, subjects=%v", bus.subjects)
	}
}

func TestHandleWalletCompleted_DelegationsRoundTripAndEOAObserved(t *testing.T) {
	h, db, cache, bus := setupDelegationHandler(t)
	userID := uuid.New()
	scanID := uuid.New()
	address := "0xabc0000000000000000000000000000000000002"
	target := "0x1111111111111111111111111111111111111111"
	if err := storage.NewWalletWriter(db).OnStarted(scanID, userID, address); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	err := h.HandleCompleted(&nats.ScanCompletedMessage{
		ScanID:  scanID,
		Kind:    "wallet",
		UserID:  userID,
		Address: address,
		Result: map[string]any{
			"address":     address,
			"type":        "EOA",
			"algorithm":   "ECDSA-secp256k1",
			"nist_level":  1,
			"is_eoa":      true,
			"is_erc4337":  false,
			"risk_score":  1,
			"networks":    []any{"ethereum-mainnet"},
			"connections": []any{},
			"delegations": []any{map[string]any{"chain_id": 1, "delegated_address": target}},
		},
	})
	if err != nil {
		t.Fatalf("HandleCompleted: %v", err)
	}

	var stored domain.ScanResultEntity
	if err := db.Where("id = ?", scanID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	out := stored.ToScanResult()
	if len(out.Delegations) != 1 || out.Delegations[0].ChainID != 1 || out.Delegations[0].DelegatedAddress != target {
		t.Fatalf("postgres delegations = %#v raw=%s", out.Delegations, stored.Delegations)
	}

	raw := cache.values[walletCacheKey(userID, address)]
	var cached domain.ScanResult
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		t.Fatal(err)
	}
	if len(cached.Delegations) != 1 || cached.Delegations[0].DelegatedAddress != target {
		t.Fatalf("redis delegations = %#v", cached.Delegations)
	}

	if publishedSubjects(bus, nats.SubjectDiscoveryWalletObserved) != 1 {
		t.Fatalf("subjects=%v", bus.subjects)
	}
	var ev struct {
		Payload struct {
			AccountKind      string `json:"account_kind"`
			CurrentAlgorithm string `json:"current_algorithm"`
		} `json:"payload"`
	}
	var observed []byte
	for i, subject := range bus.subjects {
		if subject == nats.SubjectDiscoveryWalletObserved {
			observed = bus.payloads[i]
		}
	}
	if err := json.Unmarshal(observed, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Payload.AccountKind != "eoa" || ev.Payload.CurrentAlgorithm != "secp256k1_ecrecover" {
		t.Fatalf("observed payload = %+v", ev.Payload)
	}
}

func TestHandleWalletCompleted_LegacyAAStillDecoded(t *testing.T) {
	h, db, _, bus := setupDelegationHandler(t)
	userID := uuid.New()
	scanID := uuid.New()
	address := "0xabc0000000000000000000000000000000000003"
	if err := storage.NewWalletWriter(db).OnStarted(scanID, userID, address); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	err := h.HandleCompleted(&nats.ScanCompletedMessage{
		ScanID:  scanID,
		Kind:    "wallet",
		UserID:  userID,
		Address: address,
		Result: map[string]any{
			"address":     address,
			"type":        "AA",
			"algorithm":   "ECDSA-secp256k1",
			"nist_level":  1,
			"is_eoa":      false,
			"is_erc4337":  true,
			"networks":    []any{"ethereum-mainnet"},
			"connections": []any{},
		},
	})
	if err != nil {
		t.Fatalf("HandleCompleted: %v", err)
	}

	var stored domain.ScanResultEntity
	if err := db.Where("id = ?", scanID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Type != domain.AccountTypeAA || !stored.IsERC4337 {
		t.Fatalf("stored = %+v", stored)
	}
	if stored.Delegations != "[]" {
		t.Fatalf("delegations = %q", stored.Delegations)
	}
	if publishedSubjects(bus, nats.SubjectDiscoveryWalletObserved) != 1 {
		t.Fatalf("legacy AA must still publish wallet.observed, subjects=%v", bus.subjects)
	}
}

var _ redisconn.Connection = (*memRedis)(nil)
var _ nats.Connection = (*recordingNATS)(nil)
