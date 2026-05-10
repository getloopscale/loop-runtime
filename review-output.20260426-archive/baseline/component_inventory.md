# Component Inventory — Phase 1 Baseline

## Controllers (8 total, 7 active)

### comp_tenant_controller — TenantController
- **Path:** `/home/paulo/loopzero/operators/internal/controller/tenant_controller.go` (+ 6 support files)
- **Support files:** `tenant_build.go`, `tenant_deletion.go`, `tenant_gitops.go`, `tenant_harbor.go`, `tenant_priority.go`, `tenant_credentials.go`
- **Technology:** Go 1.25, controller-runtime, kubebuilder
- **Status:** Active, wired in main.go
- **Cluster targets:** mgmt (r.Client), build (r.BuildClient), runtime (r.RuntimeClient)
- **MaxConcurrentReconciles:** 5 default (configurable: LOOPZERO_TENANT_MAX_CONCURRENT)
- **Dependencies:** gitea.GitRepoProvisioner, argocd.Provisioner, harbor.Provisioner (optional), minio.CredentialProvisioner (optional), pkg/statemachine, pkg/naming
- **State machine:** `tenantSM = statemachine.NewTenantMachine()` — transitions: Empty->Provisioning, Provisioning->Active, any->Deleting
- **Reconcile steps:** 7 top-level (git, argocd, buildns, priority, harbor-project, harbor-robots, environment-CRs)
- **Build namespace sub-steps:** 10 (ns, quota, networkpolicy, limitrange, SA, storage-creds, registry-creds, executor-rbac, submitter-rbac, minio-per-tenant)
- **Notable patterns:** runSteps() with per-step condition setting; re-fetch before status update; DOCR token caching via annotation

### comp_environment_controller — EnvironmentController
- **Path:** `/home/paulo/loopzero/operators/internal/controller/environment_controller.go` (+ 3 support files)
- **Support files:** `environment_network.go`, `environment_rbac.go`, `environment_quota.go`
- **Technology:** Go 1.25, controller-runtime
- **Status:** Active, wired in main.go
- **Cluster targets:** mgmt (r.Client), runtime (r.RuntimeClient)
- **Dependencies:** pkg/statemachine.NewEnvironmentMachine, conditions, constants
- **State machine:** `environmentSM = statemachine.NewEnvironmentMachine()` — Empty->Pending->Active->Deleting; uses TryTransition for runtime phase validation
- **Reconcile steps:** 4 (namespace, RBAC, NetworkPolicy, ResourceQuota) on runtime cluster
- **Drift detection:** RequeueAfter 5min for periodic cross-cluster reconcile
- **Finalizer:** Waits for runtime namespace deletion before removing (RequeueNamespaceDeletionWait)

### comp_application_controller — ApplicationController
- **Path:** `/home/paulo/loopzero/operators/internal/controller/application_controller.go`
- **Technology:** Go 1.25, controller-runtime
- **Status:** Active, wired in main.go
- **Cluster targets:** mgmt only (r.Client)
- **Dependencies:** pkg/naming.Hostname, pkg/naming.CertSecretName, conditions, constants
- **State machine:** None — phase computed from conditions (Progressing/Serving/Reachable/Degraded/Down)
- **Owns:** ExposedRoute CRs (child, cascade delete)
- **Notable:** Uses Status().Patch (not Update) to avoid UID precondition failures from concurrent reconciles

### comp_exposure_controller — ExposureController
- **Path:** `/home/paulo/loopzero/operators/internal/controller/exposure_controller.go`
- **Technology:** Go 1.25, controller-runtime
- **Status:** Active, wired in main.go
- **Cluster targets:** mgmt only (r.Client)
- **Dependencies:** pkg/naming.CertSecretName, httpclient, conditions, constants
- **State machine:** None — direct phase assignment (Pending->CertIssued->Reachable/Failed)
- **Security:** AllowProbeURLOverride=false in production (prevents SSRF via annotation)
- **Adaptive requeue:** 15s when unstable, 5min when Reachable
- **Failure tracking:** ConsecutiveFailures counter; >= MaxConsecutiveProbeFailures -> phase=Failed with recovery on success

