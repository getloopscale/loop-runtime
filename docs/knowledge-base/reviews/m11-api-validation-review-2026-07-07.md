# Review — M11 API Validation (spec 100% + fuzzer spike + Fase-A migration)

**Date:** 2026-07-07
**Milestone:** M11 — API Validation: complete spec (73%→100%) + property-based fuzzing
**Plans:** `m11-api-validation-plan.md` v1.0 (autonomous core) + `m11-fase-a-migrate-de-api-scripts-to-go-plan.md` v1.1 (migration)
**ADR:** `.claude/knowledge-base/decisions/2026-07-05-m11-fuzzer-choice.md` (ACCEPTED — schemathesis LIVE gate)
**Verdict:** **READY_TO_MERGE** for the autonomous scope (DoD#1 + DoD#2-decision + DoD#3 migration) — with mandatory caveats. DoD#2 fuzzing RUN + DoD#5 DELETE + behavioral parity = sponsor/LIVE halt boundary.

## DoD status at review time

| # | DoD item | Status | Evidence |
|---|---|---|---|
| 1 | engine.yaml 100% router coverage + spec-vs-router parity gate (no orphan route) | **DONE + GREEN** | `go test ./internal/contract/ -run TestEngineConformance` → both `SpecPathsAreRegistered` (spec⊆router) AND `RouterRoutesAreDocumented` (router⊆spec) PASS; 53 /api/v1 paths, bijective |
| 2 | Fuzzer spike concluído + decisão (ADR) + fuzzer wired num runner determinístico c/ JUnit | **DECISION DONE (ADR ACCEPTED); RUN LIVE-gated** | ADR `2026-07-05-m11-fuzzer-choice`: schemathesis chosen as a LIVE/operator gate (needs a running engine — cannot fuzz hermetically); the RUN is the halt boundary. **Independent spike disagreement — see below.** |
| 3 | ≥50% dos dev-public-*.sh de asserção-de-API migrados p/ Go integration tests no harness M10 | **DONE (structural) + GREEN** | `api/internal/contract/dev_public/` 7 files / 58 Test* / 85 subtests, `-race -count=1` → ok 1.8s. Inventory: 54 scripts = 79.4% of 68 addressable → MEETS ≥50% |
| 4 | Falha vira diff estruturado (não exit 1); roda headless sem infra LIVE | **DONE** | testify `require.Equal/Contains` → structured diffs; hermetic httptest, zero kubectl/LIVE egress, zero Skip on hermetic paths |
| 5 | DELETE bash substituídos + syntax-tests + aggregators + allowlist entries | **SPONSOR/LIVE-gated (halt boundary)** | Deleting LIVE regression protectors requires LIVE behavioral parity (Class B) per `cleanroom-up-live-test-mandate.md` + `feedback_issue_fix_requires_failing_e2e_test`. 79 `.sh` remain on disk by design |

## Fase-A migration review (test-auditor, adversarial)

**Verdict: READY_TO_MERGE for the structural Fase-A scope — with mandatory caveats.** `-race -count=1` GREEN.

### The governing property (HONEST — must never be misread)

**H1 — 100% self-mock.** Every one of the 85 subtests asserts against a mock HTTP handler authored in the same test file; ZERO import of production `api/internal/routes`/handlers (grep-confirmed). **A regression in the real `builds.go`/`scale.go`/`promote` handler cannot fail any of these tests.** The suite is (a) hermetic HTTP plumbing + (b) executable contract-shape documentation — **NOT** production-regression protection. The plan **discloses and accepts this** (Risk lines 155/159; DoD success metric line 449 = "54 top-level Test* declared" = structural by design; behavioral parity is explicitly Class B / sponsor JUNTOS). **Guard: "58 green contract tests" ≠ "58 production-verified endpoints."**

### Genuine positives (not vacuous)
- Hermetic/headless (DoD#4) fully met — no kubectl, no `os/exec`, no LIVE egress, no Skip; `-race` clean 1.8s.
- Structured-diff-on-failure (DoD#4 literal) via testify — no bare booleans, no custom differ.
- **Negative-path specificity NOT broadly weakened** (the #1 migration risk): typed-error assertions check specific code AND message — `400 MISSING_FIELD`+`.loopzero/services.json`, `402 PLAN_UPGRADE_REQUIRED`, `400 HPA_TARGET_OUT_OF_BOUNDS`, `404 PROJECT_NOT_FOUND`, `401`+`application/json`, `429`+`Retry-After`.
- EC-2 concurrency `sync.WaitGroup` barriers present; no shared mutable state across subtests.
- Coverage ≥54 met (58 Test funcs; 12/12 sampled source scripts map to a Go test).

### Tracked caveats (NOT merge-blocking; fix before Class B inherits them)
- **M2** — ADR D2's `assertConformsToSpec` (validate response vs real engine.yaml) is called only twice (both in the /health smoke); ZERO migrated subtests use it. The one mechanism tying a test to a real artifact is unused for the migration.
- **M3** — `#158` PromoteEnsureEnv comment claims "proof the composition-root wiring is present" but asserts a counter the mock increments itself — an overstated (evidence-fabrication-adjacent) comment. Down-scope the comment.
- **M4** — `#107` timeout test sleeps 100ms against a no-timeout httptest server → cannot fail for the reason the bug existed (Traefik 60s writeTimeout). Vacuous vs its named bug; comment concedes it's a "proxy for the LIVE scenario".
- **M5** — `#64` large-header test builds an `http.Server{MaxHeaderBytes:1<<20}` then discards it (`_ = srv`) and serves via 1MB-default httptest; 20KB header ≪ 1MB, assertion trivially true; the 431 boundary is never approached. Remove dead server or approach the boundary.
- **L1** — EC-3 in-memory RSA key generated but never used to sign (mint uses HS256 hardcoded string); RSA key is effectively dead.
- **L2** — register-name-maxlength: boundary-valid 256-char case documented but only the negative 257 is exercised (`rules/testing.md §4.1` wants both).
- **L3** — dead `_ = require` (shadowed testify) + dead `_ = srv`; vet-clean but sloppy.

## DoD#2 — ADR-vs-independent-spike disagreement (SURFACED, not overridden)

An independent empirical spike run THIS session reached a **different conclusion** than the ACCEPTED ADR. Per Rule 3 + the disagreement protocol, this is surfaced for human/sponsor adjudication — the ACCEPTED ADR is NOT unilaterally overridden.

| | Accepted ADR (`2026-07-05`) | Independent spike (2026-07-07) |
|---|---|---|
| Choice | **schemathesis** (Python, via uvx) as a **LIVE/operator gate** | **Go-rapid** (`pgregory.net/rapid`) hermetic, in the M10 Go harness |
| Hermetic? | No — needs a running engine (`--url` mandatory) | Yes — httptest.NewRecorder, 10k cases/154ms, offline |
| Go-native rejection rationale | "handlers need full Config → not hermetic in practice" | Refined: you CAN hermetically fuzz the **validation logic** (rfc1123 name contract) below the handler — demonstrated: found + shrank a planted bug to minimal counterexample `"ABC"` |
| Toolchain cost | +Python (39 pip pkgs / 72MB); Python isolated to loopzero-sdk today | Zero — 1 Go module, zero transitive deps, "no tool-sprawl" (CLAUDE.md) |
| Coverage | Full-surface, spec-derived (envelope: undocumented status, `additionalProperties`, 405 `Allow`) — but bounded by spec richness (thin spec fuzzes envelope, not `name` semantics) | Deep field-semantics on validators; needs hand-written generators; misses the envelope class |

**Synthesis (recommended to the sponsor):** the two are **complementary at different layers**, not mutually exclusive — Go-rapid hermetic for validation-semantics **pre-push**, schemathesis LIVE for full-surface envelope. The ADR's Go-native rejection conflated "full handler" (needs Config) with "validation logic" (hermetically fuzzable). Adopting Go-rapid as a hermetic pre-push complement is a **governance change to an ACCEPTED ADR** (T2 — requires an ADR amendment + sponsor sign-off), not an autonomous flip. Spike artifacts preserved in scratch (`m11-fuzz-spike/`).

## Hard gates (BLOCKER-level) — all pass

- [x] Failing tests — NONE (dev_public `-race` GREEN; parity gate GREEN).
- [x] New secrets — NONE (in-test HS256 string + generated RSA key only).
- [x] Direct commit to `main` — NO.
- [x] Co-Authored-By — NONE in M11 commits.

## Verdict

**READY_TO_MERGE for the autonomous M11 scope.** DoD#1 (spec 100% + bijective parity gate) is GREEN; DoD#2 the decision is recorded (ADR ACCEPTED, RUN LIVE-gated); DoD#3 the Fase-A migration delivers 58 hermetic race-clean structured-diff Go tests meeting the ≥50% threshold with specific typed-error assertions. The migration is faithful to its **structural** DoD and honestly discloses it is not behavioral-regression protection.

**M11 checkbox does NOT flip.** Three items are sponsor/LIVE-gated per `cleanroom-up-live-test-mandate.md`: (a) DoD#2 the schemathesis fuzzing RUN against a live engine; (b) DoD#5 DELETE of the LIVE-regression `.sh` scripts (needs LIVE behavioral parity — Class B); (c) behavioral parity of the migrated tests (H1). The autonomous engineering is done + evidence-backed; closing M11 requires the operator+sponsor LIVE ceremony.

**Recommended before Class B (not merge-blocking):** fix M3's overstated comment, remove M5's dead server, wire `assertConformsToSpec` into engine-surface subtests (M2), sign-with-or-drop the RSA key (L1); and adjudicate the DoD#2 ADR-vs-spike disagreement (ADR amendment for a Go-rapid hermetic complement).
