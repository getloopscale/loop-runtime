# Completeness Audit — Phase 2, Iteration 1
**Date:** 2026-04-26
**Scope:** `/home/paulo/loopzero/operators/`
**Auditor:** completeness-auditor (directed by chief-reviewer)
**Review depth:** Full — all 11 Phase 1 risk hypotheses investigated with direct code reading

---

## Executive Summary

8 findings registered. 2 high, 5 medium, 1 low. No critical findings.

The most significant structural defect is the **double dead end in preview environments**: the operator controller is written but never registered in main.go (COMP-001), AND the GitHub PR webhook handler that would trigger it is a no-op TODO (COMP-008). A user expecting preview environments on PRs will receive no preview URL, no error message, and no indication that the feature is non-functional. This affects advertised product capability.

The second structural concern is **Prometheus alert LoopZeroSecretSyncFailed monitoring a metric that is never emitted** (COMP-003) — the AppSecretOperator does not exist, so `loopzero_secret_sync_hash_mismatch_total` is never incremented. The alert is permanently silent, creating false confidence in secret monitoring coverage.

---

## Summary Table

| ID | Title | Severity | C4 | File | Line |
|----|-------|----------|----|------|------|
| COMP-001 | PreviewController completely unregistered | **HIGH** | completo | preview_controller.go | 213 |
| COMP-008 | GitHub PR webhook handler is a no-op | **HIGH** | completo | api/cmd/setup_github.go | 173 |
| COMP-002 | PreviewController bypasses state machine | medium | confiavel | preview_controller.go | 29–169 |
| COMP-003 | AppSecret CRD with no controller + silent alert | medium | completo | groupversion_info.go | 35 |
| COMP-005 | Build namespace NetworkPolicy allows unrestricted egress port 9000/6443 | medium | controlavel | tenant_build.go | 466 |
| COMP-006 | DOKS exec-auth rejection undocumented | medium | completo | clustercredential_controller.go | 256 |
| COMP-007 | httpclient TLS defaults InsecureSkipVerify=true | medium | controlavel | httpclient/client.go | 28 |
| COMP-004 | ReconciliationReport CRD deployed but unregistered | low | completo | groupversion_info.go | 39 |

---

## High Severity Findings

### COMP-001: PreviewController Completely Unregistered

**Claim in CLAUDE.md:** "PreviewEnvironment: Controller skeleton exists, no production logic (Sprint 5)."

**Reality:** The controller has a complete reconcile loop (Creating → Active → Expiring → Deleting phases, TTL management, namespace creation on runtime cluster, URL assignment). However `SetupPreviewController` (defined at `operators/internal/controller/preview_controller.go:213-224`) is **never called** from `cmd/main.go:setupControllers()` (lines 280-343).

`setupControllers` calls exactly 7 controllers: Tenant, ClusterCredential, Environment, Application, Exposure, GitRepositoryBinding, PlatformBootstrap. `SetupPreviewController` is absent.

**Impact:** No PreviewEnvironment CR will ever be reconciled by the running production binary. Preview namespaces are never created on the runtime cluster. Preview URLs are never assigned. TTL cleanup never runs. Any PreviewEnvironment CR created (e.g., by a future webhook implementation) will sit in the cluster forever, consuming etcd storage, with no status updates.

**Evidence:**
```go
// operators/cmd/main.go:279-343
// setupControllers wires all controllers to the manager.
func setupControllers(setupLog logr.Logger, mgr ctrl.Manager, deps provisionerDeps) {
    // SetupTenantController — ✓
    // SetupClusterCredentialController — ✓
    // SetupEnvironmentController — ✓
    // SetupApplicationController — ✓
    // SetupExposureController — ✓
    // SetupGitRepositoryBindingController — ✓
    // SetupPlatformBootstrapController — ✓
    // SetupPreviewController — MISSING
}

// operators/internal/controller/preview_controller.go:213
func SetupPreviewController(mgr ctrl.Manager, runtimeClient client.Client, baseDomain string) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&loopzerov1.PreviewEnvironment{}).
        Named("preview").
        Complete(&PreviewReconciler{...})
}
```

**Recommendation:** Either (a) call `SetupPreviewController(mgr, deps.runtimeClient, baseDomain)` from `setupControllers()` as part of Sprint 5, or (b) remove the controller from the binary entirely and gate it behind `LOOPZERO_PREVIEW_ENABLED` feature flag. Do not leave an unregistered controller silently doing nothing.

**Effort:** High (Sprint 5 work)

---

### COMP-008: GitHub PR Webhook Handler is a No-Op

**Reality:** The API wires a `OnPullRequest` handler in `api/cmd/setup_github.go:173-182` that logs the event and immediately returns nil with a TODO comment:

```go
wh.OnPullRequest(func(event ghwebhook.PullRequestEvent) error {
    logger.Info("github webhook: pull_request received",
        "repo", event.Repository.FullName,
        "action", event.Action,
        "pr", event.Number,
    )
    // TODO: Preview environments — deploy PR branch
    return nil
})
```

