# Deep Code Review

**Date:** 2026-03-29
**Target:** /home/paulo/loopzero
**Reviewer:** code-reviewer

## Executive Summary

- Files reviewed: 10 (build_reconcile_loop.go, container_delivery.go, stream_handler.go, event_store.go, auth.go, build_orchestrator.go, builds.go, source.go, validation/path.go, lock/environment_lock.go)
- Critical path coverage: build reconcile loop, container delivery activation, SSE streaming, auth middleware, input validation
- Total findings: 10 (critical: 0, high: 1, medium: 5, low: 4)
- Key concern: The active-release supersession query in phaseEFinalize fires AFTER the current release is already marked ACTIVE, making the query non-deterministic — it can return the current release, leaving the previous active release permanently un-superseded.

---

## Critical Paths Reviewed

### Build Reconcile Loop
- Files: `api/internal/services/build_reconcile_loop.go`, `api/internal/services/build_orchestrator.go`
- Findings: 3 (find_code_0002, find_code_0006, find_code_0010)
- Assessment: NEEDS ATTENTION — error from FindReleaseForBuild is discarded (idempotency guard bypass), and OnBuildCompleted failures leave the build stuck in RUNNING for repeated reconciliation.

### Container Delivery Activation
- Files: `api/internal/services/container_delivery.go`
- Findings: 3 (find_code_0001, find_code_0005, find_code_0009)
- Assessment: NEEDS ATTENTION — supersession ordering bug can leave two releases permanently ACTIVE in the same environment; rollback uses single-shot sync trigger without retry.

### SSE Stream Handler
- Files: `api/internal/sse/stream_handler.go`, `api/internal/sse/event_store.go`
- Findings: 1 (find_code_0007)
- Assessment: SOLID — connection limiting (200 max) via atomic counter, client disconnection detected via context cancellation, heartbeat at 15s, safety timeout at 5 minutes. One minor id=0 semantic issue on JOURNAL_EXPIRED.

### Auth Middleware
- Files: `api/internal/middleware/auth.go`
- Findings: 2 (find_code_0003, find_code_0004)
- Assessment: NEEDS ATTENTION — cache eviction has counter/entry timing gap; token revocation window is 5 minutes and not documented. sync.Map is thread-safe; cache size limit (10,000 entries) prevents unbounded growth.

### Input Validation
- Files: `api/internal/validation/path.go`, `api/internal/routes/builds.go`, `api/internal/routes/source.go`
- Findings: 1 (find_code_0008)
- Assessment: SOLID — path traversal protection is correct (reject `..`, absolute paths, shell metacharacters), environment name regex enforced, app paths validated at route boundary. One fragile error-type detection in source upload.

---

## Findings (Detailed)

### CODE-0001: phaseEFinalize supersedes wrong release — GetActiveByEnvTx called AFTER current release already marked ACTIVE

- **Severity:** high
- **Checklist area:** transaction, contract
- **Location:** `api/internal/services/container_delivery.go:320`
- **Description:** In `phaseEFinalize`, the current release is transitioned to `status='ACTIVE'` at line 302 via `UpdateStatus(ReleaseSyncCompleted)`. Five lines later, `GetActiveByEnvTx` (line 320) queries `releases WHERE status='ACTIVE'` — inside the same transaction. At this point, the current release IS already ACTIVE, so the query may return the new release instead of the previous one. The ID guard at line 324 (`prevRelease.ID != release.ID`) then prevents the old release from being superseded, leaving it permanently ACTIVE. Two concurrent ACTIVE releases for the same environment violate the one-active-release invariant.
- **Evidence:**
  ```go
  // Line 302 — marks current release ACTIVE
  c.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseSyncCompleted))
  // ...
  // Line 320 — queries WHERE status='ACTIVE' — may return the release we JUST activated
  prevRelease, err := c.releaseStore.GetActiveByEnvTx(ctx, tx, ...)
  if prevRelease != nil && prevRelease.ID != release.ID { // guard prevents self-supersession
      c.releaseStore.UpdateStatus(ctx, tx, prevRelease.ID, statemachine.ReleaseNewActivated)
  }
  ```
