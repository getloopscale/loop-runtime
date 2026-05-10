# Threat Model — Critical Flows

**Date:** 2026-05-10
**Analyst:** Threat Modeler (Phase 6)
**Scope:** Deploy Flow + Authentication Flow
**Methodology:** STRIDE per Flow

---

## Executive Summary

| Metric | Value |
|--------|-------|
| Flows analyzed | 2 |
| Threats identified | 12 |
| Critical threats | 4 |
| High threats | 5 |
| Toxic combinations | 3 |
| OWASP gaps | A01, A07, A09 |

---

## Trust Boundary Map

```
[Developer machine]
      | Bearer JWT (RS256, exp enforced)
      v
[Cloudflare Edge / Tunnel]
      | TLS terminated; no internal traffic inspection
      v
[LoopZero API — mgmt cluster]
      | pg_advisory_xact_lock (env mutation boundary)
      |     AuthN: JWT validator → Hydra JWKS
      |     AuthZ: TenantID match + Role from DB
      v
[Argo Workflows — build cluster]
      | WorkflowID reference only; no user token forwarded
      v
[BuildKit (rootless)] ← [S3 bundle] ← [user source code]
      | cosign sign → image digest
      v
[Gitea — gitops push]
      | ArgoCD sync (cluster credentials via ClusterCredential CRD)
      v
[Argo Rollouts — workload cluster / cell-N]
      | 60s stale cache window on CellConfig lookup
      v
[Tenant workload namespace — isolated]
```

---

## Flow 1: Deploy Flow

**Entry:** `loopzero deploy` → POST /api/v1/builds
**Exit:** SSE `deploy.completed`

### Assets at Risk

| Asset | Impact if Compromised |
|-------|-----------------------|
| Tenant source code bundle (S3) | Exfiltration of proprietary application code |
| Container image registry credentials | Supply chain injection across all tenants |
| BuildKit workspace | Code execution during build; secrets injection into image layers |
| Gitea gitops repo | Arbitrary manifest push → arbitrary workload on any cell |
| ArgoCD cluster credentials | Full workload cluster access across all cells |
| Cell routing table (60s cache) | Cross-tenant workload routing during stale window |

### STRIDE Analysis

| Category | Threat ID | Threat | Likelihood | Impact | Risk | Control Exists? | Control Adequate? |
|----------|-----------|--------|-----------|--------|------|-----------------|-------------------|
| **S** Spoofing | D-S1 | Attacker submits deploy with forged tenant claim via X-Service-Auth path | Low | Critical | High | Yes — X-Service-Auth validated with ConstantTimeCompare | Partial — identity headers X-LoopZero-User-ID/X-LoopZero-Tenant are user-controlled if secret leaks |
| **T** Tampering | D-T1 | Malicious Dockerfile injects secrets into image layers at build time | Medium | High | High | Yes — dockerfile linter (FROM registry allow-list, --privileged reject) | Partial — linter runs in-band; `RUN` commands that exfiltrate env vars are not blocked |
| **T** Tampering | D-T2 | Source bundle in S3 tampered between upload and BuildKit fetch | Low | Critical | High | No — no integrity hash verified on bundle fetch | **Missing** |
| **T** Tampering | D-T3 | Gitea gitops repo push unauthenticated (internal network assumed trusted) | Low | Critical | Medium | Yes — scoped kubeconfig + ClusterCredential | Partial — internal Gitea network exposure not auditable |
| **R** Repudiation | D-R1 | Build callback (`OnBuildCompleted`) invoked by anyone knowing build ID | Medium | High | High | Yes — X-Service-Auth header required on callback endpoint | Partial — `GetByID` has no tenant_id filter (IDOR gap confirmed) |
| **I** Info Disclosure | D-I1 | Build logs stream reveals secrets injected as env vars | Medium | High | High | Yes — SSE endpoint checks `build.TenantID != uc.TenantID` | Yes — isolation enforced in logs_stream.go:98 |
| **I** Info Disclosure | D-I2 | `BuildStore.GetByID` returns build for any tenant (IDOR) | Medium | High | High | Partial — route-layer calls `GetBuildTenantID` then compares | **Partial** — not enforced at DB layer; depends on every caller performing the comparison |
| **D** DoS | D-D1 | Rate limiter is per-tenant; attacker creates free-tier tenant and floods builds | High | Medium | High | Yes — rate limiter exists | **No** — keyed by TenantID; unauthenticated path keyed "anonymous" allows per-IP burst without account |
| **D** DoS | D-D2 | Large source bundle upload exhausts API memory/disk before S3 offload | Medium | Medium | Medium | Unknown — no max upload size found in routes | **Unknown** — needs verification |
| **E** Privilege Escalation | D-E1 | 60s stale cell routing resolves cell-N during maintenance → deploys land on wrong cluster | Low | Critical | High | Yes — ClusterCredential health polling | **No** — 60s window is acknowledged in dynamic_router.go:97 |
| **E** Privilege Escalation | D-E2 | cosign keyless verification not yet enforced (pending Sprint 9); policy-controller in warn-mode | Medium | Critical | Critical | Partial — `loopzero-images-warn` ClusterImagePolicy in warn mode only | **No** — unsigned images admitted; deny-flip deferred |

