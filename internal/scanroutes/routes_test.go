package scanroutes_test

import (
	"strings"
	"testing"

	"cafe-persistence/internal/scanroutes"
)

func TestV1BaseIsInternalOnly(t *testing.T) {
	if strings.HasPrefix(scanroutes.V1Base, "/api/") {
		t.Fatal("internal scan API must not use public /api prefix")
	}
	if !strings.HasPrefix(scanroutes.V1Base, "/internal/") {
		t.Fatalf("expected /internal/ prefix, got %q", scanroutes.V1Base)
	}
}

func TestJoinPrefixesV1Base(t *testing.T) {
	got := scanroutes.Join(scanroutes.PendingWallet)
	want := scanroutes.V1Base + scanroutes.PendingWallet
	if got != want {
		t.Fatalf("Join: got %q want %q", got, want)
	}
}

func TestRouteLiteralsAreRelative(t *testing.T) {
	for _, route := range []string{
		scanroutes.PendingWallet,
		scanroutes.PendingTLS,
		scanroutes.PendingByScanID,
		scanroutes.WalletScans,
		scanroutes.WalletScanByID,
		scanroutes.TLSScans,
		scanroutes.TLSScansDefaults,
		scanroutes.TLSScanByID,
		scanroutes.LedgerUsage,
	} {
		if strings.HasPrefix(route, scanroutes.V1Base) {
			t.Fatalf("openapi-relative route %q must not include V1Base", route)
		}
		if !strings.HasPrefix(route, "/") {
			t.Fatalf("route %q must start with /", route)
		}
	}
}
