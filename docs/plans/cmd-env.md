# Execution Plan: `loopzero env` Subcommands

**Date:** 2026-03-31
**Status:** Draft (requires meeting for T2 classification -- touches API contracts, CRD mutations, CLI output)
**Spec reference:** technical.md SS9 (data model), SS14 (multi-tenancy), SS25 (CLI reference)

---

## Open Questions (Require Decision Before Implementation)

### Q1: Are environments per-project or per-tenant?

**Current state:** Environments are per-tenant. The Tenant CRD `spec.environments[]` is a list of `TenantEnvironmentEntry{Name, Cluster}`. All projects within a tenant share the same environment set. The `environment_targets` table tracks `(tenant_id, project_id, environment)` -- the project dimension only exists at the deployment level, not at the environment definition level.

**Recommendation:** Keep environments per-tenant. Projects deploy *into* environments, but environments are infrastructure-level constructs (namespace + RBAC + quota + network policy). Per-project environments would multiply infrastructure resources without clear benefit.

**Implication for CLI:** `loopzero env list` does NOT require a `loopzero.yaml` -- it lists environments for the authenticated tenant. The `--project` flag is only relevant for `loopzero env inspect` (to show project-specific deployment state within an environment).

### Q2: Should there be a default set created automatically?

**Current state:** `buildTenantCR()` in `api/internal/k8s/tenant_cr_manager.go` hardcodes a single `production` environment when creating a Tenant CR. There is no `staging` default.

**Recommendation:** Keep `production` as the only automatic default. Users create additional environments explicitly via `loopzero env create staging`. This avoids paying the infrastructure cost (namespace + RBAC + quota + network policy) for unused environments.

### Q3: Delete env = delete all deployments in it?

**Current state:** Deleting an entry from `spec.environments[]` would cause the TenantController to stop reconciling it, but the existing Environment CR (owned by the Tenant via ownerRef) would become orphaned unless explicitly handled. The EnvironmentController's `reconcileDelete` already handles namespace cleanup when the Environment CR is deleted.

**Recommendation:** Yes. `loopzero env delete <name>` must:
1. Refuse to delete `production` (safety guard)
2. Require `--confirm <name>` flag (destructive operation)
3. API removes the environment entry from the Tenant CRD `spec.environments[]`
4. API deletes all `environment_targets` rows for `(tenant_id, *, environment)`
5. The TenantController detects the removed entry and deletes the child Environment CR
6. The EnvironmentController's finalizer handles namespace + RBAC + quota cleanup on the runtime cluster
7. All active releases in that environment become orphaned (ArgoCD will prune them when the namespace disappears)

**Open sub-question:** Should the TenantController proactively delete Environment CRs that exist but are NOT in `spec.environments[]`? Today `reconcileEnvironmentCRs` only creates/updates -- it does not garbage-collect. This is a **gap** that must be fixed for `loopzero env delete` to work end-to-end.

---

## Architecture Overview

```
CLI (loopzero env create staging)
  |
  v
API  POST /api/v1/environments  { name: "staging", cluster: "workload" }
  |
  v
API patches Tenant CR: adds entry to spec.environments[]
  |
  v
TenantController reconciles -> creates Environment CR (tenant-staging)
  |
  v
EnvironmentController reconciles -> creates namespace + RBAC + quota + network policy
  |
  v
API returns 201 { name: "staging", status: "provisioning" }
```

The key challenge: **environments are defined in the Tenant CRD spec, not in PostgreSQL**. Creating/deleting an environment means PATCHing the Tenant CR. The API already has a `TenantCRManager` that uses the dynamic client to mutate Tenant CRs. This must be extended with methods to add/remove environment entries.

---

## Component Changes

### 1. API: `TenantCRManager` Extension

**File:** `api/internal/k8s/tenant_cr_manager.go`

New methods on `TenantCRManager`:

