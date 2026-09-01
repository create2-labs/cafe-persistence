#!/usr/bin/env bash
# cafe-persistence: golangci-lint, govulncheck, Dockerfile, Docker Scout, rapport.
# Phase 0 baseline — aligné sur cafe-crypto-policy-mgt (canonique à venir Phase 1).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_ID="${RUN_ID:-$(date +%Y%m%d-%H%M%S)}"
IMAGE_TAG="${IMAGE_TAG:-cafe-audit-$RUN_ID}"
IMAGE_PREFIX="${IMAGE_PREFIX:-oleglod}"
REPORT_DIR="${REPORT_DIR:-$REPO_ROOT/reports}"
REPORT_FILE="${REPORT_FILE:-$REPORT_DIR/cafe-persistence-security-audit-$RUN_ID.md}"
SKIP_SCOUT="${SKIP_SCOUT:-0}"

info()  { printf '%s\n' "→ $*"; }
warn()  { printf '%s\n' "⚠ $*" >&2; }
have() { command -v "$1" >/dev/null 2>&1; }

STATE_FILE=""
state_init() { STATE_FILE=$(mktemp); : >"$STATE_FILE"; }
state_set() { local k="$1" v="${2:-}"; grep -v "^${k}|" "$STATE_FILE" 2>/dev/null >"${STATE_FILE}.n" || true; echo "${k}|${v}" >>"${STATE_FILE}.n"; mv "${STATE_FILE}.n" "$STATE_FILE"; }
state_get() { grep "^${1}|" "$STATE_FILE" 2>/dev/null | head -1 | cut -d'|' -f2- || true; }

run_lint() {
  if ! have golangci-lint; then
    if (cd "$REPO_ROOT" && go vet ./... 2>&1); then
      state_set lint "go vet OK"
    else
      state_set lint "échec go vet"
    fi
  else
    if (cd "$REPO_ROOT" && golangci-lint run ./... 2>&1); then
      state_set lint "golangci-lint OK"
    else
      state_set lint "échec golangci-lint"
    fi
  fi
}
run_gov() {
  if ! have govulncheck; then state_set gov "absent"; return; fi
  local out
  if out=$(cd "$REPO_ROOT" && GOWORK=off govulncheck ./... 2>&1); then
    if echo "$out" | grep -q "Vulnerability #"; then state_set gov "alertes"; else state_set gov "OK"; fi
  else state_set gov "échec"; fi
}

main() {
  mkdir -p "$REPORT_DIR"
  state_init
  run_lint || true
  run_gov || true

  local im="${IMAGE_PREFIX}/cafe-persistence:${IMAGE_TAG}" ok=0 sc="KO"
  if ( cd "$REPO_ROOT" && docker build -f Dockerfile -t "$im" . ); then ok=1; else warn "docker build échoué"; fi
  if [ "$ok" = 1 ] && [ "$SKIP_SCOUT" != 1 ] && docker scout version >/dev/null 2>&1; then
    sc=$(docker scout quickview "local://$im" 2>&1 | tr -d '\033' | grep -E 'Target[[:space:]]+│' | head -1 | \
      sed -E 's/.*[[:space:]]([0-9]+)C[[:space:]]+([0-9]+)H[[:space:]]+([0-9]+)M[[:space:]]+([0-9]+)L.*/C=\1 H=\2 M=\3 L=\4/') || sc="?"
  else
    [ "$SKIP_SCOUT" = 1 ] && sc=SKIP
  fi
  {
    echo "# cafe-persistence — audit"
    echo "- Généré: $(date -u '+%Y-%m-%d %H:%M UTC')"
    echo "## Statique: linter: $(state_get lint | tr '|' /) — govulncheck: $(state_get gov | tr '|' /)"
    echo ""
    echo "## Image \`$im\` (build: $([ "$ok" = 1 ] && echo OK || echo KO))"
    echo "Scout: $sc"
    echo ""
    if [ "$ok" = 1 ] && [ "$SKIP_SCOUT" != 1 ] && docker scout version >/dev/null 2>&1; then
      docker scout cves "local://$im" --format markdown 2>&1 || true
    fi
  } > "$REPORT_FILE"
  info "Rapport: $REPORT_FILE"
}

main
