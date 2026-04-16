# ADR 0006 — Implementation Plan (v2)

> Replaces the original plan after Staff review. Key changes: invariant-first approach,
> observability before removal, phased rollout with explicit gates, cluster cleanup procedure.

---

## Principles

1. **Prove absence of dependency before removing code** — tests first, deletion second
2. **Replace observability before removing surfaces** — no `kubectl get X` without `loopzero X` substitute
3. **Fail loud at ownership boundaries** — silent errors at PG↔K8s boundary are bugs
4. **One gate per phase** — phase N+1 does not start until phase N gate passes

---

## Phase 0: Architecture Guardrails

**Goal:** Prove that Build and Release lifecycles work without LoopZeroBuild/LoopZeroRelease CRDs.

### 0.1 Add invariant tests

Write integration tests that prove:

```
test_build_lifecycle_without_loopzerobuild_crd:
  - DO NOT install loopzero.io_loopzerobuilds.yaml CRD
  - Create build via API (QUEUED)
  - Submit workflow (QUEUED → RUNNING)
  - Complete workflow (RUNNING → SUCCEEDED)
  - Assert: build state correct in PostgreSQL
  - Assert: no LoopZeroBuild CR was created or consulted

test_release_lifecycle_without_loopzerorelease_crd:
  - DO NOT install loopzero.io_loopzeroreleases.yaml CRD
  - Create release via API (READY)
  - Push to GitOps (READY → PENDING)
  - Confirm ArgoCD sync (PENDING → ACTIVE)
  - Assert: release state correct in PostgreSQL
  - Assert: environment_targets updated
  - Assert: no LoopZeroRelease CR was created or consulted

test_operator_boots_without_build_release_watches:
  - Remove BuildReconciler and ReleaseReconciler from manager registration
  - Assert: operator binary starts cleanly
  - Assert: no watch errors for LoopZeroBuild/LoopZeroRelease GVKs
  - Assert: Tenant/Environment reconciliation still works
```

### 0.2 Exhaustive reference scan

Scan ALL codebases and artifacts for references, not just Go source:

```bash
# Go source (operators, api, pkg)
grep -rn "LoopZeroBuild\|LoopZeroRelease\|loopzerobuilds\|loopzeroreleases" operators/ api/ pkg/

# CLI source
grep -rn "LoopZeroBuild\|LoopZeroRelease\|loopzerobuilds\|loopzeroreleases" cli/

# Helm charts, templates, values
grep -rn "LoopZeroBuild\|LoopZeroRelease\|loopzerobuilds\|loopzeroreleases" infra/helm/

# Scripts, Taskfile, Tiltfile
grep -rn "LoopZeroBuild\|LoopZeroRelease\|loopzerobuilds\|loopzeroreleases" infra/scripts/ Taskfile.yml Tiltfile

# Docs, runbooks, dashboards
grep -rn "LoopZeroBuild\|LoopZeroRelease\|loopzerobuilds\|loopzeroreleases" docs/

# GVK references (Group/Version/Kind strings)
grep -rn "loopzero.io/v1.*Build\|loopzero.io/v1.*Release" operators/ api/ infra/

# RBAC rules that only exist for these resources
grep -rn "loopzerobuilds\|loopzeroreleases" operators/config/ infra/helm/
```

Document every reference found and classify as: code (must remove), docs (must update), test (must remove/update).

### Gate 0
- [ ] Invariant tests pass (build/release lifecycle without CRDs)
- [ ] Operator boots without build/release reconcilers
- [ ] Complete reference inventory documented
- [ ] No unknown dependencies found

---

## Phase 1: Logical Deprecation

**Goal:** Block new usage before physical removal.

### 1.1 Disable controller registration

In `operators/cmd/main.go`, comment out (not delete) BuildReconciler and ReleaseReconciler setup with `// DEPRECATED: ADR-0006` marker. This makes the operator run without these controllers while keeping code compilable for rollback.

### 1.2 Add build-time guard

Add a Go build tag or linter check that fails if any NEW code imports `LoopZeroBuild` or `LoopZeroRelease` types outside of test files marked for cleanup.

### 1.3 Verify in dev environment

Deploy operator with disabled controllers to dev cluster:
- Confirm existing Tenant/Environment reconciliation works
- Confirm no error logs about missing watches
- Run `task test:smoke` — full deploy flow works

### Gate 1
- [ ] Operator deployed without Build/Release controllers in dev
- [ ] `task test:smoke` passes
- [ ] No regression in Tenant/Environment reconciliation
- [ ] Zero errors in operator logs related to disabled controllers

---

