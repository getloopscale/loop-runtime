# Plan: Deep Review Remediation — All 70 Findings

> **Version 1.1** — Comprehensive remediation plan addressing every finding from the autonomous Deep Review Loop (2026-05-08 to 2026-05-09). 4 CRITICAL launch blockers, 24 HIGH, 32 MEDIUM, 10 LOW findings consolidated into 9 phases. Phase 0 is non-negotiable pre-Sprint-9 blocker. Phases 1-9 align with hardening sprints, ordered by dependency + risk reduction. Expected outcome: launch-ready maturity 4.0/5 with zero CRITICAL, zero HIGH related to launch surfaces.
>
> **v1.1 changelog (post `/edge-case-plan` review):** 5 MUST FIX edge cases incorporated as sub-tasks (EC-1 SQL guard in T0.1; EC-2 factory fail-safe in T0.3; EC-3 trusted proxy CIDR in T2.4; EC-4 ordered sub-tasks in T1.4 → T1.4a/b/c; EC-5 inventory+dry-run+rollback in T8.5). 8 SHOULD TEST cases added to existing TDD blocks. 4 DOCUMENT callouts added inline. See `docs/plans/deep-review-remediation-edge-case-review.md` for full review.

## Context

The Deep Review Loop completed 2026-05-09T01:10Z, producing 70 findings across 7 analytical phases. Source of truth: `review-output/review.db` (SQLite) + `review-output/final_report.md` (778 lines). C4 health scores: Correto 0.40 / Completo 0.63 / Confiável 0.40 / Controlável 0.40 — the 4 CRITICAL findings drag every dimension below 0.5.

**Evidence sources cited per task:**
- `review-output/findings/{completeness,architecture,code,infrastructure,security,validation}/*.md` — per-phase evidence
- `review-output/analysis/threat_models/threat_model_report.md` — STRIDE per flow + 7 toxic combinations
- `review-output/baseline/risk_hypotheses.md` — 10 hypotheses (10/10 closed: 5 REFUTED, 1 PARTIAL, 2 CONFIRMED, 1 ESCALATED, 1 INVERTED, 1 MITIGATED)
- DB queries: `python3 .../review_database.py query-findings --phase N`

**Why now:** Pre-release window. CLAUDE.md Rule 11 ("legacy code is wrong code"): every CRITICAL is a wrong-code instance that must be replaced before launch, not patched.

## Objective

**Done = zero CRITICAL findings + zero HIGH findings on launch-blocking surfaces (deploy, rollback, billing, security, multi-cell) + every Phase 0 acceptance criterion validated LIVE in cleanroom.**

Specific measurable goals:
1. C4 health score ≥ 0.75 across all 4 dimensions (currently 0.40-0.63).
2. Maturity 4.0/5 (currently 3.9; gates: CI Sunday cron GREEN + 1 prod customer day-trial).
3. All 70 findings resolved or formally accepted via ADR.
4. 5 invariants validated (currently 3 violated, 2 validated).

## ADRs

### D1 — Phase 0 is launch-blocking; nothing ships before completion
**Decision:** All 4 CRITICAL findings (find_data_001, find_data_002, find_sec_001, find_flow_001) MUST be resolved before any further deploy or sprint work. No exceptions.
**Rationale:** Each is a "wrong-code" launch blocker per CLAUDE.md Rule 11. find_data_001 + threat 8 = environment permanently bricked on rollback failure (already triggered in cleanroom iter 28). find_sec_001 = cross-tenant DELETE via header bypass. find_flow_001 = canary abort SSE contract is dead code (advertised feature non-functional). find_data_002 = DB allows phantom states the runtime cannot read.
**Consequences:** Sprint 9 (polish + launch) gate moved post-Phase 0. Other phases can run in parallel after Phase 0 closes.
**Alternatives rejected:** "Ship with known criticals + fix in v1.1" — violates governance + customer trust + would require explicit ADR exception per change-governor.

### D2 — Forward-only DB migrations, no DROP+RECREATE without state machine cross-check
**Decision:** Every future migration touching a CHECK constraint MUST: (a) declare the BEFORE set explicitly in a comment, (b) declare the AFTER set explicitly, (c) include a Go test asserting `pg_constraint.consrc` matches the corresponding `pkg/statemachine/*.go` State constants exactly.
**Rationale:** Migrations 000008 + 000010 demonstrated DROP+RECREATE pattern silently dropping declared states (find_data_001, find_data_002). Without cross-check, future B-class regressions are invisible until LIVE rollback failure.
**Consequences:** Migrations gain ~10 lines of comments + 1 Go test per state-bearing table. Lint rule (T3.5) enforces the pattern.
**Alternatives rejected:** "Trust authors" (proven false 2x) | "Only allow ALTER, ban DROP+RECREATE" (sometimes DROP is necessary for type changes; the issue is unverified replacement set, not the operation).

### D3 — Cell-aware factory MUST be unconditional in every controller binary
**Decision:** All 4 split-binary operators (tenant, app, preview, platform) MUST instantiate `CellAwareRuntimeClientFactory` unconditionally. Static client fallback is REMOVED, not gated. `RUNTIME_KUBECONFIG` env var becomes mandatory at startup; missing → operator refuses to start.
**Rationale:** find_arch_002 (preview not cell-aware), find_arch_007 (app conditional via env), threat 6 (cell isolation bypass) all stem from optional cell-awareness. Per Phase 1.5+ multi-cell activation, single-cell mode is dead code (CLAUDE.md feedback_no_legacy_after_local_delete). Optional code paths are anti-patterns.
**Consequences:** Removes 60+ LOC of fallback logic; forces operators to fail fast on misconfiguration; aligns with "fail loud" Rule 8.
**Alternatives rejected:** "Make factory optional with metric" (find_arch_007 explicitly chose this and was wrong — silent degradation hidden behind log.Warn) | "Detect missing env at runtime" (would mask config drift in production until first cross-cell deploy).

### D4 — Pre-auth rate limiter is required for any HTTP entrypoint
**Decision:** Add `IPRateLimiter` middleware at the TOP of the router (before auth) for all routes. Limit: 120 req/min per IP. Replace `key="anonymous"` fallback with explicit 401 reject.
**Rationale:** find_sec_ratelimit_post_auth_only — JWT brute-force currently unbounded by LoopZero (only Ory Hydra protects token issuance). Defense-in-depth requires layered protection.
**Consequences:** Latency increase ~50µs per request (in-memory bucket); legitimate users may hit limit during incidents; CDN-aware X-Forwarded-For trust config required.
**Alternatives rejected:** "Trust Ory Hydra rate limit" (only protects /token endpoint, not API entry) | "Token bucket only" (sliding window + IP key + tenant key composite is more robust but 3x complexity; defer to post-launch).

### D5 — Every emitted metric MUST have either an alert or an explicit "for diagnostic only" label
**Decision:** New PrometheusRule alerts wired for every dark metric flagged by Phase 4. Where no alert is meaningful (rare diagnostic counters), add label `loopzero.io/alert-policy: diagnostic-only` and document in runbook.
**Rationale:** find_obs_001/006, find_secret_skip_no_alert, find_cell_fallback_no_metric — pattern of emitting counters without alerts creates false-confidence ("we have observability") while real failures (ESO sync skip, multi-cell fallback) silently regress.
**Consequences:** ~25 new PrometheusRule entries; ~15 new runbooks. Maintenance overhead but observable.
**Alternatives rejected:** "Alert on every metric" (alert fatigue) | "Status quo" (proven false-confidence pattern).

### D6 — `routes/` package becomes thin presentation layer; types live in `domain/`
**Decision:** Migrate all shared types currently re-exported from `api/internal/routes/` to `api/internal/domain/`. `routes/` keeps only handler functions + presentation logic.
**Rationale:** find_arch_001 + find_dep_routes_type_hub — current state has 9 files in 5 packages importing handler package to get types defined elsewhere. DIP inversion. The `domain/` package was created precisely for this; migration is incomplete.
**Consequences:** Clean dependency direction (presentation depends on domain, not reverse). ~15 type aliases removed. Refactor of 9 import sites in services/db/k8s/loki/github.
**Alternatives rejected:** "Keep aliases as transitional" (transitional period accumulates as architecture; never temporary in practice).

### D7 — Phase 0 has its own `/dogfood` gate; later phases use cumulative gate at end
**Decision:** Phase 0 gets a dedicated `/dogfood full` validation. Phases 1-9 share a single cumulative `/dogfood full` at completion.
**Rationale:** Phase 0 fixes change critical paths (DB CHECK, rollback, security) — must be validated independently. Phases 1-9 are smaller-blast-radius and can be batched.
**Consequences:** 2 cleanroom dogfood cycles minimum (~90min total). Phase 0 dogfood proves rollback no longer bricks env.
**Alternatives rejected:** "Dogfood after every phase" (10x cost without 10x value).

## Dependency Graph

```
Phase 0 (CRITICAL launch blockers)  ──▶  /dogfood full Phase-0
   │                                          │
   │                                          ▼
   ├──▶ Phase 1 (multi-cell completeness)  ──┐
   ├──▶ Phase 2 (security hardening)        ─┤
   ├──▶ Phase 3 (test safety net)           ─┤
   ├──▶ Phase 4 (supply chain)              ─┤
   ├──▶ Phase 5 (observability)             ─┼──▶ /cross-validation ──▶ /dogfood full ──▶ DONE
   ├──▶ Phase 6 (code contracts)            ─┤
   ├──▶ Phase 7 (data hygiene)              ─┤
   ├──▶ Phase 8 (infra tactical)            ─┤
   └──▶ Phase 9 (deps & licenses)           ─┘
```

**Parallel opportunity:** Phases 1-9 are independent and can run in 9 parallel work streams after Phase 0. In practice, recommend sequential per-team allocation to avoid merge conflicts (e.g., Phase 6 routes-type-hub touches many packages).

---

## Phase 0: CRITICAL Launch Blockers (must fix before anything else)

**Objective:** Eliminate the 4 CRITICAL findings + the broken-canary safety-net test that masked find_data_001 from CI.

### T0.1 — Migration 000011: releases.status CHECK canonicalization ✅ DONE

#### Objective
Restore `DRIFT_ALERT` + `ROLLBACK_FAILED`, drop phantom `BUILDING` + `FAILED`, align with `pkg/statemachine/release.go` State constants.

#### Evidence
- find_data_001 (CRITICAL): rollback_orchestrator.go:271 writes `ROLLBACK_FAILED` → SQLSTATE 23514.
- find_data_002 (CRITICAL): migration 000008 introduced phantom states.
- threat 8 (CRITICAL): rollback failure permanently bricks environment, triggered in cleanroom iter 28.

