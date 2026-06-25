package cpstore

import "errors"

var (
	ErrOwnerRequired         = errors.New("owner scope is required")
	ErrDraftNotFound         = errors.New("draft not found")
	ErrDraftAlreadyPersisted = errors.New("draft already persisted")
	ErrPolicyNotFound        = errors.New("policy not found")
	ErrForbidden             = errors.New("forbidden")
	ErrInvalidWalletAddress  = errors.New("wallet address is invalid")
	ErrScanIDRequired        = errors.New("scan_id is required")
	ErrScanIDMismatch        = errors.New("scan_id does not match draft")
)