## Phase 2: Observability Replacement

**Goal:** Ensure operational visibility exists before removing CRD surfaces.

### 2.1 Verify Build observability via API

Confirm these endpoints exist and return sufficient operational data:

| Endpoint | Required fields |
|----------|----------------|
| `GET /api/v1/builds/:id` | id, status, workflow_name, image_digest, started_at, finished_at, error_message, tenant_id, project_id, environment |
| `GET /api/v1/builds/:id/stream` | SSE events with build progress |
| `POST /api/v1/builds/:id/cancel` | Cancellation with status update |

### 2.2 Verify Release observability via API

Confirm these endpoints exist and return sufficient operational data:

| Endpoint | Required fields |
|----------|----------------|
| `GET /api/v1/releases/:id` (if exists) | id, status, git_commit, environment, activated_at |
| Build completion SSE events | release_id, release_status, environment |

If `GET /api/v1/releases/:id` does not exist, create it. This replaces `kubectl get loopzeroreleases`.

### 2.3 Verify CLI observability

Confirm these CLI commands provide equivalent information:

| CLI command | Replaces |
|-------------|----------|
| `loopzero deploy` (with SSE streaming) | `kubectl get loopzerobuilds -w` |
| `loopzero status` (if exists) | `kubectl get loopzeroreleases` |

If `loopzero status` does not exist yet, document as known gap (Phase 4 will address with LoopZeroApplication CRD).

### 2.4 Verify structured logging

Confirm that API logs include these fields on every build/release operation:
- `build_id`
- `release_id`
- `workflow_name`
- `tenant_id`
- `project_id`
- `environment`
- `correlation_id`
- `status_transition` (e.g., "QUEUED→RUNNING")

### Gate 2
- [ ] Build status queryable via API with all required fields
- [ ] Release status queryable via API (or gap documented)
- [ ] CLI provides real-time build progress
- [ ] Structured logs include all correlation fields
- [ ] Operational runbook updated to use API/CLI instead of kubectl for build/release

---

## Phase 3: Physical Removal

**Goal:** Delete code, types, controllers, CRDs, RBAC.

### 3.1 Remove LoopZeroBuild

```
DELETE: operators/api/v1/build_types.go
DELETE: operators/internal/controller/build_controller.go
DELETE: operators/internal/controller/build_controller_test.go
DELETE: config/crd/bases/loopzero.io_loopzerobuilds.yaml
UPDATE: operators/cmd/main.go — remove commented-out BuildReconciler setup
UPDATE: operators/internal/controller/conditions/types.go — remove Build condition constants
         (WorkflowSubmitted, SourcesResolved, BuildSucceeded, ArtifactsPublished)
REGENERATE: operators/api/v1/zz_generated.deepcopy.go
UPDATE: Any RBAC ClusterRole that grants access to loopzerobuilds resource
```

### 3.2 Remove LoopZeroRelease

```
DELETE: operators/api/v1/release_types.go
DELETE: operators/internal/controller/release_controller.go
DELETE: operators/internal/controller/release_controller_test.go
DELETE: config/crd/bases/loopzero.io_loopzeroreleases.yaml
UPDATE: operators/cmd/main.go — remove commented-out ReleaseReconciler setup
UPDATE: operators/internal/controller/conditions/types.go — remove Release condition constants
         (GitMirrorCompleted, SyncCompleted, ReleaseActive)
REGENERATE: operators/api/v1/zz_generated.deepcopy.go
UPDATE: Any RBAC ClusterRole that grants access to loopzeroreleases resource
```

### 3.3 Clean up all references

Address every reference found in Phase 0.2 scan:
- Remove imports
- Remove test fixtures that create LoopZeroBuild/LoopZeroRelease objects
- Update docs that mention these CRDs
- Remove Helm template references (if any)
- Remove Taskfile/Tiltfile references (if any)
- Update `CLAUDE.md` repository structure section

### 3.4 Verify clean compilation

```bash
cd operators && go build ./...
cd operators && go test ./...
cd operators && go vet ./...
cd api && go build ./...
cd api && go test ./...
task lint
task validate:manifests
```

### Gate 3
- [ ] `go build ./...` passes in operators/ and api/
- [ ] `go test ./...` passes in operators/ and api/
- [ ] `task lint` passes
- [ ] `task validate:manifests` passes
- [ ] Zero grep results for LoopZeroBuild/LoopZeroRelease across entire repo
- [ ] RBAC cleaned (no permissions for loopzerobuilds/loopzeroreleases resources)

---

## Phase 4: Cluster Cleanup

**Goal:** Remove CRDs from clusters without leaving orphans.

