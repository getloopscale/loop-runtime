# ADR 0006: Eliminate state duality — single owner per entity

## Status
Accepted (2026-03-24)

## Context
An architectural audit identified that several entities (Build, Release, Tenant) have state distributed across multiple systems (PostgreSQL, Kubernetes CRDs, Git) without explicit reconciliation. Some CRDs (LoopZeroBuild, LoopZeroRelease) are fully defined with controllers but never instantiated by any code path — they are dead code that creates architectural confusion.

The core principle: **each piece of state must have exactly ONE owner**. Multiple owners without reconciliation guarantees divergence under failure.

## Investigation Findings

### LoopZeroBuild CRD
- **Defined in:** `operators/api/v1/build_types.go`
- **Controller:** `operators/internal/controller/build_controller.go`
- **Created by:** Nobody. No code path creates LoopZeroBuild CRs.
- **De facto owner:** PostgreSQL `builds` table (all transitions via API)
- **Verdict:** Dead code. Remove.

### LoopZeroRelease CRD
- **Defined in:** `operators/api/v1/release_types.go`
- **Controller:** `operators/internal/controller/release_controller.go`
- **Created by:** Test harness only. ReleaseReconciler is a passive stub.
- **De facto owner:** PostgreSQL `releases` + `environment_targets` tables (all transitions via API ReleaseActivator)
- **Verdict:** Dead weight. Remove.

### Tenant
- **PostgreSQL:** `tenants` + `tenant_members` tables (identity, auth)
- **Kubernetes:** Tenant CRD (infrastructure: namespaces, Gitea, ArgoCD, RBAC)
- **Bug found:** `routes/projects.go:138-143` creates Tenant CRD as **best-effort** — silent failure via `slog.Error` without HTTP error response. Tenant exists in PG without K8s infrastructure.
- **Verdict:** Two legitimate owners for two different concerns (identity vs infrastructure). Fix the silent failure.

### Deploy execution path
- **Spec describes** fallback path (kubectl apply --server-side) for Gitea downtime
- **Code implements** only normal path (Git → ArgoCD → workload)
- **Verdict:** No execution duality exists in practice. Do not implement fallback in v1.

## Architectural Invariants

These invariants are frozen rules that MUST hold before, during, and after implementation. They are the acceptance criteria for the ADR itself.

### Invariant A — Build Ownership
The `builds` table in PostgreSQL is the **sole authority** for build lifecycle state. Derived state may exist in Argo Workflow CRD status, SSE events, logs, and metrics, but none of them participate in lifecycle decisions. No controller, reconciler, or external system may transition build state — only the API service layer via `BuildStore.UpdateStatus()` with state machine validation.

**Test:** A complete build lifecycle (QUEUED → RUNNING → SUCCEEDED) must work with zero LoopZeroBuild CRDs installed in the cluster.

### Invariant B — Release Ownership
The `releases` table and `environment_targets` table in PostgreSQL are the **sole authority** for release lifecycle state and deployment pointers. Git commits are delivery artifacts. ArgoCD sync status is convergence feedback. Neither is consulted for lifecycle decisions. No controller may transition release state — only the API service layer via `ReleaseStore.UpdateStatus()`.

**Test:** A complete release lifecycle (READY → PENDING → ACTIVE) must work with zero LoopZeroRelease CRDs installed in the cluster.

### Invariant C — CRD Existence Criteria
A Kubernetes CRD is justified if and only if:
1. It represents **durable declarative intent** (not transient execution state)
2. Its controller performs **real side effects** (creates namespaces, repos, syncs secrets — not just status mirroring)
3. It has an **unambiguous single owner** (one controller, one reconcile loop)

If a CRD fails any of these criteria, it must not exist. This prevents CRD inflation and dead-code controllers.

### Invariant D — No Silent Ownership Failures
When system A creates state that system B must reconcile, the creation must be **fail-loud**. A silent log without HTTP error response is a bug, not a design choice. Specifically: if Tenant identity (PG) is created but Tenant infrastructure (CRD) fails, the API must return an error, not silently continue.

## Decisions

### Decision 1: Remove LoopZeroBuild CRD and BuildReconciler
Remove `operators/api/v1/build_types.go`, `operators/internal/controller/build_controller.go`, and `config/crd/bases/loopzero.io_loopzerobuilds.yaml`. PostgreSQL is the single owner of build state. Argo Workflow CRDs remain as transient execution runtime (not state).

### Decision 2: Remove LoopZeroRelease CRD and ReleaseReconciler
Remove `operators/api/v1/release_types.go`, `operators/internal/controller/release_controller.go`, and `config/crd/bases/loopzero.io_loopzeroreleases.yaml`. PostgreSQL is the single owner of release state. Git is the delivery mechanism (not source of truth).

### Decision 3: Fix Tenant CRD creation — fail loud, not silent
Change `routes/projects.go` to return HTTP 503 when `EnsureTenant()` fails instead of silently logging. Tenant CRD is a prerequisite for deploy — its absence causes cascading failures.

### Decision 4: Do not implement fallback deploy path in v1
If Gitea is down, deploy fails with GIT_MIRROR_FAILED. User retries. One delivery path is simpler to test, monitor, and debug than two.

### Decision 5: Document API worker extraction for v2
API process runs background loops (BuildReconcileLoop, reapers, cleaners) alongside HTTP server. Acceptable for single-replica v1. Must extract to `loopzero-worker` process with leader election before scaling API to multiple replicas.

## Consequences

### Positive
- CRD count reduced from 12 to 10 (Build, Release removed)
- No dead code in operator binary
- Clear ownership: PostgreSQL owns transactional state, K8s CRDs own infrastructure state
- Tenant creation failure becomes visible instead of causing mysterious deploy failures
- Single deploy path simplifies testing and incident response

### Negative
- If future architecture wants operator-driven builds/releases, CRDs must be re-created
- Removing CRDs means no `kubectl get loopzerobuilds` observability (mitigated by API endpoints + CLI)

### Neutral
- Argo Workflow CRDs (build cluster) remain — they are execution runtime, not state
- ArgoCD Applications remain — they are convergence executors, not state
- LoopZeroApplication CRD (Phase 4) remains planned — it serves a distinct purpose (health observability)

## Entity Ownership Map (Post-Decision)

| Entity | Owner | System | Secondary (derived) |
|--------|-------|--------|---------------------|
| Build state | PostgreSQL | `builds` table | Argo Workflow CRD (transient execution) |
| Release state | PostgreSQL | `releases` + `environment_targets` | Git (delivery), ArgoCD (convergence) |
| Tenant identity | PostgreSQL | `tenants` + `tenant_members` | — |
| Tenant infrastructure | Kubernetes | Tenant CRD → TenantReconciler | — |
| Environment infrastructure | Kubernetes | Environment CRD → EnvironmentReconciler | — |
| Deployment pointer | PostgreSQL | `environment_targets` | — |
| App health (Phase 4) | Kubernetes | LoopZeroApplication CRD → ApplicationController | — |
| Route/TLS (Phase 4) | Kubernetes | ExposedRoute CRD → ExposureController | — |
| Secret sync | Kubernetes | AppSecret CRD → AppSecretOperator | — |
| Cluster credentials | Kubernetes | ClusterCredential CRD | — |
| Git repo bindings | Kubernetes | GitRepositoryBinding CRD | — |
| Platform bootstrap | Kubernetes | PlatformBootstrap CRD (one-shot) | — |
| Drift detection | Kubernetes | ReconciliationReport CRD (observational) | — |
