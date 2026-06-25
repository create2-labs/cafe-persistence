package cpstore

import (
	"testing"
	"time"

	"cafe-persistence/internal/domain"

	"github.com/google/uuid"
)

func TestNormalizeWalletTargetAddress_canonical(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"0x742D35Cc6634C0532925a3b844Bc454e4438f44e": "0x742d35cc6634c0532925a3b844bc454e4438f44e",
		"742d35cc6634c0532925a3b844bc454e4438f44e":  "0x742d35cc6634c0532925a3b844bc454e4438f44e",
	}
	for in, want := range cases {
		got, err := NormalizeWalletTargetAddress(in)
		if err != nil {
			t.Fatalf("NormalizeWalletTargetAddress(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("NormalizeWalletTargetAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWalletTargetFromPayload_policyContext(t *testing.T) {
	t.Parallel()
	addr := "0x742d35cc6634c0532925a3b844bc454e4438f44e"
	payload := map[string]any{
		"policy_context": map[string]any{
			"wallet_address": addr,
		},
	}
	if got := walletTargetFromPayload(payload); got != addr {
		t.Fatalf("walletTargetFromPayload = %q want %q", got, addr)
	}
}

func TestPolicyPayloadFromDraft_stripsSignatureFields(t *testing.T) {
	t.Parallel()
	draftID := uuid.New().String()
	scanID := uuid.New().String()
	wallet := "0x742d35cc6634c0532925a3b844bc454e4438f44e"
	verifiedAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	payload := domain.PolicyPayloadFromDraft(map[string]any{
		"signed_message": "msg",
		"signature":      "sig",
		"mode":           "strict",
	}, draftID, scanID, wallet, 1, verifiedAt)
	if _, ok := payload["signature"]; ok {
		t.Fatal("signature must be stripped")
	}
	if _, ok := payload["signed_message"]; ok {
		t.Fatal("signed_message must be stripped")
	}
	if payload["ownership_status"] != "verified" {
		t.Fatalf("ownership_status = %#v", payload["ownership_status"])
	}
}