### 4.1 Inventory existing instances

```bash
# Check all clusters for existing instances
kubectl get loopzerobuilds --all-namespaces --context k3d-loopzero-mgmt 2>/dev/null || echo "CRD not installed"
kubectl get loopzeroreleases --all-namespaces --context k3d-loopzero-mgmt 2>/dev/null || echo "CRD not installed"
```

### 4.2 Delete instances if they exist

If any LoopZeroBuild or LoopZeroRelease CRs exist (from testing):

```bash
# Remove finalizers first (if any)
kubectl get loopzerobuilds --all-namespaces -o json --context k3d-loopzero-mgmt | \
  jq '.items[] | select(.metadata.finalizers != null)' | \
  # For each: kubectl patch to remove finalizers, then delete

# Delete instances
kubectl delete loopzerobuilds --all --all-namespaces --context k3d-loopzero-mgmt
kubectl delete loopzeroreleases --all --all-namespaces --context k3d-loopzero-mgmt
```

### 4.3 Deploy updated operator

```bash
# Deploy operator without Build/Release controllers
tilt up  # or equivalent deployment
# Verify operator starts cleanly
kubectl logs -l app=loopzero-operator --context k3d-loopzero-mgmt --tail=50
```

### 4.4 Remove CRDs from cluster

```bash
# Only after operator is running WITHOUT watches for these types
kubectl delete crd loopzerobuilds.loopzero.io --context k3d-loopzero-mgmt 2>/dev/null || true
kubectl delete crd loopzeroreleases.loopzero.io --context k3d-loopzero-mgmt 2>/dev/null || true
```

### 4.5 Verify clean cluster state

```bash
# Confirm CRDs removed from API discovery
kubectl api-resources --api-group=loopzero.io --context k3d-loopzero-mgmt
# Expected: loopzerobuilds and loopzeroreleases NOT in list

# Confirm no watch errors in operator
kubectl logs -l app=loopzero-operator --context k3d-loopzero-mgmt --since=2m | grep -i "error\|panic\|watch"
# Expected: no errors related to removed types

# Confirm remaining CRDs still work
kubectl get tenants --all-namespaces --context k3d-loopzero-mgmt
kubectl get environments --all-namespaces --context k3d-loopzero-mgmt
```

### Gate 4
- [ ] Zero instances of LoopZeroBuild/LoopZeroRelease in any cluster
- [ ] CRDs removed from API discovery
- [ ] Operator running cleanly without watch errors
- [ ] Remaining CRDs (Tenant, Environment, etc.) reconciling normally
- [ ] `task test:smoke` passes end-to-end

---

## Phase 5: Fix Tenant Boundary Creation

**Goal:** Eliminate silent failure at PG↔K8s ownership boundary.

### 5.1 Make EnsureTenant fail-loud

**File:** `api/internal/routes/projects.go`

```go
// BEFORE (silent failure — BUG per ADR-0006 Invariant D)
if err := h.crMgr.EnsureTenant(...); err != nil {
    slog.Error("failed to ensure tenant CR", ...)
}

// AFTER (fail-loud)
if err := h.crMgr.EnsureTenant(...); err != nil {
    slog.Error("failed to ensure tenant CR", "tenant_id", uc.TenantID, "error", err)
    respondError(w, loopzeroerrors.ErrPlatformDegraded.Wrap(
        fmt.Sprintf("tenant infrastructure provisioning failed for %s", uc.TenantID),
    ))
    return
}
```

### 5.2 Add retry with error classification

**File:** `api/internal/k8s/tenant_cr_manager.go`

```go
func (m *TenantCRManager) EnsureTenant(ctx context.Context, tenantID string, spec TenantCRSpec) error {
    var lastErr error
    for attempt := 0; attempt < 3; attempt++ {
        if attempt > 0 {
            time.Sleep(time.Duration(attempt) * 500 * time.Millisecond) // 500ms, 1s
        }
        err := m.ensureTenantOnce(ctx, tenantID, spec)
        if err == nil {
            return nil
        }
        lastErr = err
        // Don't retry validation errors or permission errors
        if isNonRetryable(err) {
            return err
        }
    }
    return fmt.Errorf("EnsureTenant failed after 3 attempts: %w", lastErr)
}
```

Error classification:
- **Retryable:** API server unavailable (503), connection timeout, conflict (409)
- **Non-retryable:** forbidden (403), validation error (422), invalid spec

### 5.3 Ensure idempotency

`EnsureTenant` must be safe for retry even if:
- CR was created but status hasn't converged
- Previous request failed after create but before response
- Two concurrent requests for the same tenant

