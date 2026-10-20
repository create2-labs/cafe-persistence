package handlers

import (
	"context"
	"encoding/json"
	"time"

	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/persistence/planlimit"
	"cafe-persistence/internal/persistence/storage"
	"cafe-persistence/internal/repository"
	"cafe-persistence/pkg/nats"
	"cafe-persistence/pkg/scan"

	lifecycle "github.com/create2-labs/cafe-contracts/discovery/scanlifecycle/v1"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

type ScanEventHandler struct {
	tlsWriter  *storage.TLSWriter
	redisCache *storage.RedisCache
	natsConn   nats.Connection // optional: when set, publish scan.ready after writing to Redis so API can return result on GET
	db         *gorm.DB
	ledger     repository.ScanUsageLedgerRepository
	planLimits *planlimit.Resolver
}

const IGNORE_SCAN_MSG = "unknown scan kind, ignoring"

func NewScanEventHandler(
	tlsWriter *storage.TLSWriter,
	walletWriter *storage.WalletWriter,
	redisCache *storage.RedisCache,
	natsConn nats.Connection,
	chainIDsByNetwork map[string]int64,
	db *gorm.DB,
	ledger repository.ScanUsageLedgerRepository,
	planLimits *planlimit.Resolver,
) *ScanEventHandler {
	// Wallet NATS no longer writes or publishes. These parameters stay so existing callers do not change.
	_ = chainIDsByNetwork
	_ = walletWriter
	return &ScanEventHandler{
		tlsWriter:  tlsWriter,
		redisCache: redisCache,
		natsConn:   natsConn,
		db:         db,
		ledger:     ledger,
		planLimits: planLimits,
	}
}

func (h *ScanEventHandler) applyStarted(msg *scanStarted) error {
	// TODO: tracing integration
	log.Info().
		Str("scan_id", msg.ScanID.String()).
		Str("kind", msg.Kind).
		Str("user_id", msg.UserID.String()).
		Msg("scan.started")

	switch msg.Kind {
	case "tls":
		var userID *uuid.UUID
		if msg.UserID != uuid.Nil {
			userID = &msg.UserID
		}
		if err := h.tlsWriter.OnStarted(msg.ScanID, userID, msg.Endpoint); err != nil {
			log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: OnStarted TLS failed")
			return err
		}
	case "wallet":
		ignoreWalletLifecycle(msg.ScanID)
	default:
		log.Warn().Str("kind", msg.Kind).Msg(IGNORE_SCAN_MSG)
	}
	return nil
}

func (h *ScanEventHandler) applyCompleted(msg *scanCompleted) error {
	log.Info().
		Str("scan_id", msg.ScanID.String()).
		Str("kind", msg.Kind).
		Str("user_id", msg.UserID.String()).
		Msg("scan.completed")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	switch msg.Kind {
	case "tls":
		return h.handleTLSCompleted(ctx, msg)
	case "wallet":
		ignoreWalletLifecycle(msg.ScanID)
		return nil
	default:
		log.Warn().Str("kind", msg.Kind).Msg(IGNORE_SCAN_MSG)
		return nil
	}
}

func (h *ScanEventHandler) handleTLSCompleted(ctx context.Context, msg *scanCompleted) error {
	current, _ := h.tlsWriter.GetStatus(msg.ScanID)
	if current == "" {
		log.Warn().
			Str("scan_id", msg.ScanID.String()).
			Str("subject", lifecycle.SubjectScanCompleted).
			Msg("persistence: missing scan on completed (no row yet), will insert")
	}
	if !scan.ValidTransition(current, scan.StateSUCCESS) {
		log.Warn().
			Str("scan_id", msg.ScanID.String()).
			Str("current_status", current).
			Str("attempted_status", scan.StateSUCCESS).
			Str("subject", lifecycle.SubjectScanCompleted).
			Msg("persistence: invalid transition or duplicate completion, ignoring")
		return nil
	}
	var result domain.TLSScanResult
	if err := h.decodeResult(msg.Result, &result); err != nil {
		log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: decode TLS result failed")
		return err
	}
	userID := &msg.UserID
	if msg.UserID == uuid.Nil {
		userID = nil
	}
	entity := domain.FromTLSScanResult(userID, &result, result.Default)
	entity.ID = msg.ScanID
	acquired, err := h.commitTLSCompletion(msg, entity, &result)
	if err != nil {
		log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: commit TLS completion failed")
		return err
	}
	if !acquired {
		h.publishScanReady(msg.UserID, "tls", msg.Endpoint, "", "failed")
		return nil
	}
	if err := h.redisCache.SaveTLSScan(ctx, msg.UserID, msg.Endpoint, &result); err != nil {
		log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: Redis TLS write failed")
		return err
	}
	h.publishScanReady(msg.UserID, "tls", msg.Endpoint, "", "success")
	return nil
}

func ignoreWalletLifecycle(scanID uuid.UUID) {
	log.Info().
		Str("scan_id", scanID.String()).
		Msg("persistence: wallet event ignored; ApplyWalletTransition owns wallet state")
}

func (h *ScanEventHandler) decodeResult(in interface{}, out interface{}) error {
	if in == nil {
		return nil
	}
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (h *ScanEventHandler) commitTLSCompletion(
	msg *scanCompleted,
	entity *domain.TLSScanResultEntity,
	result *domain.TLSScanResult,
) (acquired bool, err error) {
	if msg.UserID == uuid.Nil || h.db == nil || h.ledger == nil || h.planLimits == nil {
		if err := h.tlsWriter.OnCompleted(msg.ScanID, entity); err != nil {
			return false, err
		}
		return true, nil
	}

	limit, unlimited, err := h.planLimits.ScanLimit(msg.UserID, domain.ScanUsageKindEndpoint)
	if err != nil {
		return false, err
	}

	url := msg.Endpoint
	if url == "" && result != nil {
		url = result.URL
	}
	if url == "" {
		url = entity.URL
	}

	userID := &msg.UserID
	if msg.UserID == uuid.Nil {
		userID = nil
	}

	err = h.db.Transaction(func(tx *gorm.DB) error {
		var slotErr error
		acquired, slotErr = h.confirmExistingUsageOrAcquire(
			tx, msg.UserID, msg.ScanID, domain.ScanUsageKindEndpoint, limit, unlimited,
		)
		if slotErr != nil {
			return slotErr
		}
		if acquired {
			return h.tlsWriter.OnCompletedInTx(tx, msg.ScanID, entity)
		}
		return h.tlsWriter.OnPlanLimitExceededInTx(tx, msg.ScanID, userID, url)
	})
	return acquired, err
}

// confirmExistingUsageOrAcquire keeps a ledger row that already belongs to this scan_id.
// That row is the reservation (or a previous completion). It confirms the scan and does not
// take another credit. A missing row is acquired under the plan limit.
func (h *ScanEventHandler) confirmExistingUsageOrAcquire(
	tx *gorm.DB,
	userID, scanID uuid.UUID,
	kind domain.ScanUsageKind,
	limit int,
	unlimited bool,
) (bool, error) {
	reserved, err := h.ledger.HasSuccessUsageForScanInTx(tx, scanID)
	if err != nil {
		return false, err
	}
	if reserved {
		return true, nil
	}
	if unlimited {
		if err := h.ledger.RecordSuccessUsageInTx(tx, userID, scanID, kind); err != nil {
			return false, err
		}
		return true, nil
	}
	acquired, err := h.ledger.RecordSuccessUsageIfUnderLimitInTx(tx, userID, scanID, kind, limit)
	if err != nil || acquired {
		return acquired, err
	}
	// A concurrent completion of this same scan_id may have inserted the row after the first read.
	reserved, err = h.ledger.HasSuccessUsageForScanInTx(tx, scanID)
	if err != nil {
		return false, err
	}
	return reserved, nil
}

func (h *ScanEventHandler) applyFailed(msg *scanFailed) error {
	log.Info().
		Str("scan_id", msg.ScanID.String()).
		Str("kind", msg.Kind).
		Str("user_id", msg.UserID.String()).
		Str("error", msg.Error).
		Msg("scan.failed")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch msg.Kind {
	case "tls":
		return h.handleTLSFailed(ctx, msg)
	case "wallet":
		ignoreWalletLifecycle(msg.ScanID)
		return nil
	default:
		log.Warn().Str("kind", msg.Kind).Msg(IGNORE_SCAN_MSG)
		return nil
	}
}

func (h *ScanEventHandler) handleTLSFailed(ctx context.Context, msg *scanFailed) error {
	current, err := h.tlsWriter.GetStatus(msg.ScanID)
	if err != nil {
		log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: GetStatus failed")
		return err
	}
	if current == "" {
		log.Warn().
			Str("scan_id", msg.ScanID.String()).
			Str("subject", lifecycle.SubjectScanFailed).
			Msg("persistence: missing scan on failed (no row yet), will insert")
	} else if !scan.ValidTransition(current, scan.StateFAILED) {
		log.Warn().
			Str("scan_id", msg.ScanID.String()).
			Str("current_status", current).
			Str("attempted_status", scan.StateFAILED).
			Str("subject", lifecycle.SubjectScanFailed).
			Msg("persistence: invalid transition, ignoring")
		return nil
	}
	userID := (*uuid.UUID)(nil)
	if msg.UserID != uuid.Nil {
		userID = &msg.UserID
	}
	if err := h.persistTLSFailure(msg.ScanID, userID, msg.Endpoint, msg.Error); err != nil {
		log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: OnFailed TLS failed")
		return err
	}
	if err := h.redisCache.SaveTLSFailure(ctx, msg.UserID, msg.Endpoint, msg.Error); err != nil {
		log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: Redis TLS failure write failed")
		return err
	}
	h.publishScanReady(msg.UserID, "tls", msg.Endpoint, "", "failed")
	return nil
}

func (h *ScanEventHandler) persistTLSFailure(scanID uuid.UUID, userID *uuid.UUID, url, errMsg string) error {
	if h.db == nil || h.ledger == nil {
		return h.tlsWriter.OnFailed(scanID, userID, url, errMsg)
	}
	return h.db.Transaction(func(tx *gorm.DB) error {
		if err := h.tlsWriter.OnFailedInTx(tx, scanID, userID, url, errMsg); err != nil {
			return err
		}
		return h.ledger.ReleaseSuccessUsageByScanIDInTx(tx, scanID)
	})
}

func (h *ScanEventHandler) publishScanReady(userID uuid.UUID, kind, endpoint, address, status string) {
	if h.natsConn == nil {
		return
	}
	ready := lifecycle.Ready{
		UserID: userID.String(), Kind: kind, Endpoint: endpoint, Address: address, Status: status,
	}
	if err := nats.PublishJSON(h.natsConn, lifecycle.SubjectScanReady, ready); err != nil {
		log.Warn().Err(err).Str("kind", kind).Str("status", status).Msg("persistence: scan.ready publish failed")
	}
}

// Ensure valid state transitions (idempotency: same event twice is safe)
var _ = []interface{}{
	scan.StatePENDING,
	scan.StateRUNNING,
	scan.StateSUCCESS,
	scan.StateFAILED,
	scan.StateTIMEOUT,
	scan.StateUNREACHABLE,
}
