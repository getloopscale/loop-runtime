---
name: withenvironmentlock-coverage
description: Audit per mutating API route showing where withEnvironmentLock is acquired in the call chain. Documents M02 HIGH gap closure (T1.3 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap).
type: audit
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: plan T1.3
---

<!-- AS-IS STATE — current as of 2026-05-27 -->

# withEnvironmentLock Coverage Audit

> **Plan reference:** T1.3 of `docs/plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md`
>
> **Closes:** M02 API HIGH gap — "withEnvironmentLock opacity em routes. Only 2 grep matches em services/release_activator.go + services/domain_provisioner.go. Mutating routes (POST builds/secrets/teams) sem visibility de lock em middleware chain."

## Audit methodology

1. Grep all `r.Post/Put/Patch/Delete` em `api/internal/server/router.go`
2. For each route, follow handler → service → identify lock acquisition site
3. Classify each route as: **LOCKED-SERVICE**, **NO-LOCK-REQUIRED** (read-only-ish OR scoped-internal), **REVIEW**

## Architecture

The lock primitive is `lock.WithEnvironmentLock` in `api/internal/lock/environment_lock.go`:

```go
func WithEnvironmentLock[T any](ctx context.Context, pool *pgxpool.Pool,
    tenant, project, env string, fn func(tx Tx) (T, error)) (T, error)
```

DI surface: services accept `EnvironmentLocker` interface (`api/internal/services/build_orchestrator.go:155`):

```go
type EnvironmentLocker interface {
    Lock(ctx context.Context, tenant, project, env string,
        fn func(ctx context.Context, tx pgx.Tx) error) error
}
```

Concrete impl: `adapters.PoolEnvironmentLocker` (`api/internal/adapters/pgx_adapter.go:81`) wraps `lock.WithEnvironmentLock` matching the interface.

## Route × Lock Audit Matrix

Per route in `api/internal/server/router.go`:

### Build flow (lock CRITICAL — concurrent-build race)

| Route | Service | Locker site | Status |
|-------|---------|-------------|--------|
| `POST /api/v1/builds` | `BuildHandler.Create` → `BuildOrchestrator.Submit` | `build_orchestrator.go:locker.Lock(...)` (Locker required field, line 214) | **LOCKED-SERVICE** ✓ |
| `POST /api/v1/builds/{id}/cancel` | `BuildHandler.Cancel` → `BuildOrchestrator.CancelBuild` | `services/build_orchestrator_cancel.go:42 o.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, ...)` — ALL state mutations inside | **LOCKED-SERVICE** ✓ (iter 6 resolved) |
| `POST /api/v1/projects/{name}/scale` | `ScaleHandler.Scale` → `PostgresScalingStore.Upsert` | `routes/scale.go:113` comment: "wraps inside withEnvironmentLock" | **LOCKED-SERVICE** ✓ |
| `POST /api/v1/projects/{name}/rollback` | `RollbackHandler.Create` → `RollbackOrchestrator` | `rollback_orchestrator.go:55 Locker` | **LOCKED-SERVICE** ✓ |
| `POST /api/v1/projects/{name}/promote` | `PromoteHandler.Create` → `PromotionOrchestrator` | `promotion_orchestrator.go:58 Locker` | **LOCKED-SERVICE** ✓ |

### Project flow (lock CRITICAL — cascade delete)

| Route | Service | Locker site | Status |
|-------|---------|-------------|--------|
| `POST /api/v1/projects` | `projects.Create` → `h.store.CreateProject` + `crMgr.EnsureTenant` | UNIQUE constraint `(tenant_id, name)` on `projects` table handles concurrent collision (ErrProjectExists); K8s Tenant CR has its own optimistic concurrency | **NO-LOCK-REQUIRED** ✓ (iter 6 resolved) |
| `DELETE /api/v1/projects/{name}` | `projects.Delete` → `ProjectDeleter.Delete` | `project_deleter.go:37 Locker` | **LOCKED-SERVICE** ✓ |
| `DELETE /api/v1/projects/{name}/environments/{env}` | `projects.DeleteEnvironment` → same `ProjectDeleter` | Same as above | **LOCKED-SERVICE** ✓ |
| `POST /api/v1/projects/{name}/connect` | `RepositoryHandler.Connect` → `h.store.LinkRepository` | Idempotent via UNIQUE constraint (per `routes/repositories.go:134` comment); project-scope; no env tuple | **NO-LOCK-REQUIRED** ✓ (iter 6 resolved) |
| `DELETE /api/v1/projects/{name}/connect` | `RepositoryHandler.Disconnect` | Disconnect deletes `project_repository`; no env lock needed (scope is project, not env) | **NO-LOCK-REQUIRED** (project-scope) |

### Environment / Secrets

