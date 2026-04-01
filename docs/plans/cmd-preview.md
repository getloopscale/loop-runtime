# Execution Plan: `loopzero preview` — Preview Environments

**Date:** 2026-03-31
**Status:** Draft
**Tier:** T3 (architecture — new controller, webhook handler, DNS routing, CLI commands, API endpoints, DB schema)
**Depends on:** Sprint 2 (deploy happy path), Environment controller (wired), TenantController (preview spec), pkg/statemachine/preview.go
**Sprint:** 5 (preview-environments)

---

## Context

### What Preview Environments Are

Preview environments are ephemeral, isolated deployments created automatically (or manually) for pull requests. Each PR gets its own namespace, its own URL, and its own lifecycle. When the PR is merged or closed, the preview is destroyed. If forgotten, TTL expires it.

This is the MOST COMPLEX feature in the LoopZero platform because it touches every plane:

| Plane | Impact |
|-------|--------|
| **Developer** | New CLI commands (`loopzero preview list/create/delete/inspect`), GitHub PR comment integration |
| **Control** | New PreviewEnvironmentController, webhook handler, GitHub App/webhook receiver, TTL reaper |
| **Build** | Same Argo Workflow pipeline but with preview-specific parameters (branch, PR number) |
| **Workload** | Ephemeral namespaces, reduced resource quotas, preview-specific DNS routes |

### What Already Exists

| Component | Status | Location |
|-----------|--------|----------|
| PreviewEnvironment CRD | Defined, not wired | `operators/api/v1/preview_types.go` |
| Preview state machine | Implemented + tested | `pkg/statemachine/preview.go` |
| PreviewSpec on Tenant | Defined (maxEnvironments, ttlHours) | `operators/api/v1/tenant_types.go:104-110` |
| PreviewPhase constants | Defined in CRD types | `operators/api/v1/preview_types.go:14-22` |
| PreviewEnvironmentController | NOT implemented | `operators/internal/controller/` (absent) |
| CLI `loopzero preview` | NOT implemented | `cli/src/commands/` (absent) |
| API preview endpoints | NOT implemented | `api/internal/routes/` (absent) |
| DB preview_environments table | NOT implemented | `api/internal/db/migrations/` (absent) |
| GitHub webhook handler | NOT implemented | `api/internal/routes/` (absent) |
| Preview cleanup job | NOT implemented | — |

### State Machine (Already Implemented)

```
CREATING ──CreationCompleted──> ACTIVE ──ExpirationTriggered──> EXPIRING ──CleanupCompleted──> DELETED
    │                              │                                │
    │ CreationFailed               │ DeletionTriggered              │ CleanupFailed
    v                              v                                v
  FAILED ──DeletionTriggered──> DELETING ──CleanupCompleted──> DELETED
                                    │
                                    │ CleanupFailed
                                    v
                               CLEANUP_FAILED ──GarbageCollected──> DELETED
```

Terminal state: `DELETED`
TTL is handled by `ExpiresAt` field on the CRD spec, NOT by state machine timeouts.

---

## Open Questions (Require Meeting Decision)

These questions MUST be resolved in a `/meeting` before implementation begins. Each has a recommendation.

### Q1: Auto-create on PR push, or manual `loopzero preview create`?

| Option | Flow | Pros | Cons |
|--------|------|------|------|
| **A: Auto-create via GitHub webhook** | GitHub sends PR event to API webhook → API creates PreviewEnvironment CR → controller provisions | Zero friction, Vercel-like DX. Developer opens PR and gets a URL. | Requires GitHub App or webhook setup. Every PR creates a preview even if unwanted. Cost for high-PR-volume repos. |
| **B: Manual `loopzero preview create --pr 42`** | Developer explicitly requests a preview for a specific PR. | No webhook setup. Developer controls cost. Simpler to implement first. | Extra step. Not the Vercel DX we target. |
| **C: Both (webhook default, `loopzero preview create` as fallback)** | Webhook auto-creates. CLI allows manual creation for repos without webhook. | Best DX. Covers all cases. | Most complex to implement. |

**Recommendation:** Option C, phased. Phase A implements manual CLI flow. Phase B adds webhook auto-creation. This way we ship value incrementally and the manual path serves as the fallback forever.

### Q2: One preview per PR, or one per PR per app (monorepo)?

| Option | Flow | Pros | Cons |
|--------|------|------|------|
| **A: One preview per PR** | Single namespace with all apps from the monorepo deployed together. | Simpler. Matches how production works (all apps deploy together). | All-or-nothing — can't preview just the frontend. |
| **B: One preview per PR per app** | Each app in `loopzero.yaml` gets its own preview namespace. | Granular. Can preview just the changed app. | Explosion of namespaces. Complex routing. Which apps changed? |

**Recommendation:** Option A. One preview per PR mirrors the production model. The preview namespace contains all apps from the project, deployed from the PR branch. Changed-file detection (Option B) is a future optimization — YAGNI for now.

### Q3: Preview database — shared or isolated?

