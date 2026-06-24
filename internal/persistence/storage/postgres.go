package storage

import (
	"errors"
	"time"

	"cafe-persistence/internal/domain"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TLSWriter persists TLS scan state/results (one Postgres row per scan_id).
type TLSWriter struct {
	db *gorm.DB
}

func NewTLSWriter(db *gorm.DB) *TLSWriter {
	return &TLSWriter{db: db}
}

// GetStatus returns the current status for the scan, or "" if not found.
func (w *TLSWriter) GetStatus(scanID uuid.UUID) (string, error) {
	var ent domain.TLSScanResultEntity
	err := w.db.Select("status").Where("id = ?", scanID).First(&ent).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return ent.Status, nil
}

// OnStarted inserts a lifecycle-only row for scan_id (internal status RUNNING; API maps to started).
// Crypto posture fields are filled only on scan.completed (IMM-D1).
// Idempotent for the same scan_id: no downgrade from terminal; duplicate start is a no-op.
func (w *TLSWriter) OnStarted(scanID uuid.UUID, userID *uuid.UUID, url string) error {
	current, err := w.GetStatus(scanID)
	if err != nil {
		return err
	}
	if current != "" {
		return nil
	}
	ent := &domain.TLSScanResultEntity{
		ID: scanID, UserID: userID, URL: url, Status: scan.StateRUNNING,
	}
	return w.db.Create(ent).Error
}

// OnCompleted updates the row by scan_id; inserts on replay when the row is missing.
func (w *TLSWriter) OnCompleted(scanID uuid.UUID, entity *domain.TLSScanResultEntity) error {
	return w.OnCompletedInTx(w.db, scanID, entity)
}

// OnCompletedInTx is the transactional variant of OnCompleted.
func (w *TLSWriter) OnCompletedInTx(tx *gorm.DB, scanID uuid.UUID, entity *domain.TLSScanResultEntity) error {
	entity.ID = scanID
	entity.Status = scan.StateSUCCESS
	entity.Error = ""
	res := tx.Model(entity).Where("id = ?", scanID).Omit("created_at").Select("*").Updates(entity)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return tx.Create(entity).Error
	}
	return nil
}

// OnPlanLimitExceededInTx writes a stripped failed row when quota is exceeded at completion (IMM-6b G3, IMM-D3).
// Only lifecycle identity (user_id, url), status, and error are set — no fabricated crypto posture.
func (w *TLSWriter) OnPlanLimitExceededInTx(tx *gorm.DB, scanID uuid.UUID, userID *uuid.UUID, url string) error {
	updates := map[string]interface{}{
		"status":           scan.StateFAILED,
		"error":            scan.ErrPlanLimitExceeded,
		"host":             "",
		"port":             0,
		"protocol_version": "",
		"nist_level":       0,
		"risk_score":       0,
		"pqc_risk":         "",
		"certificate":      "",
		"cipher_suites":    "",
		"supported_pq_cs":  "",
		"recommendations":  "",
		"nist_levels":      "",
		"updated_at":       time.Now(),
	}
	res := tx.Model(&domain.TLSScanResultEntity{}).Where("id = ?", scanID).Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		ent := &domain.TLSScanResultEntity{
			ID: scanID, UserID: userID, URL: url,
			Status: scan.StateFAILED, Error: scan.ErrPlanLimitExceeded,
		}
		return tx.Create(ent).Error
	}
	return nil
}