```go
// AddEnvironment adds an environment entry to the Tenant CR spec.environments[].
// Idempotent: if the environment already exists, returns without error.
AddEnvironment(ctx context.Context, tenantID, envName, cluster string) error

// RemoveEnvironment removes an environment entry from the Tenant CR spec.environments[].
// Returns error if it's the last environment (spec.environments MinItems=1).
RemoveEnvironment(ctx context.Context, tenantID, envName string) error

// ListEnvironments returns all environment entries from the Tenant CR spec.environments[].
ListEnvironments(ctx context.Context, tenantID string) ([]EnvironmentEntry, error)

// GetEnvironmentStatus returns the Environment CR status for a specific environment.
GetEnvironmentStatus(ctx context.Context, tenantID, envName string) (*EnvironmentStatus, error)
```

**Interface update** in `api/internal/routes/projects.go`:

The `TenantCRManager` interface must be extended (or a new `EnvironmentManager` interface created per ISP). Recommendation: new interface `EnvironmentCRManager` to avoid bloating `TenantCRManager`.

```go
// EnvironmentCRManager abstracts Kubernetes Environment operations (DIP).
type EnvironmentCRManager interface {
    AddEnvironment(ctx context.Context, tenantID, envName, cluster string) error
    RemoveEnvironment(ctx context.Context, tenantID, envName string) error
    ListEnvironments(ctx context.Context, tenantID string) ([]EnvironmentEntry, error)
    GetEnvironmentStatus(ctx context.Context, tenantID, envName string) (*EnvironmentStatus, error)
}
```

**DoD:**
- [ ] `AddEnvironment` implemented with idempotency (no error if already exists)
- [ ] `RemoveEnvironment` implemented with MinItems=1 guard (cannot remove last env)
- [ ] `ListEnvironments` implemented, returns all entries from Tenant CR spec
- [ ] `GetEnvironmentStatus` implemented, reads Environment CR status
- [ ] `EnvironmentCRManager` interface defined (ISP — separate from TenantCRManager)
- [ ] `retry.RetryOnConflict` used for Tenant CR PATCH operations
- [ ] All methods use typed errors (`LoopZeroError`), not generic strings
- [ ] Unit tests for all 4 methods (success + error cases)
- [ ] `go test ./...` passes in `api/`

### 2. API: New Routes

**File:** `api/internal/routes/environments.go` (new)

| Method | Path | Purpose | Lock? |
|--------|------|---------|-------|
| `GET` | `/api/v1/environments` | List environments for tenant | No |
| `POST` | `/api/v1/environments` | Create environment (PATCH Tenant CRD) | No (idempotent CRD patch) |
| `GET` | `/api/v1/environments/:name` | Get environment details + active release | No |
| `DELETE` | `/api/v1/environments/:name` | Delete environment (PATCH Tenant CRD) | Yes (advisory lock on tenant) |

**DoD:**
- [ ] `GET /api/v1/environments` returns all tenant environments (200)
- [ ] `POST /api/v1/environments` creates environment via Tenant CR PATCH (201)
- [ ] `POST /api/v1/environments` idempotent — returns 200 if already exists
- [ ] `POST /api/v1/environments` validates name (400 for invalid, 400 for reserved)
- [ ] `GET /api/v1/environments/:name` returns env details + active releases per project (200)
- [ ] `GET /api/v1/environments/:name` returns 404 if not found
- [ ] `DELETE /api/v1/environments/:name` removes env from Tenant CR + cleans `environment_targets` (200)
- [ ] `DELETE /api/v1/environments/production` returns 403 (protected)
- [ ] `DELETE /api/v1/environments/:name` uses `withEnvironmentLock` (advisory lock on tenant)
- [ ] `DELETE /api/v1/environments/:name` returns 404 if not found
- [ ] Auth middleware active on all endpoints (401 without token)
- [ ] Idempotency middleware active on POST
- [ ] SEC-4: tenant ownership verified on all endpoints
- [ ] Correlation-ID forwarded
- [ ] Routes wired in `server.go`
- [ ] Unit tests in `api/tests/unit/environments_route_test.go` (all 11 test cases from Test Plan)
- [ ] `go test ./...` passes in `api/`

