# Threat Model Report — LoopZero PaaS

**Date:** 2026-03-29
**Methodology:** STRIDE per Critical Flow
**Flows Analyzed:** 7
**Threats Registered:** 15
**Critical Threats:** 4
**High Threats:** 6
**Toxic Combinations:** 4

---

## Executive Summary

LoopZero is a multi-tenant Kubernetes PaaS exposing a public CLI-facing API protected by GitHub token authentication. The deploy flow touches five trust boundaries: developer machine, API server, build cluster (Argo Workflows + BuildKit), Gitea/ArgoCD GitOps pipeline, and Cloudflare edge (Workers KV + DO Spaces CDN).

The threat model identified **four critical threats** requiring immediate remediation before any production deployment:

1. **Plaintext secret storage** — `encrypted_value` column stores raw bytes with no cipher.
2. **Unauthenticated /internal/bootstrap** — any caller can create admin users and tenants.
3. **Unauthenticated /internal/argocd-callback** — any caller can force-complete a deploy.
4. **Single Cloudflare API token write-access to all tenant routing** — one token compromise affects every static frontend.

Two of these (2 + 3) combine into a **platform takeover toxic combination** that allows an attacker to create a tenant, get credentials, and immediately mark their deploy as live — bypassing the entire build pipeline.

---

## Trust Boundary Map

```
Internet
    │
    ▼
[Cloudflare WAF / DDoS]
    │
    ▼
[Cloudflare Tunnel] ──────────────────────────[CF Worker + DO Spaces CDN]
    │                                                     │ (static frontends)
    ▼                                                     │
[Traefik Ingress]                                         │
    │                                                     │
    ▼           ┌─ AuthN boundary (GitHub token) ──┐     │
[API Server] ◄──┘                                  │     │
    │                                               │     │
    │           ┌─ AuthZ boundary (tenant scope) ──┘     │
    │           │                                         │
    ├──► [PostgreSQL] (tenant state, secrets, builds)     │
    │                                                     │
    ├──► [Gitea] (source repos, gitops repos)             │
    │                                                     │
    ├──► [ArgoCD] ──► [Workload K8s Cluster]              │
    │                         │                           │
    ├──► [Argo Workflows] ◄───┤                           │
    │    [Build Cluster]      │                           │
    │    [BuildKit pods]      │                           │
    │                         │                           │
    └──► [CF KV] ─────────────┴───────────────────────► ─┘
         (routing table)

Trust Boundaries:
  TB-1: Internet → Cloudflare (WAF, DDoS, geo-restrictions)
  TB-2: Cloudflare → Traefik (TLS termination)
  TB-3: CLI → API (GitHub token authentication)
  TB-4: API → internal services (advisory lock, tenant scope)
  TB-5: API → Build Cluster (Argo Workflow submission)
  TB-6: API → Gitea/ArgoCD (gitops push + sync trigger)
  TB-7: /internal/ routes (no authentication — intended for cluster-internal only)
```

---

## Flow-by-Flow Threat Analysis

### Flow 1: Authentication (flow_auth)

**Assets at Risk**
- GitHub personal access tokens: full access to all tenant resources for token lifetime
- Tenant membership data: which users belong to which tenants
- 5-minute cached validation results: window for token replay attacks

#### STRIDE Analysis

| Category | Threat | Likelihood | Impact | Risk | Control | Adequate? |
|----------|--------|-----------|--------|------|---------|-----------|
| Spoofing | GitHub token replay within 5-min cache TTL | Medium | High | High | 5-min TTL cache | No — no revocation mechanism |
| Tampering | X-LoopZero-Tenant header injection to access non-member tenant | Low | Critical | High | Membership check in middleware | Yes |
| Repudiation | Auth failures not audit-logged (only mutations audited) | Medium | Medium | Medium | AuditLog middleware for mutations | No — no failed-auth log |
| Information Disclosure | Token enumeration via timing difference in cache hits | Low | Low | Low | sync.Map concurrent access | Acceptable |
| Denial of Service | Auth cache exhaustion via token flooding (10K cap triggers mass eviction) | Medium | Medium | Medium | 10K cap + eviction | Partial — no pre-auth rate limit |
| EoP | Multi-tenant user forges X-LoopZero-Tenant to a third tenant they do not belong to | Low | Critical | Medium | Membership verification | Yes |

