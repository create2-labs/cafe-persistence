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
	"cafe-persistence/pkg/scan"

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

func assertNoWalletProjection(t *testing.T, cache *memRedis, bus *recordingNATS) {
	t.Helper()
	if len(cache.values) != 0 || len(bus.subjects) != 0 {
		t.Fatalf("wallet projection redis=%v nats=%v", cache.values, bus.subjects)
	}
}

func assertCompletedPayloadNotStored(t *testing.T, db *gorm.DB, scanID uuid.UUID, cache *memRedis, bus *recordingNATS) {
	t.Helper()
	var stored domain.ScanResultEntity
	if err := db.Where("id = ?", scanID).First(&stored).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if stored.Status != scan.StateRUNNING || stored.Type != "" || stored.Delegations != "" || stored.Algorithm != "" || stored.IsERC4337 {
		t.Fatalf("completed payload stored: %+v", stored)
	}
	assertNoWalletProjection(t, cache, bus)
}

func TestHandleWalletCompleted_MissingDelegationsUnknownSkipsObserved(t *testing.T) {
	h, db, cache, bus := setupDelegationHandler(t)
	userID := uuid.New()
	scanID := uuid.New()
	address := "0xabc0000000000000000000000000000000000001"
	if err := storage.NewWalletWriter(db).OnStarted(scanID, userID, address); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	err := h.applyCompleted(&scanCompleted{
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
		t.Fatalf("applyCompleted: %v", err)
	}
	assertCompletedPayloadNotStored(t, db, scanID, cache, bus)
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

	err := h.applyCompleted(&scanCompleted{
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
		t.Fatalf("applyCompleted: %v", err)
	}
	assertCompletedPayloadNotStored(t, db, scanID, cache, bus)
}

func TestHandleWalletCompleted_LegacyAAStillDecoded(t *testing.T) {
	h, db, cache, bus := setupDelegationHandler(t)
	userID := uuid.New()
	scanID := uuid.New()
	address := "0xabc0000000000000000000000000000000000003"
	if err := storage.NewWalletWriter(db).OnStarted(scanID, userID, address); err != nil {
		t.Fatalf("OnStarted: %v", err)
	}

	err := h.applyCompleted(&scanCompleted{
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
		t.Fatalf("applyCompleted: %v", err)
	}
	assertCompletedPayloadNotStored(t, db, scanID, cache, bus)
}

func TestWalletStartedDoesNotInsert(t *testing.T) {
	h, db, _, _ := setupDelegationHandler(t)
	scanID := uuid.New()
	if err := h.applyStarted(&scanStarted{
		ScanID: scanID, Kind: "wallet", UserID: uuid.New(), Address: "0xabc",
	}); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Model(&domain.ScanResultEntity{}).Where("id = ?", scanID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("scan.started inserted a wallet row")
	}
}

var _ redisconn.Connection = (*memRedis)(nil)
var _ nats.Connection = (*recordingNATS)(nil)
