# cafe-persistence

Data-plane persistence service for the CAFE stack.

Extracted from `cafe-discovery` as part of **PERS-D1** — mechanical scan persistence extraction. Behaviour is identical to `cmd/persistence` in Discovery today; production deploy is unchanged until **PERS-D2**.

## Role

- **Single writer** for scan lifecycle events (`scan.started`, `scan.completed`, `scan.failed`)
- **Owner DDL** for `scan_results`, `tls_scan_results`, `scan_usage_events`
- Writes to **Postgres** and **Redis**; publishes `persistence.ready` on **NATS**
- Consumes NATS subjects `scan.*` (same contract as Discovery persistence)

Non-objectifs (PERS-D1) : pas de module CP, pas d'API HTTP publique, pas de migration DDL identity (`users`, `plans`).

## Build

```bash
go test ./...
go build -o persistence ./cmd/persistence/main.go
docker build -f Dockerfile-persistence -t oleglod/cafe-persistence:local .
```

## Configuration

Environment variables (or `config.yaml` via `CONFIG_PATH`):

| Variable | Default |
|----------|---------|
| `POSTGRES_HOST` | `127.0.0.1` |
| `POSTGRES_PORT` | `5432` |
| `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DATABASE` | `cafe` |
| `POSTGRES_SSLMODE` | `disable` |
| `NATS_URL` | `nats://localhost:4222` |
| `REDIS_URL` | `redis://localhost:6379` |
| `LOG_LEVEL` | `info` |
| `PERSISTENCE_HEALTH_PORT` | `8081` (HTTP `/health`, `/ready` — PERS-D2b) |
| `PERSISTENCE_INTERNAL_HTTP_PORT` | `8082` (internal scan API — PERS-D3a-impl) |
| `CAFE_PERSISTENCE_SERVICE_TOKEN` | *(unset — internal API rejects all callers until set)* |

`config.yaml` must include `blockchains[].chain_id` for wallet observation export (CPM wire contract).

## Internal scan API contract (PERS-D3a-spec / PERS-D3a-impl)

OpenAPI spec and HTTP handlers for service-to-service scan operations (pending, read/list, delete, ledger).

| Artifact | Path |
|----------|------|
| OpenAPI | `openapi/internal/scan/v1.yaml` |
| Route constants | `internal/scanroutes/routes.go` |
| Contract tests (spec) | `internal/contract/scan_v1_openapi_test.go` |
| HTTP handlers | `internal/scanapi/` |
| Handler contract tests | `internal/scanapi/handler_test.go` |

**Base path:** `/internal/scan/v1` on `PERSISTENCE_INTERNAL_HTTP_PORT` (default `8082`, distinct from health `8081`).

**Auth:** `Authorization: Bearer <CAFE_PERSISTENCE_SERVICE_TOKEN>` plus caller-propagated `X-User-Id` / optional `X-Tenant-Id` (ADR §9.1). Not exposed on public NGINX edge.

**Consumer:** `cafe-discovery` D6a-* milestones map public `/api/discovery/v1` to this contract.

## Internal CP API contract (PERS-D3b-spec)

OpenAPI spec for service-to-service crypto policy storage (drafts, persist, policies, W1/W3 references).

| Artifact | Path |
|----------|------|
| OpenAPI | `openapi/internal/cp/v1.yaml` |
| Route constants | `internal/cproutes/routes.go` |
| Contract tests (spec) | `internal/contract/cp_v1_openapi_test.go` |

**Base path:** `/internal/cp/v1` on `PERSISTENCE_INTERNAL_HTTP_PORT` (default `8082`, same listener as scan).

**Auth:** same as scan — `Authorization: Bearer <CAFE_PERSISTENCE_SERVICE_TOKEN>` plus `X-User-Id` / optional `X-Tenant-Id` (ADR §9.2).

**Semantic ownership:** CPM §8.2 (payload, statuses, persist-once). CPM review: `cafe-crypto-policy-mgt/docs/PERS_D3B_SPEC_REVIEW.md`.

**Consumers:** `cafe-crypto-policy-mgt` D5a+ (`CPM_STORE=persistence`); `cafe-discovery` D6b (existence-only refs).

