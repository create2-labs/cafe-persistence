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
		scanroutes.WalletScanTransition,
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

func TestScanV1OpenAPI_WalletScanRowDelegations(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	components := spec["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	row := schemas["WalletScanRow"].(map[string]any)
	props := row["properties"].(map[string]any)

	delegations, ok := props["delegations"].(map[string]any)
	if !ok {
		t.Fatal("WalletScanRow.delegations missing")
	}
	if delegations["type"] != "string" {
		t.Fatalf("delegations type = %#v, want string", delegations["type"])
	}
	if delegations["description"] != "JSON-encoded delegation array stored in Postgres." {
		t.Fatalf("delegations description = %#v", delegations["description"])
	}
	erc4337, ok := props["is_erc4337"].(map[string]any)
	if !ok {
		t.Fatal("WalletScanRow.is_erc4337 must remain")
	}
	if erc4337["type"] != "boolean" {
		t.Fatalf("is_erc4337 type = %#v, want boolean", erc4337["type"])
	}
}

func TestScanV1OpenAPI_WalletScanRowPublicKeyRecovery(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	components := spec["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	row := schemas["WalletScanRow"].(map[string]any)
	props := row["properties"].(map[string]any)

	if _, ok := props["first_seen"]; ok {
		t.Fatal("WalletScanRow.first_seen must be absent")
	}
	if _, ok := props["last_seen"]; ok {
		t.Fatal("WalletScanRow.last_seen must be absent")
	}
	recovery, ok := props["public_key_recovery"].(map[string]any)
	if !ok {
		t.Fatal("WalletScanRow.public_key_recovery missing")
	}
	if recovery["type"] != "string" {
		t.Fatalf("public_key_recovery type = %#v, want string", recovery["type"])
	}
	enumVals, ok := recovery["enum"].([]any)
	if !ok {
		t.Fatal("public_key_recovery.enum missing")
	}
	have := make(map[string]bool, len(enumVals))
	for _, v := range enumVals {
		have[v.(string)] = true
	}
	for _, expected := range []string{"", "not_required", "recovered", "unresolved"} {
		if !have[expected] {
			t.Fatalf("public_key_recovery missing %q", expected)
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

func TestScanV1OpenAPI_WalletTransition(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	paths := spec["paths"].(map[string]any)
	path, ok := paths[scanroutes.WalletScanTransition].(map[string]any)
	if !ok {
		t.Fatal("transition path missing")
	}
	post := path["post"].(map[string]any)
	if post["operationId"] != "applyWalletTransition" {
		t.Fatalf("operationId = %v", post["operationId"])
	}
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	quota := schemas["WalletTransitionQuotaAction"].(map[string]any)["enum"].([]any)
	if len(quota) != 3 || quota[0] != "none" || quota[1] != "keep" || quota[2] != "release" {
		t.Fatalf("quota_action enum = %#v", quota)
	}
	outcomes := schemas["WalletTransitionOutcome"].(map[string]any)["enum"].([]any)
	if len(outcomes) != 6 {
		t.Fatalf("outcome enum = %#v", outcomes)
	}
	resp := schemas["WalletTransitionResponse"].(map[string]any)
	required := map[string]bool{}
	for _, name := range resp["required"].([]any) {
		required[name.(string)] = true
	}
	for _, name := range []string{"outcome", "projection_ready", "ready_published"} {
		if !required[name] {
			t.Fatalf("response required missing %s", name)
		}
	}
	if required["status"] {
		t.Fatal("status must stay optional on retryable_error and rejected")
	}
	req := schemas["WalletTransitionRequest"].(map[string]any)["properties"].(map[string]any)
	if _, ok := req["error"]; !ok {
		t.Fatal("request error property missing")
	}
	errDesc, _ := req["error"].(map[string]any)["description"].(string)
	if !strings.Contains(errDesc, "SUCCESS") {
		t.Fatal("error must be documented as absent on SUCCESS")
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
