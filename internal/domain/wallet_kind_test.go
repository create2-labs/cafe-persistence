package domain

import "testing"

func TestDeriveWalletTypeV1(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		typ     AccountType
		isEOA   bool
		is4337  bool
		want    string
	}{
		{name: "eoa from type", typ: AccountTypeEOA, isEOA: false, want: WalletTypeEOA},
		{name: "eoa from flag", typ: "", isEOA: true, want: WalletTypeEOA},
		{name: "smart account from erc4337", typ: AccountTypeContract, is4337: true, want: WalletTypeSmartAccount},
		{name: "smart account from aa type", typ: AccountTypeAA, want: WalletTypeSmartAccount},
		{name: "contract", typ: AccountTypeContract, want: WalletTypeContract},
		{name: "unknown empty", want: WalletTypeUnknown},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DeriveWalletTypeV1(tc.typ, tc.isEOA, tc.is4337); got != tc.want {
				t.Fatalf("DeriveWalletTypeV1(%q, %v, %v) = %q, want %q", tc.typ, tc.isEOA, tc.is4337, got, tc.want)
			}
		})
	}
}

func TestNormalizeWalletAccountKind_typeEOAWithFalseIsEOA(t *testing.T) {
	t.Parallel()

	typ, isEOA, is4337, walletType := NormalizeWalletAccountKind(AccountTypeEOA, false, false)
	if walletType != WalletTypeEOA {
		t.Fatalf("wallet_type = %q, want %q", walletType, WalletTypeEOA)
	}
	if typ != AccountTypeEOA {
		t.Fatalf("type = %q, want %q", typ, AccountTypeEOA)
	}
	if !isEOA {
		t.Fatal("is_eoa must be true when wallet_type is eoa")
	}
	if is4337 {
		t.Fatal("is_erc4337 must be false for eoa")
	}
}

func TestNormalizeScanResultWalletKind(t *testing.T) {
	t.Parallel()

	result := &ScanResult{
		Type:      AccountTypeEOA,
		IsEOA:     false,
		IsERC4337: false,
	}
	NormalizeScanResultWalletKind(result)
	if result.Type != AccountTypeEOA || !result.IsEOA || result.IsERC4337 {
		t.Fatalf("normalized result = %+v", result)
	}
}
