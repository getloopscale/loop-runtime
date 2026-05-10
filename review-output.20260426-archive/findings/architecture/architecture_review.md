# Architecture Review — operators/

**Phase 3, Iteration 1**
**Date:** 2026-04-26
**Reviewers:** architecture-analyst, dependency-analyzer (under chief-reviewer direction)

---

## Executive Summary

The `operators/` codebase has a **sound architectural foundation**: no circular dependencies exist between packages, the provisioner DIP pattern is consistently applied across all 4 external systems (Gitea, ArgoCD, MinIO, Harbor), and the circuit breaker pattern is structurally complete for 3 of 4 systems. The state machine is used correctly in all 3 controllers where it applies (Tenant, Environment, PlatformBootstrap). No god modules were found — the largest controller file is tenant_build.go at 490 LOC, well below the 800-LOC concern threshold.

Four findings were registered, all low-medium severity. No critical or high-severity architectural defects were found in this phase.

---

## Import Graph Analysis

### Layering

The codebase follows a clean three-layer architecture:

```
Layer 1: cmd/main.go (entry point — wires everything)
Layer 2: internal/controller/ (reconcile loops — K8s orchestration)
         internal/argocd, gitea, minio, harbor (provisioners — external system clients)
Layer 3: pkg/ (shared library — zero external deps, no operators/ imports)
         api/v1/ (CRD type definitions)
```

Dependency direction flows strictly downward. No reverse imports exist.

### Circular Dependency Result

**NONE found.** Full verification:
- `pkg/` imports nothing from `operators/internal/` — confirmed
- No provisioner imports another provisioner — confirmed
- `controller/` imports provisioner interfaces only — confirmed
- `api/v1/` is imported by controller and provisioners (types only) — confirmed

See `/home/paulo/loopzero/review-output/analysis/dependency_graph.md` for the full text DAG.

---

## Separation of Concerns Analysis

### Tenant Reconciler (Distributed God Struct)

The TenantReconciler is spread across 7 files totaling ~1,678 lines. This is intentional and documented with an ARCH-002 comment in tenant_controller.go:46-52 explaining which fields each sub-file uses. The split is:

| File | Lines | Responsibility |
|------|-------|----------------|
| tenant_controller.go | 342 | Core reconcile loop, step orchestration, Environment CR management |
| tenant_build.go | 490 | Build namespace, quotas, MinIO credentials, NetworkPolicy |
| tenant_harbor.go | 290 | Harbor project and robot account provisioning |
| tenant_deletion.go | 251 | Deletion orchestration, cleanup steps, state machine transition to Deleting |
| tenant_credentials.go | 124 | Build cluster credential secrets (DO Token, registry docker config) |
| tenant_gitops.go | 112 | Gitea repo + ArgoCD Application provisioning |
| tenant_priority.go | 69 | PriorityClass on runtime cluster |

**Assessment:** This is an appropriate decomposition. Each file has a single cohesive responsibility. No file exceeds 500 LOC. The TenantReconciler struct fields are long but documented — this is inherent to a multi-cluster controller that orchestrates 5 external systems. Not a finding.

### Controller vs Provisioner Boundary

The controller/provisioner boundary is clean for the primary flows:
- Tenant→Gitea: via `gitea.GitRepoProvisioner` interface (DIP)
- Tenant→ArgoCD: via `argocd.Provisioner` interface (DIP)
- Tenant→MinIO: via `minio.CredentialProvisioner` interface (DIP)
- Tenant→Harbor: via `harbor.Provisioner` interface (DIP)

**Exception — GitRepositoryBinding:** gitrepositorybinding_controller.go:251-290 makes direct `http.NewRequestWithContext()` and `httpClient.Do()` calls for reachability checks without going through a provisioner interface. This is flagged as ARCH-004 (low severity).

**Exception — Credentials:** tenant_credentials.go uses `httpclient.NewWithTimeout()` for direct DOCR token validation. This is a narrow, single-purpose HTTP call; not flagged as a finding given the existing `HTTPClient *http.Client` field already provides an injection seam.

### Environment, Application, ExposedRoute Reconcilers

These controllers have clean single responsibilities:
- `EnvironmentReconciler`: namespace + RBAC + NetworkPolicy + quota on runtime cluster
- `ApplicationReconciler`: hostname assignment + ExposedRoute CR management + health tracking
- `ExposureReconciler`: TLS certificate readiness + HTTP reachability probe

No mixing of K8s reconcile concerns with business logic was found.

---

## Pattern Audit Table