| Option | Flow | Pros | Cons |
|--------|------|------|------|
| **A: No database for previews** | Preview apps connect to nothing or use in-memory/SQLite. | Zero cost. Zero risk. | Useless for apps that need a DB. |
| **B: Shared staging DB** | Previews connect to the staging database. | Simple. Data already exists. | Previews can corrupt staging data. |
| **C: Ephemeral DB per preview** | Each preview gets a fresh database (PG schema or container). | Isolated. Safe. | Complex provisioning. Cost. Cleanup. |

**Recommendation:** Option A for v1. Previews are for visual/API verification, not data workflows. Apps that need a DB can use in-memory or connect to an external dev DB via `loopzero secret`. Ephemeral DB (Option C) is a clear v2 feature.

### Q4: Preview secrets — inherit from staging or dedicated?

| Option | Flow | Pros | Cons |
|--------|------|------|------|
| **A: Inherit from staging** | Preview namespace gets the same secrets as the staging environment. | Simple. Works immediately. | Staging secrets in ephemeral namespaces — risk if preview is compromised. |
| **B: Dedicated preview secrets** | Separate secret set for previews, managed via `loopzero secret set --env preview`. | Clean isolation. | Extra setup per project. |
| **C: Inherit staging, override per preview** | Start with staging secrets, allow overrides via `loopzero preview create --secret KEY=VALUE`. | Best of both. | Complex. |

**Recommendation:** Option A for v1. Staging secrets are already non-production. The security boundary is acceptable for preview environments. Preview-specific secrets (Option B) can be added when tenants request it.

### Q5: GitHub PR comment integration?

| Option | Flow | Pros | Cons |
|--------|------|------|------|
| **A: No comment** | Preview URL only visible via `loopzero preview list`. | Simplest. | Developer must leave GitHub to find the URL. |
| **B: Comment on PR** | API posts a comment with preview URL, status, and TTL on the PR. Updates on redeploy. | Vercel-like DX. URL where the developer already is. | Requires GitHub API token with write access. Comment management (edit vs new). |

**Recommendation:** Option B. This is the DX differentiator. A single PR comment that updates with each push: preview URL, deploy status, TTL remaining. Requires the GitHub App token (already used for auth) to have `pull_requests:write` scope.

### Q6: Resource limits for previews?

| Option | Flow | Pros | Cons |
|--------|------|------|------|
| **A: Same as production** | Preview gets same CPU/memory as prod. | Consistent behavior. | Expensive. Previews consume prod-equivalent resources. |
| **B: Reduced (50% of staging)** | Hardcoded lower limits for previews. | Cost-effective. Fair to other tenants. | May not surface performance issues. |
| **C: Configurable per tenant** | `PreviewSpec.resourceScale` factor (0.25, 0.5, 1.0). | Flexible. | Premature config — YAGNI? |

**Recommendation:** Option B. Previews are for functional verification, not load testing. Hardcode 50% of staging limits. Add `PreviewSpec.resourceScale` only if a tenant requests it.

---

## Architecture

### System Flow

```
GitHub PR Event ──webhook──> API Webhook Handler
                                │
                                ├─ Validate: tenant exists, project exists, PR is open
                                ├─ Check: count active previews < tenant.Preview.MaxEnvironments
                                ├─ Calculate: expiresAt = now + tenant.Preview.TTLHours
                                │
                                ├─ Insert preview record in PostgreSQL
                                ├─ Create PreviewEnvironment CR on mgmt cluster
                                │
                                v
                     PreviewEnvironmentController (mgmt cluster)
                                │
                                ├─ Create namespace on runtime cluster: `preview-{tenant}-{project}-pr{number}`
                                ├─ Create RBAC, ResourceQuota, LimitRange, NetworkPolicy (same as Environment controller, reduced limits)
                                ├─ Trigger build via API (same Argo Workflow pipeline, branch = PR branch)
                                ├─ Wait for build completion
                                ├─ Push manifests to Gitea (preview-specific path in gitops repo)
                                ├─ ArgoCD syncs preview app
                                ├─ Create ExposedRoute for preview DNS
                                │
                                ├─ Update CR status: phase = Active, url = pr-{number}.{project}.{tenant}.{baseDomain}
                                ├─ Post GitHub PR comment with URL
                                │
                                v
                           ACTIVE (serving traffic)
                                │
                    ┌───────────┼───────────────┐
                    │           │               │
              PR updated    PR merged/     TTL expired
              (new push)     closed
                    │           │               │
                    v           v               v
              Rebuild      DELETING         EXPIRING
              (same flow)      │               │
                               v               v
                         Cleanup namespace, ArgoCD app, gitops path
                               │               │
                               v               v
                            DELETED          DELETED
```

### DNS Routing

Preview URLs follow the pattern:

```
pr-{number}.{project}.{tenant}.{baseDomain}
```

Examples:
- `pr-42.api-server.acme.dev.loopzero.example`
- `pr-7.web-app.acme.dev.loopzero.example`

This works with the existing wildcard DNS (`*.dev.loopzero.example`) and Cloudflare Tunnel routing. No new DNS records needed per preview — the wildcard covers it.

