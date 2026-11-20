package domain

import (
	"testing"

	addressv1 "github.com/create2-labs/cafe-contracts/observation/address/v1"
)

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
