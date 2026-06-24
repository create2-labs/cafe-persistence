package domain

// Wallet type strings match OpenAPI WalletScanResult.wallet_type enum.
const (
	WalletTypeEOA           = "eoa"
	WalletTypeSmartAccount  = "smart_account"
	WalletTypeContract      = "contract"
	WalletTypeUnknown       = "unknown"
)

// DeriveWalletTypeV1 maps scanner posture fields to the v1 wallet_type enum.
// Callers should prefer NormalizeWalletAccountKind so type, is_eoa, is_erc4337 stay aligned.
func DeriveWalletTypeV1(t AccountType, isEOA, is4337 bool) string {
	if is4337 || t == AccountTypeAA {
		return WalletTypeSmartAccount
	}
	if t == AccountTypeContract {
		return WalletTypeContract
	}
	if isEOA || t == AccountTypeEOA {
		return WalletTypeEOA
	}
	return WalletTypeUnknown
}

// NormalizeWalletAccountKind returns canonical type/is_eoa/is_erc4337/wallet_type for a completed scan.
func NormalizeWalletAccountKind(t AccountType, isEOA, is4337 bool) (AccountType, bool, bool, string) {
	switch DeriveWalletTypeV1(t, isEOA, is4337) {
	case WalletTypeSmartAccount:
		return AccountTypeAA, false, true, WalletTypeSmartAccount
	case WalletTypeContract:
		return AccountTypeContract, false, false, WalletTypeContract
	case WalletTypeEOA:
		return AccountTypeEOA, true, false, WalletTypeEOA
	default:
		return "", false, false, WalletTypeUnknown
	}
}

// NormalizeScanResultWalletKind aligns Type, IsEOA, and IsERC4337 on a scanner completion payload.
func NormalizeScanResultWalletKind(r *ScanResult) {
	if r == nil {
		return
	}
	t, isEOA, is4337, _ := NormalizeWalletAccountKind(r.Type, r.IsEOA, r.IsERC4337)
	r.Type = t
	r.IsEOA = isEOA
	r.IsERC4337 = is4337
}