### comp_platformbootstrap_controller — PlatformBootstrapController
- **Path:** `/home/paulo/loopzero/operators/internal/controller/platformbootstrap_controller.go` (+ `platformbootstrap_api.go`, `platformbootstrap_argocd.go`, `platformbootstrap_gitea.go`)
- **Technology:** Go 1.25, controller-runtime
- **Status:** Active, wired in main.go
- **Cluster targets:** mgmt only (r.Client)
- **Dependencies:** pkg/statemachine.NewPlatformBootstrapMachine, gitea.BootstrapClient, ArgoCDBootstrapClient (interface), httpclient, conditions
- **State machine:** `platformBootstrapSM` — Empty->Pending->ConfiguringGitea->ConfiguringArgoCD->WaitingForAPI->Complete|Failed
- **Testability:** Factory injection pattern (GiteaClientFunc, ArgoCDClientFunc) enables unit testing without HTTP calls
- **One-shot:** Terminal phases (Complete/Failed) stop reconcile permanently
- **Cleanup:** Manages platform-tokens Secret (deletes on CR deletion if managed-by label matches)

### comp_clustercredential_controller — ClusterCredentialController
- **Path:** `/home/paulo/loopzero/operators/internal/controller/clustercredential_controller.go`
- **Technology:** Go 1.25, controller-runtime, k8s.io/client-go
- **Status:** Active, wired in main.go
- **Cluster targets:** mgmt (reads kubeconfig Secret + writes ArgoCD Secret), target cluster (connectivity check)
- **Dependencies:** conditions, constants
- **State machine:** None
- **Watch trigger:** Watches corev1.Secret — Secret rotation triggers immediate requeue (MEET-053 D-02)
- **Auth extraction:** Uses clientcmd.RESTConfigFromKubeConfig (not naive string parsing, MEET-053 A-02)
- **Limitation:** exec-based auth (DOKS default kubeconfigs) explicitly rejected
- **Periodic:** RequeueAfter=RequeuePeriodicCheck (5min) for connectivity health

### comp_gitrepositorybinding_controller — GitRepositoryBindingController
- **Path:** `/home/paulo/loopzero/operators/internal/controller/gitrepositorybinding_controller.go`
- **Technology:** Go 1.25, controller-runtime
- **Status:** Active, wired in main.go
- **Cluster targets:** mgmt only (r.Client)
- **Dependencies:** gitea.RepoAPIURL, httpclient, conditions, constants
- **State machine:** None
- **Change detection:** SHA256 hash of credential token stored in status.CredentialHash
- **Non-blocking:** Repository reachability check does not block binding (success just sets condition)
- **Periodic:** RequeueAfter=RequeueRepoBindingInterval

### comp_preview_controller — PreviewController
- **Path:** `/home/paulo/loopzero/operators/internal/controller/preview_controller.go`
- **Technology:** Go 1.25, controller-runtime
- **Status:** DEFINED but NOT WIRED in main.go setupControllers()
- **Cluster targets:** mgmt (r.Client), runtime (r.RuntimeClient)
- **Dependencies:** conditions, constants, pkg/statemachine (blank import only)
- **State machine:** `var _ = statemachine.NewPreviewMachine` — blank identifier, never instantiated or used. Direct phase assignments bypass SM.
- **Functional gaps:** (1) Not wired in main.go — zero PreviewEnvironment CRs are reconciled; (2) No workload deployment — namespace created but no ArgoCD Application or Ingress provisioned; (3) State machine bypassed.
- **Phase logic:** Creating->Active->Expiring->Deleting (all via direct assignment, not SM)
- **TTL:** Based on ExpiresAt field, checked every 5min

---

## Provisioners (4, all circuit-breaker wrapped)

### comp_gitea_provisioner — Gitea Provisioner
- **Path:** `/home/paulo/loopzero/operators/internal/gitea/`
- **Files:** `provisioner.go` (interfaces), `real_provisioner.go`, `circuitbreaker_provisioner.go`, `api.go`
- **Interfaces:** `GitRepoProvisioner` (EnsureRepo, RepoExists, ArchiveRepo), `BootstrapClient` (CheckVersion, ValidateToken, ListTokens, DeleteToken, CreateToken, EnsureOrg)
- **Circuit breaker:** `NewCircuitBreakerProvisioner(rawProvisioner, cb, logger)`
- **Dependencies:** pkg/circuitbreaker

### comp_argocd_provisioner — ArgoCD Provisioner
- **Path:** `/home/paulo/loopzero/operators/internal/argocd/`
- **Files:** `provisioner.go` (interface), `real_provisioner.go`, `circuitbreaker_provisioner.go`, `applicationset.go`, `real_provisioner_test.go`
- **Interface:** `argocd.Provisioner` (EnsureAppProject, EnsureApplication, DeleteApplications, DeleteAppProject)
- **Implementation:** dynamic.Client to create/update ArgoCD CRs in mgmt cluster
- **Circuit breaker:** `NewCircuitBreakerProvisioner(rawProvisioner, cb, logger)`
- **Cell routing:** CellID from ApplicationSpec passed to provisioner (ADR-0008 Phase 0: defaults to "cell-default")

