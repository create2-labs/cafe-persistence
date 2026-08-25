package cpstore

import "errors"

var (
	ErrOwnerRequired          = errors.New("owner scope is required")
	ErrPolicyNotFound         = errors.New("policy not found")
	ErrPolicyAlreadyExists    = errors.New("active policy already exists for wallet")
	ErrForbidden              = errors.New("forbidden")
	ErrInvalidWalletAddress   = errors.New("wallet address is invalid")
	ErrScanIDRequired         = errors.New("scan_id is required")
	ErrPayloadSHA256Required = errors.New("payload_sha256 is required")
	ErrPayloadRequired       = errors.New("payload is required")
	ErrInvalidChainID        = errors.New("chain_id must be >= 1")
)
