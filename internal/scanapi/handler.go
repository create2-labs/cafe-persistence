package scanapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cafe-persistence/internal/config"
	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/persistence/storage"
	"cafe-persistence/internal/repository"

	"github.com/google/uuid"
)

// Handler implements the internal scan API (openapi/internal/scan/v1.yaml).
type Handler struct {
	pending     repository.PendingV1ScanRepository
	walletScans repository.ScanResultRepository
	tlsScans    repository.TLSScanResultRepository
	ledger      repository.ScanUsageLedgerRepository
	cache       *storage.RedisCache
	chainCfg    *config.ChainConfig
}

// NewHandler wires scan API dependencies.
func NewHandler(
	pending repository.PendingV1ScanRepository,
	walletScans repository.ScanResultRepository,
	tlsScans repository.TLSScanResultRepository,
	ledger repository.ScanUsageLedgerRepository,
	cache *storage.RedisCache,
	chainCfg *config.ChainConfig,
) *Handler {
	return &Handler{
		pending:     pending,
		walletScans: walletScans,
		tlsScans:    tlsScans,
		ledger:      ledger,
		cache:       cache,
		chainCfg:    chainCfg,
	}
}

func (h *Handler) requireUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := strings.TrimSpace(r.Header.Get(headerUserID))
	if raw == "" {
		writeBadRequest(w, r, "X-User-Id header is required")
		return uuid.Nil, false
	}
	userID, err := uuid.Parse(raw)
	if err != nil || userID == uuid.Nil {
		writeBadRequest(w, r, "X-User-Id must be a valid UUID")
		return uuid.Nil, false
	}
	return userID, true
}

// --- pending wallet ---

type reserveWalletPendingRequest struct {
	ScanID    uuid.UUID  `json:"scan_id"`
	Address   string     `json:"address"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

func (h *Handler) ReserveWalletPending(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	if h.pending == nil {
		writeServiceUnavailable(w, r, "pending scan store is temporarily unavailable")
		return
	}
	var req reserveWalletPendingRequest
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, r, "invalid request body")
		return
	}
	if req.ScanID == uuid.Nil || strings.TrimSpace(req.Address) == "" {
		writeBadRequest(w, r, "scan_id and address are required")
		return
	}
	createdAt := time.Now().UTC()
	if req.CreatedAt != nil {
		createdAt = req.CreatedAt.UTC()
	}
	rec := &repository.PendingV1ScanRecord{
		ScanID:    req.ScanID,
		UserID:    userID,
		Family:    "wallet",
		Address:   normalizeAddress(req.Address),
		CreatedAt: createdAt,
	}
	reserved, err := h.pending.PutWallet(r.Context(), rec)
	if err != nil {
		writeServiceUnavailable(w, r, "pending scan store is temporarily unavailable")
		return
	}
	if !reserved {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error":   "SCAN_IN_PROGRESS",
			"message": "A wallet scan is already in progress for this target.",
		})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"reserved": true,
		"record":   pendingRecordJSON(rec),
	})
}

func (h *Handler) GetWalletPendingByAddress(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	if h.pending == nil {
		writeServiceUnavailable(w, r, "pending scan store is temporarily unavailable")
		return
	}
	addr := normalizeAddress(r.URL.Query().Get("address"))
	if addr == "" {
		writeBadRequest(w, r, "address query parameter is required")
		return
	}
	rec, err := h.pending.GetWalletByOwnerAddress(r.Context(), userID, addr)
	if err != nil {
		writeServiceUnavailable(w, r, "pending scan store is temporarily unavailable")
		return
	}
	if rec == nil {
		writeNotFound(w, r, "pending scan not found")
		return
	}
	writeJSON(w, http.StatusOK, pendingRecordJSON(rec))
}

func (h *Handler) ReleaseWalletPendingReservation(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	if h.pending == nil {
		writeServiceUnavailable(w, r, "pending scan store is temporarily unavailable")
		return
	}
	addr := normalizeAddress(r.URL.Query().Get("address"))
	scanIDStr := strings.TrimSpace(r.URL.Query().Get("scan_id"))
	if addr == "" || scanIDStr == "" {
		writeBadRequest(w, r, "address and scan_id query parameters are required")
		return
	}
	scanID, err := uuid.Parse(scanIDStr)
	if err != nil {
		writeBadRequest(w, r, "scan_id must be a UUID")
		return
	}
	if err := h.pending.DeleteWalletReservation(r.Context(), userID, addr, scanID); err != nil {
		writeServiceUnavailable(w, r, "pending scan store is temporarily unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- pending TLS ---

type putTLSPendingRequest struct {
	ScanID    uuid.UUID  `json:"scan_id"`
	Endpoint  string     `json:"endpoint"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

func (h *Handler) PutTLSPending(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	if h.pending == nil {
		writeServiceUnavailable(w, r, "pending scan store is temporarily unavailable")
		return
	}
	var req putTLSPendingRequest
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, r, "invalid request body")
		return
	}
	if req.ScanID == uuid.Nil || strings.TrimSpace(req.Endpoint) == "" {
		writeBadRequest(w, r, "scan_id and endpoint are required")
		return
	}
	createdAt := time.Now().UTC()
	if req.CreatedAt != nil {
		createdAt = req.CreatedAt.UTC()
	}
	rec := &repository.PendingV1ScanRecord{
		ScanID:    req.ScanID,
		UserID:    userID,
		Family:    "tls",
		Endpoint:  strings.TrimSpace(req.Endpoint),
		CreatedAt: createdAt,
	}
	if err := h.pending.Put(r.Context(), rec); err != nil {
		writeServiceUnavailable(w, r, "pending scan store is temporarily unavailable")
		return
	}
	writeJSON(w, http.StatusCreated, pendingRecordJSON(rec))
}

