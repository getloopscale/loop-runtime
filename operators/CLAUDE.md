# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> This is the `operators/` module of the LoopZero PaaS. For full system context, see `../CLAUDE.md`.

---

## What This Is

Kubernetes operators (Go + kubebuilder + controller-runtime) that manage the LoopZero PaaS control plane. Runs on the **management cluster**, reconciles resources across three clusters (mgmt, build, runtime). Part of a Go workspace (`../go.work`) alongside `pkg/`, `api/`, `loopzero-forge/`, `loopzero-packs/`.

---

## Development Commands

```bash
# Run all tests (Ginkgo + envtest, requires KUBEBUILDER_ASSETS or setup-envtest)
make test

# Run a single test file or focused spec
go test ./internal/controller/ -v -count=1 -run TestControllers -ginkgo.focus="should create"

# Generate CRD YAML from kubebuilder markers
make manifests

# Generate DeepCopy methods
make generate

# Lint
make vet

# From project root (preferred — sets KUBEBUILDER_ASSETS automatically)
task test:unit
```

**envtest setup:** Tests need `KUBEBUILDER_ASSETS` pointing to etcd/kube-apiserver binaries. The suite auto-discovers `~/.local/share/kubebuilder-envtest/k8s/1.35.0-linux-amd64/` if the env var is unset. Install via `setup-envtest use 1.35.0`.

---

## Architecture

### Multi-Cluster Client Pattern

The TenantController holds three distinct Kubernetes clients:

```go
r.Client        // mgmt cluster — CRDs, status updates, Gitea CRs, ArgoCD CRs
r.BuildClient   // build cluster — build-{tenant} namespaces, quotas, secrets
r.RuntimeClient // runtime cluster — {tenant}-{env} namespaces, RBAC, NetworkPolicy
```

Cross-cluster resources cannot use `ownerReference`, so cleanup uses **label-based listing + finalizer pattern**.

In envtest, all three clients point to the same API server. Physical separation is tested in E2E.

### Controller Dependency Chain

```
Tenant (root) → Environment → LoopZeroApplication → ExposedRoute
```

Each controller creates child CRs via `SetControllerReference`. Independent controllers: GitRepositoryBinding, ClusterCredential, PlatformBootstrap.

Wiring order in `cmd/main.go` matters — follows the dependency chain.

### Provisioner Interfaces

External systems are accessed through interfaces injected at startup:

| Interface | Package | Real Implementation |
|-----------|---------|-------------------|
| `gitea.GitRepoProvisioner` | `internal/gitea/` | `RealProvisioner` (Gitea HTTP API) |
| `argocd.Provisioner` | `internal/argocd/` | `RealProvisioner` (dynamic K8s client) |
| `minio.CredentialProvisioner` | `internal/minio/` | `AdminProvisioner` (MinIO Admin API) |

Test doubles live in `internal/controller/fake_provisioners_test.go` (internal) and `operatortest/setup.go` (public, for E2E).

### State Machine Enforcement

Tenant and PlatformBootstrap phases use `pkg/statemachine`. Direct phase assignment is **forbidden** — invalid transitions panic (programming errors).

```go
var tenantSM = statemachine.NewTenantMachine()
// Usage: tenantSM.Transition(currentPhase, event) → newPhase or panic
```

---

## Key Patterns

### Idempotent Reconciliation

All controllers use `controllerutil.CreateOrUpdate`. Every reconcile step is unconditionally re-applied (no condition-gated skipping). This ensures drift correction.

### Typed Conditions

All condition types and reasons are constants in `internal/conditions/types.go`. No freeform strings. Use `conditions.SetCondition()` which preserves `LastTransitionTime` when status hasn't changed.

### Centralized Constants

All string identifiers (labels, finalizers, secret names, ports, timeouts, resource defaults) live in `internal/constants/`. No string literals for identifiers in controller code.

### CRD Type → Controller → Setup Function

Each controller follows the pattern:
1. Types in `api/v1/{kind}_types.go` with kubebuilder markers
2. Reconciler struct in `internal/controller/{kind}_controller.go`
3. `Setup{Kind}Controller(mgr, deps...)` function wired in `cmd/main.go`
4. Tests in `internal/controller/{kind}_controller_test.go` (Ginkgo)

### Test Suite Structure

`suite_test.go` boots a shared envtest environment with all 7 controllers registered. Individual `*_test.go` files contain Ginkgo specs. The shared `BeforeSuite` creates fake provisioners and a non-cached `k8sClient` for assertions.

```go
// Use Eventually for async reconciliation assertions
Eventually(func(g Gomega) {
    var tenant loopzerov1.Tenant
    g.Expect(k8sClient.Get(ctx, key, &tenant)).To(Succeed())
    g.Expect(tenant.Status.Phase).To(Equal("Active"))
}, timeout, interval).Should(Succeed())
```

---

## CRDs (10 types, 7 with controllers)

Generated from kubebuilder markers in `api/v1/`. After modifying `*_types.go`:

```bash
make generate   # DeepCopy methods
make manifests  # CRD YAML in config/crd/bases/
```

CRD group: `loopzero.io`, version: `v1`.

---

## Environment Variables

Required: `BUILD_KUBECONFIG`, `RUNTIME_KUBECONFIG`.

Optional: `GITEA_URL`, `GITEA_TOKEN`, `ARGOCD_NAMESPACE`, `BASE_DOMAIN`, `WORKLOAD_CLUSTER_URL`, `MINIO_ENDPOINT`/`ACCESS_KEY`/`SECRET_KEY`, `SPACES_ACCESS_KEY_ID`/`SECRET_ACCESS_KEY`, `DIGITALOCEAN_TOKEN`, `REGISTRY_DOCKER_CONFIG`.

See `cmd/main.go` for defaults and initialization logic.
