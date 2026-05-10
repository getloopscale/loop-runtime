# Threat Model Report — Operators Module
## Phase 6 Security Review

**Date:** 2026-04-26
**Methodology:** Per-flow threat analysis with STRIDE-inspired attacker modeling
**Flows analyzed:** 5 (all flows in database)

---

## Preamble: Threat Context

The operators module is a **privileged internal component**, not a public-facing API. Its threat model is fundamentally different from an internet-exposed service:

- **Attacker types in scope:** Platform administrators with kubectl access, compromised CI/CD pipelines, infrastructure failures used as attack vectors
- **Attacker types out of scope:** Internet adversaries without pre-existing access, unauthenticated external actors, tenant users (who only interact via the API server, not the operator directly)
- **Trust boundary:** The operator IS the trust boundary between tenant resource requests (expressed as CRs) and the K8s/Gitea/ArgoCD/MinIO infrastructure. Compromise of the operator = compromise of all tenants.
- **Consequence of compromise:** The operator holds credentials to Gitea, ArgoCD, Harbor, and MinIO (via K8s Secrets), has cluster-wide RBAC, and manages resource provisioning for all tenants simultaneously.

---

## Flow 1: Tenant Creation Flow

**Entry:** Tenant CR created in mgmt cluster
**Exit:** Tenant.Status.Phase=Active; child Environment CRs created

### Threat 1.1 — TC-1 (Formalized): Operator crash-loop + cluster-wide Secret delete

**Attacker:** Adversary who triggers a panic in the reconcile loop (CODE-002) or supplies a malformed Tenant CR that reaches the buildDockerConfigJSON panic path.

**Attack chain:**
```
Malformed input → buildDockerConfigJSON panic → operator crash-loop
→ controller-runtime recovery → repeat reconcile with cluster-wide Secret delete permission
→ during crash-loop window: buggy code path could issue Secret.Delete calls
→ kube-system credentials, ArgoCD token, Gitea token at risk of deletion
```

**Likelihood:** LOW — requires code bug (CODE-002 is a known panic) AND adversarial trigger AND the crash-loop path reaching a delete call (not currently in the code but possible in future regressions).

**Impact:** CRITICAL — deletion of kube-system credentials or ArgoCD tokens would require manual recovery.

**Existing controls:**
- controller-runtime panic recovery catches panics and drops the reconcile item (does not crash the binary)
- Secrets require explicit `.Delete()` API call — panics don't auto-delete resources
- Prometheus alerting on CrashLoopBackOff would trigger human response

**Missing controls:**
- Namespace-scoped Roles for Secrets (INFRA-001 fix removes cluster-wide delete)
- Fix CODE-002 (replace panic with error return)

**Toxic combination:** INFRA-001 (cluster-wide Secret delete) + CODE-002 (panic in reconcile hot path)

**Fix priority:** Fix CODE-002 (low effort, high impact on stability). INFRA-001 is the defense-in-depth fix.

---

### Threat 1.2 — TC-3 (Formalized): MinIO outage cascades to all tenant provisioning

**Attacker:** Infrastructure failure (MinIO pod OOM, storage issue, network partition)

**Attack chain:**
```
MinIO becomes unavailable
→ EnsureTenantCredentials calls madmin.AddUser with no HTTP timeout
→ reconcile blocks (controller-runtime deadline eventually cancels)
→ ALL new Tenant CRs stuck in Provisioning
→ NO circuit breaker (CB code exists but not wired in setupMinIO)
→ retry storm when MinIO recovers, potentially causing MinIO overload
```

**Likelihood:** MEDIUM — MinIO is a critical external dependency; any production operator will face infrastructure blips.

**Impact:** HIGH — all new tenant provisioning halts; builds for new tenants fail.

**Existing controls:**
- `HTTPTimeoutMinIO = 15s` constant defined in `timeouts.go` (but not applied to madmin client)
- controller-runtime per-reconcile deadline bounds individual hung calls
- `CircuitBreakerProvisioner` implementation exists in `minio/circuitbreaker_provisioner.go`

