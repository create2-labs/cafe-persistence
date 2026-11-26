package scanapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// Limits of the open-enrichment list (technical design §5.13). A value outside 1..openEnrichmentsMaxLimit is
// rejected, never clamped.
const (
	openEnrichmentsDefaultLimit = 100
	openEnrichmentsMaxLimit     = 100
)

// openEnrichmentItem is one item of the open-enrichment list (design §5.11). It carries no chains: the stored
// value is never decoded, so a row that does not decode cannot fail the list (OP-5.14).
type openEnrichmentItem struct {
	EnrichmentID string `json:"enrichment_id"`
	ScanID       string `json:"scan_id"`
	UserID       string `json:"user_id"`
	Family       string `json:"family"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
}

// ListOpenEnrichments serves GET /internal/scan/v1/enrichments (technical design §5.11). It takes no X-User-Id.
func (h *Handler) ListOpenEnrichments(w http.ResponseWriter, r *http.Request) {
	if h.walletScans == nil {
		writeServiceUnavailable(w, r, "wallet scan store is temporarily unavailable")
		return
	}
	statuses, before, limit, err := parseOpenEnrichmentsQuery(r)
	if err != nil {
		writeBadRequest(w, r, err.Error())
		return
	}
	rows, err := h.walletScans.ListOpenEnrichments(statuses, before, limit)
	if err != nil {
		log.Error().Err(err).Msg("list open enrichments failed")
		writeServiceUnavailable(w, r, "wallet scan store is temporarily unavailable")
		return
	}
	items := make([]openEnrichmentItem, 0, len(rows))
	for _, e := range rows {
		items = append(items, openEnrichmentItem{
			EnrichmentID: e.EnrichmentID.String(),
			ScanID:       e.ScanID.String(),
			UserID:       e.UserID.String(),
			Family:       e.Family,
			Status:       e.Status,
			CreatedAt:    e.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// parseOpenEnrichmentsQuery applies the parameter rules of design §5.11; every violation is an error.
func parseOpenEnrichmentsQuery(r *http.Request) (statuses []string, before time.Time, limit int, err error) {
	q := r.URL.Query()

	statuses = []string{"PENDING", "RUNNING"}
	if q.Has("status") {
		statuses = strings.Split(q.Get("status"), ",")
		seen := make(map[string]bool, len(statuses))
		for _, s := range statuses {
			if s != "PENDING" && s != "RUNNING" {
				return nil, time.Time{}, 0, fmt.Errorf("status values must be PENDING or RUNNING")
			}
			if seen[s] {
				return nil, time.Time{}, 0, fmt.Errorf("status must not repeat a value")
			}
			seen[s] = true
		}
	}

	before, err = time.Parse(time.RFC3339, q.Get("created_before"))
	if err != nil {
		return nil, time.Time{}, 0, fmt.Errorf("created_before is required and must be RFC 3339")
	}
	before = before.UTC()

	limit = openEnrichmentsDefaultLimit
	if q.Has("limit") {
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil || limit < 1 || limit > openEnrichmentsMaxLimit {
			return nil, time.Time{}, 0, fmt.Errorf("limit must be an integer from 1 to %d", openEnrichmentsMaxLimit)
		}
	}
	return statuses, before, limit, nil
}