Combined with COMP-001, the full preview flow is entirely non-functional:
- GitHub PR opened → webhook received → logged → **no PreviewEnvironment CR created**
- PreviewEnvironment CR → **not reconciled** (controller not registered)
- User sees: nothing (no preview URL, no error, no indication)

**Impact:** This is the API-side confirmation of the double dead end. Both the trigger (webhook) and the executor (operator) are non-functional.

**Effort:** High (Sprint 5 work)

---

## Medium Severity Findings

### COMP-002: PreviewController Bypasses State Machine

The preview controller imports `statemachine.NewPreviewMachine` via blank identifier:

```go
// operators/internal/controller/preview_controller.go:29
var _ = statemachine.NewPreviewMachine
```

This is a compile-time anchor — it forces the statemachine package to be compiled but does nothing at runtime. The machine is never instantiated. All 4 phase transitions are direct assignments:

- Line 79: `preview.Status.Phase = loopzerov1.PreviewPhaseCreating` (initialization)
- Line 133: `preview.Status.Phase = loopzerov1.PreviewPhaseActive` (after namespace creation)
- Line 155: `preview.Status.Phase = loopzerov1.PreviewPhaseExpiring` (TTL expired)
- Line 169: `preview.Status.Phase = loopzerov1.PreviewPhaseDeleting` (in reconcileExpiring)

This violates CLAUDE.md Rule 8: "Transitions go through the state machine functions. Direct status updates are forbidden."

**Recommendation:** When implementing Sprint 5, instantiate `pkg/statemachine.PreviewMachine` in `PreviewReconciler` and route all transitions through it.

**Effort:** Medium

---

### COMP-003: AppSecret CRD With No Controller + Silent Prometheus Alert

**Three-part finding:**

1. **AppSecret CRD deployed:** `charts/loopzero-crds/templates/appsecrets.yaml` installs the CRD. `charts/loopzero-crds/values.yaml:18-19` has `appsecrets.enabled=true`.

2. **No controller:** No `appsecret_controller.go` exists. AppSecret and AppSecretList are intentionally excluded from the runtime scheme (`groupversion_info.go:35-36`). A user creating an AppSecret CR will have it accepted by the apiserver (CRD exists) but nothing will ever reconcile it.

3. **Permanently silent Prometheus alert:** `LoopZeroSecretSyncFailed` alert in `infra/helm/monitoring/manifests/prometheusrule-warning.yaml:53-60` uses metric `loopzero_secret_sync_hash_mismatch_total`. This metric is **never emitted** by any Go code in the repository (`grep -rn loopzero_secret_sync returns zero results`). The alert fires on a metric that does not exist — it will never trigger even if secret sync is broken.

**Impact:** Operators believing they have secret sync monitoring are wrong. Secret sync failures go undetected.

**Recommendation:** (1) Register `loopzero_secret_sync_hash_mismatch_total` in `operators/internal/metrics/` when AppSecretController is implemented. (2) Until then, remove or comment out the LoopZeroSecretSyncFailed alert to prevent false confidence.

**Effort:** High (for controller implementation)

---

### COMP-005: Build NetworkPolicy Allows Unrestricted Egress on Ports 9000 and 6443

`tenant_build.go:reconcileBuildNetworkPolicy()` creates egress rules for build namespaces. Two rules have no `To:` destination selector:

```go
// Port 9000 (MinIO) — no To: field = any destination
{Ports: []networkingv1.NetworkPolicyPort{
    {Protocol: &protoTCP, Port: &minioPort},
}},
// Port 6443 (K8s API) — no To: field = any destination
{Ports: []networkingv1.NetworkPolicyPort{
    {Protocol: &protoTCP, Port: &k8sAPIPort},
}},
```

Contrast with the registry rule which correctly restricts to `RegistryCIDRs` when configured.

**Impact:** A malicious Dockerfile (or a compromised base image) can exfiltrate source code, environment variables, or secrets by sending data to any external server listening on port 9000 or 6443. The code comment at line 427 says "Prevents source code exfiltration via malicious Dockerfiles" but this is only true for port 443 when `RegistryCIDRs` is configured.

**Recommendation:** Add `To:` selectors to restrict MinIO egress to the MinIO service namespace/pod and K8s API egress to the cluster API server endpoint. Apply the same CIDR-restriction pattern used for registry port 443.

**Effort:** Medium

---

### COMP-006: ClusterCredential Rejects DOKS Default Kubeconfigs — Undocumented

`clustercredential_controller.go:256-257` hard-rejects kubeconfigs without a bearer token or client certificate:

```go
if token == "" && len(restConfig.CertData) == 0 {
    return fmt.Errorf("kubeconfig has no bearer token or client certificate — exec-based auth not supported for ArgoCD cluster secrets")
}
```

The comment at lines 242-243 says this "Properly handles bearer tokens, client certs, and CA data from any valid kubeconfig format (including exec-based auth used by DOKS default kubeconfigs)" — which is directly contradicted by the code that follows. DOKS default kubeconfigs use exec-based auth via `doctl` and will fail this check.

**Impact:** Adding a DOKS cluster as a ClusterCredential with its default kubeconfig will fail silently (error in operator logs only, not surfaced to user as CR status condition). Users must manually generate a static-token kubeconfig, which is undocumented.

