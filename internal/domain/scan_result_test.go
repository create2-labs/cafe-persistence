package domain

import (
	"encoding/json"
	"strings"
	"testing"

	addressv1 "github.com/create2-labs/cafe-contracts/observation/address/v1"
)

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

	entity := &ScanResultEntity{Address: decoded.Address, PublicKeyRecovery: decoded.PublicKeyRecovery}
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

func TestToScanResult_PublicKeyRecoveryValues(t *testing.T) {
	t.Parallel()

	for _, value := range []PublicKeyRecovery{
		PublicKeyRecoveryNotRequired,
		PublicKeyRecoveryRecovered,
		PublicKeyRecoveryUnresolved,
	} {
		entity := &ScanResultEntity{Address: "0xabc", PublicKeyRecovery: value}
		if got := entity.ToScanResult().PublicKeyRecovery; got != value {
			t.Fatalf("public_key_recovery = %q, want %q", got, value)
		}
	}
}

// ET8 (OC-P21): the three decoding helpers of the qualification columns.

func TestDecodeChains(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "[]"} {
		got, err := (&ScanResultEntity{Chains: raw}).DecodeChains()
		if err != nil {
			t.Fatalf("DecodeChains(%q) error: %v", raw, err)
		}
		if len(got) != 0 {
			t.Fatalf("DecodeChains(%q) = %v, want empty", raw, got)
		}
	}

	got, err := (&ScanResultEntity{Chains: `[{"chain_id":1,"qualification":"eoa"},{"chain_id":10,"qualification":"blank"}]`}).DecodeChains()
	if err != nil {
		t.Fatalf("DecodeChains valid: %v", err)
	}
	if len(got) != 2 || got[0].ChainID != 1 || got[0].Qualification != addressv1.QualificationEOA ||
		got[1].ChainID != 10 || got[1].Qualification != addressv1.QualificationBlank {
		t.Fatalf("DecodeChains valid = %+v", got)
	}

	if _, err := (&ScanResultEntity{Chains: `{not json`}).DecodeChains(); err == nil {
		t.Fatal("DecodeChains must fail on unreadable JSON")
	}

	if _, err := (&ScanResultEntity{Chains: "null"}).DecodeChains(); err == nil || err.Error() != "decode scan_results.chains: invalid JSON array" {
		t.Fatalf("DecodeChains(null) error = %v", err)
	}
}

func TestDecodeUnobservedChainIDs(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "[]"} {
		got, err := (&ScanResultEntity{UnobservedChainIDs: raw}).DecodeUnobservedChainIDs()
		if err != nil {
			t.Fatalf("DecodeUnobservedChainIDs(%q) error: %v", raw, err)
		}
		if len(got) != 0 {
			t.Fatalf("DecodeUnobservedChainIDs(%q) = %v, want empty", raw, got)
		}
	}

	got, err := (&ScanResultEntity{UnobservedChainIDs: `[137,42161]`}).DecodeUnobservedChainIDs()
	if err != nil {
		t.Fatalf("DecodeUnobservedChainIDs valid: %v", err)
	}
	if len(got) != 2 || got[0] != 137 || got[1] != 42161 {
		t.Fatalf("DecodeUnobservedChainIDs valid = %v", got)
	}

	if _, err := (&ScanResultEntity{UnobservedChainIDs: `[1,`}).DecodeUnobservedChainIDs(); err == nil {
		t.Fatal("DecodeUnobservedChainIDs must fail on unreadable JSON")
	}

	if _, err := (&ScanResultEntity{UnobservedChainIDs: "null"}).DecodeUnobservedChainIDs(); err == nil || err.Error() != "decode scan_results.unobserved_chain_ids: invalid JSON array" {
		t.Fatalf("DecodeUnobservedChainIDs(null) error = %v", err)
	}
}

func TestDecodeAnomalies(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "[]"} {
		got, err := (&ScanResultEntity{Anomalies: raw}).DecodeAnomalies()
		if err != nil {
			t.Fatalf("DecodeAnomalies(%q) error: %v", raw, err)
		}
		if len(got) != 0 {
			t.Fatalf("DecodeAnomalies(%q) = %v, want empty", raw, got)
		}
	}

	got, err := (&ScanResultEntity{Anomalies: `["contract_with_eoa_activity"]`}).DecodeAnomalies()
	if err != nil {
		t.Fatalf("DecodeAnomalies valid: %v", err)
	}
	if len(got) != 1 || got[0] != addressv1.AnomalyContractWithEOAActivity {
		t.Fatalf("DecodeAnomalies valid = %v", got)
	}

	if _, err := (&ScanResultEntity{Anomalies: `"oops`}).DecodeAnomalies(); err == nil {
		t.Fatal("DecodeAnomalies must fail on unreadable JSON")
	}

	if _, err := (&ScanResultEntity{Anomalies: "null"}).DecodeAnomalies(); err == nil || err.Error() != "decode scan_results.anomalies: invalid JSON array" {
		t.Fatalf("DecodeAnomalies(null) error = %v", err)
	}
}
