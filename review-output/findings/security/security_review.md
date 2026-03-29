# Security Audit

**Date:** 2026-03-29
**Target:** /home/paulo/loopzero/api
**Reviewer:** security-auditor
**Phase:** 4 — API Security Review

---

## Executive Summary

- **Overall security posture:** NEEDS ATTENTION
- **Total findings:** 9 (critical: 0, high: 3, medium: 3, low: 3)
- **Key concern:** Secret values stored without actual encryption despite a column named `encrypted_value`, and two unauthenticated internal endpoints (`/internal/bootstrap`, `/internal/argocd-callback`) that accept state-mutating operations.

The API demonstrates strong security foundations: all public routes are authenticated, tenant isolation is consistently enforced on every data access path, SQL queries are fully parameterized (zero string-concatenated queries found), application path traversal (SEC-3) is validated, and command injection via user-controlled paths is blocked. The critical weaknesses are concentrated in (1) the secrets subsystem where encryption is named but not implemented, (2) internal endpoints that bypass authentication, and (3) a credential token leakage asymmetry between the gitops and source store components.

---

## Secrets Scan

- **Hardcoded secrets found:** Development defaults committed to source (`loopzero-dev-password`, `loopzero-admin`) in `infra/helm/` chart values files.
- **.env files tracked in git:** No `.env` files tracked.
- **Secret management approach:** Environment variables for production. Dev defaults in config code with partial production detection.
- **Real credentials (AWS keys, GitHub tokens, private keys):** None found in the codebase.

---

## OWASP Top 10 Coverage

| # | Category | Status | Findings |
|---|----------|--------|----------|
| A01 | Broken Access Control | REVIEWED | 3 findings (find_sec_0004, find_sec_0005, find_sec_0008) |
| A02 | Cryptographic Failures | REVIEWED | 2 findings (find_sec_0001, find_sec_0006) |
| A03 | Injection | REVIEWED | 1 finding (find_sec_0002); SQL injection: CLEAN |
| A04 | Insecure Design | REVIEWED | 0 findings; rate limiting, advisory lock, idempotency all present |
| A05 | Security Misconfiguration | REVIEWED | 2 findings (find_sec_0007, find_sec_0009) |
| A06 | Vulnerable Components | REVIEWED | 0 findings; no pinned-version violations found in spot check |
| A07 | XSS | REVIEWED | 0 findings; API-only server, no HTML rendering |
| A08 | SSRF | REVIEWED | 0 findings; all outbound HTTP targets are config-driven service URLs, not user-supplied |
| A09 | Security Logging Failures | REVIEWED | 1 finding (find_sec_0003) |
| A10 | CSRF / Request Forgery | REVIEWED | 0 findings; API uses Bearer token auth (not cookies), CSRF not applicable |

---

## Authentication Review

- **Auth mechanism:** GitHub Bearer token validated via GitHub API `/user` endpoint, cached for 5 minutes in a bounded in-memory `sync.Map` (max 10,000 entries with eviction).
- **Password hashing:** Not applicable — authentication delegates to GitHub OAuth.
- **MFA support:** Inherited from GitHub account settings (not enforced by LoopZero).
- **Cache TTL:** 5 minutes default, configurable via `AUTH_CACHE_TTL`. Tokens revoked at GitHub take up to 5 minutes to be rejected.
- **Assessment:** SECURE. The cache eviction is implemented correctly (bounded, LRU-like fallback on overflow). Token invalidation lag is acceptable given the cache TTL.

---

## Authorization Review

- **Authorization model:** Tenant-scoped RBAC — every authenticated request resolves to a `(userID, tenantID, role)` triple. All data access uses tenantID from the authenticated context.
- **Per-resource access control:** YES on builds (SEC-4: `GetBuildTenantID` compared to `uc.TenantID` on GET, cancel, and logs). YES on projects (lookup scoped to `uc.TenantID`). YES on secrets (all store methods take `tenantID` from context).
- **IDOR prevention:** The pattern `if buildTenantID != uc.TenantID { return 404 }` is used correctly — 404 rather than 403 avoids leaking resource existence.
- **Assessment:** SECURE for all public routes. The gap is in the two unauthenticated `/internal/*` endpoints (findings 0004, 0005).

---

## Findings (Detailed)

### SEC-001: Secret values stored without encryption (find_sec_0001)

