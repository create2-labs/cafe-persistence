#!/usr/bin/env bash
# Run all RD-P3 manual checklist scripts (01 → 02 → 03).
set -euo pipefail

_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

usage() {
  cat <<EOF
Usage: $(basename "$0") [--help|-h] [--live] [--skip-legacy]

Runs:
  test-rd-p3-01-legacy-drop.sh     (needs Postgres)
  test-rd-p3-02-w1-conflict.sh     [--live]
  test-rd-p3-03-no-draft-routes.sh [--live]

Flags
  --live         forward to 02/03 (needs CP_BASE + CAFE_PERSISTENCE_SERVICE_TOKEN)
  --skip-legacy  skip 01 if Postgres is unavailable
EOF
}

LIVE_ARGS=()
SKIP_LEGACY=0
for arg in "$@"; do
  case "$arg" in
    -h|--help) usage; exit 0 ;;
    --live) LIVE_ARGS=(--live) ;;
    --skip-legacy) SKIP_LEGACY=1 ;;
    *) echo "unexpected argument: $arg" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ "$SKIP_LEGACY" -eq 1 ]]; then
  echo "==> skip 01 (--skip-legacy)"
elif (echo >/dev/tcp/"${POSTGRES_HOST:-127.0.0.1}"/"${POSTGRES_PORT:-5432}") 2>/dev/null; then
  "${_SCRIPT_DIR}/test-rd-p3-01-legacy-drop.sh"
else
  echo "error: Postgres not reachable; start it or pass --skip-legacy" >&2
  exit 1
fi

"${_SCRIPT_DIR}/test-rd-p3-02-w1-conflict.sh" "${LIVE_ARGS[@]+"${LIVE_ARGS[@]}"}"
"${_SCRIPT_DIR}/test-rd-p3-03-no-draft-routes.sh" "${LIVE_ARGS[@]+"${LIVE_ARGS[@]}"}"

echo "==> test-rd-p3-all: PASS"
