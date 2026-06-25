package cpstore

import "cafe-persistence/internal/domain"

// OwnerScope identifies the end-user owner for CP rows (propagated from caller JWT).
type OwnerScope struct {
	UserID   string
	TenantID string
}

func (o OwnerScope) validate() error {
	return domain.ValidateOwnerScope(o.UserID, o.TenantID)
}

func sameOwner(recordUserID, recordTenantID, userID, tenantID string) bool {
	return recordUserID == userID && recordTenantID == tenantID
}