For static frontend previews (detected via `loopzero.yaml` render config):
- Build output uploaded to DO Spaces at prefix `previews/{tenant}/{project}/pr-{number}/{buildId}/`
- Cloudflare KV entry: `pr-{number}.{project}.{tenant}` -> `{ buildId, s3Prefix }`
- CF Worker routes the request to DO Spaces (same as production static delivery)

### Namespace Naming

```
preview-{tenant}-{project}-pr{number}
```

Examples:
- `preview-acme-api-server-pr42`
- `preview-acme-web-app-pr7`

Must be valid RFC 1123 label (63 chars max). Use `pkg/naming` for deterministic generation with truncation.

### Preview Record (PostgreSQL)

New `preview_environments` table:

```sql
CREATE TABLE preview_environments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL,
    project_name    TEXT NOT NULL,
    pr_number       INTEGER NOT NULL,
    branch          TEXT NOT NULL,
    phase           TEXT NOT NULL DEFAULT 'CREATING',
    url             TEXT,
    build_id        TEXT,
    release_id      TEXT,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ,

    UNIQUE (tenant_id, project_name, pr_number)
);

CREATE INDEX idx_preview_env_tenant_project ON preview_environments (tenant_id, project_name);
CREATE INDEX idx_preview_env_phase ON preview_environments (phase) WHERE phase != 'DELETED';
CREATE INDEX idx_preview_env_expires ON preview_environments (expires_at) WHERE phase = 'ACTIVE';
```

The `UNIQUE (tenant_id, project_name, pr_number)` constraint enforces one preview per PR per project (Q2 decision).

---

## Phase A: PreviewEnvironment Controller + Manual CLI (1 sprint)

> Foundation. Controller creates/destroys previews. CLI provides manual control.
> No webhook. No GitHub integration. Developer explicitly creates previews.

### A.1 — Database Migration: `preview_environments` Table

**File:** `api/internal/db/migrations/NNNN_create_preview_environments.up.sql`

Create the table as defined above. Forward-only migration (no `.down.sql`).

**Tests:**
- Migration applies cleanly on fresh DB
- Migration is idempotent (run twice, no error)
- Unique constraint rejects duplicate (tenant, project, pr_number)

### A.2 — Preview Store (PostgreSQL)

**File:** `api/internal/db/preview_store.go`

```go
type PreviewStore interface {
    Create(ctx context.Context, tx db.QueryExecutor, p *PreviewRecord) error
    GetByID(ctx context.Context, tx db.QueryExecutor, id string) (*PreviewRecord, error)
    GetByPR(ctx context.Context, tx db.QueryExecutor, tenantID, projectName string, prNumber int) (*PreviewRecord, error)
    ListByProject(ctx context.Context, tx db.QueryExecutor, tenantID, projectName string) ([]PreviewRecord, error)
    ListActive(ctx context.Context, tx db.QueryExecutor, tenantID string) ([]PreviewRecord, error)
    CountActive(ctx context.Context, tx db.QueryExecutor, tenantID string) (int, error)
    UpdatePhase(ctx context.Context, tx db.QueryExecutor, id string, phase string) error
    UpdateBuild(ctx context.Context, tx db.QueryExecutor, id string, buildID, releaseID string) error
    UpdateURL(ctx context.Context, tx db.QueryExecutor, id string, url string) error
    MarkDeleted(ctx context.Context, tx db.QueryExecutor, id string) error
    ListExpired(ctx context.Context, tx db.QueryExecutor) ([]PreviewRecord, error)
}
```

**Tests:**
- CRUD operations work
- `CountActive` excludes DELETED and EXPIRED previews
- `ListExpired` returns only ACTIVE previews past `expires_at`
- Unique constraint violation returns typed error
- `GetByPR` returns nil when no preview exists for that PR

### A.3 — API Endpoints: Preview CRUD

**Files:** `api/internal/routes/previews.go`, `api/internal/services/preview_service.go`

| Method | Path | Description | Lock |
|--------|------|-------------|------|
| `POST` | `/api/v1/projects/:project/previews` | Create preview for a PR | `withEnvironmentLock` (tenant, project, `preview-pr{number}`) |
| `GET` | `/api/v1/projects/:project/previews` | List previews for project | No lock |
| `GET` | `/api/v1/projects/:project/previews/:id` | Get preview details | No lock |
| `DELETE` | `/api/v1/projects/:project/previews/:id` | Delete preview | `withEnvironmentLock` |

**POST request body:**
```json
{
  "pr_number": 42,
  "branch": "feat/new-feature"
}
```

**POST response (202 Accepted):**
```json
{
  "id": "prev_abc123",
  "pr_number": 42,
  "branch": "feat/new-feature",
  "phase": "CREATING",
  "expires_at": "2026-04-01T18:00:00Z",
  "url": null
}
```