// OnFailed updates the row by scan_id; inserts on replay when the row is missing.
func (w *TLSWriter) OnFailed(scanID uuid.UUID, userID *uuid.UUID, url, errMsg string) error {
	res := w.db.Model(&domain.TLSScanResultEntity{}).Where("id = ?", scanID).
		Updates(map[string]interface{}{
			"status":     scan.StateFAILED,
			"error":      errMsg,
			"updated_at": time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		ent := &domain.TLSScanResultEntity{
			ID: scanID, UserID: userID, URL: url, Host: "", Port: 0,
			ProtocolVersion: "unknown", NISTLevel: domain.NISTLevel1,
			RiskScore: 0, PQCRisk: "unknown", Status: scan.StateFAILED, Error: errMsg,
		}
		return w.db.Create(ent).Error
	}
	return nil
}

// WalletWriter persists wallet scan state/results (one Postgres row per scan_id).
type WalletWriter struct {
	db *gorm.DB
}

func NewWalletWriter(db *gorm.DB) *WalletWriter {
	return &WalletWriter{db: db}
}

// GetStatus returns the current status for the scan, or "" if not found.
func (w *WalletWriter) GetStatus(scanID uuid.UUID) (string, error) {
	var ent domain.ScanResultEntity
	err := w.db.Select("status").Where("id = ?", scanID).First(&ent).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return ent.Status, nil
}

// OnStarted inserts a lifecycle-only row for scan_id (internal status RUNNING; API maps to started).
// Crypto posture fields are filled only on scan.completed (IMM-D1).
// A new scan_id for the same address always inserts a new row (no target-level upsert).
func (w *WalletWriter) OnStarted(scanID, userID uuid.UUID, address string) error {
	current, err := w.GetStatus(scanID)
	if err != nil {
		return err
	}
	if current != "" {
		return nil
	}
	ent := &domain.ScanResultEntity{
		ID: scanID, UserID: userID, Address: address, Status: scan.StateRUNNING,
	}
	return w.db.Create(ent).Error
}

// OnCompleted updates the row by scan_id; inserts on replay when the row is missing.
func (w *WalletWriter) OnCompleted(scanID uuid.UUID, entity *domain.ScanResultEntity) error {
	return w.OnCompletedInTx(w.db, scanID, entity)
}

// OnCompletedInTx is the transactional variant of OnCompleted.
func (w *WalletWriter) OnCompletedInTx(tx *gorm.DB, scanID uuid.UUID, entity *domain.ScanResultEntity) error {
	entity.ID = scanID
	entity.Status = scan.StateSUCCESS
	entity.Error = ""
	res := tx.Model(entity).Where("id = ?", scanID).Omit("created_at").Select("*").Updates(entity)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return tx.Create(entity).Error
	}
	return nil
}

// OnPlanLimitExceededInTx writes a stripped failed row when quota is exceeded at completion (IMM-6b G3, IMM-D3).
// Only lifecycle identity (user_id, address), status, and error are set — no fabricated crypto posture.
func (w *WalletWriter) OnPlanLimitExceededInTx(tx *gorm.DB, scanID, userID uuid.UUID, address string) error {
	updates := map[string]interface{}{
		"status":           scan.StateFAILED,
		"error":            scan.ErrPlanLimitExceeded,
		"type":             "",
		"algorithm":        "",
		"nist_level":       0,
		"key_exposed":      false,
		"public_key":       "",
		"transaction_hash": "",
		"exposed_network":  "",
		"is_eoa":           false,
		"is_erc4337":       false,
		"risk_score":       0,
		"networks":         "",
		"connections":      "",
		"updated_at":       time.Now(),
	}
	res := tx.Model(&domain.ScanResultEntity{}).Where("id = ?", scanID).Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		ent := &domain.ScanResultEntity{
			ID: scanID, UserID: userID, Address: address,
			Status: scan.StateFAILED, Error: scan.ErrPlanLimitExceeded,
		}
		return tx.Create(ent).Error
	}
	return nil
}

// OnFailed updates the row by scan_id; inserts on replay when the row is missing.
func (w *WalletWriter) OnFailed(scanID, userID uuid.UUID, address, errMsg string) error {
	res := w.db.Model(&domain.ScanResultEntity{}).Where("id = ?", scanID).
		Updates(map[string]interface{}{
			"status":     scan.StateFAILED,
			"error":      errMsg,
			"updated_at": time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		ent := &domain.ScanResultEntity{
			ID: scanID, UserID: userID, Address: address,
			Status: scan.StateFAILED, Error: errMsg,
		}
		return w.db.Create(ent).Error
	}
	return nil
}