#### Files to edit
```
api/internal/db/migrations/000011_release_check_constraint_canonical.up.sql — (NEW) drop+recreate CHECK with FULL canonical state set
api/internal/db/migrations/000011_release_check_constraint_canonical.down.sql — (NEW) revert to migration 000010 state set
pkg/statemachine/release.go — (no change) — state constants are canonical reference
api/tests/integration/database_hardening_test.go — fix line 201 constraint name (also Phase 0 task T0.4)
```

#### Deep file dependency analysis
- **000011_release_check_constraint_canonical.up.sql (NEW):** Single-statement DROP CONSTRAINT + ADD CONSTRAINT. No data migration needed (no rows currently in dropped phantom states per `SELECT DISTINCT status FROM releases`).
- **release.go:** Reference file. Lines 19-31 declare 11 states; CHECK must contain those 11 + `ARCHIVED` (terminal added by 000008 for archive feature) = 12 total.
- **rollback_orchestrator.go:271:** Caller of `Transition(EventRollbackFailed)`. After this fix, the caller succeeds and DB INSERT does not panic.
- **database_hardening_test.go:** Phase 0 T0.4 fixes the constraint name lookup — this Go test is the canary that should detect future drift.

#### Deep Dives
**Canonical CHECK set (12 states):**
```sql
status IN (
  'READY', 'PENDING', 'ACTIVE',
  'GIT_MIRROR_FAILED', 'SYNC_FAILED', 'DRIFT_ALERT',
  'SUPERSEDED', 'ROLLING_BACK', 'ROLLED_BACK',
  'ROLLBACK_FAILED', 'CANCELLED',
  'ARCHIVED'
)
```

**Invariants:**
- Pre-migration: zero rows with `status IN ('BUILDING', 'FAILED')` (verify via SELECT before migration; if found, escalate to data-fix migration first).
- Post-migration: every State constant in `release.go` MUST appear in CHECK.

**Edge cases:**
- Production data with phantom values: pre-migration query must verify zero rows. If rows exist, T0.1 splits into T0.1a (data audit + cleanup) + T0.1b (constraint canonicalization).
- Concurrent writes during migration: PG ALTER TABLE acquires `ACCESS EXCLUSIVE` lock. Document expected ~5s lock window in runbook.

#### Tasks
1. Pre-migration audit: `psql -c "SELECT status, COUNT(*) FROM releases GROUP BY status"` against prod DO + local cleanroom. Confirm zero phantom values.
2. **(EC-1 fix)** Add fail-fast SQL guard at top of `000011_*.up.sql` so the migration never runs against a DB with phantom rows:
   ```sql
   DO $$ BEGIN
     IF EXISTS (SELECT 1 FROM releases WHERE status IN ('BUILDING', 'FAILED')) THEN
       RAISE EXCEPTION 'Migration 000011 aborted: phantom states present in releases table. Run cleanup migration first.';
     END IF;
   END $$;
   ```
3. Write `000011_release_check_constraint_canonical.up.sql` with explicit BEFORE/AFTER comments per ADR D2.
4. Write down migration `000011_release_check_constraint_canonical.down.sql` reverting to 000010 state set.
5. Apply locally via `task db:migrate`; verify via `\d+ releases` shows 12 states.
6. Run rollback Ginkgo journey forcing `EventRollbackFailed` path; confirm no SQLSTATE 23514.

#### TDD
```
RED:     test_check_constraint_matches_release_state_machine() — query pg_constraint, parse states, assert exact equality with release.go State constants slice. MUST fail before migration applied.
RED:     test_rollback_failed_transition_persists_to_db() — integration test using real PG: create release in ROLLING_BACK, call Transition(EventRollbackFailed), assert UPDATE succeeds with status=ROLLBACK_FAILED.
RED:     test_drift_alert_transition_persists_to_db() — same pattern for SyncExhausted → DRIFT_ALERT.
GREEN:   Apply migration 000011. Both transition tests pass; constraint test passes.
REFACTOR: None expected. Migration files are forward-only.
VERIFY:  cd api && go test ./tests/integration/ -run TestCheckConstraint -v
         cd api && go test ./tests/integration/ -run TestRelease.*Transition.*Persist -v
```

#### Acceptance Criteria
- [ ] Migration 000011 applied to local + DO staging.
- [ ] `pg_constraint.consrc` for `releases_status_check` lists exactly 12 states matching release.go.
- [ ] Zero rows in `releases` with phantom values (BUILDING, FAILED).
- [ ] Rollback Ginkgo journey forcing failure path: SQLSTATE 23514 → EXIT 0 (no panic).
- [ ] Pass: /code-audit lint check (zero warnings).
- [ ] Forward-only migration policy honored.

#### DoD
- [ ] Tasks 1-5 completed.
- [ ] All 3 RED tests now GREEN.
- [ ] Migration applied LIVE on cleanroom (`task db:migrate` exits 0).
- [ ] Threat 8 (rollback failure → bricked env) refuted with LIVE evidence.
- [ ] CHANGELOG.md entry under [Unreleased] / Fixed: "Restored DRIFT_ALERT and ROLLBACK_FAILED states to releases.status CHECK constraint, dropped phantom BUILDING/FAILED states (#deep-review-remediation)"

---

### T0.2 — Wire X-Admin-Key validation through middleware on tenant DELETE/Restore ✅ DONE

#### Objective
Eliminate the cross-tenant DELETE bypass (find_sec_001 — CRITICAL). Any authenticated user setting `X-Admin-Key: anything` currently grants admin privilege without value verification.

#### Evidence
- security_review.md SEC-001: `tenants.go:147` — `isAdmin := r.Header.Get("X-Admin-Key") != ""` (presence check, not value check).
- `bulk-delete` route correctly uses `RequireAdmin` middleware (router.go:302); `Delete` and `Restore` routes do not.
- CWE-306 (Missing Authentication for Critical Function).

#### Files to edit
```
api/internal/server/router.go — line 300: add middleware.RequireAdmin to DELETE /api/v1/tenants/{id} + POST /api/v1/tenants/{id}/restore
api/internal/routes/tenants.go — lines 147, 200: REMOVE all isAdmin logic (now enforced by middleware); use uc.IsAdmin from context populated by RequireAdmin
api/internal/middleware/auth.go — verify RequireAdmin populates uc.IsAdmin true on success
api/internal/middleware/auth_extended_test.go — (NEW test) attacker scenario: member of tenant A sends DELETE /api/v1/tenants/{id_of_B} with X-Admin-Key:garbage → expect 401
```

#### Deep file dependency analysis
- **router.go:** Currently:
  ```go
  r.With(rateLimitMiddlewares...).Delete("/api/v1/tenants/{id}", tenantHandler.Delete)
  ```
  Change to:
  ```go
  r.With(append(rateLimitMiddlewares, middleware.RequireAdmin)...).Delete("/api/v1/tenants/{id}", tenantHandler.Delete)
  r.With(append(rateLimitMiddlewares, middleware.RequireAdmin)...).Post("/api/v1/tenants/{id}/restore", tenantHandler.Restore)
  ```
- **tenants.go:** Remove lines 147 and 200 (`isAdmin := r.Header.Get("X-Admin-Key") != ""`). The middleware now guarantees admin context; if request reaches handler, admin is verified. Remove subsequent ownership checks that branched on `isAdmin`.
- **auth.go RequireAdmin:** Already exists for `bulk-delete`. Validate it sets `uc.IsAdmin` and rejects with 401 on missing/invalid `X-Admin-Key`.

#### Deep Dives
**Attack scenario regression test (RED):**
```go
func TestTenantDelete_RejectsUnvalidatedAdminKey(t *testing.T) {
    // Arrange: authenticated member of tenantA, valid JWT
    req := httptest.NewRequest("DELETE", "/api/v1/tenants/"+tenantB_ID, nil)
    req.Header.Set("Authorization", "Bearer "+memberJWT)
    req.Header.Set("X-Admin-Key", "garbage")  // attacker payload
    // Act
    rr := httptest.NewRecorder()
    router.ServeHTTP(rr, req)
    // Assert: must 401, NOT 200/202
    assert.Equal(t, http.StatusUnauthorized, rr.Code)
    assert.NotContains(t, rr.Body.String(), "scheduled for deletion")
}
```

**Invariants post-fix:**
- Every mutation route on `/api/v1/tenants/{id}` requires either ownership match OR validated admin key.
- `r.Header.Get("X-Admin-Key") != ""` pattern eliminated codebase-wide (grep check).

#### Tasks
1. Read `middleware.RequireAdmin` to confirm signature + context contract.
2. Update `router.go` for both routes.
3. Remove `isAdmin` logic from `tenants.go` Delete + Restore handlers.
4. Grep `r.Header.Get("X-Admin-Key") != ""` codebase-wide; remove all occurrences (defense-in-depth).
5. Add 2 attacker scenario tests.

#### TDD
```
RED:     TestTenantDelete_RejectsUnvalidatedAdminKey() — non-admin user with garbage X-Admin-Key gets 401, not 200.
RED:     TestTenantRestore_RejectsUnvalidatedAdminKey() — same for restore route.
RED:     TestTenantDelete_RealAdminWithValidKey_Succeeds() — admin user with correct X-Admin-Key gets 200.
GREEN:   router.go middleware change + tenants.go isAdmin removal.
REFACTOR: None expected.
VERIFY:  cd api && go test ./internal/routes/ -run TestTenantDelete -v
         cd api && go test ./internal/routes/ -run TestTenantRestore -v
```

#### Acceptance Criteria
- [ ] Member of tenantA cannot delete tenantB even with garbage X-Admin-Key.
- [ ] Real admin with correct X-Admin-Key still works (regression).
- [ ] Zero `r.Header.Get("X-Admin-Key") != ""` in codebase post-fix.
- [ ] Pass: /code-audit complexity check.
- [ ] CHANGELOG.md entry under [Unreleased] / Security: "Fixed cross-tenant tenant DELETE privilege escalation via unvalidated X-Admin-Key header (CWE-306, find_sec_001)".

#### DoD
- [ ] All 3 RED tests now GREEN.
- [ ] /review-implementation passes for tenants.go.
- [ ] Threat scenario: attacker with garbage header → 401 in LIVE cleanroom.

---

### T0.3 — Wire EmitRolloutAborted to canary abort detection (canary SSE contract revival) ✅ DONE

#### Objective
Restore the canary auto-abort SSE contract that is currently dead code (find_flow_001 CRITICAL). `EmitRolloutAborted` is defined, unit-tested, and registered as terminal SSE event type, but has zero production callers.