### Missing Controls

1. **S3 bundle integrity** — compute SHA256 of bundle at upload; BuildKit workflow step verifies digest before extraction. Without this, S3 MITM or misconfigured bucket policy allows code substitution.
2. **DB-layer tenant filter on GetByID** — add `AND tenant_id = $2` to the `builds` query in `PostgresBuildStore.GetByID`. Caller-enforced isolation is not defence-in-depth.
3. **policy-controller deny flip** — promote `ClusterImagePolicy` from warn to deny. Every build already cosign-signs; the policy enforcement gap is purely configuration.
4. **Upload size limit** — enforce `http.MaxBytesReader` on the source bundle upload endpoint.

---

## Flow 2: Authentication Flow

**Entry:** `loopzero login` → Ory Hydra OAuth2 PKCE
**Exit:** Bearer JWT cached at `~/.loopzero/credentials.json`

### Assets at Risk

| Asset | Impact if Compromised |
|-------|-----------------------|
| RS256 access token (Hydra) | Full API access as that user for token lifetime |
| HMAC session token (dashboard) | Dashboard session hijack |
| `~/.loopzero/credentials.json` | Long-lived token on developer workstation |
| Hydra JWKS endpoint | Key substitution → token forgery if compromised |
| Token validation cache (5-min TTL) | Revoked user stays authenticated for up to 5 min |

### STRIDE Analysis

| Category | Threat ID | Threat | Likelihood | Impact | Risk | Control Exists? | Control Adequate? |
|----------|-----------|--------|-----------|--------|------|-----------------|-------------------|
| **S** Spoofing | A-S1 | Algorithm confusion attack (alg:none / RS→HS swap) | Low | Critical | High | Yes — `WithValidMethods(["RS256"])` + HMAC path `["HS256"]` strictly separated | Yes |
| **S** Spoofing | A-S2 | Cross-JWT confusion (dashboard HMAC token accepted on CLI RS256-expected path) | Low | High | Medium | Yes — `JWTOnlyValidator.ValidateToken` tries RS256 first, then HS256 | Partial — HMAC session tokens accepted by the same validator chain as CLI tokens; if an attacker obtains a dashboard session token they can call CLI-targeted endpoints |
| **T** Tampering | A-T1 | Local credentials file `~/.loopzero/credentials.json` read by other processes or malware | Medium | High | High | No — file permissions depend on OS defaults | **Missing** — no file permission enforcement, no credential encryption at rest |
| **R** Repudiation | A-R1 | Token use not audit-logged per API call; only mutation events logged | Medium | Medium | Medium | Partial — audit middleware exists for mutations | Partial — read-only API calls (status, logs) leave no trace |
| **I** Info Disclosure | A-I1 | Token validation cache holds user identity for 5 min after revocation | Medium | High | High | No — no revocation check (no JTI denylist, no introspection call) | **Missing** — revocation is entirely absent |
| **I** Info Disclosure | A-I2 | JWKS cache (5-min TTL) serves stale keys after Hydra key rotation | Low | High | Medium | Yes — circuit breaker + cached fallback | Partial — graceful degradation keeps stale keys valid longer than intended during Hydra outage |
| **D** DoS | A-D1 | Token validation cache bounded at 10,000 entries; flood with unique tokens forces O(n) eviction under lock | Low | Medium | Medium | Yes — `evictCache` with two-pass LRU approximation | Partial — eviction is O(n) linear scan; 10,000 entries × concurrent requests could cause latency spike |
| **E** Privilege Escalation | A-E1 | X-LoopZero-Tenant header on service-auth path trusted without membership re-validation | Low | Critical | High | Yes — role resolved from DB even on service-auth path (auth.go:186-200) | Yes — role is not trusted from header |
| **E** Privilege Escalation | A-E2 | Three separate admin secret env vars (LOOPZERO_SERVICE_AUTH_SECRET, LOOPZERO_ADMIN_API_KEY, LOOPZERO_ADMIN_API) with no central rotation coordination | Medium | High | High | Partial — each secret has dual-key rotation support | **Partial** — fragmented secret management; LOOPZERO_ADMIN_API (bool flag) independent of key material creates bypass if flag set true but key empty |

