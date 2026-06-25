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

func TestPostgresStore_persistDraftLifecycle(t *testing.T) {
	db := openTestDB(t)
	store := NewPostgresStore(db)

	scope := OwnerScope{UserID: "user-a", TenantID: "t1"}
	scanID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	draftID := uuid.New()
	wallet := "0x742d35cc6634c0532925a3b844bc454e4438f44e"

	_, err := store.SaveDraft(scope, draftID, &scanID, map[string]any{
		"policy_context": map[string]any{
			"wallet_address": wallet,
			"wallet_type":    "eoa",
		},
		"mode": "strict",
	})
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	verifiedAt := time.Date(2026, 6, 10, 12, 1, 0, 0, time.UTC)
	result, err := store.PersistDraftOnce(scope, draftID, PersistDraftInput{
		WalletAddress: wallet,
		ChainID:       1,
		ScanID:        scanID,
		VerifiedAt:    verifiedAt,
	})
	if err != nil {
		t.Fatalf("PersistDraftOnce: %v", err)
	}
	if result.PolicyID == uuid.Nil || result.DraftID != draftID || result.ScanID != scanID {
		t.Fatalf("unexpected result: %#v", result)
	}

	if _, err := store.GetDraft(scope, draftID); !errors.Is(err, ErrDraftNotFound) {
		t.Fatalf("expected draft removed after persist, got %v", err)
	}
	policy, err := store.GetPolicy(scope, result.PolicyID)
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if policy.Payload["ownership_status"] != "verified" {
		t.Fatalf("ownership_status = %#v", policy.Payload["ownership_status"])
	}
	if _, ok := policy.Payload["signature"]; ok {
		t.Fatal("raw signature must not be stored")
	}
}

func TestPostgresStore_persistReplayReturnsAlreadyPersisted(t *testing.T) {
	db := openTestDB(t)
	store := NewPostgresStore(db)

	scope := OwnerScope{UserID: "user-a", TenantID: "t1"}
	scanID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	draftID := uuid.New()
	wallet := "0x742d35cc6634c0532925a3b844bc454e4438f44e"

	_, err := store.SaveDraft(scope, draftID, &scanID, map[string]any{
		"policy_context": map[string]any{"wallet_address": wallet, "wallet_type": "eoa"},
	})
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	in := PersistDraftInput{WalletAddress: wallet, ChainID: 1, ScanID: scanID, VerifiedAt: time.Now().UTC()}
	if _, err := store.PersistDraftOnce(scope, draftID, in); err != nil {
		t.Fatalf("first persist: %v", err)
	}
	if _, err := store.PersistDraftOnce(scope, draftID, in); !errors.Is(err, ErrDraftAlreadyPersisted) {
		t.Fatalf("replay: want ErrDraftAlreadyPersisted, got %v", err)
	}
}

func TestPostgresStore_persistRetryReusesPolicyID(t *testing.T) {
	db := openTestDB(t)
	store := NewPostgresStore(db)

	scope := OwnerScope{UserID: "user-a", TenantID: "t1"}
	scanID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	draftID := uuid.New()
	wallet := "0x742d35cc6634c0532925a3b844bc454e4438f44e"
	reservedPolicyID := uuid.New()

	_, err := store.SaveDraft(scope, draftID, &scanID, map[string]any{
		"policy_context": map[string]any{"wallet_address": wallet, "wallet_type": "eoa"},
	})
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := db.Create(&domain.DraftPersistStateEntity{
		DraftID:   draftID,
		PolicyID:  reservedPolicyID,
		Completed: false,
		UserID:    scope.UserID,
		TenantID:  scope.TenantID,
	}).Error; err != nil {
		t.Fatalf("seed draft_persist_state: %v", err)
	}

	in := PersistDraftInput{WalletAddress: wallet, ChainID: 1, ScanID: scanID, VerifiedAt: time.Now().UTC()}
	result, err := store.PersistDraftOnce(scope, draftID, in)
	if err != nil {
		t.Fatalf("retry persist: %v", err)
	}
	if result.PolicyID != reservedPolicyID {
		t.Fatalf("policy_id = %s want %s", result.PolicyID, reservedPolicyID)
	}
}

