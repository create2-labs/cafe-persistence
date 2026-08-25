package cpstore

import (
	"errors"
	"strings"
)

// NormalizeWalletTargetAddress applies the same normalization as Discovery wallet scans (0x + lowercase).
func NormalizeWalletTargetAddress(address string) (string, error) {
	a := strings.TrimSpace(address)
	if a == "" {
		return "", errors.New("target_address is required")
	}
	if strings.HasPrefix(a, "0X") {
		a = "0x" + a[2:]
	}
	if !strings.HasPrefix(a, "0x") {
		a = "0x" + a
	}
	a = strings.ToLower(a)
	if len(a) != 42 || !strings.HasPrefix(a, "0x") {
		return "", errors.New("target_address must be a normalized EVM address")
	}
	for _, c := range a[2:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", errors.New("target_address must be a normalized EVM address")
		}
	}
	return a, nil
}
