// Package wallettransition applies one Discovery wallet command in a single transaction.
package wallettransition

import (
	"errors"
	"expvar"
	"time"

	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/repository"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	QuotaNone    = "none"
	QuotaKeep    = "keep"
	QuotaRelease = "release"

	OutcomeApplied         = "applied"
	OutcomeAlreadyApplied  = "already_applied"
	OutcomeAlreadyRunning  = "already_running"
	OutcomeAlreadyTerminal = "already_terminal"
	OutcomeRetryable       = "retryable_error"
	OutcomeRejected        = "rejected"

	// KeepLedgerMissingMetric counts keep commands whose credit reservation is absent.
	KeepLedgerMissingMetric = "wallet_transition_keep_ledger_missing"
)

var keepLedgerMissing = expvar.NewInt(KeepLedgerMissingMetric)

var errKeepMissing = errors.New("keep ledger missing")

// Command is one decoded wallet transition. Address and a SUCCESS result address are already normalized.
type Command struct {
	TransitionID string
	UserID       uuid.UUID
	Address      string
	SourceStates []string
	TargetState  string
	QuotaAction  string
	Result       *domain.ScanResult
	Error        string
}

// Result is the transactional outcome. Status is empty when state was not reread. Row is set when a projection may use it.
type Result struct {
	Outcome string
	Status  string
	Row     *domain.ScanResultEntity
}

// Apply writes state, transition_id, and quota in one transaction. A missing row is PENDING.
func Apply(db *gorm.DB, ledger repository.ScanUsageLedgerRepository, scanID uuid.UUID, cmd Command) Result {
	if db == nil {
		return Result{Outcome: OutcomeRetryable}
	}
	var out Result
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		out, err = applyTx(tx, ledger, scanID, cmd)
		return err
	})
	if err != nil {
		log.Error().Err(err).Str("scan_id", scanID.String()).Msg("wallet transition transaction failed")
		return Result{Outcome: OutcomeRetryable, Status: out.Status}
	}
	return out
}

func applyTx(tx *gorm.DB, ledger repository.ScanUsageLedgerRepository, scanID uuid.UUID, cmd Command) (Result, error) {
	var ent domain.ScanResultEntity
	err := tx.Where("id = ?", scanID).First(&ent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return applyMissing(tx, ledger, scanID, cmd)
	}
	if err != nil {
		return Result{}, err
	}
	return applyExisting(tx, ledger, scanID, cmd, &ent)
}

func applyMissing(tx *gorm.DB, ledger repository.ScanUsageLedgerRepository, scanID uuid.UUID, cmd Command) (Result, error) {
	if !contains(cmd.SourceStates, scan.StatePENDING) {
		return Result{Outcome: OutcomeRejected, Status: scan.StatePENDING}, nil
	}
	if stop, err := prepareQuota(tx, ledger, scanID, cmd, scan.StatePENDING); err != nil || stop.Outcome != "" {
		return stopResult(stop, err, scan.StatePENDING)
	}
	ent := newRow(scanID, cmd)
	res := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoNothing: true}).Create(ent)
	if res.Error != nil {
		return Result{Status: scan.StatePENDING}, res.Error
	}
	if res.RowsAffected == 0 {
		var winner domain.ScanResultEntity
		if err := tx.Where("id = ?", scanID).First(&winner).Error; err != nil {
			return Result{Status: scan.StatePENDING}, err
		}
		return applyExisting(tx, ledger, scanID, cmd, &winner)
	}
	if err := finishQuota(tx, ledger, scanID, cmd); err != nil {
		return Result{Status: scan.StatePENDING}, err
	}
	return reload(tx, scanID, OutcomeApplied)
}

func applyExisting(tx *gorm.DB, ledger repository.ScanUsageLedgerRepository, scanID uuid.UUID, cmd Command, ent *domain.ScanResultEntity) (Result, error) {
	status := logical(ent.Status)
	if sameTransition(ent, cmd.TransitionID) {
		row := *ent
		row.Status = status
		return Result{Outcome: OutcomeAlreadyApplied, Status: status, Row: &row}, nil
	}
	if !contains(cmd.SourceStates, status) {
		return classify(ent, cmd), nil
	}
	if stop, err := prepareQuota(tx, ledger, scanID, cmd, status); err != nil || stop.Outcome != "" {
		return stopResult(stop, err, status)
	}
	n, err := writeScoped(tx, scanID, cmd)
	if err != nil {
		return Result{Status: status}, err
	}
	if n == 0 {
		var again domain.ScanResultEntity
		if err := tx.Where("id = ?", scanID).First(&again).Error; err != nil {
			return Result{Status: status}, err
		}
		if sameTransition(&again, cmd.TransitionID) || !contains(cmd.SourceStates, logical(again.Status)) {
			return classify(&again, cmd), nil
		}
		n, err = writeScoped(tx, scanID, cmd)
		if err != nil || n == 0 {
			if err != nil {
				return Result{Status: logical(again.Status)}, err
			}
			_ = tx.Where("id = ?", scanID).First(&again).Error
			return classify(&again, cmd), nil
		}
	}
	if err := finishQuota(tx, ledger, scanID, cmd); err != nil {
		return Result{Status: status}, err
	}
	return reload(tx, scanID, OutcomeApplied)
}