**Spec only** — no HTTP handlers (PERS-D4b), no Postgres CP tables (PERS-D4). Public `/api/cpm/v1` unchanged.

## Health probes (PERS-D2b)

Internal HTTP server (not exposed on public edge):

| Endpoint | Role |
|----------|------|
| `GET /health` | Liveness — process up |
| `GET /ready` | Readiness — scan migrations applied + NATS connected + scan subscriptions active |

Compose healthcheck uses `/ready` (see `cafe-deploy/compose/20-discovery.yml`).

## DDL scan (ADR §14.5)

### Ownership

At boot, `cafe-persistence` is the sole writer of scan tables DDL in this jalon:

1. GORM `AutoMigrate` on `scan_results`, `tls_scan_results`, `scan_usage_events`
2. IMM index DDL (drop legacy uniques, create list indexes, ledger index, status default drop)

Logic lives in `internal/scanddl/migrate.go` and is invoked from `cmd/persistence/main.go`.

### Index attendus

| Index | Table | Rôle |
|-------|-------|------|
| `idx_scan_results_user_address_created_at` | `scan_results` | Historique liste (IMM-2) |
| `idx_tls_scan_results_user_url_created_at` | `tls_scan_results` | Historique liste (IMM-2) |
| `idx_scan_usage_events_user_kind` | `scan_usage_events` | Quota plan (IMM-6b-1) |

Index legacy **absents** après migration : `idx_scan_results_user_address`, `idx_tls_scan_results_user_url`.

### Vérification (CI + local)

Test d'intégration Postgres (`-tags=integration`) compare `pg_indexes` au golden file :

```bash
# Postgres requis (stack cafe-deploy, ou conteneur local)
export POSTGRES_HOST=127.0.0.1
export POSTGRES_PORT=5432
export POSTGRES_USER=cafe
export POSTGRES_PASSWORD=cafe
export POSTGRES_DATABASE=cafe
export POSTGRES_SSLMODE=disable

go test -tags=integration ./internal/scanddl/...
```

Golden file : `testdata/ddl/scan_indexes.golden`

### Régénérer le golden DDL après un changement de schéma

Quand `internal/scanddl/migrate.go` change (nouvel index, nouvelle table scan, etc.) :

1. Démarrer Postgres vide (ou reset volume dev) :

```bash
docker run -d --name cafe-pers-ddl \
  -e POSTGRES_USER=cafe -e POSTGRES_PASSWORD=cafe -e POSTGRES_DB=cafe \
  -p 5432:5432 postgres:16
```

2. Régénérer le snapshot :

```bash
export POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=5432
export POSTGRES_USER=cafe POSTGRES_PASSWORD=cafe POSTGRES_DATABASE=cafe POSTGRES_SSLMODE=disable

go run ./scripts/gen_scan_indexes_golden.go
```

3. Vérifier le diff sur `testdata/ddl/scan_indexes.golden`, puis relancer :

```bash
go test -tags=integration ./internal/scanddl/...
```

4. Committer golden + code DDL ensemble (même PR).

## Docker image

Published as `oleglod/cafe-persistence:<tag>` :

| Tag | Source |
|-----|--------|
| `sha-<short_sha>` | Chaque build RC |
| `vX.Y.Z-rc<run_id>` | Label PR `rc-vX.Y.Z` ou `workflow_dispatch` |
| `vX.Y.Z`, `latest` | Promotion release (sans rebuild) |

## Related ADR

- [ADR persistence](https://github.com/create2-labs/cafe-discovery/blob/main/docs/ADR/ADR_20260622_persistence.md) — §14.5 critère DDL
- [PR plan PERS-D1](https://github.com/create2-labs/cafe-discovery/blob/main/docs/ADR/ADR_20260622_persistence_PR_PLAN.md)

## Rollback (PERS-D1)

Cette PR n'active rien en stack. `cafe-discovery` conserve `cmd/persistence` jusqu'à **PERS-D2** validé, puis suppression en **PERS-D1b**.

Rollback opérationnel = ne pas merger D2 ; image legacy `oleglod/cafe-discovery-persistence` reste buildable depuis Discovery.
