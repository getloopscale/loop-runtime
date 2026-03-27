# LoopZero Operators

Kubernetes operators for the LoopZero PaaS platform. Manages the full lifecycle of tenants, environments, applications, and platform infrastructure using the [operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

## Architecture

The operators run on the **management cluster** and reconcile resources across three clusters:

```
Management Cluster (operators live here)
  |
  |--- Build Cluster      build-{tenant} namespaces, quotas, RBAC, NetworkPolicy
  |--- Runtime Cluster     {tenant}-{env} namespaces, deployments, ingress, TLS
  |--- Gitea               GitOps repositories (tenant source of truth)
  |--- ArgoCD              Application/AppProject CRs, cluster secrets
  |--- MinIO/Spaces        Per-tenant credential isolation (optional)
```

### Controller Dependency Chain

```
Tenant (root)
  |
  |--- Environment            namespace + RBAC + NetworkPolicy + ResourceQuota
  |       |
  |       |--- LoopZeroApplication    hostname + ExposedRoute + health tracking
  |               |
  |               |--- ExposedRoute    TLS cert check + reachability probes
  |
  |--- [ArgoCD Applications]   one per environment, path-scoped
  |--- [Build Namespace]       quotas, ServiceAccount, RBAC, registry creds
  |--- [PriorityClass]         plan-based scheduling priority

GitRepositoryBinding           credential sync to ArgoCD (independent)
ClusterCredential              multi-cluster connectivity + ArgoCD secrets (independent)
PlatformBootstrap              declarative setup: Gitea -> ArgoCD -> API seed (independent)
```

## Custom Resource Definitions

| CRD | Controller | Purpose |
|-----|-----------|---------|
| **Tenant** | TenantController | Team/account lifecycle: Gitea repo, ArgoCD apps, build namespace, quotas |
| **Environment** | EnvironmentController | Deployment target: namespace, RBAC, NetworkPolicy, ResourceQuota |
| **LoopZeroApplication** | ApplicationController | Hostname assignment, ExposedRoute creation, pod health tracking |
| **ExposedRoute** | ExposureController | TLS certificate validation, HTTP reachability probes |
| **GitRepositoryBinding** | GitRepositoryBindingController | Credential sync to ArgoCD with rotation detection (SHA256) |
| **ClusterCredential** | ClusterCredentialController | Multi-cluster kubeconfig distribution, connectivity checks |
| **PlatformBootstrap** | PlatformBootstrapController | Declarative bootstrap: Gitea + ArgoCD + DB seed (replaces shell scripts) |
| **PreviewEnvironment** | -- | Ephemeral PR environments with TTL (Sprint 5, not yet implemented) |
| **ReconciliationReport** | -- | GitOps drift detection (not yet implemented) |
| **AppSecret** | -- | Cross-cluster secret sync (not yet implemented) |

## Package Structure

```
operators/
|-- api/v1/                        CRD type definitions (10 CRDs)
|-- cmd/main.go                    Manager entrypoint, controller wiring, env var config
|-- config/crd/bases/              Generated CRD YAML manifests
|-- internal/
|   |-- argocd/                    ArgoCD provisioner (interface + dynamic client implementation)
|   |-- conditions/                Typed condition helpers (SetCondition, AllConditionsTrue)
|   |-- constants/                 Centralized identifiers: labels, finalizers, ports, timeouts, resources
|   |-- controller/                7 reconcilers + supporting files
|   |-- gitea/                     Gitea API client (typed URL builder) + provisioner
|   |-- minio/                     Per-tenant MinIO credential provisioning
|   |-- naming/                    Kubernetes resource naming (delegates to pkg/naming)
|-- operatortest/                  Public test facade for E2E (fake provisioners)
|-- Dockerfile                     Multi-stage build (Go 1.25 + Alpine 3.21)
|-- Makefile                       Build, test, lint, CRD generation
```

### Internal Packages

| Package | Responsibility |
|---------|---------------|
| `argocd` | Interface `Provisioner` + `RealProvisioner` using dynamic Kubernetes client for Application/AppProject CRs |
| `conditions` | `SetCondition()` with LastTransitionTime preservation, `AllConditionsTrue()`, typed condition constants |
| `constants` | Single source of truth for all string identifiers: label keys, finalizer names, secret names, ports, timeouts, resource defaults |
| `controller` | All 7 reconcilers. Each follows: finalizer pattern, idempotent CreateOrUpdate, condition-based status |
| `gitea` | `APIClient` with typed methods (CheckVersion, CreateToken, EnsureOrg, GetRepo, CreateRepo, ArchiveRepo) and centralized URL builder |
| `minio` | `AdminProvisioner` creates per-tenant users with S3 bucket policies scoped to `loopzero-builds/{tenant}/*` |
| `naming` | Deterministic RFC 1123 naming for namespaces, hostnames, secrets (delegates to `pkg/naming`) |

## Configuration

### Required Environment Variables

| Variable | Description |
|----------|-------------|
| `BUILD_KUBECONFIG` | Path to build cluster kubeconfig |
| `RUNTIME_KUBECONFIG` | Path to runtime cluster kubeconfig |

### Optional Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GITEA_URL` | `http://gitea-http.loopzero-system.svc.cluster.local:3000` | Gitea HTTP API base URL |
| `GITEA_TOKEN` | (empty) | Gitea admin API token |
| `ARGOCD_NAMESPACE` | `argocd` | Namespace where ArgoCD is installed |
| `WORKLOAD_CLUSTER_URL` | `https://kubernetes.default.svc` | Workload cluster API server URL |
| `BASE_DOMAIN` | `dev.loopzero.example` | Base domain for application hostnames |
| `MINIO_ENDPOINT` | (empty) | MinIO endpoint; empty disables per-tenant isolation |
| `MINIO_ACCESS_KEY` | (empty) | MinIO admin access key |
| `MINIO_SECRET_KEY` | (empty) | MinIO admin secret key |
| `SPACES_ACCESS_KEY_ID` | (empty) | S3-compatible object storage access key (DO Spaces) |
| `SPACES_SECRET_ACCESS_KEY` | (empty) | S3-compatible object storage secret key |
| `DIGITALOCEAN_TOKEN` | (empty) | DO API token for registry credential generation |
| `REGISTRY_DOCKER_CONFIG` | (empty) | Raw Docker config.json (local dev alternative to DO token) |

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--metrics-bind-address` | `:8080` | Prometheus metrics endpoint |
| `--health-probe-bind-address` | `:8081` | Liveness/readiness probe endpoint |
| `--leader-elect` | `true` | Enable leader election for HA |

## Development

### Prerequisites

- Go 1.25+
- Docker
- [k3d](https://k3d.io) (local clusters)
- [controller-gen](https://book.kubebuilder.io/reference/controller-gen) v0.16.5

### Commands

```bash
# Run tests (Ginkgo + envtest, ~150s)
make test

# Run go vet
make vet

# Format code
make fmt

# Generate CRD manifests from kubebuilder markers
make manifests

# Generate DeepCopy methods
make generate

# Tidy dependencies
make tidy
```

### Running Locally

```bash
# From project root with Tilt
tilt up

# Or manually (requires k3d clusters running)
export BUILD_KUBECONFIG=~/.kube/build-cluster.yaml
export RUNTIME_KUBECONFIG=~/.kube/runtime-cluster.yaml
export GITEA_TOKEN=your-token
go run ./operators/cmd/main.go
```

## Design Decisions

### Multi-Cluster Client Pattern

The TenantController holds three Kubernetes clients:

```go
type TenantReconciler struct {
    client.Client      // mgmt cluster (CRDs, status)
    BuildClient        // build cluster (namespaces, quotas, secrets)
    RuntimeClient      // runtime cluster (namespaces, RBAC, NetworkPolicy)
}
```

Cross-cluster resources (build namespaces, runtime namespaces) cannot use `ownerReference` (Kubernetes limitation), so cleanup relies on label-based listing and the verify-then-remove finalizer pattern.

### Delegation Pattern

The TenantController creates child CRs instead of managing everything directly:

- **Tenant** creates **Environment** CRs (with `SetControllerReference`)
- **EnvironmentController** handles namespace/RBAC/NetworkPolicy on the runtime cluster
- **ApplicationController** creates **ExposedRoute** CRs
- **ExposureController** handles TLS checks and reachability probes

This follows SRP: each controller has one reason to change.

### State Machine Enforcement

Tenant and PlatformBootstrap phase transitions go through `pkg/statemachine`. Direct phase assignment is forbidden for these CRDs. Invalid transitions panic (programming errors, not runtime errors).

### Idempotent Reconciliation

All controllers use `controllerutil.CreateOrUpdate` for resource management. Every reconcile step is unconditionally re-applied (no condition-gated skipping), ensuring drift correction even when conditions were set by a previous controller version.

### Centralized Constants

All string identifiers (labels, finalizers, secret names, ports, timeouts) are defined in `internal/constants/`. No string literal identifiers appear in controller code. This eliminates the scattered-strings anti-pattern where the same label key appears in 15+ locations.

## Testing

Tests use [Ginkgo v2](https://onsi.github.io/ginkgo/) + [Gomega](https://onsi.github.io/gomega/) with [envtest](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest) (in-process Kubernetes API server).

```
Production:  6,466 lines (41 files)
Tests:       6,775 lines (20 files)
Ratio:       1.05:1 (test-to-production)
```

Test doubles for Gitea and ArgoCD provisioners live in `_test.go` files only (thread-safe with `sync.Mutex`). The `operatortest/` package exports public fakes for E2E tests.

### Test Categories

| Category | Location | Framework |
|----------|----------|-----------|
| CRD type validation | `api/v1/*_test.go` | Go testing |
| Controller unit tests | `internal/controller/*_test.go` | Ginkgo + envtest |
| Integration tests | `internal/controller/tenant_integration_test.go` | Ginkgo + envtest |
| Concurrency tests | `internal/controller/concurrent_reconcile_test.go` | Ginkgo + envtest |
| Condition helpers | `internal/conditions/conditions_test.go` | Go testing |
| Naming utilities | `internal/naming/naming_test.go` | Go testing |
