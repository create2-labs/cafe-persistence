package walletobservation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cafe-persistence/internal/domain"

	addressv1 "github.com/create2-labs/cafe-contracts/observation/address/v1"
	v01 "github.com/create2-labs/cafe-contracts/observation/wallet/v01"
	"github.com/google/uuid"
)

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

func eoaRow(t *testing.T, observedAt *time.Time, chainIDs ...int64) *domain.ScanResultEntity {
	t.Helper()
	chains := make([]addressv1.Chain, 0, len(chainIDs))
	for _, id := range chainIDs {
		chains = append(chains, addressv1.Chain{ChainID: id, Qualification: addressv1.QualificationEOA})
	}
	raw, err := json.Marshal(chains)
	if err != nil {
		t.Fatal(err)
	}
	return &domain.ScanResultEntity{
		ID:          uuid.New(),
		Address:     "0x742d35Cc6634C0532925a3b844Bc454e4438f44e",
		AccountType: "eoa",
		Algorithm:   domain.AlgorithmECDSAsecp256k1,
		NISTLevel:   domain.NISTLevel1,
		Chains:      string(raw),
		ObservedAt:  observedAt,
	}
}

func testMeta() ExportMeta {
	return ExportMeta{EventID: "evt_1", CorrelationID: "c1", CausationID: "a1"}
}

func TestToWalletObservedEvent_mapsRowToWire(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 4, 17, 9, 59, 58, 0, time.UTC)
	row := eoaRow(t, &ts, 8453, 1)
	row.KeyExposed = true
	ev, err := ToWalletObservedEvent(testMeta(), row)
	if err != nil {
		t.Fatal(err)
	}
	if err := ev.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if ev.EventID != "evt_1" || ev.CorrelationID != "c1" || ev.CausationID != "a1" {
		t.Fatalf("envelope ids: %+v", ev)
	}
	if ev.Subject.ID != "wallet:0x742d35cc6634c0532925a3b844bc454e4438f44e" {
		t.Fatalf("subject id: %q", ev.Subject.ID)
	}
	if want := []int64{1, 8453}; !reflect.DeepEqual(ev.Payload.ChainIDs, want) {
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
	if ev.Payload.CurrentPQPosture != string(v01.PQPostureClassicalOnly) {
		t.Fatalf("posture: %q", ev.Payload.CurrentPQPosture)
	}
	if !ev.Payload.ObservedAt.Equal(ts) || !ev.OccurredAt.Equal(ts) {
		t.Fatalf("observed_at %v occurred_at %v, want %v", ev.Payload.ObservedAt, ev.OccurredAt, ts)
	}
}

func TestToWalletObservedEvent_chainIDsUniqueAscending(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)
	ev, err := ToWalletObservedEvent(testMeta(), eoaRow(t, &ts, 10, 1, 10, 137))
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{1, 10, 137}; !reflect.DeepEqual(ev.Payload.ChainIDs, want) {
		t.Fatalf("chain_ids: %#v want %#v", ev.Payload.ChainIDs, want)
	}
}

func TestToWalletObservedEvent_singleChainIsNotMultichain(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)
	ev, err := ToWalletObservedEvent(testMeta(), eoaRow(t, &ts, 1))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Payload.IsMultichain {
		t.Fatal("one chain must not be multichain")
	}
	if want := []int64{1}; !reflect.DeepEqual(ev.Payload.ChainIDs, want) {
		t.Fatalf("chain_ids: %#v want %#v", ev.Payload.ChainIDs, want)
	}
}

func TestToWalletObservedEvent_derivesCurrentPQPostureFromNISTLevel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		nistLevel domain.NISTLevel
		want      string
	}{
		{"classical_only at level 1", domain.NISTLevel1, string(v01.PQPostureClassicalOnly)},
		{"hybrid at level 3", domain.NISTLevel3, string(v01.PQPostureHybrid)},
		{"full_pq at level 5", domain.NISTLevel5, string(v01.PQPostureFullPQ)},
		{"unknown at unsupported level", domain.NISTLevel(0), string(v01.PQPostureUnknown)},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ts := time.Now().UTC()
			row := eoaRow(t, &ts, 1)
			row.NISTLevel = tc.nistLevel
			ev, err := ToWalletObservedEvent(testMeta(), row)
			if err != nil {
				t.Fatal(err)
			}
			if err := ev.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if ev.Payload.CurrentPQPosture != tc.want {
				t.Fatalf("posture: %q want %q", ev.Payload.CurrentPQPosture, tc.want)
			}
		})
	}
}

func TestToWalletObservedEvent_nullObservedAtIsError(t *testing.T) {
	t.Parallel()
	ev, err := ToWalletObservedEvent(testMeta(), eoaRow(t, nil, 1))
	if err == nil {
		t.Fatal("a NULL observed_at must be an error")
	}
	if !reflect.DeepEqual(ev, v01.Event{}) {
		t.Fatalf("expected the zero event, got %#v", ev)
	}
}

func TestToWalletObservedEvent_undecodableChainsIsError(t *testing.T) {
	t.Parallel()
	ts := time.Now().UTC()
	row := eoaRow(t, &ts, 1)
	row.Chains = "null"
	ev, err := ToWalletObservedEvent(testMeta(), row)
	if err == nil {
		t.Fatalf("a stored null chains column must be a decoding error, got %v", err)
	}
	if !reflect.DeepEqual(ev, v01.Event{}) {
		t.Fatalf("expected the zero event, got %#v", ev)
	}
}

func TestToWalletObservedEvent_noDelegationFieldOnWire(t *testing.T) {
	t.Parallel()
	ts := time.Now().UTC()
	ev, err := ToWalletObservedEvent(testMeta(), eoaRow(t, &ts, 1))
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(&ev)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "delegat") {
		t.Fatalf("wallet.observed must not gain a delegation field: %s", out)
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
	row := eoaRow(t, &ts, 1)
	row.Address = "0x742d35cc6634c0532925a3b844bc454e4438f44e"
	got, err := ToWalletObservedEvent(ExportMeta{EventID: "evt_pr3_001", CorrelationID: "corr_1", CausationID: "cause_1"}, row)
	if err != nil {
		t.Fatal(err)
	}
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

// OP-11.4 (a): a nil row is an error and never an empty event with a nil error.
func TestToWalletObservedEvent_nilRowIsError(t *testing.T) {
	t.Parallel()
	ev, err := ToWalletObservedEvent(ExportMeta{}, nil)
	if err == nil {
		t.Fatal("a nil row must return an error")
	}
	if !reflect.DeepEqual(ev, v01.Event{}) {
		t.Fatalf("expected the zero event, got %#v", ev)
	}
}