**POST flow:**
1. Validate tenant is Active
2. Validate project exists
3. Acquire environment lock on `(tenant, project, preview-pr{number})`
4. Check `CountActive < tenant.Preview.MaxEnvironments`
5. Check no existing preview for this PR (idempotency: if exists and ACTIVE, return it)
6. Calculate `expires_at = now() + tenant.Preview.TTLHours`
7. Insert preview record (phase = CREATING)
8. Create PreviewEnvironment CR on mgmt cluster
9. Return 202

**Error responses:**
- 409: Preview already exists for this PR (and is not DELETED)
- 429: `MaxEnvironments` limit reached (`"Maximum preview environments reached (5/5). Delete an existing preview or wait for TTL expiry."`)
- 404: Project not found
- 4: Auth error

**Tests:**
- Happy path: create preview, verify 202 + record in DB + CR created
- Limit enforcement: create N previews, N+1 returns 429
- Idempotency: same PR number returns existing preview (not duplicate)
- Concurrent creation: two requests for same PR — one 202, one 409
- Delete then recreate: deleted preview allows new one for same PR
- Auth: unauthenticated returns 401

### A.4 — PreviewEnvironmentController

**File:** `operators/internal/controller/preview_controller.go`

The controller reconciles PreviewEnvironment CRs on the mgmt cluster and provisions resources on the runtime cluster. It follows the same multi-cluster pattern as EnvironmentController.

**Reconcile flow:**

```
1. Read PreviewEnvironment CR
2. Handle deletion (finalizer pattern — same as EnvironmentController)
3. If phase == "" or "Creating":
   a. Create namespace on runtime cluster (pkg/naming for deterministic name)
   b. Create RBAC (ServiceAccount, RoleBinding)
   c. Create ResourceQuota (50% of staging limits)
   d. Create LimitRange (same defaults)
   e. Create NetworkPolicy (same deny-all + allow-ingress as EnvironmentController)
   f. Copy secrets from staging namespace to preview namespace
   g. Transition: CREATING -> ACTIVE (via state machine)
   h. Update status.url with the preview URL
   i. Update status.createdAt
4. If phase == "Active":
   a. Check if ExpiresAt has passed -> transition to EXPIRING
   b. Requeue after (ExpiresAt - now) duration
5. If phase == "Expiring" or "Deleting":
   a. Delete namespace on runtime cluster
   b. Remove gitops manifests from Gitea
   c. Delete ArgoCD Application for this preview
   d. Remove CF KV entry (if static app)
   e. Transition: -> DELETED
   f. Remove finalizer
6. If phase == "CleanupFailed":
   a. Retry cleanup with exponential backoff
   b. After max retries -> GarbageCollected -> DELETED
```

**Key design decisions:**
- Controller does NOT trigger builds directly. The API `POST /previews` endpoint handles the build trigger (same as `loopzero deploy`). Controller only provisions infrastructure.
- TTL check is done in the reconcile loop with requeue. No separate CronJob needed.
- Secrets are copied from the staging environment namespace (Q4 decision).

**Tests (envtest):**
- Create CR -> namespace + RBAC + quota created on runtime
- Delete CR -> namespace deleted, finalizer removed
- TTL expired -> phase transitions to EXPIRING -> DELETED
- Idempotent reconcile: run twice, same result
- Crash recovery: restart mid-reconcile, resources are correct
- MaxEnvironments: controller does NOT enforce this (API enforces it pre-creation)
- State machine transitions: all transitions via `statemachine.NewPreviewMachine()`

### A.5 — Preview Build Integration

**File:** `api/internal/services/preview_build_service.go`

Preview builds reuse the existing `BuildOrchestrator` with preview-specific parameters:

| Parameter | Production Deploy | Preview Deploy |
|-----------|-------------------|----------------|
| Branch | `main` (or configured) | PR branch |
| Environment | `production`/`staging` | `preview-pr{number}` |
| Namespace | `{tenant}-{project}-{env}` | `preview-{tenant}-{project}-pr{number}` |
| GitOps path | `{project}/{env}/` | `{project}/preview-pr{number}/` |
| ArgoCD App | `{tenant}-{env}` (shared) | `{tenant}-preview-pr{number}` (dedicated) |
| TTL | None | `tenant.Preview.TTLHours` |

The build flow is identical to `loopzero deploy`:
1. Upload source bundle (same path)
2. Submit Argo Workflow (same CWTs — clone, cache-restore, build, cache-save, summary)
3. Push image to registry (same registry, different tag: `pr-{number}-{buildId}`)
4. Generate manifests (same renderer, different namespace + hostname)
5. Push to Gitea gitops repo (preview-specific path)
6. ArgoCD syncs (preview-specific Application)

**No new Argo Workflow templates needed.** The existing pipeline handles preview builds via parameters.

**Tests:**
- Preview build uses correct branch (not main)
- Preview manifests target correct namespace
- Preview gitops path is isolated from production
- Image tag includes PR number
- ArgoCD Application is preview-specific (not the shared tenant app)

### A.6 — CLI: `loopzero preview` Commands

**File:** `cli/src/commands/preview.ts`

#### `loopzero preview list`

```
loopzero preview list [--json]
```

Lists active previews for the current project (from `loopzero.yaml`).

