# Completeness Audit — Operators & Control Plane

**Date:** 2026-03-29
**Target:** /home/paulo/loopzero/operators/
**Scope:** Cluster management, integrations, tools, and Control Plane
**Reviewer:** completeness-auditor

---

## Executive Summary

- CRDs promised: 10
- CRDs with controllers: 7 (fully wired, no stubs)
- CRDs partial: 1 (ClusterCredential — ArgoCD secret format incomplete)
- CRDs with no controller: 3 (AppSecret, PreviewEnvironment, ReconciliationReport)
- TODO/FIXME/HACK markers: 0
- Stubs (NotImplementedError / panic / empty bodies): 0
- Silent failures: 0 (all errors propagate)
- Dead code fields: 1 (EndpointsReady never set)
- Critical bugs: 2 (bootstrap auth mismatch, MinIO SigV4)
- Completeness score: 7/10 wired (70%), but 2 of 7 have functional bugs

---

## Traceability Matrix

| CRD | Types | Controller | Wired in main.go | Tests | Status |
|-----|-------|-----------|-----------------|-------|--------|
| Tenant | tenant_types.go | tenant_controller.go + tenant_build.go + tenant_gitops.go + tenant_credentials.go + tenant_priority.go | YES | tenant_controller_test.go + tenant_integration_test.go | COMPLETE |
| Environment | environment_types.go | environment_controller.go | YES | environment_controller_test.go | COMPLETE |
| LoopZeroApplication | application_types.go | application_controller.go | YES | application_controller_test.go | PARTIAL (EndpointsReady field never set) |
| ExposedRoute | exposedroute_types.go | exposure_controller.go | YES | exposure_controller_test.go | COMPLETE |
| PlatformBootstrap | platformbootstrap_types.go | platformbootstrap_controller.go + _gitea.go + _argocd.go + _api.go | YES | platformbootstrap_controller_test.go | PARTIAL (bootstrap endpoint auth missing) |
| GitRepositoryBinding | gitrepositorybinding_types.go | gitrepositorybinding_controller.go | YES | gitrepositorybinding_controller_test.go | COMPLETE |
| ClusterCredential | clustercredential_types.go | clustercredential_controller.go | YES | clustercredential_controller_test.go | PARTIAL (ArgoCD cluster secret schema incorrect) |
| AppSecret | appsecret_types.go | — | NO | — | MISSING |
| PreviewEnvironment | preview_types.go | — | NO | — | NOT STARTED (Sprint 5) |
| ReconciliationReport | reconciliationreport_types.go | — | NO | — | DEAD CODE (no sprint assigned) |

---

## Controller-by-Controller Analysis

### a) TenantController

**Files:** tenant_controller.go, tenant_build.go, tenant_gitops.go, tenant_credentials.go, tenant_priority.go

**Reconcile steps (all implemented, none stubbed):**
1. Finalizer management — implemented
2. Phase transition Provisioning via state machine — implemented
3. `reconcileGitRepo` — creates Gitea repo via RealProvisioner (tenant_gitops.go:22)
4. `reconcileArgoCDApp` — creates AppProject + per-environment ArgoCD Applications (tenant_gitops.go:34)
5. `reconcileBuildNamespace` — 10 sub-steps: ns, quota, netpol, limitrange, SA, storage-creds, registry-creds, executor-rbac, submitter-rbac, per-tenant-minio (tenant_build.go:26)
6. `reconcilePriorityClass` — plan-tiered PriorityClass (starter/growth/enterprise) on runtime cluster (tenant_priority.go:36)
7. `reconcileEnvironmentCRs` — creates Environment CRs with ownerRef for cascade delete (tenant_controller.go:303)
8. Phase → Active when all 5 conditions True — implemented
9. Deletion: child env wait → ArgoCD cleanup → build namespace cleanup → Gitea archive — implemented

**Gap found:** MinIO user and policy are NOT deleted during tenant deletion (finding comp_ctrl_007). The CredentialProvisioner interface has no Delete method.

**State machine compliance:** Phase transitions use `tenantSM.Transition()` — direct assignment is forbidden and enforced.

**requiredProvisioningConditions** (line 177): GitRepoReady, ArgoCDAppReady, BuildNamespaceReady, QuotaEnforced, PriorityClassReady — all 5 are set by their respective reconcile functions.

### b) EnvironmentController

**Files:** environment_controller.go

