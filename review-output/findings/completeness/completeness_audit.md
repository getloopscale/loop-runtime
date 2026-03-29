# Completeness Audit

**Date:** 2026-03-29
**Target:** /home/paulo/loopzero
**Reviewer:** completeness-auditor

---

## Executive Summary

- Features promised: 15 (tracked across CLAUDE.md, README, sprint table)
- Features complete: 10
- Features partial: 3 (static delivery wired but wrong activator, status wired but docs stale, circuit breaker wired but DB store unused)
- Features missing: 2 (promote/build/env/team CLI commands, image signing)
- Dead code files: 2 (startup_reconciler.go, db/circuit_breaker_store.go)
- Stubs found: 1 (aws/index.ts — all methods throw)
- Orphan CRDs: 3 (AppSecret, PreviewEnvironment, ReconciliationReport — no controllers)
- Silent failures: 1 (failRollback in rollback_orchestrator.go)
- Completeness score: 10/15 (67%)

---

## Traceability Matrix

| Feature | Docs | API Route | Service | Repository | Test | Status |
|---------|------|-----------|---------|------------|------|--------|
| loopzero login | README.md | GET /api/v1/auth/whoami | — | membership | cli/tests/unit | COMPLETE |
| loopzero deploy | README.md | POST /api/v1/builds | BuildOrchestrator | buildStore, releaseStore | cli/tests/unit | COMPLETE |
| loopzero logs | README.md | GET /api/v1/builds/{id}/logs | LogsHandler (Loki) | buildStore | cli/tests/unit | COMPLETE |
| loopzero rollback | README.md | POST /api/v1/projects/{name}/rollback | RollbackOrchestrator | releaseStore | cli/tests/unit | COMPLETE |
| loopzero secret | README.md | POST/GET/DELETE /api/v1/secrets | SecretHandler + K8sManager | secretStore | cli/tests/unit | COMPLETE |
| loopzero status | README.md | GET /api/v1/projects/{name}/status | StatusHandler | buildStore, releaseStore | cli/tests/unit | COMPLETE |
| loopzero validate | README.md | (local only) | ValidateCommand | — | cli/tests/unit | COMPLETE |
| loopzero project delete | CLAUDE.md | DELETE /api/v1/projects/{name} | ProjectDeleter | buildStore, envTargetStore | cli/tests/unit | COMPLETE |
| Static frontend delivery | CLAUDE.md Sprint 10 | POST /api/v1/builds (rendering=static) | writeStaticRoutingEntries + KVClient | releaseStore | — | PARTIAL (StaticDeliveryActivator unconnected) |
| Secret sync via ESO | CLAUDE.md Sprint H6 | POST /api/v1/secrets | SecretK8sManager | secretStore | — | COMPLETE |
| loopzero promote | CLAUDE.md Sprint 4 | — | — | — | — | MISSING |
| loopzero build cancel | CLAUDE.md Sprint 4 | POST /api/v1/builds/{id}/cancel | BuildOrchestrator.Cancel | buildStore | api unit tests | PARTIAL (route exists, no CLI command) |
| loopzero env / loopzero team | CLAUDE.md Sprint 4 | — | — | — | — | MISSING |
| Preview environments | CLAUDE.md Sprint 5 | — | — | — | — | NOT STARTED |
| Image signing (cosign/syft) | CLAUDE.md Sprint 6 | — | — | — | — | NOT STARTED |
| AppSecret CRD controller | CLAUDE.md | — | — | — | — | NOT STARTED |
| Drift detection (ReconciliationReport) | CLAUDE.md | — | — | — | — | NOT STARTED |
| AWS cloud provider | CLAUDE.md | n/a | AWS stub | — | — | STUB |

---

## Incomplete Implementation Markers

### TODOs / FIXMEs (Go and TypeScript)
No TODO, FIXME, HACK, or XXX markers found in any Go source files under api/, operators/, or pkg/. None found in cli/src/ TypeScript files either. The codebase is clean of inline markers.