**Human output:**
```
Previews for api-server (2 active)

  PR    BRANCH              STATUS    URL                                          EXPIRES
  #42   feat/new-feature    Active    pr-42.api-server.acme.dev.loopzero.example       in 18h
  #37   fix/login-bug       Active    pr-37.api-server.acme.dev.loopzero.example       in 4h
```

**JSON output:**
```json
{
  "project": "api-server",
  "previews": [
    {
      "id": "prev_abc123",
      "pr_number": 42,
      "branch": "feat/new-feature",
      "phase": "ACTIVE",
      "url": "https://pr-42.api-server.acme.dev.loopzero.example",
      "expires_at": "2026-04-01T18:00:00Z",
      "created_at": "2026-04-01T00:00:00Z"
    }
  ]
}
```

**Exit codes:** 0 (success), 4 (auth error)

#### `loopzero preview create`

```
loopzero preview create --pr <number> [--branch <branch>] [--json]
```

Creates a preview environment for the given PR. If `--branch` is omitted, the CLI queries the GitHub API for the PR's head branch.

**Human output:**
```
Creating preview for PR #42 (branch: feat/new-feature)...

  Preview created
  URL:     https://pr-42.api-server.acme.dev.loopzero.example (deploying)
  TTL:     24 hours
  Status:  Creating

  Run loopzero preview inspect prev_abc123 to check status.
```

**Exit codes:** 0 (success), 1 (bad args — missing --pr), 3 (deploy failure), 4 (auth error)

#### `loopzero preview delete`

```
loopzero preview delete <preview-id> [--json]
```

Deletes a preview environment. Accepts preview ID or `--pr <number>`.

```
loopzero preview delete --pr 42 [--json]
```

**Human output:**
```
Deleting preview for PR #42...

  Preview deleted. Namespace cleanup in progress.
```

**Exit codes:** 0 (success), 1 (bad args), 4 (auth error)

#### `loopzero preview inspect`

```
loopzero preview inspect <preview-id> [--json]
loopzero preview inspect --pr 42 [--json]
```

Shows detailed preview status.

**Human output:**
```
Preview: PR #42 — feat/new-feature

  Phase:      Active
  URL:        https://pr-42.api-server.acme.dev.loopzero.example
  Created:    2026-04-01 00:00:00 UTC
  Expires:    2026-04-01 18:00:00 UTC (in 18h)
  Build:      bld_abc123 (succeeded)
  Release:    rel_def456 (active)

  Apps:
    api-server   Running   1/1 pods
    web-app      Running   1/1 pods
```

**JSON output:**
```json
{
  "id": "prev_abc123",
  "pr_number": 42,
  "branch": "feat/new-feature",
  "phase": "ACTIVE",
  "url": "https://pr-42.api-server.acme.dev.loopzero.example",
  "created_at": "2026-04-01T00:00:00Z",
  "expires_at": "2026-04-01T18:00:00Z",
  "build_id": "bld_abc123",
  "release_id": "rel_def456",
  "apps": [
    { "name": "api-server", "status": "Running", "ready_pods": 1, "total_pods": 1 },
    { "name": "web-app", "status": "Running", "ready_pods": 1, "total_pods": 1 }
  ]
}
```

**Exit codes:** 0 (success), 1 (bad args), 4 (auth error)

**Tests (all commands):**
- `--json` output matches schema
- Exit codes for all error paths
- No internal terms in any output string
- CI mode: no spinners, structured output
- `--pr` and positional ID both work for delete/inspect

### A.7 — Gitops Cleanup on Preview Delete

**File:** `api/internal/gitops/preview.go`

When a preview is deleted, the gitops path must be cleaned up:

1. Remove all files under `{project}/preview-pr{number}/` in the Gitea gitops repo
2. Delete the ArgoCD Application CR for `{tenant}-preview-pr{number}`
3. Remove CF KV entry if static app

This follows the same pattern as `gitops.Remove` in project delete. **`gitops.Remove` failure is FATAL** (existing rule — orphaned manifests block all deploys via ArgoCD).

**Tests:**
- Gitops path is removed on delete
- ArgoCD Application is deleted
- Failure to remove gitops path fails the delete operation (non-silent)

---

## Phase B: GitHub Webhook Integration (0.5 sprint)

> Auto-create/update/delete previews based on GitHub PR events.
> Depends on Phase A being complete.

### B.1 — GitHub Webhook Receiver

