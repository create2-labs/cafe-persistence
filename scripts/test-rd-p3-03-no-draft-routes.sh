#!/usr/bin/env bash
# RD-P3 checklist: old draft clients fail (accepted) — no /drafts* on internal CP API.
#
# Default: contract + route unit tests (no server required).
# Optional live HTTP: set CP_BASE + CAFE_PERSISTENCE_SERVICE_TOKEN (--live).
set -euo pipefail

_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
_REPO_ROOT="$(cd "${_SCRIPT_DIR}/.." && pwd)"
_SCRIPT_NAME="$(basename "${BASH_SOURCE[0]}")"

show_help() {
  cat <<EOF
Usage: ${_SCRIPT_NAME} [--help|-h] [--live]

Assert draft routes/schemas are gone from OpenAPI + cproutes, and (optional) that a live
persistence returns 404 for PUT/POST /drafts*.

Environment (optional, --live)
  CP_BASE                         e.g. http://127.0.0.1:8082/internal/cp/v1
  CAFE_PERSISTENCE_SERVICE_TOKEN  service bearer
  X_USER_ID                       owner UUID
EOF
}

LIVE=0
case "${1:-}" in
  -h|--help) show_help; exit 0 ;;
  --live) LIVE=1 ;;
  "") ;;
  *) echo "${_SCRIPT_NAME}: unexpected argument '$1'" >&2; exit 2 ;;
esac

die() { echo "error: $*" >&2; exit 1; }
ok() { echo "OK: $*"; }

command -v go >/dev/null || die "go is required"
cd "$_REPO_ROOT"

echo "==> ${_SCRIPT_NAME}: OpenAPI + routes — no drafts"
go test ./internal/contract/ -run 'TestCpV1OpenAPI_RequiredPathsDocumented|TestCpV1OpenAPI_RequiredSchemasDocumented' -count=1 -v
go test ./internal/cproutes/ -run 'TestNoDraftRoutes' -count=1 -v
ok "contract/routes have no draft surface"

if [[ "$LIVE" -eq 1 ]]; then
  command -v curl >/dev/null || die "curl required for --live"
  CP_BASE="${CP_BASE:?CP_BASE required for --live}"
  TOKEN="${CAFE_PERSISTENCE_SERVICE_TOKEN:?CAFE_PERSISTENCE_SERVICE_TOKEN required for --live}"
  USER_ID="${X_USER_ID:-11111111-1111-4111-8111-111111111111}"
  CP_BASE="${CP_BASE%/}"
  DRAFT_ID="${DRAFT_ID:-aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa}"
  SCAN_ID="${SCAN_ID:-550e8400-e29b-41d4-a716-446655440000}"
  WALLET="${WALLET_ADDRESS:-0x742d35cc6634c0532925a3b844bc454e4438f44e}"

  echo "==> live PUT /drafts/{id} → expect 404"
  code="$(curl -sS -o /tmp/rd-p3-draft-put.json -w '%{http_code}' -X PUT "${CP_BASE}/drafts/${DRAFT_ID}" \
    -H "Authorization: Bearer ${TOKEN}" -H "X-User-Id: ${USER_ID}" \
    -H "Content-Type: application/json" -d '{"payload":{}}')"
  [[ "$code" == "404" ]] || die "PUT draft expected 404, got ${code}: $(cat /tmp/rd-p3-draft-put.json 2>/dev/null || true)"
  ok "PUT /drafts → 404"

  echo "==> live POST /drafts/{id}/persist → expect 404"
  code="$(curl -sS -o /tmp/rd-p3-draft-persist.json -w '%{http_code}' -X POST "${CP_BASE}/drafts/${DRAFT_ID}/persist" \
    -H "Authorization: Bearer ${TOKEN}" -H "X-User-Id: ${USER_ID}" \
    -H "Content-Type: application/json" \
    -d "{\"wallet_address\":\"${WALLET}\",\"chain_id\":1,\"scan_id\":\"${SCAN_ID}\",\"wallet_control_verified_at\":\"2026-06-10T12:00:00Z\"}")"
  [[ "$code" == "404" ]] || die "POST persist draft expected 404, got ${code}: $(cat /tmp/rd-p3-draft-persist.json 2>/dev/null || true)"
  ok "POST /drafts/.../persist → 404 (old CPM draft clients fail — accepted)"
fi

echo "==> ${_SCRIPT_NAME}: PASS"