**4 conditions — all implemented:**
1. `TypeEnvNamespaceReady` — `reconcileNamespace` creates namespace on RuntimeClient with tenant/env/managed-by labels
2. `TypeEnvRBACReady` — `reconcileRBAC` creates ServiceAccount, Role (plan-tiered), RoleBinding
3. `TypeEnvNetworkPolicyReady` — `reconcileNetworkPolicies` creates deny-all + allow-ingress (from platform-system) + allow-egress (DNS/intra-ns/API-server)
4. `TypeEnvQuotaReady` — `reconcileQuota` creates ResourceQuota (if spec.resourceQuota set) and LimitRange (always)

All steps use CreateOrUpdate (idempotent). Phase → Active when all 4 conditions succeed. Deletion waits for namespace termination on RuntimeClient before removing finalizer.

**Status:** COMPLETE — no gaps.

### c) ApplicationController

**Files:** application_controller.go

**Implemented steps:**
1. `reconcileHostname` — assigns `pkg/naming.Hostname(project, environment, baseDomain)` to `spec.Hostname` if empty, persists via Update
2. `reconcileExposedRoute` — creates ExposedRoute CR with ownerRef, TLS enabled, ServiceRef populated
3. `reconcileRouteStatus` — reads ExposedRoute status, propagates CertificateReady and Reachable to app conditions
4. `reconcileHealthCondition` — sets ApplicationHealthy based on phase

**Gap found (comp_ctrl_003):** `computePhase` at line 234 returns `ApplicationPhaseServing` when `app.Status.EndpointsReady == true`. No code anywhere in the controller or any other controller sets `EndpointsReady = true`. The field is defined (application_types.go:85) but is a write-dead field. ApplicationPhaseServing is therefore unreachable.

### d) ExposureController

**Files:** exposure_controller.go

**Implemented steps:**
1. Certificate check — `checkCertificateReady` reads the TLS Secret by `naming.CertSecretName(hostname)`, verifies `SecretTypeTLS`. Returns true if TLS disabled.
2. Phase gate — if cert not ready and TLS required, stays Pending and requeues at `RequeueProbeInterval`
3. Phase → CertIssued when cert ready
4. Reachability probe — `performProbe` makes HTTP GET (with InsecureSkipVerify for self-signed certs), `resolveProbeURL` supports `loopzero.io/probe-url-override` annotation for test injection
5. Consecutive failure tracking — after `MaxConsecutiveProbeFailures` consecutive failures, phase → Failed
6. Adaptive requeue — `RequeueProbeStableInterval` when Reachable, `RequeueProbeInterval` otherwise

**Status:** COMPLETE — both TLS check and reachability probe are fully implemented.

### e) PlatformBootstrapController

**Files:** platformbootstrap_controller.go, platformbootstrap_gitea.go, platformbootstrap_argocd.go, platformbootstrap_api.go

**Phase 1 — Gitea (reconcileGiteaPhase):**
- Checks version reachability
- Reads admin secret (username + password)
- Validates or creates admin API token (ListTokens → delete old → CreateToken)
- Stores token in platform-tokens Secret
- Ensures organizations via EnsureOrg
- All retry-gated with `requeueWithRetry`

**Phase 2 — ArgoCD (reconcileArgoCDPhase):**
- Checks ArgoCD reachable via GET /api/version
- Generates non-expiring apiKey via two-step flow (admin session → apiKey for loopzero-api account)
- Stores apiKey in platform-tokens Secret
- Creates repo credential Secrets via CreateOrUpdate

**Phase 3 — API (reconcileAPIPhase):**
- Checks API health via GET healthEndpoint
- Calls POST bootstrapEndpoint

**CRITICAL GAP (comp_ctrl_001):** `callBootstrapEndpoint` (platformbootstrap_api.go:64) sends no Authorization header. The API's bootstrap endpoint requires `Authorization: Bearer <LOOPZERO_BOOTSTRAP_SECRET>` (api/internal/routes/bootstrap.go:70-80). The default value of LOOPZERO_BOOTSTRAP_SECRET is `loopzero-dev-bootstrap-secret` (non-empty). The APIBootstrapSpec has no field for passing this secret to the controller. This is a functional break in the bootstrap flow.

### f) GitRepositoryBindingController

**Files:** gitrepositorybinding_controller.go

**SHA256 hash rotation — fully implemented:**
1. `resolveCredential` — reads token from Secret.Data[ref.Key]
2. `computeCredentialHash` — `sha256.Sum256([]byte(token))`, returns first 12 hex chars
3. `ensureArgoCDSecret` — CreateOrUpdate ArgoCD Secret with `argocd.io/secret-type=repository` label, sets `annotations[AnnotationCredentialHash] = hash`
4. `binding.Status.CredentialHash = hash` — stored for comparison on next reconcile
5. `checkReachability` — HTTP GET to Gitea repo API with token, non-blocking (never blocks reconcile success)
6. Periodic requeue at `RequeueRepoBindingInterval` — ensures rotation detection