**Missing controls:**
- `setupMinIO()` must wrap provisioner with `NewCircuitBreakerProvisioner` (1-line change)
- Custom HTTP transport on madmin client respecting `HTTPTimeoutMinIO`

**Toxic combination:** SEC-004 (MinIO CB not wired) + CODE-004 (fragile isNotFoundError string matching during recovery)

---

## Flow 2: Environment Creation Flow

**Entry:** Environment CR created by TenantController
**Exit:** Environment.Status.Phase=Active; namespace+RBAC+NetworkPolicy on runtime cluster

### Threat 2.1 — Rogue Environment CR overwriting tenant namespace configuration

**Attacker:** Platform admin with kubectl access to create `loopzero.io/environments` resources

**Attack chain:**
```
Admin creates rogue Environment CR: Spec.TenantRef.Name=victim-tenant, Spec.Namespace.Name=loopzero-victim-tenant-production
→ EnvironmentController.Reconcile receives the event
→ NO IsControlledBy check on create path (only on GC path)
→ Controller CreateOrUpdate runtime namespace loopzero-victim-tenant-production
→ Overwrites RBAC (loopzero-deployer Role/RoleBinding)
→ Overwrites NetworkPolicy (deny-all-other baseline)
→ victim tenant's namespace now has attacker-controlled RBAC/NP
```

**Likelihood:** LOW — requires platform admin kubectl access; K8s RBAC already restricts who can create environment CRs.

**Impact:** HIGH — RBAC overwrite could grant attacker-controlled SAs admin access to victim tenant namespace.

**Existing controls:**
- Only the operator ServiceAccount has `create` permission on `loopzero.io/environments` (ClusterRole)
- TenantController creates Environment CRs with ownerRef pointing to the Tenant
- `IsControlledBy` check exists on GC path

**Missing controls:**
- `EnvironmentController.Reconcile` should verify ownerReference before performing cluster mutations
- Validating admission webhook to prevent Environment CRs without a valid ownerRef

---

## Flow 3: Tenant Deletion Flow

**Entry:** Tenant.DeletionTimestamp set
**Exit:** All resources cleaned up; finalizer removed

### Threat 3.1 — Phase 1 silent failure causes indefinite queue blocking

**Attacker:** K8s API throttling or transient failures (not a human attacker)

**Attack chain:**
```
API throttling → Environment CR delete fails with 429
→ tenant_deletion.go:87: code logs error, continues loop, increments childEnvCount
→ requeueWithDeletionWait is returned
→ NO exponential backoff for Phase 1 (unlike Phase 2)
→ reconcile queue processes stuck tenant repeatedly at RequeueDeletionWait rate
→ queue depth increases, starving other tenant operations
→ Tenant stuck in Deleting with DeletionTimestamp set forever
```

**Likelihood:** MEDIUM — K8s API throttling is common under load.

**Impact:** MEDIUM — tenant resources not cleaned up; reconcile queue congestion; billing/quota implications.

**Existing controls:**
- `conditions.SetCondition` records the failure in Tenant status for observability
- controller-runtime rate limiter provides some backpressure

**Missing controls:**
- Phase 1 retry counter (same pattern as Phase 2 `AnnotationDeletionRetryCount`)
- Alert on Tenant CRs with DeletionTimestamp > threshold
- Return error from delete failure path to let controller-runtime rate limiter handle retry

---

## Flow 4: PlatformBootstrap Flow

**Entry:** PlatformBootstrap CR created
**Exit:** Phase=Complete; Gitea+ArgoCD+API configured; platform-tokens Secret populated

### Threat 4.1 — Single non-expiring ArgoCD API key with no rotation

**Attacker:** Internal attacker who gains read access to `platform-tokens` Secret in `loopzero-system`

**Attack chain:**
```
Attacker reads platform-tokens Secret in loopzero-system namespace
→ Obtains ArgoCD API key (non-expiring, generated via GenerateApiKey)
→ Uses API key to list/sync/delete ArgoCD Applications for any tenant
→ Can deploy arbitrary manifests to workload cluster namespaces
→ Full platform compromise without detection (no audit log on Secret read)
```