**Missing Controls**
1. No forced re-validation on sensitive operations (secret write, project delete, rollback)
2. No IP-based or pre-auth rate limiting (rate limiter fires AFTER auth succeeds)
3. No audit log for authentication failures or TENANT_NOT_FOUND rejections
4. No server-side token revocation or invalidation endpoint

---

### Flow 2: Container Deploy (flow_deploy_container / flow_container_deploy)

**Assets at Risk**
- Source code of all tenants (uploaded to Gitea/MinIO)
- Container images (pushed to DO Registry)
- Build secrets (registry credentials mounted in build pods)
- GitOps manifests (deployment configuration)
- Running workloads (ArgoCD convergence target)

#### STRIDE Analysis

| Category | Threat | Likelihood | Impact | Risk | Control | Adequate? |
|----------|--------|-----------|--------|------|---------|-----------|
| Spoofing | Forged ArgoCD callback marks release as ACTIVE prematurely | Medium | High | High | BuildReconcileLoop eventual consistency | No — no HMAC verification |
| Tampering | Path traversal via crafted app.path in build request | Low | High | Medium | ValidateAppPath() SEC-3 | Partial — binary encoding bypass possible |
| Tampering | Supply chain attack via loopzero-forge SDK | Low | Critical | High | Pinned module version + go.sum | Partial — no SBOM, no Kyverno policy |
| Repudiation | Build submissions not individually signed or non-repudiable | Low | Low | Low | AuditLog middleware | Acceptable |
| Information Disclosure | Registry credentials accessible in BuildKit pod environment | Low | Critical | Medium | Build pods are isolated per-build | No pod security policy |
| DoS | 500MB bundle fully buffered in memory, 10+ concurrent uploads = OOM | Medium | High | High | 500MB cap, 60 req/min rate limit | No global concurrent upload limit |
| EoP | Malicious Argo Workflow template injected via loopzero-forge compromise | Low | Critical | Medium | Pinned version | No — no Kyverno constraint |

**Missing Controls**
1. HMAC-SHA256 verification on /internal/argocd-callback (secret exists as env var but not used)
2. Streaming upload to Gitea/MinIO instead of full in-memory buffer
3. Pod security admission policy on build pods (read-only root, no host network)
4. SBOM generation for loopzero-forge and supply chain verification

---

### Flow 3: Secret Management (flow_secret_set)

**Assets at Risk**
- All application secrets across all tenants: DATABASE_URL, API keys, OAuth credentials
- K8s Secrets on mgmt cluster (cross-tenant if RBAC is misconfigured)
- ESO-synced secrets on workload cluster

#### STRIDE Analysis

| Category | Threat | Likelihood | Impact | Risk | Control | Adequate? |
|----------|--------|-----------|--------|------|---------|-----------|
| Spoofing | Attacker impersonates legitimate user to read/write secrets | Low | Critical | High | GitHub token auth | Yes |
| Tampering | DB backup exfiltration exposes all secrets in plaintext | Medium | Critical | Critical | None | MISSING — no encryption |
| Repudiation | Secret write/delete not in dedicated audit trail | Medium | Medium | Medium | AuditLog middleware (generic) | Partial |
| Information Disclosure | Plaintext bytes in encrypted_value column readable by DB admin | Medium | Critical | Critical | Column named "encrypted" but no cipher | MISSING — false assurance |
| Information Disclosure | ESO cross-cluster sync transmits secrets without confirmed mTLS | Medium | High | High | K8s RBAC, NetworkPolicy | Not verified |
| DoS | Secret flooding (no limit on number of secrets per tenant) | Low | Low | Low | None identified | Acceptable risk |
| EoP | Compromised mgmt cluster pod reads all tenants' K8s Secrets | Medium | Critical | Critical | NetworkPolicy | No etcd encryption at rest |

**Missing Controls**
1. Application-level encryption (AES-256-GCM) before writing to PostgreSQL
2. Etcd encryption at rest on mgmt cluster for secrets resource
3. Per-tenant K8s namespace RBAC for secrets isolation
4. Dedicated audit log for secret operations (not just generic mutation log)
5. Verification that ESO uses mTLS for cross-cluster sync