**Request/response shapes:**

```
POST /api/v1/environments
Request:  { "name": "staging" }
Response: 201 { "name": "staging", "status": "provisioning" }

GET /api/v1/environments
Response: 200 { "environments": [{ "name": "production", "status": "active" }, { "name": "staging", "status": "active" }] }

GET /api/v1/environments/staging
Response: 200 { "name": "staging", "status": "active", "created_at": "...", "projects": [{ "name": "my-api", "active_release": "rel-abc123", "url": "https://my-api-staging.dev.loopzero.example" }] }

DELETE /api/v1/environments/staging
Response: 200 { "name": "staging", "status": "deleting" }
```

### 3. Operator: TenantController Garbage Collection

**File:** `operators/internal/controller/tenant_controller.go`

`reconcileEnvironmentCRs` must be extended to delete Environment CRs that exist but are NOT in `spec.environments[]`. Today it only creates/updates.

```go
// After creating/updating all desired environments, list all Environment CRs
// owned by this Tenant and delete any that are not in spec.environments[].
```

This is critical for `loopzero env delete` to work. Without it, removing an entry from `spec.environments[]` leaves an orphaned Environment CR.

**DoD:**
- [ ] `reconcileEnvironmentCRs` lists all Environment CRs owned by Tenant after create/update loop
- [ ] Orphaned Environment CRs (not in `spec.environments[]`) are deleted
- [ ] Deletion triggers EnvironmentController finalizer (namespace + RBAC + quota cleanup)
- [ ] `errors.IsNotFound` handled gracefully (no error if CR already gone)
- [ ] Log emitted for each orphaned CR deletion
- [ ] Reconcile is idempotent (running twice produces same result)
- [ ] Crash mid-reconcile resumes correctly on next reconcile
- [ ] envtest test: remove env from spec -> Environment CR deleted after reconcile
- [ ] envtest test: remove env from spec -> namespace cleaned up on runtime cluster
- [ ] envtest test: CRD validation rejects spec with zero environments (MinItems=1)
- [ ] `go test ./...` passes in `operators/`

### 4. CLI: `loopzero env` Command Group

**File:** `cli/src/commands/env.ts` (new)

---

## Subcommand Details

### `loopzero env list`

**Syntax:**
```
loopzero env list [--json]
```

**Flags:**
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | boolean | false | Output as JSON |

**Exit codes:**
| Code | Condition |
|------|-----------|
| 0 | Success |
| 4 | Not authenticated |

**Human output:**
```
Environments for team my-team:

  NAME         STATUS     SERVICES
  production   active     3
  staging      active     1

2 environments
```

**JSON output:**
```json
{
  "environments": [
    { "name": "production", "status": "active", "service_count": 3 },
    { "name": "staging", "status": "active", "service_count": 1 }
  ]
}
```

**Implementation notes:**
- Calls `GET /api/v1/environments`
- Does NOT require `loopzero.yaml` (operates on tenant level)
- `service_count` comes from counting distinct projects with `environment_targets` rows for that environment
- Rule 3: "status" not "phase", "services" not "deployments" or "apps deployed"

**DoD:**
- [ ] `EnvListCommand` class implemented (DI via deps interface, following rollback.ts pattern)
- [ ] Auth check (exit 4, message suggests `loopzero login`)
- [ ] Human output: table with NAME, STATUS, SERVICES columns + count footer
- [ ] `--json` output matches schema (`{ "environments": [...] }`)
- [ ] Zero internal terms in any output string
- [ ] Unit tests: success, --json, not authenticated (3 tests minimum)
- [ ] `npm run build` passes

