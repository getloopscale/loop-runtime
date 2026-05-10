# Flow Diagrams — Phase 1 Baseline

## Flow 1: Tenant Creation (flow_tenant_creation)

```
CLI/API → Tenant CR created in mgmt cluster
                │
                ▼
        TenantController.Reconcile
                │
                ├─ [1] Add finalizer (loopzero.io/tenant-cleanup)
                │
                ├─ [2] tenantSM.Transition(TenantEmpty, TenantCreate)
                │       → Status.Phase = "Provisioning"
                │
                ├─ [3] reconcileGitRepo (via GitProvisioner)
                │       GitProvisioner.EnsureRepo(spec.GitOps.RepoName, spec.GitOps.Branch)
                │       → CircuitBreaker(Gitea) → Gitea HTTP API
                │       → tenant.Status.GitOpsRepo = repoURL
                │       → Condition: TypeGitRepoReady = True
                │
                ├─ [4] reconcileArgoCDApp (via ArgoCDProvisioner)
                │       EnsureAppProject(AppProjectSpec{
                │           SourceRepos: [repoURL],
                │           DestinationServer: WorkloadClusterURL,  ← SEC-007
                │           DestinationNamespaces: [per-env]
                │       })
                │       per env in spec.environments[]:
                │         EnsureApplication(path=env.Name, CellID=spec.CellID||"cell-default")
                │       → CircuitBreaker(ArgoCD) → dynamic.Client → mgmt cluster ArgoCD CRs
                │       → Condition: TypeArgoCDAppReady = True
                │
                ├─ [5] reconcileBuildNamespace (10 sub-steps on BUILD cluster)
                │       via r.BuildClient:
                │       ├─ ensureBuildNs: Namespace(build-{tenant})
                │       ├─ ensureBuildQuota: ResourceQuota (CPU/Mem limits + workflow count)
                │       ├─ reconcileBuildNetworkPolicy: egress-only (DNS+MinIO+registry+k8sAPI)
                │       │   RISK-007: registry egress restricted to RegistryCIDRs if configured
                │       ├─ ensureBuildLimitRange: default container CPU/mem
                │       ├─ ensureBuildServiceAccount: SA(workflow-builder)
                │       ├─ ensureBuildStorageCredentials: MinIO/Spaces Secret (if global creds)
                │       ├─ ensureBuildRegistryCredentials: BuildKit docker config Secret
                │       │   DOCR token caching: skip DO API if token fresh (AnnotationTokenExpiresAt)
                │       ├─ ensureBuildExecutorRBAC: Role+RoleBinding for Argo executor
                │       ├─ ensureBuildSubmitterRBAC: Role+RoleBinding for workflow-submitter SA
                │       └─ ensureBuildMinIOPerTenant: per-tenant MinIO user+policy (if provisioner != nil)
                │           auto-rotate: delete+reprovision if Secret missing but user exists
                │       → Conditions: TypeBuildNamespaceReady + TypeQuotaEnforced = True
                │
                ├─ [6] reconcilePriorityClass (RUNTIME cluster via r.RuntimeClient)
                │       → Condition: TypePriorityClassReady = True
                │
                ├─ [7] ensureHarborProject (if r.HarborClient != nil)
                │       CircuitBreaker(Harbor) → Harbor HTTP API
                │       → Condition: TypeHarborProjectReady = True
                │
                ├─ [8] ensureHarborRobotAccounts (if r.HarborClient != nil)
                │       per environment: EnsureRobotAccount (push-pull scoped to project)
                │
                ├─ [9] reconcileEnvironmentCRs
                │       CreateOrUpdate Environment CR per spec.environments[] (owned by Tenant)
                │       propagate tenant.Generation via AnnotationParentGeneration
                │       GC: delete Environment CRs no longer in spec
                │
                └─ [10] All conditions True?
                         ├─ YES: tenantSM.Transition(TenantProvisioning, TenantAllReady)
                         │        → Status.Phase = "Active"
                         │        re-fetch → Status().Update
                         └─ NO: return error (requeue with backoff)

PARALLEL: Each Environment CR triggers EnvironmentController (see Flow 2)
```