func TestPostgresStore_supersedesPriorPolicyForScan(t *testing.T) {
	db := openTestDB(t)
	store := NewPostgresStore(db)

	scope := OwnerScope{UserID: "user-a", TenantID: "t1"}
	scanID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	wallet := "0x742d35cc6634c0532925a3b844bc454e4438f44e"
	oldPolicyID := uuid.New()

	now := time.Now().UTC()
	oldDraftID := uuid.New()
	if err := db.Create(&domain.CryptoPolicyEntity{
		ID:                  oldPolicyID,
		UserID:              scope.UserID,
		TenantID:            scope.TenantID,
		ScanID:              scanID,
		DraftID:             oldDraftID,
		WalletAddress:       wallet,
		ChainID:             1,
		Payload:             domain.JSONMap{"policy_template_id": "tmpl-pq-ready-v2"},
		OwnershipStatus:     "verified",
		WalletControlMethod: "eoa_signature",
		Status:              domain.CryptoPolicyStatusPersisted,
		PersistedAt:         now,
		CreatedAt:           now,
		UpdatedAt:           now,
	}).Error; err != nil {
		t.Fatalf("seed old policy: %v", err)
	}

	draftID := uuid.New()
	_, err := store.SaveDraft(scope, draftID, &scanID, map[string]any{
		"policy_template_id": "tmpl-hybrid-classic-v1",
		"policy_context":     map[string]any{"wallet_address": wallet, "wallet_type": "eoa"},
	})
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	result, err := store.PersistDraftOnce(scope, draftID, PersistDraftInput{
		WalletAddress: wallet,
		ChainID:       1,
		ScanID:        scanID,
		VerifiedAt:    time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PersistDraftOnce replacement: %v", err)
	}
	if result.PolicyID == oldPolicyID {
		t.Fatal("replacement persist should create a new policy id")
	}

	list, err := store.ListPersistedPoliciesForScan(scope, scanID, 20, 0)
	if err != nil {
		t.Fatalf("ListPersistedPoliciesForScan: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("want exactly one active persisted policy per scan, got %d", len(list.Items))
	}
	if list.Items[0].ID != result.PolicyID {
		t.Fatalf("listed policy id = %s want %s", list.Items[0].ID, result.PolicyID)
	}
	if _, err := store.GetPolicy(scope, oldPolicyID); !errors.Is(err, ErrPolicyNotFound) {
		t.Fatalf("prior policy should be superseded, got %v", err)
	}
}

func TestPostgresStore_walletAndScanReferenceCounts(t *testing.T) {
	db := openTestDB(t)
	store := NewPostgresStore(db)

	scope := OwnerScope{UserID: "user-b", TenantID: ""}
	scanID := uuid.New()
	draftID := uuid.New()
	wallet := "0x742d35cc6634c0532925a3b844bc454e4438f44e"

	_, err := store.SaveDraft(scope, draftID, &scanID, map[string]any{
		"policy_context": map[string]any{"wallet_address": wallet},
	})
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	walletCounts, err := store.CountPoliciesByWallet(scope, wallet)
	if err != nil {
		t.Fatalf("CountPoliciesByWallet: %v", err)
	}
	if !walletCounts.Exists || walletCounts.DraftCount != 1 || walletCounts.PolicyCount != 0 {
		t.Fatalf("unexpected wallet counts: %#v", walletCounts)
	}

	scanCounts, err := store.CountPoliciesByScan(scope, scanID)
	if err != nil {
		t.Fatalf("CountPoliciesByScan before persist: %v", err)
	}
	if scanCounts.Referenced || scanCounts.Count != 0 {
		t.Fatalf("unexpected scan counts before persist: %#v", scanCounts)
	}

	if _, err := store.PersistDraftOnce(scope, draftID, PersistDraftInput{
		WalletAddress: wallet,
		ChainID:       1,
		ScanID:        scanID,
		VerifiedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PersistDraftOnce: %v", err)
	}

	walletCounts, err = store.CountPoliciesByWallet(scope, wallet)
	if err != nil {
		t.Fatalf("CountPoliciesByWallet after persist: %v", err)
	}
	if walletCounts.PolicyCount != 1 || walletCounts.DraftCount != 0 {
		t.Fatalf("unexpected wallet counts after persist: %#v", walletCounts)
	}

	scanCounts, err = store.CountPoliciesByScan(scope, scanID)
	if err != nil {
		t.Fatalf("CountPoliciesByScan after persist: %v", err)
	}
	if !scanCounts.Referenced || scanCounts.Count != 1 {
		t.Fatalf("unexpected scan counts after persist: %#v", scanCounts)
	}
}

func TestPostgresStore_deleteDraftAndPolicy(t *testing.T) {
	db := openTestDB(t)
	store := NewPostgresStore(db)

	scope := OwnerScope{UserID: "user-del", TenantID: "t-del"}
	scanID := uuid.New()
	draftID := uuid.New()
	wallet := "0x742d35cc6634c0532925a3b844bc454e4438f44e"

	_, err := store.SaveDraft(scope, draftID, &scanID, map[string]any{
		"policy_context": map[string]any{"wallet_address": wallet},
	})
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := store.DeleteDraft(scope, draftID); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	if _, err := store.GetDraft(scope, draftID); !errors.Is(err, ErrDraftNotFound) {
		t.Fatalf("GetDraft after delete: want ErrDraftNotFound, got %v", err)
	}

	draftID2 := uuid.New()
	_, err = store.SaveDraft(scope, draftID2, &scanID, map[string]any{
		"policy_context": map[string]any{"wallet_address": wallet},
	})
	if err != nil {
		t.Fatalf("SaveDraft for persist: %v", err)
	}
	result, err := store.PersistDraftOnce(scope, draftID2, PersistDraftInput{
		WalletAddress: wallet,
		ChainID:       1,
		ScanID:        scanID,
		VerifiedAt:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("PersistDraftOnce: %v", err)
	}
	if err := store.DeletePolicy(scope, result.PolicyID); err != nil {
		t.Fatalf("DeletePolicy: %v", err)
	}
	if _, err := store.GetPolicy(scope, result.PolicyID); !errors.Is(err, ErrPolicyNotFound) {
		t.Fatalf("GetPolicy after delete: want ErrPolicyNotFound, got %v", err)
	}
	scanCounts, err := store.CountPoliciesByScan(scope, scanID)
	if err != nil {
		t.Fatalf("CountPoliciesByScan after delete: %v", err)
	}
	if scanCounts.Count != 0 {
		t.Fatalf("count after delete = %d, want 0", scanCounts.Count)
	}
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
		_ = db.Exec("TRUNCATE draft_persist_state, crypto_policies, crypto_policy_drafts RESTART IDENTITY CASCADE").Error
	})
	return db
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
