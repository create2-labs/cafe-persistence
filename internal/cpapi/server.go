// Package cpapi implements the internal CP HTTP API (PERS-D4b).
package cpapi

import (
	"net/http"

	"cafe-persistence/internal/cproutes"
)

// RegisterRoutes mounts CP handlers on mux (openapi/internal/cp/v1.yaml).
func RegisterRoutes(mux *http.ServeMux, h *Handler) {
	base := cproutes.V1Base

	register := func(method, rel string, fn http.HandlerFunc) {
		pattern := method + " " + base + rel
		mux.HandleFunc(pattern, fn)
	}

	register("PUT", cproutes.DraftByID, h.UpsertDraft)
	register("GET", cproutes.DraftByID, h.GetDraft)
	register("DELETE", cproutes.DraftByID, h.DeleteDraft)
	register("POST", cproutes.DraftPersist, h.PersistDraft)
	register("GET", cproutes.PolicyByID, h.GetPolicy)
	register("DELETE", cproutes.PolicyByID, h.DeletePolicy)
	register("GET", cproutes.Policies, h.ListPoliciesByScan)
	register("GET", cproutes.ReferenceWallet, h.CountPoliciesByWallet)
	register("GET", cproutes.ReferenceScan, h.CountPoliciesByScan)
}
