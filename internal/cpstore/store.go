package cpstore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"cafe-persistence/internal/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PolicyRecord is the store projection of crypto_policies.
type PolicyRecord struct {
	ID                      uuid.UUID
	UserID                  string
	TenantID                string
	ScanID                  uuid.UUID
	WalletAddress           string
	ChainID                 int64
	Payload                 map[string]any
	PayloadSHA256           string
	SignedMessageHash       string
	OwnershipStatus         string
	WalletControlMethod     string
	WalletControlVerifiedAt *time.Time
	ChallengeIssuedAt       *time.Time
	ChallengeExpiresAt      *time.Time
	Status                  string
	PersistedAt             time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// CreatePolicyInput carries the pre-verified policy create request from CPM (ADR_20260824).
// Wallet auth / EIP-191 stay in CPM; persistence stores audit hashes only (no raw signatures).
type CreatePolicyInput struct {
	ScanID                  uuid.UUID
	WalletAddress           string
	ChainID                 int64
	Payload                 map[string]any
	PayloadSHA256           string // server authority from CPM; client values must not be trusted upstream
	SignedMessageHash       string
	WalletControlMethod     string
	WalletControlVerifiedAt time.Time
	ChallengeIssuedAt       *time.Time
	ChallengeExpiresAt      *time.Time
}

// CreatePolicyResult is the durable outcome of a successful policy create.
type CreatePolicyResult struct {
	PolicyID        uuid.UUID
	ScanID          uuid.UUID
	WalletAddress   string
	ChainID         int64
	PayloadSHA256   string
	PersistedAt     time.Time
}

// WalletReferenceCount is the W1 existence projection (policies only after RD-P3).
type WalletReferenceCount struct {
	Exists      bool
	PolicyCount int64
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

// PostgresStore is the owner-scoped CP Postgres writer (PERS-D4 / RD-P3 policy-only).
type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) CreatePolicy(scope OwnerScope, in CreatePolicyInput) (CreatePolicyResult, error) {
	if err := scope.validate(); err != nil {
		return CreatePolicyResult{}, ErrOwnerRequired
	}
	if in.ScanID == uuid.Nil {
		return CreatePolicyResult{}, ErrScanIDRequired
	}
	sha := strings.TrimSpace(in.PayloadSHA256)
	if sha == "" {
		return CreatePolicyResult{}, ErrPayloadSHA256Required
	}
	if in.Payload == nil {
		return CreatePolicyResult{}, ErrPayloadRequired
	}
	if in.ChainID < 1 {
		return CreatePolicyResult{}, ErrInvalidChainID
	}
	verifiedAt := in.WalletControlVerifiedAt.UTC()
	if verifiedAt.IsZero() {
		verifiedAt = time.Now().UTC()
	}
	normWallet, err := NormalizeWalletTargetAddress(in.WalletAddress)
	if err != nil {
		return CreatePolicyResult{}, fmt.Errorf("%w: %v", ErrInvalidWalletAddress, err)
	}
	method := strings.TrimSpace(in.WalletControlMethod)
	if method == "" {
		method = "eoa_signature"
	}

	payload := domain.NormalizePolicyPayload(
		in.Payload,
		in.ScanID.String(),
		normWallet,
		in.ChainID,
		verifiedAt,
	)
	policyID := uuid.New()
	policy := domain.CryptoPolicyEntity{
		ID:                      policyID,
		UserID:                  scope.UserID,
		TenantID:                scope.TenantID,
		ScanID:                  in.ScanID,
		WalletAddress:           normWallet,
		ChainID:                 in.ChainID,
		Payload:                 domain.JSONMap(payload),
		PayloadSHA256:           sha,
		SignedMessageHash:       strings.TrimSpace(in.SignedMessageHash),
		OwnershipStatus:         "verified",
		WalletControlMethod:     method,
		WalletControlVerifiedAt: &verifiedAt,
		ChallengeIssuedAt:       cloneTimeUTC(in.ChallengeIssuedAt),
		ChallengeExpiresAt:      cloneTimeUTC(in.ChallengeExpiresAt),
		Status:                  domain.CryptoPolicyStatusPersisted,
		PersistedAt:             verifiedAt,
		CreatedAt:               verifiedAt,
		UpdatedAt:               verifiedAt,
	}

	if err := s.db.Create(&policy).Error; err != nil {
		if isUniqueViolation(err) {
			return CreatePolicyResult{}, ErrPolicyAlreadyExists
		}
		return CreatePolicyResult{}, err
	}
	return CreatePolicyResult{
		PolicyID:      policyID,
		ScanID:        in.ScanID,
		WalletAddress: normWallet,
		ChainID:       in.ChainID,
		PayloadSHA256: sha,
		PersistedAt:   verifiedAt,
	}, nil
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

	return WalletReferenceCount{
		Exists:      policyCount > 0,
		PolicyCount: policyCount,
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

func policyFromEntity(ent domain.CryptoPolicyEntity) PolicyRecord {
	return PolicyRecord{
		ID:                      ent.ID,
		UserID:                  ent.UserID,
		TenantID:                ent.TenantID,
		ScanID:                  ent.ScanID,
		WalletAddress:           ent.WalletAddress,
		ChainID:                 ent.ChainID,
		Payload:                 domain.CloneJSONMap(ent.Payload),
		PayloadSHA256:           ent.PayloadSHA256,
		SignedMessageHash:       ent.SignedMessageHash,
		OwnershipStatus:         ent.OwnershipStatus,
		WalletControlMethod:     ent.WalletControlMethod,
		WalletControlVerifiedAt: ent.WalletControlVerifiedAt,
		ChallengeIssuedAt:       ent.ChallengeIssuedAt,
		ChallengeExpiresAt:      ent.ChallengeExpiresAt,
		Status:                  ent.Status,
		PersistedAt:             ent.PersistedAt,
		CreatedAt:               ent.CreatedAt,
		UpdatedAt:               ent.UpdatedAt,
	}
}

func cloneTimeUTC(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "23505")
}
