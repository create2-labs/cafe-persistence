#!/usr/bin/env bash
# RD-P3 checklist: seed legacy draft tables → MigrateCPSchema drops them → policies recreatable.
#
# Requires: Postgres reachable (defaults cafe/cafe@127.0.0.1:5432), go, and either `psql`
# or `docker` with a running postgres container (POSTGRES_DOCKER set).
set -euo pipefail

_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
_REPO_ROOT="$(cd "${_SCRIPT_DIR}/.." && pwd)"
_SCRIPT_NAME="$(basename "${BASH_SOURCE[0]}")"

show_help() {
  cat <<EOF
Usage: ${_SCRIPT_NAME} [--help|-h]

Seed draft-era CP tables, run RD-P3 migrate via integration tests, assert drafts are gone
and CreatePolicy works.

Environment (optional)
  POSTGRES_HOST / PORT / USER / PASSWORD / DATABASE / SSLMODE
  POSTGRES_DOCKER   If set (e.g. cafe-persistence-dev-postgres-1), use docker exec psql
  TEST_POSTGRES_DSN Override DSN for go tests

Example
  ${_SCRIPT_NAME}
  POSTGRES_DOCKER=cafe-postgres-1 ${_SCRIPT_NAME}
EOF
}

case "${1:-}" in
  -h|--help) show_help; exit 0 ;;
  "") ;;
  *) echo "${_SCRIPT_NAME}: unexpected argument '$1'" >&2; exit 2 ;;
esac

export POSTGRES_HOST="${POSTGRES_HOST:-127.0.0.1}"
export POSTGRES_PORT="${POSTGRES_PORT:-5432}"
export POSTGRES_USER="${POSTGRES_USER:-cafe}"
export POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-cafe}"
export POSTGRES_DATABASE="${POSTGRES_DATABASE:-cafe}"
export POSTGRES_SSLMODE="${POSTGRES_SSLMODE:-disable}"
export PGPASSWORD="${POSTGRES_PASSWORD}"

die() { echo "error: $*" >&2; exit 1; }
ok() { echo "OK: $*"; }

run_psql() {
  if [[ -n "${POSTGRES_DOCKER:-}" ]]; then
    command -v docker >/dev/null || die "docker required when POSTGRES_DOCKER is set"
    docker exec -i -e PGPASSWORD="$POSTGRES_PASSWORD" "$POSTGRES_DOCKER" \
      psql -U "$POSTGRES_USER" -d "$POSTGRES_DATABASE" "$@"
  else
    command -v psql >/dev/null || die "psql not found; install client or set POSTGRES_DOCKER=<container>"
    psql -h "$POSTGRES_HOST" -p "$POSTGRES_PORT" -U "$POSTGRES_USER" -d "$POSTGRES_DATABASE" "$@"
  fi
}

echo "==> ${_SCRIPT_NAME}: seed legacy draft-era schema"
run_psql -v ON_ERROR_STOP=1 <<'SQL'
CREATE TABLE IF NOT EXISTS crypto_policy_drafts (
  id uuid PRIMARY KEY,
  user_id text NOT NULL,
  tenant_id text,
  payload jsonb NOT NULL DEFAULT '{}',
  status text NOT NULL DEFAULT 'server_draft',
  created_at timestamptz,
  updated_at timestamptz,
  deleted_at timestamptz
);
CREATE TABLE IF NOT EXISTS draft_persist_state (
  draft_id uuid PRIMARY KEY,
  policy_id uuid NOT NULL,
  completed boolean NOT NULL DEFAULT false,
  user_id text NOT NULL,
  tenant_id text
);
-- Force legacy crypto_policies shape (draft_id, no payload_sha256) so migrate RAZ path runs.
DROP TABLE IF EXISTS crypto_policies CASCADE;
CREATE TABLE crypto_policies (
  id uuid PRIMARY KEY,
  user_id text NOT NULL,
  tenant_id text,
  scan_id uuid,
  draft_id uuid NOT NULL,
  wallet_address text NOT NULL,
  chain_id bigint NOT NULL,
  payload jsonb NOT NULL DEFAULT '{}',
  status text NOT NULL,
  persisted_at timestamptz NOT NULL,
  created_at timestamptz,
  updated_at timestamptz,
  deleted_at timestamptz
);
INSERT INTO crypto_policy_drafts (id, user_id, payload, status)
VALUES ('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'user-seed', '{"mode":"legacy"}', 'server_draft')
ON CONFLICT (id) DO NOTHING;
SQL

cnt="$(run_psql -Atc "SELECT count(*) FROM pg_tables WHERE schemaname='public' AND tablename IN ('crypto_policy_drafts','draft_persist_state');")"
[[ "$cnt" == "2" ]] || die "expected 2 legacy tables before migrate, got ${cnt}"
ok "legacy tables present (${cnt})"

echo "==> migrate + assert drop (integration)"
cd "$_REPO_ROOT"
go test -tags=integration ./internal/cpstore/ \
  -run 'TestPostgresStore_migrateDropsDraftTables|TestPostgresStore_createPolicyLifecycle' \
  -count=1 -v

cnt_after="$(run_psql -Atc "SELECT count(*) FROM pg_tables WHERE schemaname='public' AND tablename IN ('crypto_policy_drafts','draft_persist_state');")"
[[ "$cnt_after" == "0" ]] || die "legacy tables still present after migrate (${cnt_after})"
ok "legacy draft tables dropped"

has_sha="$(run_psql -Atc "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='crypto_policies' AND column_name='payload_sha256';")"
[[ "$has_sha" == "1" ]] || die "crypto_policies.payload_sha256 missing after migrate"
has_draft_col="$(run_psql -Atc "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='crypto_policies' AND column_name='draft_id';")"
[[ "$has_draft_col" == "0" ]] || die "crypto_policies.draft_id still present"
ok "policy schema is RD-P3 (payload_sha256, no draft_id)"

echo "==> ${_SCRIPT_NAME}: PASS"
