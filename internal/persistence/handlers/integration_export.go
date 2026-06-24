package handlers

import (
	"cafe-persistence/internal/domain"
	"cafe-persistence/pkg/nats"
)

// CommitWalletCompletionForIntegrationTest exposes commitWalletCompletion for IMM-6b-8 cross-package tests.
func (h *ScanEventHandler) CommitWalletCompletionForIntegrationTest(
	msg *nats.ScanCompletedMessage,
	entity *domain.ScanResultEntity,
	result *domain.ScanResult,
) (bool, error) {
	return h.commitWalletCompletion(msg, entity, result)
}
