package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cafe-persistence/internal/cproutes"
	"gopkg.in/yaml.v3"
)

// PERS-D3b-spec OpenAPI contract checks (openapi/internal/cp/v1.yaml).
func TestCpV1OpenAPI_RequiredPathsDocumented(t *testing.T) {
	spec := loadCpV1OpenAPISpec(t)
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths missing or invalid")
	}

	for _, route := range []string{
		cproutes.DraftByID,
		cproutes.DraftPersist,
		cproutes.PolicyByID,
		cproutes.Policies,
		cproutes.ReferenceWallet,
		cproutes.ReferenceScan,
	} {
		if _, ok := paths[route]; !ok {
			t.Fatalf("openapi paths missing %q", route)
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
		"DraftRow",
		"DraftUpsertBody",
		"PersistDraftRequest",
		"PersistDraftResponse",
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
}

func TestCpV1OpenAPI_DraftOperations(t *testing.T) {
	spec := loadCpV1OpenAPISpec(t)
	paths := spec["paths"].(map[string]any)

	draft := paths[cproutes.DraftByID].(map[string]any)
	for _, method := range []string{"put", "get", "delete"} {
		if _, ok := draft[method]; !ok {
			t.Fatalf("draft route missing %s", method)
		}
	}
	put := draft["put"].(map[string]any)
	if put["operationId"] != "upsertDraft" {
		t.Fatalf("unexpected upsert operationId: %v", put["operationId"])
	}
}

func TestCpV1OpenAPI_PersistDraftIdempotenceDocumented(t *testing.T) {
	spec := loadCpV1OpenAPISpec(t)
	paths := spec["paths"].(map[string]any)
	persist := paths[cproutes.DraftPersist].(map[string]any)
	post, ok := persist["post"].(map[string]any)
	if !ok {
		t.Fatal("POST persist missing")
	}
	if post["operationId"] != "persistDraft" {
		t.Fatalf("unexpected operationId: %v", post["operationId"])
	}
	desc, _ := post["description"].(string)
	lower := strings.ToLower(desc)
	for _, needle := range []string{"idempotent", "draft_id", "409", "draft_already_persisted"} {
		if !strings.Contains(lower, needle) {
			t.Fatalf("persist description missing %q", needle)
		}
	}

	components := spec["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	req := schemas["PersistDraftRequest"].(map[string]any)
	props := req["properties"].(map[string]any)
	for _, field := range []string{"wallet_address", "chain_id", "scan_id", "wallet_control_verified_at"} {
		if _, ok := props[field]; !ok {
			t.Fatalf("PersistDraftRequest missing %q", field)
		}
	}
	if _, ok := props["signature"]; ok {
		t.Fatal("PersistDraftRequest must not include signature (wallet auth stays in CPM)")
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
		"pers-d3b-spec",
		"cpm",
		"draft_id",
		"not",
		"nginx",
	} {
		if !strings.Contains(lower, needle) {
			t.Fatalf("info.description missing %q", needle)
		}
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
