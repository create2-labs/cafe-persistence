package cpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cafe-persistence/internal/cpstore"

	"github.com/google/uuid"
)

// Store is the owner-scoped CP persistence surface consumed by HTTP handlers.
type Store interface {
	SaveDraft(scope cpstore.OwnerScope, draftID uuid.UUID, scanID *uuid.UUID, payload map[string]any) (cpstore.DraftRecord, error)
	GetDraft(scope cpstore.OwnerScope, draftID uuid.UUID) (cpstore.DraftRecord, error)
	DeleteDraft(scope cpstore.OwnerScope, draftID uuid.UUID) error
	PersistDraftOnce(scope cpstore.OwnerScope, draftID uuid.UUID, in cpstore.PersistDraftInput) (cpstore.PersistDraftResult, error)
	GetPolicy(scope cpstore.OwnerScope, policyID uuid.UUID) (cpstore.PolicyRecord, error)
	DeletePolicy(scope cpstore.OwnerScope, policyID uuid.UUID) error
	ListPersistedPoliciesForScan(scope cpstore.OwnerScope, scanID uuid.UUID, limit, offset int) (cpstore.ListPoliciesResult, error)
	CountPoliciesByWallet(scope cpstore.OwnerScope, walletAddress string) (cpstore.WalletReferenceCount, error)
	CountPoliciesByScan(scope cpstore.OwnerScope, scanID uuid.UUID) (cpstore.ScanReferenceCount, error)
}

// Handler implements the internal CP API (openapi/internal/cp/v1.yaml).
type Handler struct {
	store Store
}

// NewHandler wires CP API dependencies.
func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) requireOwnerScope(w http.ResponseWriter, r *http.Request) (cpstore.OwnerScope, bool) {
	rawUser := strings.TrimSpace(r.Header.Get(headerUserID))
	if rawUser == "" {
		writeBadRequest(w, r, "X-User-Id header is required")
		return cpstore.OwnerScope{}, false
	}
	if _, err := uuid.Parse(rawUser); err != nil {
		writeBadRequest(w, r, "X-User-Id must be a valid UUID")
		return cpstore.OwnerScope{}, false
	}
	return cpstore.OwnerScope{
		UserID:   rawUser,
		TenantID: strings.TrimSpace(r.Header.Get(headerTenantID)),
	}, true
}

func (h *Handler) requireStore(w http.ResponseWriter, r *http.Request) bool {
	if h.store == nil {
		writeServiceUnavailable(w, r)
		return false
	}
	return true
}

type draftUpsertBody struct {
	ScanID  *uuid.UUID     `json:"scan_id"`
	Payload map[string]any `json:"payload"`
}

func (h *Handler) UpsertDraft(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.requireOwnerScope(w, r)
	if !ok {
		return
	}
	if !h.requireStore(w, r) {
		return
	}
	draftID, err := parseDraftIDPath(r)
	if err != nil {
		writeBadRequest(w, r, err.Error())
		return
	}
	var req draftUpsertBody
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, r, "invalid request body")
		return
	}
	if req.Payload == nil {
		req.Payload = map[string]any{}
	}
	rec, err := h.store.SaveDraft(scope, draftID, req.ScanID, req.Payload)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, draftRowJSON(rec))
}

func (h *Handler) GetDraft(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.requireOwnerScope(w, r)
	if !ok {
		return
	}
	if !h.requireStore(w, r) {
		return
	}
	draftID, err := parseDraftIDPath(r)
	if err != nil {
		writeBadRequest(w, r, err.Error())
		return
	}
	rec, err := h.store.GetDraft(scope, draftID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, draftRowJSON(rec))
}

