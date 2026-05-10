# Dependency Graph — operators/

**Phase 3, Iteration 1 — Architecture Review**
**Date:** 2026-04-26

## Text-Based DAG (import direction: A → B means A imports B)

```
cmd/main.go
  ├── api/v1             (CRD types)
  ├── internal/argocd    (provisioner)
  ├── internal/gitea     (provisioner)
  ├── internal/harbor    (provisioner)
  ├── internal/minio     (provisioner)
  ├── internal/httpclient
  ├── internal/constants
  ├── internal/controller
  ├── internal/tracing
  ├── internal/metrics
  ├── pkg/circuitbreaker
  └── pkg/logging

internal/controller/
  ├── tenant_controller.go
  │     ├── api/v1
  │     ├── internal/argocd     (via GitProvisioner, ArgoCDProvisioner fields)
  │     ├── internal/gitea      (via GitRepoProvisioner interface)
  │     ├── internal/harbor     (via Provisioner interface)
  │     ├── internal/minio      (via CredentialProvisioner interface)
  │     ├── internal/conditions
  │     ├── internal/constants
  │     ├── pkg/naming
  │     └── pkg/statemachine
  │
  ├── tenant_build.go (methods on TenantReconciler)
  │     ├── internal/conditions
  │     └── (no external provisioner imports — uses TenantReconciler.MinIOProvisioner field)
  │
  ├── tenant_gitops.go (methods on TenantReconciler)
  │     ├── internal/argocd     (uses TenantReconciler.ArgoCDProvisioner)
  │     └── internal/conditions
  │
  ├── tenant_harbor.go (methods on TenantReconciler)
  │     ├── internal/harbor     (uses TenantReconciler.HarborClient)
  │     └── internal/conditions
  │
  ├── tenant_deletion.go (methods on TenantReconciler)
  │     └── internal/conditions
  │
  ├── tenant_credentials.go (methods on TenantReconciler)
  │     └── internal/httpclient (direct HTTP for DOCR token validation)
  │
  ├── tenant_priority.go (methods on TenantReconciler)
  │     └── (no external imports)
  │
  ├── environment_controller.go
  │     ├── api/v1
  │     ├── internal/conditions
  │     ├── internal/constants
  │     └── pkg/statemachine
  │
  ├── environment_rbac.go (methods on EnvironmentReconciler)
  │     └── internal/conditions
  │
  ├── environment_network.go (methods on EnvironmentReconciler)
  │     └── (no external imports)
  │
  ├── environment_quota.go (methods on EnvironmentReconciler)
  │     └── (no external imports)
  │
  ├── application_controller.go
  │     ├── api/v1
  │     ├── internal/conditions
  │     ├── internal/constants
  │     └── pkg/naming
  │     NOTE: NO pkg/statemachine import — phase computed directly
  │
  ├── exposure_controller.go
  │     ├── api/v1
  │     ├── internal/conditions
  │     ├── internal/constants
  │     ├── internal/httpclient  (for reachability probe)
  │     └── pkg/naming
  │     NOTE: NO pkg/statemachine import — phase assigned directly
  │
  ├── gitrepositorybinding_controller.go
  │     ├── api/v1
  │     ├── internal/gitea      (GitRepoProvisioner)
  │     ├── internal/httpclient (DIRECT HTTP for reachability — no CB)
  │     ├── internal/conditions
  │     └── internal/constants
  │
  ├── clustercredential_controller.go
  │     ├── api/v1
  │     ├── internal/conditions
  │     └── internal/constants
  │
  ├── platformbootstrap_controller.go
  │     ├── api/v1
  │     ├── internal/gitea      (GitRepoProvisioner)
  │     ├── internal/httpclient
  │     ├── internal/conditions
  │     ├── internal/constants
  │     └── pkg/statemachine
  │
  └── preview_controller.go
        ├── api/v1
        ├── internal/conditions
        └── internal/constants
        NOTE: NO pkg/statemachine usage at runtime (blank identifier only)

internal/argocd/
  ├── circuitbreaker_provisioner.go → pkg/circuitbreaker
  ├── real_provisioner.go           → (k8s dynamic client only)
  ├── provisioner.go                → (interface only)
  └── applicationset.go             → (interface only)

internal/gitea/
  ├── circuitbreaker_provisioner.go → pkg/circuitbreaker
  ├── real_provisioner.go           → (HTTP client)
  ├── api.go                        → (HTTP types)
  └── provisioner.go                → (interface only)

internal/minio/
  ├── circuitbreaker_provisioner.go → pkg/circuitbreaker + pkg/naming
  ├── provisioner.go                → (interface only)
  └── provisioner_impl.go           → (MinIO S3 client)

internal/harbor/
  ├── circuitbreaker_client.go      → pkg/circuitbreaker
  ├── client.go                     → (HTTP client, 464 LOC)
  └── provisioner.go                → (interface only)

pkg/ (shared, zero operators/ imports — no cycles)
  ├── statemachine/  → (pure Go, no external deps)
  ├── circuitbreaker/ → (pure Go)
  ├── naming/        → (pure Go)
  ├── logging/       → (slog wrapper)
  ├── errors/        → (typed LoopZeroError)
  ├── id/            → (crypto/rand)
  ├── jwt/           → (RSA validation)
  ├── health/        → (temporal aggregation)
  └── enforcement/   → (plan limits)
```

