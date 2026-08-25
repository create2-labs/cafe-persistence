package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cafe-persistence/internal/cproutes"
	"gopkg.in/yaml.v3"
)

// OpenAPI contract checks (openapi/internal/cp/v1.yaml) — RD-P3 policy-only.
func TestCpV1OpenAPI_RequiredPathsDocumented(t *testing.T) {
	spec := loadCpV1OpenAPISpec(t)
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths missing or invalid")
	}

	for _, route := range []string{
		cproutes.PolicyByID,
		cproutes.Policies,
		cproutes.ReferenceWallet,
		cproutes.ReferenceScan,
	} {
		if _, ok := paths[route]; !ok {
			t.Fatalf("openapi paths missing %q", route)
		}
	}
	for route := range paths {
		if strings.Contains(route, "draft") {
			t.Fatalf("draft path must be removed from OpenAPI: %q", route)
		}
	}
}

func TestCpV1OpenAPI_NoPublicAPIPrefix(t *testing.T) {
	spec := loadCpV1OpenAPISpec(t)
	paths := spec["paths"].(map[string]any)
	for route := range paths {
		if strings.HasPrefix(route, "/api/") {
			t.Fatalf("internal contract must not expose public path %q", route)
		}
	}
}

func TestCpV1OpenAPI_ServiceBearerSecurity(t *testing.T) {
	spec := loadCpV1OpenAPISpec(t)
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

func TestCpV1OpenAPI_OwnerHeadersDocumented(t *testing.T) {
	spec := loadCpV1OpenAPISpec(t)
	components := spec["components"].(map[string]any)
	params := components["parameters"].(map[string]any)
	for _, name := range []string{"UserIdHeader", "TenantIdHeader"} {
		if _, ok := params[name]; !ok {
			t.Fatalf("components.parameters missing %q", name)
		}
	}
}

func TestCpV1OpenAPI_RequiredSchemasDocumented(t *testing.T) {
	spec := loadCpV1OpenAPISpec(t)
	components := spec["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)

	for _, name := range []string{
		"CreatePolicyRequest",
		"CreatePolicyResponse",
		"PolicyRow",
		"WalletReferenceCount",
		"ScanReferenceCount",
		"PolicyStatus",
		"CpErrorResponse",
		"ServiceAuthError",
	} {
		if _, ok := schemas[name]; !ok {
			t.Fatalf("openapi schema missing %q", name)
		}
	}
	for _, banned := range []string{"DraftRow", "DraftUpsertBody", "PersistDraftRequest", "PersistDraftResponse"} {
		if _, ok := schemas[banned]; ok {
			t.Fatalf("draft schema %q must be removed", banned)
		}
	}
}

func TestCpV1OpenAPI_CreatePolicyDocumented(t *testing.T) {
	spec := loadCpV1OpenAPISpec(t)
	paths := spec["paths"].(map[string]any)
	policies := paths[cproutes.Policies].(map[string]any)
	post, ok := policies["post"].(map[string]any)
	if !ok {
		t.Fatal("POST /policies missing")
	}
	if post["operationId"] != "createPolicy" {
		t.Fatalf("unexpected operationId: %v", post["operationId"])
	}
	desc, _ := post["description"].(string)
	lower := strings.ToLower(desc)
	for _, needle := range []string{"payload_sha256", "409", "w1"} {
		if !strings.Contains(lower, needle) {
			t.Fatalf("createPolicy description missing %q", needle)
		}
	}

	components := spec["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	req := schemas["CreatePolicyRequest"].(map[string]any)
	props := req["properties"].(map[string]any)
	for _, field := range []string{
		"wallet_address", "chain_id", "scan_id", "payload", "payload_sha256", "wallet_control_verified_at",
	} {
		if _, ok := props[field]; !ok {
			t.Fatalf("CreatePolicyRequest missing %q", field)
		}
	}
	if _, ok := props["signature"]; ok {
		t.Fatal("CreatePolicyRequest must not include signature (wallet auth stays in CPM)")
	}
	row := schemas["PolicyRow"].(map[string]any)
	rowProps := row["properties"].(map[string]any)
	if _, ok := rowProps["payload_sha256"]; !ok {
		t.Fatal("PolicyRow must expose payload_sha256")
	}
	if _, ok := rowProps["draft_id"]; ok {
		t.Fatal("PolicyRow must not include draft_id")
	}
}

func TestCpV1OpenAPI_ReferenceEndpointsForW1W3(t *testing.T) {
	spec := loadCpV1OpenAPISpec(t)
	paths := spec["paths"].(map[string]any)

	wallet := paths[cproutes.ReferenceWallet].(map[string]any)
	getWallet, ok := wallet["get"].(map[string]any)
	if !ok {
		t.Fatal("GET references/wallet missing")
	}
	if getWallet["operationId"] != "countPoliciesByWallet" {
		t.Fatalf("unexpected wallet ref operationId: %v", getWallet["operationId"])
	}

	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	wref := schemas["WalletReferenceCount"].(map[string]any)
	wprops := wref["properties"].(map[string]any)
	if _, ok := wprops["draft_count"]; ok {
		t.Fatal("WalletReferenceCount must not include draft_count")
	}

	scan := paths[cproutes.ReferenceScan].(map[string]any)
	getScan, ok := scan["get"].(map[string]any)
	if !ok {
		t.Fatal("GET references/scan missing")
	}
	if getScan["operationId"] != "countPoliciesByScan" {
		t.Fatalf("unexpected scan ref operationId: %v", getScan["operationId"])
	}
}

func TestCpV1OpenAPI_DescriptionMentionsADR(t *testing.T) {
	spec := loadCpV1OpenAPISpec(t)
	info := spec["info"].(map[string]any)
	desc, _ := info["description"].(string)
	lower := strings.ToLower(desc)
	for _, needle := range []string{
		"cpm",
		"payload_sha256",
		"not",
		"nginx",
		"w1",
	} {
		if !strings.Contains(lower, needle) {
			t.Fatalf("info.description missing %q", needle)
		}
	}
	if strings.Contains(lower, "draft_id") && strings.Contains(lower, "idempotence key") {
		t.Fatal("description must not present draft_id as idempotence key")
	}
}

func loadCpV1OpenAPISpec(t *testing.T) map[string]any {
	t.Helper()
	path := filepath.Join("..", "..", "openapi", "internal", "cp", "v1.yaml")
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