### Stubs
| Location | Description |
|----------|-------------|
| infra/pulumi/src/providers/aws/index.ts:14 | AWS provider — all 6 methods throw PROVIDER_NOT_IMPLEMENTED |

---

## Dead Code

### Functions/files defined but never called in production

1. **`StartupReconciler`** in `api/internal/services/startup_reconciler.go`
   - Defined at line 29, constructor at line 37
   - No caller in production code (main.go references it only in a comment)
   - Superseded by `BuildReconcileLoop` (C-03 fix), which handles startup recovery internally
   - Test file `api/tests/unit/startup_reconciler_test.go` still runs but tests dead code

2. **`db.CircuitBreakerStore`** in `api/internal/db/circuit_breaker_store.go`
   - Defined at line 26, constructor at line 31
   - `NewCircuitBreakerStore` is never called outside the file itself
   - Replaced by the in-memory `CircuitBreakerSyncClient` (pg_circuit_breaker.go, M-02 fix)
   - Migration 000006 creates `circuit_breakers` table that is never queried by any live code

3. **`StaticDeliveryActivator`** in `api/internal/services/static_delivery.go`
   - Full implementation with Activate/Rollback/phase methods (lines 35–272)
   - `NewStaticDeliveryActivator` never called in production
   - `BuildOrchestratorDeps.Delivery` is never set in `main.go`, causing permanent fallback to `ContainerDeliveryActivator`
   - Comment in build_orchestrator.go:657 says "Dispatches to ContainerDeliveryActivator or StaticDeliveryActivator" but dispatch logic does not exist

### Orphan CRDs (types with no controller)

The following CRD types are defined, have YAML manifests generated, and are registered in the scheme. No controller exists for them, so CRs of these types are never reconciled:

1. **`AppSecret`** (`operators/api/v1/appsecret_types.go`)
   - CRD YAML: `operators/config/crd/bases/loopzero.io_appsecrets.yaml`
   - Purpose: Secret sync from mgmt cluster to workload cluster
   - Status: acknowledged as not implemented in CLAUDE.md
   
2. **`PreviewEnvironment`** (`operators/api/v1/preview_types.go`)
   - CRD YAML: `operators/config/crd/bases/loopzero.io_previewenvironments.yaml`
   - Purpose: Ephemeral PR preview environments (Sprint 5)
   - Status: Sprint 5 not started

3. **`ReconciliationReport`** (`operators/api/v1/reconciliationreport_types.go`)
   - CRD YAML: `operators/config/crd/bases/loopzero.io_reconciliationreports.yaml`
   - Purpose: Drift detection between Git desired state and cluster actual state
   - Status: spec §13 describes behavior, no implementation exists

### Documented module that does not exist

**`loopzero-packs/`** — described in CLAUDE.md as "language/framework detection + Dockerfile generation" module. The directory does not exist. It is absent from `go.work`. No Go source file imports it. The language detection is handled in-process by `api/internal/argo/languages.go` using the external `github.com/getloopscale/loopzero-forge` package (which IS real and published).

Note: `loopzero-forge` is correctly an external dependency (`api/go.mod:11`), not a local module — the CLAUDE.md directory structure map is misleading on this point.

---

## Silent Failures

### 1. `failRollback` — empty lock keys + fully swallowed errors
**Location:** `api/internal/services/rollback_orchestrator.go:238-245`

```go
func (o *RollbackOrchestrator) failRollback(ctx context.Context, releaseID string, cause error) error {
    _ = o.deps.Locker.WithLock(ctx, "", "", "", func(tx db.DBTX) error {
        _, _ = o.deps.Releases.UpdateStatus(ctx, tx, releaseID, string(statemachine.ReleaseRollbackFailed_))
        return nil
    })
    return cause
}
```

Two problems:
- **Empty lock keys**: Advisory lock key becomes `::` — different from the actual `(tenant:project:env)` lock. Does not correctly serialize with the rollback's own lock, creating a race condition in the failure path.
- **Swallowed errors**: Both `WithLock` and `UpdateStatus` errors are discarded. If the lock times out or the DB is unavailable, the release stays in `ROLLING_BACK` state permanently with no diagnostic.