Implementation: use `retry.RetryOnConflict()` wrapping `CreateOrUpdate()` — already the pattern used by TenantReconciler.

### 5.4 Write tests

```
test_ensure_tenant_returns_503_on_k8s_failure:
  - Mock K8s client to return error
  - Call EnsureTenant
  - Assert: returns error (not nil)
  - Assert: API returns HTTP 503

test_ensure_tenant_retries_on_transient_failure:
  - Mock K8s client: fail first 2 calls, succeed on 3rd
  - Call EnsureTenant
  - Assert: succeeds after retry

test_ensure_tenant_does_not_retry_validation_error:
  - Mock K8s client: return 422
  - Call EnsureTenant
  - Assert: fails immediately (1 attempt, not 3)

test_ensure_tenant_idempotent_on_existing_cr:
  - Pre-create Tenant CR
  - Call EnsureTenant with same spec
  - Assert: no error, no update needed

test_deploy_fails_without_tenant_infra:
  - Create tenant in PG only (no CRD)
  - Attempt deploy
  - Assert: HTTP 503 (not silent success)
```

### Gate 5
- [ ] EnsureTenant failure returns HTTP 503
- [ ] Retries 3x on transient errors
- [ ] Does not retry on validation/permission errors
- [ ] Idempotent on existing CR
- [ ] Deploy blocked when Tenant CRD missing
- [ ] All 5 tests pass

---

## Phase 6: Single-Replica Contract & Documentation

**Goal:** Document constraints and prepare for v2 worker extraction.

### 6.1 Pin API replica count

Add comment and constraint to API deployment manifests:

```yaml
# infra/helm/loopzero-api (or Tiltfile equivalent)
replicaCount: 1
# ADR-0006: DO NOT increase without extracting background loops to loopzero-worker.
# Background loops (BuildReconcileLoop, BuildReaper, ReleaseReaper, IdempotencyCleaner,
# EventPruner) are not coordinated across replicas. Running 2+ replicas causes
# duplicate processing of builds and releases.
```

### 6.2 Update spec

In `docs/technical.md`, mark the emergency fallback path section:

```
> **DEFERRED (v2):** The emergency fallback path (kubectl apply --server-side) described
> below is not implemented in v1. The single delivery path is: Git → ArgoCD → workload.
> If Gitea is unavailable, deploys fail with GIT_MIRROR_FAILED and users retry.
```

### 6.3 Update CLAUDE.md

Remove LoopZeroBuild and LoopZeroRelease from:
- Repository Structure section (no longer in operators/api/v1/)
- Stack section (if mentioned)
- Sprint references (if affected)

### 6.4 Update CHANGELOG.md

```markdown
## [Unreleased]

### Removed
- LoopZeroBuild CRD and BuildReconciler — build state owned exclusively by PostgreSQL (ADR-0006)
- LoopZeroRelease CRD and ReleaseReconciler — release state owned exclusively by PostgreSQL (ADR-0006)

### Fixed
- Tenant CRD creation now fails loudly (HTTP 503) instead of silent log — prevents deploys without infrastructure (ADR-0006)

### Changed
- Emergency fallback deploy path marked as deferred to v2 — single delivery path: Git → ArgoCD (ADR-0006)
```

### Gate 6
- [ ] API deployment pinned to 1 replica with documented reason
- [ ] Spec updated with deferred fallback note
- [ ] CLAUDE.md updated (no references to removed CRDs)
- [ ] CHANGELOG.md updated
- [ ] ADR-0006 status changed from "Proposed" to "Accepted"

---

## Final Acceptance Gate

**Definition of Done:** Build and Release have a single owner (PostgreSQL), zero residual dependency on removed CRDs, and the system remains operable end-to-end.

- [ ] **Gate A — Ownership:** No build/release state outside PostgreSQL is treated as authority
- [ ] **Gate B — Runtime:** Full deploy lifecycle works without LoopZeroBuild/LoopZeroRelease CRDs installed
- [ ] **Gate C — Operators:** Operator boots cleanly, no residual watches for removed types
- [ ] **Gate D — Operability:** CLI/API provide sufficient status for build/release debugging
- [ ] **Gate E — Failure handling:** EnsureTenant fails visibly, retries are idempotent, deploy without Tenant infra is blocked
- [ ] **Gate F — Rollout safety:** Validated in dev environment, `task test:smoke` passes full deploy flow (create project → build → release → sync → health check)
- [ ] **Gate G — Clean codebase:** Zero grep results for LoopZeroBuild/LoopZeroRelease anywhere in the repository
