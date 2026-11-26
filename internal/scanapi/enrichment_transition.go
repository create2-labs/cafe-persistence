package scanapi

import (
	"net/http"
	"slices"
	"strings"

	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/enrichmenttransition"
	"cafe-persistence/internal/wallettransition"

	"github.com/google/uuid"
)

const quotaNone = "none"

type enrichmentTransitionBody struct {
	TransitionID string                                         `json:"transition_id"`
	UserID       string                                         `json:"user_id"`
	Family       string                                         `json:"family"`
	SourceStates []string                                       `json:"source_states"`
	TargetState  string                                         `json:"target_state"`
	QuotaAction  string                                         `json:"quota_action"`
	Result       *enrichmenttransition.EnrichmentTransitionResult `json:"result,omitempty"`
	Error        *string                                        `json:"error,omitempty"`
}

type enrichmentTransitionResponse struct {
	Outcome string  `json:"outcome"`
	Status  *string `json:"status,omitempty"`
}

// ApplyEnrichmentTransition serves POST /internal/scan/v1/wallets/scans/{scan_id}/enrichments/{enrichment_id}/transition
// (technical design §5.8). Protocol errors are 400; every other answer is 200 with an outcome.
func (h *Handler) ApplyEnrichmentTransition(w http.ResponseWriter, r *http.Request) {
	headerUser, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	scanID, err := parseScanIDPath(r)
	if err != nil {
		writeBadRequest(w, r, err.Error())
		return
	}
	enrichmentID, err := uuid.Parse(strings.TrimSpace(r.PathValue("enrichment_id")))
	if err != nil {
		writeBadRequest(w, r, "enrichment_id must be a UUID")
		return
	}
	var body enrichmentTransitionBody
	if err := decodeJSON(r, &body); err != nil {
		writeBadRequest(w, r, "invalid request body")
		return
	}
	cmd, valid := enrichmentCommand(headerUser, &body)
	if !valid {
		writeJSON(w, http.StatusOK, enrichmentTransitionResponse{Outcome: wallettransition.OutcomeRejected})
		return
	}
	res := enrichmenttransition.Apply(h.walletDB, scanID, enrichmentID, cmd)
	writeJSON(w, http.StatusOK, enrichmentTransitionResponse{Outcome: res.Outcome, Status: res.Status})
}

// enrichmentCommand validates the field rules of design §5.8. A false result is a rejection with no write.
// Only the PENDING and RUNNING targets are served until OC-P42d.
func enrichmentCommand(headerUser uuid.UUID, body *enrichmentTransitionBody) (enrichmenttransition.Command, bool) {
	userID, err := uuid.Parse(strings.TrimSpace(body.UserID))
	if err != nil || userID != headerUser {
		return enrichmenttransition.Command{}, false
	}
	servedTarget := body.TargetState == domain.EnrichmentStatusPending || body.TargetState == domain.EnrichmentStatusRunning
	if !servedTarget || strings.TrimSpace(body.TransitionID) == "" || body.Family != domain.EnrichmentFamilyHistory ||
		body.QuotaAction != quotaNone || body.Error != nil || !slices.Equal(body.SourceStates, []string{domain.EnrichmentStatusPending}) {
		return enrichmenttransition.Command{}, false
	}
	if body.TargetState == domain.EnrichmentStatusRunning {
		if body.Result != nil {
			return enrichmenttransition.Command{}, false
		}
	} else if !pendingResultValid(body.Result) {
		return enrichmenttransition.Command{}, false
	}
	return enrichmenttransition.Command{
		TransitionID: strings.TrimSpace(body.TransitionID),
		UserID:       userID,
		Family:       body.Family,
		SourceStates: body.SourceStates,
		TargetState:  body.TargetState,
		QuotaAction:  body.QuotaAction,
		Result:       body.Result,
	}, true
}

// pendingResultValid applies the result, chain, and address-level rules of a PENDING target (design §5.8).
func pendingResultValid(r *enrichmenttransition.EnrichmentTransitionResult) bool {
	if r == nil || len(r.Chains) == 0 || len(r.RelatedAddresses) != 0 || r.PublicKey != "" || r.TransactionHash != "" ||
		r.ExposedNetwork != "" || r.PublicKeyRecovery != "" {
		return false
	}
	seen := make(map[int64]bool, len(r.Chains))
	pending := false
	for _, c := range r.Chains {
		if c.ChainID <= 0 || seen[c.ChainID] || c.TransactionHash != "" || c.Deployer != "" || c.CreationTxHash != "" {
			return false
		}
		seen[c.ChainID] = true
		switch c.Outcome {
		case domain.EnrichmentOutcomePending:
			pending = true
		case domain.EnrichmentOutcomeNotObserved, domain.EnrichmentOutcomeError:
		default:
			return false
		}
		if (c.Code != "") != (c.Outcome == domain.EnrichmentOutcomeError) {
			return false
		}
	}
	return pending
}