---

### Flow 4: Static Frontend Deploy (flow_static_deploy)

**Assets at Risk**
- Static frontend content served to end users
- CF KV routing table (maps all tenant subdomains to build assets)
- DO Spaces content (user-facing JS/CSS/HTML)

#### STRIDE Analysis

| Category | Threat | Likelihood | Impact | Risk | Control | Adequate? |
|----------|--------|-----------|--------|------|---------|-----------|
| Spoofing | CF Worker serves forged content if KV poisoned | Low | Critical | High | None | MISSING |
| Tampering | CF KV routing table poisoned via compromised CFAPIToken | Low | Critical | High | Token is env var (not hardcoded) | No rotation, single token all tenants |
| Tampering | DO Spaces content replaced if Spaces credentials compromised | Low | Critical | High | SigV4 signed uploads | No object immutability/lock |
| Repudiation | KV mutations not logged by application | Medium | Medium | Medium | Cloudflare audit logs (external) | Partial |
| Information Disclosure | Subdomain enumeration via KV list operations | Medium | Low | Low | None | Acceptable |
| DoS | Mass KV deletion removes routing for all static frontends | Low | High | Medium | CF dashboard manual recovery | No automated detection |

**Missing Controls**
1. Signed routing entries (e.g., signed with HMAC) so CF Worker can detect tampering
2. Per-tenant CF API tokens or scoped service tokens
3. DO Spaces object lock (WORM) for immutable build artifacts
4. Monitoring alert for unexpected KV mutations

---

### Flow 5: Tenant Provisioning (flow_tenant_provisioning)

**Assets at Risk**
- Gitea repository credentials
- ArgoCD AppProject/Application configuration
- Build namespace RBAC and quotas

#### STRIDE Analysis

| Category | Threat | Likelihood | Impact | Risk | Control | Adequate? |
|----------|--------|-----------|--------|------|---------|-----------|
| Spoofing | Unauthenticated /internal/bootstrap creates arbitrary admin | Medium | Critical | Critical | None | MISSING |
| EoP | Bootstrap endpoint creates tenant with admin role for any GitHub account | Medium | Critical | Critical | Endpoint is /internal/ (advisory) | No authentication |
| Tampering | ArgoCD AppProject misconfiguration allows cross-project access | Low | High | Medium | Operator provisions AppProject | Dependent on ArgoCD RBAC config |

---

### Flow 6: Log Streaming (flow_log_streaming)

**Assets at Risk**
- Build logs of all tenants (could contain secrets if developers log them)
- Loki query capacity (expensive LogQL queries)

#### STRIDE Analysis

| Category | Threat | Likelihood | Impact | Risk | Control | Adequate? |
|----------|--------|-----------|--------|------|---------|-----------|
| Information Disclosure | LogQL injection via unvalidated step parameter | Medium | Medium | Medium | Build ownership check | Partial — query constructed via fmt.Sprintf |
| Information Disclosure | Loki has no authentication — any pod can query all logs | Medium | High | High | NetworkPolicy | Not enforced — Loki in monitoring ns |
| DoS | Expensive LogQL regex query via malicious step parameter | Low | Medium | Low | 1000-line limit | Partial |

**Missing Controls**
1. Sanitize/allowlist `step` query parameter before LogQL interpolation
2. Loki authentication (bearer token or mTLS from API pod)
3. Per-tenant Loki label scoping to prevent cross-tenant log access if Loki is directly reachable

---

### Flow 7: Rollback (flow_rollback)

**Assets at Risk**
- Live production release (can be reverted to a broken state)
- Previous release manifests in Gitea

#### STRIDE Analysis

| Category | Threat | Likelihood | Impact | Risk | Control | Adequate? |
|----------|--------|-----------|--------|------|---------|-----------|
| Tampering | failRollback uses empty advisory lock key (global lock) | High | Medium | High | Advisory lock present | Wrong coordinates — found in find_arch_rollback_lock |
| Repudiation | Rollback silently succeeds even if best-effort ArgoCD sync fails | High | High | High | BuildReconcileLoop | Partial — sync failure not user-visible |
| DoS | Rapid rollback-rollback cycles exhaust environment lock | Low | Medium | Low | 5s lock timeout | Acceptable |

