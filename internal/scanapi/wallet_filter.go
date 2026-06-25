package scanapi

import (
	"encoding/json"
	"strconv"
	"strings"

	"cafe-persistence/internal/config"
	"cafe-persistence/internal/domain"
	"cafe-persistence/pkg/scan"
)

func walletEntityIsCompleted(e *domain.ScanResultEntity) bool {
	return e != nil && strings.ToUpper(strings.TrimSpace(e.Status)) == scan.StateSUCCESS
}

func walletEntityMatchesChainID(e *domain.ScanResultEntity, want int64, cfg *config.ChainConfig) bool {
	if e == nil || cfg == nil {
		return false
	}
	nets := parseNetworksFromEntity(e.Networks)
	if len(nets) == 0 {
		return false
	}
	byName := cfg.ChainIDByNetwork()
	wantStr := strconv.FormatInt(want, 10)
	for _, n := range nets {
		if n == wantStr {
			return true
		}
		if cid, ok := byName[n]; ok && cid == want {
			return true
		}
	}
	return false
}

func parseNetworksFromEntity(networksJSON string) []string {
	if networksJSON == "" || networksJSON == "[]" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(networksJSON), &arr); err != nil {
		return nil
	}
	return arr
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