func classify(ent *domain.ScanResultEntity, cmd Command) Result {
	status := logical(ent.Status)
	if cmd.TargetState == scan.StateRUNNING && status == scan.StateRUNNING {
		return Result{Outcome: OutcomeAlreadyRunning, Status: status}
	}
	if scan.IsTerminal(status) {
		return Result{Outcome: OutcomeAlreadyTerminal, Status: status}
	}
	return Result{Outcome: OutcomeRejected, Status: status}
}

func prepareQuota(tx *gorm.DB, ledger repository.ScanUsageLedgerRepository, scanID uuid.UUID, cmd Command, status string) (Result, error) {
	if cmd.QuotaAction == QuotaNone {
		return Result{}, nil
	}
	if ledger == nil {
		return Result{Outcome: OutcomeRetryable, Status: status}, nil
	}
	if cmd.QuotaAction != QuotaKeep {
		return Result{}, nil
	}
	has, err := ledger.HasSuccessUsageForScanInTx(tx, scanID)
	if err != nil {
		return Result{Status: status}, err
	}
	if !has {
		noteKeepMissing(scanID)
		return Result{Outcome: OutcomeRetryable, Status: status}, nil
	}
	return Result{}, nil
}

func finishQuota(tx *gorm.DB, ledger repository.ScanUsageLedgerRepository, scanID uuid.UUID, cmd Command) error {
	switch cmd.QuotaAction {
	case QuotaRelease:
		if ledger == nil {
			return errKeepMissing
		}
		return ledger.ReleaseSuccessUsageByScanIDInTx(tx, scanID)
	case QuotaKeep:
		has, err := ledger.HasSuccessUsageForScanInTx(tx, scanID)
		if err != nil || has {
			return err
		}
		noteKeepMissing(scanID)
		return errKeepMissing
	default:
		return nil
	}
}

func writeScoped(tx *gorm.DB, scanID uuid.UUID, cmd Command) (int64, error) {
	ent := newRow(scanID, cmd)
	q := sourceScope(tx, scanID, cmd.SourceStates)
	var res *gorm.DB
	if cmd.TargetState == scan.StateSUCCESS {
		res = q.Omit("created_at", "deleted_at").Select("*").Updates(ent)
	} else {
		res = q.Updates(map[string]any{
			"status": ent.Status, "error": ent.Error, "user_id": ent.UserID,
			"address": ent.Address, "transition_id": cmd.TransitionID, "updated_at": time.Now().UTC(),
		})
	}
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func sourceScope(tx *gorm.DB, scanID uuid.UUID, sources []string) *gorm.DB {
	pending := contains(sources, scan.StatePENDING)
	stored := make([]string, 0, len(sources))
	for _, s := range sources {
		if s != "" {
			stored = append(stored, s)
		}
	}
	q := tx.Model(&domain.ScanResultEntity{}).Where("id = ?", scanID)
	switch {
	case pending && len(stored) > 0:
		return q.Where("status IN ? OR status = '' OR status IS NULL", stored)
	case len(stored) > 0:
		return q.Where("status IN ?", stored)
	case pending:
		return q.Where("status = '' OR status IS NULL OR status = ?", scan.StatePENDING)
	default:
		return q.Where("id IS NULL")
	}
}

func newRow(scanID uuid.UUID, cmd Command) *domain.ScanResultEntity {
	tid := cmd.TransitionID
	switch cmd.TargetState {
	case scan.StateSUCCESS:
		domain.EnsureScanResultDelegations(cmd.Result)
		domain.NormalizeScanResultWalletKind(cmd.Result)
		ent := domain.FromScanResult(cmd.UserID, cmd.Result)
		ent.ID = scanID
		ent.Status = scan.StateSUCCESS
		ent.Error = ""
		ent.Address = cmd.Address
		ent.TransitionID = &tid
		return ent
	case scan.StateFAILED:
		return &domain.ScanResultEntity{
			ID: scanID, UserID: cmd.UserID, Address: cmd.Address,
			Status: scan.StateFAILED, Error: cmd.Error, TransitionID: &tid,
		}
	default:
		return &domain.ScanResultEntity{
			ID: scanID, UserID: cmd.UserID, Address: cmd.Address,
			Status: scan.StateRUNNING, TransitionID: &tid,
		}
	}
}

func reload(tx *gorm.DB, scanID uuid.UUID, outcome string) (Result, error) {
	var stored domain.ScanResultEntity
	if err := tx.Where("id = ?", scanID).First(&stored).Error; err != nil {
		return Result{}, err
	}
	stored.Status = logical(stored.Status)
	return Result{Outcome: outcome, Status: stored.Status, Row: &stored}, nil
}

func stopResult(stop Result, err error, status string) (Result, error) {
	if err != nil {
		return Result{Status: status}, err
	}
	return stop, nil
}

func sameTransition(ent *domain.ScanResultEntity, id string) bool {
	return ent.TransitionID != nil && *ent.TransitionID == id
}

func logical(status string) string {
	if status == "" {
		return scan.StatePENDING
	}
	return status
}

func contains(states []string, want string) bool {
	for _, s := range states {
		if s == want {
			return true
		}
	}
	return false
}

func noteKeepMissing(scanID uuid.UUID) {
	keepLedgerMissing.Add(1)
	log.Warn().Str("scan_id", scanID.String()).Str("metric", KeepLedgerMissingMetric).
		Msg("wallet transition keep without ledger reservation")
}