**Recommendation:** (1) Fix the misleading comment. (2) Surface the error as a CR status condition with `SetCondition`. (3) Document the static-token generation procedure.

**Effort:** Low

---

### COMP-007: httpclient TLS Verification Defaults to InsecureSkipVerify=true

`operators/internal/httpclient/client.go:skipTLSVerify()` returns `true` (skip verification) unless `OPERATOR_INSECURE_SKIP_TLS` is explicitly set to `"false"` or `"0"`:

```go
func skipTLSVerify() bool {
    v := strings.ToLower(os.Getenv("OPERATOR_INSECURE_SKIP_TLS"))
    if v == "false" || v == "0" {
        return false
    }
    return true  // default: insecure
}
```

**Mitigating context:** The Pulumi production deployment (`infra/pulumi-control-plane/src/cp-tenant-controller.ts:180`) correctly sets `OPERATOR_INSECURE_SKIP_TLS=false`. However the Tilt dev deployment explicitly sets it to `true`, and any new deployment environment (staging, DR, new cells) that omits this variable will silently accept any certificate.

**Recommendation:** Invert the default — return `false` unless `OPERATOR_INSECURE_SKIP_TLS` is explicitly `"true"` or `"1"`. This is the secure-by-default pattern. Update Tilt dev deployment to explicitly opt into insecure mode.

**Effort:** Low

---

## Low Severity Findings

### COMP-004: ReconciliationReport CRD Deployed but Unregistered in Scheme

`charts/loopzero-crds/values.yaml:16-17` has `reconciliationreports.enabled=true` which installs the CRD. However `groupversion_info.go:39` explicitly excludes it:

```go
// ReconciliationReport: intentionally unregistered — dead code, no controller, no sprint.
// Types retained in source for future drift-detection work.
```

No controller exists. The CRD installs cleanly but is never used. This wastes apiserver storage and creates operational confusion.

**Recommendation:** Set `reconciliationreports.enabled=false` in `charts/loopzero-crds/values.yaml` until a controller is built. The source types can remain for future work as documented.

**Effort:** Low

---

## Phase 1 Risk Hypothesis Verification

| RH | Hypothesis | Verified? | Severity | Finding |
|----|-----------|-----------|----------|---------|
| RH-001 | PreviewController not wired | **CONFIRMED** | High | COMP-001 |
| RH-002 | State machine bypassed in preview | **CONFIRMED** | Medium | COMP-002 |
| RH-003 | AppSecret no controller | **CONFIRMED** | Medium | COMP-003 |
| RH-004 | ReconciliationReport no controller | **CONFIRMED** | Low | COMP-004 |
| RH-005 (now RH-006) | NetworkPolicy egress ports 9000/6443 | **CONFIRMED** | Medium | COMP-005 |
| RH-006 (now RH-004) | DOKS exec-auth rejection | **CONFIRMED** | Medium | COMP-006 |
| N/A (new) | httpclient TLS default insecure | **NEW FINDING** | Medium | COMP-007 |
| N/A (new) | PR webhook handler no-op | **NEW FINDING** | High | COMP-008 |

All 6 Phase 1 risk hypotheses targeted for Phase 2 were confirmed. 2 additional findings emerged from the investigation.

---

## What Was NOT Found (Negative Findings)

- No TODO/FIXME/HACK/XXX/STUB markers in any non-test Go file under `operators/` — `grep -rn -E "(TODO|FIXME|HACK|XXX|STUB|panic.*not.*implemented)" operators --include="*.go" | grep -v _test.go` returned zero results.
- No swallowed errors (`_ = err` pattern) in business logic — only HTTP response body closes which is correct practice.
- No `defer func() { _ = r.Update(...) }` patterns — status updates are explicit and their errors are returned.
- Tenant, Environment, Application, Exposure, ClusterCredential, GitRepositoryBinding, PlatformBootstrap controllers are all wired and have corresponding test files.
- Circuit breaker pattern is consistently applied across all 4 external provisioners (Gitea, ArgoCD, MinIO, Harbor).
- No silent failures in tenant deletion flow — all step errors are returned and trigger requeue.

---

## Recommendations by Priority

1. **COMP-001 + COMP-008 (Sprint 5):** Wire `SetupPreviewController` in `main.go` AND implement the PR webhook handler. Both changes are needed for end-to-end preview environment functionality.

2. **COMP-005 (Security):** Add `To:` selectors to MinIO and K8s API egress rules in build NetworkPolicy. This is a one-file change with high security impact.

3. **COMP-007 (Security):** Invert TLS default in `httpclient/client.go`. One-line change with significant security posture improvement.

4. **COMP-003 (Observability):** Remove the `LoopZeroSecretSyncFailed` alert until AppSecretController is implemented and the metric is registered.

5. **COMP-006 (Documentation):** Fix misleading comment, surface error as CR condition, add runbook.

6. **COMP-002 (Technical debt):** Route preview phase transitions through state machine when Sprint 5 is implemented.

7. **COMP-004 (Housekeeping):** Set `reconciliationreports.enabled=false` in Helm values.