### Missing Controls

1. **Token revocation / introspection** — call Hydra `/oauth2/introspect` on cache miss or add JTI denylist. The 5-minute cache TTL means account deprovisioning is not immediate.
2. **Credentials file permissions** — CLI should `chmod 0600 ~/.loopzero/credentials.json` on write. Currently no enforcement found.
3. **Audience separation between dashboard and CLI tokens** — RS256 (CLI) and HS256 (dashboard) tokens are accepted by the same validator. If dashboard tokens carry a different audience claim, validate it strictly to prevent cross-context use.

---

## Toxic Combinations

### TC-1: IDOR Gap + No Token Revocation

- **Finding A (D-I2):** `BuildStore.GetByID` returns any build without DB-level tenant filter; isolation is caller-enforced.
- **Finding B (A-I1):** Revoked tokens remain valid for up to 5 minutes due to in-process cache.
- **Individual Severity:** A = High, B = High
- **Combined Severity:** Critical
- **Attack Scenario:** Attacker compromises a legitimate user's token (e.g., via phishing). Admin revokes the account in Hydra. During the 5-minute window the token remains cached valid. Attacker calls `GET /api/v1/builds/{id}` with sequential IDs harvested from SSE events. Because `GetByID` has no DB-level tenant filter, a caller that bypasses or races the route-layer check reads cross-tenant build metadata (WorkflowID, CommitSHA, AppsSpec including secret names). The 5-minute revocation gap is the window; the IDOR is the blast radius multiplier.
- **Mitigation (in priority order):**
  1. Add `AND tenant_id = $2` to `PostgresBuildStore.GetByID` immediately — eliminates DB-layer exposure regardless of caller correctness.
  2. Reduce token validation cache TTL from 5 min to 30 s, or add Hydra introspection on privileged-read paths.

### TC-2: warn-mode Policy Controller + No Bundle Integrity Check

- **Finding A (D-E2):** `ClusterImagePolicy` is in warn mode; unsigned or tampered images are admitted.
- **Finding B (D-T2):** S3 source bundle has no integrity hash verified at BuildKit fetch.
- **Individual Severity:** A = Critical, B = High
- **Combined Severity:** Critical
- **Attack Scenario:** Attacker with write access to S3 bucket (misconfigured bucket policy, leaked DO Spaces key) replaces the source bundle after upload but before BuildKit fetches it. BuildKit builds the tampered image. cosign signs the tampered image (it signs whatever BuildKit produces). The signed tampered image passes policy-controller warn check. Workload runs attacker code with tenant's secrets, on tenant's cluster, with tenant's service account. No alert fires because warn mode only logs.
- **Mitigation:**
  1. Compute SHA256 of bundle at upload; pass digest as Argo Workflow parameter; verify in DAG step before extraction.
  2. Flip `ClusterImagePolicy` to deny — does not block this attack but prevents unsigned lateral-movement images.
  3. Audit S3 bucket policies to ensure only the API service account can write to bundle prefixes.

### TC-3: Per-Tenant Rate Limit + Free-Tier Account Creation

- **Finding A (D-D1):** Rate limiter keys on TenantID; creating a free-tier tenant gets a fresh bucket.
- **Finding B (context):** Starter tier is free; account creation is self-service.
- **Individual Severity:** A = Medium, B = Low
- **Combined Severity:** High
- **Attack Scenario:** Attacker scripts free-tier account creation (automated via `/api/v1/bootstrap`) to generate N distinct tenant IDs. Each tenant gets its own rate limit bucket. Attacker submits 60 concurrent builds across 60 tenants, all hitting the build cluster simultaneously, exhausting Argo Workflow slots and blocking legitimate builds. Existing rate limiter provides zero protection because the limit is per-tenant, not per-IP or per-plan.
- **Mitigation:**
  1. Add per-IP rate limit in addition to per-tenant (iprate.go already exists — apply to `/api/v1/bootstrap`).
  2. Enforce Starter plan build concurrency cap at plan enforcement layer (not just rate limiter).
  3. Add account creation CAPTCHA or email verification before granting first API token.

