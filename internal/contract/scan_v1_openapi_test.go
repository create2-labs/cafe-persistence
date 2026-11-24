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
		scanroutes.WalletScanEnrichmentTransition,
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

// CA1.1, CA1.2, CA1.3, CA1.4: the row drops the replaced keys and carries the qualification (design 5.4).
func TestScanV1OpenAPI_WalletScanRowQualification(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	props := schemas["WalletScanRow"].(map[string]any)["properties"].(map[string]any)

	for _, removed := range []string{"type", "is_eoa", "is_erc4337", "networks", "delegations", "connections"} {
		if _, ok := props[removed]; ok {
			t.Errorf("WalletScanRow.%s must be removed", removed)
		}
	}
	for _, kept := range []string{"account_type", "observed_at", "chains", "unobserved_chain_ids", "anomalies", "key_exposed", "risk_score"} {
		if _, ok := props[kept]; !ok {
			t.Errorf("WalletScanRow.%s missing", kept)
		}
	}
	accountType, _ := props["account_type"].(map[string]any)
	enum, _ := accountType["enum"].([]any)
	want := []string{"", "eoa", "contract", "blank"}
	if len(enum) != len(want) {
		t.Fatalf("account_type enum = %#v, want %v", enum, want)
	}
	for i, w := range want {
		if enum[i] != w {
			t.Fatalf("account_type enum = %#v, want %v", enum, want)
		}
	}
}

// CA2.6: the chain schema allows a chain without nonce and carries key_exposed.
func TestScanV1OpenAPI_AddressQualificationChain(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	chain, ok := schemas["AddressQualificationChain"].(map[string]any)
	if !ok {
		t.Fatal("AddressQualificationChain schema missing")
	}
	props, _ := chain["properties"].(map[string]any)
	for _, name := range []string{"chain_id", "qualification", "nonce", "key_exposed"} {
		if _, ok := props[name]; !ok {
			t.Errorf("AddressQualificationChain.%s missing", name)
		}
	}
	required, _ := chain["required"].([]any)
	for _, r := range required {
		if r == "nonce" || r == "key_exposed" {
			t.Errorf("AddressQualificationChain.%v must be optional", r)
		}
	}
}

