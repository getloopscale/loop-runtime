# Risk Hypotheses — Phase 1 Baseline

Generated: 2026-03-29
Target: /home/paulo/loopzero

These are testable hypotheses formed from the initial architecture scan. Each will be validated or invalidated in later phases.

---

## H1: Rate Limiter Memory Leak (Medium)

**Observation:** `api/internal/middleware/ratelimit.go` uses an in-memory `map[string]*bucket` with a cleanup goroutine that has no shutdown signal (no `context.Context`). The goroutine leaks on server shutdown.

**Risk:** The cleanup goroutine runs forever via `for range ticker.C` with no exit path. Under graceful shutdown, this goroutine is leaked. Additionally, if the server is restarted frequently (e.g., in dev), goroutines accumulate.

**Test in Phase 4:** Verify goroutine shutdown path. Check if `cleanupLoop` respects `ShutdownCtx`.

---

## H2: Rate Limiter Keyed by TenantID After Auth — Anonymous Requests Share One Bucket (Medium)

**Observation:** `ratelimit.go:82` — unauthenticated requests all share the key `"anonymous"`. If rate limiting is applied before auth middleware, all unauthenticated traffic competes against one bucket. If after auth, unauthenticated endpoints have no rate limiting.

**Risk:** Either DoS on unauthenticated endpoints (no per-IP limiting) or false rate limiting when many unauthenticated clients share one bucket.

**Test in Phase 6:** Verify rate limiter placement in middleware chain and key strategy.

---

## H3: Advisory Lock Hash Collision — hashtext() Collision Risk (Low)

**Observation:** `lock/environment_lock.go:68-70` — lock key is `"{tenant}:{project}:{env}"` hashed via `hashtext()` to int4. The code acknowledges ~0.001% collision risk with 2000 environments.

**Risk:** False serialization (liveness impact, not safety). Acceptable per spec, but worth documenting as a known limitation.

**Test in Phase 4:** Verify collision impact analysis is correct. No data corruption possible.

---

## H4: GitHub Token as Primary Auth — No JWT, No Session Rotation (High)

**Observation:** `middleware/auth.go` validates GitHub OAuth tokens directly (no session JWT). Results cached for 5 minutes. Token revocation on GitHub side is delayed by cache TTL.

**Risk:** If a GitHub token is compromised, it remains valid in LoopZero for up to 5 minutes after revocation. No server-side session invalidation mechanism.

**Test in Phase 6:** Map full auth flow, check for token revocation handling, cache invalidation on error.

---

## H5: Multi-Cluster Credential Management (High)

**Observation:** The API server connects to 3 Kubernetes clusters (mgmt, build, workload) with kubeconfigs loaded from env vars. Credentials for Gitea, ArgoCD, MinIO, and PostgreSQL are also env-var based.

**Risk:** Credential rotation requires API restart. No runtime secret refresh mechanism visible. In production, a credential expiration during operation could cause cascading failures.

**Test in Phase 5:** Check credential refresh patterns, rotation procedures, and secret sync (ESO).

---

## H6: Build Reconcile Loop — Potential for Stuck Builds (High)

**Observation:** Build state machine has timeout states (QUEUED>5m, RUNNING>30m, RELEASING>10m). A `BuildReconcileLoop` and `BuildReaper` service exist in `api/internal/services/`.

**Risk:** If the reconcile loop crashes or misses a build, it could remain in RUNNING state indefinitely. Need to verify that the reaper catches all stuck builds and that reconciliation is truly idempotent.

**Test in Phase 4:** Read BuildReconcileLoop and BuildReaper. Verify timeout enforcement, crash recovery, idempotency.

---

## H7: SSE Stream Resource Exhaustion (Medium)

**Observation:** Server config has `SSEMaxConcurrentStreams int64` suggesting a limit exists, but need to verify it's enforced. SSE connections are long-lived HTTP connections.

**Risk:** Without proper limits, many simultaneous `loopzero deploy` commands could exhaust server connection capacity. Need to verify connection counting and cleanup.

**Test in Phase 4:** Read SSE implementation, check connection limit enforcement, timeout handling.

---

## H8: GitOps Push as Single Point of Failure (High)

**Observation:** The deploy flow requires a successful git push to Gitea for ArgoCD sync. If Gitea is unavailable, deploys fail completely.

**Risk:** No fallback for Gitea outages. Circuit breaker exists for some services but need to verify Gitea push has retry/circuit-breaker protection.

**Test in Phase 4:** Check GitOpsPusher error handling, retry logic, and circuit breaker coverage.

---

## H9: Forward-Only Migrations — No Rollback Path (Medium)

**Observation:** CLAUDE.md states "forward-only, .up.sql only" for migrations. The project explicitly has no down migrations.

**Risk:** A bad migration in production requires manual intervention. Data schema rollback is impossible without manual SQL. This is a design choice, not a bug, but increases operational risk.

**Test in Phase 5:** Verify migration safety, check for destructive operations in existing migrations.

---

## H10: ArgoCD Manual Sync Only — Convergence Delay (Medium)

**Observation:** ArgoCD is configured for manual sync only (not auto-sync). The API triggers sync explicitly.

**Risk:** If the API's ArgoCD sync call fails after gitops push, the desired state is in Git but not applied. Need to verify retry/recovery mechanism for failed syncs.

**Test in Phase 4:** Check ArgoCD sync client error handling and retry logic.

---

## H11: Static Frontend Delivery — CF Worker KV Consistency (Medium)

**Observation:** Static frontends are served via Cloudflare Worker reading routing from KV. The API writes to KV during build completion.

**Risk:** KV is eventually consistent. There's a window after deploy where the old version is still served. Need to verify the write timing and any cache invalidation.

**Test in Phase 4:** Read CF KV client, check write timing in deploy flow.

---

## H12: Operator CRDs Without Webhooks — No Admission Validation (Medium)

**Observation:** 9 CRDs defined but no webhook configuration visible. CRD validation relies solely on OpenAPI schema in CRD spec.

**Risk:** Invalid CRs can be created that pass OpenAPI validation but violate business rules (e.g., invalid state transitions via direct kubectl). Controllers must handle all invalid states defensively.

**Test in Phase 3:** Check CRD validation strategy, controller defensive handling of invalid input.

---

## H13: "Implemented but Not Wired" Pattern (High)

**Observation:** CLAUDE.md explicitly warns about this systemic pattern — "Code exists in correct location but not integrated in main.go or runtime path."

**Risk:** Features that appear complete in code review but are actually unreachable at runtime. This is a known pattern in the project.

**Test in Phase 2:** Comprehensive wiring audit — trace from entry point to every feature handler.

---

## Summary Priority

| Priority | Hypothesis | Risk Level |
|----------|------------|------------|
| P1 | H4 (GitHub token auth) | High |
| P1 | H5 (Multi-cluster credentials) | High |
| P1 | H6 (Stuck builds) | High |
| P1 | H8 (GitOps SPOF) | High |
| P1 | H13 (Not wired) | High |
| P2 | H2 (Rate limiter keying) | Medium |
| P2 | H7 (SSE exhaustion) | Medium |
| P2 | H10 (ArgoCD sync failure) | Medium |
| P2 | H11 (CF KV consistency) | Medium |
| P2 | H12 (No admission webhooks) | Medium |
| P3 | H1 (Rate limiter goroutine leak) | Medium |
| P3 | H9 (Forward-only migrations) | Medium |
| P4 | H3 (Hash collision) | Low |