## Circular Dependency Analysis

**Result: NO circular dependencies found.**

Dependency direction is strictly:
```
cmd/main.go → controller → provisioners → pkg/
                         ↘ pkg/
              api/v1 (CRD types only, no logic)
```

Key checks:
- `pkg/` does NOT import `operators/internal/` — confirmed
- `internal/argocd/` does NOT import `internal/gitea/` or `internal/controller/` — confirmed
- `internal/gitea/` does NOT import `internal/argocd/` — confirmed
- `internal/minio/` does NOT import `internal/harbor/` — confirmed
- No provisioner imports another provisioner — confirmed

## Circuit Breaker Coverage Matrix

| External System | CB Wrapper Exists | CB Wired in main.go | Status |
|-----------------|-------------------|---------------------|--------|
| Gitea           | YES (circuitbreaker_provisioner.go) | YES (line 133) | COVERED |
| ArgoCD          | YES (circuitbreaker_provisioner.go) | YES (line 149) | COVERED |
| Harbor          | YES (circuitbreaker_client.go)      | YES (line 253) | COVERED |
| MinIO           | YES (circuitbreaker_provisioner.go) | **NO** — setupMinIO() returns raw provisioner | **GAP (ARCH-001)** |
| httpclient (GitRepositoryBinding reachability) | NO wrapper | NO | **GAP (ARCH-004)** |

## State Machine Coverage Matrix

| CRD | Machine in pkg/statemachine | Controller Usage | Status |
|-----|------------------------------|------------------|--------|
| Tenant | YES (NewTenantMachine) | tenantSM.Transition() — CORRECT | COVERED |
| Environment | YES (NewEnvironmentMachine) | environmentSM.TryTransition() — CORRECT | COVERED |
| PlatformBootstrap | YES (NewPlatformBootstrapMachine) | platformBootstrapSM.TryTransition() — CORRECT | COVERED |
| PreviewEnvironment | YES (NewPreviewMachine) | blank identifier var _ = — BYPASS (COMP-002) | PARTIAL |
| ExposedRoute | **NO** | direct phase assignments | INTENTIONAL/UNDOCUMENTED |
| LoopZeroApplication | **NO** | computePhase() direct write | INTENTIONAL/UNDOCUMENTED |
| ClusterCredential | NO | no phase tracking | N/A |
| GitRepositoryBinding | NO | condition-based, no phase | N/A |
| Build/Release | YES (in pkg/) | used by api/, not operators/ | OUT OF SCOPE |