---

## Toxic Combinations

These are the highest-value findings from this threat model. Individual medium findings that combine into critical risk.

---

### Toxic Combination 1: Platform Takeover (CRITICAL)

**Finding A:** POST /internal/bootstrap — no authentication (find_sec_0005)
**Finding B:** POST /internal/argocd-callback — no authentication (find_sec_0004)

**Individual Severity:** A = high, B = high
**Combined Severity:** CRITICAL

**Attack Scenario:**
1. Attacker discovers the LoopZero API endpoint (public via Cloudflare Tunnel or via NodePort scanning)
2. Attacker calls POST /internal/bootstrap with their own GitHub account:
   ```json
   {"admin_github_id": "attacker123", "admin_login": "attacker", "tenant_slug": "attacker-corp"}
   ```
3. Attacker now has a valid tenant. They authenticate with their real GitHub token to trigger a deploy.
4. Before the build completes, attacker calls POST /internal/argocd-callback:
   ```json
   {"app_name": "tn-attacker-corp-production", "sync_status": "Synced", "health_status": "Healthy", "revision": "abc123"}
   ```
5. The release activation fires: the API creates a release record as ACTIVE, superseding any previously active legitimate tenant release if tenant naming collision was achieved.
6. If an attacker can predict a legitimate tenant's ArgoCD app name (format: `tn-{slug}-{environment}`), they can force-complete deployments for tenants they do not own.

**Mitigation (priority order):**
1. Add HMAC-SHA256 signature verification to /internal/argocd-callback (use existing LOOPZERO_ARGO_CALLBACK_SECRET)
2. Add a one-time bootstrap token to /internal/bootstrap (disable after first successful bootstrap)
3. Add NetworkPolicy to restrict /internal/ routes to specific source pod labels only

---

### Toxic Combination 2: Silent Full-Secret Exfiltration (CRITICAL)

**Finding A:** Secret values stored as plaintext despite encrypted_value column name (find_sec_0001)
**Finding B:** PostgreSQL deployed as single-node with no backup encryption configuration (find_infra_011)
**Finding C:** Hardcoded default credentials in config.go (find_sec_0007, MinIOSecretKey defaulting to "loopzero-dev-password")

**Individual Severity:** A = high, B = high, C = medium
**Combined Severity:** CRITICAL

**Attack Scenario:**
1. Attacker obtains a DO managed PostgreSQL backup (via compromised DO account, backup bucket misconfiguration, or insider access)
2. The backup is restored to an attacker-controlled PostgreSQL instance
3. `SELECT name, encrypted_value FROM secrets` returns all tenant secrets in plaintext
4. There is no encryption key to break, no KMS to compromise — the data is the secret
5. If LOOPZERO_ENV is not explicitly set to "production" on the DO cluster, the default MinIO credentials (loopzero-admin/loopzero-dev-password) are used, giving the attacker access to all source bundles and build artifacts as well

**Mitigation (priority order):**
1. Implement AES-256-GCM encryption in secret_store.go before the INSERT
2. Store the encryption master key in a separate K8s Secret (not the database)
3. Enable DO managed PostgreSQL encrypted backups
4. Audit all config.go defaults to ensure no production-meaningful default exists (empty string forces explicit configuration)

---

### Toxic Combination 3: Token Revocation Window + No Re-auth on Sensitive Operations (HIGH)

**Finding A:** GitHub token revocation not detected within 5-minute cache TTL (find_code_0004 / find_sec_token_cache_revocation)
**Finding B:** Audit middleware only logs mutations after they succeed — no pre-auth mutation logging
**Finding C:** Secret set, project delete, and rollback have no additional verification beyond the initial token check

**Individual Severity:** A = medium, B = low, C = low
**Combined Severity:** HIGH

**Attack Scenario:**
1. Legitimate user (Alice) is fired from the company
2. Alice revokes her GitHub token on github.com
3. Company admin calls GitHub to revoke access
4. Alice continues to use her token for up to 5 minutes against the LoopZero API
5. During this window, Alice can: delete projects, overwrite secrets, trigger rollbacks to broken releases
6. These actions are audit-logged but only after completion — there is no real-time alerting
7. The actions succeed and are irreversible (deleted project data, overwritten secrets)

