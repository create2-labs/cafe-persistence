package walletobservation

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"cafe-persistence/internal/domain"

	v01 "github.com/create2-labs/cafe-contracts/observation/wallet/v01"
	"github.com/google/uuid"
)

// ExportMeta carries envelope identifiers for cafe.discovery.wallet.observed v0.1.
type ExportMeta struct {
	EventID       string
	CorrelationID string
	CausationID   string
}

// ExportMetaForScanJob builds stable envelope IDs from a wallet scan job ID (e.g. NATS scan.completed scan_id).
func ExportMetaForScanJob(scanID uuid.UUID) ExportMeta {
	s := scanID.String()
	return ExportMeta{
		EventID:       "evt_disc_" + strings.ReplaceAll(s, "-", ""),
		CorrelationID: s,
		CausationID:   s,
	}
}

// ToWalletObservedEvent maps a stored wallet scan row to the shared wire contract
// (design §5.5). A nil row, a NULL observed_at, or an undecodable chains column
// returns the zero event and an error. The caller filters on account type.
func ToWalletObservedEvent(meta ExportMeta, row *domain.ScanResultEntity) (v01.Event, error) {
	if row == nil {
		return v01.Event{}, errors.New("walletobservation: nil scan result row")
	}
	if row.ObservedAt == nil {
		return v01.Event{}, errors.New("walletobservation: scan result row has no observed_at")
	}
	chains, err := row.DecodeChains()
	if err != nil {
		return v01.Event{}, fmt.Errorf("walletobservation: decode chains: %w", err)
	}

	chainIDs := make([]int64, 0, len(chains))
	for _, c := range chains {
		chainIDs = append(chainIDs, c.ChainID)
	}
	slices.Sort(chainIDs)
	chainIDs = slices.Compact(chainIDs)

	observedAt := *row.ObservedAt
	return v01.Event{
		EventID:       meta.EventID,
		EventType:     v01.EventTypeWalletObserved,
		EventVersion:  v01.EventVersion,
		OccurredAt:    observedAt,
		CorrelationID: meta.CorrelationID,
		CausationID:   meta.CausationID,
		Producer:      v01.ProducerCafeDiscovery,
		Subject: v01.Subject{
			Type: string(v01.SubjectTypeWallet),
			ID:   walletSubjectID(row.Address),
		},
		Payload: v01.Payload{
			ChainIDs:         chainIDs,
			AccountKind:      string(v01.AccountKindEOA),
			CurrentAlgorithm: mapAlgorithm(row.Algorithm),
			PublicKeyExposed: row.KeyExposed,
			IsMultichain:     len(chainIDs) > 1,
			ObservedAt:       observedAt,
			CurrentPQPosture: string(deriveCurrentPQPosture(row.NISTLevel)),
		},
	}, nil
}

// deriveCurrentPQPosture derives the posture deterministically from the NIST level:
// level 1 is classical_only, levels 2..4 are hybrid, level 5 is full_pq, any other value is unknown.
func deriveCurrentPQPosture(level domain.NISTLevel) v01.CurrentPQPosture {
	switch level {
	case domain.NISTLevel1:
		return v01.PQPostureClassicalOnly
	case domain.NISTLevel2, domain.NISTLevel3, domain.NISTLevel4:
		return v01.PQPostureHybrid
	case domain.NISTLevel5:
		return v01.PQPostureFullPQ
	default:
		return v01.PQPostureUnknown
	}
}

func walletSubjectID(address string) string {
	a := strings.TrimSpace(strings.ToLower(address))
	if !strings.HasPrefix(a, "0x") {
		a = "0x" + a
	}
	return "wallet:" + a
}

func mapAlgorithm(alg domain.Algorithm) string {
	switch alg {
	case domain.AlgorithmECDSAsecp256k1:
		return string(v01.AlgorithmSecp256k1ECRecover)
	case "":
		// No retained chain: do not invent secp256k1_ecrecover. v0.1 rejects an empty current_algorithm.
		return ""
	default:
		// Discovery currently only emits ECDSA-secp256k1; keep export valid and explicit.
		return string(v01.AlgorithmSecp256k1ECRecover)
	}
}
