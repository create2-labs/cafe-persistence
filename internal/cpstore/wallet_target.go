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

func walletTargetFromPayload(payload map[string]any) string {
	if addr := extractWalletTargetAddress(payload); addr != "" {
		norm, err := NormalizeWalletTargetAddress(addr)
		if err == nil {
			return norm
		}
	}
	return ""
}

func extractWalletTargetAddress(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if pc, ok := payload["policy_context"].(map[string]any); ok {
		if addr := walletTargetFromPolicyContext(pc); addr != "" {
			return addr
		}
	}
	if swc, ok := payload["selected_wallet_policy_context"].(map[string]any); ok {
		if addr := walletTargetFromScanContext(swc); addr != "" {
			return addr
		}
	}
	if draft, ok := payload["draft"].(map[string]any); ok {
		if addr := extractWalletTargetAddress(draft); addr != "" {
			return addr
		}
	}
	return firstNonEmpty(
		stringField(payload, "target_address"),
		stringField(payload, "wallet_address"),
		stringField(payload, "walletAddress"),
	)
}

func walletTargetFromPolicyContext(pc map[string]any) string {
	if addr := firstNonEmpty(
		stringField(pc, "target_address"),
		stringField(pc, "wallet_address"),
		stringField(pc, "walletAddress"),
	); addr != "" {
		return addr
	}
	if res, ok := pc["result"].(map[string]any); ok {
		return stringField(res, "target_address")
	}
	return ""
}

func walletTargetFromScanContext(ctx map[string]any) string {
	if addr := firstNonEmpty(
		stringField(ctx, "target_address"),
		stringField(ctx, "wallet_address"),
	); addr != "" {
		return addr
	}
	if res, ok := ctx["result"].(map[string]any); ok {
		return stringField(res, "target_address")
	}
	return ""
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return strings.TrimSpace(s)
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