- **Severity:** high
- **OWASP Category:** A02:Cryptographic Failures
- **CWE:** CWE-312
- **Location:** `api/internal/routes/secrets.go:153`, `api/internal/db/secret_store.go:37`
- **Description:** The column is named `encrypted_value` and the migration comment says "Stores encrypted secret values", but no encryption is applied before the value reaches the database. The secrets route calls `store.Set(..., []byte(req.Value))` passing the raw plaintext bytes. The API module has no symmetric encryption dependency in `go.mod`. A database breach exposes all user secrets in plaintext.
- **Attack scenario:** An attacker who gains read access to the PostgreSQL database (e.g., via SQL injection in a future code path, a compromised DB credential, or a database backup) can read all user application secrets directly from the `encrypted_value` column without any decryption step.
- **Evidence:** `api/internal/routes/secrets.go:153`: `store.Set(r.Context(), uc.TenantID, project, req.Environment, req.App, req.Name, []byte(req.Value))`. No AES/GCM import anywhere in the API module.
- **Recommendation:** Apply AES-256-GCM encryption with a per-installation key stored outside the database (Kubernetes Secret, DO Secrets Manager, or Vault). The encryption key should be injected at API startup. Add `Encrypt(key, plaintext) []byte` and `Decrypt(key, ciphertext) []byte` functions before the store call. Never store the key in the same database as the ciphertext.
- **References:** CWE-312, OWASP Cryptographic Storage Cheat Sheet

---

### SEC-002: Gitea token leaked in error logs in source store (find_sec_0003)

- **Severity:** high
- **OWASP Category:** A09:Security Logging Failures
- **CWE:** CWE-532
- **Location:** `api/internal/source/gitea_store.go:233`
- **Description:** `GiteaSourceStore.gitCmdWithAuth` injects the Gitea token as a git `-c http.extraHeader=Authorization: token XXX` argument. When `gitCmd` encounters a failure (network error, clone failure, push rejection), it constructs the error message via `strings.Join(args, " ")` which includes the raw token value in the output. The gitops pusher (`gitea_pusher.go`) handles this correctly with `sanitizeArgs` and `sanitizeOutput` methods, but the source store has no equivalent sanitization — it is a copy of the pattern without the security fix.
- **Attack scenario:** Any git network error during source upload causes the Gitea token to appear in: (1) structured log output (slog), (2) error responses propagated to callers, and potentially (3) Prometheus metrics labels if error strings are captured.
- **Evidence:** `api/internal/source/gitea_store.go:233`: `return fmt.Errorf("%s: %w (output: %s)", strings.Join(args, " "), err, ...)` — `args` contains the auth header. Compare with `api/internal/gitops/gitea_pusher.go:338-360` which redacts the token correctly.
- **Recommendation:** Add `sanitizeOutput(output string) string` and `sanitizeArgs(args []string) []string` methods to `GiteaSourceStore`, identical to the implementations in `GiteaGitOpsPusher`. Apply them in `gitCmd` before constructing the error string.

---

### SEC-003: POST /internal/bootstrap has no authentication (find_sec_0005)

- **Severity:** high
- **OWASP Category:** A01:Broken Access Control
- **CWE:** CWE-306
- **Location:** `api/internal/server/server.go:113-116`, `api/internal/routes/bootstrap.go:64`
- **Description:** `POST /internal/bootstrap` is registered outside the auth middleware group with no token, no HMAC, and no IP allowlist. Any actor who can reach port 3001 can create an admin user and tenant, gaining full platform access. The endpoint is idempotent, so an attacker could silently create a rogue tenant that isn't the initial one.
- **Attack scenario:** If the Kubernetes NetworkPolicy is misconfigured or if a pod inside the cluster is compromised, an attacker can POST `{"admin_github_id": "attacker", "admin_login": "attacker", "tenant_slug": "rogue"}` and gain a valid tenant with admin role, then use the GitHub Bearer token flow to authenticate and access all API endpoints as a legitimate user.
- **Evidence:** `api/internal/server/server.go:113-116` registers the route before the auth group. `api/internal/routes/bootstrap.go:64` has no auth check.
- **Recommendation:** Add a `BOOTSTRAP_SECRET` environment variable. Verify an `X-Bootstrap-Secret` header on every call to `/internal/bootstrap`. Alternatively, disable the endpoint after initial bootstrap by checking a DB flag and returning 404 on subsequent calls. Defense-in-depth: the NetworkPolicy already restricts access but the endpoint should not rely solely on network controls.

---

### SEC-004: POST /internal/argocd-callback has no request verification (find_sec_0004)