---

## OWASP Top 10 Checklist

| # | Category | Status | Evidence |
|---|----------|--------|----------|
| A01 | Broken Access Control | PARTIAL GAP | `GetByID` no DB-level tenant filter; route-layer isolation only |
| A02 | Cryptographic Failures | PASS | RS256 pinned; ConstantTimeCompare on secrets; SHA256 token cache keys |
| A03 | Injection | PASS | Parameterized queries throughout; dockerfile linter blocks shell injection in build |
| A04 | Insecure Design | PARTIAL GAP | No bundle integrity; warn-mode policy controller; free-tier DoS vector |
| A05 | Security Misconfiguration | PARTIAL GAP | Three admin secrets without rotation coordination; LOOPZERO_ADMIN_API bool independent of key |
| A06 | Vulnerable Components | UNKNOWN | No automated dependency scanning found in CI workflows reviewed |
| A07 | Authentication Failures | PARTIAL GAP | No token revocation / introspection; 5-min revocation window; credentials file permissions unset |
| A08 | Software/Data Integrity Failures | GAP | S3 bundle no integrity check; policy-controller in warn mode (cosign-signed but not enforced) |
| A09 | Logging/Monitoring Failures | PARTIAL GAP | Read-only API calls not audit-logged; no alert on GetByID cross-tenant probe attempts |
| A10 | SSRF | PASS | No user-controlled URL fetch found in deploy or auth flows |

---

## Remediation Priority

| Priority | Severity | Threat | Fix | Effort |
|----------|----------|--------|-----|--------|
| 1 | Critical | D-I2 — IDOR on GetByID | Add `AND tenant_id = $2` to `PostgresBuildStore.GetByID` SQL | 30 min |
| 2 | Critical | D-E2 — policy-controller warn mode | Flip `ClusterImagePolicy` to deny in `infra/helm/admission-policies/` | 1 hr |
| 3 | Critical | TC-2 — bundle tamper + warn mode | Add SHA256 digest parameter to Argo Workflow DAG; verify in build step | 4 hr |
| 4 | High | A-I1 — no token revocation | Reduce cache TTL to 30 s OR add Hydra introspection on sensitive reads | 2 hr |
| 5 | High | A-T1 — credentials file permissions | CLI: `os.Chmod("~/.loopzero/credentials.json", 0600)` after write | 15 min |
| 6 | High | D-D1 + TC-3 — rate limit bypass | Per-IP rate limit on bootstrap + plan-level build concurrency cap | 3 hr |
| 7 | High | A-S2 — cross-context JWT acceptance | Enforce distinct audience claim for dashboard vs CLI tokens | 2 hr |
| 8 | Medium | A-E2 — fragmented admin secrets | Document rotation runbook; add `loopzero_admin_secret_age_seconds` metric per key | 1 hr |
| 9 | Medium | A-R1 — read-only calls unlogged | Add audit log entry for `GetBuildStatus` and `StreamBuildLogs` on first call | 2 hr |
| 10 | Medium | A06 — no dependency scanning | Add `govulncheck` + `npm audit` steps to CI | 1 hr |

---

## Evidence References

| Finding | File | Line |
|---------|------|------|
| D-I2 GetByID no tenant filter | `api/internal/db/build_store.go` | 128–136 |
| D-I2 route-layer tenant check | `api/internal/routes/logs_stream.go` | 93–101 |
| D-E2 warn-mode policy | `infra/helm/admission-policies/` | ClusterImagePolicy template |
| A-I1 5-min cache TTL | `api/internal/middleware/auth.go` | 87 |
| A-S2 dual-path validator | `api/internal/middleware/jwt_validator.go` | 29–56 |
| A-E2 fragmented secrets | `api/internal/middleware/auth.go` | 97–111 |
| D-D1 per-tenant key only | `api/internal/middleware/ratelimit.go` | 99–100 |
| D-T2 no bundle integrity | `api/internal/db/build_store.go` (no integrity field) | — |
| D-E1 60s cache window | `api/internal/placement/collector.go` | 33 |
| TC-2 S3 bundle window | `api/internal/placement/dynamic_router.go` | 97 |
