package walletobservation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"cafe-persistence/internal/domain"

	v01 "github.com/create2-labs/cafe-contracts/observation/wallet/v01"
	"github.com/google/uuid"
)

// Mirrors typical config.yaml chain_id entries for tests.
func TestExportMetaForScanJob(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	meta := ExportMetaForScanJob(id)
	if meta.CorrelationID != id.String() || meta.CausationID != id.String() {
		t.Fatalf("correlation/causation: %+v", meta)
	}
	if meta.EventID != "evt_disc_550e8400e29b41d4a716446655440000" {
		t.Fatalf("event_id: %q", meta.EventID)
	}
}

var testChainMap = map[string]int64{
	"ethereum-mainnet": 1,
	"optimism":         10,
	"arbitrum-one":     42161,
	"base":             8453,
	"bsc":              56,
	"polygon":          137,
}

func TestToWalletObservedEvent_mapsScanToWire(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 4, 17, 9, 59, 58, 0, time.UTC)
	scan := &domain.ScanResult{
		Address:         "0x742d35Cc6634C0532925a3b844Bc454e4438f44e",
		Type:            domain.AccountTypeEOA,
		Algorithm:       domain.AlgorithmECDSAsecp256k1,
		KeyExposed:      true,
		IsEOA:           true,
		IsERC4337:       false,
		ScannedAt:       ts,
		Networks:        []string{"ethereum-mainnet", "base"},
		Connections:     []string{},
	}
	meta := ExportMeta{
		EventID:       "evt_1",
		CorrelationID: "c1",
		CausationID:   "a1",
		OccurredAt:    time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC),
	}
	ev := ToWalletObservedEvent(meta, scan, testChainMap)
	if err := ev.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if ev.Payload.CurrentPQPosture != string(v01.PQPostureUnknown) {
		t.Fatalf("posture: %q", ev.Payload.CurrentPQPosture)
	}
	if ev.Subject.ID != "wallet:0x742d35cc6634c0532925a3b844bc454e4438f44e" {
		t.Fatalf("subject id: %q", ev.Subject.ID)
	}
	want := []int64{1, 8453}
	if !reflect.DeepEqual(ev.Payload.ChainIDs, want) {
		t.Fatalf("chain_ids: %#v want %#v", ev.Payload.ChainIDs, want)
	}
	if !ev.Payload.IsMultichain {
		t.Fatal("expected multichain true")
	}
	if ev.Payload.AccountKind != string(v01.AccountKindEOA) {
		t.Fatalf("account_kind: %s", ev.Payload.AccountKind)
	}
	if ev.Payload.CurrentAlgorithm != string(v01.AlgorithmSecp256k1ECRecover) {
		t.Fatalf("algorithm: %s", ev.Payload.CurrentAlgorithm)
	}
	if !ev.Payload.PublicKeyExposed {
		t.Fatal("public_key_exposed")
	}
}

func TestToWalletObservedEvent_derivesCurrentPQPostureFromNISTLevel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		nistLevel domain.NISTLevel
		want      string
	}{
		{
			name:      "classical_only at level 1",
			nistLevel: domain.NISTLevel1,
			want:      string(v01.PQPostureClassicalOnly),
		},
		{
			name:      "hybrid at level 3",
			nistLevel: domain.NISTLevel3,
			want:      string(v01.PQPostureHybrid),
		},
		{
			name:      "full_pq at level 5",
			nistLevel: domain.NISTLevel5,
			want:      string(v01.PQPostureFullPQ),
		},
		{
			name:      "unknown at unsupported level",
			nistLevel: domain.NISTLevel(0),
			want:      string(v01.PQPostureUnknown),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scan := &domain.ScanResult{
				Address:    "0xabc0000000000000000000000000000000000009",
				Type:       domain.AccountTypeEOA,
				Algorithm:  domain.AlgorithmECDSAsecp256k1,
				NISTLevel:  tc.nistLevel,
				ScannedAt:  time.Now().UTC(),
				Networks:   []string{"ethereum-mainnet"},
			}

			ev := ToWalletObservedEvent(ExportMeta{
				EventID:       "evt_pq_1",
				CorrelationID: "corr_pq_1",
				CausationID:   "cause_pq_1",
			}, scan, testChainMap)
			if err := ev.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if ev.Payload.CurrentPQPosture != tc.want {
				t.Fatalf("posture: %q want %q", ev.Payload.CurrentPQPosture, tc.want)
			}
		})
	}
}

func TestToWalletObservedEvent_emptyChainIDsUnknownNetwork(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)
	scan := &domain.ScanResult{
		Address:    "0xabc0000000000000000000000000000000000001",
		Type:       domain.AccountTypeEOA,
		Algorithm:  domain.AlgorithmECDSAsecp256k1,
		ScannedAt:  ts,
		Networks:   []string{"unknown-custom-net", "other-net"},
	}
	ev := ToWalletObservedEvent(ExportMeta{
		EventID: "e1", CorrelationID: "c", CausationID: "x",
	}, scan, testChainMap)
	if err := ev.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(ev.Payload.ChainIDs) != 0 {
		t.Fatalf("chain_ids: %#v", ev.Payload.ChainIDs)
	}
	if !ev.Payload.IsMultichain {
		t.Fatal("expected multichain from multiple network names")
	}
}

func TestToWalletObservedEvent_erc4337AccountKind(t *testing.T) {
	t.Parallel()
	scan := &domain.ScanResult{
		Address:    "0xabc0000000000000000000000000000000000003",
		Type:       domain.AccountTypeAA,
		Algorithm:  domain.AlgorithmECDSAsecp256k1,
		ScannedAt:  time.Now().UTC(),
		Networks:   []string{"base"},
	}
	ev := ToWalletObservedEvent(ExportMeta{EventID: "e", CorrelationID: "c", CausationID: "a"}, scan, testChainMap)
	if err := ev.Validate(); err != nil {
		t.Fatal(err)
	}
	if ev.Payload.AccountKind != string(v01.AccountKindERC4337SmartAccount) {
		t.Fatalf("got %q", ev.Payload.AccountKind)
	}
}

func TestGoldenPlaceholderJSON_matchesExport(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "export_wallet_observed_v01_placeholder.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want v01.Event
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	ts := time.Date(2026, 4, 17, 9, 59, 58, 0, time.UTC)
	scan := &domain.ScanResult{
		Address:    "0x742d35cc6634c0532925a3b844bc454e4438f44e",
		Type:       domain.AccountTypeEOA,
		Algorithm:  domain.AlgorithmECDSAsecp256k1,
		KeyExposed: false,
		ScannedAt:  ts,
		Networks:   nil,
	}
	got := ToWalletObservedEvent(ExportMeta{
		EventID:       "evt_pr3_001",
		CorrelationID: "corr_1",
		CausationID:   "cause_1",
		OccurredAt:    time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC),
	}, scan, nil)
	if err := got.Validate(); err != nil {
		t.Fatalf("validate got: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mismatch\n got: %#v\nwant: %#v", got, want)
	}
	out, err := json.Marshal(&got)
	if err != nil {
		t.Fatal(err)
	}
	var round v01.Event
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatal(err)
	}
	if err := round.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestToWalletObservedEvent_nilScan(t *testing.T) {
	ev := ToWalletObservedEvent(ExportMeta{}, nil, nil)
	if ev.EventID != "" || ev.EventType != "" {
		t.Fatalf("expected empty envelope, got %#v", ev)
	}
}