// CA1.3: RelatedAddress is a schema and WalletScanRow documents related_addresses (by-id route only).
func TestScanV1OpenAPI_RelatedAddress(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	related, ok := schemas["RelatedAddress"].(map[string]any)
	if !ok {
		t.Fatal("RelatedAddress schema missing")
	}
	props, _ := related["properties"].(map[string]any)
	for _, name := range []string{"chain_id", "role", "address", "creation_tx_hash", "enrichment_id"} {
		if _, ok := props[name]; !ok {
			t.Errorf("RelatedAddress.%s missing", name)
		}
	}
	required, _ := related["required"].([]any)
	for _, r := range required {
		if r == "creation_tx_hash" || r == "enrichment_id" {
			t.Errorf("RelatedAddress.%v must be optional", r)
		}
	}
	rowProps := schemas["WalletScanRow"].(map[string]any)["properties"].(map[string]any)
	if _, ok := rowProps["related_addresses"]; !ok {
		t.Error("WalletScanRow.related_addresses missing")
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

func schemaSet(t *testing.T, schemas map[string]any, name string) map[string]bool {
	t.Helper()
	def, ok := schemas[name].(map[string]any)
	if !ok {
		t.Fatalf("schema %q missing", name)
	}
	enum, ok := def["enum"].([]any)
	if !ok {
		t.Fatalf("%s.enum missing", name)
	}
	have := make(map[string]bool, len(enum))
	for _, v := range enum {
		have[v.(string)] = true
	}
	return have
}

func schemaProps(t *testing.T, schemas map[string]any, name string) (map[string]any, map[string]bool) {
	t.Helper()
	def, ok := schemas[name].(map[string]any)
	if !ok {
		t.Fatalf("schema %q missing", name)
	}
	props, _ := def["properties"].(map[string]any)
	required := map[string]bool{}
	list, _ := def["required"].([]any)
	for _, r := range list {
		required[r.(string)] = true
	}
	return props, required
}

// ADR 6.6, design 5.8: the route is POST, takes both path identifiers, and answers 200 or a protocol error.
func TestScanV1OpenAPI_EnrichmentTransitionRoute(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	paths := spec["paths"].(map[string]any)
	item, ok := paths[scanroutes.WalletScanEnrichmentTransition].(map[string]any)
	if !ok {
		t.Fatal("enrichment transition path missing")
	}
	post, ok := item["post"].(map[string]any)
	if !ok {
		t.Fatal("POST enrichment transition missing")
	}
	for _, other := range []string{"get", "put", "patch", "delete"} {
		if _, ok := item[other]; ok {
			t.Fatalf("unexpected %s on the enrichment transition path", other)
		}
	}

	params := spec["components"].(map[string]any)["parameters"].(map[string]any)
	pathParams := map[string]bool{}
	collect := func(list any) {
		items, _ := list.([]any)
		for _, raw := range items {
			p, _ := raw.(map[string]any)
			if ref, ok := p["$ref"].(string); ok {
				p, _ = params[strings.TrimPrefix(ref, "#/components/parameters/")].(map[string]any)
			}
			if p["in"] == "path" {
				pathParams[p["name"].(string)] = true
			}
		}
	}
	collect(item["parameters"])
	collect(post["parameters"])
	for _, name := range []string{"scan_id", "enrichment_id"} {
		if !pathParams[name] {
			t.Errorf("path parameter %q not documented", name)
		}
	}

	body := post["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if body["$ref"] != "#/components/schemas/EnrichmentTransitionRequest" {
		t.Errorf("request schema = %#v", body)
	}
	responses := post["responses"].(map[string]any)
	ok200, _ := responses["200"].(map[string]any)
	schema := ok200["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if schema["$ref"] != "#/components/schemas/EnrichmentTransitionResponse" {
		t.Errorf("200 schema = %#v", schema)
	}
	for _, code := range []string{"400", "401"} {
		if _, ok := responses[code]; !ok {
			t.Errorf("response %s not documented", code)
		}
	}
}

// Design 5.8 (OpenAPI schemas): vocabulary of the enrichment transition.
func TestScanV1OpenAPI_EnrichmentVocabulary(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)

	status := schemaSet(t, schemas, "EnrichmentStatus")
	for _, v := range []string{"PENDING", "RUNNING", "DONE", "ERROR"} {
		if !status[v] {
			t.Errorf("EnrichmentStatus missing %q", v)
		}
	}
	if !schemaSet(t, schemas, "EnrichmentFamily")["history"] {
		t.Error("EnrichmentFamily missing history")
	}
	outcome := schemaSet(t, schemas, "EnrichmentChainOutcome")
	for _, v := range []string{"pending", "observed", "not_observed", "error"} {
		if !outcome[v] {
			t.Errorf("EnrichmentChainOutcome missing %q", v)
		}
	}
}

// Design 5.8 (Body, Response): request, result, chain, and response shapes.
func TestScanV1OpenAPI_EnrichmentTransitionSchemas(t *testing.T) {
	spec := loadScanV1OpenAPISpec(t)
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)

	props, required := schemaProps(t, schemas, "EnrichmentTransitionRequest")
	for _, name := range []string{"transition_id", "user_id", "family", "source_states", "target_state", "quota_action", "result", "error"} {
		if _, ok := props[name]; !ok {
			t.Errorf("EnrichmentTransitionRequest.%s missing", name)
		}
	}
	for _, name := range []string{"transition_id", "user_id", "family", "source_states", "target_state", "quota_action"} {
		if !required[name] {
			t.Errorf("EnrichmentTransitionRequest.%s must be required", name)
		}
	}
	for _, name := range []string{"result", "error"} {
		if required[name] {
			t.Errorf("EnrichmentTransitionRequest.%s must be optional", name)
		}
	}

	props, required = schemaProps(t, schemas, "EnrichmentTransitionResult")
	for _, name := range []string{"chains", "related_addresses", "public_key", "transaction_hash", "exposed_network", "public_key_recovery"} {
		if _, ok := props[name]; !ok {
			t.Errorf("EnrichmentTransitionResult.%s missing", name)
		}
	}
	if !required["chains"] || len(required) != 1 {
		t.Errorf("EnrichmentTransitionResult required = %v, want only chains", required)
	}

	props, required = schemaProps(t, schemas, "EnrichmentChain")
	for _, name := range []string{"chain_id", "outcome", "code", "transaction_hash", "deployer", "creation_tx_hash"} {
		if _, ok := props[name]; !ok {
			t.Errorf("EnrichmentChain.%s missing", name)
		}
	}
	if !required["chain_id"] || !required["outcome"] || len(required) != 2 {
		t.Errorf("EnrichmentChain required = %v, want chain_id and outcome", required)
	}

	props, required = schemaProps(t, schemas, "EnrichmentTransitionResponse")
	out, _ := props["outcome"].(map[string]any)
	if out["$ref"] != "#/components/schemas/WalletTransitionOutcome" {
		t.Errorf("response outcome must reuse WalletTransitionOutcome, got %#v", out)
	}
	if _, ok := props["status"]; !ok {
		t.Error("EnrichmentTransitionResponse.status missing")
	}
	if !required["outcome"] || required["status"] {
		t.Errorf("response required = %v, want outcome only", required)
	}
	for _, name := range []string{"projection_ready", "ready_published"} {
		if _, ok := props[name]; ok {
			t.Errorf("EnrichmentTransitionResponse.%s must not exist", name)
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