**Mitigation:**
1. Add forced re-validation bypass (skip cache) for: POST /secrets, DELETE /projects, POST /rollback
2. Add real-time alerting on audit log entries for destructive operations
3. Implement a manual token revocation endpoint for admins

---

### Toxic Combination 4: Loki Injection + No Loki Authentication (MEDIUM)

**Finding A:** LogQL injection via unvalidated step parameter (find_sec_0002)
**Finding B:** Loki has no authentication — any pod in the cluster with network access to the monitoring namespace can query it

**Individual Severity:** A = medium, B = high
**Combined Severity:** HIGH

**Attack Scenario:**
1. Attacker is an authenticated tenant user (or has compromised a tenant account)
2. They issue: `GET /builds/{id}/logs?step=x"}+||+{workflow=~".*"`
3. The LogQL query becomes: `{workflow=~".*{id}.*", step="x"} || {workflow=~".*"}`
4. This matches ALL workflows from ALL tenants in Loki
5. Since Loki has no authentication, if the attacker can also reach Loki directly (from a build pod or compromised workload), they can query all build logs without going through the tenant-isolation check in the API

**Mitigation:**
1. Sanitize the `step` parameter: allowlist `[a-zA-Z0-9_-]+` before LogQL interpolation
2. Add Loki authentication (at minimum a shared token, ideally per-tenant multi-tenancy via Loki X-Scope-OrgID header)
3. Enable Loki multi-tenancy mode with tenant IDs as scope labels

---

## OWASP Top 10 Checklist

| # | Category | Status | Evidence |
|---|----------|--------|----------|
| A01 | Broken Access Control | PARTIAL | Tenant isolation via auth middleware is correct, BUT /internal/bootstrap and /internal/argocd-callback are unauthenticated (find_sec_0005, find_sec_0004). Cross-tenant build access returns 404 not 403 (by design). |
| A02 | Cryptographic Failures | FAIL | Secrets stored as plaintext despite encrypted_value column name (find_sec_0001). PostgreSQL connection uses sslmode=disable in dev (find_sec_0006). ArgoCD InsecureSkipVerify enabled when ARGOCD_INSECURE=true (find_infra_012). |
| A03 | Injection | PARTIAL | SQL injection prevented by parameterized queries throughout. LogQL injection possible via unvalidated step parameter (find_sec_0002). App path injection mitigated by ValidateAppPath() (SEC-3). |
| A04 | Insecure Design | FAIL | No application-level encryption design for secrets. Single CF API token for all tenant routing. In-memory rate limiter (non-distributed). No defense-in-depth for /internal/ routes. |
| A05 | Security Misconfiguration | FAIL | Hardcoded dev credentials in Helm values checked into git (find_infra_010, find_sec_0007). sslmode=disable in Tilt manifests. All GitHub Actions use mutable tag pins (cicd_001). |
| A06 | Vulnerable Components | PARTIAL | govulncheck runs in CI. All Helm chart versions pinned. loopzero-forge is a pinned external module but no SBOM exists. GitHub Actions use tag (not SHA) pins. |
| A07 | Authentication Failures | PARTIAL | GitHub token validation is correct. 5-minute cache creates a revocation window (find_code_0004). No account lockout (auth delegates to GitHub). No MFA enforcement. |
| A08 | Software/Data Integrity Failures | FAIL | No image signing (find_infra_003, comp_012). GitHub Actions not SHA-pinned (cicd_001). No container image provenance. No SBOM for any component. |
| A09 | Logging/Monitoring Failures | PARTIAL | Audit middleware logs all mutations. No dedicated security event stream. No alerting on auth failures. ArgoCD callback spoofing would not be detected by current logging. |
| A10 | SSRF | LOW RISK | No user-controlled URL parameters that trigger server-side fetches identified. Loki URL and ArgoCD URL are configuration-time values. The GitHub validator calls github.com with the token — no user control over the URL. |

---

## Remediation Priority