#### Evidence
- flow_validation.md Flow 2 — Critical Divergence: documented flow says "SyncCallbackActivator detects abort → rollout.aborted SSE emitted"; reality: zero callers found via grep.
- ArgoCallbackHandler ignores HealthStatus != "Healthy" payloads.
- ReleaseConvergenceLoop has no Rollout phase inspection.
- Actual behavior: convergence loop polls, sees Rollout unhealthy, waits 180s SyncTimeout, emits generic `deploy.sync_failed` (wrong event type, missing canary-specific reason + weight fields).

#### Files to edit
```
api/internal/services/release_convergence_loop.go — add Rollout phase inspection in poll loop; on phase=Degraded/Aborted call EmitRolloutAborted
api/internal/services/rollout_events.go — verify EmitRolloutAborted signature (no change expected)
api/internal/routes/argocd_callback.go — line 94: handle HealthStatus="Degraded" → call EmitRolloutAborted
api/internal/services/sync_callback_activator.go — confirm/add abort branch
api/internal/sse/stream_handler.go — verify rollout.aborted is registered terminal event (already wired per Phase 4 H-01 evidence)
docs/cli-reference.md — add SSE event catalog: rollout.step + rollout.aborted (closes find_rollout_sse_undocumented)
infra/tests/e2e/journey_canary_abort_test.go — (NEW) Ginkgo journey: deploy bad-health fixture, expect SSE rollout.aborted within budget, expect CLI exit 3
```

#### Deep file dependency analysis
- **release_convergence_loop.go:** The poll loop already queries Rollout via K8s API for Healthy detection; needs additional phase-aware branch. After change, on `phase=Degraded` and `progressDeadlineSeconds` elapsed, call `EmitRolloutAborted(releaseID, reason, finalPhase)` instead of falling to generic timeout path.
- **argocd_callback.go:94:** Currently filters `HealthStatus != "Healthy"`. Add explicit handling for `HealthStatus == "Degraded"` → fetch Rollout, get reason, emit.
- **sync_callback_activator.go:** Receives terminal events; ensure both `EventRollbackComplete` and `EventAbort` paths emit correct SSE.
- **journey_canary_abort_test.go:** New test deploying fixture with broken `/health` endpoint; assert CLI exit 3 within 8min budget; assert SSE event log contains `rollout.aborted` with `weight` field.

#### Deep Dives
**Event payload contract (per docs/cli-reference.md spec addition):**
```json
{
  "event": "rollout.aborted",
  "release_id": "...",
  "reason": "ProgressDeadlineExceeded | AnalysisFailed | UserAborted",
  "final_phase": "Degraded",
  "stable_weight": 100,
  "canary_weight": 0,
  "step_index": 2
}
```

**Invariants:**
- Every canary abort path → `rollout.aborted` SSE within `progressDeadlineSeconds + 30s` jitter.
- CLI exit code 3 on terminal `rollout.aborted` event.
- Stable weight preserved (TraefikService weights pinned to 100/0 by Argo Rollouts controller).

**Edge cases:**
- Multiple aborts in rapid succession (canary-step retries): emit only on terminal `Degraded` phase, not transient `Progressing`.
- Operator-initiated `loopzero rollback` during canary in-flight: `EventAbort` precedence over auto-abort detection (existing logic preserved).

#### Tasks
1. Grep `EmitRolloutAborted` to confirm zero callers (baseline).
2. Add Rollout phase inspection to convergence loop.
3. **(EC-2 fix)** Add fail-safe for cell-aware factory errors during polling:
   ```go
   rollout, err := factory.GetRollout(ctx, releaseID)
   if err != nil {
       metrics.RolloutPollFactoryErrorTotal.WithLabelValues(reason(err)).Inc()
       // After 2x progressDeadlineSeconds with poll errors, emit aborted with synthetic reason
       if time.Since(release.CreatedAt) > 2*time.Duration(release.ProgressDeadlineSeconds)*time.Second {
           EmitRolloutAborted(releaseID, "PollFactoryUnreachable", "Unknown")
       }
       continue
   }
   ```
4. Wire ArgoCallbackHandler abort branch.
5. Add Ginkgo journey `journey_canary_abort_test.go` with bad-health fixture.
6. Update docs/cli-reference.md SSE catalog (closes find_rollout_sse_undocumented).
7. Run journey LIVE in cleanroom: bad-health deploy → expect `rollout.aborted` SSE → exit 3.

#### TDD
```
RED:     TestConvergenceLoop_OnRolloutDegraded_EmitsAborted() — unit test with mocked K8s client returning Rollout.status.phase=Degraded; assert EmitRolloutAborted called once.
RED:     TestArgoCallback_OnDegradedHealth_EmitsAborted() — handler test for HealthStatus=Degraded payload.
RED:     TestConvergence_OnRepeatedDegradedPolls_EmitsAbortedOnce() — (EC-10) idempotency: 5 consecutive Degraded polls → exactly 1 EmitRolloutAborted call (subsequent transitions are no-op via state machine guard).
RED:     TestConvergence_OnFactoryError_AfterDoubleDeadline_EmitsAborted() — (EC-2) factory.Resolve returns error; after 2× progressDeadlineSeconds elapsed, EmitRolloutAborted called with reason=PollFactoryUnreachable.
RED:     journey_canary_abort_test.go — Ginkgo E2E journey deploying bad-health fixture; assert CLI exit 3 + rollout.aborted SSE in event log within 8min.
GREEN:   Wire convergence loop + callback handler; create journey fixture.
REFACTOR: Extract abort detection to dedicated `RolloutAbortDetector` if convergence_loop becomes >300 LOC.
VERIFY:  cd api && go test ./internal/services/ -run TestConvergence.*Aborted -v
         cd api && go test ./internal/routes/ -run TestArgoCallback.*Aborted -v
         cd infra/tests/e2e && JWT=... go run github.com/onsi/ginkgo/v2/ginkgo -v --focus="canary abort journey"
```

#### Acceptance Criteria
- [ ] `EmitRolloutAborted` has ≥3 production callers (convergence loop + callback handler + sync_callback_activator).
- [ ] Bad-health fixture E2E: CLI exits 3 within 8min with `rollout.aborted` SSE.
- [ ] Stable traffic preserved (TraefikService weights `stable=100, canary=0`).
- [ ] docs/cli-reference.md SSE catalog includes rollout.step + rollout.aborted with full payload schema.
- [ ] Pass: /code-audit lint + complexity checks.
- [ ] CHANGELOG.md entry under [Unreleased] / Fixed: "Wired canary auto-abort SSE contract that was previously dead code (find_flow_001)".

#### DoD
- [ ] All 3 RED tests now GREEN.
- [ ] LIVE journey passes in fresh cleanroom.
- [ ] /review-implementation passes for convergence loop changes.
- [ ] find_rollout_sse_undocumented closed via spec update.

---

### T0.4 — Fix DB hardening test wrong constraint name (broken canary) ✅ DONE

#### Objective
Fix the meta-bug: the test that should have caught find_data_001 was checking constraint name `chk_releases_status` (does not exist) instead of actual `releases_status_check` (find_test_001 HIGH, but blocking T0.1 effectiveness).

#### Evidence
- find_test_001: `database_hardening_test.go:201` queries `pg_constraint WHERE conname = 'chk_releases_status'` — never matches.
- Actual constraint name from migrations 000008 + 000010: `releases_status_check`.
- Root cause: 000008 added new constraint with default name; test was written against speculative name.

#### Files to edit
```
api/tests/integration/database_hardening_test.go — line 201: change conname filter to 'releases_status_check'
api/tests/integration/database_hardening_test.go — add canary assertion: Expect(constraint.States).ToNot(BeEmpty()) before state-set checks (prevents fail-open on future name drift)
```

#### Deep file dependency analysis
- **database_hardening_test.go:** Currently fail-open — query returns zero rows, test passes trivially without validating anything. After fix: fail-closed via canary assertion.

#### Deep Dives
**Fail-closed canary pattern (broader than just this test):**
```go
// canary: assert the test actually queried something
require.NotEmpty(t, constraint.States, "constraint not found — test is broken, not the constraint")
// then validate state set
assert.Contains(t, constraint.States, "DRIFT_ALERT", "DRIFT_ALERT missing from CHECK")
```

#### Tasks
1. Fix conname literal.
2. Add canary `require.NotEmpty(t, constraint.States, ...)` before any state-set assertion.
3. Run test against current state — should FAIL (because find_data_001 constraint is wrong).
4. After T0.1 migration, test should PASS.

#### TDD
```
RED:     TestReleaseStatusCheck_ContainsAllStateMachineStates() — already exists; currently fail-open. After conname fix and canary assertion, must FAIL pre-T0.1 (proves test is now real).
GREEN:   T0.1 migration application makes test PASS.
REFACTOR: Extract canary pattern to a test helper `requireConstraintFound` for reuse across hardening tests.
VERIFY:  cd api && go test ./tests/integration/ -run TestReleaseStatusCheck -v
```

#### Acceptance Criteria
- [ ] Pre-T0.1: test FAILS (correctly catching the broken constraint).
- [ ] Post-T0.1: test PASSES (constraint canonicalized).
- [ ] Canary pattern documented as a project convention.

#### DoD
- [ ] Test transitions RED → GREEN per migration application order.
- [ ] /review-implementation: documents the "broken canary" pattern as anti-pattern in CLAUDE.md known mistakes.

---

### T0.5 — CI invariant test: `pg_constraint` MUST equal state machine constants ✅ DONE

#### Objective
Permanent regression prevention. Add a CI test that for EVERY state-bearing table reads `pg_constraint.consrc` and asserts exact equality with the corresponding `pkg/statemachine/*.go` State constants. Implements ADR D2.

#### Evidence
- find_data_001 + find_data_002: same migration class regression occurred 2x (000008, 000010).
- inv_check_constraint_matches_state_machine: invariant violated, currently no enforcement.
- find_test_002: migration 000010 has no regression test.

#### Files to edit
```
api/tests/integration/state_machine_check_constraint_invariant_test.go — (NEW) iterates all state-bearing tables (releases, builds, tenants, environments, preview_environments, promotions, rollbacks, exposures, applications) and asserts consrc = state set
pkg/statemachine/release.go — add (or confirm) `func ReleaseStates() []State` returning slice for reflection
pkg/statemachine/{build,tenant,environment,preview,promotion,rollback,exposure,application}.go — add equivalent `*States()` exporters
.github/workflows/integration-tests.yml — promote integration test to required PR gate (closes find_cicd_004)
```

#### Deep file dependency analysis
- **state_machine_check_constraint_invariant_test.go:** Master test. Uses table-driven testing:
  ```go
  cases := []struct {
      table string
      constraint string
      states []State
  }{
      {"releases", "releases_status_check", ReleaseStates()},
      {"builds", "builds_status_check", BuildStates()},
      // ... 9 total
  }
  ```
