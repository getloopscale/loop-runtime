# Risk Hypotheses — Phase 1 Baseline

These are testable, evidence-grounded hypotheses derived from the baseline code reading. Each will be targeted by specific review phases (2-6).

---

## RH-001: PreviewController Completely Inoperable

**Evidence:** `cmd/main.go:setupControllers()` calls SetupTenantController, SetupClusterCredentialController, SetupEnvironmentController, SetupApplicationController, SetupExposureController, SetupGitRepositoryBindingController, SetupPlatformBootstrapController — but NOT SetupPreviewController.

**Evidence:** `preview_controller.go:28`: `var _ = statemachine.NewPreviewMachine` — blank identifier, machine never instantiated.

**Hypothesis:** No PreviewEnvironment CR will ever be reconciled in the running production binary. The controller code exists but is dead. Any PR preview feature advertised to users is entirely non-functional.

**Testable in Phase 2 (Completeness):** Confirm SetupPreviewController is absent from main.go. Confirm no integration test exercises the preview flow end-to-end.

**Severity estimate:** High — documented feature is inoperable.

---

## RH-002: AppSecret CRD Has No Controller — ESO-Only Workaround

**Evidence:** `operators/api/v1/appsecret_types.go` defines the AppSecret CRD. No `appsecret_controller.go` exists in `internal/controller/`. CLAUDE.md states: "AppSecret controller not implemented — secrets work via ESO directly."

**Hypothesis:** Applications relying on AppSecret lifecycle automation (creation, rotation, deletion) have no automated lifecycle management. If a user deletes an AppSecret CR, the actual secret in ESO is not cleaned up. Cross-tenant secret leaks are possible if ESO ClusterSecretStore grants are not scoped correctly.

**Testable in Phase 4 (Code) and Phase 6 (Security):** Check ESO ClusterSecretStore RBAC scoping. Check if AppSecret deletion leaves orphaned ESO resources.

**Severity estimate:** Medium — workaround exists but lifecycle is unmanaged.

---

## RH-003: Tenant Deletion Retry Counter Stored in Annotation — Race with Concurrent Reconciles

**Evidence:** `tenant_deletion.go`: retry count stored in `constants.AnnotationDeletionRetryCount` annotation via `r.Update(ctx, tenant)`. If two concurrent reconciles (MaxConcurrentReconciles=5) both read the annotation, increment, and write, one write wins and the other's increment is lost.

**Hypothesis:** The deletion retry counter may undercount in concurrent scenarios, extending the time before the stuck-alert threshold is reached. With MaxConcurrentReconciles=5, concurrent deletion reconciles for the same tenant (unlikely but possible on requeue storms) could produce annotation write conflicts.

**Testable in Phase 4 (Code):** Check if r.Update uses ResourceVersion conflict detection. Review whether a single tenant can have multiple concurrent reconciles in the deletion path.

**Severity estimate:** Low — race is unlikely in practice but represents a correctness issue.

---

## RH-004: ClusterCredentialController Rejects DOKS Default Kubeconfigs

**Evidence:** `clustercredential_controller.go:reconcileArgoCDClusterSecret()`: "kubeconfig has no bearer token or client certificate — exec-based auth not supported for ArgoCD cluster secrets". DOKS (DigitalOcean Kubernetes Service) default kubeconfigs use exec-based auth via `doctl`.

**Hypothesis:** Adding a DOKS cluster as a ClusterCredential using its default kubeconfig will fail with this error. Users must generate a static token kubeconfig manually (e.g., create a ServiceAccount + ClusterRoleBinding + get token). This is an undocumented operational constraint that will surface as a confusing error message in production.

**Testable in Phase 2 (Completeness) and Phase 7 (Dogfood):** Check if documentation covers this. Check if any test exercises the exec-auth rejection path.

**Severity estimate:** Medium — operational friction for DOKS deployments.

---

## RH-005: ExposureController Probe Creates External HTTP Traffic from Operator Pod

**Evidence:** `exposure_controller.go:performProbe()` makes live HTTP GET requests to `https://{hostname}` from within the operator pod. No timeout guard beyond `ProbeTimeout` constant. No circuit breaker on the probe HTTP client.

**Hypothesis:** If an application hostname resolves to a slow or unresponsive endpoint, the operator reconcile goroutine is held waiting for ProbeTimeout. With many ExposedRoutes, this could exhaust the reconcile worker pool. If a hostname is external (e.g., custom domain before DNS propagation), the operator generates external traffic on every reconcile cycle.

**Testable in Phase 4 (Code):** Check ProbeTimeout value. Check reconcile worker pool size. Verify behavior when DNS does not resolve (connection refused vs timeout).

**Severity estimate:** Medium — potential for reconcile pool starvation under load.

---

## RH-006: Build Namespace NetworkPolicy Allows Unrestricted MinIO and K8s API Egress

**Evidence:** `tenant_build.go:reconcileBuildNetworkPolicy()`: egress rules allow port 9000 (MinIO) to ALL destinations and port 6443 (K8s API) to ALL destinations. RISK-007 restriction applies only to registry port 443 when RegistryCIDRs is configured.