### 2. `sync_callback_activator.go` — swallowed predecessor supersession
**Location:** `api/internal/services/sync_callback_activator.go:110`

```go
prev, _ := a.releaseStore.GetActiveByEnvTx(ctx, tx, ...)
if prev != nil && prev.ID != release.ID {
    _, _ = a.releaseStore.UpdateStatus(ctx, tx, prev.ID, string(statemachine.ReleaseNewActivated))
}
```

If the predecessor supersession fails, it is silently ignored. The new release becomes ACTIVE while the old one stays ACTIVE in DB, creating a split-brain scenario in `environment_targets`. The error should be returned to roll back the transaction.

---

## Deprecated Infrastructure Kept Active

### LOOPZERO_ARGO_CALLBACK_SECRET required in production despite removal
**Location:** `api/cmd/main.go:72`

The ArgoCD callback HTTP route was removed in fix C-01. The `ArgoCallbackSecret` field in `server.Config` is explicitly marked deprecated. Yet `main.go:72` still requires `LOOPZERO_ARGO_CALLBACK_SECRET` as a mandatory environment variable in production startup validation. Any production deployment missing this variable will crash on startup with exit code 1.

This forces operators to maintain a meaningless secret in their deployment configuration.

---

## Feature Flags

No feature flag system found (no LaunchDarkly, Unleash, or custom FEATURE_ env var pattern). Environment-gated code uses direct env var checks (e.g., `cfg.IsProduction()`), which is appropriate.

---

## Findings (Detailed)

### COMP-001: 3 CRDs installed with no controllers
- **Severity:** high
- **Type:** missing_feature
- **Location:** operators/api/v1/appsecret_types.go, preview_types.go, reconciliationreport_types.go
- **Description:** AppSecret, PreviewEnvironment, and ReconciliationReport CRDs are installed on the cluster but have no controllers. Any object created with these types will never be reconciled.
- **Impact:** Users cannot use secret sync via AppSecret CRD. Preview environments are non-functional at the CRD level. Drift detection is unavailable.
- **Recommendation:** Do not install uncontrolled CRDs in production. Either add to a feature-gated install or remove until controllers are implemented.

### COMP-002: StaticDeliveryActivator implemented but never connected
- **Severity:** medium
- **Type:** dead_code / partial_implementation
- **Location:** api/internal/services/static_delivery.go:68
- **Description:** Full static delivery implementation exists but is never wired. Static frontends use the container delivery path which issues ArgoCD syncs — correct for container apps, incorrect for static frontends that only need CF KV writes.
- **Impact:** Sprint 10 is marked complete but the purpose-built delivery activator for static apps is unused.
- **Recommendation:** Wire StaticDeliveryActivator in main.go for builds where all apps have rendering=static. Add integration test.

### COMP-003: StartupReconciler is dead code
- **Severity:** medium
- **Type:** dead_code
- **Location:** api/internal/services/startup_reconciler.go
- **Impact:** Confusion about which recovery path is active. Test maintenance burden for code that never runs.
- **Recommendation:** Delete the file and its test file.

### COMP-004: db.CircuitBreakerStore is dead code
- **Severity:** medium
- **Type:** dead_code
- **Location:** api/internal/db/circuit_breaker_store.go
- **Impact:** Orphan migration creates a table that is never queried. Developer confusion about the actual circuit breaker mechanism.
- **Recommendation:** Delete the file. Add a drop migration with explicit sign-off.

### COMP-005: loopzero-packs module documented but does not exist
- **Severity:** medium
- **Type:** stale_documentation
- **Location:** CLAUDE.md:113, CLAUDE.md:242
- **Impact:** Developer confusion about module boundaries and where framework detection lives.
- **Recommendation:** Remove from CLAUDE.md documentation.

