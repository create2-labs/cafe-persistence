package domain

// Wallet type strings match OpenAPI WalletScanResult.wallet_type enum.
const (
	WalletTypeEOA          = "eoa"
	WalletTypeSmartAccount = "smart_account"
	WalletTypeContract     = "contract"
	WalletTypeUnknown      = "unknown"
)

// DeriveWalletTypeV1 maps scanner posture fields to the v1 wallet_type enum.
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