| Pattern | Status | Evidence |
|---------|--------|----------|
| Provisioner DIP (interface + real + CB + controller field) | CONSISTENT for Gitea/ArgoCD/Harbor | circuitbreaker_provisioner.go in each package |
| Provisioner DIP for MinIO | PARTIAL — CB wrapper exists but not wired | setupMinIO() returns raw provisioner (ARCH-001) |
| Circuit breaker (CB) applied | 3/4 external systems | Gitea CB line 133, ArgoCD CB line 149, Harbor CB line 253 |
| State machine for lifecycle | CORRECT where applicable | Tenant, Environment, PlatformBootstrap use TryTransition |
| State machine for ExposedRoute/Application | NOT APPLIED (no machine defined) | Documented gap (ARCH-002) |
| State machine for Preview | CB ANCHOR ONLY — 4 direct bypasses | COMP-002 (Phase 2 finding) |
| Conditions — typed constants | MOSTLY CONSISTENT | 2 freeform strings in preview+deletion (ARCH-003) |
| Conditions — reasons | CONSISTENT | All use ReasonReady, ReasonFailed, ReasonWaiting constants |
| Naming — RFC 1123 via pkg/naming | CONSISTENT | naming.EnvironmentNamespace, naming.Hostname, naming.CertSecretName used throughout |
| Hardcoded namespace names | MINOR — "loopzero-system" secret name | NamespacePlatformSystem constant exists for namespace; secret name duplicated raw |
| No circular dependencies | CLEAN | Confirmed by import graph analysis |
| God module (>800 LOC) | NONE | Largest file: tenant_build.go 490 LOC |

---

## Findings

### ARCH-001 — MEDIUM: MinIO CredentialProvisioner not wrapped in circuit breaker

**File:** `operators/cmd/main.go:208-231`

The MinIO circuit breaker wrapper (`minio/circuitbreaker_provisioner.go`) exists and implements the same pattern as Gitea, ArgoCD, and Harbor, but is never applied in `setupMinIO()`. The function returns `miniop.NewAdminProvisioner()` directly. If MinIO is degraded during bulk tenant provisioning/deletion, reconcile loops will hammer the MinIO API without backoff protection.

**Fix:** Add CB creation and wrapping in `setupMinIO()`, add `cbStateLogger` parameter. Effort: LOW.

### ARCH-002 — LOW: ExposedRoute and LoopZeroApplication phase tracking without state machine

**Files:** `operators/internal/controller/exposure_controller.go:95-161`, `application_controller.go:205`

Neither `ExposedRoute` nor `LoopZeroApplication` has a machine in `pkg/statemachine/`. Both controllers use direct `Status.Phase =` assignments for phase transitions. This is NOT in the 9 documented machines (CLAUDE.md). The controllers are structurally correct but violate the spirit of Rule 8 by having undocumented phase transition logic.

**Fix:** Either add state machines for these two CRDs, or document explicitly in CLAUDE.md and controller comments that their phase logic is intentionally computed rather than machine-driven. Effort: MEDIUM.

### ARCH-003 — LOW: Freeform condition type strings bypass typed constants

**Files:** `operators/internal/controller/preview_controller.go:119,127`, `tenant_deletion.go:79`

Two files pass raw string literals (`"NamespaceReady"`, `"EnvironmentCleanup"`) to `conditions.SetCondition()` instead of using the typed constants from `conditions/types.go`. The constant `conditions.TypeEnvNamespaceReady = "NamespaceReady"` exists. `TypeEnvironmentCleanup` does not exist and needs to be added.

**Fix:** Use existing constant; add missing constant; add CI grep check. Effort: LOW.

### ARCH-004 — LOW: GitRepositoryBindingController has inline HTTP calls without CB protection

**File:** `operators/internal/controller/gitrepositorybinding_controller.go:251-290`

The controller makes direct `http.NewRequestWithContext()` calls for repository reachability validation inside the reconcile loop, bypassing the provisioner DIP pattern used elsewhere. No circuit breaker protection is applied to these calls.

**Fix:** Extract into a `ReachabilityChecker` interface injected into the reconciler, enabling CB wrapping. Effort: MEDIUM.

---

## Positive Architecture Findings

The following areas are architecturally sound and do not require action:

1. **No circular dependencies** — the full import graph is a clean DAG with strictly downward flow.

2. **Circuit breaker consistency (3/4 systems)** — Gitea, ArgoCD, and Harbor all have `circuitbreaker_provisioner.go` wrappers that are correctly instantiated with named circuit breakers (`cb.New(cb.Config{Name: "gitea"...})`), connected to the shared state change logger, and wired through `provisionerDeps`.

3. **State machine correctness (3/3 applicable controllers)** — TenantController, EnvironmentController, and PlatformBootstrapController all instantiate state machine singletons at the package level (`var tenantSM = statemachine.NewTenantMachine()`) and use `TryTransition()` for all phase changes.

4. **Conditions package discipline** — The `conditions/types.go` package correctly separates typed constants from the `conditions.SetCondition()` helper, and the vast majority of callers use the typed constants. Only 2 files have freeform strings (ARCH-003).

5. **No god modules** — All controller files are under 500 LOC. The TenantReconciler decomposition into 7 focused sub-files is well-structured and documented.

6. **Naming consistency** — `pkg/naming` is used consistently for namespace names (`naming.EnvironmentNamespace`), hostnames (`naming.Hostname`), and certificate secret names (`naming.CertSecretName`) throughout the controller package.

7. **main.go size and structure** — At 438 LOC, main.go is organized into well-named functions (`setupProvisioners`, `setupMinIO`, `setupHarbor`, `setupControllers`, `createClusterClient`) with clear responsibilities. Not bloated.
