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

func TestNormalizePolicyPayload_stripsSignatureAndClientHash(t *testing.T) {
	t.Parallel()
	scanID := uuid.New().String()
	wallet := "0x742d35cc6634c0532925a3b844bc454e4438f44e"
	verifiedAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	payload := domain.NormalizePolicyPayload(map[string]any{
		"signed_message": "msg",
		"signature":      "sig",
		"payload_sha256": "deadbeef",
		"draft_id":       "should-go",
		"mode":           "strict",
	}, scanID, wallet, 1, verifiedAt)
	if _, ok := payload["signature"]; ok {
		t.Fatal("signature must be stripped")
	}
	if _, ok := payload["signed_message"]; ok {
		t.Fatal("signed_message must be stripped")
	}
	if _, ok := payload["payload_sha256"]; ok {
		t.Fatal("client payload_sha256 must be stripped from JSON payload")
	}
	if _, ok := payload["draft_id"]; ok {
		t.Fatal("draft_id must not be stored in payload")
	}
	if payload["ownership_status"] != "verified" {
		t.Fatalf("ownership_status = %#v", payload["ownership_status"])
	}
}