**Error handling:** Any step failure sets condition False, saves status (best-effort), returns error. controller-runtime retries with exponential backoff. Re-fetch before status update prevents stale resourceVersion errors.

---

## Flow 2: Environment Creation (flow_environment_creation)

```
TenantController.reconcileEnvironmentCRs
    → CreateOrUpdate Environment CR in mgmt cluster
                │
                ▼
        EnvironmentController.Reconcile
                │
                ├─ [1] Add finalizer (loopzero.io/environment-cleanup)
                │
                ├─ [2] environmentSM.Transition(EnvironmentEmpty, EnvironmentCreate)
                │       → Status.Phase = "Pending"
                │
                ├─ [3] reconcileNamespace (RUNTIME cluster, r.RuntimeClient)
                │       CreateOrUpdate Namespace(spec.Namespace.Name)
                │       Labels: tenant, managed-by
                │       PSA: pod-security.kubernetes.io/enforce=restricted
                │       → Condition: TypeEnvNamespaceReady = True
                │
                ├─ [4] reconcileRBAC (RUNTIME cluster)
                │       ClusterRoleBinding: admin role → tenant SA in namespace
                │       → Condition: TypeEnvRBACReady = True
                │
                ├─ [5] reconcileNetworkPolicies (RUNTIME cluster)
                │       ├─ deny-all ingress (default)
                │       ├─ allow-same-namespace (within namespace traffic)
                │       ├─ allow-traefik (ingress from Traefik namespace)
                │       └─ [IngressNamespace from INGRESS_NAMESPACE env var]
                │       → Condition: TypeEnvNetworkPolicyReady = True
                │
                ├─ [6] reconcileQuota (RUNTIME cluster)
                │       ResourceQuota from spec.ResourceQuota (CPU/Memory/Storage)
                │       → Condition: TypeEnvQuotaReady = True
                │
                └─ [7] All conditions True?
                         ├─ YES: environmentSM.TryTransition(current, EnvironmentProvisioned)
                         │        → Status.Phase = "Active"
                         │        Status.Namespace = nsName
                         │        re-fetch → Status().Update
                         │        → RequeueAfter 5min (drift detection)
                         └─ NO: return error (requeue)

DELETION FLOW:
    DeletionTimestamp set
        → environmentSM.TryTransition(current, EnvironmentDeleteRequested)
        → Status.Phase = "Deleting"
        → r.RuntimeClient.Delete(namespace)
        → Wait for namespace gone (RequeueNamespaceDeletionWait)
        → Only then: RemoveFinalizer
```

**Drift detection:** 5-minute periodic requeue ensures NetworkPolicies, RBAC, and ResourceQuotas are corrected if manually modified or deleted.

---

## Flow 3: Application/Exposure Chain (flow_application_exposure)

