package cproutes_test

import (
	"strings"
	"testing"

	"cafe-persistence/internal/cproutes"
)

func TestV1BaseIsInternalOnly(t *testing.T) {
	if strings.HasPrefix(cproutes.V1Base, "/api/") {
		t.Fatal("internal CP API must not use public /api prefix")
	}
	if !strings.HasPrefix(cproutes.V1Base, "/internal/") {
		t.Fatalf("expected /internal/ prefix, got %q", cproutes.V1Base)
	}
}

func TestJoinPrefixesV1Base(t *testing.T) {
	got := cproutes.Join(cproutes.PolicyByID)
	want := cproutes.V1Base + cproutes.PolicyByID
	if got != want {
		t.Fatalf("Join: got %q want %q", got, want)
	}
}

func TestRouteLiteralsAreRelative(t *testing.T) {
	for _, route := range []string{
		cproutes.PolicyByID,
		cproutes.Policies,
		cproutes.ReferenceWallet,
		cproutes.ReferenceScan,
	} {
		if strings.HasPrefix(route, cproutes.V1Base) {
			t.Fatalf("openapi-relative route %q must not include V1Base", route)
		}
		if !strings.HasPrefix(route, "/") {
			t.Fatalf("route %q must start with /", route)
		}
	}
}

func TestNoDraftRoutes(t *testing.T) {
	for _, route := range []string{
		cproutes.PolicyByID,
		cproutes.Policies,
		cproutes.ReferenceWallet,
		cproutes.ReferenceScan,
	} {
		if strings.Contains(route, "draft") {
			t.Fatalf("draft route must not remain after RD-P3: %q", route)
		}
	}
}