func (h *Handler) DeleteDraft(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.requireOwnerScope(w, r)
	if !ok {
		return
	}
	if !h.requireStore(w, r) {
		return
	}
	draftID, err := parseDraftIDPath(r)
	if err != nil {
		writeBadRequest(w, r, err.Error())
		return
	}
	if err := h.store.DeleteDraft(scope, draftID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type persistDraftRequest struct {
	WalletAddress           string    `json:"wallet_address"`
	ChainID                 int64     `json:"chain_id"`
	ScanID                  uuid.UUID `json:"scan_id"`
	WalletControlVerifiedAt time.Time `json:"wallet_control_verified_at"`
}

func (h *Handler) PersistDraft(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.requireOwnerScope(w, r)
	if !ok {
		return
	}
	if !h.requireStore(w, r) {
		return
	}
	draftID, err := parseDraftIDPath(r)
	if err != nil {
		writeBadRequest(w, r, err.Error())
		return
	}
	var req persistDraftRequest
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, r, "invalid request body")
		return
	}
	if strings.TrimSpace(req.WalletAddress) == "" || req.ChainID < 1 || req.ScanID == uuid.Nil || req.WalletControlVerifiedAt.IsZero() {
		writeBadRequest(w, r, "wallet_address, chain_id, scan_id, and wallet_control_verified_at are required")
		return
	}
	result, err := h.store.PersistDraftOnce(scope, draftID, cpstore.PersistDraftInput{
		WalletAddress: req.WalletAddress,
		ChainID:       req.ChainID,
		ScanID:        req.ScanID,
		VerifiedAt:    req.WalletControlVerifiedAt,
	})
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, persistDraftResponseJSON(draftID, result))
}

func (h *Handler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.requireOwnerScope(w, r)
	if !ok {
		return
	}
	if !h.requireStore(w, r) {
		return
	}
	policyID, err := parsePolicyIDPath(r)
	if err != nil {
		writeBadRequest(w, r, err.Error())
		return
	}
	rec, err := h.store.GetPolicy(scope, policyID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, policyRowJSON(rec))
}

func (h *Handler) DeletePolicy(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.requireOwnerScope(w, r)
	if !ok {
		return
	}
	if !h.requireStore(w, r) {
		return
	}
	policyID, err := parsePolicyIDPath(r)
	if err != nil {
		writeBadRequest(w, r, err.Error())
		return
	}
	if err := h.store.DeletePolicy(scope, policyID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListPoliciesByScan(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.requireOwnerScope(w, r)
	if !ok {
		return
	}
	if !h.requireStore(w, r) {
		return
	}
	scanIDStr := strings.TrimSpace(r.URL.Query().Get("scan_id"))
	if scanIDStr == "" {
		writeBadRequest(w, r, "scan_id query parameter is required")
		return
	}
	scanID, err := uuid.Parse(scanIDStr)
	if err != nil || scanID == uuid.Nil {
		writeBadRequest(w, r, "scan_id must be a valid UUID")
		return
	}
	limit, offset := parsePagination(r.URL.Query().Get("limit"), r.URL.Query().Get("offset"))
	result, err := h.store.ListPersistedPoliciesForScan(scope, scanID, limit, offset)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, row := range result.Items {
		items = append(items, policyRowJSON(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  result.Total,
		"limit":  result.Limit,
		"offset": result.Offset,
	})
}

func (h *Handler) CountPoliciesByWallet(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.requireOwnerScope(w, r)
	if !ok {
		return
	}
	if !h.requireStore(w, r) {
		return
	}
	wallet := strings.TrimSpace(r.URL.Query().Get("wallet_address"))
	if wallet == "" {
		writeBadRequest(w, r, "wallet_address query parameter is required")
		return
	}
	counts, err := h.store.CountPoliciesByWallet(scope, wallet)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"exists":        counts.Exists,
		"policy_count":  counts.PolicyCount,
		"draft_count":   counts.DraftCount,
	})
}

func (h *Handler) CountPoliciesByScan(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.requireOwnerScope(w, r)
	if !ok {
		return
	}
	if !h.requireStore(w, r) {
		return
	}
	scanIDStr := strings.TrimSpace(r.URL.Query().Get("scan_id"))
	if scanIDStr == "" {
		writeBadRequest(w, r, "scan_id query parameter is required")
		return
	}
	scanID, err := uuid.Parse(scanIDStr)
	if err != nil || scanID == uuid.Nil {
		writeBadRequest(w, r, "scan_id must be a valid UUID")
		return
	}
	counts, err := h.store.CountPoliciesByScan(scope, scanID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"referenced": counts.Referenced,
		"count":      counts.Count,
	})
}

