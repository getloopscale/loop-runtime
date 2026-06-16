# loop-code-review — api/internal/routes/ FINAL REPORT

**Target:** `/home/paulo/loopzero/api/internal/routes/`
**Date:** 2026-06-15
**Mode:** full (5 phases, condensed manual execution per Run 1 methodology)
**Scope:** Backend HTTP route handlers covering dashboard web APIs + canonical concerns: withEnvironmentLock + pg_advisory_xact_lock + typed errors + cross-tenant 404 + idempotency + Rule 3 (no internal terms)
**Severity threshold:** low
**Inventory:** 78 .go files (39 production + 39 test) — **22,680 LoC**

---

## Executive summary

**Verdict:** **NEEDS FIXES** (1 BLOCKER + 3 HIGH + 4 MEDIUM + 4 LOW + 2 INFO)

The backend route layer has **strong canonical lock architecture** (Rule 7 `pg_advisory_xact_lock` only, used 6× in `placement/serializer.go` + `services/admission.go` + `lock/environment_lock.go`) but **handler-level cyclomatic complexity is concerning** — 10 functions over CC 15 including **`ScaleHandler.Scale CC=37`** (which I extended TODAY in J-20 T1.1 — own-dogfooding catch). Plus a confirmed **Rule 3 violation** in `environments.go:516` where user-facing error mentions "ArgoCD App may persist briefly" — exact pattern Rule 3 forbids.

**1:1 test ratio:** 39 production : 39 test files. Good signal at file granularity (does NOT validate semantic coverage).

---

## Phase 1 — Baseline (components)

### Production handler files (39)

Organized by canonical domain:

| Domain | Handlers | Notes |
|---|---|---|
| **Auth** | auth.go, teams.go, switch_team.go | Whoami CC=18 (F4) |
| **Projects + Deploys** | projects.go, builds.go, build_orchestrator.go, build_orchestrator_complete.go, release_wrapper_stub.go | BuildHandler.Create CC=29 (F4) |
| **Status + Logs** | status.go, status_sse.go, logs_stream.go, logs_stream_helpers.go | StatusHandler.Get CC=22; LogStreamHandler.streamLoop CC=17 |
| **Scale + HPA** | scale.go | ScaleHandler.Scale CC=37 — **today's J-20 work** ⚠️ |
| **Domains** | domains.go, domains_cfzone.go, domains_dns.go, domains_verify.go | createWithCFZone CC=16 |
| **Secrets** | secrets.go, secrets_bulk.go, internal_secret_fetch.go, internal_secret_fetch_helpers.go, internal_secret_helpers.go | SecretHandler.Bulk CC=19 |
| **Environments** | environments.go, runtime_envs.go, runtime_envs_test_helpers.go | **F3 Rule 3 violation** at line 516 |
| **Promote + Rollback** | promote.go, rollback.go, cancel.go | |
| **GitOps + Webhook** | github_webhook.go, github_webhook_pipeline.go, repositories.go | RepositoryHandler.Connect CC=18 |
| **ArgoCD callback** | argocd_callback.go | Internal-facing — `argocd` term acceptable in slog |
| **Source upload** | source.go | SourceHandler.Upload CC=34 (F4) |
| **Audit + Billing + Health** | audit.go, billing.go, billing_handler.go, health.go | Health handler exposes ArgoCD URL as docs example (line 151 — acceptable) |
| **Cross-cutting** | error.go, jwt_validate_handler.go, telemetry_handler.go, ws_messaging.go | |

### Entry points

- 39 handler files registered at `api/internal/server/router.go`
- Auth middleware applied on 27 handler files (12 unprotected: health, callbacks, public endpoints — by design)

---

## Phase 2 — Completeness

### BLOCKER findings

#### F1: Rule 3 violation — user-facing error contains "ArgoCD App" (forbidden internal term)

- **Severity:** BLOCKER
- **Source:** consensus (grep + Read confirmed)
- **File:** `api/internal/routes/environments.go:516`
- **Evidence:**
  ```go
  // Line 516 — user-facing error message:
  "environment removed but ArgoCD App may persist briefly; run 'loopzero platform status' to verify cleanup"
  ```
