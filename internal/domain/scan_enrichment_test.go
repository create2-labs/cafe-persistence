package domain

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// ET8 (OC-P41): DecodeChains of scan_enrichments.chains (design 5.7, Decoding).

const wantEnrichmentDecodeError = "decode scan_enrichments.chains: invalid JSON array"

func TestScanEnrichmentDecodeChains_validArray(t *testing.T) {
	t.Parallel()

	raw := `[` +
		`{"chain_id":1,"outcome":"observed","code":"c1","transaction_hash":"0xaa","deployer":"0xbb","creation_tx_hash":"0xcc"},` +
		`{"chain_id":10,"outcome":"pending"}]`
	got, err := (&ScanEnrichmentEntity{Chains: raw}).DecodeChains()
	if err != nil {
		t.Fatalf("DecodeChains valid: %v", err)
	}
	want := []EnrichmentChain{
		{ChainID: 1, Outcome: EnrichmentOutcomeObserved, Code: "c1", TransactionHash: "0xaa", Deployer: "0xbb", CreationTxHash: "0xcc"},
		{ChainID: 10, Outcome: EnrichmentOutcomePending},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DecodeChains valid = %+v, want %+v", got, want)
	}
}

func TestScanEnrichmentDecodeChains_emptyArrayGivesEmptyNonNilList(t *testing.T) {
	t.Parallel()

	got, err := (&ScanEnrichmentEntity{Chains: "[]"}).DecodeChains()
	if err != nil {
		t.Fatalf("DecodeChains([]) error: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("DecodeChains([]) = %#v, want empty non-nil list", got)
	}
}

func TestScanEnrichmentDecodeChains_doesNotCheckOutcomeValues(t *testing.T) {
	t.Parallel()

	got, err := (&ScanEnrichmentEntity{Chains: `[{"chain_id":1,"outcome":"whatever"}]`}).DecodeChains()
	if err != nil || len(got) != 1 || got[0].Outcome != "whatever" {
		t.Fatalf("DecodeChains unknown outcome = %+v, %v", got, err)
	}
}

func TestScanEnrichmentDecodeChains_unreadableOrNotAnArrayIsAnError(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty string":      "",
		"unreadable JSON":   `{not json`,
		"JSON null":         "null",
		"JSON object":       `{"chain_id":1}`,
		"JSON string":       `"x"`,
		"JSON number":       `1`,
		"bad element type":  `[{"chain_id":"one","outcome":"observed"}]`,
		"non-object member": `[1]`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := (&ScanEnrichmentEntity{Chains: raw}).DecodeChains()
			if err == nil || err.Error() != wantEnrichmentDecodeError {
				t.Fatalf("DecodeChains(%q) error = %v, want %q", raw, err, wantEnrichmentDecodeError)
			}
			if got != nil {
				t.Fatalf("DecodeChains(%q) list = %#v, want nil", raw, got)
			}
			var syntax *json.SyntaxError
			var typed *json.UnmarshalTypeError
			if errors.As(err, &syntax) || errors.As(err, &typed) || errors.Unwrap(err) != nil {
				t.Fatalf("DecodeChains(%q) error must not wrap the encoding/json error: %v", raw, err)
			}
		})
	}
}
