// Package cproutes defines canonical path constants for the internal CP API (PERS-D3b-spec).
// Handlers, contract tests, and CPM HTTP clients import these literals.
package cproutes

// V1Base is the internal CP API prefix on the persistence HTTP listener (not public edge).
const V1Base = "/internal/cp/v1"

// OpenAPI-relative paths (see openapi/internal/cp/v1.yaml paths block; server url = V1Base).
const (
	DraftByID       = "/drafts/{draft_id}"
	DraftPersist    = "/drafts/{draft_id}/persist"
	PolicyByID      = "/policies/{policy_id}"
	Policies        = "/policies"
	ReferenceWallet = "/references/wallet"
	ReferenceScan   = "/references/scan"
)

// Join returns the absolute mux path for a V1-relative route.
func Join(relative string) string {
	return V1Base + relative
}