- **Severity:** medium
- **OWASP Category:** A01:Broken Access Control
- **CWE:** CWE-306
- **Location:** `api/internal/routes/argocd_callback.go:47`
- **Description:** The ArgoCD callback endpoint has no HMAC signature validation, no shared-secret token check, and no Bearer token requirement. `ArgoCallbackSecret` exists in the config struct but is never wired to the handler. Any actor inside the cluster can POST a spoofed `{app_name, sync_status: "Synced", health_status: "Healthy"}` payload to prematurely activate a release.
- **Attack scenario:** A compromised pod inside the cluster sends `POST /internal/argocd-callback` with `app_name=tn-victim-production` and `sync_status=Synced, health_status=Healthy`. The API activates the release for that application before the actual deployment is complete or healthy, potentially routing traffic to a broken pod.
- **Evidence:** `api/internal/server/server.go:117-119` — no auth middleware. `api/internal/config/config.go:175` — `ArgoCallbackSecret` defined but never used in handler.
- **Recommendation:** Wire `ArgoCallbackSecret` from the config into `ArgoCallbackHandler`. In `Handle()`, verify `Authorization: Bearer <ArgoCallbackSecret>` header. The ArgoCD Notifications webhook configuration supports adding a custom Authorization header.

---

### SEC-005: LogQL injection via unvalidated `step` query parameter (find_sec_0002)

- **Severity:** medium
- **OWASP Category:** A03:Injection
- **CWE:** CWE-943
- **Location:** `api/internal/loki/client.go:49`, `api/internal/routes/logs.go:102-103`
- **Description:** The `step` URL query parameter is passed directly to the Loki client and interpolated into a LogQL selector string via `fmt.Sprintf`. While the `buildID` is validated via tenant ownership check (the user must own the build), `opts.Step` has no character allowlist validation. A malicious step value could inject additional LogQL selectors or operators.
- **Attack scenario:** An authenticated user with a valid build accesses `GET /api/v1/builds/{buildID}/logs?step=x%22}+|+line_filter+%22sensitive`. While Loki's query language is not a direct code execution environment, injection can enable reading log streams from other applications in the same Loki instance or bypass label-based access controls.
- **Evidence:** `api/internal/routes/logs.go:102-103`: `step` read from URL, no validation. `api/internal/loki/client.go:49`: `query = fmt.Sprintf(...)` with raw `opts.Step`.
- **Recommendation:** Add `var stepRegex = regexp.MustCompile("^[a-z0-9_-]{1,64}$")` and validate `opts.Step` against it before passing to the Loki client. Return HTTP 400 on invalid values.

---

### SEC-006: PostgreSQL connection uses sslmode=disable unconditionally (find_sec_0006)

- **Severity:** medium
- **OWASP Category:** A02:Cryptographic Failures
- **CWE:** CWE-319
- **Location:** `api/internal/db/connection.go:39`
- **Description:** The DSN hard-codes `sslmode=disable`, disabling TLS for all database traffic including production. While TLS skip is correctly blocked for ArgoCD (`ArgocdInsecure`) and the container registry (`RegistryInsecure`) in production, no equivalent check exists for the database. The DigitalOcean managed PostgreSQL supports and requires TLS on production clusters.
- **Recommendation:** Add `DATABASE_SSL_MODE` environment variable (default `disable` for dev). In `config.Validate()`, return an error if `IsProduction() && cfg.DBSSLMode == "disable"`. Update `DSN()` to use the configured SSL mode.

---

### SEC-007: Hardcoded MinIO credentials with only log-level warnings in production (find_sec_0007)

- **Severity:** medium
- **OWASP Category:** A05:Security Misconfiguration
- **CWE:** CWE-1188
- **Location:** `api/internal/config/config.go:173-174`, `infra/helm/minio/minio-credentials-secret.yaml:23-24`
- **Description:** `MinIOAccessKey` defaults to `loopzero-admin` and `MinIOSecretKey` to `loopzero-dev-password`. The production detection uses `slog.Error` (a log line) rather than failing the server. A misconfigured production deployment starts successfully with well-known default credentials. The same values are committed to `infra/helm/minio/minio-credentials-secret.yaml`.
- **Recommendation:** Change the production checks to `return fmt.Errorf(...)` matching the pattern used for `ArgocdInsecure`. Remove hardcoded defaults for security-sensitive credentials (use empty string defaults that force explicit configuration). The Helm values files should use placeholder comments, not actual credential values.

---

### SEC-008: Path traversal defense-in-depth missing in gitops layer (find_sec_0008)

- **Severity:** low
- **OWASP Category:** A01:Broken Access Control
- **CWE:** CWE-22
- **Location:** `api/internal/gitops/gitea_pusher.go:95`
- **Description:** `filepath.Join(repoDir, opts.Environment, opts.ProjectID)` constructs the manifest path without verifying the result stays within `repoDir`. The gitops layer trusts upstream validation (route-level `environmentRegex` and `rfc1123`) but does not defend itself. If a future code path calls `Push()` directly without upstream validation (e.g., a background reconciler), crafted values could escape the temp directory.
- **Recommendation:** After constructing `envPath`, verify `strings.HasPrefix(filepath.Clean(envPath), filepath.Clean(repoDir))`. Return an error if the check fails. This is standard defense-in-depth for any function that constructs filesystem paths from external inputs.

