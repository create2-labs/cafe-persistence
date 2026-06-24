package handlers

import (
	"context"
	"encoding/json"
	"time"

	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/persistence/planlimit"
	"cafe-persistence/internal/persistence/storage"
	"cafe-persistence/internal/repository"
	"cafe-persistence/internal/walletobservation"
	"cafe-persistence/pkg/nats"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

type ScanEventHandler struct {
	tlsWriter         *storage.TLSWriter
	walletWriter      *storage.WalletWriter
	redisCache        *storage.RedisCache
	natsConn          nats.Connection  // optional: when set, publish scan.ready after writing to Redis so API can return result on GET
	chainIDsByNetwork map[string]int64 // blockchains[].name → chain_id; optional, from config.ChainConfig.ChainIDByNetwork()
	db                *gorm.DB
	ledger            repository.ScanUsageLedgerRepository
	planLimits        *planlimit.Resolver
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
	return &ScanEventHandler{
		tlsWriter:         tlsWriter,
		walletWriter:      walletWriter,
		redisCache:        redisCache,
		natsConn:          natsConn,
		chainIDsByNetwork: chainIDsByNetwork,
		db:                db,
		ledger:            ledger,
		planLimits:        planLimits,
	}
}

func (h *ScanEventHandler) HandleStarted(msg *nats.ScanStartedMessage) error {
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
		if err := h.walletWriter.OnStarted(msg.ScanID, msg.UserID, msg.Address); err != nil {
			log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: OnStarted wallet failed")
			return err
		}
	default:
		log.Warn().Str("kind", msg.Kind).Msg(IGNORE_SCAN_MSG)
	}
	return nil
}

const subjectScanCompleted = "scan.completed"

func (h *ScanEventHandler) HandleCompleted(msg *nats.ScanCompletedMessage) error {
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
		return h.handleWalletCompleted(ctx, msg)
	default:
		log.Warn().Str("kind", msg.Kind).Msg(IGNORE_SCAN_MSG)
		return nil
	}
}

