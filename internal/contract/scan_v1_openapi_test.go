package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cafe-persistence/internal/scanroutes"
	"gopkg.in/yaml.v3"
)

// PERS-D3a-spec OpenAPI contract checks (openapi/internal/scan/v1.yaml).
func TestScanV1OpenAPI_RequiredPathsDocumented(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths missing or invalid")
	}

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
		if _, ok := paths[route]; !ok {
			t.Fatalf("openapi paths missing %q", route)
		}
	}
}

func TestScanV1OpenAPI_NoPublicAPIPrefix(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	paths := spec["paths"].(map[string]any)
	for route := range paths {
		if strings.HasPrefix(route, "/api/") {
			t.Fatalf("internal contract must not expose public path %q", route)
		}
	}
}

func TestScanV1OpenAPI_ServiceBearerSecurity(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("components missing")
	}
	schemes, ok := components["securitySchemes"].(map[string]any)
	if !ok {
		t.Fatal("securitySchemes missing")
	}
	bearer, ok := schemes["serviceBearer"].(map[string]any)
	if !ok {
		t.Fatal("serviceBearer security scheme missing")
	}
	if bearer["type"] != "http" || bearer["scheme"] != "bearer" {
		t.Fatalf("serviceBearer must be http bearer, got %#v", bearer)
	}
}

func TestScanV1OpenAPI_OwnerHeadersDocumented(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	components := spec["components"].(map[string]any)
	params := components["parameters"].(map[string]any)
	for _, name := range []string{"UserIdHeader", "TenantIdHeader"} {
		if _, ok := params[name]; !ok {
			t.Fatalf("components.parameters missing %q", name)
		}
	}
}

func TestScanV1OpenAPI_RequiredSchemasDocumented(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	components := spec["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)

	for _, name := range []string{
		"PendingScanRecord",
		"ReserveWalletPendingRequest",
		"PutTlsPendingRequest",
		"WalletScanRow",
		"TlsScanRow",
		"ScanLedgerUsage",
		"ScanUsageKind",
		"PersistenceScanStatus",
		"ServiceAuthError",
	} {
		if _, ok := schemas[name]; !ok {
			t.Fatalf("openapi schema missing %q", name)
		}
	}
}

func TestScanV1OpenAPI_PendingOperations(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	paths := spec["paths"].(map[string]any)

	walletPending := paths[scanroutes.PendingWallet].(map[string]any)
	if _, ok := walletPending["post"]; !ok {
		t.Fatal("POST pending wallet missing")
	}
	post := walletPending["post"].(map[string]any)
	if post["operationId"] != "reserveWalletPending" {
		t.Fatalf("unexpected operationId: %v", post["operationId"])
	}
	if _, ok := walletPending["get"]; !ok {
		t.Fatal("GET pending wallet by address missing")
	}
	if _, ok := walletPending["delete"]; !ok {
		t.Fatal("DELETE pending wallet reservation missing")
	}

	pendingByID := paths[scanroutes.PendingByScanID].(map[string]any)
	if _, ok := pendingByID["get"]; !ok {
		t.Fatal("GET pending by scan_id missing")
	}
	if _, ok := pendingByID["delete"]; !ok {
		t.Fatal("DELETE pending by scan_id missing")
	}
}

func TestScanV1OpenAPI_LedgerKindEnum(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	components := spec["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	kind := schemas["ScanUsageKind"].(map[string]any)
	enumVals, ok := kind["enum"].([]any)
	if !ok {
		t.Fatal("ScanUsageKind.enum missing")
	}
	have := make(map[string]bool, len(enumVals))
	for _, v := range enumVals {
		have[v.(string)] = true
	}
	for _, expected := range []string{"wallet", "endpoint"} {
		if !have[expected] {
			t.Fatalf("ScanUsageKind missing %q", expected)
		}
	}
}

func TestScanV1OpenAPI_DescriptionMentionsADR(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	info := spec["info"].(map[string]any)
	desc, _ := info["description"].(string)
	lower := strings.ToLower(desc)
	for _, needle := range []string{
		"pers-d3a-spec",
		"not",
		"nginx",
		"cafe-discovery",
	} {
		if !strings.Contains(lower, needle) {
			t.Fatalf("info.description missing %q", needle)
		}
	}
}

func loadScanV1OpenAPISpec(t *testing.T) map[string]any {
	t.Helper()
	path := filepath.Join("..", "..", "openapi", "internal", "scan", "v1.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read openapi: %v", err)
	}
	var spec map[string]any
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse openapi: %v", err)
	}
	return spec
}