```
API deploy flow creates LoopZeroApplication CR in mgmt cluster
                │
                ▼
        ApplicationController.Reconcile
                │
                ├─ [1] reconcileHostname
                │       if spec.Hostname == "": naming.Hostname(project, env, baseDomain)
                │       Update spec (persists hostname permanently)
                │       re-fetch after Update
                │
                ├─ [2] Status.URL = "https://" + spec.Hostname
                │
                ├─ [3] reconcileExposedRoute
                │       CreateOrUpdate ExposedRoute CR (child, owned by LoopZeroApp)
                │       Spec: ApplicationRef, Hostname, ServiceRef{Name, Port}, TLS.Enabled=true
                │
                ├─ [4] reconcileRouteStatus
                │       Read ExposedRoute.Status from mgmt cluster
                │       Map: route.Status.Phase == Reachable → app.Status.Reachable = true
                │       Set conditions: TypeCertificateIssued, TypeRouteReachable
                │
                ├─ [5] reconcileHealthCondition
                │       Phase Reachable/Serving → TypeApplicationHealthy = True
                │       Otherwise → False with phase description
                │
                └─ [6] Status().Patch (not Update — avoids UID precondition failures)

ExposedRoute CR triggers ExposureController:
                │
                ▼
        ExposureController.Reconcile
                │
                ├─ [1] checkCertificateReady
                │       naming.CertSecretName(hostname) → read Secret in same namespace
                │       Secret.Type == TLS? → certReady=true
                │
                ├─ [2] TLS required and cert not ready?
                │       → phase=Pending, RequeueAfter 15s
                │
                ├─ [3] Cert ready: phase=CertIssued (if was Pending)
                │
                ├─ [4] performProbe(https://{hostname})
                │       AllowProbeURLOverride=false in prod (SEC-005 SSRF prevention)
                │       HTTP GET with httpclient timeout (ProbeTimeout constant)
                │
                ├─ [5] Probe result:
                │       Success (2xx/3xx): ConsecutiveFailures=0, phase=Reachable, RequeueAfter 5min
                │       Failure: ConsecutiveFailures++
                │           >= MaxConsecutiveProbeFailures: phase=Failed (condition Reason=Failed)
                │           < threshold: phase stays CertIssued (condition Reason=Unreachable)
                │           → RequeueAfter 15s
                │
                └─ [6] Status().Update

Recovery: When probe succeeds after failures, phase returns to Reachable and ConsecutiveFailures resets.
```

---

## Flow 4: Tenant Deletion (flow_tenant_deletion)

```
kubectl delete tenant {name}
    → Tenant.DeletionTimestamp set
                │
                ▼
        TenantController.reconcileDelete
                │
                ├─ [GATE] if not Deleting: tenantSM.TryTransition(current, TenantDelete)
                │           → Status.Phase = "Deleting", Status().Update
                │
                ├─ [Phase 1] Wait for child Environment CRs to be gone
                │   List all Environments in namespace
                │   Filter by ownerRef UID == tenant.UID
                │   For each: if DeletionTimestamp.IsZero() → Delete(envCR)
                │   if childEnvCount > 0: RequeueAfter(RequeueDeletionWait) — WAIT
                │
                ├─ [Phase 2] Cleanup tenant-level resources (timedCleanupStep each):
                │   ├─ deleteHarborProject (if HarborClient != nil)
                │   ├─ ArgoCDProvisioner.DeleteApplications(tenant.Name)
                │   ├─ ArgoCDProvisioner.DeleteAppProject(tenant.Name)
                │   ├─ Build cluster: list+delete ns with LabelTenant=tenant.Name
                │   ├─ MinIOProvisioner.DeleteTenantCredentials (if provisioner != nil)
                │   └─ GitProvisioner.ArchiveRepo(spec.GitOps.RepoName)
                │
                │   On partial failures:
                │   ├─ retryCount = getDeletionRetryCount(annotation) + 1
                │   ├─ retryCount < DeletionMaxAttempts:
                │   │     backoff = min(DeletionBackoffBase * 2^attempt, DeletionBackoffMax)
                │   │     setDeletionRetryCount(annotation), Update CR
                │   │     → RequeueAfter(backoff)
                │   └─ retryCount >= DeletionMaxAttempts:
                │         metrics.TenantDeletionStuckTotal.Inc()
                │         → RequeueAfter(RequeueDeletionStuck) — operator alert expected
                │
                ├─ [Phase 3] Verify build namespaces are gone
                │   r.BuildClient.List(LabelTenant=tenant.Name)
                │   if any remain: RequeueAfter(RequeueDeletionWait) — WAIT
                │
                └─ [Phase 4] All clean: RemoveFinalizer, Update CR
                              → Tenant CR is garbage collected by K8s

Each timedCleanupStep records duration to Prometheus: TenantDeletionStepDuration{step,result}
```

---

## Flow 5: PlatformBootstrap (flow_platformbootstrap)