**Hypothesis:** A malicious Dockerfile can exfiltrate data via port 9000 (to any external server listening on that port) or port 6443. The registry CIDR restriction (RISK-007) does not cover these ports. This partially undermines the build isolation intent documented in spec §17.1.

**Testable in Phase 6 (Security):** Verify egress rules. Check if internal MinIO IP is restricted or if any external server could receive traffic on port 9000.

**Severity estimate:** Medium — partial bypass of build egress controls.

---

## RH-007: Compiled Binary Committed to Repository Root

**Evidence:** `/home/paulo/loopzero/operators/tenant-controller` is an ELF 64-bit executable (confirmed via `file` command). It is committed to the repository alongside Go source code.

**Hypothesis:** (a) The binary may be stale relative to the source code, causing confusion about what version is deployed. (b) CI/CD pipelines may inadvertently deploy this binary instead of building from source. (c) The binary contains debug symbols (`not stripped`) that expose internal function names and type layouts.

**Testable in Phase 5 (Infrastructure) and Phase 6 (Security):** Check .gitignore for binary exclusion. Check build pipelines. Verify binary matches current source.

**Severity estimate:** Low-Medium — supply chain concern.

---

## RH-008: PlatformBootstrapController Cleanup Checks Managed-By Label But Bootstrap Is One-Time

**Evidence:** `platformbootstrap_controller.go`: cleanup path deletes platform-tokens Secret only if `tokenSecret.Labels[LabelManagedBy] == "platformbootstrap-controller"`. This label is set by the controller when creating the Secret.

**Hypothesis:** If platform-tokens Secret is manually patched (e.g., a rotated token written directly), the managed-by label may not be present, causing cleanup to be skipped. The Secret persists after PlatformBootstrap CR deletion, containing active ArgoCD credentials.

**Testable in Phase 4 (Code):** Check if platform-tokens Secret creation always sets the managed-by label. Check test coverage of cleanup path.

**Severity estimate:** Low — Secret retention after CR deletion is a security concern but limited blast radius (argocd credentials already in cluster).

---

## RH-009: Environment NetworkPolicy IngressNamespace Is Read from Env Var at Setup — Not Dynamically

**Evidence:** `environment_controller.go:SetupEnvironmentController()`: `ingressNS := os.Getenv("INGRESS_NAMESPACE")`. This value is set once at startup and used for all NetworkPolicy reconciles.

**Hypothesis:** If Traefik is moved to a different namespace (or the env var changes), existing NetworkPolicies allow-traefik rules will be stale and block legitimate traffic. No mechanism to detect or correct namespace drift for this specific value.

**Testable in Phase 4 (Code) and Phase 5 (Infrastructure):** Check if NetworkPolicy reconcile uses the stored IngressNamespace field or re-reads env var. Check if environment controller re-reconciles this on operator restart.

**Severity estimate:** Low — operational config drift issue.

---

## RH-010: pkg/errors (LoopZeroError) Not Used in Controllers

**Evidence:** Grep of all controller files shows zero imports of `github.com/loopzero/loopzero/pkg/errors`. All error wrapping uses `fmt.Errorf("step description: %w", err)`.

**Hypothesis:** Errors from operator reconcile paths are not typed with LoopZeroError codes. This means monitoring/alerting that queries for specific error types (by code) will not work for operator-originated errors. Log correlation is harder.

**Testable in Phase 4 (Code):** Confirm no LoopZeroError usage in any controller or provisioner. Assess impact on error observability.

**Severity estimate:** Low — observability gap, not a functional defect.

---

## RH-011: TenantController runSteps Is Sequential — Single Step Failure Aborts All Subsequent Steps

**Evidence:** `tenant_controller.go:runSteps()`: iterates steps sequentially and returns on first error. A Gitea failure prevents ArgoCD app creation. An ArgoCD failure prevents build namespace provisioning.

**Hypothesis:** Transient failures in early steps (e.g., Gitea briefly unreachable) cause all 7 reconcile steps to be skipped, including critical ones like reconcileEnvironmentCRs. While the circuit breaker protects against sustained failures, brief transients will cascade. Under high load with many Tenants being provisioned, a brief Gitea outage causes all tenants to fail entirely (not just git step).

**Testable in Phase 4 (Code) and Phase 8 (Chaos — if scope):** Test what happens when Gitea fails during a multi-step reconcile. Check if partial completion is preserved.

**Severity estimate:** Low-Medium — operational resilience concern.

---

## Summary Table

| ID | Area | Severity | Phase to Test |
|----|------|----------|---------------|
| RH-001 | Completeness | High | Phase 2, Phase 7 |
| RH-002 | Completeness/Security | Medium | Phase 2, Phase 6 |
| RH-003 | Code Correctness | Low | Phase 4 |
| RH-004 | Completeness/Ops | Medium | Phase 2, Phase 7 |
| RH-005 | Code/Operational | Medium | Phase 4 |
| RH-006 | Security | Medium | Phase 6 |
| RH-007 | Security/Infrastructure | Low-Medium | Phase 5, Phase 6 |
| RH-008 | Security | Low | Phase 4 |
| RH-009 | Operational | Low | Phase 4, Phase 5 |
| RH-010 | Observability | Low | Phase 4 |
| RH-011 | Operational Resilience | Low-Medium | Phase 4 |
