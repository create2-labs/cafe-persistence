package cpstore

import (
	"errors"
	"fmt"
	"time"

	"cafe-persistence/internal/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DraftRecord is the store projection of crypto_policy_drafts.
type DraftRecord struct {
	ID        uuid.UUID
	UserID    string
	TenantID  string
	ScanID    *uuid.UUID
	Payload   map[string]any
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PolicyRecord is the store projection of crypto_policies.
type PolicyRecord struct {
	ID                      uuid.UUID
	UserID                  string
	TenantID                string
	ScanID                  uuid.UUID
	DraftID                 uuid.UUID
	WalletAddress           string
	ChainID                 int64
	Payload                 map[string]any
	OwnershipStatus         string
	WalletControlMethod     string
	WalletControlVerifiedAt *time.Time
	Status                  string
	PersistedAt             time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// PersistDraftInput carries wallet ownership metadata applied on persist (CPM §8.2).
type PersistDraftInput struct {
	WalletAddress string
	ChainID       int64
	ScanID        uuid.UUID
	VerifiedAt    time.Time
}

// PersistDraftResult is the durable outcome of a successful draft persist transition.
type PersistDraftResult struct {
	PolicyID      uuid.UUID
	DraftID       uuid.UUID
	ScanID        uuid.UUID
	WalletAddress string
	ChainID       int64
	PersistedAt   time.Time
}

// WalletReferenceCount is the W1 existence projection.
type WalletReferenceCount struct {
	Exists           bool
	PolicyCount      int64
	DraftCount       int64
	PlatformDraftID  string // set when DraftCount == 1 (CPM wallet-target-context UI)
}

// ScanReferenceCount is the W3 existence projection.
type ScanReferenceCount struct {
	Referenced bool
	Count      int64
}

// ListPoliciesResult is a paginated list of persisted policies for a scan.
type ListPoliciesResult struct {
	Items  []PolicyRecord
	Total  int64
	Limit  int
	Offset int
}

// PostgresStore is the owner-scoped CP Postgres writer (PERS-D4).
type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) SaveDraft(scope OwnerScope, draftID uuid.UUID, scanID *uuid.UUID, payload map[string]any) (DraftRecord, error) {
	if err := scope.validate(); err != nil {
		return DraftRecord{}, ErrOwnerRequired
	}
	now := time.Now().UTC()
	payloadCopy := domain.CloneJSONMap(payload)

	var out DraftRecord
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var existing domain.CryptoPolicyDraftEntity
		err := tx.Where("id = ?", draftID).First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			if !sameOwner(existing.UserID, existing.TenantID, scope.UserID, scope.TenantID) {
				return ErrForbidden
			}
		}
		createdAt := now
		if err == nil {
			createdAt = existing.CreatedAt
		}
		ent := domain.CryptoPolicyDraftEntity{
			ID:        draftID,
			UserID:    scope.UserID,
			TenantID:  scope.TenantID,
			ScanID:    scanID,
			Payload:   domain.JSONMap(payloadCopy),
			Status:    domain.CryptoPolicyDraftStatusServerDraft,
			CreatedAt: createdAt,
			UpdatedAt: now,
		}
		if err := tx.Save(&ent).Error; err != nil {
			return err
		}
		out = draftFromEntity(ent)
		return nil
	})
	return out, err
}

func (s *PostgresStore) GetDraft(scope OwnerScope, draftID uuid.UUID) (DraftRecord, error) {
	if err := scope.validate(); err != nil {
		return DraftRecord{}, ErrOwnerRequired
	}
	var ent domain.CryptoPolicyDraftEntity
	err := s.db.Where("id = ? AND user_id = ? AND tenant_id = ?", draftID, scope.UserID, scope.TenantID).
		First(&ent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DraftRecord{}, ErrDraftNotFound
	}
	if err != nil {
		return DraftRecord{}, err
	}
	return draftFromEntity(ent), nil
}

