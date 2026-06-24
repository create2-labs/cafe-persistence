package nats

import (
	"encoding/json"

	natsio "github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"

	"cafe-persistence/internal/persistence/handlers"
	"cafe-persistence/pkg/nats"
)

// SubscribeScanEvents subscribes to scan.started, scan.completed, scan.failed and delegates to h.
func SubscribeScanEvents(conn nats.Connection, h *handlers.ScanEventHandler) ([]*natsio.Subscription, error) {
	var subs []*natsio.Subscription

	started, err := conn.QueueSubscribe(nats.SubjectScanStarted, nats.QueuePersistence, func(msg *natsio.Msg) {
		var m nats.ScanStartedMessage
		if err := json.Unmarshal(msg.Data, &m); err != nil {
			log.Warn().Err(err).Msg("persistence: invalid scan.started payload")
			return
		}
		log.Info().
			Str("subject", nats.SubjectScanStarted).
			Str("scan_id", m.ScanID.String()).
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

	completed, err := conn.QueueSubscribe(nats.SubjectScanCompleted, nats.QueuePersistence, func(msg *natsio.Msg) {
		var m nats.ScanCompletedMessage
		if err := json.Unmarshal(msg.Data, &m); err != nil {
			log.Warn().Err(err).Msg("persistence: invalid scan.completed payload")
			return
		}
		log.Info().
			Str("subject", nats.SubjectScanCompleted).
			Str("scan_id", m.ScanID.String()).
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

	failed, err := conn.QueueSubscribe(nats.SubjectScanFailed, nats.QueuePersistence, func(msg *natsio.Msg) {
		var m nats.ScanFailedMessage
		if err := json.Unmarshal(msg.Data, &m); err != nil {
			log.Warn().Err(err).Msg("persistence: invalid scan.failed payload")
			return
		}
		log.Info().
			Str("subject", nats.SubjectScanFailed).
			Str("scan_id", m.ScanID.String()).
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
		Str("subjects", nats.SubjectScanStarted+","+nats.SubjectScanCompleted+","+nats.SubjectScanFailed).
		Str("queue", nats.QueuePersistence).
		Msg("persistence: subscribed to scan events")
	return subs, nil
}
