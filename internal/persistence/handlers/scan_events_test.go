package handlers

import (
	"testing"

	"cafe-persistence/pkg/scan"
)

// TestInvalidTransitionIsIgnored: duplicate scan.completed (second has current=SUCCESS) is ignored.
func TestInvalidTransitionIsIgnored(t *testing.T) {
	if scan.ValidTransition(scan.StateSUCCESS, scan.StateSUCCESS) {
		t.Error("duplicate completion: SUCCESS -> SUCCESS must be invalid")
	}
}

// TestDuplicateCompletedDoesNotChangeResult: duplicate scan.completed is rejected by ValidTransition.
func TestDuplicateCompletedDoesNotChangeResult(t *testing.T) {
	if scan.ValidTransition(scan.StateSUCCESS, scan.StateSUCCESS) {
		t.Error("duplicate scan.completed must be invalid (idempotent)")
	}
}

// TestScanStartedAfterSuccessIsIgnored: terminal state must not accept RUNNING (no downgrade).
func TestScanStartedAfterSuccessIsIgnored(t *testing.T) {
	if scan.ValidTransition(scan.StateSUCCESS, scan.StateRUNNING) {
		t.Error("scan.started after SUCCESS must be invalid (no downgrade)")
	}
	if !scan.IsTerminal(scan.StateSUCCESS) {
		t.Error("SUCCESS must be terminal")
	}
}