---

### `loopzero env create <name>`

**Syntax:**
```
loopzero env create <name> [--json]
```

**Arguments:**
| Arg | Required | Description |
|-----|----------|-------------|
| `name` | Yes | Environment name (lowercase, a-z0-9 and hyphens, max 63 chars) |

**Flags:**
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | boolean | false | Output as JSON |

**Exit codes:**
| Code | Condition |
|------|-----------|
| 0 | Success (environment created or already exists) |
| 1 | Invalid name (validation failed) |
| 3 | Creation failed (API/infrastructure error) |
| 4 | Not authenticated |

**Validation (client-side, reuse `validateEnvironment` from deploy.ts):**
- Must be lowercase
- Must start with a letter
- Max 63 chars (RFC 1123 for namespace compatibility)
- Only `a-z0-9-`
- Reserved names: cannot create `default`, `kube-system`, `kube-public`, `loopzero-system`

**Human output (success):**
```
Environment "staging" created. It will be ready in a few seconds.

Deploy to it with: loopzero deploy --env staging
```

**Human output (already exists):**
```
Environment "staging" already exists.
```

**JSON output:**
```json
{
  "name": "staging",
  "status": "provisioning",
  "created": true
}
```

**Implementation notes:**
- Calls `POST /api/v1/environments` with `{ "name": "staging" }`
- The API patches the Tenant CR to add the environment entry
- The `cluster` field defaults to `"workload"` (single-cluster for now, YAGNI)
- Idempotent: if environment already exists, returns success with `created: false`
- Rule 3: never mention namespace, CRD, controller, reconciler in output

**DoD:**
- [ ] `EnvCreateCommand` class implemented (DI via deps interface)
- [ ] Auth check (exit 4)
- [ ] Name argument required (exit 1 if missing)
- [ ] Client-side validation: lowercase, starts with letter, max 63 chars, `a-z0-9-` only (exit 1)
- [ ] Reserved name rejection: `default`, `kube-system`, `kube-public`, `loopzero-system` (exit 1)
- [ ] Success output shows confirmation + `loopzero deploy --env <name>` hint (exit 0)
- [ ] Idempotent: "already exists" message when env exists (exit 0)
- [ ] `--json` output for all scenarios (success, already exists, error)
- [ ] Error mapping: 400 -> validation (exit 1), 401 -> auth (exit 4), 5xx -> deploy failure (exit 3)
- [ ] Zero internal terms in any output string
- [ ] Unit tests: success, already exists, invalid name (uppercase), bad start char, missing name, reserved name, --json (7 tests minimum)
- [ ] `npm run build` passes

---

### `loopzero env delete <name>`

**Syntax:**
```
loopzero env delete <name> --confirm <name> [--json]
```

**Arguments:**
| Arg | Required | Description |
|-----|----------|-------------|
| `name` | Yes | Environment name to delete |

**Flags:**
| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--confirm` | string | Yes | Must match `<name>` exactly (safety guard) |
| `--json` | boolean | No | Output as JSON |

**Exit codes:**
| Code | Condition |
|------|-----------|
| 0 | Success (environment deletion initiated) |
| 1 | Validation failed (missing --confirm, name mismatch, reserved name, env not found) |
| 3 | Deletion failed (API/infrastructure error) |
| 4 | Not authenticated |

**Guards:**
- Cannot delete `production` -- hard block, no override
- `--confirm` flag must match the environment name exactly
- Cannot delete an environment that does not exist (returns exit 1 with clear message)

**Human output:**
```
Environment "staging" is being deleted. All services in this environment will be removed.