### COMP-006: AWS provider stub throws on all operations
- **Severity:** high
- **Type:** stub
- **Location:** infra/pulumi/src/providers/aws/index.ts:14
- **Impact:** Selecting AWS provider silently fails at Pulumi evaluation. CLAUDE.md's "AWS stub ready" implies readiness.
- **Recommendation:** Correct documentation to "not implemented". Add early-exit guard.

### COMP-007: failRollback uses wrong lock keys and swallows all errors
- **Severity:** medium
- **Type:** silent_failure
- **Location:** api/internal/services/rollback_orchestrator.go:240-244
- **Impact:** Releases can be permanently stuck in ROLLING_BACK state. Failure is invisible in logs.
- **Recommendation:** Use correct tenant/project/env keys. Log errors explicitly.

### COMP-008: LOOPZERO_ARGO_CALLBACK_SECRET required in production but feature is removed
- **Severity:** low
- **Type:** stale_code
- **Location:** api/cmd/main.go:72
- **Impact:** Operators must maintain a meaningless env var. Production deploy fails without it.
- **Recommendation:** Remove from required vars map and from config loading.

### COMP-009: All chaos engineering tests are Skip stubs
- **Severity:** low
- **Type:** stub
- **Location:** infra/tests/chaos/chaos_test.go:18
- **Impact:** Sprint 8 (Hardening + Chaos) prerequisite tests do not exist.
- **Recommendation:** Expected for unreached sprint. Prioritize advisory lock and concurrent deploy tests.

### COMP-010: CLAUDE.md incorrectly states loopzero status is pending
- **Severity:** low
- **Type:** stale_documentation
- **Location:** CLAUDE.md current status paragraph
- **Impact:** Developer confusion about feature completeness.
- **Recommendation:** Update CLAUDE.md to reflect H6 is 4/4 complete.

### COMP-011: Sprint 4 CLI commands promote, env, team not implemented
- **Severity:** medium
- **Type:** missing_feature
- **Location:** cli/src/commands/ (missing files)
- **Impact:** Sprint 4 exit gate cannot be met. Build cancel route exists in API but has no CLI command.
- **Recommendation:** Create tracking issues for promote, env, team commands.

### COMP-012: Image signing (cosign/syft) has zero implementation
- **Severity:** high
- **Type:** missing_feature (known, Sprint 6)
- **Location:** CLAUDE.md:152
- **Impact:** Container supply chain is unsigned. No SBOM generation.
- **Recommendation:** Expected for Sprint 6. Ensure CI scanning partially compensates until then.

---

## Recommendations (Prioritized)

1. **Remove uncontrolled CRDs from production clusters** (COMP-001) — Installing CRDs without controllers creates user-visible failures. Until AppSecret, PreviewEnvironment, and ReconciliationReport controllers are implemented, do not install their CRD YAML manifests.

2. **Fix failRollback silent failure** (COMP-007) — A rollback failure that leaves a release in ROLLING_BACK permanently is a production reliability issue. The fix is straightforward: use correct lock keys and log errors.

3. **Fix sync_callback_activator predecessor supersession** — Silent error on UpdateStatus during release activation can create a dual-ACTIVE state in the database. Return the error to abort the transaction.

4. **Wire StaticDeliveryActivator** (COMP-002) — Sprint 10 is marked complete but static app delivery uses the wrong code path. The implementation exists and just needs wiring in main.go.

5. **Delete dead code files** (COMP-003, COMP-004) — startup_reconciler.go and db/circuit_breaker_store.go are never called. Their presence creates maintenance burden and confusion.

6. **Remove LOOPZERO_ARGO_CALLBACK_SECRET from production required list** (COMP-008) — Operators must not be forced to maintain a secret for a removed feature.

7. **Update documentation** (COMP-005, COMP-010) — Remove loopzero-packs from CLAUDE.md module table. Update H6 sprint status to complete.

8. **Create tracking issues for missing Sprint 4 CLI commands** (COMP-011) — promote, env, team commands are exit gate requirements for Sprint 4.