- **Recommendation:** Query the previous active release BEFORE marking the new one ACTIVE:
  ```go
  // Step 1: find previous active release (before we change anything)
  prevRelease, err := c.releaseStore.GetActiveByEnvTx(ctx, tx, ...)
  if err != nil { return ... }
  // Step 2: mark new release ACTIVE
  c.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseSyncCompleted))
  // Step 3: supersede old release
  if prevRelease != nil && prevRelease.ID != release.ID {
      c.releaseStore.UpdateStatus(ctx, tx, prevRelease.ID, statemachine.ReleaseNewActivated)
  }
  ```
- **Risk if not fixed:** After every successful deployment, the previous active release is left in ACTIVE status. Over time, multiple releases accumulate in ACTIVE state for the same environment. Rollback queries (GetActiveByEnv) become non-deterministic. The ReleaseReaper may not correctly identify which release to supersede.

---

### CODE-0002: retryReleaseActivation silently swallows FindReleaseForBuild errors — idempotency guard bypassed on DB failure

- **Severity:** medium
- **Checklist area:** error_handling, transaction
- **Location:** `api/internal/services/build_reconcile_loop.go:332`
- **Description:** `existingRelease, _ := r.orch.FindReleaseForBuild(ctx, build.ID)` — the error return is discarded. If the DB query fails (transient connection error, context timeout), `existingRelease` is `nil`. The code falls through to reconstruct `BuildCallbackRequest` and calls `RetryReleaseActivation`, which calls `Activate()`, which calls `phaseACreateRelease` to CREATE a new release record. The idempotency guard (preventing duplicate releases) is defeated by any transient DB error in the lookup.
- **Evidence:** Line 332: `existingRelease, _ := r.orch.FindReleaseForBuild(ctx, build.ID)` — blank identifier discards error. The function comment warns against duplicate releases in an infinite loop, but does not account for the DB-error code path.
- **Recommendation:**
  ```go
  existingRelease, err := r.orch.FindReleaseForBuild(ctx, build.ID)
  if err != nil {
      r.logger.Error("reconcile loop: failed to look up existing release for RELEASING build, skipping",
          "build_id", build.ID, "error", err)
      return // retry next tick
  }
  ```
- **Risk if not fixed:** A single transient DB error during the RELEASING-state retry loop causes duplicate release records. Multiple releases for the same build in different states (one PENDING, one GIT_MIRROR_FAILED) creates inconsistent state that the system has no recovery path for.

---

### CODE-0003: Auth cache size counter incremented before cache.Store() — creates window for counter/entry mismatch

- **Severity:** medium
- **Checklist area:** concurrency
- **Location:** `api/internal/middleware/auth.go:182`
- **Description:** `cacheSize.Add(1)` at line 182 increments the counter before `cache.Store()` at line 186. Under high concurrency, N goroutines can all call `Add(1)` between each other's Store calls. This inflates the counter above the actual entry count, triggering unnecessary evictions. The eviction logic uses `LoadAndDelete` correctly (preventing double-decrement), but the inflation means entries may be evicted even when the cache is below capacity.
- **Evidence:** Lines 182–186:
  ```go
  if cacheSize.Add(1) > maxTokenCacheSize {
      evictCache(cache, cacheSize, now)
  }
  cache.Store(token, &cachedValidation{...})
  ```
  Four lines separate the counter increment from the actual store.
- **Recommendation:** Move `cacheSize.Add(1)` to after `cache.Store()`. This does not eliminate all races (two goroutines can still both decide the cache is under limit), but it aligns the counter increment with the actual entry existence, making eviction decisions more accurate.
- **Risk if not fixed:** Under a burst of concurrent requests with new tokens (e.g., CI runs with fresh tokens), the cache evicts valid entries prematurely. This increases GitHub API calls and latency spikes, but does not cause authentication failures or incorrect access grants.

---

### CODE-0004: Auth cache does not detect token revocation within 5-minute TTL window

- **Severity:** medium
- **Checklist area:** contract (security contract)
- **Location:** `api/internal/middleware/auth.go:166`
- **Description:** Token validation results are cached for 5 minutes. A revoked GitHub token (deleted PAT, org membership removed, account suspended) continues to authenticate API requests for up to 5 minutes after revocation. Tenant membership is NOT cached (GetMemberships is called on every request), but the token-validity check is. An attacker who loses access to a GitHub org at T=0 retains API access until T+5min.
- **Evidence:** Line 80: `return AuthWithTTL(validator, store, 5*time.Minute)`. Cache hit path at line 164–167 returns immediately without contacting GitHub.
- **Recommendation:** This is a known trade-off in token-caching auth systems. Mitigations: (1) Expose a `POST /api/v1/auth/invalidate` endpoint that purges a token from the cache (for use in logout flow). (2) The CLI's `loopzero logout` command should call this endpoint. (3) Document the 5-minute revocation window in the security model (docs/technical.md §16). The current behavior is not a bug per se but the window is undocumented and the logout flow does not invalidate the server-side cache.
- **Risk if not fixed:** Revoked users retain access for up to 5 minutes. In a security incident scenario (compromised token, employee offboarding), this window may be significant.