This may take a few minutes to complete.
```

**Human output (production guard):**
```
Cannot delete the "production" environment. This environment is protected.
```

**JSON output:**
```json
{
  "name": "staging",
  "status": "deleting"
}
```

**Implementation notes:**
- Calls `DELETE /api/v1/environments/staging`
- API must:
  1. Verify environment exists in Tenant CRD
  2. Reject if name is `production`
  3. Acquire advisory lock on tenant (prevents concurrent env mutations)
  4. Remove entry from `spec.environments[]` in Tenant CR
  5. Delete all `environment_targets` rows for `(tenant_id, *, environment)`
  6. TenantController detects missing entry, deletes Environment CR
  7. EnvironmentController finalizer cleans up namespace + RBAC + quota

**DoD:**
- [ ] `EnvDeleteCommand` class implemented (DI via deps interface)
- [ ] Auth check (exit 4)
- [ ] Name argument required (exit 1 if missing)
- [ ] `--confirm` flag required (exit 1 if missing, with clear message)
- [ ] `--confirm` value must match environment name exactly (exit 1 on mismatch)
- [ ] Production guard: hard block on `production` (exit 1, "protected" message)
- [ ] Success output shows deletion message + "may take a few minutes" (exit 0)
- [ ] Error mapping: 404 -> not found (exit 1), 401 -> auth (exit 4), 403 -> protected (exit 1), 5xx -> failure (exit 3)
- [ ] `--json` output for all scenarios
- [ ] Zero internal terms in any output string
- [ ] Unit tests: success, missing --confirm, wrong --confirm, production blocked, not found (5 tests minimum)
- [ ] `npm run build` passes

---

### `loopzero env inspect <name>`

**Syntax:**
```
loopzero env inspect <name> [--project <project>] [--json]
```

**Arguments:**
| Arg | Required | Description |
|-----|----------|-------------|
| `name` | Yes | Environment name to inspect |

**Flags:**
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project` | string | from loopzero.yaml if present | Filter to a specific project |
| `--json` | boolean | false | Output as JSON |

**Exit codes:**
| Code | Condition |
|------|-----------|
| 0 | Success |
| 1 | Environment not found |
| 4 | Not authenticated |

**Human output:**
```
Environment: staging
Status:      active
Created:     2026-03-28 14:22:00 UTC

Services:
  NAME       ACTIVE RELEASE   HEALTH   URL
  my-api     rel-abc123       healthy  https://my-api-staging.dev.loopzero.example
  my-worker  rel-def456       healthy  --
```

**JSON output:**
```json
{
  "name": "staging",
  "status": "active",
  "created_at": "2026-03-28T14:22:00Z",
  "services": [
    {
      "name": "my-api",
      "active_release": "rel-abc123",
      "health": "healthy",
      "url": "https://my-api-staging.dev.loopzero.example"
    },
    {
      "name": "my-worker",
      "active_release": "rel-def456",
      "health": "healthy",
      "url": null
    }
  ]
}
```

**Implementation notes:**
- Calls `GET /api/v1/environments/<name>`
- API joins `environment_targets` + `releases` + `builds` to assemble service-level data
- Health comes from the LoopZeroApplication CR status (or the health store)
- URL comes from ExposedRoute CR or release metadata
- If `--project` is specified, filters to that project only
- Rule 3: "services" not "deployments", "health" not "readiness probe", "active release" not "active_release_id"

**DoD:**
- [ ] `EnvInspectCommand` class implemented (DI via deps interface)
- [ ] Auth check (exit 4)
- [ ] Name argument required (exit 1 if missing)
- [ ] Human output: env details header + services table (NAME, ACTIVE RELEASE, HEALTH, URL)
- [ ] `--project` flag filters to specific project
- [ ] `--json` output matches schema (env details + services array)
- [ ] Environment not found returns exit 1 with clear message
- [ ] Zero internal terms in any output string
- [ ] Unit tests: success, --json, not found, --project filter (4 tests minimum)
- [ ] `npm run build` passes

---

## Data Flow Diagram

