package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestFromScanResult_NilDelegationsRoundTrip(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"address":"0xabc","type":"unknown","algorithm":"","nist_level":0,"is_eoa":false,"is_erc4337":false}`)
	var decoded ScanResult
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Delegations != nil {
		t.Fatalf("missing delegations decoded as %#v, want nil", decoded.Delegations)
	}
	if decoded.Type != AccountTypeUnknown {
		t.Fatalf("type = %q", decoded.Type)
	}

	EnsureScanResultDelegations(&decoded)
	NormalizeScanResultWalletKind(&decoded)
	if decoded.Delegations == nil {
		t.Fatal("delegations must be an empty slice after normalize")
	}
	if decoded.Type != AccountTypeUnknown {
		t.Fatalf("type normalized to %q", decoded.Type)
	}

	entity := FromScanResult(uuid.New(), &decoded)
	if entity.Delegations != "[]" {
		t.Fatalf("stored delegations = %q, want []", entity.Delegations)
	}
	if entity.Type != AccountTypeUnknown {
		t.Fatalf("stored type = %q", entity.Type)
	}

	out := entity.ToScanResult()
	if out.Delegations == nil || len(out.Delegations) != 0 {
		t.Fatalf("round trip delegations = %#v", out.Delegations)
	}
	if out.Type != AccountTypeUnknown {
		t.Fatalf("round trip type = %q", out.Type)
	}

	cached, err := json.Marshal(&decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(cached) {
		t.Fatalf("cache json invalid: %s", cached)
	}
	var cache map[string]any
	if err := json.Unmarshal(cached, &cache); err != nil {
		t.Fatal(err)
	}
	delegations, ok := cache["delegations"].([]any)
	if !ok || len(delegations) != 0 {
		t.Fatalf("redis delegations = %#v", cache["delegations"])
	}
}

func TestFromScanResult_DelegationsRoundTrip(t *testing.T) {
	t.Parallel()

	in := &ScanResult{
		Address: "0xabc",
		Type:    AccountTypeEOA,
		Delegations: []Delegation{{
			ChainID:          10,
			DelegatedAddress: "0x1111111111111111111111111111111111111111",
		}},
	}
	entity := FromScanResult(uuid.New(), in)
	out := entity.ToScanResult()
	if len(out.Delegations) != 1 {
		t.Fatalf("delegations = %#v", out.Delegations)
	}
	if out.Delegations[0].ChainID != 10 || out.Delegations[0].DelegatedAddress != in.Delegations[0].DelegatedAddress {
		t.Fatalf("delegation = %#v", out.Delegations[0])
	}
}

func TestScanResultJSON_PublicKeyRecoveryWithoutActivityDates(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"address":"0xabc",
		"public_key_recovery":"recovered",
		"first_seen":"2019-05-06T07:08:09Z",
		"last_seen":"2024-11-12T13:14:15Z"
	}`)
	var decoded ScanResult
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.PublicKeyRecovery != PublicKeyRecoveryRecovered {
		t.Fatalf("public_key_recovery = %q", decoded.PublicKeyRecovery)
	}

	entity := FromScanResult(uuid.New(), &decoded)
	if entity.PublicKeyRecovery != PublicKeyRecoveryRecovered {
		t.Fatalf("stored public_key_recovery = %q", entity.PublicKeyRecovery)
	}
	out := entity.ToScanResult()
	if out.PublicKeyRecovery != PublicKeyRecoveryRecovered {
		t.Fatalf("round trip = %q", out.PublicKeyRecovery)
	}

	body, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "first_seen") || strings.Contains(string(body), "last_seen") {
		t.Fatalf("activity dates present in %s", body)
	}
	if !strings.Contains(string(body), `"public_key_recovery":"recovered"`) {
		t.Fatalf("body = %s", body)
	}
}

func TestFromScanResult_PublicKeyRecoveryValues(t *testing.T) {
	t.Parallel()

	for _, value := range []PublicKeyRecovery{
		PublicKeyRecoveryNotRequired,
		PublicKeyRecoveryRecovered,
		PublicKeyRecoveryUnresolved,
	} {
		entity := FromScanResult(uuid.New(), &ScanResult{
			Address:           "0xabc",
			PublicKeyRecovery: value,
		})
		if got := entity.ToScanResult().PublicKeyRecovery; got != value {
			t.Fatalf("public_key_recovery = %q, want %q", got, value)
		}
	}
}

func TestDecodeLegacyAAPayload(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"address":"0xabc","type":"AA","algorithm":"ECDSA-secp256k1","is_eoa":false,"is_erc4337":true}`)
	var decoded ScanResult
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	NormalizeScanResultWalletKind(&decoded)
	if decoded.Type != AccountTypeAA || decoded.IsEOA || !decoded.IsERC4337 {
		t.Fatalf("legacy AA = %+v", decoded)
	}
	if decoded.Algorithm != AlgorithmECDSAsecp256k1 {
		t.Fatalf("algorithm = %q", decoded.Algorithm)
	}
}