---

### CODE-0005: OnBuildCompleted: comment says "Transition to SUCCEEDED" but actually transitions to RELEASING

- **Severity:** low
- **Checklist area:** quality (misleading comment, maintainability risk)
- **Location:** `api/internal/services/build_orchestrator.go:619`
- **Description:** The comment at line 619 reads `// Transition to SUCCEEDED` but the code at line 620 calls `statemachine.BuildSucceededEvent` which, per the state machine definition in `pkg/statemachine/build.go:78`, transitions RUNNING → RELEASING (not RUNNING → SUCCEEDED). RELEASING is the intermediate state introduced in MEET-2026-03-26-008. The code is functionally correct — Phase 2 later transitions RELEASING → SUCCEEDED. The comment will mislead future maintainers into assuming the build is SUCCEEDED after Phase 1.
- **Evidence:** `pkg/statemachine/build.go:78`: `{From: BuildRunning, Event: BuildSucceededEvent, To: BuildReleasing}`.
- **Recommendation:** Change comment to: `// Transition RUNNING → RELEASING (image built, release activation pending)`
- **Risk if not fixed:** A future developer adding logic after Phase 1 may incorrectly assume the build is in SUCCEEDED state and add terminal-state checks that pass incorrectly, or emit events that should only fire on SUCCEEDED.

---

### CODE-0006: Reconcile loop goroutines share parent server context — safe by design but undocumented

- **Severity:** low
- **Checklist area:** concurrency
- **Location:** `api/internal/services/build_reconcile_loop.go:164`
- **Description:** The goroutines spawned in `reconcile()` capture the server lifecycle context `ctx` from `Start(ctx)`. This is intentional: server shutdown cancels the context, which propagates to all in-flight reconciliations, causing their DB transactions to roll back cleanly. The build stays in RELEASING and is retried on next boot. This design is correct and safe. However, there is no comment explaining this invariant, making the behavior look like a potential resource leak to reviewers.
- **Evidence:** Line 103: `Start(ctx context.Context)` — parent context. Line 164: `r.reconcileBuild(ctx, &b)` — same ctx.
- **Recommendation:** Add a comment in `reconcile()` explaining that goroutines intentionally use the server context, and that context cancellation (shutdown) causes clean rollback + RELEASING-state retry on next boot.
- **Risk if not fixed:** Low — the design is correct. Risk is a future change that wraps ctx with a goroutine-local deadline, accidentally cutting off long-running syncs.

---

### CODE-0007: JOURNAL_EXPIRED SSE event uses id="0" — breaks reconnect semantics

- **Severity:** low
- **Checklist area:** contract
- **Location:** `api/internal/sse/stream_handler.go:270`
- **Description:** When the event journal is pruned and a reconnecting client's Last-Event-ID has no newer events, a synthetic JOURNAL_EXPIRED event is sent with `id: 0`. Per the SSE spec, the browser stores this as the Last-Event-ID for the next reconnect. At line 235–237, the handler parses the Last-Event-ID: `if lastEventID > 0`. Sending id=0 means the reconnect is treated as a fresh connection (lastEventID stays 0), not as a post-expiry reconnect. This is accidentally correct (fresh start after expiry), but the mechanism is fragile — if the id parsing changes, the behavior could silently regress.
- **Evidence:** Line 270: `writeSSEEvent(w, flusher, "0", "JOURNAL_EXPIRED", ...)`. Line 244–246: `if lastEventID > 0 { replayStart = lastEventID }` — id=0 means replayStart=0 (from beginning).
- **Recommendation:** Document explicitly why id=0 is used for JOURNAL_EXPIRED: "id=0 intentionally resets the client's Last-Event-ID to force a full replay on reconnect, since the journal no longer has the events the client missed." Alternatively, use a non-numeric sentinel that will never be parsed as a valid event ID.
- **Risk if not fixed:** Cosmetic for now. If the SSE reconnection handling changes, the intent is unclear.