```
loopzero env create staging
    |
    v
CLI: validate name (client-side) -> POST /api/v1/environments { name: "staging" }
    |
    v
API EnvironmentHandler:
    1. Auth middleware extracts tenant_id
    2. Validate name (server-side, same rules)
    3. Call EnvironmentCRManager.AddEnvironment(tenantID, "staging", "workload")
        |
        v
    TenantCRManager (dynamic client):
        1. GET Tenant CR
        2. Append { name: "staging", cluster: "workload" } to spec.environments[]
        3. UPDATE Tenant CR (retry on conflict)
    4. Return 201 { name: "staging", status: "provisioning" }
    |
    v
TenantController (async reconcile):
    1. Detects spec change (new environment entry)
    2. reconcileEnvironmentCRs() creates Environment CR "tenant-staging"
    |
    v
EnvironmentController (async reconcile):
    1. Creates namespace on runtime cluster
    2. Creates RBAC (ServiceAccount, Role, RoleBinding)
    3. Creates NetworkPolicy (deny-all + allow-ingress + allow-egress)
    4. Creates ResourceQuota + LimitRange
    5. Sets Environment CR status.phase = Active
```

---

## Gap: TenantController Environment Garbage Collection

**Current behavior:** `reconcileEnvironmentCRs()` iterates `spec.environments[]` and creates/updates Environment CRs. It does NOT delete Environment CRs that are no longer in the spec.

**Required behavior:** After processing all entries in `spec.environments[]`, list all Environment CRs owned by this Tenant (via ownerRef) and delete any whose `spec.environment` name is not in the current `spec.environments[]` list.

**Implementation:**

```go
// After the create/update loop in reconcileEnvironmentCRs:
var envList loopzerov1.EnvironmentList
if err := r.Client.List(ctx, &envList, client.InNamespace(tenant.Namespace),
    client.MatchingFields{"metadata.ownerReferences.name": tenant.Name}); err != nil {
    return fmt.Errorf("listing environment CRs: %w", err)
}

desiredEnvs := make(map[string]bool)
for _, env := range tenant.Spec.Environments {
    desiredEnvs[env.Name] = true
}

for i := range envList.Items {
    envCR := &envList.Items[i]
    if !desiredEnvs[envCR.Spec.Environment] {
        if err := r.Client.Delete(ctx, envCR); err != nil && !errors.IsNotFound(err) {
            return fmt.Errorf("deleting orphaned environment CR %s: %w", envCR.Name, err)
        }
        log.FromContext(ctx).Info("deleted orphaned environment CR", "environment", envCR.Name)
    }
}
```

This must be carefully tested: deleting an Environment CR triggers the EnvironmentController's finalizer, which cleans up the namespace on the runtime cluster.

**DoD (Garbage Collection Bug Fix):**
- [ ] `reconcileEnvironmentCRs` extended with orphan deletion loop after create/update
- [ ] Lists all Environment CRs owned by Tenant (via ownerRef)
- [ ] Deletes CRs whose `spec.environment` name is NOT in `spec.environments[]`
- [ ] Handles `errors.IsNotFound` gracefully (no error if CR already deleted)
- [ ] Structured log emitted for each orphaned CR deletion
- [ ] envtest test: Tenant with env removed from spec -> reconcile deletes orphaned Environment CR
- [ ] envtest test: Tenant with all envs present -> reconcile does NOT delete anything
- [ ] envtest test: Orphaned CR deletion triggers EnvironmentController finalizer chain
- [ ] Reconcile is idempotent (deleting already-gone CR does not error)
- [ ] `go test ./...` passes in `operators/`

---

## Test Plan

### CLI Tests (`cli/tests/unit/env.test.ts`)

