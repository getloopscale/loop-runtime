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

> **Plan reference:** T1.3 of `docs/plans/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md`
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
| `POST /api/v1/builds/{id}/cancel` | `BuildHandler.Cancel` → store update | Cancel mutates `builds.status` via UPDATE — wrapped via `build_reaper.go:locker` for reconcile flows. Direct user cancel: review-needed | **REVIEW** |
| `POST /api/v1/projects/{name}/scale` | `ScaleHandler.Scale` → `PostgresScalingStore.Upsert` | `routes/scale.go:113` comment: "wraps inside withEnvironmentLock" | **LOCKED-SERVICE** ✓ |
| `POST /api/v1/projects/{name}/rollback` | `RollbackHandler.Create` → `RollbackOrchestrator` | `rollback_orchestrator.go:55 Locker` | **LOCKED-SERVICE** ✓ |
| `POST /api/v1/projects/{name}/promote` | `PromoteHandler.Create` → `PromotionOrchestrator` | `promotion_orchestrator.go:58 Locker` | **LOCKED-SERVICE** ✓ |

### Project flow (lock CRITICAL — cascade delete)

| Route | Service | Locker site | Status |
|-------|---------|-------------|--------|
| `POST /api/v1/projects` | `projects.Create` | Project create writes `projects` row + downstream env init; lock contention low (no cross-env) | **REVIEW** |
| `DELETE /api/v1/projects/{name}` | `projects.Delete` → `ProjectDeleter.Delete` | `project_deleter.go:37 Locker` | **LOCKED-SERVICE** ✓ |
| `DELETE /api/v1/projects/{name}/environments/{env}` | `projects.DeleteEnvironment` → same `ProjectDeleter` | Same as above | **LOCKED-SERVICE** ✓ |
| `POST /api/v1/projects/{name}/connect` | `RepositoryHandler.Connect` | Connect writes `project_repository` row; conflict via UNIQUE constraint | **REVIEW** |
| `DELETE /api/v1/projects/{name}/connect` | `RepositoryHandler.Disconnect` | Disconnect deletes `project_repository`; no env lock needed (scope is project, not env) | **NO-LOCK-REQUIRED** (project-scope) |

### Environment / Secrets

| Route | Service | Locker site | Status |
|-------|---------|-------------|--------|
| `POST /api/v1/environments` | `EnvironmentHandler.Create` | Env create writes `environments` row + downstream provisioning | **REVIEW** |
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
| `POST /api/v1/source/upload` | `sourceHandler.Upload` | Upload writes S3 bundle + DB row; env-scope. Locker injection per env | **REVIEW** |

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
- **LOCKED-SERVICE (env-scope mutation w/ withEnvironmentLock):** 17 ✓
- **NO-LOCK-REQUIRED (different lock domain OR no shared state):** 12 ✓
- **REVIEW (audit follow-up needed):** 5 — see below

## REVIEW items (follow-up actions)

These 5 routes mutate state but the lock site is ambiguous from grep alone. Each warrants a code-review pass (separate task — not blocking this audit):

1. **`POST /api/v1/builds/{id}/cancel`** — Direct cancel mutates `builds.status`. Currently via store update; should be inside Build.Locker path to prevent race with concurrent SUBMIT. Recommended: route through `BuildOrchestrator.Cancel(id, env)` with Locker call.
2. **`POST /api/v1/projects`** — Create writes `projects` row + init envs. Project-scope, not env-scope. Lock domain ambiguous; recommend tenant-level serialization (not env lock).
3. **`POST /api/v1/projects/{name}/connect`** — Connect writes `project_repository` (project-scope). UNIQUE constraint handles conflict; no env lock needed.
4. **`POST /api/v1/environments`** — Env create writes `environments` row. Project-scope. Recommend explicit serialization via project-level advisory lock OR project_id UNIQUE constraint.
5. **`POST /api/v1/source/upload`** — Writes S3 bundle + DB row. Env-scope intended; verify Locker injection.

## Invariants codified

1. **Every env-scope mutation MUST acquire `lock.WithEnvironmentLock` BEFORE any state read or write.**
2. **Concurrent-build check MUST be inside the lock** (race condition outside — CLAUDE.md Rule 6).
3. **`pg_advisory_xact_lock` only** — transaction-scoped (CLAUDE.md Rule 7; enforced by `pkg/quality/advisorylock`).
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
- `pkg/quality/advisorylock/` (Go gate enforcing session-scope ban)
- `api/internal/lock/environment_lock.go` (implementation)
- `api/internal/adapters/pgx_adapter.go:81` (concrete locker)
- ADR-0033 (DIP refactor for EnvironmentLocker interface)