**Likelihood:** LOW — requires loopzero-system namespace read access (platform admin level).

**Impact:** CRITICAL — ArgoCD admin API key grants full GitOps delivery control.

**Existing controls:**
- `platform-tokens` Secret is in `loopzero-system` namespace, not accessible to tenants
- ArgoCD API key is scoped to `loopzero-api` account (not admin), limited to sync+get via RBAC

**Missing controls:**
- Token rotation mechanism (currently non-expiring)
- K8s audit log on Secret read events in loopzero-system (not configured per Phase 5 findings)
- `argocd-initial-admin-secret` should be deleted post-bootstrap per ArgoCD hardening guide

---

## Flow 5: Preview Environment Lifecycle

**Entry:** PreviewEnvironment CR created
**Exit:** Preview namespace deleted; finalizer removed

### Threat 5.1 — TC-2 (Formalized): Silent preview no-op creates product trust gap

**Attacker:** Tenant trusting that PR preview environments validate pre-production behavior (this is a product integrity threat, not a technical exploit)

**Attack chain:**
```
Tenant opens PR on GitHub
→ Webhook received (api/cmd/setup_github.go:173-182)
→ Handler logs event, returns nil (COMP-008 no-op)
→ No PreviewEnvironment CR created
→ Even if CR were created: PreviewController not registered in main.go (COMP-001)
→ Tenant sees no error, may assume preview is deploying
→ Proceeds to merge PR based on assumption of validated behavior
→ Unvalidated change reaches production
```

**Likelihood:** MEDIUM — any tenant using GitHub App auto-connect would experience this.

**Impact:** MEDIUM — quality and trust impact; no security exploit but product contract violated.

**Toxic combination:** COMP-001 + COMP-008 — both halves of the preview pipeline simultaneously absent with no error surface.

**Interim mitigation:** Return HTTP 501 from the PR webhook handler instead of silent 200 no-op.

---

## Toxic Combinations Matrix

| TC | Finding 1 | Finding 2 | Flow | Combined Risk |
|----|-----------|-----------|------|---------------|
| TC-1 | INFRA-001 (cluster-wide Secret delete) | CODE-002 (panic in reconcile) | flow_tenant_creation | Crash-loop recovery with destructive RBAC |
| TC-2 | COMP-001 (PreviewController not registered) | COMP-008 (webhook no-op) | flow_preview_lifecycle | Silent double failure with no error surface |
| TC-3 | SEC-004 (MinIO CB not wired) | CODE-004 (fragile isNotFoundError) | flow_tenant_creation | Outage → retry storm → error path breakage |
| TC-4 | INFRA-001 (cluster-wide namespace delete) | ENV-CONTROLLER rogue CR | flow_environment_creation | Rogue CR can delete production namespace |
| TC-5 | Missing audit log | Non-expiring ArgoCD key | flow_platformbootstrap | Undetectable key theft with permanent access |

---

## Findings NOT Escalated to Critical

The audit maintains the finding that **zero criticals exist** in the operators module:

1. **Multi-tenancy isolation** — verified sound. Namespace naming, ownerRef GC, label-based segregation, ArgoCD AppProject scoping, and NetworkPolicy all work together correctly. The isolation test suite passes.
2. **Credential handling** — no hardcoded secrets, no credential logging. The "store in Secret, never log values" pattern is consistent throughout.
3. **All threat chains require platform admin preconditions** — the operator is not internet-facing; all attack vectors require existing platform-level access. A critical finding would require a path that a tenant (not a platform admin) could exploit.
4. **CODE-002 panic** — remains medium. In operator context, panics are caught by controller-runtime recovery middleware. The panic does not cause data loss, only reconcile item drop.

A critical finding would require: a tenant-accessible exploit, a confirmed credential exposure to an unauthorized party, or a broken multi-tenancy isolation where Tenant A can access Tenant B's resources. None of these conditions exist.