| Test | Exit Code | Assertion |
|------|-----------|-----------|
| `env list` success | 0 | Renders table with name, status, service count |
| `env list --json` | 0 | JSON matches schema |
| `env list` not authenticated | 4 | Error message suggests `loopzero login` |
| `env create staging` success | 0 | Shows confirmation + deploy hint |
| `env create staging` already exists | 0 | Shows "already exists" message |
| `env create STAGING` invalid (uppercase) | 1 | Error with lowercase suggestion |
| `env create -` invalid (bad start) | 1 | Error with format hint |
| `env create` missing name | 1 | Usage error |
| `env create default` reserved name | 1 | Error: reserved name |
| `env create staging --json` | 0 | JSON matches schema |
| `env delete staging --confirm staging` success | 0 | Shows deletion message |
| `env delete staging` missing confirm | 1 | Error: --confirm required |
| `env delete staging --confirm wrong` | 1 | Error: confirmation mismatch |
| `env delete production --confirm production` | 1 | Error: production is protected |
| `env delete nonexistent --confirm nonexistent` | 1 | Error: environment not found |
| `env inspect staging` success | 0 | Shows env details + services table |
| `env inspect staging --json` | 0 | JSON matches schema |
| `env inspect nonexistent` | 1 | Error: environment not found |
| No internal terms in any output | -- | Grep for forbidden terms (Rule 3) |

**DoD:**
- [ ] 19 tests written and passing (all rows above)
- [ ] Each test uses DI deps (project pattern)
- [ ] AAA pattern (Arrange-Act-Assert) in every test
- [ ] `npm test` passes
- [ ] No flaky tests

### API Tests (`api/tests/unit/environments_route_test.go`)

| Test | Assertion |
|------|-----------|
| `POST /api/v1/environments` creates env | 201, patches Tenant CR |
| `POST /api/v1/environments` idempotent | 200, no duplicate entry |
| `POST /api/v1/environments` invalid name | 400, typed error |
| `POST /api/v1/environments` reserved name | 400, typed error |
| `GET /api/v1/environments` lists all | 200, includes all tenant environments |
| `GET /api/v1/environments/:name` found | 200, includes services |
| `GET /api/v1/environments/:name` not found | 404 |
| `DELETE /api/v1/environments/staging` success | 200, patches Tenant CR |
| `DELETE /api/v1/environments/production` blocked | 403, "protected" |
| `DELETE /api/v1/environments/nonexistent` | 404 |
| Auth required on all endpoints | 401 without token |

**DoD:**
- [ ] 11 tests written and passing (all rows above)
- [ ] Tests use real `EnvironmentCRManager` interface with test doubles
- [ ] AAA pattern in every test
- [ ] `go test ./...` passes in `api/`
- [ ] No flaky tests

### Operator Tests (`operators/internal/controller/tenant_controller_test.go`)

| Test | Assertion |
|------|-----------|
| Remove env from spec -> Environment CR deleted | CR no longer exists after reconcile |
| Remove env from spec -> namespace cleaned up | Namespace deleted on runtime cluster |
| Cannot remove last env (MinItems=1) | CRD validation rejects |

**DoD:**
- [ ] 3 tests written and passing (all rows above)
- [ ] Tests run against envtest (no real cluster)
- [ ] Ginkgo spec format
- [ ] `go test ./...` passes in `operators/`
- [ ] No flaky tests

### Integration Tests

| Test | Assertion |
|------|-----------|
| `loopzero env create staging` -> `loopzero env list` shows it | End-to-end via CLI |
| `loopzero env create staging` -> `loopzero deploy --env staging` works | Deploy targets new env |
| `loopzero env delete staging --confirm staging` -> namespace gone | Infrastructure cleanup verified |

**DoD:**
- [ ] 3 integration tests written and passing
- [ ] Tests run against real k3d clusters (not mocks)
- [ ] `task test:e2e` passes
- [ ] No flaky tests

---

## Implementation Order