**Rotation detection:** The current hash is stored in `status.CredentialHash`. On the next reconcile, if the token in the Secret has changed, the new hash will differ and `ensureArgoCDSecret` will update the ArgoCD Secret (CreateOrUpdate always overwrites data). This is correct.

**Status:** COMPLETE — hash comparison works correctly.

### g) ClusterCredentialController

**Files:** clustercredential_controller.go

**Implemented steps:**
1. `resolveKubeconfig` — reads Secret by SecretRef, extracts `kubeconfig` key
2. `checkConnectivity` — parses kubeconfig via clientcmd.RESTConfigFromKubeConfig, creates k8s clientset, calls GET /healthz, validates response == "ok"
3. `reconcileArgoCDClusterSecret` — creates/updates ArgoCD cluster Secret when `spec.scope.argoCD == true`

**GAP found (comp_ctrl_008):** The ArgoCD cluster Secret's `config` field only contains `{"tlsClientConfig": {"insecure": true/false}}`. It does not include `bearerToken`, `caData`, `certData`, or `keyData`. ArgoCD cannot authenticate to the target cluster with only the server URL and a TLS insecure flag — it needs actual auth credentials embedded in the config JSON. The raw kubeconfig is written as a separate `kubeconfig` key which ArgoCD does not recognize. This means ArgoCD cluster registration via ClusterCredential will not actually work for authentication.

---

## Dead Code

### Fields never written
- `app.Status.EndpointsReady` (application_types.go:85) — defined, read in computePhase, never set to true by any code. ApplicationPhaseServing is unreachable.

### CRDs registered but fully unused
- `ReconciliationReport` — scheme-registered, deepcopy generated, no controller, no callers, no sprint. This type is dead code in production.

---

## Stubs and Placeholders

None found. All 7 wired controllers have substantive implementations. No `NotImplementedError`, `panic("not implemented")`, or empty function bodies were found in any non-test file.

---

## Silent Failures

None found in production code. All error paths in controllers propagate errors up the call stack. The only `_ = err` patterns found are:
- `defer func() { _ = resp.Body.Close() }()` — standard Go practice for deferred body close (not a silent failure)
- `io.ReadAll(resp.Body)` discards body content where it is explicitly not needed (MinIO userExists check reads and discards 404 body)

---

## TODO/FIXME Markers

None found. Zero matches for TODO, FIXME, HACK, XXX, STUB, MOCK, TEMP, WORKAROUND in any `.go` file under `/operators/`.

---

## Feature Flags

No feature flags found. The only conditional behavior is:
- `MinIOProvisioner == nil` — disables per-tenant MinIO isolation, uses global credentials. Documented, intentional.
- `cr.Spec.Scope.ArgoCD` — skips ArgoCD cluster secret creation when false. Intentional.

---

## Findings (Detailed)

### COMP-CTRL-001: PlatformBootstrapController callBootstrapEndpoint sends no auth token
- **Severity:** critical
- **Type:** missing_implementation
- **Location:** operators/internal/controller/platformbootstrap_api.go:64
- **Description:** The API's /internal/bootstrap endpoint requires `Authorization: Bearer <LOOPZERO_BOOTSTRAP_SECRET>`. The controller sends no auth header. The default secret is non-empty (`loopzero-dev-bootstrap-secret`). Bootstrap will fail with 401 in all deployments where LOOPZERO_BOOTSTRAP_SECRET is set.
- **Evidence:** platformbootstrap_api.go:64-78 has no header setting. api/internal/routes/bootstrap.go:70-80 requires auth. api/internal/config/config.go:178 has non-empty default.
- **Recommendation:** Add `BootstrapSecretRef` to `APIBootstrapSpec`, read it in `reconcileAPIPhase`, pass as `Authorization: Bearer <secret>` in `callBootstrapEndpoint`.

### COMP-CTRL-002: AppSecret CRD has no controller — secret sync to workload is unimplemented
- **Severity:** high
- **Type:** missing_feature
- **Location:** operators/api/v1/appsecret_types.go:1
- **Description:** AppSecret is a fully-specified CRD for syncing secrets from mgmt cluster to workload namespace. No controller exists. Secrets stored via `loopzero secret` reach the API/ESO but are never delivered to pod environments via this CRD path.
- **Evidence:** No AppSecretController file. Not wired in cmd/main.go.
- **Recommendation:** Implement in Sprint 6. Until then, document the gap in CLI output for `loopzero secret`.