| Priority | Severity | Threat / Finding | Fix | Effort |
|----------|----------|-----------------|-----|--------|
| 1 | Critical | Unauthenticated /internal/bootstrap (find_sec_0005) | Add one-time bootstrap token via env var header check. Disable after first bootstrap. | Low (hours) |
| 2 | Critical | Unauthenticated /internal/argocd-callback (find_sec_0004) | Implement HMAC-SHA256 verification using existing LOOPZERO_ARGO_CALLBACK_SECRET. | Low (hours) |
| 3 | Critical | Plaintext secret storage (find_sec_0001) | Implement AES-256-GCM encryption in secret_store.go. Store master key in separate K8s Secret. | Medium (days) |
| 4 | Critical | Toxic combination: bootstrap + callback = platform takeover | Resolved by items 1+2 above | — |
| 5 | High | CF KV single token for all tenants (threat 9) | Scope token to minimum permissions. Document incident response for token compromise. | Low (hours) |
| 6 | High | LogQL injection via step parameter (find_sec_0002) | Sanitize step parameter with allowlist regex before LogQL interpolation. | Low (hours) |
| 7 | High | Gitea token leaked in error logs (find_sec_0003) | Truncate or redact tokens in error messages. | Low (hours) |
| 8 | High | ArgoCD InsecureSkipVerify in production (find_infra_012) | Verify LOOPZERO_ENV=production is set on all DO DOKS deployments. Use cert-manager TLS for ArgoCD. | Medium (days) |
| 9 | High | Token revocation window + no re-auth on sensitive ops | Force cache bypass for: POST /secrets, DELETE /projects, POST /rollback. | Medium (days) |
| 10 | High | No image signing (find_infra_003, comp_012) | Implement cosign signing in build-images.yml. Generate SBOM via syft. | Medium (days) |
| 11 | Medium | Pre-auth rate limiting gap (unauthenticated requests not rate-limited per-IP) | Add Cloudflare WAF rate rule for API auth failure patterns. Or add IP-based limiter before auth middleware. | Medium (days) |
| 12 | Medium | Large bundle memory exhaustion (threat 11) | Stream bundle to Gitea/Spaces. Add global upload semaphore. | High (week) |
| 13 | Medium | GitHub Actions not SHA-pinned (cicd_001) | Pin all actions to full commit SHA. Add Dependabot for github-actions ecosystem. | Medium (days) |

---

## Appendix: Threat Database Summary

| ID | Flow | Category | Likelihood | Impact | Title |
|----|------|----------|-----------|--------|-------|
| 1 | auth | Spoofing | Medium | High | GitHub token replay within 5-min cache TTL |
| 2 | auth | DoS | Medium | Medium | Auth cache exhaustion via token flooding |
| 3 | auth | EoP | Low | Critical | X-LoopZero-Tenant header injection for cross-tenant access |
| 4 | deploy | Tampering | Low | High | Path traversal via crafted app.path in build request |
| 5 | deploy | Repudiation | Medium | High | Unauthenticated ArgoCD callback — spoofed sync completion |
| 6 | deploy | InfoDisc | Low | Critical | Supply chain compromise via loopzero-forge SDK dependency |
| 7 | secrets | InfoDisc | Medium | Critical | Secret values stored as plaintext in PostgreSQL |
| 8 | secrets | EoP | Medium | Critical | K8s Secret cross-tenant exposure via mgmt cluster |
| 9 | static | Tampering | Low | Critical | CF KV routing table poisoning via compromised API token |
| 10 | deploy | InfoDisc | Medium | High | Hardcoded dev credentials in Helm charts committed to git |
| 11 | deploy | DoS | Medium | High | Large bundle upload memory exhaustion (500MB × N concurrent) |
| 12 | tenant_prov | EoP | Medium | Critical | Unauthenticated /internal/bootstrap creates arbitrary admin |
| 13 | deploy | InfoDisc | Low | High | ArgoCD InsecureSkipVerify=true allows MITM on sync calls |
| 14 | logs | InfoDisc | Medium | Medium | Loki LogQL injection via unvalidated step parameter |
| 15 | deploy | DoS | Low | Medium | In-memory rate limiter state lost on pod restart |