### comp_minio_provisioner — MinIO Provisioner
- **Path:** `/home/paulo/loopzero/operators/internal/minio/`
- **Files:** `provisioner.go`, `provisioner_test.go`, `circuitbreaker_provisioner.go`
- **Interface:** `CredentialProvisioner` (EnsureTenantCredentials, DeleteTenantCredentials)
- **SDK:** `minio/madmin-go/v3` for admin operations
- **Optional:** nil means global credentials mode (Spaces keys used directly)
- **Auto-rotate:** Delete+reprovision if Secret is missing but MinIO user still exists
- **Circuit breaker:** wraps AdminProvisioner

### comp_harbor_provisioner — Harbor Provisioner
- **Path:** `/home/paulo/loopzero/operators/internal/harbor/`
- **Files:** `provisioner.go` (interface), `client.go`, `client_test.go`, `circuitbreaker_client.go`
- **Interface:** `harbor.Provisioner` (EnsureProject, DeleteProject, EnsureRobotAccount, DeleteRobotAccount, Healthy)
- **Optional:** nil means DOCR mode (Harbor integration disabled)
- **Gate:** When HarborClient != nil, TypeHarborProjectReady required for Tenant ACTIVE transition
- **Circuit breaker:** `NewCircuitBreakerClient(client, cb, logger)`

---

## Shared Internal Packages

| Package | Path | Purpose |
|---------|------|---------|
| conditions | `internal/conditions/` | SetCondition, GetCondition, AllConditionsTrue helpers |
| constants | `internal/constants/` | All K8s label/annotation/name constants centralized |
| httpclient | `internal/httpclient/` | HTTP client utilities with timeout |
| metrics | `internal/metrics/` | Prometheus metrics (reconcile outcomes, deletion steps, CB state) |
| tracing | `internal/tracing/` | OTel span helpers (startReconcileSpan, startChildSpan, recordError, setSuccess) |

## Shared External Pkg (../pkg)

| Package | Used by Operators |
|---------|------------------|
| pkg/statemachine | TenantSM, EnvironmentSM, PlatformBootstrapSM; PreviewSM (blank import) |
| pkg/naming | BuildNamespace, EnvironmentNamespace, Hostname, CertSecretName |
| pkg/circuitbreaker | All 4 provisioner circuit breakers |
| pkg/logging | RegisterTraceExtractor (main.go only) |
| pkg/errors | NOT used by any controller (fmt.Errorf used instead) |
| pkg/health | NOT used by operators |
| pkg/jwt | NOT used by operators |

---

## Entrypoint

### comp_main_entrypoint — cmd/main.go
- **Path:** `/home/paulo/loopzero/operators/cmd/main.go`
- **Leader election:** Enabled by default (`--leader-elect=true`)
- **Tracing:** OTel initialized via tracing.Init("loopzero-operator")
- **pprof:** Controlled by LOOPZERO_PPROF_ENABLED=true (default off, isolated ServeMux)
- **Retry:** `newClientWithRetry` with 3 attempts, 2s base exponential backoff for cluster client creation
- **Env vars required:** GITEA_TOKEN, GITEA_URL, BUILD_KUBECONFIG, RUNTIME_KUBECONFIG, BASE_DOMAIN
- **Env vars optional:** ARGOCD_NAMESPACE, WORKLOAD_CLUSTER_URL, MINIO_ENDPOINT+ACCESS+SECRET, HARBOR_URL+USER+PASS+HOST, DIGITALOCEAN_TOKEN, REGISTRY_DOCKER_CONFIG, REGISTRY_CIDRS, LOOPZERO_TENANT_MAX_CONCURRENT, LOOPZERO_PPROF_ENABLED
- **Mutex enforcement:** DIGITALOCEAN_TOKEN and REGISTRY_DOCKER_CONFIG are mutually exclusive (os.Exit(1) if both set)
- **Controllers wired:** 7 (Tenant, ClusterCredential, Environment, Application, Exposure, GitRepositoryBinding, PlatformBootstrap)
- **Controllers NOT wired:** PreviewController
- **Anomaly:** `tenant-controller` compiled binary (ELF 64-bit) committed to repo root

## Test Infrastructure

- **Framework:** Ginkgo v2 + Gomega + envtest (K8s 1.31.0)
- **Shared setup:** `operatortest/` package
- **Suite:** `suite_test.go` with fake provisioner Reset() for state-leak prevention
- **Fake provisioners:** `fake_provisioners_test.go` for all controller tests
- **Coverage:** Most controllers have dedicated `*_controller_test.go`; shared: `allconditions_test.go`, `network_policies_test.go`, `resource_quotas_test.go`