func (h *Handler) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, cpstore.ErrDraftNotFound):
		writeDraftNotFound(w, r)
	case errors.Is(err, cpstore.ErrPolicyNotFound):
		writePolicyNotFound(w, r)
	case errors.Is(err, cpstore.ErrDraftAlreadyPersisted):
		writeDraftAlreadyPersisted(w, r)
	case errors.Is(err, cpstore.ErrForbidden):
		writeForbidden(w, r)
	case errors.Is(err, cpstore.ErrInvalidWalletAddress):
		writeInvalidWalletAddress(w, r, "wallet_address must be a normalized EVM address")
	case errors.Is(err, cpstore.ErrScanIDRequired), errors.Is(err, cpstore.ErrScanIDMismatch):
		writeBadRequest(w, r, err.Error())
	case errors.Is(err, cpstore.ErrOwnerRequired):
		writeBadRequest(w, r, "X-User-Id header is required")
	default:
		writeServiceUnavailable(w, r)
	}
}

func draftRowJSON(rec cpstore.DraftRecord) map[string]any {
	out := map[string]any{
		"id":         rec.ID.String(),
		"user_id":    rec.UserID,
		"payload":    rec.Payload,
		"status":     rec.Status,
		"created_at": rec.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": rec.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if rec.TenantID != "" {
		out["tenant_id"] = rec.TenantID
	}
	if rec.ScanID != nil {
		out["scan_id"] = rec.ScanID.String()
	}
	return out
}

func policyRowJSON(rec cpstore.PolicyRecord) map[string]any {
	out := map[string]any{
		"id":              rec.ID.String(),
		"user_id":         rec.UserID,
		"scan_id":         rec.ScanID.String(),
		"draft_id":        rec.DraftID.String(),
		"wallet_address":  rec.WalletAddress,
		"chain_id":        rec.ChainID,
		"payload":         rec.Payload,
		"status":          rec.Status,
		"persisted_at":    rec.PersistedAt.UTC().Format(time.RFC3339Nano),
		"created_at":      rec.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":      rec.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if rec.TenantID != "" {
		out["tenant_id"] = rec.TenantID
	}
	if rec.OwnershipStatus != "" {
		out["ownership_status"] = rec.OwnershipStatus
	}
	if rec.WalletControlMethod != "" {
		out["wallet_control_method"] = rec.WalletControlMethod
	}
	if rec.WalletControlVerifiedAt != nil {
		out["wallet_control_verified_at"] = rec.WalletControlVerifiedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

func persistDraftResponseJSON(draftID uuid.UUID, result cpstore.PersistDraftResult) map[string]any {
	return map[string]any{
		"policy_id":             result.PolicyID.String(),
		"draft_id":              draftID.String(),
		"scan_id":               result.ScanID.String(),
		"wallet_address":        result.WalletAddress,
		"chain_id":              result.ChainID,
		"status":                "persisted",
		"ownership_status":      "verified",
		"wallet_control_method": "eoa_signature",
		"persisted_at":          result.PersistedAt.UTC().Format(time.RFC3339Nano),
	}
}

func parseDraftIDPath(r *http.Request) (uuid.UUID, error) {
	return parseUUIDPath(r, "draft_id")
}

func parsePolicyIDPath(r *http.Request) (uuid.UUID, error) {
	return parseUUIDPath(r, "policy_id")
}

func parseUUIDPath(r *http.Request, name string) (uuid.UUID, error) {
	raw := strings.TrimSpace(r.PathValue(name))
	if raw == "" {
		return uuid.Nil, errors.New(name + " must be a UUID")
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New(name + " must be a UUID")
	}
	return id, nil
}

func decodeJSON(r *http.Request, dst any) error {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func parsePagination(limitStr, offsetStr string) (limit, offset int) {
	limit = 20
	offset = 0
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil {
			limit = v
		}
	}
	if offsetStr != "" {
		if v, err := strconv.Atoi(offsetStr); err == nil {
			offset = v
		}
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
