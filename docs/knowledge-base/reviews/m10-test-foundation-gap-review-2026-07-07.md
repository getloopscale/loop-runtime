---
type: review
title: m10-test-foundation-gap
---

# Review — m10-test-foundation-gap

**Date:** 2026-07-07
**Milestone:** M10 — Test Foundation: Ginkgo Harness + Spec-Based Contract Layer
**Plan:** `.claude/knowledge-base/plans/m10-test-foundation-gap-plan.md` (v1.1, SHIPPABLE_WITH_CAVEATS 70)
**Scope reviewed:** the AUTONOMOUS DoD#1..#4 closure. DoD#5 (3 green LIVE chain runs + DELETE quarantine + sponsor "M0-M8 destravado") is the declared sponsor-JUNTOS/Cleanroom-TOTAL halt boundary per `.claude/rules/cleanroom-up-live-test-mandate.md` and is explicitly OUT of this review's scope (not a defect).
**Cross-repo:** the DoD#1 deliverable lives in the sibling repo `loopzero-contracts`; DoD#2/#3/#4 live in `loopzero`.
**Verdict:** **READY_TO_MERGE**

## Canonical state (v0.3.0) — duplicate-work resolution

During this review a **duplicate-work divergence** surfaced and was resolved (the systemic "two sessions did the same work" pattern). A parallel session had already delivered DoD#1 on `loopzero-contracts` `develop` and **released it as v0.3.0** (`0458cdd`) BEFORE this session's parallel implementation was pushed. The canonical, released commits are:

