# Flow Diagrams — Cluster Management & Control Plane

> 6 critical flows registered in review database

---

## Flow 1: Tenant Provisioning (Critical)

**Entry:** Tenant CR created in mgmt cluster
**Exit:** Tenant.Status.Phase = Active, all 5 conditions True

```
API / CLI                    mgmt cluster                build cluster         workload cluster
   │                              │                           │                       │
   │── POST /tenants ──────────> API creates Tenant CR        │                       │
   │                              │                           │                       │
   │                    TenantController.Reconcile()           │                       │
   │                              │                           │                       │
   │                    1. SM: "" → Provisioning               │                       │
   │                              │                           │                       │
   │                    2. gitea.EnsureRepo(source)            │                       │
   │                    3. gitea.EnsureRepo(gitops)            │                       │
   │                              │                           │                       │
   │                    4. argocd.EnsureAppProject()           │                       │
   │                    5. argocd.EnsureApplication()          │                       │
   │                              │                           │                       │
   │                    6. reconcileBuildNamespace() ────────> build-{tenant} ns       │
   │                       (10 idempotent steps)              RBAC, Quota, NetPol     │
   │                       a. namespace                       ServiceAccount          │
   │                       b. quota                           Storage credentials     │
   │                       c. networkpolicy                   Registry credentials    │
   │                       d. limitrange                      MinIO per-tenant        │
   │                       e. serviceaccount                        │                 │
   │                       f. storage-creds                         │                 │
   │                       g. registry-creds                        │                 │
   │                       h. executor-rbac                         │                 │
   │                       i. submitter-rbac                        │                 │
   │                       j. minio-per-tenant                      │                 │
   │                              │                                 │                 │
   │                    7. Create Environment CRs ──────────────────────────────────> │
   │                              │                                 │                 │
   │                    EnvironmentController.Reconcile()            │                 │
   │                       a. create namespace                      │     {t}-{env} ns│
   │                       b. create RBAC                           │     RBAC        │
   │                       c. create NetworkPolicy                  │     NetworkPolicy│
   │                       d. create ResourceQuota                  │     Quota       │
   │                              │                                 │                 │
   │                    8. All conditions True                      │                 │
   │                    9. SM: Provisioning → Active                │                 │
   │                              │                                 │                 │
   │<── 201 Created ─────────────┘                                  │                 │
```

---

## Flow 2: Platform Bootstrap (Critical)

**Entry:** PlatformBootstrap CR created
**Exit:** PlatformBootstrap.Status.Phase = BootstrapComplete

```
CI / Manual                  mgmt cluster                    External Services
   │                              │                                │
   │── Create PlatformBootstrap ─>│                                │
   │   CR in mgmt                 │                                │
   │                    PlatformBootstrapController.Reconcile()     │
   │                              │                                │
   │                    SM: "" → Pending → ConfiguringGitea        │
   │                              │                                │
   │                    platformbootstrap_gitea.go:                 │
   │                    ├── Create loopzero org in Gitea ─────────────>│ Gitea API
   │                    ├── Create admin user + token              │
   │                    ├── Set condition: GiteaReady=True         │
   │                              │                                │
   │                    SM: ConfiguringGitea → ConfiguringArgoCD   │
   │                              │                                │
   │                    platformbootstrap_argocd.go:               │
   │                    ├── Register clusters in ArgoCD ──────────>│ ArgoCD (K8s CRs)
   │                    ├── Register repo credentials              │
   │                    ├── Set condition: ArgoCDReady=True        │
   │                              │                                │
   │                    SM: ConfiguringArgoCD → WaitingForAPI      │
   │                              │                                │
   │                    platformbootstrap_api.go:                  │
   │                    ├── HTTP POST to API /bootstrap ──────────>│ API Server
   │                    ├── Seed admin tenant, default plans       │
   │                    ├── Set condition: DatabaseSeeded=True     │
   │                              │                                │
   │                    SM: WaitingForAPI → BootstrapComplete      │
   │                    Set condition: BootstrapComplete=True      │
   │                              │                                │
   │<── Status: Complete ────────┘                                 │
```

---

## Flow 3: Cluster Credential Registration (Critical)

**Entry:** ClusterCredential CR with kubeconfig Secret reference
**Exit:** ArgoCD cluster Secret created, Connected=True

```
Admin / Pulumi               mgmt cluster                   Target Cluster
   │                              │                                │
   │── Create ClusterCredential ─>│                                │
   │   CR + Secret with kubeconfig│                                │
   │                              │                                │
   │                    ClusterCredentialController.Reconcile()     │
   │                    1. Resolve kubeconfig from Secret           │
   │                    2. Validate connectivity ─────────────────>│ GET /healthz
   │                       (5s timeout)                            │ (5s timeout)
   │                    3. Create ArgoCD cluster Secret             │
   │                       in argocd namespace                     │
   │                    4. Set Connected=True                      │
   │                    5. Requeue every 5min                      │
   │                              │                                │
```

---

## Flow 4: Environment Namespace Setup (High)

**Entry:** Environment CR created by TenantController
**Exit:** All 4 conditions True (NamespaceReady, RBACReady, NetworkPolicyReady, QuotaReady)

```
TenantController              mgmt cluster               workload cluster
   │                              │                           │
   │── Create Environment CR ────>│                           │
   │                              │                           │
   │                    EnvironmentController.Reconcile()      │
   │                    1. CreateOrUpdate namespace ──────────>│ {tenant}-{env}
   │                    2. CreateOrUpdate Role/RoleBinding ───>│ RBAC
   │                    3. CreateOrUpdate NetworkPolicy ──────>│ isolation
   │                    4. CreateOrUpdate ResourceQuota ──────>│ limits
   │                    5. Update conditions on mgmt ─────────│
   │                              │                           │
```

---

## Flow 5: Git Repository Credential Binding (Critical)

**Entry:** GitRepositoryBinding CR with credential Secret reference
**Exit:** ArgoCD repository Secret created/updated, ConsumerHealthy=True

```
PlatformBootstrap             mgmt cluster
   │                              │
   │── Create GitRepositoryBinding>│
   │   CR + credential Secret      │
   │                              │
   │                    GitRepositoryBindingController.Reconcile()
   │                    1. Resolve credentials from Secret
   │                    2. SHA256 hash comparison (rotation detection)
   │                    3. Create/Update ArgoCD repository Secret
   │                    4. HTTP health check to Gitea endpoint
   │                    5. Set ConsumerHealthy=True
   │                              │
```

---

## Flow 6: Build Workflow Orchestration (Critical)

**Entry:** API submits Argo Workflow to build-{tenant} namespace
**Exit:** Build image pushed to registry, workflow SUCCEEDED

```
API Server                   build cluster                 External
   │                              │                           │
   │── Submit Argo Workflow ─────>│ build-{tenant} ns         │
   │   (via Argo REST API)        │                           │
   │                              │                           │
   │                    Workflow steps:                        │
   │                    1. clone (CWT) ──────────────────────>│ Gitea (source)
   │                    2. cache-restore (CWT) ──────────────>│ MinIO (cache)
   │                    3. build (BuildKit rootless) ─────────│
   │                    4. push image ───────────────────────>│ Registry
   │                    5. cache-save (CWT) ─────────────────>│ MinIO (cache)
   │                    6. summary (CWT)                      │
   │                              │                           │
   │<── Status callback/poll ────┘                            │
```
