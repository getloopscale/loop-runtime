# Security Review — Phase 6
## Operators Module Security Audit

**Date:** 2026-04-26
**Phase:** 6 / 7
**Reviewer:** Chief Reviewer + Security Auditor + Threat Modeler
**Scope:** `/home/paulo/loopzero/operators/` — all controllers, provisioners, CRD types, Helm charts

---

## Executive Summary

The operators module implements strong multi-tenant isolation through namespace naming, ownerRef-based garbage collection, and label-based segregation. No hardcoded credentials were found. No credential leakage in log statements was found. The token handling follows a "store in Secret, never log values" pattern throughout.

**Critical findings: 0**
**High findings (security category): 0**
**Medium findings: 3 (SEC-001, SEC-002, SEC-003)**
**Low findings: 3 (SEC-004, SEC-005, SEC-006)**

The audit found no evidence of remote code execution vectors, broken multi-tenancy isolation, or hardcoded secrets. The threat surface is primarily limited to platform administrators — not to tenant users. The two most important fixes are the TLS default inversion (SEC-001) and the MinIO circuit breaker gap (SEC-004, already partially captured in ARCH-001).

---

## Section 1: Hardcoded Secret Scan

**Methodology:** `grep -rn "password\|api[_-]key\|token" --include="*.go" --include="*.yaml"` with filtering for string literal assignments.

**Result: CLEAN**

- No hardcoded credentials found in production Go code.
- Test files contain expected stub values (`"admin-password"`, `"argocd-admin-password"` at `platformbootstrap_controller_test.go:166,183,276,385`) — these are test fixtures in non-production code and are acceptable.
- All production credential access follows the K8s Secret pattern: read from `corev1.Secret.Data[key]` at runtime.

---

## Section 2: Credential Leak in Log Statements

**Methodology:** Grep for all `log.*`, `logger.*`, `slog.*` statements containing sensitive key names.

**Result: CLEAN**

All log statements containing secret-adjacent words reference secret **names and namespaces** (acceptable), not secret **values**:

- `logger.Info("deleted ArgoCD repository secret", "secret", secretName, "namespace", consumerNs)` — logs the Secret resource name, not the token content.
- `slog.Info("harbor build robot credentials stored", "tenant", tenant.Name, "robot", buildCreds.Username)` — logs the robot username (not secret).
- `platformbootstrap_gitea.go:107`: `log.Info("ListTokens failed", "error", err, "tokenName", tokenName)` — logs token name, not value.
- `requeueWithRetry` error messages embed error strings from the Secret access path but do not include credential values.

**One exception to note (low risk):** `platformbootstrap_argocd.go:195-204` — error messages include `account %s may not exist` with the account name, not credential values. Acceptable.

---

## Section 3: TLS Configuration

**Finding SEC-001:** `operators/internal/httpclient/client.go:22-52`

The shared HTTP client factory (`httpclient.New()` and `httpclient.NewWithTimeout()`) has `InsecureSkipVerify: skipTLSVerify()` where `skipTLSVerify()` returns `true` by default unless `OPERATOR_INSECURE_SKIP_TLS` is explicitly `"false"` or `"0"`.

**Production mitigation present:** The Helm chart `charts/loopzero-tenant-controller/values.yaml` sets `insecureSkipTls: "false"` as default, flowing into the ConfigMap. Production Helm-managed deployments correctly get InsecureSkipVerify=false.

**Gap:** Non-Helm deployments (Tilt, manual `kubectl apply`, CI debugging) default to TLS skip. The Tilt manifest comments warn about this but comments are not enforcement.

**Positive control:** Harbor client (`harbor/client.go:58-67`) uses its own http.Client with Go defaults (TLS verification ON). This is a consistency gap (CODE-003) but actually means Harbor is more secure than the httpclient default.

---

## Section 4: CRD Input Validation

**Finding SEC-002:** `operators/api/v1/tenant_types.go:85-91`

The `TenantEnvironmentEntry` struct has no `+kubebuilder:validation` markers on `Name` or `Cluster`. Compare with `DisplayName` (line 54-56) which has `MinLength=1 + MaxLength=128 + Pattern` — the SEC-011 comment explicitly documents this was done for injection prevention.

**Runtime mitigation present:** `pkg/naming/EnvironmentNamespace()` calls `ValidateKubernetesName()` which enforces RFC 1123 at reconcile time. Invalid names cause the reconcile to fail with an error condition on the Tenant CR.

**Why it matters:** The API layer enforces RFC 1123 for tenant users creating environments (`environments.go:118-121`). Platform admins creating Tenant CRs directly via kubectl bypass the API layer and only hit the runtime check — which surfaces as a confusing condition failure rather than an admission rejection.

**Other validation findings:**

- `ClusterCredential.Spec.APIServer` — has `+kubebuilder:validation:Pattern=^https?://` (good)
- `ClusterCredential.Spec.ClusterID` — has `+kubebuilder:validation:Pattern=^cell-[a-z0-9-]+$` (good)
- `GitRepositoryBinding.Spec.RepoURL` — only `MinLength=1` (SEC-003 below)
- `PlatformBootstrap` URL fields — have `https?://` pattern (good)
- `ExposedRoute.Spec.Port` — has `Minimum=1 + Maximum=65535` (good)

---

## Section 5: SSRF Analysis