---

### CODE-0008: source.go: MaxBytesReader error detected via string comparison — fragile

- **Severity:** low
- **Checklist area:** error_handling
- **Location:** `api/internal/routes/source.go:84`
- **Description:** `if err.Error() == "http: request body too large"` detects the too-large error by comparing against a hardcoded Go stdlib error message string. The `http.MaxBytesError` type has been available since Go 1.19 and should be used with `errors.As()` instead. The stdlib error message has already changed between Go versions historically, and relying on it is fragile.
- **Evidence:** Line 84: `if err.Error() == "http: request body too large" {`
- **Recommendation:**
  ```go
  var maxBytesErr *http.MaxBytesError
  if errors.As(err, &maxBytesErr) {
      writeLoopZeroError(w, loopzeroerrors.ErrPayloadTooLarge)
      return
  }
  ```
- **Risk if not fixed:** If Go changes the error message text (as happened between 1.18 and 1.19), oversized uploads return a generic 400 INVALID_REQUEST instead of 413 PAYLOAD_TOO_LARGE. The upload still fails, but the error message is misleading.

---

### CODE-0009: Rollback ArgoCD sync is single-shot, failure swallowed — reported as success

- **Severity:** medium
- **Checklist area:** error_handling, transaction
- **Location:** `api/internal/services/container_delivery.go:157`
- **Description:** In `Rollback()`, after successfully pushing old manifests to the gitops repo, `TriggerSync` is called once with no retry. If the call fails (ArgoCD is temporarily unreachable, network blip), the error is logged as a warning and the function returns `nil` — success. The rollback orchestrator receives a success response and transitions the rollback state machine to COMPLETED. The old manifests ARE in the gitops repo, so ArgoCD will eventually sync them on its next auto-sync cycle. However, the operator's experience is "rollback completed" while the workload cluster is still running the broken version.
- **Evidence:** Line 157:
  ```go
  if syncErr := c.sync.TriggerSync(ctx, argoAppName); syncErr != nil {
      slog.Warn("container rollback: argocd sync trigger failed, continuing", "error", syncErr)
  }
  ```
  Compare to Activate path at line 104: `c.triggerSyncWithRetry(ctx, argoAppName)` — 3 attempts, exponential backoff.
- **Recommendation:** Use the same `triggerSyncWithRetry` already available on the struct:
  ```go
  if syncErr := c.triggerSyncWithRetry(ctx, argoAppName); syncErr != nil {
      return fmt.Errorf("container rollback: argocd sync trigger failed after retries: %w", syncErr)
  }
  ```
  The rollback orchestrator should handle this error by transitioning to FAILED state.
- **Risk if not fixed:** A rollback reports COMPLETED but the workload cluster remains on the broken release until the next ArgoCD auto-sync (minutes to hours depending on ArgoCD poll interval). During an incident, this compounds the outage.

---

### CODE-0010: onWorkflowSucceeded: OnBuildCompleted error leaves build stuck in RUNNING, triggers tight retry loop

- **Severity:** medium
- **Checklist area:** error_handling, concurrency
- **Location:** `api/internal/services/build_reconcile_loop.go:311`
- **Description:** When `r.orch.OnBuildCompleted()` returns an error (e.g., Phase 1 DB write fails — the transition to RELEASING fails), the error is logged but no recovery action is taken. The build remains in RUNNING state. On the next reconcile tick (5 seconds later), the Argo Workflow still shows Succeeded, so `onWorkflowSucceeded` is called again, calling `OnBuildCompleted` again. This creates a tight retry loop until the DB recovers or the RUNNING timeout (30 min) fires. Under a sustained DB outage, this generates ~6 retry attempts per minute per stuck build.
- **Evidence:** Lines 311–314:
  ```go
  if err := r.orch.OnBuildCompleted(ctx, build.ID, result); err != nil {
      r.logger.Error("reconcile loop: build completion processing failed",
          "build_id", build.ID, "error", err)
  }
  ```
  No failBuild() call, no rate-limiting of retries.