### COMP-CTRL-003: ApplicationController EndpointsReady field never set
- **Severity:** medium
- **Type:** dead_code
- **Location:** operators/internal/controller/application_controller.go:234
- **Description:** `computePhase` returns `ApplicationPhaseServing` when `EndpointsReady == true`, but nothing ever sets this field to true. ApplicationPhaseServing is unreachable code.
- **Evidence:** Zero results for `EndpointsReady = true` in all operator code.
- **Recommendation:** Watch Endpoints/EndpointSlice resource, or remove the phase if not needed.

### COMP-CTRL-004: ReconciliationReport CRD is complete dead code
- **Severity:** medium
- **Type:** dead_code
- **Location:** operators/api/v1/reconciliationreport_types.go:1
- **Description:** Fully specified type, no controller, no callers, no sprint assignment.
- **Evidence:** Not in cmd/main.go. No controller file. No sprint in CLAUDE.md sprint table.
- **Recommendation:** Remove from codebase or assign to a sprint.

### COMP-CTRL-005: MinIO AdminProvisioner uses Basic Auth instead of AWS SigV4
- **Severity:** medium
- **Type:** partial_implementation
- **Location:** operators/internal/minio/provisioner.go:291
- **Description:** All MinIO Admin API calls use SetBasicAuth. MinIO Admin API requires SigV4. Per-tenant credential isolation (spec §17.2) cannot function with a real MinIO server.
- **Evidence:** provisioner.go:292 uses SetBasicAuth. No SigV4 implementation found.
- **Recommendation:** Use madmin-go library or implement SigV4 signing.

### COMP-CTRL-006: PreviewEnvironment CRD registered but no controller (Sprint 5 not started)
- **Severity:** medium
- **Type:** missing_feature (expected)
- **Location:** operators/api/v1/preview_types.go:1
- **Description:** Expected for unstarted Sprint 5. Risk: accidental object creation results in silent no-op.
- **Recommendation:** Add admission webhook to reject creates with clear message.

### COMP-CTRL-007: TenantController does not clean up MinIO user on tenant deletion
- **Severity:** low
- **Type:** missing_implementation
- **Location:** operators/internal/controller/tenant_controller.go:191
- **Description:** reconcileDelete cleans up ArgoCD, Gitea, build namespaces — but not the per-tenant MinIO user and policy. MinIO user credentials are orphaned.
- **Recommendation:** Add DeleteTenantCredentials to CredentialProvisioner interface and call it in reconcileDelete.

### COMP-CTRL-008: ClusterCredential ArgoCD cluster secret missing auth credentials
- **Severity:** low
- **Type:** partial_implementation
- **Location:** operators/internal/controller/clustercredential_controller.go:212
- **Description:** The ArgoCD cluster secret's config JSON only has `tlsClientConfig.insecure`. It lacks `bearerToken`, `caData`, `certData`, `keyData`. ArgoCD cannot authenticate to the target cluster. Raw kubeconfig key is not recognized by ArgoCD.
- **Recommendation:** Parse kubeconfig to extract auth fields and embed in config JSON per ArgoCD cluster secret schema.

---

## Recommendations (Prioritized)

1. **Fix PlatformBootstrapController auth gap (comp_ctrl_001)** — This is a hard break in the bootstrap flow. Add `BootstrapSecretRef` to the spec and pass auth on the POST request. Without this, the operator cannot bootstrap new installations.

2. **Fix MinIO SigV4 (comp_ctrl_005)** — Per-tenant credential isolation is a security feature (spec §17.2). Until SigV4 is implemented, the feature silently fails with 403 on real MinIO and tenant isolation is broken.

3. **Fix ClusterCredential ArgoCD secret auth fields (comp_ctrl_008)** — ArgoCD cluster registration won't work for auth without bearer token or cert/key in config JSON.

4. **Implement AppSecretController (comp_ctrl_002)** — Assign to Sprint 6. This is the missing link in the `loopzero secret` end-to-end flow for container apps (static frontends use ESO/CF KV differently).

5. **Set EndpointsReady or remove ApplicationPhaseServing (comp_ctrl_003)** — Low operational impact but creates confusion in health dashboards showing only Progressing or Reachable states.

6. **Add MinIO delete to tenant cleanup (comp_ctrl_007)** — Low priority but causes resource leakage on MinIO server over time.

7. **Remove ReconciliationReport or assign to sprint (comp_ctrl_004)** — Dead CRD adds schema noise with no benefit.

8. **PreviewEnvironment admission webhook (comp_ctrl_006)** — Optional guard for Sprint 5 gap.