**Finding SEC-003:** `operators/api/v1/gitrepositorybinding_types.go:32-33`

`GitRepositoryBinding.Spec.RepoURL` has no URL validation. `checkReachability()` in `gitrepositorybinding_controller.go:236-290` makes an authenticated HTTP GET to the URL derived from this field, including `Authorization: token {gitea_token}` header.

**Attack chain:**
1. Platform admin creates a `GitRepositoryBinding` CR with `RepoURL: "http://169.254.169.254/api/v1/repos/anything/thing"`
2. `gitea.RepoAPIURL()` parses the URL: scheme=`http`, host=`169.254.169.254`, builds path `/api/v1/repos/anything/thing`
3. `checkReachability()` makes `GET http://169.254.169.254/api/v1/repos/anything/thing` with `Authorization: token <gitea-token>`
4. AWS metadata endpoint responds (in cloud environments), leaking the Gitea token to an external observer if the response is logged

**Mitigating factors:**
- Only platform admins can create GitRepositoryBinding CRs (ClusterRole restricted)
- `gitea.RepoAPIURL()` enforces `owner/repo` path structure — some URL structures are blocked
- This is a lateral movement/credential exposure risk, not an RCE or data exfiltration vector

---

## Section 6: Identity and Access

**Operator ServiceAccount privileges (INFRA-001, already registered):**
The operator ClusterRole grants cluster-wide `get/list/watch/create/update/delete` on `core/v1 Secrets` with no namespace restriction. This is the most significant privilege in the entire operator.

**What IS well-scoped:**
- Gitea, ArgoCD, Harbor credentials: read from K8s Secrets, not embedded in binary
- Token rotation: `GitRepositoryBinding` includes `computeCredentialHash()` for rotation detection; `CredentialHash` change triggers ArgoCD Secret update
- Harbor robot account TTL: `HarborRobotAccountTTL` constant limits robot credential lifetime
- Self-healing: `secretExists()` check in `ensureHarborRobotAccounts` detects missing K8s Secrets and force-recreates robots

**Token expiry status:**
- ArgoCD API key: non-expiring (platformbootstrap_argocd.go — explicitly "non-expiring apiKey")
- Gitea admin token: expiry not documented, validated via `ValidateToken()` call
- Harbor robot accounts: TTL-bounded (HarborRobotAccountTTL)
- ClusterCredential kubeconfig: uses bearer token from DOKS kubeconfig, no rotation mechanism

---

## Section 7: Multi-Tenancy Isolation Assessment

**Result: STRONG**

Verified controls:
1. **Namespace naming**: `pkg/naming/EnvironmentNamespace(tenantName, envName)` enforces `loopzero-{tenant}-{env}` format with RFC 1123 validation. Namespace collision between tenants is structurally impossible.
2. **Ownership-based GC**: `TenantController.reconcileEnvironmentCRs` uses `metav1.IsControlledBy(envCR, tenant)` before GC operations (`tenant_controller.go:289`).
3. **Label-based segregation**: All resources carry `loopzero.io/tenant` labels; build namespace cleanup uses `client.MatchingLabels{constants.LabelTenant: tenant.Name}`.
4. **ArgoCD AppProject scoping**: Each tenant gets an AppProject with explicit `DestinationNamespaces` and `DestinationServer` (SEC-007 comment at `tenant_gitops.go:67`). No wildcard destinations.
5. **NetworkPolicy**: `deny-all-other` policy created per environment namespace by `EnvironmentController`.
6. **Test coverage**: `tenant_isolation_test.go:250-311` verifies cross-tenant namespace isolation in envtest.

**Gap identified (not registered as finding — requires precondition of platform admin access):**
`EnvironmentController.Reconcile` does not verify `IsControlledBy` on the create/update path — only on the GC path in TenantController. A rogue Environment CR pointing to a real tenant's namespace could overwrite NetworkPolicy. This requires platform admin kubectl access to exploit and is documented in the threat model.

---

## Section 8: Positive Security Controls

The following were explicitly verified as good practice:

- `pkg/naming` package centralizes all resource naming with RFC 1123 enforcement — no ad-hoc string concatenation for namespace names
- `DisplayName` has `SEC-011` comment documenting the injection prevention rationale
- `SEC-007` comment in `tenant_gitops.go:67` documents explicit `DestinationServer` instead of wildcard
- Harbor Docker config JSON uses `json.Marshal` not string formatting, preventing JSON injection
- `ArgoCD cluster secret stores only bearer token + CA, not full kubeconfig` (comment in `clustercredential_controller.go:237-238`): minimizes credential exposure if ArgoCD namespace is compromised
- `computeCredentialHash()` in GitRepositoryBinding uses SHA-256 not MD5

---

## Summary Table

| ID | Title | Severity | Effort | Status |
|----|-------|----------|--------|--------|
| SEC-001 | InsecureSkipVerify=true by default | medium | low | open |
| SEC-002 | TenantEnvironmentEntry.Name no CRD pattern validation | medium | low | open |
| SEC-003 | GitRepositoryBinding RepoURL no URL validation (SSRF) | medium | medium | open |
| SEC-004 | MinIO CB not wired + potential credential endpoint in errors | low | low | open |
| SEC-005 | ClusterCredential.TLS.Insecure allows disabling ArgoCD TLS | low | medium | open |
| SEC-006 | Positive: multi-tenancy isolation well-enforced | low | — | documented |
