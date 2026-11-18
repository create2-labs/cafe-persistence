package scanapi

import (
	"strconv"
	"strings"

	"cafe-persistence/internal/domain"
	"cafe-persistence/pkg/scan"
)

func walletEntityIsCompleted(e *domain.ScanResultEntity) bool {
	return e != nil && strings.ToUpper(strings.TrimSpace(e.Status)) == scan.StateSUCCESS
}

// walletEntityMatchesChainID reports whether an entry of the stored chains has chain_id want,
// whatever its qualification. An unobserved chain does not match (CA2.2).
func walletEntityMatchesChainID(e *domain.ScanResultEntity, want int64) (bool, error) {
	chains, err := e.DecodeChains()
	if err != nil {
		return false, err
	}
	for _, c := range chains {
		if c.ChainID == want {
			return true, nil
		}
	}
	return false, nil
}

func paginateWalletScanEntities(in []*domain.ScanResultEntity, limit, offset int) []*domain.ScanResultEntity {
	if offset >= len(in) {
		return nil
	}
	end := offset + limit
	if end > len(in) {
		end = len(in)
	}
	return in[offset:end]
}

func parsePagination(limitStr, offsetStr string) (limit, offset int) {
	limit = 20
	offset = 0
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil {
			limit = v
		}
	}
	if offsetStr != "" {
		if v, err := strconv.Atoi(offsetStr); err == nil {
			offset = v
		}
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func normalizeAddress(addr string) string {
	return strings.ToLower(strings.TrimSpace(addr))
}
