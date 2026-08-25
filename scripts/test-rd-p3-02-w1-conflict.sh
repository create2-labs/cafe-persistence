#!/usr/bin/env bash
# RD-P3 checklist: W1 unique — double create → 409; DELETE then recreate → 201.
#
# Default: runs Go unit tests (sqlite httptest; no Postgres required).
# Optional live HTTP: set CP_BASE + CAFE_PERSISTENCE_SERVICE_TOKEN.
set -euo pipefail

_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
_REPO_ROOT="$(cd "${_SCRIPT_DIR}/.." && pwd)"
_SCRIPT_NAME="$(basename "${BASH_SOURCE[0]}")"

show_help() {
  cat <<EOF
Usage: ${_SCRIPT_NAME} [--help|-h] [--live]

Prove W1: second active policy for same owner+wallet → 409 POLICY_ALREADY_EXISTS;
soft-DELETE then create again → 201.

Modes
  (default)  go test ./internal/cpapi/ (sqlite) + optional integration if Postgres up
  --live     also curl against a running persistence (requires CP_BASE + token)

Environment (optional)
  CP_BASE                         e.g. http://127.0.0.1:8082/internal/cp/v1
  CAFE_PERSISTENCE_SERVICE_TOKEN  service bearer
  X_USER_ID                       owner UUID (default: fixed test UUID)
  SKIP_INTEGRATION=1              do not attempt -tags=integration even if Postgres listens
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

echo "==> ${_SCRIPT_NAME}: unit tests (sqlite HTTP)"
cd "$_REPO_ROOT"
go test ./internal/cpapi/ \
  -run 'TestCpAPI_CreateDuplicateReturns409|TestCpAPI_DeletePolicy' \
  -count=1 -v
ok "unit W1 + DELETE/recreate"

if [[ "${SKIP_INTEGRATION:-0}" != "1" ]] && (echo >/dev/tcp/"${POSTGRES_HOST:-127.0.0.1}"/"${POSTGRES_PORT:-5432}") 2>/dev/null; then
  echo "==> Postgres reachable — integration store tests"
  export POSTGRES_HOST="${POSTGRES_HOST:-127.0.0.1}"
  export POSTGRES_PORT="${POSTGRES_PORT:-5432}"
  export POSTGRES_USER="${POSTGRES_USER:-cafe}"
  export POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-cafe}"
  export POSTGRES_DATABASE="${POSTGRES_DATABASE:-cafe}"
  export POSTGRES_SSLMODE="${POSTGRES_SSLMODE:-disable}"
  go test -tags=integration ./internal/cpstore/ \
    -run 'TestPostgresStore_createPolicyW1UniqueViolation|TestPostgresStore_deleteThenRecreateAllowsW1' \
    -count=1 -v
  ok "integration W1 + DELETE/recreate"
else
  echo "==> skip integration (Postgres not on ${POSTGRES_HOST:-127.0.0.1}:${POSTGRES_PORT:-5432} or SKIP_INTEGRATION=1)"
fi

if [[ "$LIVE" -eq 1 ]]; then
  command -v curl >/dev/null || die "curl required for --live"
  command -v jq >/dev/null || die "jq required for --live"
  CP_BASE="${CP_BASE:?CP_BASE required for --live (e.g. http://127.0.0.1:8082/internal/cp/v1)}"
  TOKEN="${CAFE_PERSISTENCE_SERVICE_TOKEN:?CAFE_PERSISTENCE_SERVICE_TOKEN required for --live}"
  USER_ID="${X_USER_ID:-11111111-1111-4111-8111-111111111111}"
  WALLET="${WALLET_ADDRESS:-0x742d35cc6634c0532925a3b844bc454e4438f44e}"
  CP_BASE="${CP_BASE%/}"

  uuid() {
    if command -v uuidgen >/dev/null; then uuidgen | tr '[:upper:]' '[:lower:]'
    else python3 -c 'import uuid; print(uuid.uuid4())'
    fi
  }

  auth=(-H "Authorization: Bearer ${TOKEN}" -H "X-User-Id: ${USER_ID}" -H "Content-Type: application/json")
  body() {
    local scan="$1" sha="$2"
    cat <<JSON
{"scan_id":"${scan}","wallet_address":"${WALLET}","chain_id":1,"payload":{"mode":"strict"},"payload_sha256":"${sha}","signed_message_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","wallet_control_method":"eoa_signature","wallet_control_verified_at":"2026-06-10T12:00:00Z"}
JSON
  }

  echo "==> live HTTP create #1 → 201"
  SCAN1="$(uuid)"
  code="$(curl -sS -o /tmp/rd-p3-cp1.json -w '%{http_code}' -X POST "${CP_BASE}/policies" "${auth[@]}" -d "$(body "$SCAN1" "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")")"
  [[ "$code" == "201" ]] || die "create #1 expected 201, got ${code}: $(cat /tmp/rd-p3-cp1.json)"
  POLICY_ID="$(jq -r .policy_id /tmp/rd-p3-cp1.json)"
  [[ -n "$POLICY_ID" && "$POLICY_ID" != null ]] || die "missing policy_id"
  ok "created ${POLICY_ID}"

  echo "==> live HTTP create #2 same wallet → 409"
  SCAN2="$(uuid)"
  code="$(curl -sS -o /tmp/rd-p3-cp2.json -w '%{http_code}' -X POST "${CP_BASE}/policies" "${auth[@]}" -d "$(body "$SCAN2" "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")")"
  [[ "$code" == "409" ]] || die "create #2 expected 409, got ${code}: $(cat /tmp/rd-p3-cp2.json)"
  err="$(jq -r .error /tmp/rd-p3-cp2.json)"
  [[ "$err" == "POLICY_ALREADY_EXISTS" ]] || die "expected POLICY_ALREADY_EXISTS, got ${err}"
  ok "409 POLICY_ALREADY_EXISTS"

  echo "==> live HTTP DELETE → 204"
  code="$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE "${CP_BASE}/policies/${POLICY_ID}" \
    -H "Authorization: Bearer ${TOKEN}" -H "X-User-Id: ${USER_ID}")"
  [[ "$code" == "204" ]] || die "DELETE expected 204, got ${code}"
  ok "deleted"

  echo "==> live HTTP recreate → 201"
  SCAN3="$(uuid)"
  code="$(curl -sS -o /tmp/rd-p3-cp3.json -w '%{http_code}' -X POST "${CP_BASE}/policies" "${auth[@]}" -d "$(body "$SCAN3" "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")")"
  [[ "$code" == "201" ]] || die "recreate expected 201, got ${code}: $(cat /tmp/rd-p3-cp3.json)"
  ok "recreated $(jq -r .policy_id /tmp/rd-p3-cp3.json)"
fi

echo "==> ${_SCRIPT_NAME}: PASS"
