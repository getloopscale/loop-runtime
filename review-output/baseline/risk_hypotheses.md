# Risk Hypotheses — Cluster Management & Control Plane

> Initial risk hypotheses formed during Phase 1 baseline analysis. Each will be confirmed or dismissed with evidence in subsequent phases.

---

## High Severity (4)

### RISK-007: Build Namespace Egress Policy — Unrestricted Port 443
**Category:** Security
**Component:** TenantController (tenant_build.go)
**Hypothesis:** The `reconcileBuildNetworkPolicy` in tenant_build.go creates an egress rule that allows traffic on port 443 to **any IP address**. This means a malicious Dockerfile (or compromised build dependency) could exfiltrate secrets, source code, or build artifacts to any HTTPS endpoint on the internet.
**Spec Reference:** §17.1 — Exfiltration prevention in build isolation
**Impact:** HIGH — If build secrets (registry credentials, MinIO access keys, DO tokens) are available to the build container, they can be exfiltrated to external servers.
**Testable Assertion:** Grep `reconcileBuildNetworkPolicy` for egress rules with port 443 and verify whether an IPBlock restriction exists.

### RISK-003: MinIO Credential Recovery Gap
**Category:** Operational
**Component:** TenantController (tenant_build.go), MinIO Provisioner
**Hypothesis:** When a MinIO user already exists but the corresponding K8s Secret is missing (deleted out-of-band), the controller enters an unrecoverable state. `EnsureTenantCredentials` returns an empty `secretKey` for existing users, and the Secret reconciliation skips the update.
**Impact:** HIGH — Tenant builds fail until manual intervention (delete MinIO user or recreate Secret manually).
**Testable Assertion:** Read MinIO provisioner's `EnsureTenantCredentials` return value when user exists; trace what happens when K8s Secret is absent.

### RISK-004: Cross-Cluster Orphaned Resources
**Category:** Operational
**Component:** TenantController (reconcileDelete)
**Hypothesis:** Because `ownerReference` cannot cross cluster boundaries, build-{tenant} namespaces on the build cluster are cleaned up via the tenant's finalizer. If the build cluster is unreachable during tenant deletion, the finalizer blocks indefinitely and the build namespace persists as an orphan with allocated quota.
**Impact:** HIGH — Resource leaks accumulate on the build cluster; no automated garbage collection exists.
**Testable Assertion:** Read reconcileDelete path; check if build cluster unreachability has explicit error handling or timeout.

### RISK-006: Multi-Cluster Failure Modes Untested
**Category:** Testing
**Component:** operatortest/ (envtest setup)
**Hypothesis:** The operator test suite uses a single envtest API server for all 3 cluster clients. Cross-cluster failure scenarios (build cluster down, runtime cluster partitioned, network timeout between clusters) are never exercised in tests.
**Impact:** HIGH — Production failures in multi-cluster communication may not be caught before deployment. Controllers may corrupt state or hang on unreachable clusters.
**Testable Assertion:** Read suite_test.go and operatortest/setup.go; search for any test that mocks a cluster failure or returns a connection error.

---

## Medium Severity (3)

### RISK-001: InsecureSkipVerify on Shared HTTP Client
**Category:** Security
**Component:** operators/cmd/main.go
**Hypothesis:** A single `http.Client` with `InsecureSkipVerify=true` is shared by GitRepositoryBindingController and PlatformBootstrapController. While intended for self-signed internal certs, this disables all TLS certificate validation. If a provisioner is extended to call external endpoints, MITM attacks become possible.
**Impact:** MEDIUM — Currently contained to internal cluster traffic, but the pattern creates a latent vulnerability.
**Testable Assertion:** Audit all code paths that use this httpClient; confirm they only reach internal cluster endpoints.

### RISK-002: MinIO Admin API Authentication via Basic Auth
**Category:** Security
**Component:** operators/internal/minio/provisioner.go
**Hypothesis:** The MinIO Admin provisioner uses `req.SetBasicAuth()` for authentication. If the MINIO_ENDPOINT is HTTP (not HTTPS), credentials are transmitted in cleartext. The MinIO Admin API typically uses HMAC-SHA256 signing.
**Impact:** MEDIUM — Credentials exposed in transit if TLS not enforced. In production, MinIO is disabled (DO Spaces used instead), reducing actual risk.
**Testable Assertion:** Read signRequest(); verify MINIO_ENDPOINT defaults; check if TLS is enforced in production.

### RISK-005: PlatformBootstrap API Seed Idempotency
**Category:** Operational
**Component:** PlatformBootstrapController (platformbootstrap_api.go)
**Hypothesis:** The WaitingForAPI phase calls the API to seed the database. If the operator crashes after the API call succeeds but before the state machine transitions, the next reconcile will retry the seed. If the seed endpoint is not idempotent (insert vs. upsert), duplicate data may be created.
**Impact:** MEDIUM — Duplicate seed data could cause runtime errors or data inconsistency. Platform bootstrap runs rarely (once per cluster setup).
**Testable Assertion:** Read platformbootstrap_api.go; check if the API seed endpoint uses upsert semantics.

---

## Summary Table

| ID | Severity | Category | Component | Status |
|----|----------|----------|-----------|--------|
| RISK-007 | High | Security | TenantController | Open — needs code evidence |
| RISK-003 | High | Operational | TenantController + MinIO | Open — needs code trace |
| RISK-004 | High | Operational | TenantController | Open — needs reconcileDelete read |
| RISK-006 | High | Testing | operatortest | Open — needs test coverage scan |
| RISK-001 | Medium | Security | operators/cmd/main.go | Open — needs endpoint audit |
| RISK-002 | Medium | Security | MinIO Provisioner | Open — needs production config check |
| RISK-005 | Medium | Operational | PlatformBootstrapController | Open — needs API endpoint check |