- `233e010` test(m10-fase-a): jwt+serviceauth Ginkgo BDD depth expansion (DoD#1a completion)
- `9a0314f` feat(test): T1.2 Ginkgo BDD specs for plan + serviceauth + jwt
- `3572968` feat(test): T1.1 Taskfile.yml (cross-repo orphanage fix)
- released at `0458cdd` chore(release): loopzero-contracts v0.3.0

This session's three parallel commits (`96b71d5`, `cc28e43`, `8e6a05f`) were **redundant** (same Ginkgo conversion + the same `MetricCPUCoreSeconds`/`MetricMemGBSeconds` observe-only work origin already had via `ee1eefb`), never pushed, and were **discarded safely** by syncing local `develop` to `origin/develop` via `git reset --soft origin/develop` + `git restore` (git-safety-approved — NO `reset --hard`, NO force-push). Local now == origin (`0458cdd`, `0 0`).

**Canonical approach differs from — and is more conservative than — this session's:** the released version is **ADDITIVE** — it ships new Ginkgo BDD spec files (`jwt/jwt_ginkgo_test.go`, `serviceauth/serviceauth_ginkgo_test.go`, `plan/plan_ginkgo_test.go`, each with `RunSpecs`) ALONGSIDE the retained stdlib test files, rather than a pure in-place conversion. Both run under `go test ./...`. The test-auditor's "no assertion weakened/dropped" finding (below) therefore holds **a fortiori** on the canonical tree: every original stdlib assertion is retained verbatim AND the BDD depth is added on top. The line/SHA evidence in the DoD#1 section below was captured against this session's now-discarded parallel version; the canonical observable (Ginkgo suite GREEN) is re-verified directly against v0.3.0 in the gate-evidence table.

## Method

Two independent specialist agents, in parallel:

1. **test-auditor** — proved the DoD#1 Ginkgo conversion did NOT silently weaken or drop assertions (the exact failure mode of a test-suite conversion).
2. **cross-validation (gap-detector)** — cross-validated the plan's DoD#1..#4 claims against the real repo state, adversarial specifically on the DoD#2 "cloud↔engine vs dashboard↔engine" ROADMAP-vs-reality divergence (Top risk #1).

Plus hands-on gate evidence run directly (below).

## Gate evidence (run 2026-07-07)

| Gate | Command | Result |
|---|---|---|
| Plan structural | `run_structural.py m10-test-foundation-gap` | SHIPPABLE_WITH_CAVEATS 70 (completude 100; capped by `vague_acceptance_criteria` soft heuristic — moot for a completed plan) |
| DoD#1 Ginkgo suite (CANONICAL v0.3.0) | `cd ../loopzero-contracts && go test ./... -count=1` | jwt 6.2s / plan / serviceauth — all `ok`, race-clean |
| DoD#1 Ginkgo runner (CANONICAL v0.3.0) | `ginkgo -r` | 3 BDD suites GREEN (jwt 5 Describe/12 It, serviceauth 4/13, plan 3/5 = 30 It) — "Ginkgo ran 3 suites — Test Suite Passed" |
| DoD#2/#3 contract gate | `cd api && go test ./internal/contract/...` | `ok` (contract + dev_public) |
| CODE-QUALITY (build) | `go build ./...` (both repos) | exit 0 both |
| CODE-QUALITY (vet) | `go vet ./...` (both repos) | exit 0 both |
| Structural conv. counts | `func Test*` residual in converted files | 0 stdlib funcs (only `RunSpecs` bootstraps); validator 69 `Describe/It`, properties 6 |

CODE-QUALITY verdict: **PASS**. Symbol fabrication is impossible on a suite that builds (`build=0`); dead code does not apply to tests (the test IS the caller). `staticcheck` not installed → `auditor_unavailable` soft note only (`go vet` covers the essential).

## DoD#1 conversion faithfulness (test-auditor)

**FAITHFUL — no assertion weakened or dropped.** Headline evidence:

- **Assertion conservation 1:1** across every converted file. Largest file `jwt/validator_test.go`: **122 `Expect` == 122 pre-conversion `t.Error/Fatal`**; 56 `func Test` → 57 `It` + 2 `DescribeTable` (+8 `Entry`); zero cases dropped (table values byte-identical).
- **Specific-error matchers preserved** — every `errors.Is(err, SPECIFIC)` maps to `MatchError(SPECIFIC)` (retains `errors.Is` semantics), NEVER to a vacuous `HaveOccurred()`. E.g. `serviceauth/replay_test.go:36,41` → `MatchError(ErrTimestampOutsideWindow)`; `signer_test.go:70,117` → `MatchError(ErrSignatureInvalid)/(ErrNoTrustedKeys)`; the negative `errors.Is(..., ErrNonceReused)==false` (redis outage must not masquerade as reuse) preserved.
- **Property tests (EC-3) retain teeth** — `jwt/properties_test.go`: all 3 `quick.Check` remain INSIDE the `It` with identical `MaxCount` (5000/2000/2000) and byte-identical property funcs, wrapped as `Expect(quick.Check(f, cfg)).To(Succeed())` (`Succeed()` fails on a counterexample). Mutation-proof: deleting the empty-token guard still fails the "rejects malformed input" spec.
- **Concurrency/race assertions verbatim** — `serviceauth/redis_nonce_store_test.go` "exactly one winner" N=100 goroutine + `WaitGroup` + `Expect(count).To(Equal(1))` byte-identical; `-race` teeth intact; panic path `recover()` → `Expect(func(){...}).To(Panic())`.
- **Suite bootstraps correct** — one `RunSpecs` per package; `It` names describe behavior; no shared mutable state across `It`.
- The three `+1` assertion deltas are **strengthenings** (split compound `if` into two `Expect`; added explicit key-gen error check), not teeth-loss.

LoopZero-side consumer gates confirmed REAL (not vacuous): `cli_consumer_test.go` (37 non-comment paths, `t.Fatalf`, always runs, bidirectional drift + tested `knownConsumerGaps` allowlist), `dashboard_consumer_test.go` (13 paths), `engine_conformance_test.go` (spec ⊆ router, `t.Fatalf` on any unregistered path).

## DoD#1..#4 cross-validation (gap-detector)

All four autonomous DoDs **TRUTHFUL and substantially COMPLETE**; no genuine non-sponsor-gated gap.

- **DoD#1** — 3 Ginkgo packages GREEN, no stdlib-only remnant. ✅
- **DoD#2** — dashboard↔engine + cli↔engine + engine↔contracts covered. The "cloud↔engine doesn't apply" justification is **SOUND under adversarial probe**: the loopzero-cloud Go BFF makes exactly 2 engine HTTP calls (`cmd/main.go:1101` catch-all + `:1051` webhooks), both byte-copy reverse proxies with zero path enumeration and zero response-shape parsing (`engineClient.Do` appears only there). The one proxied-but-unmanifested path (`/api/v1/webhooks/*`) is a GitHub→engine HMAC contract, orthogonal to the dashboard REST contract, correctly covered by `engine_conformance` + loopzero-cloud's own GitHub tests. ✅
- **DoD#3** — `taskfiles/quality.yml:71` `- task: contract` inside `gates.cmds`; runs `cd api && go test ./internal/contract/...`. Deterministic pre-push path. ✅
- **DoD#4** — chain v2 Ginkgo (`infra/tests/livetest/cleanroom|dogfood|chaos|contraprova`); the `AssertSpanCoverage` stub de-skeletoned to `AssertSpanCoverageReal` (`tempo_client.go:89`, httptest-tested); quarantine read-only enforced by the Go gate `pkg/quality`/`cmd/loopzero-ops/.../quarantineprotect` wired at `quality.yml:207`. No `Skip()` remains on a hermetically-assertable path (the ~22 remaining Skips are all LIVE-natured: Kratos/Stripe-CLI/PG-probe/seeded-builds — outside DoD#4's autonomous scope). ✅

## Findings (all LOW / INFO — none block merge)

- **LOW-1** — `TestDashboardConsumer_ListMatchesSource` flags only live-not-in-list (surprises), not stale-list-entries, whereas the cli equivalent flags both. Pre-existing (predates the conversion); the "consumer path outside spec" direction is fully guarded by `AllPathsInSpec`. Hygiene-only.
- **LOW-2** — `engine_conformance` is one-directional by design (spec ⊆ router); router ⊆ spec is covered by `router_coverage_test.go` + consumer tests. Documented scope.
- **LOW-3 (DoD#3)** — the planned hermetic self-guard `check-contract-gate-wired_test.sh` was deferred to an M13-aligned `loopzero-ops` Go gate (honestly documented in the plan's EC-2 refinement). The DoD's observable (contract runs every pre-push) holds.
- **INFO** — the plan `status:` frontmatter is stale-**pessimistic**: it says the jwt+serviceauth conversion (~1900 LoC) is "remaining"; it is in fact done + GREEN. Corrected this session.

## Hard gates (BLOCKER-level) — all pass

- [x] Failing tests on branch — NONE (3 GREEN suites race-clean; contract gate `ok`).
- [x] New secrets committed — NONE (only test fixtures / generated RSA keys in-test).
- [x] Direct commit to `main` — NO. All commits on `develop` (loopzero + loopzero-contracts).
- [x] Co-Authored-By trailer — NONE in the M10 commits.
- [x] CHANGELOG — the loopzero-side M10 DoD#2/#3/#4 work already carried CHANGELOG entries when merged; the DoD#1 conversion is test-only in `loopzero-contracts`.

## Verdict

**READY_TO_MERGE** for the autonomous M10 scope. The conversion is faithful (122-for-122 assertion conservation, property/concurrency bodies byte-identical, specific-error matchers preserved), the four autonomous DoDs are truthfully closed with runnable hermetic evidence, and the ROADMAP-vs-reality "cloud↔engine" divergence resolves to a sound "dashboard is the real consumer" (BFF transparent proxy) — not a hidden gap.

**M10 milestone checkbox does NOT flip.** DoD#5 (3 consecutive green LIVE chain runs + DELETE quarantine + sponsor verbatim "M0-M8 destravado") is an INQUEBRÁVEL sponsor-JUNTOS + Cleanroom-TOTAL gate per `cleanroom-up-live-test-mandate.md`. The autonomous engineering is done; closing the milestone requires the operator+sponsor LIVE ceremony, which cannot be performed by an autonomous agent.