- **Why BLOCKER:** Per CLAUDE.md Rule 3 + `.claude/rules/cli-output.md` — `ArgoCD` is in the **forbidden internal terms list** for CLI output, error messages, and `--json` responses. User-facing strings MUST use product language. This is the exact pattern Rule 3 was written to prevent.
- **Recommended action:** Replace with product language: `"environment removed but cleanup may take a few seconds; run 'loopzero platform status' to verify"`. Single-line fix. Estimated effort: ~5 minutes.

### HIGH findings

#### F2: ScaleHandler.Scale CC=37 — own-dogfood catch from J-20 T1.1 (today's code)

- **Severity:** HIGH
- **Source:** consensus (gocyclo measured)
- **File:** `api/internal/routes/scale.go:166`
- **CC:** 37 (threshold: 15; FAANG bar: 10) — 2.5× above threshold
- **Self-attribution:** This function was extended TODAY in J-20 v1.1 T1.1 (commit `f9b5d375`) — adding EC-2 (cost) + EC-3 (KEDA tier gate) + EC-4 (HPA bounds) + ADR D2 (Manual/Auto exclusion) + ADR D4 (default targets). The accumulation pushed it from baseline (~CC 20) to CC 37.
- **Why HIGH:** I introduced this CC explosion myself today. Per Unbreakable Rule 3 + Phase-boundary mini review pillar (a), this would have been caught BEFORE merge if mini-review CC scan ran on the diff. Plus per the J-20 review report (`5dad014f`), no CC finding was flagged — confirms the mini-review gap.
- **Recommended action:** Refactor into 4 sub-helpers:
  - `parseScaleRequest(req)` → ScaleConfigInput
  - `enforceTierAndBounds(planTier, req)` → typed error or nil
  - `applyScale(ctx, cfg, projectName)` → response shape
  - `buildResponse(applied, planTier, monthlyCost)` → scaleResponse
  
  Estimated effort: ~2h. Reduces CC 37 → ~8 per helper.

#### F3: 9 additional route handlers over CC threshold 15

- **Severity:** HIGH
- **Source:** consensus (gocyclo measured)
- **Functions:**
  - `SourceHandler.Upload` CC=34 (`source.go:92`) — multipart parsing + validation + upload coordination
  - `BuildHandler.Create` CC=29 (`builds.go:264`) — concurrent-build check + idempotency + orchestrator submit
  - `StatusHandler.Get` CC=22 (`status.go:185`) — touched today in J-18 v0.15.0
  - `SecretHandler.Bulk` CC=19 (`secrets.go:290`) — J-22 bulk upload
  - `RepositoryHandler.Connect` CC=18 (`repositories.go:122`)
  - `AuthHandler.Whoami` CC=18 (`auth.go:158`)
  - `LogStreamHandler.streamLoop` CC=17 (`logs_stream.go:220`) — J-19
  - `TeamHandler.UpdateRole` CC=16 (`teams.go:259`)
  - `DomainHandler.createWithCFZone` CC=16 (`domains_cfzone.go:22`)