func (h *Handler) GetPendingScan(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	if h.pending == nil {
		writeServiceUnavailable(w, r, "pending scan store is temporarily unavailable")
		return
	}
	scanID, err := parseScanIDPath(r)
	if err != nil {
		writeBadRequest(w, r, err.Error())
		return
	}
	rec, err := h.pending.Get(r.Context(), scanID)
	if err != nil {
		writeServiceUnavailable(w, r, "pending scan store is temporarily unavailable")
		return
	}
	if rec == nil || rec.UserID != userID {
		writeNotFound(w, r, "pending scan not found")
		return
	}
	writeJSON(w, http.StatusOK, pendingRecordJSON(rec))
}

func (h *Handler) ReleasePendingScan(w http.ResponseWriter, r *http.Request) {
	_, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	if h.pending == nil {
		writeServiceUnavailable(w, r, "pending scan store is temporarily unavailable")
		return
	}
	scanID, err := parseScanIDPath(r)
	if err != nil {
		writeBadRequest(w, r, err.Error())
		return
	}
	if err := h.pending.Delete(r.Context(), scanID); err != nil {
		writeServiceUnavailable(w, r, "pending scan store is temporarily unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- wallet scans ---

func (h *Handler) ListWalletScans(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	if h.walletScans == nil {
		writeServiceUnavailable(w, r, "wallet scan store is temporarily unavailable")
		return
	}
	q := r.URL.Query()
	limit, offset := parsePagination(q.Get("limit"), q.Get("offset"))
	addrQ := strings.TrimSpace(q.Get("address"))
	chainQ := strings.TrimSpace(q.Get("chain_id"))
	latest, latestErr := parseBoolQuery(q.Get("latest"))
	if latestErr != nil {
		writeBadRequest(w, r, latestErr.Error())
		return
	}
	if latest && addrQ == "" {
		writeBadRequest(w, r, "latest requires address")
		return
	}
	if chainQ != "" && addrQ == "" {
		writeBadRequest(w, r, "chain_id requires address")
		return
	}
	normalizedAddr := normalizeAddress(addrQ)
	var chainID *int64
	if chainQ != "" {
		v, perr := strconv.ParseInt(chainQ, 10, 64)
		if perr != nil || v <= 0 {
			writeBadRequest(w, r, "chain_id must be a positive integer")
			return
		}
		chainID = &v
	}

	var entities []*domain.ScanResultEntity
	var total int64

	if latest {
		all, lerr := h.walletScans.ListOwnerWalletScansByAddress(userID, normalizedAddr)
		if lerr != nil {
			writeServiceUnavailable(w, r, "wallet scan store is temporarily unavailable")
			return
		}
		for _, ent := range all {
			if !walletEntityIsCompleted(ent) {
				continue
			}
			if chainID != nil && !walletEntityMatchesChainID(ent, *chainID, h.chainCfg) {
				continue
			}
			entities = []*domain.ScanResultEntity{ent}
			total = 1
			break
		}
	} else if normalizedAddr != "" && chainID != nil {
		all, lerr := h.walletScans.ListOwnerWalletScansByAddress(userID, normalizedAddr)
		if lerr != nil {
			writeServiceUnavailable(w, r, "wallet scan store is temporarily unavailable")
			return
		}
		filtered := make([]*domain.ScanResultEntity, 0, len(all))
		for _, ent := range all {
			if walletEntityMatchesChainID(ent, *chainID, h.chainCfg) {
				filtered = append(filtered, ent)
			}
		}
		total = int64(len(filtered))
		entities = paginateWalletScanEntities(filtered, limit, offset)
	} else if normalizedAddr != "" {
		var qerr error
		entities, total, qerr = h.walletScans.ListOwnerWalletScansDiscoveryV1(userID, normalizedAddr, limit, offset)
		if qerr != nil {
			writeServiceUnavailable(w, r, "wallet scan store is temporarily unavailable")
			return
		}
	} else {
		var qerr error
		entities, total, qerr = h.walletScans.ListOwnerWalletScansDiscoveryV1(userID, "", limit, offset)
		if qerr != nil {
			writeServiceUnavailable(w, r, "wallet scan store is temporarily unavailable")
			return
		}
	}

	items := make([]map[string]any, 0, len(entities))
	for _, e := range entities {
		items = append(items, walletScanRowJSON(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *Handler) GetWalletScan(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	if h.walletScans == nil {
		writeServiceUnavailable(w, r, "wallet scan store is temporarily unavailable")
		return
	}
	scanID, err := parseScanIDPath(r)
	if err != nil {
		writeBadRequest(w, r, err.Error())
		return
	}
	ent, qerr := h.walletScans.FindOwnedWalletScanByID(userID, scanID)
	if qerr != nil {
		writeServiceUnavailable(w, r, "wallet scan store is temporarily unavailable")
		return
	}
	if ent == nil {
		writeNotFound(w, r, "scan not found")
		return
	}
	writeJSON(w, http.StatusOK, walletScanRowJSON(ent))
}

func (h *Handler) DeleteWalletScan(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	if h.walletScans == nil {
		writeServiceUnavailable(w, r, "wallet scan store is temporarily unavailable")
		return
	}
	scanID, err := parseScanIDPath(r)
	if err != nil {
		writeBadRequest(w, r, err.Error())
		return
	}
	ent, qerr := h.walletScans.FindOwnedWalletScanByID(userID, scanID)
	if qerr != nil {
		writeServiceUnavailable(w, r, "wallet scan store is temporarily unavailable")
		return
	}
	if ent == nil {
		writeNotFound(w, r, "scan not found")
		return
	}
	deleted, derr := h.walletScans.DeleteOwnedWalletScan(userID, scanID)
	if derr != nil {
		writeServiceUnavailable(w, r, "wallet scan store is temporarily unavailable")
		return
	}
	if !deleted {
		writeNotFound(w, r, "scan not found")
		return
	}
	if h.cache != nil {
		remaining, rerr := h.walletScans.ListOwnerWalletScansByAddress(userID, ent.Address)
		if rerr != nil {
			writeServiceUnavailable(w, r, "wallet scan store is temporarily unavailable")
			return
		}
		if len(remaining) == 0 {
			_ = h.cache.DeleteWalletScan(r.Context(), userID, ent.Address)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- TLS scans ---

func (h *Handler) ListTLSScans(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	if h.tlsScans == nil {
		writeServiceUnavailable(w, r, "tls scan store is temporarily unavailable")
		return
	}
	limit, offset := parsePagination(r.URL.Query().Get("limit"), r.URL.Query().Get("offset"))
	entities, total, err := h.tlsScans.ListOwnerUserTLSScansDiscoveryV1(userID, limit, offset)
	if err != nil {
		writeServiceUnavailable(w, r, "tls scan store is temporarily unavailable")
		return
	}
	items := make([]map[string]any, 0, len(entities))
	for _, e := range entities {
		items = append(items, tlsScanRowJSON(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *Handler) ListTLSDefaultScans(w http.ResponseWriter, r *http.Request) {
	_, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	if h.tlsScans == nil {
		writeServiceUnavailable(w, r, "tls scan store is temporarily unavailable")
		return
	}
	entities, err := h.tlsScans.FindAllDefault()
	if err != nil {
		writeServiceUnavailable(w, r, "tls scan store is temporarily unavailable")
		return
	}
	items := make([]map[string]any, 0, len(entities))
	for _, e := range entities {
		items = append(items, tlsScanRowJSON(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  int64(len(items)),
		"limit":  len(items),
		"offset": 0,
	})
}

func (h *Handler) GetTLSScan(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	if h.tlsScans == nil {
		writeServiceUnavailable(w, r, "tls scan store is temporarily unavailable")
		return
	}
	scanID, err := parseScanIDPath(r)
	if err != nil {
		writeBadRequest(w, r, err.Error())
		return
	}
	ent, qerr := h.tlsScans.FindOwnedUserTLSScanByID(userID, scanID)
	if qerr != nil {
		writeServiceUnavailable(w, r, "tls scan store is temporarily unavailable")
		return
	}
	if ent == nil {
		ent, qerr = h.tlsScans.FindDefaultTLSScanByID(scanID)
		if qerr != nil {
			writeServiceUnavailable(w, r, "tls scan store is temporarily unavailable")
			return
		}
	}
	if ent == nil {
		writeNotFound(w, r, "scan not found")
		return
	}
	writeJSON(w, http.StatusOK, tlsScanRowJSON(ent))
}

func (h *Handler) DeleteTLSScan(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	if h.tlsScans == nil {
		writeServiceUnavailable(w, r, "tls scan store is temporarily unavailable")
		return
	}
	scanID, err := parseScanIDPath(r)
	if err != nil {
		writeBadRequest(w, r, err.Error())
		return
	}
	ent, qerr := h.tlsScans.FindOwnedUserTLSScanByID(userID, scanID)
	if qerr != nil {
		writeServiceUnavailable(w, r, "tls scan store is temporarily unavailable")
		return
	}
	if ent == nil {
		writeNotFound(w, r, "scan not found")
		return
	}
	deleted, derr := h.tlsScans.DeleteOwnedUserTLSScan(userID, scanID)
	if derr != nil {
		writeServiceUnavailable(w, r, "tls scan store is temporarily unavailable")
		return
	}
	if !deleted {
		writeNotFound(w, r, "scan not found")
		return
	}
	if h.cache != nil && ent.URL != "" {
		_ = h.cache.DeleteTLSScan(r.Context(), userID, ent.URL)
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- ledger ---

func (h *Handler) GetScanLedgerUsage(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}
	if h.ledger == nil {
		writeServiceUnavailable(w, r, "ledger store is temporarily unavailable")
		return
	}
	kindStr := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kindStr == "" {
		writeBadRequest(w, r, "kind query parameter is required")
		return
	}
	kind := domain.ScanUsageKind(kindStr)
	switch kind {
	case domain.ScanUsageKindWallet, domain.ScanUsageKindEndpoint:
	default:
		writeBadRequest(w, r, "kind must be wallet or endpoint")
		return
	}
	successCount, err := h.ledger.CountSuccessUsage(userID, kind)
	if err != nil {
		writeServiceUnavailable(w, r, "ledger store is temporarily unavailable")
		return
	}
	inFlight, err := h.ledger.CountInFlightScans(userID, kind)
	if err != nil {
		writeServiceUnavailable(w, r, "ledger store is temporarily unavailable")
		return
	}
	visibleSuccess, err := h.ledger.CountVisibleSuccessScans(userID, kind)
	if err != nil {
		writeServiceUnavailable(w, r, "ledger store is temporarily unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":               userID.String(),
		"kind":                  kindStr,
		"success_count":         successCount,
		"in_flight_count":       inFlight,
		"visible_success_count": visibleSuccess,
	})
}

// --- JSON mapping ---

func pendingRecordJSON(rec *repository.PendingV1ScanRecord) map[string]any {
	out := map[string]any{
		"scan_id":    rec.ScanID.String(),
		"user_id":    rec.UserID.String(),
		"family":     rec.Family,
		"created_at": rec.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if rec.Address != "" {
		out["address"] = rec.Address
	}
	if rec.Endpoint != "" {
		out["endpoint"] = rec.Endpoint
	}
	return out
}

func walletScanRowJSON(e *domain.ScanResultEntity) map[string]any {
	return map[string]any{
		"id":               e.ID.String(),
		"user_id":          e.UserID.String(),
		"address":          e.Address,
		"type":             string(e.Type),
		"algorithm":        string(e.Algorithm),
		"nist_level":       int(e.NISTLevel),
		"key_exposed":      e.KeyExposed,
		"public_key":       e.PublicKey,
		"transaction_hash": e.TransactionHash,
		"exposed_network":  e.ExposedNetwork,
		"is_eoa":           e.IsEOA,
		"is_erc4337":       e.IsERC4337,
		"risk_score":       e.RiskScore,
		"networks":         e.Networks,
		"connections":      e.Connections,
		"status":           e.Status,
		"error":            e.Error,
		"created_at":       e.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":       e.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func tlsScanRowJSON(e *domain.TLSScanResultEntity) map[string]any {
	out := map[string]any{
		"id":               e.ID.String(),
		"url":              e.URL,
		"host":             e.Host,
		"port":             e.Port,
		"protocol_version": e.ProtocolVersion,
		"nist_level":       int(e.NISTLevel),
		"risk_score":       e.RiskScore,
		"pqc_risk":         e.PQCRisk,
		"kex_algorithm":    e.KexAlgorithm,
		"kex_pqc_ready":    e.KexPQCReady,
		"pqc_mode":         e.PQCMode,
		"pfs":              e.PFS,
		"alpn":             e.ALPN,
		"ocsp_stapled":     e.OCSPStapled,
		"curve":            e.Curve,
		"certificate":      e.Certificate,
		"cipher_suites":    e.CipherSuites,
		"supported_pqcs":   e.SupportedPQCs,
		"recommendations":  e.Recommendations,
		"nist_levels":      e.NISTLevels,
		"default":          e.Default,
		"status":           e.Status,
		"error":            e.Error,
		"created_at":       e.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":       e.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if e.UserID != nil {
		out["user_id"] = e.UserID.String()
	}
	return out
}

func parseScanIDPath(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(r.PathValue("scan_id"))
	if raw == "" {
		return uuid.Nil, errors.New("scan_id must be a UUID")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, errors.New("scan_id must be a UUID")
	}
	return id, nil
}

func decodeJSON(r *http.Request, dst any) error {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func parseBoolQuery(raw string) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, errors.New("latest must be a boolean")
	}
	return v, nil
}