---

### SEC-009: No HTTP security headers on API responses (find_sec_0009)

- **Severity:** low
- **OWASP Category:** A05:Security Misconfiguration
- **CWE:** CWE-693
- **Location:** `api/internal/server/server.go:87`
- **Description:** The chi router middleware chain has no security headers middleware. Headers including `X-Content-Type-Options`, `X-Frame-Options`, and `Strict-Transport-Security` are absent from API responses. The Traefik `security-headers` middleware applies these to user application IngressRoutes but not to the API server itself. Low severity for an API-only server but represents incomplete defense-in-depth.
- **Recommendation:** Add a `SecurityHeaders` middleware to the chi router chain. At minimum: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `X-XSS-Protection: 0` (modern browsers), `Referrer-Policy: strict-origin-when-cross-origin`.

---

## Confirmed Secure Patterns

The following were reviewed and found to be well-implemented:

1. **SQL Injection (A03):** All 9 store files use parameterized queries exclusively (`$1`, `$2`, ... placeholders). Zero string-formatted SQL queries found. CLEAN.
2. **SSRF (A08):** All outbound HTTP calls target configuration-provided service URLs (Gitea, ArgoCD, Loki, MinIO, CF KV). No user-controlled URLs are passed to HTTP clients. CLEAN.
3. **Command Injection:** `exec.CommandContext` is called with `args[0]` as the binary (always `"git"`) and the remaining args as a slice — not passed to a shell. No `sh -c` or shell interpolation. CLEAN.
4. **Path Traversal (A01 / SEC-3):** `ValidateAppPath` in `api/internal/validation/path.go` correctly rejects `..` components, absolute paths, and shell metacharacters. Applied at build submission route. CLEAN.
5. **IDOR (A01):** Build ownership verified via `GetBuildTenantID` before every build read/cancel/log operation. Project lookups scoped to `uc.TenantID`. Secret queries parameterized by `tenantID` from authenticated context. CLEAN.
6. **Auth Cache Security:** Cache is bounded at 10,000 entries, evicts expired entries first, then oldest entries under pressure. No unbounded growth possible. CLEAN.
7. **Concurrency / Advisory Locks:** `withEnvironmentLock` uses `pg_advisory_xact_lock` (transaction-scoped) correctly. Concurrent build check is inside the lock. CLEAN.
8. **Token Injection in git URLs (SEC-1):** Credentials passed via `http.extraHeader` config arg, not embedded in remote URLs. Correctly implemented in `GiteaGitOpsPusher`. Gap exists in `GiteaSourceStore` (SEC-003 above).
9. **Rate Limiting:** Per-tenant fixed-window rate limiter applied to all mutation routes. Unauthenticated requests keyed as "anonymous". CLEAN.
10. **Idempotency:** Idempotency middleware active on build creation and cancellation. CLEAN.

---

## Recommendations (Prioritized)

1. **[HIGH] Implement actual encryption for secrets** (SEC-001): The column name implies a security guarantee that does not exist. Add AES-256-GCM encryption before the store call. This is the most impactful fix.
2. **[HIGH] Sanitize Gitea token from error logs in source store** (SEC-002): Mirror `sanitizeArgs`/`sanitizeOutput` from the gitops pusher. Simple copy-paste fix.
3. **[HIGH] Add authentication to /internal/bootstrap** (SEC-003): Add a `BOOTSTRAP_SECRET` header check. Consider disabling the endpoint post-bootstrap.
4. **[MEDIUM] Wire ArgoCallbackSecret to the callback handler** (SEC-004): The config field already exists. Add header verification in `Handle()`.
5. **[MEDIUM] Validate `step` parameter before LogQL interpolation** (SEC-005): Add a one-line regex allowlist. Blocks LogQL injection.
6. **[MEDIUM] Enforce PostgreSQL TLS in production** (SEC-006): Add `DATABASE_SSL_MODE` config and production gate.
7. **[MEDIUM] Make MinIO credential misconfiguration fatal in production** (SEC-007): Change `slog.Error` to `return fmt.Errorf`.
8. **[LOW] Add path escape check in gitops layer** (SEC-008): Defense-in-depth `strings.HasPrefix` check.
9. **[LOW] Add security headers middleware to chi router** (SEC-009): X-Content-Type-Options, X-Frame-Options at minimum.
