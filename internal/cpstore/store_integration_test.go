//go:build integration

package cpstore

import (
	"errors"
	"os"
	"testing"
	"time"

	"cafe-persistence/internal/cpddl"
	"cafe-persistence/internal/domain"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresStore_createPolicyLifecycle(t *testing.T) {
	db := openTestDB(t)
	store := NewPostgresStore(db)

	scope := OwnerScope{UserID: "user-a", TenantID: "t1"}
	scanID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	wallet := "0x742d35cc6634c0532925a3b844bc454e4438f44e"
	verifiedAt := time.Date(2026, 6, 10, 12, 1, 0, 0, time.UTC)
	issued := verifiedAt.Add(-2 * time.Minute)
	expires := verifiedAt.Add(8 * time.Minute)
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	result, err := store.CreatePolicy(scope, CreatePolicyInput{
		ScanID:                  scanID,
		WalletAddress:           wallet,
		ChainID:                 1,
		Payload:                 map[string]any{"mode": "strict", "signature": "raw-should-strip"},
		PayloadSHA256:           sha,
		SignedMessageHash:       "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		WalletControlMethod:     "eoa_signature",
		WalletControlVerifiedAt: verifiedAt,
		ChallengeIssuedAt:       &issued,
		ChallengeExpiresAt:      &expires,
	})
	if err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}
	if result.PolicyID == uuid.Nil || result.ScanID != scanID || result.PayloadSHA256 != sha {
		t.Fatalf("unexpected result: %#v", result)
	}

	policy, err := store.GetPolicy(scope, result.PolicyID)
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if policy.PayloadSHA256 != sha {
		t.Fatalf("payload_sha256 = %q", policy.PayloadSHA256)
	}
	if policy.SignedMessageHash == "" || policy.ChallengeIssuedAt == nil || policy.ChallengeExpiresAt == nil {
		t.Fatalf("audit fields missing: %#v", policy)
	}
	if _, ok := policy.Payload["signature"]; ok {
		t.Fatal("raw signature must not be stored")
	}
}

func TestPostgresStore_createPolicyW1UniqueViolation(t *testing.T) {
	db := openTestDB(t)
	store := NewPostgresStore(db)

	scope := OwnerScope{UserID: "user-a", TenantID: "t1"}
	scanID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	wallet := "0x742d35cc6634c0532925a3b844bc454e4438f44e"
	in := CreatePolicyInput{
		ScanID:                  scanID,
		WalletAddress:           wallet,
		ChainID:                 1,
		Payload:                 map[string]any{"mode": "strict"},
		PayloadSHA256:           "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		WalletControlVerifiedAt: time.Now().UTC(),
	}
	if _, err := store.CreatePolicy(scope, in); err != nil {
		t.Fatalf("first create: %v", err)
	}
	in.PayloadSHA256 = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	in.ScanID = uuid.New()
	if _, err := store.CreatePolicy(scope, in); !errors.Is(err, ErrPolicyAlreadyExists) {
		t.Fatalf("second create: want ErrPolicyAlreadyExists, got %v", err)
	}
}

func TestPostgresStore_deleteThenRecreateAllowsW1(t *testing.T) {
	db := openTestDB(t)
	store := NewPostgresStore(db)

	scope := OwnerScope{UserID: "user-del", TenantID: "t-del"}
	scanID := uuid.New()
	wallet := "0x742d35cc6634c0532925a3b844bc454e4438f44e"
	in := CreatePolicyInput{
		ScanID:                  scanID,
		WalletAddress:           wallet,
		ChainID:                 1,
		Payload:                 map[string]any{"mode": "strict"},
		PayloadSHA256:           "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		WalletControlVerifiedAt: time.Now().UTC(),
	}
	first, err := store.CreatePolicy(scope, in)
	if err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}
	if err := store.DeletePolicy(scope, first.PolicyID); err != nil {
		t.Fatalf("DeletePolicy: %v", err)
	}
	in.PayloadSHA256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	in.ScanID = uuid.New()
	if _, err := store.CreatePolicy(scope, in); err != nil {
		t.Fatalf("recreate after delete: %v", err)
	}
}

func TestPostgresStore_walletAndScanReferenceCounts(t *testing.T) {
	db := openTestDB(t)
	store := NewPostgresStore(db)

	scope := OwnerScope{UserID: "user-b", TenantID: ""}
	scanID := uuid.New()
	wallet := "0x742d35cc6634c0532925a3b844bc454e4438f44e"

	walletCounts, err := store.CountPoliciesByWallet(scope, wallet)
	if err != nil {
		t.Fatalf("CountPoliciesByWallet: %v", err)
	}
	if walletCounts.Exists || walletCounts.PolicyCount != 0 {
		t.Fatalf("unexpected wallet counts: %#v", walletCounts)
	}

	if _, err := store.CreatePolicy(scope, CreatePolicyInput{
		ScanID:                  scanID,
		WalletAddress:           wallet,
		ChainID:                 1,
		Payload:                 map[string]any{"mode": "strict"},
		PayloadSHA256:           "1111111111111111111111111111111111111111111111111111111111111111",
		WalletControlVerifiedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	walletCounts, err = store.CountPoliciesByWallet(scope, wallet)
	if err != nil {
		t.Fatalf("CountPoliciesByWallet after create: %v", err)
	}
	if !walletCounts.Exists || walletCounts.PolicyCount != 1 {
		t.Fatalf("unexpected wallet counts after create: %#v", walletCounts)
	}

	scanCounts, err := store.CountPoliciesByScan(scope, scanID)
	if err != nil {
		t.Fatalf("CountPoliciesByScan after create: %v", err)
	}
	if !scanCounts.Referenced || scanCounts.Count != 1 {
		t.Fatalf("unexpected scan counts after create: %#v", scanCounts)
	}
}

func TestPostgresStore_migrateDropsDraftTables(t *testing.T) {
	db := openTestDB(t)
	for _, table := range []string{"crypto_policy_drafts", "draft_persist_state"} {
		var exists bool
		if err := db.Raw(`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = ?
		)`, table).Scan(&exists).Error; err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if exists {
			t.Fatalf("legacy table %s must be dropped after MigrateCPSchema", table)
		}
	}
	var hasDraftID bool
	if err := db.Raw(`SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'crypto_policies' AND column_name = 'draft_id'
	)`).Scan(&hasDraftID).Error; err != nil {
		t.Fatalf("check draft_id column: %v", err)
	}
	if hasDraftID {
		t.Fatal("crypto_policies.draft_id must be dropped")
	}
	_ = domain.CryptoPolicyStatusPersisted
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		host := envOr("POSTGRES_HOST", "127.0.0.1")
		port := envOr("POSTGRES_PORT", "5432")
		user := envOr("POSTGRES_USER", "cafe")
		pass := envOr("POSTGRES_PASSWORD", "cafe")
		dbname := envOr("POSTGRES_DATABASE", "cafe")
		sslmode := envOr("POSTGRES_SSLMODE", "disable")
		dsn = "host=" + host + " port=" + port + " user=" + user + " password=" + pass + " dbname=" + dbname + " sslmode=" + sslmode
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if err := cpddl.MigrateCPSchema(db); err != nil {
		t.Fatalf("MigrateCPSchema: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("TRUNCATE crypto_policies RESTART IDENTITY CASCADE").Error
	})
	return db
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