1. **Operator: TenantController GC** -- add orphan deletion to `reconcileEnvironmentCRs` + tests
2. **API: EnvironmentCRManager interface** -- define in `routes/environments.go`
3. **API: TenantCRManager extension** -- implement `AddEnvironment`, `RemoveEnvironment`, `ListEnvironments`, `GetEnvironmentStatus`
4. **API: Environment routes** -- `GET/POST/DELETE /api/v1/environments`, wire to server
5. **CLI: env command group** -- `list`, `create`, `delete`, `inspect` with tests
6. **Integration test** -- E2E via CLI against real clusters

---

## Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|------------|
| TenantController does not GC orphaned Environment CRs | **Blocker** | Must implement GC before `env delete` can work |
| Concurrent env mutations (two `env create` at same time) | Medium | `retry.RetryOnConflict` in TenantCRManager handles this |
| Deleting env with active deployments leaves orphaned releases in DB | Medium | `DELETE` route must clean `environment_targets`; release rows become historical |
| Environment name collision with internal namespaces | Low | Reserved name validation (default, kube-system, loopzero-system) |
| PATCH race between API and TenantController | Low | Both use optimistic concurrency (resourceVersion) |

---

## Change Classification

**Tier: T2** -- This change touches:
- API contracts (new routes, new response shapes)
- CRD mutations (patching Tenant CR spec)
- Operator reconcile logic (garbage collection of Environment CRs)
- CLI output (new command, new --json schemas)

Per governance rules, this requires a technical meeting before implementation.

---

## Gate: Complete

All items below must be checked before this plan is considered DONE.

### Operator
- [ ] TenantController garbage collection implemented (orphan Environment CR deletion)
- [ ] envtest tests passing (3 tests: orphan deleted, namespace cleaned, MinItems=1 guard)
- [ ] `go test ./...` passes in `operators/`
- [ ] Reconcile is idempotent and crash-safe

### API
- [ ] `EnvironmentCRManager` interface defined (ISP, separate from TenantCRManager)
- [ ] `TenantCRManager` implements all 4 methods (Add, Remove, List, GetStatus)
- [ ] `environments.go` routes file created with GET/POST/DELETE handlers
- [ ] Routes wired in `server.go`
- [ ] Auth middleware active on all environment endpoints
- [ ] Idempotency middleware active on POST
- [ ] `DELETE` uses advisory lock on tenant
- [ ] `DELETE` cleans `environment_targets` rows
- [ ] `DELETE` rejects `production` (403)
- [ ] All typed errors (`LoopZeroError`), no generic strings
- [ ] 11 API unit tests passing
- [ ] `go test ./...` passes in `api/`

### CLI
- [ ] `env.ts` command group created (list, create, delete, inspect)
- [ ] All 4 subcommands wired in `index.ts`
- [ ] `loopzero env --help` works
- [ ] `loopzero env list` works (human + JSON)
- [ ] `loopzero env create <name>` works (human + JSON, with validation)
- [ ] `loopzero env delete <name> --confirm <name>` works (human + JSON, with guards)
- [ ] `loopzero env inspect <name>` works (human + JSON, with --project filter)
- [ ] Exit codes: 0 (success), 1 (usage/validation), 3 (infra failure), 4 (auth)
- [ ] Zero internal terms in any CLI output string (Rule 3 verified)
- [ ] 19 CLI unit tests passing
- [ ] `npm test` passes
- [ ] `npm run build` passes

### Integration
- [ ] `loopzero env create staging` -> `loopzero env list` shows it
- [ ] `loopzero env create staging` -> `loopzero deploy --env staging` works
- [ ] `loopzero env delete staging --confirm staging` -> namespace removed

### Cross-Cutting
- [ ] `task test:unit` passes
- [ ] `task lint` passes
- [ ] `task validate:manifests` passes
- [ ] No internal terms in CLI output (Rule 3)
- [ ] `--json` schemas stable (Rule 5)
- [ ] Exit codes are 0/1/3/4 only (Rule 4)
- [ ] CHANGELOG.md updated (`[Unreleased]` > `Added`)
- [ ] Meeting approved (T2 classification)