```
PlatformBootstrap CR created (one-time setup)
                │
                ▼
        PlatformBootstrapController.Reconcile
                │
                ├─ [Terminal check] if phase in {Complete, Failed}: return immediately (no requeue)
                │
                ├─ [Phase dispatch]
                │
                ├─ Empty → platformBootstrapSM.Transition → Pending
                ├─ Pending → platformBootstrapSM.Transition → ConfiguringGitea
                │
                ├─ ConfiguringGitea: reconcileGiteaPhase
                │   giteaClient(pb.Spec.GiteaURL).CheckVersion()
                │   giteaClient.ValidateToken(adminToken from Secret)
                │   list tokens → delete old loopzero-api token if exists
                │   giteaClient.CreateToken(username, password, "loopzero-api")
                │   giteaClient.EnsureOrg(orgName)
                │   → on success: transitionToNextPhase → ConfiguringArgoCD
                │   → on failure: requeueWithRetry(retryCount, maxRetries, message)
                │
                ├─ ConfiguringArgoCD: reconcileArgoCDPhase
                │   argoCDClient(pb.Spec.ArgoCDURL).CheckReachable()
                │   argoCDClient.GenerateApiKey(adminPassword from Secret)
                │   write API key to platform-tokens Secret
                │   → on success: transitionToNextPhase → WaitingForAPI
                │   → on failure: requeueWithRetry
                │
                ├─ WaitingForAPI: reconcileAPIPhase
                │   GET {pb.Spec.APIURL}/health
                │   → on success: transitionToNextPhase → Complete (TERMINAL)
                │   → on failure: requeueWithRetry
                │
                └─ RetryCount >= MaxRetries: transitionToFailed → Failed (TERMINAL)

Cleanup (on deletion): delete platform-tokens Secret if labels[managed-by] = "platformbootstrap-controller"
```

---

## Flow 6: Preview Environment Lifecycle (flow_preview_lifecycle)
**STATUS: NOT OPERATIONAL — SetupPreviewController not called from main.go**

```
PreviewEnvironment CR created (e.g., by GitHub App PR handler)
  ↓ [CR exists in mgmt cluster but NO controller watches it — dead code]

IF the controller were wired:
                │
                ├─ Add finalizer (loopzero.io/preview-cleanup)
                ├─ Status.Phase = PreviewPhaseCreating (direct assignment — SM bypassed)
                ├─ Status.CreatedAt = now
                │
                ├─ reconcileCreating:
                │   CreateOrUpdate Namespace("preview-{tenantID}-pr{N}") on RUNTIME cluster
                │   Labels: tenant, managed-by, loopzero.io/preview
                │   PSA: pod-security.kubernetes.io/enforce=restricted
                │   Condition: NamespaceReady = True
                │   Status.URL = "https://prN-{projectID}.preview.{baseDomain}"
                │   Status.Phase = PreviewPhaseActive (direct assignment)
                │   Status().Update → RequeueAfter 5min
                │
                ├─ reconcileActive:
                │   Check spec.ExpiresAt → if expired: phase=Expiring, Requeue
                │   else: RequeueAfter 5min
                │
                ├─ reconcileExpiring:
                │   phase=Deleting, Status().Update
                │   Delete(preview CR) → triggers finalizer
                │
                └─ reconcileDelete:
                    r.RuntimeClient.Get namespace
                    if exists and DeletionTimestamp.IsZero(): r.RuntimeClient.Delete(ns)
                    if namespace still exists: RequeueAfter 5s
                    if namespace gone: RemoveFinalizer, Update

GAPS:
(1) SetupPreviewController NOT in main.go setupControllers() — no reconciliation happens
(2) previewSM declared as blank identifier: var _ = statemachine.NewPreviewMachine
    Phase transitions are direct assignments — state machine never enforced
(3) No workload deployment: namespace created but no ArgoCD Application CR, no Ingress/IngressRoute
    Preview URL resolves to empty namespace — 404/connection refused
(4) No RBAC, NetworkPolicy, or ResourceQuota provisioned for preview namespace
```