func (h *ScanEventHandler) handleTLSCompleted(ctx context.Context, msg *nats.ScanCompletedMessage) error {
	current, _ := h.tlsWriter.GetStatus(msg.ScanID)
	if current == "" {
		log.Warn().
			Str("scan_id", msg.ScanID.String()).
			Str("subject", subjectScanCompleted).
			Msg("persistence: missing scan on completed (no row yet), will insert")
	}
	if !scan.ValidTransition(current, scan.StateSUCCESS) {
		log.Warn().
			Str("scan_id", msg.ScanID.String()).
			Str("current_status", current).
			Str("attempted_status", scan.StateSUCCESS).
			Str("subject", subjectScanCompleted).
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

func (h *ScanEventHandler) handleWalletCompleted(ctx context.Context, msg *nats.ScanCompletedMessage) error {
	log.Debug().
		Str("scan_id", msg.ScanID.String()).
		Str("user_id", msg.UserID.String()).
		Str("address", msg.Address).
		Msg("persistence: wallet scan.completed received, checking status")
	current, _ := h.walletWriter.GetStatus(msg.ScanID)
	log.Debug().
		Str("scan_id", msg.ScanID.String()).
		Str("current_status", current).
		Msg("persistence: wallet GetStatus result")
	if current == "" {
		log.Warn().
			Str("scan_id", msg.ScanID.String()).
			Str("subject", subjectScanCompleted).
			Msg("persistence: missing scan on completed (no row yet), will insert")
	}
	if !scan.ValidTransition(current, scan.StateSUCCESS) {
		log.Warn().
			Str("scan_id", msg.ScanID.String()).
			Str("current_status", current).
			Str("attempted_status", scan.StateSUCCESS).
			Str("subject", subjectScanCompleted).
			Msg("persistence: invalid transition or duplicate completion, ignoring")
		return nil
	}
	var result domain.ScanResult
	if err := h.decodeResult(msg.Result, &result); err != nil {
		log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: decode wallet result failed")
		return err
	}
	domain.NormalizeScanResultWalletKind(&result)
	log.Debug().Str("scan_id", msg.ScanID.String()).Msg("persistence: wallet result decoded OK")
	entity := domain.FromScanResult(msg.UserID, &result)
	entity.ID = msg.ScanID
	acquired, err := h.commitWalletCompletion(msg, entity, &result)
	if err != nil {
		log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: commit wallet completion failed")
		return err
	}
	if !acquired {
		log.Warn().
			Str("scan_id", msg.ScanID.String()).
			Str("user_id", msg.UserID.String()).
			Str("address", msg.Address).
			Msg("persistence: wallet completion rejected (plan limit exceeded), stub persisted")
		h.publishScanReady(msg.UserID, "wallet", "", msg.Address, "failed")
		return nil
	}
	log.Info().
		Str("scan_id", msg.ScanID.String()).
		Str("address", msg.Address).
		Msg("persistence: wallet Postgres write OK")
	if err := h.redisCache.SaveWalletScan(ctx, msg.UserID, msg.Address, &result); err != nil {
		log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: Redis wallet write failed")
		return err
	}
	log.Info().
		Str("scan_id", msg.ScanID.String()).
		Str("address", msg.Address).
		Str("user_id", msg.UserID.String()).
		Msg("persistence: wallet Redis write OK")
	h.publishScanReady(msg.UserID, "wallet", "", msg.Address, "success")
	h.publishWalletObserved(msg, &result)
	return nil
}

// publishWalletObserved emits cafe.discovery.wallet.observed v0.1 JSON (informational observation on the bus).
// It is not a command and must not be treated by CPM as an implicit assessment trigger — see cafe_cpm_v1_prompts_0.7.md (policy.assessment.requested is the canonical trigger).
// Best-effort; does not fail the scan write path.
func (h *ScanEventHandler) publishWalletObserved(msg *nats.ScanCompletedMessage, result *domain.ScanResult) {
	if h.natsConn == nil {
		return
	}
	meta := walletobservation.ExportMetaForScanJob(msg.ScanID)
	ev := walletobservation.ToWalletObservedEvent(meta, result, h.chainIDsByNetwork)
	if err := ev.Validate(); err != nil {
		log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: wallet.observed export validation failed")
		return
	}
	if err := nats.PublishJSON(h.natsConn, nats.SubjectDiscoveryWalletObserved, &ev); err != nil {
		log.Error().Err(err).Str("subject", nats.SubjectDiscoveryWalletObserved).Msg("persistence: wallet.observed publish failed")
		return
	}
	log.Info().
		Str("subject", nats.SubjectDiscoveryWalletObserved).
		Str("scan_id", msg.ScanID.String()).
		Str("event_id", ev.EventID).
		Msg("NATS → PUB cafe.discovery.wallet.observed v0.1")
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

func (h *ScanEventHandler) commitWalletCompletion(
	msg *nats.ScanCompletedMessage,
	entity *domain.ScanResultEntity,
	result *domain.ScanResult,
) (acquired bool, err error) {
	if msg.UserID == uuid.Nil || h.db == nil || h.ledger == nil || h.planLimits == nil {
		if err := h.walletWriter.OnCompleted(msg.ScanID, entity); err != nil {
			return false, err
		}
		return true, nil
	}

	limit, unlimited, err := h.planLimits.ScanLimit(msg.UserID, domain.ScanUsageKindWallet)
	if err != nil {
		return false, err
	}

	address := msg.Address
	if address == "" {
		address = entity.Address
	}
	if address == "" && result != nil {
		address = result.Address
	}

	err = h.db.Transaction(func(tx *gorm.DB) error {
		if unlimited {
			if err := h.ledger.RecordSuccessUsageInTx(tx, msg.UserID, msg.ScanID, domain.ScanUsageKindWallet); err != nil {
				return err
			}
			acquired = true
			return h.walletWriter.OnCompletedInTx(tx, msg.ScanID, entity)
		}
		var slotErr error
		acquired, slotErr = h.ledger.RecordSuccessUsageIfUnderLimitInTx(
			tx, msg.UserID, msg.ScanID, domain.ScanUsageKindWallet, limit,
		)
		if slotErr != nil {
			return slotErr
		}
		if acquired {
			return h.walletWriter.OnCompletedInTx(tx, msg.ScanID, entity)
		}
		return h.walletWriter.OnPlanLimitExceededInTx(tx, msg.ScanID, msg.UserID, address)
	})
	return acquired, err
}

func (h *ScanEventHandler) commitTLSCompletion(
	msg *nats.ScanCompletedMessage,
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
		if unlimited {
			if err := h.ledger.RecordSuccessUsageInTx(tx, msg.UserID, msg.ScanID, domain.ScanUsageKindEndpoint); err != nil {
				return err
			}
			acquired = true
			return h.tlsWriter.OnCompletedInTx(tx, msg.ScanID, entity)
		}
		var slotErr error
		acquired, slotErr = h.ledger.RecordSuccessUsageIfUnderLimitInTx(
			tx, msg.UserID, msg.ScanID, domain.ScanUsageKindEndpoint, limit,
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

const subjectScanFailed = "scan.failed"

func (h *ScanEventHandler) HandleFailed(msg *nats.ScanFailedMessage) error {
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
		return h.handleWalletFailed(ctx, msg)
	default:
		log.Warn().Str("kind", msg.Kind).Msg(IGNORE_SCAN_MSG)
		return nil
	}
}

func (h *ScanEventHandler) handleTLSFailed(ctx context.Context, msg *nats.ScanFailedMessage) error {
	current, err := h.tlsWriter.GetStatus(msg.ScanID)
	if err != nil {
		log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: GetStatus failed")
		return err
	}
	if current == "" {
		log.Warn().
			Str("scan_id", msg.ScanID.String()).
			Str("subject", subjectScanFailed).
			Msg("persistence: missing scan on failed (no row yet), will insert")
	} else if !scan.ValidTransition(current, scan.StateFAILED) {
		log.Warn().
			Str("scan_id", msg.ScanID.String()).
			Str("current_status", current).
			Str("attempted_status", scan.StateFAILED).
			Str("subject", subjectScanFailed).
			Msg("persistence: invalid transition, ignoring")
		return nil
	}
	userID := (*uuid.UUID)(nil)
	if msg.UserID != uuid.Nil {
		userID = &msg.UserID
	}
	if err := h.tlsWriter.OnFailed(msg.ScanID, userID, msg.Endpoint, msg.Error); err != nil {
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

func (h *ScanEventHandler) handleWalletFailed(ctx context.Context, msg *nats.ScanFailedMessage) error {
	current, err := h.walletWriter.GetStatus(msg.ScanID)
	if err != nil {
		log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: GetStatus failed")
		return err
	}
	if current == "" {
		log.Warn().
			Str("scan_id", msg.ScanID.String()).
			Str("subject", subjectScanFailed).
			Msg("persistence: missing scan on failed (no row yet), will insert")
	} else if !scan.ValidTransition(current, scan.StateFAILED) {
		log.Warn().
			Str("scan_id", msg.ScanID.String()).
			Str("current_status", current).
			Str("attempted_status", scan.StateFAILED).
			Str("subject", subjectScanFailed).
			Msg("persistence: invalid transition, ignoring")
		return nil
	}
	if err := h.walletWriter.OnFailed(msg.ScanID, msg.UserID, msg.Address, msg.Error); err != nil {
		log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: OnFailed wallet failed")
		return err
	}
	if err := h.redisCache.SaveWalletFailure(ctx, msg.UserID, msg.Address, msg.Error); err != nil {
		log.Error().Err(err).Str("scan_id", msg.ScanID.String()).Msg("persistence: Redis wallet failure write failed")
		return err
	}
	h.publishScanReady(msg.UserID, "wallet", "", msg.Address, "failed")
	return nil
}

func (h *ScanEventHandler) publishScanReady(userID uuid.UUID, kind, endpoint, address, status string) {
	if h.natsConn == nil {
		return
	}
	ready := nats.ScanReadyMessage{
		UserID: userID, Kind: kind, Endpoint: endpoint, Address: address, Status: status,
	}
	if err := nats.PublishJSON(h.natsConn, nats.SubjectScanReady, ready); err != nil {
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
