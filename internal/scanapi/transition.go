package scanapi

import (
	"net/http"
	"strings"

	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/wallettransition"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// EnableWalletTransition attaches the database ApplyWalletTransition writes.
// This route does not publish Redis or NATS projections.
func (h *Handler) EnableWalletTransition(db *gorm.DB) {
	h.walletDB = db
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
		writeTransition(w, wallettransition.Result{Outcome: wallettransition.OutcomeRejected})
		return
	}
	writeTransition(w, wallettransition.Apply(h.walletDB, h.ledger, scanID, cmd))
}

func writeTransition(w http.ResponseWriter, res wallettransition.Result) {
	writeJSON(w, http.StatusOK, transitionResponse{
		Outcome: res.Outcome,
		Status:  res.Status,
	})
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