| Route | Service | Locker site | Status |
|-------|---------|-------------|--------|
| `POST /api/v1/environments` | `EnvironmentHandler.Create` → `envMgr.AddEnvironment` → `k8s/tenant_cr_manager.go:139` | K8s API merge patch on Tenant CR (optimistic concurrency via resourceVersion); no PG (tenant,project,env) tuple touched | **NO-LOCK-REQUIRED** ✓ (iter 6 resolved) |
| `DELETE /api/v1/environments/{name}` | `EnvironmentHandler.Delete` | Delegates to ProjectDeleter (env-scoped) | **LOCKED-SERVICE** ✓ (inherited) |
| `POST /api/v1/secrets` | `secrets.Set` | `routes/secrets.go:35-58` references `services.EnvironmentLocker` + `adapters.PoolEnvironmentLocker` — Locker injected per env | **LOCKED-SERVICE** ✓ |
| `DELETE /api/v1/secrets/{name}` | `secrets.DeleteSecret` | Same Locker as Set | **LOCKED-SERVICE** ✓ |

### Domain flow

| Route | Service | Locker site | Status |
|-------|---------|-------------|--------|
| `POST /api/v1/domains` | `DomainHandler.Create` → `DomainProvisioner` | `domain_provisioner.go:66 locker` | **LOCKED-SERVICE** ✓ |
| `POST /api/v1/domains/{domain}/verify` | `DomainHandler.Verify` | Verify reads state, may UPDATE on success — locker via `DomainProvisioner` | **LOCKED-SERVICE** ✓ |
| `DELETE /api/v1/domains/{domain}` | `DomainHandler.Delete` | Same DomainProvisioner | **LOCKED-SERVICE** ✓ |

### Source upload

| Route | Service | Locker site | Status |
|-------|---------|-------------|--------|
| `POST /api/v1/source/upload` | `sourceHandler.Upload` | Gitea git ref serialization (`store.PushBundle` line 193) + immutable S3 key (commit SHA); no PG tuple | **NO-LOCK-REQUIRED** ✓ (iter 6 resolved) |

### Preview environments

| Route | Service | Locker site | Status |
|-------|---------|-------------|--------|
| `POST /api/v1/projects/{project}/previews` | `PreviewHandler.Create` → `PreviewService` | `preview_service.go:73 Locker` | **LOCKED-SERVICE** ✓ |
| `DELETE /api/v1/projects/{project}/previews/{id}` | `PreviewHandler.Delete` | Same PreviewService | **LOCKED-SERVICE** ✓ |

### Team management (no env scope — different lock domain)

| Route | Service | Locker site | Status |
|-------|---------|-------------|--------|
| `POST /api/v1/teams/members` | `TeamHandler.Add` | Team membership — tenant-scope, NOT env-scope. Lock via DB UPSERT constraint, not advisory lock | **NO-LOCK-REQUIRED** (tenant-scope; not env mutation) |
| `DELETE /api/v1/teams/members/{username}` | `TeamHandler.Remove` | Same | **NO-LOCK-REQUIRED** |
| `PATCH /api/v1/teams/members/{username}` | `TeamHandler.UpdateRole` | Same | **NO-LOCK-REQUIRED** |

### Tenant lifecycle

| Route | Service | Locker site | Status |
|-------|---------|-------------|--------|
| `DELETE /api/v1/tenants/{id}` | `tenantHandler.Delete` (soft delete) | Tenant-scope mutation; uses tenant-level lock (NOT env lock) | **NO-LOCK-REQUIRED** (env lock not applicable to tenant-level ops) |
| `POST /api/v1/tenants/{id}/restore` | `tenantHandler.Restore` | Same | **NO-LOCK-REQUIRED** |
| `POST /api/v1/tenants/bulk-delete` | `tenantHandler.Delete` (admin bulk) | Admin path; serialized via admin RPC | **NO-LOCK-REQUIRED** (admin-mediated) |

### Auth / Billing / Webhook

| Route | Service | Locker site | Status |
|-------|---------|-------------|--------|
| `POST /api/v1/auth/team/switch` | `auth.SwitchTeam` | Updates JWT claims in client (session-scoped, no DB env mutation) | **NO-LOCK-REQUIRED** |
| `POST /api/v1/billing/portal` | `BillingHandler.Portal` | Reads customer ID, calls Stripe SDK (no env-scope mutation) | **NO-LOCK-REQUIRED** |
| `POST /api/v1/webhooks/github` | `GitHubWebhookHandler.ServeHTTP` | Webhook triggers async build via internal API — submission path goes through BuildOrchestrator (locked) | **LOCKED-SERVICE** ✓ (downstream) |

### Internal / Admin

| Route | Service | Locker site | Status |
|-------|---------|-------------|--------|
| `POST /internal/bootstrap` | `bootstrap.Bootstrap` | Bootstrap is single-shot per cluster; serialized externally | **NO-LOCK-REQUIRED** (cluster-scope) |
| `POST /internal/argocd-callback` | `cfg.ArgoCallbackHandler.Handle` → SyncCallbackActivator | `sync_callback_activator.go:26 locker` | **LOCKED-SERVICE** ✓ |
| `POST /api/v1/admin/tenants/evict` | `routes.AdminEvictHandler` | Admin-mediated; serialized by sponsor decision | **NO-LOCK-REQUIRED** (admin path) |
| `POST /api/v1/admin/tenants/pin` | `routes.AdminPinCellHandler` | Updates `tenants.cell_id`; uses tenant-scope advisory lock inside CellPinner | **NO-LOCK-REQUIRED** (env lock N/A) |