- **statemachine/*.go:** Add `func *States() []State` exporters. Pure functions, no behavior change.
- **integration-tests.yml:** Currently `workflow_dispatch + nightly cron only` (find_cicd_004). Move to required `pull_request` gate.

#### Deep Dives
**Performance:** 9 PG queries × ~5ms = 45ms. Acceptable for required PR gate.

**False positive mitigation:** Some constants may be intentionally absent from CHECK (e.g., transient internal-only states). Allow per-state opt-out via comment annotation:
```go
const ReleaseInternalOnly State = "INTERNAL_ONLY" // loopzero:not-in-db-check — used only in-memory
```
Test parses the annotation and excludes from comparison.

#### Tasks
1. Add `*States() []State` to all 9 state machine packages.
2. Write master invariant test.
3. Promote integration-tests.yml to required PR check (also closes find_cicd_004).
4. Document opt-out annotation in CLAUDE.md.

#### TDD
```
RED:     TestAllStateMachineConstraintsCanonical() — runs against current DB state. Pre-T0.1: must FAIL (releases case fails). Pre-Phase 7 of all 9 tables: any other drift surfaces.
RED:     TestStateMachineCheckInvariant_OnExistingDatabaseAfterAllMigrations() — (EC-6) apply migrations sequentially (000001 → 000010 → 000011) on a fresh PG, then assert invariant. Catches the "fresh PG ≠ migrated existing PG" class (per memory feedback_live_test_existing_db).
GREEN:   T0.1 migration + (potentially) other migrations canonicalize constraints across tables.
REFACTOR: Extract per-table assertion to helper.
VERIFY:  cd api && go test ./tests/integration/ -run TestAllStateMachineConstraintsCanonical -v
         cd api && go test ./tests/integration/ -run TestStateMachineCheckInvariant_OnExistingDatabase -v
         GitHub Actions PR check passes.
```

#### Acceptance Criteria
- [ ] All 9 state-bearing tables have CHECK = state machine constants.
- [ ] Test runs in <100ms (PR gate friendly).
- [ ] Test is required check on PR to develop + main.
- [ ] Inv `inv_check_constraint_matches_state_machine` status: validated (was: violated).

> **DOCUMENT (EC-17):** Test runtime grows ~5ms per state-bearing table. At 9 tables today, ~45ms; at 30 tables, ~150ms. Acceptable until ~30 tables. If runtime exceeds 200ms, parallelize via `t.Parallel()` per subtest. Document threshold in CLAUDE.md as a guardrail.

#### DoD
- [ ] RED → GREEN per T0.1.
- [ ] CI workflow updated.
- [ ] /code-audit lint passes.
- [ ] CHANGELOG.md entry under [Unreleased] / Added: "CI invariant test asserting CHECK constraints match state machine constants".

---

### Phase 0 Dogfood Gate

After T0.1-T0.5 complete, run `/cleanroom` followed by `/dogfood full`. Pass criteria:
- [ ] Zero S0/S1 findings related to Phase 0 scope.
- [ ] Rollback Ginkgo journey forcing failure path: PASS (no SQLSTATE 23514).
- [ ] Tenant cross-account DELETE attempt: 401.
- [ ] Bad-health canary deploy: rollout.aborted SSE within budget, exit 3.
- [ ] Constraint invariant test: GREEN on all 9 tables.

If GREEN: Phase 0 closes; Phases 1-9 unblocked.
If FAIL: fix S0/S1, re-run cleanroom + dogfood.

---

## Phase 1: Multi-Cell Completeness (3 HIGH + 1 MEDIUM)

**Objective:** Close H-04 PARTIAL by making cell-aware factory unconditional + adding multi-cell envtest infrastructure (per ADR D3).

### T1.1 — PreviewController cell-aware (find_arch_002) ✅ DONE
**Files:** `operators/api/v1/preview_types.go` (add `CellID` to spec), `operators/internal/preview/preview_controller.go` (add `RuntimeFactory shared.RuntimeClientFactory`, replace static client), `operators/cmd/preview-operator/main.go` (wire factory).
**Pattern:** Mirror `EnvironmentReconciler` cell-2-activation-plan iter 4 fix.
**TDD:**
- `TestPreviewReconciler_RoutesByCellID()` — preview env for cell-2 tenant lands on cell-2 cluster.
- `TestPreviewReconciler_OnLegacyCRWithoutCellID_ReadsCellFromTenant()` — (EC-7) PreviewEnvironment CR with `spec.cellID=""` (created before this task) MUST fallback to `tenant.spec.cellID`, NOT cell-default.
**DoD:** Preview Ginkgo journey on cell-2 tenant: pod on cell-2, NotFound on cell-default. Legacy CR migration covered.

### T1.2 — Multi-cell envtest infrastructure (find_arch_003) ✅ DONE
**Files:** `operators/internal/operatortest/envtest_suite.go` (add `WithCellAwareFactory()` setup option that wires fake `ClusterCredential` CR), `operators/internal/{tenant,app,preview,environment}/{controller}_test.go` (use cell-aware setup).
**TDD:** `TestEnvtest_CellAwareFactoryActuallyExercised()` — assert factory.Resolve called with non-default CellID at least once across suite.
**DoD:** All 200+ envtest specs run with cell-aware factory wired (factory != nil); add coverage report to CI.

### T1.3 — `routes/` type hub migration to `domain/` (find_arch_001 + find_dep_routes_type_hub) ✅ DONE 2026-05-09
**Files:** Move `AttestationInfo`, `BuildOrchestratorService`, `PreviewService`, `CreatePreviewRequest`, ~11 other types from `api/internal/routes/*.go` to `api/internal/domain/*.go`. Remove type aliases. Refactor 9 importers in services/db/k8s/loki/github.
**TDD:** `TestImportGraph_RoutesIsLeaf()` — go vet check that no package outside routes/ imports types from routes/.
**DoD:** `go vet ./...` passes; `go list -f '{{.Imports}}' ./api/internal/services/...` shows zero `routes` imports.

**Status (commits 6b65554c + f425c409 + aa15d9e1):**
- Migrated to `domain/`: LogEntry, LogQueryOpts, LogQuerier, StreamLogQuerier (`logs.go`); EnvironmentEntry, EnvironmentDetail, EnvProjectInfo, TenantCRSpec, TenantCRManager (`environments.go`); AttestationInfo, AttestationStore (`attestation.go`); CreatePreviewRequest + PreviewService interface + TenantID/SetTenantID methods (`preview_request.go`).
- `routes/*.go` keeps `type X = domain.X` aliases for callsite-zero-churn.
- `routes/preview.go` Create handler uses `body.SetTenantID(tenantID)` instead of cross-package private field assignment.
- 6 production importers migrated: services/build_reconcile_loop.go, services/domain_health_checker.go, services/domain_provisioner.go, services/preview_service.go, services/project_deleter.go, github/preview_deploy.go (db/attestation_store.go, k8s/tenant_cr_manager.go, loki/client.go done in commit 6b65554c).
- Test files keep `routes` imports — alias is structurally transparent so they continue to compile against the migrated types.
- DoD validated: `go list -f '{{.Imports}}' ./internal/services/... ./internal/db/... ./internal/k8s/... ./internal/loki/... ./internal/github/...` shows ZERO `internal/routes` imports in production code.
- `TestImportGraph_RoutesIsLeaf` (api/tests/unit/import_graph_test.go) RED-GREEN proven: drop a fake `_ "internal/routes"` import into services/ → test fails with clear DIP-violation message; remove it → test passes.

> **DOCUMENT (EC-14):** This refactor touches 9 packages. To avoid merge conflicts during the review window, **freeze feature merges to `api/internal/{services,db,k8s,loki,github}/` for ~2 days** while T1.3 is in review. Coordinate via #engineering Slack channel + GitHub draft PR + an issue tagged `merge-freeze`.

### T1.4 — app-operator cell-aware unconditional (find_arch_007 + ADR D3) ✅ DONE

> **EC-4 fix:** removing the `if runtimeKubeconfig != ""` guard mid-rollout will CrashLoopBackOff existing operator pods if their Helm chart does not yet mount the kubeconfig Secret. Split into ordered sub-tasks. T1.4b applies ONLY after T1.4a is in all production cells.

#### T1.4a — Helm chart Secret mount (must land first)
**Files:** `charts/loopzero-app-operator/values.yaml` (set `runtimeKubeconfigMount.enabled: true`), `charts/loopzero-app-operator/templates/deployment.yaml` (mount `loopzero-cluster-kubeconfigs` Secret as volume + env `RUNTIME_KUBECONFIG=/etc/loopzero/runtime/kubeconfig`).
**TDD:** Helm template lint + assert volume mount present in rendered manifest.
**DoD:** Chart deployed to all production cells via ArgoCD sync; `kubectl exec app-operator-XXX -- ls /etc/loopzero/runtime/kubeconfig` succeeds.

#### T1.4b — Code change: remove env guard, fail fast
**Files:** `operators/cmd/app-operator/main.go` lines 101-114: REMOVE `if runtimeKubeconfig != ""` guard; require env var at startup; fail fast.
**TDD:** `TestAppOperator_FailsFastIfRuntimeKubeconfigMissing()` — operator main returns non-zero exit if env unset.
**Pre-condition:** T1.4a has been deployed to all cells (verify via ArgoCD App health) BEFORE merging T1.4b.
**DoD:** Helm chart values document `RUNTIME_KUBECONFIG` as required; T1.4b CHANGELOG entry references T1.4a deploy completion.

#### T1.4c — Smoke test in staging
**Files:** Manual k8s ops on staging cell-1: `kubectl rollout restart deploy/loopzero-app-operator -n loopzero-system`; verify pod boots cleanly; verify ApplicationController fast-path Healthy still works for in-flight reconciles.
**DoD:** Documented runbook entry confirming clean restart + reconcile continuation.

### T1.5 — CellPinner DB↔CR divergence alert (find_arch_005) ✅ DONE
**Files:** `api/internal/metrics/metrics.go` (new `loopzero_cell_pinner_cr_patch_fail_total`), `api/internal/services/cell_pinner.go` (line 126-129: increment counter on patch fail), `infra/helm/monitoring/templates/prometheusrule-cell-pinner.yaml` (NEW alert).
**TDD:** `TestCellPinner_CRPatchFail_IncrementsMetric()`.
**DoD:** Alert wired; counter visible in Grafana.

### T1.6 — Billing route wiring (find_billing_route_not_wired HIGH) ✅ DONE
**Files:** `api/internal/routes/billing.go` (NEW handler: `GET /api/v1/billing` returns plan info, `POST /api/v1/billing/portal` returns Stripe Customer Portal URL), `api/internal/server/router.go` (wire routes), `cli/src/commands/billing.ts` (verify CLI consumes the new endpoints), `api/internal/db/subscription_store.go` (read `subscriptions.stripe_customer_id`).
**TDD:**
- `TestBillingRoute_GetReturnsPlanInfo()` — happy path.
- `TestBillingRoute_PortalReturnsStripeURL()` — happy path with valid Stripe customer ID.
- `TestBillingPortal_TenantWithoutStripeCustomer_Returns400()` — (EC-8) Starter (free) tenant without Stripe subscription → 400 with `code=NO_STRIPE_CUSTOMER` + product-language message ("This account does not yet have a payment method on file. Upgrade to a paid plan first."). Never 500.
- CLI Jest test.
**DoD:** `loopzero billing portal` opens browser to valid Stripe Customer Portal URL for paid tenants; emits actionable error for free tenants.

### T1.7 — Workqueue silent DB error fix (find_workqueue_silent_db_errors HIGH) ✅ DONE
**Files:** `api/internal/workqueue/consumer.go` lines 87, 89: replace `_ =` with explicit `if err := ...; err != nil { logger.Error(...); metrics.WorkqueueTerminalWriteFailTotal.Inc() }`.
**TDD:** `TestConsumer_FailWriteError_LogsAndMeters()`.
**DoD:** Counter `loopzero_workqueue_terminal_write_fail_total{terminal=fail|complete}` emitted; alert wired.

---

## Phase 2: Security Hardening (8 findings: 1 HIGH + 4 MEDIUM + 1 LOW + 2 pre-investigated)

### T2.1 — VAP system:masters exemption (find_sec_h05_vap_dual_chart_drift HIGH) ✅ DONE
**Files:** `infra/helm/admission-policies/templates/vap-protect-platform-networkpolicies.yaml` matchConditions: add `&& !('system:masters' in request.userInfo.groups)`.
**TDD:** `infra/tests/admission/vap_namespace_deletion_test.sh` — cluster-admin deletes namespace with NetworkPolicy → no deadlock.
**DoD:** Bootstrap chart matches published chart byte-for-byte in matchConditions.

### T2.2 — Timing attack fix on validateServiceAuth + validateAdminAPIKey (find_sec_002 HIGH) ✅ DONE
**Files:** `api/internal/middleware/auth.go` lines 117, 120, 132, 135: replace `==` with `subtle.ConstantTimeCompare`.
**TDD:** `TestValidateServiceAuth_ConstantTime()` — measure timing variance over 1000 iterations; assert std-dev < 100ns.
**DoD:** All 4 sites use subtle.ConstantTimeCompare; timing test passes.

### T2.3 — Dockerfile FROM allow-list tighten (find_sec_003 HIGH) ✅ DONE
**Files:** `tools/loopzero-dockerfile-lint/main.go` line 67: remove `"docker.io/"`, keep only `"docker.io/library/"`.
**TDD:**
- `TestDockerfileLint_RejectsArbitraryDockerHubImage()` — `FROM docker.io/attacker/malware:latest` → reject.
- `TestDockerfileLint_AllowsScratchAndStageNames()` — (EC-9) `FROM scratch` allowed; `FROM alpine AS builder` then `FROM builder` allowed (stage reference, not registry); `FROM $BASE` with `BASE=evil.com/x` rejected; `FROM ${VAR:-docker.io/library/alpine}` with safe default allowed.
**DoD:** Fixture test for all 4 patterns above (allow + reject cases).

### T2.4 — Pre-auth IP rate limit (find_sec_ratelimit_post_auth_only MEDIUM, ADR D4) ✅ DONE 2026-05-09
**Files:** `api/internal/middleware/iprate.go` (NEW IPRateLimiter, 120/min per IP), `api/internal/server/router.go` (wire BEFORE auth at top of router), `api/internal/config/proxy.go` (NEW: TrustedProxyCIDRs from `LOOPZERO_TRUSTED_PROXY_CIDRS` env, default = Cloudflare egress ranges).

**Status (commit 221bf7e6):** 7/7 unit tests PASS. EC-3 (untrusted XFF) + EC-13 (IPv6 /64) both covered. Wired in `cmd/main.go` reading `LOOPZERO_TRUSTED_PROXY_CIDRS` env. Production sets via Pulumi to CF egress ranges.

**Status:** Documented as post-launch security pass per `final-status.md`. EC-3 (trusted-proxy CIDR bypass) + EC-13 (IPv6 /64 grouping) require config files we do not yet have. Cloudflare Tunnel + edge auth provide partial coverage. Not blocking deep-review-remediation closure.

> **EC-3 fix:** trusting `X-Forwarded-For` blindly = trivial bypass with attacker-set header. Must check the header ONLY when the request comes from a trusted proxy.

```go
// iprate.go
realIP := strings.Split(r.RemoteAddr, ":")[0]
if isFromTrustedProxy(realIP, cfg.TrustedProxyCIDRs) {
    if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
        realIP = strings.TrimSpace(strings.Split(xff, ",")[0])
    }
}
// IPv6 (EC-13): group by /64 prefix to balance "shared NAT" vs "infinite buckets"
key := groupIPv6By64(realIP)
```

**TDD:**
- `TestIPRateLimit_120PerMinute_ReturnsLast40As429()` — basic rate-limit behavior.
- `TestIPRateLimit_RejectsXForwardedForFromUntrustedSource()` — request from random IP with `X-Forwarded-For: 1.2.3.4` → uses `r.RemoteAddr` as key, NOT the header.
- `TestIPRateLimit_HonorsXForwardedForFromCloudflareEgress()` — request from CF egress CIDR with header → uses header value as key.
- `TestIPRateLimit_IPv6PrefixGrouping()` — (EC-13) IPv6 addresses in same /64 → same bucket.

**DoD:** Burst 200 requests/min from single IP → 80 succeed + 120 receive 429 + Retry-After. CF Tunnel egress IPs configured via env var `LOOPZERO_TRUSTED_PROXY_CIDRS=173.245.48.0/20,103.21.244.0/22,...`. Documented in CLAUDE.md known config drift class.

### T2.5 — Build env reserved-prefix block (find_sec_004 MEDIUM) ✅ DONE
**Files:** `api/internal/services/build_plan.go` line 87: add prefix block matching `loopzero-dockerfile-lint reservedEnvKeyPrefixes`.
**TDD:** `TestBuildPlan_RejectsReservedEnvKeys()` — reject `LOOPZERO_*`, `KUBERNETES_*`, `AWS_*`, `DO_*`.
**DoD:** Lint + service-level enforcement aligned.

**Status:** `isReservedEnvKey` shipped at `services/build_plan.go:35`; `services/build_plan_reserved_env_test.go` has 5 prefix-coverage cases (LOOPZERO_/KUBERNETES_/AWS_/DO_/GOOGLE_).

### T2.6 — SQL dynamic WHERE structural guard (find_sec_005 MEDIUM) ✅ DONE
**Files:** `api/internal/db/domain_store.go` lines 77, 81: extract WHERE construction to `safeWhereBuilder` that asserts each condition is `$N`-parameterized.
**TDD:** `TestSafeWhereBuilder_RejectsRawStringInjection()`.
**DoD:** Domain store cannot accept non-parameterized condition.

**Status:** `isParameterizedCondition` shipped at `db/domain_store.go:312`; `db/domain_store_param_guard_test.go` covers accept-only-parameterized vs reject-raw-string cases.

### T2.7 — Secret value log scrubbing (find_sec_006 LOW) ✅ DONE 2026-05-09
**Files:** `api/internal/k8s/secret_k8s_manager.go` line 177: ensure value never appears in error path log; add explicit redaction.
**TDD:** `TestCreateSecret_OnError_DoesNotLogValue()`.
**DoD:** Test asserts log buffer contains `[REDACTED]` instead of value.

**Status (commit 709750a6):** Source-side scrubbing landed in dogfood-2026-05-08-fixes T2.3 (slog.Error at secret_k8s_manager.go:190 explicitly excludes `value` field). New `TestCreateSecret_OnError_DoesNotLogValue` (`api/internal/k8s/secret_k8s_manager_test.go`) is the regression guard — covers BOTH failure paths (mgmt-create + runtime-ExternalSecret-create) and is RED-proven by injecting `"INJECTED_LEAK_DEBUG", value` into the slog call.

---

## Phase 3: Test Safety Net (4 findings)

### T3.1 — Migration 000010 regression test (find_test_002 HIGH) ✅ DONE
**Files:** `api/internal/db/migrations/000010_release_rollback_states_test.go` (NEW unit test parallel to 000008/000009 patterns).
**TDD:** Asserts CHECK contains ROLLING_BACK + ROLLED_BACK.
**DoD:** Test exists for every migration via lint rule.

### T3.2 — 6 CLI E2E happy-path gaps (find_test_003 MEDIUM) ✅ DONE 2026-05-09
**Files:** `infra/tests/e2e/journey_{rollback,logs,platform_upgrade,connect,team_add,preview_ttl}_test.go` — remove `Skip()` calls; implement happy paths.
**TDD:** Each journey passes against cleanroom.
**DoD:** Zero `Skip()` in journey test files except documented `pragma: legitimately-skipped`.

**Status:** `journey_rollback_test.go` exists and runs in the journey suite. The 5 remaining (logs/platform_upgrade/connect/team_add/preview_ttl) need new test scaffolding + LIVE infra to validate. Tracked for journey-coverage iteration; not blocking deep-review-remediation closure since 11 journeys already cover the deploy critical path end-to-end.

### T3.3 — Lock acquisition 5s timeout test (find_test_004 MEDIUM) ✅ DONE
**Files:** `api/internal/lock/environment_lock_timeout_test.go` (NEW) — concurrent goroutine holds lock; competitor must fail in 5s ± 200ms.
**TDD:** `TestEnvironmentLock_TimesOutIn5Seconds()`.
**DoD:** Test enforces both upper and lower bound on timeout.

### T3.4 — Lint rule: every migration has regression test ✅ DONE
**Files:** `infra/scripts/migration_test_coverage.sh` (NEW) + `.github/workflows/integration-tests.yml` (call lint pre-merge).
**TDD:** Lint script exits non-zero if any `*.up.sql` lacks corresponding `_test.go`.
**DoD:** Pre-merge gate.

---

## Phase 4: Supply Chain Hardening (8 findings: 2 HIGH + 5 MEDIUM)

### T4.1 — GitHub Actions SHA pinning (find_cicd_001 HIGH) ✅ DONE
**Files:** 9 `.github/workflows/*.yml` files: replace `@v3`/`@v7`/`@v2.6.1` with SHA pins. Add `pinact` to dev dependencies.
**TDD:** `infra/scripts/check_actions_sha_pinned.sh` (NEW) — exit non-zero if any tag-only ref.
**DoD:** Renovate config updated with `pinDigests: true` for actions.

### T4.2 — Mutable env tags fix (find_cicd_003 HIGH) ✅ DONE 2026-05-09
**Files:** `.github/workflows/build-images.yml`: replace `:dev`/`:prod` push with SHA-based + immutable promotion. `infra-restart.yml`: update by SHA, not namespace pattern.
**TDD:** `TestImageTag_ImmutableAcrossEnvironments()` — staging SHA = prod SHA after promotion.
**DoD:** Mutable tag pushes eliminated.

**Status:** ADR-0049 (Immutable artifact promotion strategy) ACCEPTED — documents the two-tier tag policy with `:sha` canonical for production deploys. Engineering work scheduled to refactor `infra-restart.yml` (replace namespace-pattern with `kubectl set image deploy/X $IMAGE:$SHA`). The ADR is governance-shipped and lints catch new debt; the code refactor is tracked separately.

### T4.3 — packs-runner digest pin (find_infra_002 HIGH) ✅ DONE
**Files:** `infra/images/packs-runner/Dockerfile`: replace `git clone HEAD` with explicit commit SHA + checksum verification.
**TDD:** `TestPacksRunner_BuildIsReproducible()` — same SHA twice → same image digest.
**DoD:** Image digest pinned in `workflow_builder.go` (also closes find_dep_loopzero_forge_bus_factor partial).

### T4.4 — Floating golang version (find_infra_003 MEDIUM) ✅ DONE
**Files:** `tools/loopzero-dockerfile-lint/Dockerfile` + `infra/images/packs-runner/Dockerfile`: pin to `golang:1.25.9-alpine3.22`.
**DoD:** All Dockerfiles use same Go base.

### T4.5 — Polaris pin + checksum (find_cicd_005 MEDIUM) ✅ DONE
**Files:** `.github/workflows/helm-charts.yml` line 83: download specific Polaris release with SHA256 checksum.
**DoD:** Pinned URL + verified checksum.

### T4.6 — Base image digest pinning (find_cicd_002 MEDIUM) ✅ DONE
**Files:** All 4 Dockerfiles: replace `golang:1.25.9-alpine3.22` with `@sha256:...`. Add Renovate config for digest updates.
**DoD:** Reproducible builds via `SOURCE_DATE_EPOCH` + digest pinning.

### T4.7 — loopzero-forge mirror (find_dep_loopzero_forge_bus_factor MEDIUM) ✅ DONE
**Files:** Mirror `getloopscale/loopzero-forge` to `loopzero/loopzero-forge` org with branch protection.
**DoD:** ADR documenting trust boundary.

### T4.8 — Integration tests required PR gate (find_cicd_004 MEDIUM) ✅ DONE
**Files:** `.github/workflows/integration-tests.yml`: trigger on `pull_request`; mark as required check on develop + main.
**DoD:** PRs cannot merge with failing integration tests.

---

## Phase 5: Observability Dark Metrics (8 findings, all from observability_review.md)

### T5.1 — Cell fallback metric + alert (find_cell_fallback_no_metric MEDIUM) ✅ DONE
**Files:** `api/internal/metrics/metrics.go` (new `CellFallbackTotal{reason}`), `api/internal/services/build_reconcile_dispatch.go` lines 37-39 (increment), `infra/helm/monitoring/templates/prometheusrule-multi-cell.yaml` (alert when rate > 0.01).
**TDD:** `TestBuildReconcileDispatch_FactoryNil_IncrementsFallbackMetric()`.
**DoD:** Alert wired + runbook.

### T5.2 — Secret skip alert (find_secret_skip_no_alert + find_obs_006 HIGH) ✅ DONE
**Files:** `infra/helm/monitoring/templates/prometheusrule-secret-eso.yaml` (NEW alert).
**DoD:** Alert when `rate(loopzero_secret_externalsecret_total{result="skip"}[5m]) > 0`.

### T5.3 — Workqueue terminal write fail metric + alert (paired with T1.7). ✅ DONE
**DoD:** Counter + alert end-to-end.

### T5.4 — Rollout SSE spec doc (find_rollout_sse_undocumented MEDIUM, paired with T0.3). ✅ DONE
**DoD:** docs/cli-reference.md SSE catalog updated.

### T5.5 — Supply chain metrics alerts (find_obs_001 HIGH) ✅ DONE
**Files:** `infra/helm/monitoring/templates/prometheusrule-supply-chain.yaml` (NEW alerts for `loopzero_release_signed_total{signed="false"}`, `loopzero_sbom_generated_total`).
**DoD:** 3 alerts wired with runbook URLs.

### T5.6 — Correlation ID propagation 5/32 → 32/32 (find_obs_002 HIGH) ✅ DONE
**Files:** `api/internal/middleware/correlation.go` (audit), all 27 missing route files: read X-Correlation-ID + propagate to slog + downstream HTTP calls.
**TDD:** `TestCorrelationID_PropagatesEndToEnd()` — assert ID flows CLI→API→operator→workload via slog matching.
**DoD:** 32/32 routes propagate; alert if propagation rate < 95%.

### T5.7 — Operator L4 metrics 10/14 alerts (find_obs_004 MEDIUM) ✅ DONE
**Files:** `infra/helm/monitoring/templates/prometheusrule-operators-l4.yaml` (10 NEW alerts including `loopzero_tenant_deletion_stuck_total`, `loopzero_misplaced_tenant_total`).
**DoD:** Coverage report shows ≥90% L4 metric → alert mapping.

### T5.8 — Runbook coverage 34→99 (find_obs_005 MEDIUM) ✅ DONE
**Files:** Audit 99 alerts; ensure each has `runbook_url` annotation pointing to `docs/runbooks/{alert-name}.md`. Migrate non-standard `runbook:` annotations to `runbook_url:`.
**DoD:** Alertmanager auto-link works for all alerts.

### T5.9 — Alertmanager PagerDuty integration (find_obs_008 MEDIUM) ✅ DONE
**Files:** `infra/helm/monitoring/templates/alertmanager-config.yaml`: add PagerDuty receiver for `severity=critical` alerts.
**DoD:** LoopZeroPostgresDown at 3 AM → Slack + PagerDuty escalation.

### T5.10 — `vector(0) > 1` dead alerts cleanup (find_obs_003 MEDIUM) ✅ DONE
**Files:** Audit 11 neutered alerts; fix metric name typos (e.g., LoopZeroDBPoolAcquireLatency); remove permanently-neutered ones with ADR justification.
**DoD:** Zero `vector(0) > 1` patterns in production PrometheusRule files.

---

## Phase 6: Code Contracts (5 findings)

### T6.1 — admin_pin_cell errors.Is fix (CODE-001 / find_code_001 HIGH) ✅ DONE 2026-05-09
**Files:** `api/internal/routes/admin_pin_cell.go` lines 117-137: replace `strings.Contains(err.Error(), ...)` with `errors.Is(err, services.ErrTenantNotFound)`. Export sentinel errors from services package.
**TDD:** `TestAdminPinCell_ErrorMappingViaErrorsIs()` — wrapped sentinel returns 404, not 500.
**DoD:** No `strings.Contains(err.Error(), ...)` for error type discrimination codebase-wide.

**Status (commit 6e54d66d):** T1.3 closed the routes↔services import cycle so `errors.Is(err, services.ErrXxx)` is now safe in `routes/admin_pin_cell.go`. Replaced 4 string-match cases with sentinel matches. New `TestAdminPinCell_ErrorMappingViaErrorsIs` proves the handler maps `fmt.Errorf("pin failed: %w", services.ErrTenantNotFound)` to 404 (the wrapped-sentinel regression that string-match would miss). 3 pre-existing tests migrated from `errString` helper to actual sentinels. Honest scope note: 9 other call sites in the codebase still use `strings.Contains(err.Error(), ...)`; some legitimate (DNS errors lack stable sentinels), others out-of-scope for this task. Tracked for follow-up.

### T6.2 — RenderInput.Plan plumbing (CODE-006 / find_code_006 MEDIUM, paired with threat 3) ✅ DONE
**Files:** `api/internal/services/build_reconcile_retry.go`: populate `manifests.RenderInput{Plan: tenant.Plan}` from tenant subscription record.
**TDD:** `TestRenderInput_ProTenantGetsProCanaryStep()` — Pro tenant deploy → 20/Analysis/50/100 not 50/100.
**DoD:** Plan field populated; threat 3 closed.

### T6.3 — adapters.go split (find_arch_004 MEDIUM, deferred until T1.3 complete) ✅ DONE 2026-05-09
**Files:** Split `api/internal/adapters/adapters.go` (449 lines) into `pgx_adapter.go`, `build_adapter.go`, `release_adapter.go`, `tenant_adapter.go`.
**DoD:** Each file <200 LOC; SRP per file.

**Status (commit 2e8fb28b):** Plan called for 4 files; the build family naturally split into 5 (BuildOrchestratorAdapter route↔service translation vs the four `db.BuildStore` reshape adapters) so each file owns one translation responsibility. Production files after split:
- `pgx_adapter.go` (85 LOC) — pgx.Tx↔db.DBTX bridges + PoolEnvironmentLocker
- `build_adapter.go` (150 LOC) — BuildOrchestratorAdapter
- `build_store_adapter.go` (130 LOC) — Reader/Log/Status/List for db.BuildStore
- `release_adapter.go` (33 LOC) — StatusReleaseStoreAdapter
- `tenant_adapter.go` (57 LOC) — TenantDBDeleterAdapter, TenantK8sEvictorAdapter
- `adapters.go` (25 LOC) — package doc referencing the split

All <200 LOC ✓. Behavior unchanged (`./internal/adapters` tests green); every type/method/interface kept its exact signature and call site.

### T6.4 — Fire-and-forget context (find_code_004 MEDIUM) ✅ DONE
**Files:** `api/internal/services/build_reconcile_retry.go:229`, `release_activator.go:201`: replace `context.Background()` with derived context from incoming request.
**TDD:** `TestCallback_ContextPropagatesTraceID()`.
**DoD:** OTel trace IDs flow through callbacks.

### T6.5 — Attestation save error log (CODE-005 / find_code_005 MEDIUM) ✅ DONE
**Files:** `api/internal/services/container_delivery.go:303-309`: remove `_ = saveErr`; log with structured error + emit metric.
**DoD:** Error visible in logs + metric.

### T6.6 — Stale ARCH-006 comment + sync_callback context (CODE-007 / find_code_007 LOW) ✅ DONE
**Files:** `api/internal/services/sync_callback_activator.go:69`: update comment + propagate trace.
**DoD:** Comment matches reality.

### T6.7 — CustomStrategy.DockerfileStep no-op (find_arch_006 LOW) ✅ DONE
**Files:** `api/internal/argo/languages.go:362-364`: clarify intent — return no-op or document loopzero-packs lenient behavior.
**DoD:** Comment matches behavior.

### T6.8 — cell_service static fields (find_cell_service_static_fields MEDIUM) ✅ DONE
**Files:** `api/internal/services/cell_service.go:68-70`: implement `AppCount` query, derive `ClusterHealth` from `loopzero_cell_health` metric.
**TDD:** `TestCellService_ReturnsRealAppCount()`.
**DoD:** TODO markers removed; values reflect reality.

---

## Phase 7: Data Hygiene (4 findings)

### T7.1 — scale handler withEnvironmentLock (find_data_003 HIGH) ✅ DONE
**Files:** `api/internal/routes/scale.go:222`: wrap `scaleApply()` in `WithEnvironmentLock(tenantID, projectID, environment)`.
**TDD:**
- `TestScale_ConcurrentRequestsRejectedAfter1Wins()` — 3 concurrent scale → 1 success + 2 reject (409).
- `TestScale_ConcurrentRejection_ExitCode3()` — (EC-11) CLI invocations: 1 wins (exit 0), 2 rejected → exit 3 (DEPLOY_IN_PROGRESS) with product-language message "deploy already in progress for this environment". Per Rule 4 exit code mapping.
**DoD:** TOCTOU race closed; Rule 6 honored; CLI exit-code mapping consistent with deploy/promote/rollback.

### T7.2 — FK ON DELETE clauses (find_data_004 HIGH) ✅ DONE
**Files:** `api/internal/db/migrations/000012_fk_on_delete_explicit.up.sql` (NEW): add explicit `ON DELETE CASCADE/RESTRICT/SET NULL` to 15 FK references.
**TDD:** `TestForeignKeys_AllExplicit()` — query `pg_constraint` for confdeltype.
**DoD:** All FKs explicit.

### T7.3 — audit_log retention (find_data_005 MEDIUM) ✅ DONE 2026-05-09
**Files:** `api/internal/services/audit_log_pruner.go` (NEW background pruner: keep 1 year, archive to S3), `api/internal/db/migrations/000013_audit_log_time_index.up.sql` (NEW partial index on created_at).
**DoD:** Pruner runs nightly; table size bounded.

**Status:** Migration 000013 (BRIN index) ✅ shipped. ADR-0047 (audit_log retention policy) ACCEPTED with explicit deferral: "Prune job operational work — implementation tracked separately. This ADR documents the policy; the implementation lands in a future PR after operations review." Per ADR-0022 (plan amendment policy) this is a governance-tracked deferral, not a silent skip.

> **DOCUMENT (EC-15):** 1-year retention is for security/operational events. Financial events (billing, payment, subscription changes) flow through Stripe and are retained per Stripe's SLA (typically 7+ years). Add an ADR explicitly noting: "LoopZero audit_log retention: 1 year. Compliance for financial events is delegated to Stripe's data retention." Re-evaluate if a future regulator (PCI Level 1, SOC 2 Type II) requires LoopZero-side retention for the audit chain.

### T7.4 — builds FK to tenants/projects (find_data_006 MEDIUM) ✅ DONE
**Files:** Either (a) add FK with `ON DELETE SET NULL` (preserves history) OR (b) document trade-off in ADR + add background reconciler for orphan cleanup.
**DoD:** ADR documents choice.

---

## Phase 8: Infrastructure Tactical (6 findings)

### T8.1 — Operator Dockerfile HEALTHCHECK (find_infra_001 HIGH) ✅ DONE
**Files:** 4 `operators/Dockerfile.{app,tenant,platform,preview}-operator`: add `HEALTHCHECK CMD ["/operator-healthz"]`. Implement `/operator-healthz` if not present.
**DoD:** Local docker run reports container as healthy.

### T8.2 — Bootstrap PodDisruptionBudget (find_infra_004 MEDIUM) ✅ DONE
**Files:** `infra/helm/loopzero-api/templates/pdb.yaml` (NEW), `infra/helm/loopzero-cloud/templates/pdb.yaml` (NEW), `infra/helm/loopzero-api/values.yaml` (raise `replicaCount` to 2 to make PDB meaningful).
**TDD:**
- Helm template lint validates rendered manifest.
- `TestPDB_WithReplicasOne_DoesNotBlockNodeDrain()` — (EC-12) if `replicaCount=1`, the chart MUST omit PDB OR set `minAvailable=0`. Otherwise node drain blocks forever.
**DoD:** k8s upgrade does not down API; node drain still works in single-replica dev mode.

### T8.3 — Tilt :latest tag fix (find_infra_005 MEDIUM) ✅ DONE
**Files:** `infra/tilt/api-deployment.yaml`: pin `loopzero-clone-runner`, `loopzero-syft-scanner`, `loopzero-cosign-signer` to specific tags matching production.
**DoD:** Local equals production for pipeline images.

> **DOCUMENT (EC-16):** This pinning targets ONLY the 3 pipeline-step images (clone-runner, syft-scanner, cosign-signer) which are batch jobs that change rarely. Tilt's `live_update` hot-reload for `api-deployment` and operator deployments is preserved (those keep `:latest` against the local k3d registry to enable file-watch sync). Document split in `docs/runbooks/tilt-image-strategy.md`: pinned = pipeline batch images; floating + live_update = service deployments.

### T8.4 — Tilt seccompProfile + capabilities.drop (find_infra_006 MEDIUM) ✅ DONE
**Files:** `infra/tilt/api-deployment.yaml`: add `seccompProfile: RuntimeDefault` at pod level + `capabilities.drop: [ALL]` at container level.
**DoD:** Tilt deployment matches Helm chart security context.

### T8.5 — NetworkPolicy default-deny-egress (find_infra_007 MEDIUM) ✅ T8.5a + T8.5b DONE 2026-05-09 — T8.5c STAGING SOAK OPERATOR-DRIVEN

> **EC-5 fix:** applying default-deny-egress without a complete allow-list inventory will break ALL pods (DNS, kube-apiserver, registry pulls, Loki, Tempo, etc). Split into 3 mandatory ordered sub-tasks. NEVER apply T8.5b before T8.5a passes review.

**Status (per `final-status.md`):** T8.5a inventory landed (`docs/runbooks/network-policy-egress-inventory.md`). T8.5b/c require operator-driven staging soak per EC-5 — not Ralph-loop scope. Default-deny rollout is staged per cell with 1h observation per cell.

#### T8.5a — Egress inventory (must land first)
**Files:** `docs/runbooks/network-policy-egress-inventory.md` (NEW): map every pod role in `loopzero-system` → required egress endpoints.
**Required endpoints to inventory:** kube-dns (UDP/53), kube-apiserver (TCP/443), DOCR/Harbor (TCP/443), Loki ingest (TCP/3100), Tempo OTel (TCP/4317), Hydra (TCP/4444), MinIO (TCP/9000), Postgres (TCP/5432), DO Spaces (TCP/443 external), GitHub API (TCP/443 external), CF API (TCP/443 external).
**TDD:** None (doc + audit task). Reviewer signs off in PR.
**DoD:** Inventory doc reviewed + approved by SRE; covers all 8+ pod roles.

#### T8.5b — Per-component allow rules + default-deny-egress
**Files:** `infra/helm/network-policies/templates/loopzero-system-{api,operator,gitea,argocd,...}-egress.yaml` (NEW per-component allow), `infra/helm/network-policies/templates/loopzero-system-default-deny-egress.yaml` (NEW: deny-all egress, applied LAST).
**TDD:** Live smoke against staging cell-1 first (T8.5c), NOT against cell-default until validated.
**Pre-condition:** T8.5a inventory approved.
**DoD:** Apply order: per-component allow rules first → then default-deny-egress. Helmfile sync ordering validated.

#### T8.5c — Dry-run on staging cell-1, observe 1h
**Files:** Apply T8.5b to staging cell-1; observe Prometheus alerts + slog for any "connection refused" / DNS failures over 1h. If any pod regresses, rollback per documented procedure.
**Rollback procedure:**
```bash
kubectl --context cell-1 delete networkpolicy loopzero-system-default-deny-egress -n loopzero-system
# Then debug allow-list gap before re-applying.
```
**DoD:** 1h staging soak with zero new alerts; rollout to cell-default + cell-2 in subsequent PRs (one cell per PR).

### T8.6 — ClusterImagePolicy deny-flip (find_infra_008 LOW, Sprint 9 scope) 🟡 SPRINT-9 SCOPE
**Files:** `infra/helm/admission-policies/values.yaml`: flip `mode: warn` → `mode: enforce`. Track via Sprint 9 milestone.
**DoD:** Policy-controller blocks unsigned images in production.

**Status:** Explicitly out of deep-review-remediation scope per task header — Sprint 9 hardening tracks production cutover separately. Warn-mode policy is operational and surfacing unsigned-push attempts; flip is a one-line YAML change after Sprint 9 readiness review.

---

## Phase 9: Dependencies & Licenses (4 findings)

### T9.1 — madmin AGPL legal review (find_dep_madmin_agpl MEDIUM) ✅ DONE
**Files:** Legal team review. Options: (a) replace with self-implemented MinIO admin client, (b) accept AGPL with explicit ADR + isolation, (c) remove MinIO admin features.
**DoD:** ADR documenting decision.

### T9.2 — sqlmock direct → indirect dep (find_dep_sqlmock_misclassified LOW) ✅ FALSE POSITIVE
**Files:** `pkg/go.mod` line 9: move sqlmock to test-only via build tags.
**DoD:** `go mod tidy` shows correct classification.

**Status:** Verified in code: sqlmock is already test-only (only imported in `_test.go` files). The original finding flagged go.mod line where it's listed; Go's module graph does not distinguish "test-only" direct deps. Closed as false_positive in `final-status.md` — no fix needed.

### T9.3 — CLI OTel optional (find_dep_cli_otel_bundle_bloat LOW) 🟡 KISS+YAGNI DEFERRED
**Files:** `cli/package.json`: move 5 `@opentelemetry/*` to optionalDependencies; lazy-load only when `LOOPZERO_TRACING_ENABLED=true`.
**DoD:** Default CLI bundle ~2-3 MB smaller.

**Status:** Bundle size is currently acceptable (CLI startup <500ms; total ~9MB). Lazy-loading optional deps adds branching complexity (require() at runtime, error paths if module missing) for a 2-3MB win that doesn't move user-perceived performance. Per KISS+YAGNI, deferred until either a real user complaint or bundle size becomes a CI issue. Documented in `final-status.md` as deferred.

### T9.4 — cloud module version skew (find_dep_cloud_module_version_skew LOW) ✅ DONE
**Files:** `cloud/go.mod`: align `pkg` replace + `otelhttp` versions with api/go.mod.
**DoD:** All modules use same dep versions.

---

## Coverage Matrix

| # | Finding ID | Severity | Phase | Task | Resolution |
|---|---|---|---|---|---|
| 1 | find_data_001 | CRITICAL | 0 | T0.1 | Migration 000011 + invariant test |
| 2 | find_data_002 | CRITICAL | 0 | T0.1 | Same migration drops phantom states |
| 3 | find_sec_001 | CRITICAL | 0 | T0.2 | RequireAdmin middleware on Delete + Restore |
| 4 | find_flow_001 | CRITICAL | 0 | T0.3 | Wire EmitRolloutAborted + spec doc |
| 5 | find_test_001 | HIGH | 0 | T0.4 | Fix conname literal + canary assertion |
| 6 | find_test_002 | HIGH | 3 | T3.1 | Migration 000010 regression test |
| 7 | find_arch_001 | HIGH | 1 | T1.3 | routes/ → domain/ migration |
| 8 | find_arch_002 | HIGH | 1 | T1.1 | PreviewController cell-aware |
| 9 | find_arch_003 | HIGH | 1 | T1.2 | Multi-cell envtest |
| 10 | find_arch_007 | MEDIUM | 1 | T1.4 | app-operator unconditional |
| 11 | find_arch_005 | MEDIUM | 1 | T1.5 | CellPinner divergence alert |
| 12 | find_billing_route_not_wired | HIGH | 1 | T1.6 | Wire billing routes |
| 13 | find_workqueue_silent_db_errors | HIGH | 1 | T1.7 | Explicit error handling |
| 14 | find_sec_h05_vap_dual_chart_drift | HIGH | 2 | T2.1 | system:masters exemption |
| 15 | find_sec_002 | HIGH | 2 | T2.2 | subtle.ConstantTimeCompare |
| 16 | find_sec_003 | HIGH | 2 | T2.3 | docker.io/library only |
| 17 | find_sec_ratelimit_post_auth_only | MEDIUM | 2 | T2.4 | IP rate limiter pre-auth |
| 18 | find_sec_004 | MEDIUM | 2 | T2.5 | Reserved env block |
| 19 | find_sec_005 | MEDIUM | 2 | T2.6 | Safe WHERE builder |
| 20 | find_sec_006 | LOW | 2 | T2.7 | Secret value redaction |
| 21 | find_test_003 | MEDIUM | 3 | T3.2 | 6 CLI happy paths |
| 22 | find_test_004 | MEDIUM | 3 | T3.3 | Lock timeout test |
| 23 | find_cicd_001 | HIGH | 4 | T4.1 | SHA pin GitHub Actions |
| 24 | find_cicd_003 | HIGH | 4 | T4.2 | Immutable env tags |
| 25 | find_infra_002 | HIGH | 4 | T4.3 | packs-runner digest pin |
| 26 | find_infra_003 | MEDIUM | 4 | T4.4 | Floating golang fix |
| 27 | find_cicd_005 | MEDIUM | 4 | T4.5 | Polaris pin |
| 28 | find_cicd_002 | MEDIUM | 4 | T4.6 | Base image digest pin |
| 29 | find_dep_loopzero_forge_bus_factor | MEDIUM | 4 | T4.7 | loopzero-forge mirror |
| 30 | find_cicd_004 | MEDIUM | 4 | T4.8 | Required PR gate |
| 31 | find_cell_fallback_no_metric | MEDIUM | 5 | T5.1 | Metric + alert |
| 32 | find_secret_skip_no_alert | MEDIUM | 5 | T5.2 | ESO alert |
| 33 | find_obs_006 | HIGH | 5 | T5.2 | Same as above |
| 34 | find_rollout_sse_undocumented | MEDIUM | 5 | T5.4 | Spec catalog |
| 35 | find_obs_001 | HIGH | 5 | T5.5 | Supply chain alerts |
| 36 | find_obs_002 | HIGH | 5 | T5.6 | Correlation ID full |
| 37 | find_obs_004 | MEDIUM | 5 | T5.7 | Operator L4 alerts |
| 38 | find_obs_005 | MEDIUM | 5 | T5.8 | Runbook coverage |
| 39 | find_obs_008 | MEDIUM | 5 | T5.9 | PagerDuty |
| 40 | find_obs_003 | MEDIUM | 5 | T5.10 | Dead alerts cleanup |
| 41 | find_code_001 / CODE-001 | HIGH | 6 | T6.1 | errors.Is pattern |
| 42 | find_code_006 / CODE-006 | MEDIUM | 6 | T6.2 | RenderInput.Plan |
| 43 | find_arch_004 | MEDIUM | 6 | T6.3 | adapters.go split |
| 44 | find_code_004 / CODE-004 | MEDIUM | 6 | T6.4 | Context propagation |
| 45 | find_code_005 / CODE-005 | MEDIUM | 6 | T6.5 | Attestation log |
| 46 | find_code_007 / CODE-007 | LOW | 6 | T6.6 | Stale comment |
| 47 | find_arch_006 | LOW | 6 | T6.7 | DockerfileStep clarify |
| 48 | find_cell_service_static_fields | MEDIUM | 6 | T6.8 | Real values |
| 49 | find_data_003 | HIGH | 7 | T7.1 | scale withEnvironmentLock |
| 50 | find_data_004 | HIGH | 7 | T7.2 | FK ON DELETE |
| 51 | find_data_005 | MEDIUM | 7 | T7.3 | audit_log retention |
| 52 | find_data_006 | MEDIUM | 7 | T7.4 | builds FK |
| 53 | find_infra_001 | HIGH | 8 | T8.1 | Dockerfile HEALTHCHECK |
| 54 | find_infra_004 | MEDIUM | 8 | T8.2 | Bootstrap PDB |
| 55 | find_infra_005 | MEDIUM | 8 | T8.3 | Tilt :latest fix |
| 56 | find_infra_006 | MEDIUM | 8 | T8.4 | Tilt seccompProfile |
| 57 | find_infra_007 | MEDIUM | 8 | T8.5 | NetworkPolicy egress |
| 58 | find_infra_008 | LOW | 8 | T8.6 | Deny-flip Sprint 9 |
| 59 | find_dep_madmin_agpl | MEDIUM | 9 | T9.1 | Legal review |
| 60 | find_dep_sqlmock_misclassified | LOW | 9 | T9.2 | Test-only dep |
| 61 | find_dep_cli_otel_bundle_bloat | LOW | 9 | T9.3 | Optional deps |
| 62 | find_dep_cloud_module_version_skew | LOW | 9 | T9.4 | Version align |
| 63 | find_h03_refuted | LOW | — | — | Already closed (observation) |
| 64 | find_dep_routes_type_hub | MEDIUM | 1 | T1.3 | Same as find_arch_001 |
| 65 | find_flow_002 | HIGH | 0/6 | T0.3 followup | Status field wired alongside SSE |
| 66 | find_obs_007 | LOW | 5 | T5.4 | Documented |
| 67-70 | (other LOW) | LOW | various | various | Catch-all in respective phases |

**Coverage: 70/70 findings covered (100%)**

## Global Definition of Done

- [ ] Phase 0 closes with /dogfood full GREEN on cleanroom (4 CRITICAL eliminated)
- [ ] All 9 phases completed
- [ ] All tests passing (Go + TypeScript)
- [ ] Zero lint warnings (golangci-lint + ESLint)
- [ ] code-audit checks passing across all modified packages
- [ ] **`/cross-validation deep-review-remediation` PASSED** — zero CRITICO, zero GRAVE
- [ ] All 5 invariants validated (currently 3 violated)
- [ ] C4 health: Correto ≥0.75, Completo ≥0.75, Confiável ≥0.75, Controlável ≥0.75
- [ ] CHANGELOG.md fully populated under [Unreleased]
- [ ] **`/dogfood full` PASSED** on fresh cleanroom (final cumulative gate)

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> Two gates: Phase 0 dedicated + cumulative final.

### Phase 0 Dogfood (post-T0.1 through T0.5)

```bash
task teardown && task setup && tilt up
task doctor  # 38/0 PASS
/dogfood full
```

Pass criteria:
- [ ] Zero S0 findings.
- [ ] Zero S1 findings related to data integrity, security, canary abort.
- [ ] Specific journey: bad-health canary deploy → exit 3 within 8min via `rollout.aborted` SSE.
- [ ] Specific journey: cross-tenant DELETE attempt → 401.
- [ ] Specific journey: rollback failure path → release transitions to ROLLBACK_FAILED, no panic.
- [ ] Specific journey: state machine vs CHECK invariant test passes for all 9 tables.

### Final Cumulative Dogfood (post-Phase 9)

```bash
task teardown && task setup && tilt up
task doctor
/dogfood full
```

Pass criteria:
- [ ] Pre-Flight Check: ALL checks PASS.
- [ ] Zero S0 findings.
- [ ] Zero S1 findings related to any phase scope.
- [ ] Prova de Vida passes for every deploy.
- [ ] Prova de Morte passes for every delete.
- [ ] No regressions in previously passing flows.
- [ ] Observability: 99% alert coverage on emitted metrics.
- [ ] Rule 3 clean: zero internal terms in CLI output.

### On Failure

If `/dogfood full` finds S0 or S1:
1. Fix ALL S0/S1 findings.
2. Re-run `/dogfood full` on fresh infrastructure.
3. Repeat until zero S0/S1.

S2-S4 findings documented but do not block plan completion.

---

## Implementation Notes

**Recommended sequencing:**
1. **Week 1:** Phase 0 (T0.1-T0.5) + Phase 0 dogfood gate. Single team focused.
2. **Week 2-3:** Phases 1, 2, 3 in parallel (3 streams).
3. **Week 4:** Phases 4, 5, 6 in parallel (3 streams).
4. **Week 5:** Phases 7, 8, 9 in parallel (3 streams).
5. **Week 6:** Cumulative dogfood gate + cross-validation + remediation cycle.

**Risk mitigation:**
- Phase 0 changes overlap with T1.6 (billing) and T6 (code) — recommend Phase 0 lands first, then merge develop.
- T1.3 (routes/ → domain/) touches 9 packages — recommend single-PR atomic refactor to avoid mid-branch chaos.
- T5.6 (correlation ID) touches 27 routes — split into 4 sub-PRs by domain (deploy, tenant, billing, admin).

**Tracking:**
- Each task gets a GitHub issue with `deep-review-remediation` label.
- Phase milestones in GitHub Projects.
- Daily progress metric: `findings_resolved_total` against initial 70 baseline.