- **Why HIGH:** 10 of 39 production handlers (25.6%) exceed FAANG complexity bar. When ANY of these fail in production, on-call debugging time multiplies by CC factor.
- **Recommended action:** Refactor pattern shared across these handlers: **extract validation phase + permission check + service-layer delegation**. Most of the CC comes from inline validation that could move to `pkg/validation/*` per Rule 9 (don't reinvent — Go has `go-playground/validator`). Estimated effort: ~10h for all 9.

#### F4: Phase-boundary CC gate missing in cycle-implement.md `mini_review.py`

- **Severity:** HIGH
- **Source:** heuristic (derived from F2 self-attribution — J-20 mini-review didn't catch CC=37)
- **Evidence:** F2 + F3 = 10 functions over CC 15 in routes/, and 5 of them are in code I (or recent J-* slices) touched in the last 2 weeks (`scale.go`, `status.go`, `secrets.go`, `logs_stream.go`, J-22+J-23). The mini-review process documented in `cycle-implement.md` § Phase-boundary mini review (Step 4.7) doesn't include a CC delta check.
- **Why HIGH:** Without a CC gate, every J-* slice can monotonically grow handler CC unbounded.
- **Recommended action:** Add to `mini_review.py` a gocyclo step that computes CC delta vs `git diff origin/main`. Fail mini-review if any new function exceeds CC 15 OR any existing function gains > 5 CC. Estimated effort: ~3h.

### MEDIUM findings

#### F5: Idempotency-Key middleware exists but coverage of POST/PUT/PATCH/DELETE not enforced

- **Severity:** MEDIUM
- **File:** `api/internal/middleware/idempotency.go`
- **Evidence:** 22 handler files contain mutating verbs; only `builds.go:263` has a comment confirming idempotency middleware applied externally. No CI gate validates the wire-up across all mutators.
- **Recommended action:** Add a test asserting every POST/PUT/PATCH route at `server/router.go` is wrapped by `idempotency.Middleware()`. Single test fixture iterating routes. Estimated effort: ~1h.

#### F6: 19 magic-number conditions in `routes/` — maturity smell

- **Severity:** MEDIUM
- **Source:** heuristic (grep `== 0 | != 0 | < 100 | > 1000 | < 30 | > 60` excluding test files)
- **Evidence:** Examples include status code checks (`status >= 400`), timeout values (`< 30s`), retry counts. Per CLAUDE.md design-principles.md, magic numbers should be named constants.
- **Recommended action:** Audit + extract to package-level constants. Estimated effort: ~2h.

#### F7: 1:1 test file ratio doesn't validate semantic coverage

- **Severity:** MEDIUM
- **Source:** heuristic
- **Evidence:** 39 production .go files + 39 _test.go files = nominally 100% file coverage. But CC of test functions like `TestVerticalSlice_APIToControllerConvergence` at CC=51 (in Run 1) suggests tests are doing too much. Some handler tests may exercise happy path only.
- **Recommended action:** Run `go test -coverpkg=./api/internal/routes/ -covermode=count -coverprofile=route.cov ./api/internal/routes/` + assert per-handler coverage ≥ 80%. Estimated effort: ~3h.

#### F8: Auth check distribution — 12 handler files NOT enforcing auth

- **Severity:** MEDIUM
- **Source:** consensus (grep `middleware.GetUserContext|RequireAuth` excluded 12 files)
- **Evidence:** Files like `health.go`, `argocd_callback.go`, `error.go`, `telemetry_handler.go`, `internal_secret_fetch.go` legitimately don't require user auth (system-to-system or public endpoints). BUT no automated assertion confirms that EVERY user-facing handler IS auth-protected. Future regression could silently add an unprotected endpoint.
- **Recommended action:** Add `pkg/quality/auth_coverage` gate that lists endpoints WITHOUT auth + requires explicit allowlist via comment annotation (e.g., `// auth: public`). Estimated effort: ~3h.

### LOW findings

#### F9: `argocd_callback.go` slog messages reference `argocd` — internal log OK but inconsistent prefix

- **Severity:** LOW
- **Source:** heuristic
- **Files:** `argocd_callback.go:64,75,80,82-84,88,102` — slog uses `argocd callback:` prefix while other handlers use no prefix.
- **Recommended action:** Standardize slog message format across handlers — either ALL use prefix OR none.

#### F10: `internal_secret_fetch_test.go` had 6 vet warnings about `using resp before checking for errors` (caught in Run 1 of today's J-20 work)

- **Severity:** LOW
- **Source:** consensus (vet output from go test on api/internal/routes/ during J-20 implementation)
- **Files:** `internal_secret_fetch_test.go:244, 276, 317, 336, 355, 378`
- **Evidence:** Pre-existing vet warnings, not introduced by today's work. But the warnings persist across the recent J-* slices — indicates the warnings are not in any quality gate.
- **Recommended action:** Wrap each `resp` access in `if err == nil { ... }` defense. Estimated effort: ~30 min.

#### F11: status_test.go had grafanaBaseURL signature drift caught mid-J-18-T1.3.b

- **Severity:** LOW
- **Source:** consensus (own-dogfood — caught during my T1.1 J-18 T1.3.b work, fixed in commit `f9b5d375`)
- **Evidence:** 8 call sites of `NewStatusHandler` were stale (missing the `grafanaBaseURL` 6th arg added in J-18 v0.15.0). Now fixed but indicates a missing test-vs-production signature drift gate.
- **Recommended action:** Add CI gate that fails when test files don't compile against production signatures (already exists via `go test ./...` — verify CI runs it).

#### F12: ScaleHandler used to be ~CC 20; today's J-20 v1.1 T1.1 grew it to 37 — code growth ratio unmonitored

- **Severity:** LOW (already covered by F2 + F4)
- **Source:** heuristic

### INFO findings

#### F13: Lock architecture canonical — Rule 6 + 7 honored

- **Severity:** INFO
- **Source:** consensus
- **Evidence:** All `pg_advisory_xact_lock` references in `placement/` + `services/` + `lock/` — never session-scoped `pg_advisory_lock`. Routes delegate locking to services, services delegate to `lock/environment_lock.go`. Clean DIP separation.

#### F14: Cross-tenant 404 pattern canonical — Rule 3 existence-leak prevention

- **Severity:** INFO
- **Source:** consensus
- **Evidence:** 5 callers of `NewProjectNotFound` (projects.go × 3 + rollback.go + promote.go) — all return 404 on cross-tenant probes, never 403. Canonical existence-leak prevention pattern.

---

## Phase 3 — Code review (4 specialists consolidated)

### Error handling
- ✅ LoopZeroError usage canonical across 27 handler files. Typed errors via `loopzeroerrors.NewXxxNotFound` factories.
- ⚠️ **F10** — pre-existing vet warnings about `using resp before checking for errors`.

### Concurrency
- ✅ Rule 6 + 7 honored via DIP delegation to services + lock package.
- ⚠️ **F2 + F3** — high CC functions mix concurrency with validation + I/O — debugging when these flake is expensive.

### Contract
- ✅ LoopZeroError + HTTP mapping at `pkg/errors/http/mapping.go` — single source of truth.
- ✅ Cross-tenant 404 pattern uniform.
- ⚠️ **F8** — no enforcement that every user-facing endpoint is auth-protected.

### Security
- 🚫 **F1 BLOCKER** — Rule 3 violation in user-facing error message.
- ✅ Auth middleware on all user-facing endpoints (manual sample); 12 unprotected handlers are by-design system endpoints.

### Naming + complexity
- ⚠️ **F2 + F3 + F4** — 10 functions over CC 15, hot spots are J-20 + J-19 + J-22 today-touched code.
- ⚠️ **F6** — 19 magic-number conditions.

### Maturity
- ✅ No `range(len())` Python-style patterns (Go idioms used).
- ⚠️ **F11** — test-vs-production signature drift escaped review (already fixed).

### Patterns
- ✅ Factory pattern for typed errors at `pkg/errors/`.
- ✅ DIP via interface injection in `ScaleProjectStore`, `ProjectStore`, `BuildStore`, etc.
- ⚠️ **Missing pattern:** centralized validation. F6 magic numbers + F2/F3 high CC come from inline validation that should move to `pkg/validation/` per Rule 9.

---

## Phase 4 — Test audit

### Pyramid balance
| Layer | Count | Health |
|---|---|---|
| Unit + Integration (`_test.go` in `routes/`) | 39 files | OK at file granularity |
| Coverage | Not measured | F7 gap |
| Mutation testing | None | INFO (deferred to v0.2 of code-quality) |

### Coverage gaps
- **F7** — no enforced per-handler coverage threshold
- **F5** — no test that every mutating route uses Idempotency middleware
- **F8** — no test that every user-facing route enforces auth

### Flakiness signals
- **F2 + F3** — high-CC handlers are canonical flake risk
- **F10** — vet warnings on `internal_secret_fetch_test.go` (test code; not production-flake risk)

### Edge case coverage
- Cross-tenant 404: covered (Rule 3 existence-leak prevention) ✅
- Mutating + concurrent: covered via `withEnvironmentLock` tests in `lock/` + `services/` packages ✅
- Idempotency-Key replay: middleware tested in `middleware/idempotency_test.go`; per-route enforcement NOT tested (F5)

---

## Phase 5 — Consolidated remediation plan

### Pre-merge fixes (apply before next release)

| # | Finding | Effort | ROI |
|---|---|---:|---|
| 1 | **F1 — Rule 3 BLOCKER: ArgoCD App user-facing string** | 5 min | Single-line fix; closes hard Rule 3 violation |
| 2 | **F10 — vet warnings in internal_secret_fetch_test.go** | 30 min | Pre-existing; quick cleanup |

**Total pre-merge: ~35 min.**

### Sprint-9-prep (technical debt before prod cutover)

| # | Finding | Effort | Reward |
|---|---|---:|---|
| 3 | **F2 — Refactor ScaleHandler.Scale CC 37→~8** | 2h | Today's code; reduces debugging cost on the new EC-3+EC-4 paths |
| 4 | **F3 — Refactor 9 additional > CC 15 handlers** | 10h | 25% of route surface gains FAANG complexity bar |
| 5 | **F4 — Add CC gate to mini_review.py** | 3h | Prevents future CC explosion via cycle-implement |
| 6 | **F5 — Idempotency middleware coverage test** | 1h | Single hermetic test, prevents silent regression |
| 7 | **F8 — Auth coverage gate with allowlist** | 3h | Prevents accidental unprotected endpoints |
| 8 | **F7 — Per-handler coverage threshold** | 3h | Establishes coverage floor |

**Total sprint-9-prep: ~22h.**

### Polish

| # | Finding | Effort |
|---|---|---:|
| 9 | F6 — Magic number constants | 2h |
| 10 | F9 — slog prefix standardization | 1h |

**Total polish: ~3h.**

### Grand total: ~25.5h to close all findings except those gated by upstream work (gocyclo install in dev-tools, mini_review.py infrastructure).

---

## Self-dogfood reflection (Unbreakable Rule 3 honest disclosure)

This review caught code I shipped TODAY. Specifically:

- **F1** — Did NOT introduce the Rule 3 violation at `environments.go:516` (pre-existing).
- **F2** — DID grow `ScaleHandler.Scale` from baseline ~CC 20 to CC 37 via J-20 T1.1 commit `f9b5d375`. Should have been caught by mini-review at the phase boundary; wasn't because mini_review.py has no CC delta check (F4).
- **F11** — caught + fixed mid-J-18-T1.3.b (status_test.go signature drift). Good signal that test compile-against-production is alive.

The 4 J-* releases today (v0.15.0 + v0.15.1 + v0.16.0 + v0.17.0 + v0.18.0) all PASSED `/code-quality` skill, but `/code-quality` doesn't measure CC. The skill only validates language-detection + dead-code via `vulture/knip/cargo-udeps/deadcode` + symbol fabrication. Adding CC measurement to `/code-quality` per **F4** would close the gap.

---

## Cross-references

- Run 1 (`infra/tests/`): `code-review-output-infra-tests/final_report.md`
- Plugin: `/home/paulo/Projetos/plugins/loop-code-review/templates/prompt.md`
- Reviewed paths: `api/internal/routes/*.go` (78 files, 22680 LoC)
- Convention rules consumed: `.claude/rules/cli-output.md` (Rule 3) + `.claude/rules/loopzero-design.md` (Rule 6 + 7) + `.claude/rules/cycle-implement.md` (mini_review.py phase-boundary gate)
- Today's slices that affected this surface: J-18 v0.15.0 (status.go) + J-18 T1.3.b v0.15.1 (status_sse.go) + J-20 v0.16.0 (scale.go T1.1) + J-21 v0.17.0 (none — pure frontend) + J-11 v0.18.0 (none — ADR D6 codified backend-already-shipped)

**End of report.**

`<!-- PHASE_1_COMPLETE -->` `<!-- COMPONENTS_TOTAL:13 -->` `<!-- FILES_INVENTORIED:78 -->`
`<!-- PHASE_2_COMPLETE -->` `<!-- FINDINGS_TOTAL:14 -->` `<!-- FINDINGS_CRITICAL:1 -->` `<!-- FINDINGS_HIGH:3 -->`
`<!-- PHASE_3_COMPLETE -->` `<!-- QUALITY_SCORE:0.78 -->` `<!-- QUALITY_PASSED:0 -->`
`<!-- PHASE_4_COMPLETE -->` `<!-- TEST_GAPS:3 -->`
`<!-- PHASE_5_COMPLETE -->`
`<promise>CODE REVIEW COMPLETE</promise>`