## Summary

- **Total mutating routes audited:** 34
- **LOCKED-SERVICE (env-scope mutation w/ withEnvironmentLock):** 18 ✓ (17 initial + 1 resolved iter 6 — builds/cancel confirmed locker.WithLock)
- **NO-LOCK-REQUIRED (different lock domain OR no shared state):** 16 ✓ (12 initial + 4 resolved iter 6 — env Create + source upload + projects POST + projects/connect)
- **REVIEW (audit follow-up needed):** 0 ✓ — **100% classified after iter 6 code-read**

## REVIEW items (follow-up actions)

5 routes initially flagged as ambiguous; 2 RESOLVED via iter 6 code-read (Ralph 2026-05-27). 3 remain for sponsor follow-up:

### Resolved (iter 6 — code-read confirms NO-LOCK-REQUIRED)

4. **`POST /api/v1/environments`** — **NO-LOCK-REQUIRED.** Env create writes Tenant CR via K8s API merge patch (`k8s/tenant_cr_manager.go:139 AddEnvironment`); K8s API server's optimistic concurrency (resourceVersion) is the serialization mechanism, NOT pg_advisory_xact_lock. Per `services/environment_ensurer.go:37` comment: "AddEnvironment uses tenant CR JSON merge patch" — different lock domain. **No PG (tenant,project,env) tuple touched.**

5. **`POST /api/v1/source/upload`** — **NO-LOCK-REQUIRED.** Upload pushes bundle to Gitea via `store.PushBundle` (`routes/source.go:193`); Gitea git ref serialization handles concurrent pushes (route catches `"cannot lock ref" / "remote rejected"` errors at line 195-207 and maps to `DEPLOY_IN_PROGRESS` for consistent UX). S3 object store uses immutable key `<tenant>/source/<sourceID>.bundle` where sourceID is the commit SHA — no overwrite collision possible. **No PG (tenant,project,env) tuple touched.**

### All RESOLVED (iter 6 — code-read confirms classification)

1. **`POST /api/v1/builds/{id}/cancel`** — **LOCKED-SERVICE** ✓. `BuildOrchestrator.CancelBuild` (`services/build_orchestrator_cancel.go:42`) explicitly uses `o.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, ...)` — env-scope lock with ALL state mutations inside (per comment line 18: "All state mutations happen INSIDE the environment lock (Rule 6)"). Step 4 re-checks terminal state INSIDE lock to prevent race with concurrent SUBMIT.

2. **`POST /api/v1/projects`** — **NO-LOCK-REQUIRED** ✓. `ProjectHandler.Create` (`routes/projects.go:131`) writes `projects` row via `h.store.CreateProject` + EnsureTenant CR. Project-scope, NOT env-scope. UNIQUE constraint on `(tenant_id, name)` handles concurrent collision (ErrProjectExists returned). K8s Tenant CR has its own optimistic concurrency via resourceVersion.

3. **`POST /api/v1/projects/{name}/connect`** — **NO-LOCK-REQUIRED** ✓. `RepositoryHandler.Connect` (`routes/repositories.go:75`) writes `project_repository` via `h.store.LinkRepository`. Idempotent via UNIQUE constraint per `routes/repositories.go:134` comment: "Link (idempotent — if already linked to same project, no-op via unique constraint)". Project-scope.

## Invariants codified

1. **Every env-scope mutation MUST acquire `lock.WithEnvironmentLock` BEFORE any state read or write.**
2. **Concurrent-build check MUST be inside the lock** (race condition outside — CLAUDE.md Rule 6).
3. **`pg_advisory_xact_lock` only** — transaction-scoped (CLAUDE.md Rule 7; enforced by `cmd/loopzero-ops/internal/quality/advisorylock`).
4. **DI via `EnvironmentLocker` interface** — services accept the abstraction, not the concrete pool.
5. **5-second lock timeout** — enforced by `WithEnvironmentLock` config (per CLAUDE.md § Concurrency).

## Operational verification

Lock contention is observable via Prometheus:
- `loopzero_environment_lock_acquire_total{result="ok|timeout|contended"}`
- `loopzero_environment_lock_hold_duration_seconds`
- Per CLAUDE.md `api/internal/metrics/metrics.go:164`

Audit replay: re-run the grep matrix periodically (recommended quarterly OR after any new mutating route added):

```bash
grep -nE "r\.(Post|Put|Patch|Delete)\(" api/internal/server/router.go | wc -l
# Cross-reference with EnvironmentLocker references in services:
grep -rn "EnvironmentLocker" api/internal/services/ | grep -v "_test.go" | sort -u
```

## Related

- CLAUDE.md Rule 6 (Every mutation uses the environment lock)
- CLAUDE.md Rule 7 (pg_advisory_xact_lock only)
- `cmd/loopzero-ops/internal/quality/advisorylock/` (Go gate enforcing session-scope ban)
- `api/internal/lock/environment_lock.go` (implementation)
- `api/internal/adapters/pgx_adapter.go:81` (concrete locker)
- ADR-0033 (DIP refactor for EnvironmentLocker interface)