func (s *PostgresStore) DeleteDraft(scope OwnerScope, draftID uuid.UUID) error {
	if err := scope.validate(); err != nil {
		return ErrOwnerRequired
	}
	res := s.db.Where("id = ? AND user_id = ? AND tenant_id = ?", draftID, scope.UserID, scope.TenantID).
		Delete(&domain.CryptoPolicyDraftEntity{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var other domain.CryptoPolicyDraftEntity
		err := s.db.Unscoped().Where("id = ?", draftID).First(&other).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrDraftNotFound
		}
		if err != nil {
			return err
		}
		if !sameOwner(other.UserID, other.TenantID, scope.UserID, scope.TenantID) {
			return ErrForbidden
		}
		return ErrDraftNotFound
	}
	return nil
}

func (s *PostgresStore) PersistDraftOnce(scope OwnerScope, draftID uuid.UUID, in PersistDraftInput) (PersistDraftResult, error) {
	if err := scope.validate(); err != nil {
		return PersistDraftResult{}, ErrOwnerRequired
	}
	verifiedAt := in.VerifiedAt.UTC()
	if verifiedAt.IsZero() {
		verifiedAt = time.Now().UTC()
	}
	normWallet, err := NormalizeWalletTargetAddress(in.WalletAddress)
	if err != nil {
		return PersistDraftResult{}, fmt.Errorf("%w: %v", ErrInvalidWalletAddress, err)
	}
	if in.ScanID == uuid.Nil {
		return PersistDraftResult{}, ErrScanIDRequired
	}

	var result PersistDraftResult
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var state domain.DraftPersistStateEntity
		stateErr := tx.Where("draft_id = ?", draftID).First(&state).Error
		if stateErr != nil && !errors.Is(stateErr, gorm.ErrRecordNotFound) {
			return stateErr
		}
		if stateErr == nil && state.Completed {
			if !sameOwner(state.UserID, state.TenantID, scope.UserID, scope.TenantID) {
				return ErrDraftNotFound
			}
			return ErrDraftAlreadyPersisted
		}

		var draft domain.CryptoPolicyDraftEntity
		if err := tx.Where("id = ? AND user_id = ? AND tenant_id = ?", draftID, scope.UserID, scope.TenantID).
			First(&draft).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDraftNotFound
			}
			return err
		}

		draftScanID := uuid.Nil
		if draft.ScanID != nil {
			draftScanID = *draft.ScanID
		}
		if draftScanID != uuid.Nil && draftScanID != in.ScanID {
			return ErrScanIDMismatch
		}
		scanID := in.ScanID
		if draftScanID != uuid.Nil {
			scanID = draftScanID
		}

		policyID := uuid.Nil
		if stateErr == nil {
			policyID = state.PolicyID
		}
		if policyID == uuid.Nil {
			policyID = uuid.New()
		}

		if err := tx.Save(&domain.DraftPersistStateEntity{
			DraftID:   draftID,
			PolicyID:  policyID,
			Completed: false,
			UserID:    scope.UserID,
			TenantID:  scope.TenantID,
		}).Error; err != nil {
			return err
		}

		if err := supersedeOtherPoliciesForScan(tx, scope, scanID, policyID); err != nil {
			return err
		}

		payload := domain.PolicyPayloadFromDraft(
			map[string]any(draft.Payload),
			draftID.String(),
			scanID.String(),
			normWallet,
			in.ChainID,
			verifiedAt,
		)
		policy := domain.CryptoPolicyEntity{
			ID:                      policyID,
			UserID:                  scope.UserID,
			TenantID:                scope.TenantID,
			ScanID:                  scanID,
			DraftID:                 draftID,
			WalletAddress:           normWallet,
			ChainID:                 in.ChainID,
			Payload:                 domain.JSONMap(payload),
			OwnershipStatus:         "verified",
			WalletControlMethod:     "eoa_signature",
			WalletControlVerifiedAt: &verifiedAt,
			Status:                  domain.CryptoPolicyStatusPersisted,
			PersistedAt:             verifiedAt,
			UpdatedAt:               verifiedAt,
		}
		var existing domain.CryptoPolicyEntity
		findErr := tx.Where("id = ?", policyID).First(&existing).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		if findErr == nil {
			policy.CreatedAt = existing.CreatedAt
			if err := tx.Model(&existing).Updates(map[string]any{
				"user_id":                    policy.UserID,
				"tenant_id":                  policy.TenantID,
				"scan_id":                    policy.ScanID,
				"draft_id":                   policy.DraftID,
				"wallet_address":             policy.WalletAddress,
				"chain_id":                   policy.ChainID,
				"payload":                    policy.Payload,
				"ownership_status":           policy.OwnershipStatus,
				"wallet_control_method":      policy.WalletControlMethod,
				"wallet_control_verified_at": policy.WalletControlVerifiedAt,
				"status":                     policy.Status,
				"persisted_at":               policy.PersistedAt,
				"updated_at":                 policy.UpdatedAt,
				"deleted_at":                 nil,
			}).Error; err != nil {
				return err
			}
		} else {
			policy.CreatedAt = verifiedAt
			if err := tx.Create(&policy).Error; err != nil {
				return err
			}
		}

		if err := tx.Model(&domain.DraftPersistStateEntity{}).
			Where("draft_id = ?", draftID).
			Updates(map[string]any{
				"completed":    true,
				"persisted_at": verifiedAt,
				"policy_id":    policyID,
			}).Error; err != nil {
			return err
		}

		if err := tx.Unscoped().Delete(&domain.CryptoPolicyDraftEntity{}, "id = ?", draftID).Error; err != nil {
			return err
		}

		result = PersistDraftResult{
			PolicyID:      policyID,
			DraftID:       draftID,
			ScanID:        scanID,
			WalletAddress: normWallet,
			ChainID:       in.ChainID,
			PersistedAt:   verifiedAt,
		}
		return nil
	})
	return result, err
}

