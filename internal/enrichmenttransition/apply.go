// Package enrichmenttransition applies one Discovery enrichment command in a single transaction.
package enrichmenttransition

import (
	"encoding/json"
	"errors"
	"fmt"

	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/wallettransition"
	"cafe-persistence/pkg/scan"

	addressv1 "github.com/create2-labs/cafe-contracts/observation/address/v1"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errNoHistoryRow = errors.New("enrichmenttransition: history columns update affected no scan row")

// EnrichmentTransitionResult is the result body of an enrichment command (design §5.8).
type EnrichmentTransitionResult struct {
	Chains            []domain.EnrichmentChain `json:"chains"`
	RelatedAddresses  []addressv1.Related      `json:"related_addresses,omitempty"` // written by deployer only, increment 4
	PublicKey         string                   `json:"public_key,omitempty"`
	TransactionHash   string                   `json:"transaction_hash,omitempty"`
	ExposedNetwork    string                   `json:"exposed_network,omitempty"`
	PublicKeyRecovery string                   `json:"public_key_recovery,omitempty"`
}

// Command is one decoded enrichment transition.
type Command struct {
	TransitionID string
	UserID       uuid.UUID
	Family       string
	SourceStates []string
	TargetState  string
	QuotaAction  string
	Result       *EnrichmentTransitionResult
	Error        string
}

// Result is the transactional outcome. Status is nil when the row was not read.
type Result struct {
	Outcome string
	Status  *string
}

// Apply writes one enrichment transition in one transaction. Until OC-P43 only a missing row is written.
func Apply(db *gorm.DB, scanID, enrichmentID uuid.UUID, cmd Command) Result {
	if db == nil {
		return Result{Outcome: wallettransition.OutcomeRetryable}
	}
	var out Result
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		out, err = applyTx(tx, scanID, enrichmentID, cmd)
		return err
	})
	if err != nil {
		log.Error().Err(err).Str("scan_id", scanID.String()).Str("enrichment_id", enrichmentID.String()).
			Msg("enrichment transition transaction failed")
		return Result{Outcome: wallettransition.OutcomeRetryable}
	}
	return out
}

func applyTx(tx *gorm.DB, scanID, enrichmentID uuid.UUID, cmd Command) (Result, error) {
	ok, err := scanAllows(tx, scanID, cmd.UserID)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return rejected(nil), nil
	}
	row, err := findEnrichment(tx, enrichmentID)
	if err != nil {
		return Result{}, err
	}
	if row != nil {
		return applyExisting(scanID, cmd, row), nil
	}
	return applyMissing(tx, scanID, enrichmentID, cmd)
}

// scanAllows locks the scan row and reports whether it is live, owned by the user, and SUCCESS (design §5.8, The scan check).
func scanAllows(tx *gorm.DB, scanID, userID uuid.UUID) (bool, error) {
	var ent domain.ScanResultEntity
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND user_id = ? AND status = ?", scanID, userID, scan.StateSUCCESS).First(&ent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("scan check: %w", err)
	}
	return true, nil
}

// findEnrichment returns the visible row of the id, or nil when there is none.
func findEnrichment(tx *gorm.DB, enrichmentID uuid.UUID) (*domain.ScanEnrichmentEntity, error) {
	var row domain.ScanEnrichmentEntity
	err := tx.Where("enrichment_id = ?", enrichmentID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read enrichment: %w", err)
	}
	return &row, nil
}

func applyMissing(tx *gorm.DB, scanID, enrichmentID uuid.UUID, cmd Command) (Result, error) {
	if cmd.TargetState == domain.EnrichmentStatusRunning || !contains(cmd.SourceStates, domain.EnrichmentStatusPending) || cmd.Result == nil {
		return rejected(nil), nil
	}
	chains, err := json.Marshal(nonNil(cmd.Result.Chains))
	if err != nil {
		return Result{}, fmt.Errorf("encode chains: %w", err)
	}
	row := domain.ScanEnrichmentEntity{
		EnrichmentID: enrichmentID, ScanID: scanID, Family: cmd.Family, UserID: cmd.UserID,
		Status: cmd.TargetState, Chains: string(chains), TransitionID: cmd.TransitionID,
	}
	res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if res.Error != nil {
		return Result{}, fmt.Errorf("insert enrichment: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return applyConflict(tx, scanID, enrichmentID, cmd)
	}
	if cmd.TargetState == domain.EnrichmentStatusDone && cmd.Family == domain.EnrichmentFamilyHistory {
		if err := writeHistoryColumns(tx, scanID, cmd.Result); err != nil {
			return Result{}, err
		}
	}
	return Result{Outcome: wallettransition.OutcomeApplied, Status: &row.Status}, nil
}

// applyConflict handles an insert that affected no row: a concurrent winner is an existing row, anything else is rejected.
func applyConflict(tx *gorm.DB, scanID, enrichmentID uuid.UUID, cmd Command) (Result, error) {
	row, err := findEnrichment(tx, enrichmentID)
	if err != nil {
		return Result{}, err
	}
	if row == nil {
		log.Warn().Str("scan_id", scanID.String()).Str("enrichment_id", enrichmentID.String()).Str("family", cmd.Family).
			Msg("enrichment insert conflict without a visible row")
		return rejected(nil), nil
	}
	return applyExisting(scanID, cmd, row), nil
}

// applyExisting applies the existing-row rules of design §5.8. Until OC-P43, every other command is rejected.
func applyExisting(scanID uuid.UUID, cmd Command, row *domain.ScanEnrichmentEntity) Result {
	status := row.Status
	if row.ScanID != scanID || row.UserID != cmd.UserID || row.Family != cmd.Family {
		return rejected(&status)
	}
	if row.TransitionID == cmd.TransitionID {
		return Result{Outcome: wallettransition.OutcomeAlreadyApplied, Status: &status}
	}
	if cmd.TargetState != domain.EnrichmentStatusPending {
		return rejected(&status)
	}
	switch status {
	case domain.EnrichmentStatusRunning:
		return Result{Outcome: wallettransition.OutcomeAlreadyRunning, Status: &status}
	case domain.EnrichmentStatusDone, domain.EnrichmentStatusError:
		return Result{Outcome: wallettransition.OutcomeAlreadyTerminal, Status: &status}
	}
	return rejected(&status)
}

// writeHistoryColumns writes the four history columns of the scan row and no other (design §5.9, History columns).
func writeHistoryColumns(tx *gorm.DB, scanID uuid.UUID, r *EnrichmentTransitionResult) error {
	res := tx.Model(&domain.ScanResultEntity{}).Where("id = ?", scanID).Updates(map[string]any{
		"public_key":          r.PublicKey,
		"transaction_hash":    r.TransactionHash,
		"exposed_network":     r.ExposedNetwork,
		"public_key_recovery": r.PublicKeyRecovery,
	})
	if res.Error != nil {
		return fmt.Errorf("write history columns: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return errNoHistoryRow
	}
	return nil
}

func rejected(status *string) Result {
	return Result{Outcome: wallettransition.OutcomeRejected, Status: status}
}

func nonNil(chains []domain.EnrichmentChain) []domain.EnrichmentChain {
	if chains == nil {
		return []domain.EnrichmentChain{}
	}
	return chains
}

func contains(states []string, want string) bool {
	for _, s := range states {
		if s == want {
			return true
		}
	}
	return false
}
