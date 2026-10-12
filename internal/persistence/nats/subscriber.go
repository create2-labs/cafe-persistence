package nats

import (
	natsio "github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"

	"cafe-persistence/internal/persistence/handlers"
	"cafe-persistence/pkg/nats"
	lifecycle "github.com/create2-labs/cafe-contracts/discovery/scanlifecycle/v1"
)

// SubscribeScanEvents subscribes to scan.started, scan.completed, scan.failed and delegates to h.
func SubscribeScanEvents(conn nats.Connection, h *handlers.ScanEventHandler) ([]*natsio.Subscription, error) {
	var subs []*natsio.Subscription

	started, err := conn.QueueSubscribe(lifecycle.SubjectScanStarted, nats.QueuePersistence, func(msg *natsio.Msg) {
		m, err := lifecycle.DecodeStarted(msg.Data)
		if err != nil {
			log.Warn().Err(err).Msg("persistence: invalid scan.started payload")
			return
		}
		log.Info().
			Str("subject", lifecycle.SubjectScanStarted).
			Str("scan_id", m.ScanID).
			Str("kind", m.Kind).
			Str("component", "persistence").
			Msg("NATS ← RECV scan.started")
		if err := h.HandleStarted(&m); err != nil {
			log.Error().Err(err).Msg("persistence: HandleStarted failed")
		}
	})
	if err != nil {
		return nil, err
	}
	subs = append(subs, started)

	completed, err := conn.QueueSubscribe(lifecycle.SubjectScanCompleted, nats.QueuePersistence, func(msg *natsio.Msg) {
		m, err := lifecycle.DecodeCompleted(msg.Data)
		if err != nil {
			log.Warn().Err(err).Msg("persistence: invalid scan.completed payload")
			return
		}
		log.Info().
			Str("subject", lifecycle.SubjectScanCompleted).
			Str("scan_id", m.ScanID).
			Str("kind", m.Kind).
			Str("component", "persistence").
			Msg("NATS ← RECV scan.completed")
		if err := h.HandleCompleted(&m); err != nil {
			log.Error().Err(err).Msg("persistence: HandleCompleted failed")
		}
	})
	if err != nil {
		return nil, err
	}
	subs = append(subs, completed)

	failed, err := conn.QueueSubscribe(lifecycle.SubjectScanFailed, nats.QueuePersistence, func(msg *natsio.Msg) {
		m, err := lifecycle.DecodeFailed(msg.Data)
		if err != nil {
			log.Warn().Err(err).Msg("persistence: invalid scan.failed payload")
			return
		}
		log.Info().
			Str("subject", lifecycle.SubjectScanFailed).
			Str("scan_id", m.ScanID).
			Str("kind", m.Kind).
			Str("component", "persistence").
			Msg("NATS ← RECV scan.failed")
		if err := h.HandleFailed(&m); err != nil {
			log.Error().Err(err).Msg("persistence: HandleFailed failed")
		}
	})
	if err != nil {
		return nil, err
	}
	subs = append(subs, failed)

	log.Info().
		Str("subjects", lifecycle.SubjectScanStarted+","+lifecycle.SubjectScanCompleted+","+lifecycle.SubjectScanFailed).
		Str("queue", nats.QueuePersistence).
		Msg("persistence: subscribed to scan events")
	return subs, nil
}