func supersedeOtherPoliciesForScan(tx *gorm.DB, scope OwnerScope, scanID, keepPolicyID uuid.UUID) error {
	if scanID == uuid.Nil {
		return nil
	}
	now := time.Now().UTC()
	return tx.Model(&domain.CryptoPolicyEntity{}).
		Where(
			"user_id = ? AND tenant_id = ? AND scan_id = ? AND id <> ? AND status = ? AND deleted_at IS NULL",
			scope.UserID, scope.TenantID, scanID, keepPolicyID, domain.CryptoPolicyStatusPersisted,
		).
		Updates(map[string]any{
			"status":     domain.CryptoPolicyStatusSuperseded,
			"updated_at": now,
		}).Error
}

func (s *PostgresStore) GetPolicy(scope OwnerScope, policyID uuid.UUID) (PolicyRecord, error) {
	if err := scope.validate(); err != nil {
		return PolicyRecord{}, ErrOwnerRequired
	}
	var ent domain.CryptoPolicyEntity
	err := s.db.Where(
		"id = ? AND user_id = ? AND tenant_id = ? AND status = ?",
		policyID, scope.UserID, scope.TenantID, domain.CryptoPolicyStatusPersisted,
	).First(&ent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return PolicyRecord{}, ErrPolicyNotFound
	}
	if err != nil {
		return PolicyRecord{}, err
	}
	return policyFromEntity(ent), nil
}

func (s *PostgresStore) DeletePolicy(scope OwnerScope, policyID uuid.UUID) error {
	if err := scope.validate(); err != nil {
		return ErrOwnerRequired
	}
	res := s.db.Where(
		"id = ? AND user_id = ? AND tenant_id = ? AND status = ?",
		policyID, scope.UserID, scope.TenantID, domain.CryptoPolicyStatusPersisted,
	).Delete(&domain.CryptoPolicyEntity{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var other domain.CryptoPolicyEntity
		err := s.db.Unscoped().Where("id = ?", policyID).First(&other).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPolicyNotFound
		}
		if err != nil {
			return err
		}
		if !sameOwner(other.UserID, other.TenantID, scope.UserID, scope.TenantID) {
			return ErrForbidden
		}
		return ErrPolicyNotFound
	}
	return nil
}

