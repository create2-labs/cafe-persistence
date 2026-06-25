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

### Analyse statique

```bash
golangci-lint run ./...
```

`deadcode` sans options ne suit que le binaire **production** (`main`) : le module CP (`internal/cpstore`) et les routes internes apparaissent « morts » tant qu’ils ne sont reliés qu’aux tests (`-test`) ou aux tests Postgres (`-tags=integration`).

```bash
# Couverture réaliste : tests unitaires + intégration CP
deadcode -test -tags=integration ./...
```

Attendu aujourd’hui : **0 unreachable** après PERS-D4. Les handlers HTTP CP (PERS-D4b) brancheront `cpstore` depuis `main`.

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

**Spec only** — no HTTP handlers (PERS-D4b). Public `/api/cpm/v1` unchanged.

## CP Postgres storage (PERS-D4)

Owner-scoped crypto policy tables and writers (no HTTP — D4b wires handlers).  
Voir [Schéma Postgres : rôle des migrations et des golden files](#schéma-postgres--rôle-des-migrations-et-des-golden-files) pour le pourquoi des migrations malgré l’absence de prod.

| Artifact | Path |
|----------|------|
| Domain entities | `internal/domain/crypto_policy.go` |
| DDL migrations | `internal/cpddl/migrate.go` |
| Postgres store | `internal/cpstore/` |
| DDL golden | `testdata/ddl/cp_indexes.golden` |

**Tables:** `crypto_policy_drafts`, `crypto_policies`, `draft_persist_state` (ADR §8.4).

Applied at boot from `cmd/persistence/main.go` after scan migrations.

### Pourquoi trois tables ?

Un modèle mono-table avec `status IN ('draft', 'persisted', 'superseded')` peut sembler suffisant. Le schéma CP en utilise trois parce qu’il reflète la sémantique CPM §8.2 (`OwnerScopedStore`) et les invariants du contrat `internal/cp/v1` (D3b-spec) : **brouillon modifiable**, **policy immuable**, **persist idempotent**.

#### Vue d’ensemble

| Table | Nature | Rôle |
|-------|--------|------|
| `crypto_policy_drafts` | Métier | Travail en cours (`server_draft`) — payload libre, upsert/delete, **pas** une CP officielle |
| `crypto_policies` | Métier | CP durable après wallet-auth — colonnes d’audit, immuable après persist, `superseded` au remplacement |
| `draft_persist_state` | Technique | Idempotence de `PersistDraftOnce` — lie `draft_id` → `policy_id` même après suppression du draft |

```
  PUT draft                    POST persist (idempotent)
       │                              │
       ▼                              ▼
crypto_policy_drafts ─────────► crypto_policies
       │                              ▲
       │    draft_persist_state       │ (policy_id réservé / completed)
       └──────────────────────────────┘
```

#### 1. `crypto_policy_drafts` — brouillon plateforme

- Statut unique : `server_draft`.
- Payload JSON modifiable (`UpsertDraft`).
- Soft delete utilisateur (`deleted_at`).
- **Supprimé** (hard delete) quand le persist réussit — comme le store mémoire CPM.
- Compté séparément pour le guard **W1** (`draft_count` dans `/references/wallet`).
- **Exclu** du guard **W3** et de `ListPoliciesByScan` (seules les policies comptent).

L’adresse wallet est souvent **dans le JSON** (`policy_context`, etc.) et extraite à la volée ; pas besoin des colonnes d’audit indexées d’une policy persistée.

#### 2. `crypto_policies` — CP officielle durable

- Statuts : `persisted` (active) ou `superseded` (remplacée par un nouveau persist sur le même `scan_id`).
- Colonnes dédiées + index **W1** : `wallet_address`, `chain_id`, `ownership_status`, `wallet_control_method`, `wallet_control_verified_at`, `persisted_at`.
- **Immutabilité** (ADR §8.4.2) : après le premier `persisted`, le `payload` et les champs d’audit ne sont plus mis à jour ; un remplacement crée une **nouvelle** ligne et marque l’ancienne `superseded`.
- Jamais de `signed_message` / `signature` en base (wallet-auth = CPM public API uniquement).
- Comptée pour **W3** (`/references/scan`) et listée par `scan_id` (hors drafts).

Séparer drafts et policies évite de mélanger lignes mutables et lignes immuables, et permet des index partiels ciblés :

```sql
-- ex. W1 sans scan JSON
(user_id, wallet_address) WHERE status = 'persisted' AND deleted_at IS NULL
```

#### 3. `draft_persist_state` — idempotence persist-once

Table technique calquée sur `draftPersisted map[string]draftPersistState` dans `OwnerScopedStore` (CPM).

| Colonne | Rôle |
|---------|------|
| `draft_id` (PK) | Clé d’idempotence client (+ scope owner) |
| `policy_id` | ID alloué au **premier** essai (réutilisé si retry avant completion) |
| `completed` | `true` seulement après transaction persist réussie |
| `persisted_at` | Horodatage du succès |
| `user_id`, `tenant_id` | Scope owner |

**Pourquoi une table à part ?** Après un persist réussi, le draft est **supprimé**. Sans `draft_persist_state`, un replay `POST /drafts/{draft_id}/persist` ne pourrait plus répondre **`409 DRAFT_ALREADY_PERSISTED`** de façon fiable.

Sémantique (ADR §5.5, D3b-spec) :

1. **Premier succès** : réserve `policy_id`, écrit `crypto_policies`, `completed = true`, supprime le draft.
2. **Replay après succès** → `409` (même si le draft n’existe plus).
3. **Échec avant completion** : retry avec le **même** `draft_id` réutilise le **même** `policy_id` (pas de double policy).

L’ADR §8.4.3 mentionne l’alternative « colonnes sur `crypto_policy_drafts` », mais elle ne tient pas si le draft est retiré au succès — d’où la table dédiée.

#### Ce qu’un seul `status` ne couvrirait pas proprement

| Besoin | Mono-table `draft \| persisted \| superseded` | Trois tables actuelles |
|--------|---------------------------------------------|-------------------------|
| Draft supprimé après persist + replay 409 | Nécessite de garder le draft ou un hack | `draft_persist_state` survit au draft |
| Immutabilité policy vs mutabilité draft | Risque d’update accidentel sur une CP officielle | Séparation stricte |
| W1 : `policy_count` + `draft_count` | Requêtes et index plus ambigus | Comptages distincts, index W1 sur policies |
| W3 : policies seulement | Filtrage `status` partout | `crypto_policies` seule |
| Retry mid-flight (même `policy_id`) | État intermédiaire difficile à modéliser | `completed = false` + `policy_id` réservé |

**Référence métier :** `cafe-crypto-policy-mgt/docs/PERS_D3B_SPEC_REVIEW.md`, ADR persistence §8.4 et §5.5.

### CP DDL verification

```bash
export POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=5432
export POSTGRES_USER=cafe POSTGRES_PASSWORD=cafe POSTGRES_DATABASE=cafe POSTGRES_SSLMODE=disable

go test -tags=integration ./internal/cpddl/...
go test -tags=integration ./internal/cpstore/...
```

Regenerate index golden after DDL changes:

```bash
go run ./scripts/gen_cp_indexes_golden.go
```

## Health probes (PERS-D2b)

Internal HTTP server (not exposed on public edge):

| Endpoint | Role |
|----------|------|
| `GET /health` | Liveness — process up |
| `GET /ready` | Readiness — scan migrations applied + NATS connected + scan subscriptions active |

Compose healthcheck uses `/ready` (see `cafe-deploy/compose/20-discovery.yml`).

## Schéma Postgres : rôle des migrations et des golden files

> **Contexte actuel :** rien n’est en prod côté persistence CP/scan ; en dev on peut **RAZ la DB** quand on veut (`docker volume rm`, `DROP SCHEMA public CASCADE`, etc.). Les migrations ici ne servent **pas** à préserver des données existantes.

### Ce que “migrer” veut dire dans ce repo

Au boot, `cafe-persistence` applique le schéma dont le code a besoin :

1. GORM `AutoMigrate` (tables + colonnes de base)
2. DDL SQL complémentaire (index partiels IMM/W1/W3, drops legacy, etc.)

C’est invoqué depuis `cmd/persistence/main.go` (`scanddl.MigrateScanSchema`, `cpddl.MigrateCPSchema`).  
**Migrer = créer ou aligner le schéma attendu**, pas “upgrader une prod vieille de N versions”.

### Pourquoi c’est nécessaire même sans prod

| Besoin | Sans migration au boot |
|--------|-------------------------|
| **Ownership ADR** — seul `cafe-persistence` crée les tables scan et CP ; Discovery et CPM n’ont plus (ou n’auront plus) de DDL local | Schéma créé à la main, scripts ops ad hoc, ou divergence entre services |
| **Fresh install reproductible** — CI, machine locale, collègue, staging : Postgres vide à chaque run | Erreurs runtime (“relation does not exist”) ou schémas différents selon l’environnement |
| **Contrat code ↔ base** — `PostgresStore`, writers scan, index W1/W3 supposent colonnes et index précis | Code et DB désalignés ; bugs silencieux (requêtes lentes, guards faux) |
| **Jalons suivants** — D4b (HTTP CP), D5a (client CPM), D6b (refs Discovery) consomment un stockage déjà figé | DDL et API inventés en même temps, dette de coordination |

La liberté de RAZ enlève la contrainte **“ne pas casser les données”**. Elle n’enlève pas le besoin d’un **schéma défini, owned par persistence, appliqué automatiquement**.

### Golden files (`testdata/ddl/*.golden`)

Un golden DDL est un **snapshot versionné** de ce que `pg_indexes` doit retourner après migration (noms d’index sur les tables scan ou CP).

Les tests `-tags=integration` (`internal/scanddl/`, `internal/cpddl/`) :

1. connectent Postgres (souvent vide) ;
2. exécutent la migration ;
3. listent les index ;
4. comparent à `scan_indexes.golden` ou `cp_indexes.golden`.

**But :** détecter un changement d’index non voulu (oubli, renommage, régression GORM) en CI — pas imposer une procédure de rollback prod.

En dev, si tu changes volontairement le DDL : régénère le golden (`scripts/gen_scan_indexes_golden.go`, `scripts/gen_cp_indexes_golden.go`) et committe **code + golden dans la même PR**.

### Ce dont on n’a pas besoin (pour l’instant)

- Migrations incrémentales v1 → v2 → v3 avec préservation de données prod
- Scripts de rollback opérationnel sur schéma live
- Compatibilité avec une base legacy Discovery

Quand la prod existera, on pourra introduire des migrations versionnées **si** le schéma doit évoluer sans RAZ. Aujourd’hui, changer le schéma = reset volume dev + merge du nouveau DDL.

### RAZ dev (quand le schéma change brutalement)

```bash
# Exemple : conteneur compose
docker compose -f compose/20-discovery.yml down
docker volume rm <volume_postgres>   # nom selon stack cafe-deploy

# Ou dans psql
DROP SCHEMA public CASCADE;
CREATE SCHEMA public;
```

Au prochain boot, `cafe-persistence` recrée tables et index via `Migrate*Schema`.

## DDL scan (ADR §14.5)

> Rappel : [pourquoi migrer](#schéma-postgres--rôle-des-migrations-et-des-golden-files) même quand la DB dev est RAZ-able.

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
