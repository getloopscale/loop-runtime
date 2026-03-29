# Architecture Map — Cluster Management & Control Plane

> Scoped review: Cluster management, integrations, tools, and control plane

## Scope Boundary

### IN SCOPE
- 3-cluster topology (mgmt, build, workload)
- 7 operator controllers + 3 provisioner integrations
- 10 CRDs (7 wired, 3 unwired)
- State machines (Tenant, PlatformBootstrap)
- Cluster lifecycle: k3d (local), Pulumi IaC (production/DO)
- 13 Helm charts, Taskfile automation, GitHub Actions infra-deploy.yml
- API control plane integration points (k8s, ArgoCD, Argo Workflows, Gitea, MinIO)

### OUT OF SCOPE
- CLI TypeScript implementation
- API business logic beyond CP integration
- Framework detection (loopzero-packs)
- User-facing deploy/rollback/logs features

---

## Four-Plane Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│  DEVELOPER PLANE                                                     │
│  CLI (TypeScript) ←→ API (Go + chi + pgx)                          │
│  External dependency — not in scope                                  │
├─────────────────────────────────────────────────────────────────────┤
│  CONTROL PLANE  (mgmt cluster — k3d-loopzero-mgmt / DOKS SFO3)         │
│                                                                      │
│  ┌──────────────┐  ┌──────────────────┐  ┌─────────────────────┐   │
│  │ Tenant       │→ │ Environment      │→ │ LoopZeroApplication     │   │
│  │ Controller   │  │ Controller       │  │ Controller          │   │
│  └──────┬───────┘  └──────────────────┘  └──────┬──────────────┘   │
│         │                                        │                   │
│         │  ┌──────────────────┐                  ↓                   │
│         │  │ ClusterCredential│  ┌──────────────────────────────┐   │
│         │  │ Controller       │  │ ExposureController           │   │
│         │  └──────────────────┘  └──────────────────────────────┘   │
│         │                                                            │
│  ┌──────┴──────────────────┐  ┌───────────────────────────────┐    │
│  │ PlatformBootstrap       │  │ GitRepositoryBinding          │    │
│  │ Controller              │  │ Controller                    │    │
│  └─────────────────────────┘  └───────────────────────────────┘    │
│                                                                      │
│  External Services (mgmt cluster):                                   │
│  PostgreSQL 16 │ Gitea 1.22.6 │ MinIO │ ArgoCD 2.13.3              │
│  Prometheus + Grafana │ Loki + Promtail │ OTel │ ESO 0.14.3        │
├─────────────────────────────────────────────────────────────────────┤
│  BUILD PLANE  (build cluster — k3d-loopzero-build / DOKS SFO3)          │
│  Argo Workflows + BuildKit (rootless, single-container)              │
│  build-{tenant} namespaces with NetworkPolicy + Quota + RBAC         │
│  Provisioned by: TenantController via BUILD_KUBECONFIG               │
├─────────────────────────────────────────────────────────────────────┤
│  WORKLOAD PLANE  (workload cluster — k3d-loopzero-workload / DOKS SFO3) │
│  User applications managed by ArgoCD (manual sync)                   │
│  {tenant}-{env} namespaces with RBAC + NetworkPolicy + Quota         │
│  Provisioned by: EnvironmentController via RUNTIME_KUBECONFIG        │
│  Traefik 28.3.0 + cert-manager 1.14.5 │ Cloudflare Tunnel          │
└─────────────────────────────────────────────────────────────────────┘
```

## Multi-Cluster Client Pattern

The operator process runs on mgmt but manages resources across all 3 clusters:

```
operators/cmd/main.go
  │
  ├── mgr.Client        → mgmt cluster (CRDs, status, ArgoCD CRs, Gitea)
  ├── buildClient        → build cluster (build-{tenant} namespaces, secrets, quotas)
  └── runtimeClient      → workload cluster ({tenant}-{env} namespaces, RBAC, NetworkPolicy)
```

Cross-cluster resources cannot use `ownerReference`. Cleanup uses **label-based listing + finalizer pattern**.

## Controller Dependency Chain

```
Tenant (root)
  ├── creates Environment CRs (one per spec.environments entry)
  │     └── EnvironmentController creates namespace/RBAC/NetworkPolicy/Quota on workload
  ├── creates build-{tenant} namespace on build cluster (10 idempotent steps)
  ├── calls gitea.EnsureRepo (source + gitops repos)
  └── calls argocd.EnsureAppProject + EnsureApplication

LoopZeroApplication (created by API after deploy)
  └── ExposureController (checks TLS, reachability)

Independent:
  ├── GitRepositoryBindingController (credential sync to ArgoCD)
  ├── ClusterCredentialController (cluster connectivity validation)
  └── PlatformBootstrapController (one-time platform setup)
```

## Provisioner Integration Points

| Provisioner | Interface | Real Impl | Test Double | Used By |
|-------------|-----------|-----------|-------------|---------|
| Gitea | `gitea.GitRepoProvisioner` | `RealProvisioner` (HTTP API) | `FakeGitProvisioner` | TenantController |
| ArgoCD | `argocd.Provisioner` | `RealProvisioner` (dynamic K8s client) | `FakeArgoCDProvisioner` | TenantController, PlatformBootstrap |
| MinIO | `minio.CredentialProvisioner` | `AdminProvisioner` (Admin HTTP API) | nil (disabled in prod) | TenantController (optional) |

## Cluster Lifecycle

### Local (k3d)
```
task setup
  → registry:create (k3d registry, port 5000)
  → clusters:create (3 × k3d cluster create --config)
  → infra:install (monitoring-crds → mgmt → build → workload → monitoring)
  → networking:setup
  → crds:install (kubectl apply -f operators/config/crd/bases/)
  → db:migrate
  → tokens:provision
```

### Production (DigitalOcean via Pulumi)
```
infra-deploy.yml (GitHub Actions)
  → resolve (stack + environment)
  → validate (tsc + tests)
  → deploy (Pulumi up — 3-phase: infra → bootstrap → expose)
```

Pulumi 3-phase model:
1. **infra**: DOKS clusters, PostgreSQL, Container Registry, Spaces, VPC, DNS
2. **bootstrap**: Helm charts (ArgoCD, Gitea, MinIO, cert-manager, Traefik, monitoring)
3. **expose**: Cloudflare Tunnel, CF Worker, DNS records