func (s *PostgresStore) ListPersistedPoliciesForScan(scope OwnerScope, scanID uuid.UUID, limit, offset int) (ListPoliciesResult, error) {
	if err := scope.validate(); err != nil {
		return ListPoliciesResult{}, ErrOwnerRequired
	}
	if scanID == uuid.Nil {
		return ListPoliciesResult{}, ErrScanIDRequired
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	base := s.db.Model(&domain.CryptoPolicyEntity{}).
		Where(
			"user_id = ? AND tenant_id = ? AND scan_id = ? AND status = ?",
			scope.UserID, scope.TenantID, scanID, domain.CryptoPolicyStatusPersisted,
		)

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return ListPoliciesResult{}, err
	}

	var rows []domain.CryptoPolicyEntity
	if err := base.Order("persisted_at DESC, id ASC").
		Limit(limit).Offset(offset).
		Find(&rows).Error; err != nil {
		return ListPoliciesResult{}, err
	}

	items := make([]PolicyRecord, len(rows))
	for i, row := range rows {
		items[i] = policyFromEntity(row)
	}
	return ListPoliciesResult{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func (s *PostgresStore) CountPoliciesByWallet(scope OwnerScope, walletAddress string) (WalletReferenceCount, error) {
	if err := scope.validate(); err != nil {
		return WalletReferenceCount{}, ErrOwnerRequired
	}
	needle, err := NormalizeWalletTargetAddress(walletAddress)
	if err != nil {
		return WalletReferenceCount{}, fmt.Errorf("%w: %v", ErrInvalidWalletAddress, err)
	}

	var policyCount int64
	if err := s.db.Model(&domain.CryptoPolicyEntity{}).
		Where(
			"user_id = ? AND tenant_id = ? AND wallet_address = ? AND status = ?",
			scope.UserID, scope.TenantID, needle, domain.CryptoPolicyStatusPersisted,
		).Count(&policyCount).Error; err != nil {
		return WalletReferenceCount{}, err
	}

	var drafts []domain.CryptoPolicyDraftEntity
	if err := s.db.Where("user_id = ? AND tenant_id = ?", scope.UserID, scope.TenantID).
		Find(&drafts).Error; err != nil {
		return WalletReferenceCount{}, err
	}
	var draftCount int64
	var platformDraftID string
	for _, d := range drafts {
		if walletTargetFromPayload(map[string]any(d.Payload)) == needle {
			draftCount++
			if draftCount == 1 {
				platformDraftID = d.ID.String()
			} else {
				platformDraftID = ""
			}
		}
	}

	total := policyCount + draftCount
	return WalletReferenceCount{
		Exists:          total > 0,
		PolicyCount:     policyCount,
		DraftCount:      draftCount,
		PlatformDraftID: platformDraftID,
	}, nil
}

func (s *PostgresStore) CountPoliciesByScan(scope OwnerScope, scanID uuid.UUID) (ScanReferenceCount, error) {
	if err := scope.validate(); err != nil {
		return ScanReferenceCount{}, ErrOwnerRequired
	}
	if scanID == uuid.Nil {
		return ScanReferenceCount{}, ErrScanIDRequired
	}
	var count int64
	if err := s.db.Model(&domain.CryptoPolicyEntity{}).
		Where(
			"user_id = ? AND tenant_id = ? AND scan_id = ? AND status = ?",
			scope.UserID, scope.TenantID, scanID, domain.CryptoPolicyStatusPersisted,
		).Count(&count).Error; err != nil {
		return ScanReferenceCount{}, err
	}
	return ScanReferenceCount{
		Referenced: count > 0,
		Count:      count,
	}, nil
}

func draftFromEntity(ent domain.CryptoPolicyDraftEntity) DraftRecord {
	rec := DraftRecord{
		ID:        ent.ID,
		UserID:    ent.UserID,
		TenantID:  ent.TenantID,
		ScanID:    ent.ScanID,
		Payload:   domain.CloneJSONMap(ent.Payload),
		Status:    ent.Status,
		CreatedAt: ent.CreatedAt,
		UpdatedAt: ent.UpdatedAt,
	}
	return rec
}

func policyFromEntity(ent domain.CryptoPolicyEntity) PolicyRecord {
	return PolicyRecord{
		ID:                      ent.ID,
		UserID:                    ent.UserID,
		TenantID:                  ent.TenantID,
		ScanID:                    ent.ScanID,
		DraftID:                   ent.DraftID,
		WalletAddress:             ent.WalletAddress,
		ChainID:                   ent.ChainID,
		Payload:                   domain.CloneJSONMap(ent.Payload),
		OwnershipStatus:           ent.OwnershipStatus,
		WalletControlMethod:       ent.WalletControlMethod,
		WalletControlVerifiedAt: ent.WalletControlVerifiedAt,
		Status:                    ent.Status,
		PersistedAt:               ent.PersistedAt,
		CreatedAt:                 ent.CreatedAt,
		UpdatedAt:                 ent.UpdatedAt,
	}
}