**File:** `api/internal/routes/github_webhook.go`

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/webhooks/github` | `POST` | Receives GitHub webhook events |

**Webhook events handled:**

| Event | Action | Result |
|-------|--------|--------|
| `pull_request` | `opened` | Create preview (same as POST /previews) |
| `pull_request` | `synchronize` | Rebuild preview (new push to PR branch) |
| `pull_request` | `closed` | Delete preview (PR merged or closed) |
| `pull_request` | `reopened` | Create preview (if not exists) |

**Security:**
- Validate `X-Hub-Signature-256` header against webhook secret (HMAC-SHA256)
- Reject events from unknown repositories (must match a registered project)
- Rate limit: 10 events/second per tenant

**Webhook secret management:**
- Per-tenant webhook secret stored in `tenant_webhooks` table (or in Tenant CR annotation)
- Secret generated on `loopzero init` or `loopzero preview enable`
- Displayed once to the user for GitHub webhook configuration

**Flow for `pull_request.opened`:**
1. Validate signature
2. Extract tenant + project from repository mapping
3. Delegate to `PreviewService.Create()` (same code path as CLI)
4. Return 200 immediately (async processing)

**Flow for `pull_request.synchronize`:**
1. Validate signature
2. Find existing preview for this PR
3. Trigger rebuild (submit new Argo Workflow for the updated branch SHA)
4. Update preview record with new build_id
5. Return 200

**Flow for `pull_request.closed`:**
1. Validate signature
2. Find existing preview for this PR
3. Trigger deletion (same as DELETE /previews/:id)
4. Return 200

**Tests:**
- Valid signature → 200
- Invalid signature → 401
- Unknown repository → 404
- `opened` → preview created
- `synchronize` → rebuild triggered
- `closed` → preview deleted
- `reopened` → preview recreated
- Idempotent: duplicate events do not create duplicate previews
- Rate limit: >10 events/second → 429

### B.2 — Repository-to-Project Mapping

**File:** `api/internal/db/repository_mapping_store.go`

New table to map GitHub repositories to LoopZero projects:

```sql
CREATE TABLE repository_mappings (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL,
    project_name    TEXT NOT NULL,
    github_owner    TEXT NOT NULL,
    github_repo     TEXT NOT NULL,
    webhook_secret  TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (github_owner, github_repo)
);
```

This mapping is created when the user runs `loopzero preview enable` (or `loopzero init` with preview flag).

**Tests:**
- Lookup by (github_owner, github_repo) returns correct tenant + project
- Unknown repo returns nil
- Unique constraint prevents duplicate mappings

### B.3 — CLI: `loopzero preview enable`

```
loopzero preview enable [--json]
```

Enables automatic preview environments for the current project. This:
1. Generates a webhook secret
2. Creates repository mapping in DB
3. Displays the webhook URL and secret for GitHub configuration

**Human output:**
```
Preview environments enabled for api-server.

  Webhook URL:    https://api.dev.loopzero.example/api/v1/webhooks/github
  Webhook Secret: REDACTED_SECRET_057

  Add this webhook to your GitHub repository settings:
  1. Go to https://github.com/acme/api-server/settings/hooks/new
  2. Payload URL: https://api.dev.loopzero.example/api/v1/webhooks/github
  3. Content type: application/json
  4. Secret: REDACTED_SECRET_057
  5. Events: Pull requests

  Previews will be created automatically for every PR.
```

**Future improvement:** Use GitHub App installation to auto-configure the webhook (no manual step). This is Phase D scope.

---

## Phase C: GitHub PR Comment + TTL Cleanup (0.5 sprint)

> Post preview URL as PR comment. Implement robust TTL expiry.
> Depends on Phase A and Phase B.

### C.1 — GitHub PR Comment Integration

**File:** `api/internal/github/pr_comment.go`

When a preview reaches ACTIVE phase, post a comment on the PR:

```markdown
## Preview Environment

