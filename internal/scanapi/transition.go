package scanapi

import (
	"context"
	"net/http"
	"strings"

	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/walletobservation"
	"cafe-persistence/internal/wallettransition"
	natsconn "cafe-persistence/pkg/nats"
	"cafe-persistence/pkg/scan"

	"github.com/create2-labs/cafe-contracts/cafenatsv01"
	lifecycle "github.com/create2-labs/cafe-contracts/discovery/scanlifecycle/v1"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// EnableWalletTransition attaches the database ApplyWalletTransition writes.
func (h *Handler) EnableWalletTransition(db *gorm.DB) {
	h.walletDB = db
}

func (h *Handler) EnableWalletEvents(events natsconn.Connection) {
	h.events = events
}

type transitionBody struct {
	TransitionID string             `json:"transition_id"`
	UserID       string             `json:"user_id"`
	Address      string             `json:"address"`
	SourceStates []string           `json:"source_states"`
	TargetState  string             `json:"target_state"`
	QuotaAction  string             `json:"quota_action"`
	Result       *domain.ScanResult `json:"result,omitempty"`
	Error        *string            `json:"error,omitempty"`
}

type transitionResponse struct {
	Outcome         string `json:"outcome"`
	Status          string `json:"status,omitempty"`
	ProjectionReady bool   `json:"projection_ready"`
	ReadyPublished  bool   `json:"ready_published"`
}

// ApplyWalletTransition serves POST /wallets/scans/{scan_id}/transition.
func (h *Handler) ApplyWalletTransition(w http.ResponseWriter, r *http.Request) {
	headerUser, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	scanID, err := parseScanIDPath(r)
	if err != nil {
		writeBadRequest(w, r, err.Error())
		return
	}
	var body transitionBody
	if err := decodeJSON(r, &body); err != nil {
		writeBadRequest(w, r, "invalid request body")
		return
	}
	cmd, valid := transitionCommand(headerUser, &body)
	if !valid {
		writeTransition(w, wallettransition.Result{Outcome: wallettransition.OutcomeRejected}, false, false)
		return
	}
	res := wallettransition.Apply(h.walletDB, h.ledger, scanID, cmd)
	ready, published := h.projectTransition(r.Context(), res)
	writeTransition(w, res, ready, published)
}

func writeTransition(w http.ResponseWriter, res wallettransition.Result, projectionReady, readyPublished bool) {
	writeJSON(w, http.StatusOK, transitionResponse{
		Outcome:         res.Outcome,
		Status:          res.Status,
		ProjectionReady: projectionReady,
		ReadyPublished:  readyPublished,
	})
}

func (h *Handler) projectTransition(ctx context.Context, res wallettransition.Result) (bool, bool) {
	switch res.Outcome {
	case wallettransition.OutcomeApplied, wallettransition.OutcomeAlreadyApplied:
		if res.Row == nil || !scan.IsTerminal(res.Row.Status) {
			return false, false
		}
		return h.projectTerminal(ctx, res.Row)
	default:
		return false, false
	}
}

func (h *Handler) projectTerminal(ctx context.Context, row *domain.ScanResultEntity) (bool, bool) {
	if h.cache == nil || row == nil {
		return false, false
	}
	var err error
	if row.Status == scan.StateSUCCESS {
		err = h.cache.SaveWalletScan(ctx, row.UserID, row.Address, row.ToScanResult())
	} else {
		err = h.cache.SaveWalletFailure(ctx, row.UserID, row.Address, row.Error)
	}
	if err != nil {
		log.Error().Err(err).Str("scan_id", row.ID.String()).Msg("wallet transition redis projection failed")
		return false, false
	}
	readyOK := h.publishScanReady(row)
	if row.Status == scan.StateSUCCESS {
		h.publishWalletObserved(row)
	}
	return true, readyOK
}

func (h *Handler) publishScanReady(row *domain.ScanResultEntity) bool {
	if h.events == nil {
		return false
	}
	status := "failed"
	if row.Status == scan.StateSUCCESS {
		status = "success"
	}
	ready := lifecycle.Ready{UserID: row.UserID.String(), Kind: "wallet", Address: row.Address, Status: status}
	if err := natsconn.PublishJSON(h.events, lifecycle.SubjectScanReady, ready); err != nil {
		log.Warn().Err(err).Str("scan_id", row.ID.String()).Msg("wallet transition scan.ready failed")
		return false
	}
	return true
}

func (h *Handler) publishWalletObserved(row *domain.ScanResultEntity) {
	if h.events == nil {
		return
	}
	var chains map[string]int64
	if h.chainCfg != nil {
		chains = h.chainCfg.ChainIDByNetwork()
	}
	result := row.ToScanResult()
	ev := walletobservation.ToWalletObservedEvent(walletobservation.ExportMetaForScanJob(row.ID), result, chains)
	if err := ev.Validate(); err != nil {
		if result != nil && result.Algorithm == "" {
			log.Info().Str("scan_id", row.ID.String()).Msg("wallet transition wallet.observed skipped (empty algorithm)")
			return
		}
		log.Error().Err(err).Str("scan_id", row.ID.String()).Msg("wallet transition wallet.observed export validation failed")
		return
	}
	if err := natsconn.PublishJSON(h.events, cafenatsv01.NATSSubjectDiscoveryWalletObservedV01, &ev); err != nil {
		log.Error().Err(err).Str("scan_id", row.ID.String()).Msg("wallet transition wallet.observed failed")
	}
}

// transitionCommand validates a decoded body. A false result is a permanent rejection with no write.
func transitionCommand(headerUser uuid.UUID, body *transitionBody) (wallettransition.Command, bool) {
	userID, err := uuid.Parse(strings.TrimSpace(body.UserID))
	if err != nil || userID == uuid.Nil || userID != headerUser {
		return wallettransition.Command{}, false
	}
	address := normalizeAddress(body.Address)
	if address == "" || strings.TrimSpace(body.TransitionID) == "" || !knownSources(body.SourceStates) {
		return wallettransition.Command{}, false
	}
	quota, ok := quotaForTarget(body.TargetState)
	if !ok || body.QuotaAction != quota {
		return wallettransition.Command{}, false
	}
	cmd := wallettransition.Command{
		TransitionID: strings.TrimSpace(body.TransitionID),
		UserID:       userID,
		Address:      address,
		SourceStates: body.SourceStates,
		TargetState:  body.TargetState,
		QuotaAction:  body.QuotaAction,
	}
	switch body.TargetState {
	case scan.StateRUNNING:
		if body.Result != nil || body.Error != nil {
			return wallettransition.Command{}, false
		}
	case scan.StateSUCCESS:
		if body.Error != nil || body.Result == nil {
			return wallettransition.Command{}, false
		}
		resultAddr := normalizeAddress(body.Result.Address)
		if resultAddr != address {
			return wallettransition.Command{}, false
		}
		body.Result.Address = resultAddr
		cmd.Result = body.Result
	default:
		if body.Result != nil || body.Error == nil || strings.TrimSpace(*body.Error) == "" {
			return wallettransition.Command{}, false
		}
		cmd.Error = strings.TrimSpace(*body.Error)
	}
	return cmd, true
}

func quotaForTarget(target string) (string, bool) {
	switch target {
	case scan.StateRUNNING:
		return wallettransition.QuotaNone, true
	case scan.StateSUCCESS:
		return wallettransition.QuotaKeep, true
	case scan.StateFAILED:
		return wallettransition.QuotaRelease, true
	default:
		return "", false
	}
}

func knownSources(states []string) bool {
	if len(states) == 0 {
		return false
	}
	for _, state := range states {
		switch state {
		case "", scan.StatePENDING, scan.StateRUNNING, scan.StateSUCCESS, scan.StateFAILED, scan.StateTIMEOUT, scan.StateUNREACHABLE:
		default:
			return false
		}
	}
	return true
}