- **Recommendation:** Track the number of consecutive `OnBuildCompleted` failures per build ID in a `sync.Map` counter. After N failures (e.g., 5), call `failBuild()` with reason "Build completion failed after repeated retries." Reset the counter on success. This prevents indefinite retries on non-transient errors (e.g., state machine violation) while allowing recovery from transient DB errors.
- **Risk if not fixed:** Under DB instability, every succeeded Argo Workflow generates ~6 retry DB calls per minute. This can contribute to a cascading failure: DB is overloaded → retries increase → DB load increases. The RUNNING timeout (30 min) is the only current safety net.

---

## Error Handling Summary

**Good patterns found:**
- `WithEnvironmentLock` correctly defers rollback via `defer tx.Rollback(ctx)`, ensuring all lock acquisitions are released on error or panic.
- `failBuild()` re-fetches the build inside the lock before transitioning, preventing races on state change.
- `OnBuildCompleted` checks for terminal state both before and after lock acquisition (double-checked locking pattern is correct here given state-machine semantics).
- Phase C of container delivery correctly emits failure events before returning, ensuring the CLI receives feedback.

**Issues found:**
- `existingRelease, _ =` (CODE-0002): error discarded in the idempotency guard — the most important place to not swallow errors.
- `TriggerSync` in Rollback (CODE-0009): single-shot with swallowed failure — inconsistent with the Activate path.
- `OnBuildCompleted` errors in the reconcile loop (CODE-0010): logged but no recovery action, enabling tight retry loops.
- String-based error type detection in source upload (CODE-0008): fragile but not critical.

---

## Concurrency Summary

**Safe patterns found:**
- `sync.Map` used for `r.active` build tracking — thread-safe, no races.
- `atomic.Int64` used for `activeStreams` in SSE handler — correct increment-then-check-then-decrement pattern.
- `atomic.Int64` used for `cacheSize` in auth cache — all operations are atomic.
- Semaphore via `chan struct{}` for MaxConcurrent bounds — correct select/default pattern prevents blocking.
- `LoadOrStore` used correctly to prevent duplicate build processing across ticks.

**Issues found:**
- Auth cache counter incremented before Store() (CODE-0003): minor timing gap that inflates the counter, triggering unnecessary evictions under high concurrency.
- All other identified concurrency patterns are correctly implemented.

---

## Transaction Summary

**Good patterns found:**
- All state mutations use `withEnvironmentLock` with `pg_advisory_xact_lock` (Rule 6/7 compliant).
- `SET LOCAL lock_timeout = '5s'` scoped to transaction only (correct use of SET LOCAL).
- Re-fetch inside lock before every state update prevents TOCTOU races.
- `IsBuildTerminal` check inside lock handles concurrent cancel races correctly.
- The RELEASING state machine design (non-terminal, retried on boot) is a correct crash-recovery pattern.

**Issues found:**
- `phaseEFinalize` queries active release AFTER marking current release active (CODE-0001): the transaction ordering breaks the supersession invariant. This is the highest-severity finding.

---

## Recommendations (Prioritized)

1. **Fix phaseEFinalize supersession ordering (CODE-0001):** Move `GetActiveByEnvTx` to before `UpdateStatus(ReleaseSyncCompleted)`. This is a two-line reorder within the same transaction — low risk, high impact. Without this fix, every successful deploy leaves the previous release in permanently ACTIVE state.

2. **Handle FindReleaseForBuild error (CODE-0002):** Add a nil-error check before proceeding to RetryReleaseActivation. This prevents duplicate release creation on DB transient errors. One-line fix with high safety value.

3. **Use triggerSyncWithRetry in Rollback (CODE-0009):** Replace single-shot TriggerSync with the existing retry helper. Rollback is an incident-response operation — it should be as reliable as the deploy path.

4. **Rate-limit OnBuildCompleted retries in reconcile loop (CODE-0010):** Add a per-build retry counter in a sync.Map. Fail the build after N consecutive failures. Prevents tight retry loops under DB instability.

5. **Fix auth cache counter ordering (CODE-0003):** Move cacheSize.Add(1) after cache.Store(). Minor but improves eviction accuracy under high concurrency.

6. **Fix MaxBytesReader error detection (CODE-0008):** Use errors.As() with *http.MaxBytesError instead of string comparison. Stdlib-safe.

7. **Document auth token revocation window (CODE-0004):** Add a LogoutCacheInvalidate endpoint or lower TTL to 1-2 minutes. Document the 5-minute window in the security model.

8. **Fix misleading comment in OnBuildCompleted (CODE-0005):** Change "Transition to SUCCEEDED" to "Transition to RELEASING." Low risk, high clarity value.
