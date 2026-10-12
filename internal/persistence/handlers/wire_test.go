package handlers

import (
	"encoding/json"
	"testing"

	"cafe-persistence/internal/domain"

	lifecycle "github.com/create2-labs/cafe-contracts/discovery/scanlifecycle/v1"
	"github.com/google/uuid"
)

func TestContractCompletedBecomesStorageView(t *testing.T) {
	scanID := uuid.New()
	userID := uuid.New()
	raw, err := json.Marshal(map[string]any{"address": "0xabc", "type": "EOA"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := newScanCompleted(&lifecycle.Completed{
		ScanID: scanID.String(), Kind: "wallet", UserID: userID.String(), Address: "0xabc",
		Result: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.ScanID != scanID || view.UserID != userID || view.Address != "0xabc" {
		t.Fatalf("view = %+v", view)
	}
	var result domain.ScanResult
	if err := (&ScanEventHandler{}).decodeResult(view.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.Address != "0xabc" || result.Type != domain.AccountTypeEOA {
		t.Fatalf("result = %+v", result)
	}
}

func TestContractCompletedRejectsBadScanID(t *testing.T) {
	_, err := newScanCompleted(&lifecycle.Completed{
		ScanID: "nope", Kind: "wallet", UserID: uuid.New().String(), Address: "0xabc",
	})
	if err == nil {
		t.Fatal("expected scan_id error")
	}
}
