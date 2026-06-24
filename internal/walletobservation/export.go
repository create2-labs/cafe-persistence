package walletobservation

import (
	"sort"
	"strings"
	"time"

	"cafe-persistence/internal/domain"

	v01 "github.com/create2-labs/cafe-contracts/observation/wallet/v01"
	"github.com/google/uuid"
)

// ExportMeta carries envelope identifiers for cafe.discovery.wallet.observed v0.1.
// OccurredAt, if zero, defaults to scan.ScannedAt.
type ExportMeta struct {
	EventID       string
	CorrelationID string
	CausationID   string
	OccurredAt    time.Time
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

// ToWalletObservedEvent maps a Discovery scan result to the shared wire contract.
// The payload is an observation snapshot for shared types (e.g. embedded in policy.assessment.requested); it is not by itself a CPM command (see execution pack v0.7).
//
// current_pq_posture is derived from normalized observed wallet data.
// chainIDsByNetwork must come from config.ChainConfig.ChainIDByNetwork() (blockchains[].name -> chain_id).
// If nil or empty, no chain IDs are emitted for network names. Unknown names are skipped (never 0 as a sentinel).
func ToWalletObservedEvent(meta ExportMeta, scan *domain.ScanResult, chainIDsByNetwork map[string]int64) v01.Event {
	if scan == nil {
		return v01.Event{}
	}

	occurred := meta.OccurredAt
	if occurred.IsZero() {
		occurred = scan.ScannedAt
	}

	chainIDs := resolveChainIDs(scan.Networks, chainIDsByNetwork)
	isMulti := isMultichain(chainIDs, scan.Networks)

	return v01.Event{
		EventID:       meta.EventID,
		EventType:     v01.EventTypeWalletObserved,
		EventVersion:  v01.EventVersion,
		OccurredAt:    occurred,
		CorrelationID: meta.CorrelationID,
		CausationID:   meta.CausationID,
		Producer:      v01.ProducerCafeDiscovery,
		Subject: v01.Subject{
			Type: string(v01.SubjectTypeWallet),
			ID:   walletSubjectID(scan.Address),
		},
		Payload: v01.Payload{
			ChainIDs:         chainIDs,
			AccountKind:      string(mapAccountKind(scan)),
			CurrentAlgorithm: mapAlgorithm(scan),
			PublicKeyExposed: scan.KeyExposed,
			IsMultichain:     isMulti,
			ObservedAt:       scan.ScannedAt,
			CurrentPQPosture: string(deriveCurrentPQPosture(scan)),
		},
	}
}

func deriveCurrentPQPosture(scan *domain.ScanResult) v01.CurrentPQPosture {
	if scan == nil {
		return v01.PQPostureUnknown
	}

	// Deterministic posture derivation from normalized observation only.
	// Rule set:
	// - NIST level 1: classical_only (quantum-broken baseline)
	// - NIST levels 2..4: hybrid (transitional/post-quantum strengthening)
	// - NIST level 5: full_pq (PQC-ready target state)
	// - any other value: unknown
	switch scan.NISTLevel {
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

func walletSubjectID(normalizedAddress string) string {
	a := strings.TrimSpace(strings.ToLower(normalizedAddress))
	if !strings.HasPrefix(a, "0x") {
		a = "0x" + a
	}
	return "wallet:" + a
}

func mapAccountKind(scan *domain.ScanResult) v01.AccountKind {
	switch domain.DeriveWalletTypeV1(scan.Type, scan.IsEOA, scan.IsERC4337) {
	case domain.WalletTypeEOA:
		return v01.AccountKindEOA
	case domain.WalletTypeSmartAccount:
		return v01.AccountKindERC4337SmartAccount
	case domain.WalletTypeContract:
		return v01.AccountKindContractAccount
	default:
		return v01.AccountKindUnknown
	}
}

func mapAlgorithm(scan *domain.ScanResult) string {
	switch scan.Algorithm {
	case domain.AlgorithmECDSAsecp256k1:
		return string(v01.AlgorithmSecp256k1ECRecover)
	default:
		// Discovery currently only emits ECDSA-secp256k1; keep export valid and explicit.
		return string(v01.AlgorithmSecp256k1ECRecover)
	}
}

func resolveChainIDs(networks []string, chainIDsByNetwork map[string]int64) []int64 {
	if len(chainIDsByNetwork) == 0 {
		return []int64{}
	}
	seen := make(map[int64]struct{})
	for _, n := range networks {
		id, ok := chainIDsByNetwork[n]
		if !ok || id == 0 {
			continue
		}
		seen[id] = struct{}{}
	}
	out := make([]int64, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func isMultichain(chainIDs []int64, networkNames []string) bool {
	if len(chainIDs) > 1 {
		return true
	}
	uniqNames := make(map[string]struct{})
	for _, n := range networkNames {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		uniqNames[n] = struct{}{}
	}
	return len(uniqNames) > 1
}