| App | URL | Status |
|-----|-----|--------|
| api-server | [pr-42.api-server.acme.dev.loopzero.example](https://pr-42.api-server.acme.dev.loopzero.example) | Running |
| web-app | [pr-42.web-app.acme.dev.loopzero.example](https://pr-42.web-app.acme.dev.loopzero.example) | Running |

**Expires:** 2026-04-01 18:00 UTC (in 24h)

---
<sub>Deployed by LoopZero</sub>
```

On rebuild (`synchronize`), edit the existing comment (don't create a new one). Find the comment by a hidden marker: `<!-- loopzero-preview-{previewId} -->`.

**GitHub API requirements:**
- Token needs `pull_requests:write` scope
- Use the installation token from GitHub App (or the user's token if no App)

**Tests:**
- Comment is created on preview activation
- Comment is updated on rebuild (not duplicated)
- Comment includes correct URLs for all apps
- Comment is NOT deleted when preview is deleted (serves as historical record)

### C.2 — TTL Enforcement (Controller-Based)

TTL is already handled by the controller (A.4) via requeue:

```go
// In reconcile, when phase == Active:
if preview.Spec.ExpiresAt != nil && time.Now().After(preview.Spec.ExpiresAt.Time) {
    // Transition to Expiring
    newPhase := previewSM.Transition(PreviewActive, PreviewExpirationTriggered)
    preview.Status.Phase = newPhase
    return ctrl.Result{}, r.Status().Update(ctx, &preview)
}
// Requeue at expiry time
return ctrl.Result{RequeueAfter: time.Until(preview.Spec.ExpiresAt.Time)}, nil
```

Additionally, add a safety net: a `PreviewReaper` background service in the API (same pattern as `BuildReaper` and `ReleaseReaper`):

**File:** `api/internal/services/preview_reaper.go`

Runs every 5 minutes. Finds previews where:
- `phase = 'ACTIVE'` AND `expires_at < now()` (controller missed TTL — crash, partition)
- `phase = 'CREATING'` AND `created_at < now() - 30min` (stuck creation)
- `phase = 'CLEANUP_FAILED'` AND `updated_at < now() - 1h` (stuck cleanup)

For each: trigger deletion via API (not direct DB update — go through the state machine).

**Tests:**
- Reaper finds expired previews and triggers deletion
- Reaper finds stuck CREATING previews and marks as FAILED
- Reaper finds stuck CLEANUP_FAILED and retries garbage collection
- Reaper is idempotent (safe to run overlapping)

### C.3 — TTL Extension

```
loopzero preview extend --pr 42 [--hours 24] [--json]
```

Extends the TTL of an active preview. Default extension: same as original TTL (tenant.Preview.TTLHours).

**API:** `PATCH /api/v1/projects/:project/previews/:id` with body `{ "extend_hours": 24 }`

**Tests:**
- Extension updates `expires_at` in DB and CR
- Cannot extend DELETED or EXPIRING preview
- Cannot extend beyond max allowed (2x original TTL as safety limit)

---

## Phase D: Static Frontend Previews + GitHub App (0.5 sprint)

> Support static frontends via DO Spaces + CF Worker.
> GitHub App for zero-config webhook setup.
> Depends on Phases A, B, C.

### D.1 — Static Preview Delivery

For projects with `render` config in `loopzero.yaml` (static frontends):

1. Build output uploaded to DO Spaces: `previews/{tenant}/{project}/pr-{number}/{buildId}/`
2. CF KV entry: key = `pr-{number}.{project}.{tenant}`, value = `{ "buildId": "...", "s3Prefix": "previews/..." }`
3. CF Worker routes requests to DO Spaces (same as production static delivery)

On preview delete:
- Delete S3 objects under the preview prefix
- Delete CF KV entry

**Tests:**
- Static preview build uploads to correct S3 prefix
- CF KV entry is created with correct routing
- Cleanup removes S3 objects and KV entry

### D.2 — GitHub App (Future)

Replace manual webhook configuration with a GitHub App installation flow:

1. `loopzero preview enable` opens browser to install LoopZero GitHub App
2. App installation grants webhook events + PR comment permissions
3. API receives installation event, auto-creates repository mapping

This eliminates the manual webhook setup step from Phase B.

**NOT in scope for initial implementation.** Document as v2 feature.

---

## Implementation Sequence

```
Phase A (1 sprint, parallelizable)
├── A.1 DB migration          (standalone, do first)
├── A.2 Preview store         (depends on A.1)
├── A.3 API endpoints         (depends on A.2)
├── A.4 Controller            (standalone, parallel with A.2/A.3)
├── A.5 Build integration     (depends on A.3 + A.4)
├── A.6 CLI commands          (depends on A.3)
└── A.7 Gitops cleanup        (depends on A.4)

Phase B (0.5 sprint, after Phase A)
├── B.1 Webhook receiver      (depends on A.3)
├── B.2 Repository mapping    (depends on B.1)
└── B.3 CLI enable command    (depends on B.2)

Phase C (0.5 sprint, after Phase B)
├── C.1 PR comment            (depends on B.1)
├── C.2 TTL reaper            (depends on A.4)
└── C.3 TTL extension         (depends on A.3)

Phase D (0.5 sprint, after Phase C)
├── D.1 Static previews       (depends on A.5)
└── D.2 GitHub App             (v2 — document only)
```

**Total estimated effort:** 2.5 sprints

---

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Namespace explosion (many PRs) | Medium | High — cluster resource exhaustion | `MaxEnvironments` limit per tenant + TTL enforcement + PreviewReaper |
| Orphaned namespaces (controller crash during cleanup) | Medium | Medium — resources leak | Finalizer pattern + CleanupFailed state + GarbageCollected transition + Reaper safety net |
| Webhook replay (GitHub retries) | High | Low — duplicate creates | Idempotency: `UNIQUE (tenant_id, project_name, pr_number)` + return existing preview |
| DNS collision (PR number reuse across projects) | Low | High — wrong app served | Namespace name includes tenant + project + PR number — globally unique |
| Secret leakage (staging secrets in preview namespace) | Medium | Medium — non-production secrets exposed | Staging secrets only (Q4 decision). Preview namespaces have same NetworkPolicy isolation as staging. |
| ArgoCD Application sprawl (one per preview) | Medium | Medium — ArgoCD performance | Cleanup on delete. Reaper catches orphans. Monitor ArgoCD Application count metric. |
| Build queue congestion (many PR pushes) | Medium | Medium — slow previews | Preview builds use lower priority than production deploys. `MaxBuildConcurrency` applies. |
| TTL clock skew (controller vs DB) | Low | Low — preview lives slightly longer/shorter | Both controller and reaper check TTL. Safety net catches misses. |

---

## Exit Gates

### Phase A Complete When:
- [ ] `loopzero preview create --pr 42` creates a working preview with accessible URL
- [ ] `loopzero preview list` shows active previews with TTL
- [ ] `loopzero preview delete --pr 42` destroys preview and cleans up namespace
- [ ] `loopzero preview inspect --pr 42` shows detailed status
- [ ] PreviewEnvironmentController creates namespace, RBAC, quota, NetworkPolicy on runtime cluster
- [ ] TTL expiry transitions preview to DELETED and cleans up resources
- [ ] Build pipeline works for preview branch (not just main)
- [ ] Gitops cleanup removes preview manifests on delete
- [ ] All CLI commands have `--json` output matching schemas above
- [ ] All exit codes are correct (0, 1, 3, 4 only)
- [ ] No internal terms in any CLI output
- [ ] State machine transitions match `pkg/statemachine/preview.go`
- [ ] `task test:unit` passes
- [ ] `task lint` passes
- [ ] `task validate:manifests` passes

### Phase B Complete When:
- [ ] GitHub webhook creates preview on `pull_request.opened`
- [ ] GitHub webhook rebuilds preview on `pull_request.synchronize`
- [ ] GitHub webhook deletes preview on `pull_request.closed`
- [ ] Webhook signature validation rejects invalid requests
- [ ] `loopzero preview enable` outputs webhook URL and secret
- [ ] Duplicate webhook events are handled idempotently

### Phase C Complete When:
- [ ] PR comment posted with preview URL when preview becomes ACTIVE
- [ ] PR comment updated (not duplicated) on rebuild
- [ ] PreviewReaper catches expired/stuck previews
- [ ] `loopzero preview extend --pr 42` extends TTL
- [ ] TTL extension updates both DB record and CR

### Phase D Complete When:
- [ ] Static frontend previews serve via CF Worker + DO Spaces
- [ ] Static preview cleanup removes S3 objects and CF KV entry
- [ ] GitHub App design documented (implementation deferred to v2)

---

## Files Touched (Summary)

### New Files

| File | Phase | Description |
|------|-------|-------------|
| `api/internal/db/migrations/NNNN_create_preview_environments.up.sql` | A | Preview environments table |
| `api/internal/db/preview_store.go` | A | Preview DB operations |
| `api/internal/routes/previews.go` | A | Preview API endpoints |
| `api/internal/services/preview_service.go` | A | Preview business logic |
| `api/internal/services/preview_build_service.go` | A | Preview build orchestration |
| `api/internal/services/preview_reaper.go` | C | TTL enforcement safety net |
| `api/internal/gitops/preview.go` | A | Preview gitops cleanup |
| `operators/internal/controller/preview_controller.go` | A | PreviewEnvironment reconciler |
| `operators/internal/controller/preview_controller_test.go` | A | Controller tests (envtest) |
| `cli/src/commands/preview.ts` | A | CLI preview commands |
| `cli/tests/unit/commands/preview.test.ts` | A | CLI tests |
| `api/internal/routes/github_webhook.go` | B | GitHub webhook receiver |
| `api/internal/db/migrations/NNNN_create_repository_mappings.up.sql` | B | Repository mapping table |
| `api/internal/db/repository_mapping_store.go` | B | Repository mapping DB operations |
| `api/internal/github/pr_comment.go` | C | GitHub PR comment integration |

### Modified Files

| File | Phase | Change |
|------|-------|--------|
| `api/cmd/main.go` | A | Wire preview routes, controller, reaper |
| `api/internal/routes/router.go` (or equivalent) | A | Mount preview endpoints |
| `operators/cmd/main.go` | A | Register PreviewEnvironmentController |
| `cli/src/commands/index.ts` (or equivalent) | A | Register preview command group |
| `api/internal/manifests/renderer.go` | A | Preview-specific hostname in generated manifests |
| `api/internal/gitops/pusher.go` | A | Preview-specific gitops path |
| `api/internal/config/config.go` | A | Preview-related config fields |
| `CHANGELOG.md` | All | Document preview feature additions |

---

## Meeting Agenda (Pre-Implementation)

Before any code is written, convene a `/meeting` with these agents:

- **Principal Architect** — validate 4-phase decomposition, namespace naming, DNS routing
- **K8s Controller Engineer** — validate controller design, multi-cluster namespace creation, finalizer pattern
- **API/Domain Architect** — validate API endpoints, lock strategy, idempotency
- **Platform Security Engineer** — validate secret inheritance (Q4), webhook signature, preview isolation
- **SRE/Production Engineer** — validate TTL enforcement, reaper design, resource limits
- **DX/Tooling Engineer** — validate CLI UX, PR comment format, `loopzero preview enable` flow
- **GitOps/Delivery Engineer** — validate gitops path isolation, ArgoCD Application per preview

**Decisions required:**
1. Confirm Q1-Q6 recommendations
2. Approve DNS pattern: `pr-{number}.{project}.{tenant}.{baseDomain}`
3. Approve namespace naming: `preview-{tenant}-{project}-pr{number}`
4. Approve 4-phase implementation sequence
5. Confirm preview builds use same Argo Workflow pipeline (no new CWTs)
6. Confirm controller owns TTL (no separate CronJob)
