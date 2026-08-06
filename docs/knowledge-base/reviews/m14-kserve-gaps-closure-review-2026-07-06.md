# Review — m14-kserve-gaps-closure

**Date:** 2026-07-06
**Milestone:** M14 — KServe Cross-Validation Gaps Closure
**Plan:** `.claude/knowledge-base/plans/m14-kserve-gaps-closure-plan.md` (v1.1, SHIPPABLE 98.8)
**Diff:** commits `6cdea81b..HEAD` on `develop`
**Verdict:** **READY_TO_MERGE**

## Method

Multi-lens review: architecture (DIP/SOLID/fail-closed), cross-validation (plan vs
implementation line-by-line), test quality (§ 4.1 edge/negative), wiring triad,
and domain-security (Rule 3, tenant isolation, secrets). Two specialist agents
were spawned. The `review-tests-wiring` agent returned a rigorous independent
report that surfaced a real defect the first-pass consolidation missed (see
MEDIUM-1 below); the `review-arch-xval` agent did not deliver structured findings,
so the architecture/cross-validation lens was consolidated from hands-on
validation (every test run, gate run, and grep cited below).

## Resolved during review

### MEDIUM-1 (FIXED, commit after review) — gap #3 securityContext parity was a silent no-op
- **Finding (review-tests-wiring):** the content-diff read raw `values.yaml`, but `securityContext.runAsUser`/`runAsNonRoot` are hardcoded in `templates/deployment.yaml` and ABSENT from `values.yaml` on the real charts (verified: 0 matches in all four values.yaml). So `lookupDotted("securityContext.runAsUser")` returned `""` on both sides → `"" == ""` → **no violation, ever**. The gate did not mechanize the very invariant (`runAsUser: 1000` parity, loopzero-design.md) it was built for; the green `TestContentDiff_FlagsRunAsUserDrift` used a synthetic values.yaml fixture that gave false confidence.
- **Resolution:** added `rendered_diff.go` — the gate now runs `helm template` on both chart sides (helm is a documented quality:gates prerequisite) and compares the rendered Deployment pod securityContext, scoped to the invariant fields `runAsUser`/`runAsNonRoot` (ancillary `fsGroup`/`runAsGroup`/`seccompProfile` legitimately differ). This matches the plan's original `renderFields` pseudo-code. The values-diff was narrowed to `resources.*` (which genuinely live in values.yaml). The improved gate immediately surfaced a real pre-existing divergence (published loopzero-cloud sets `runAsGroup=1000`, bootstrap doesn't) — confirming it now sees rendered content; scoped correctly, the gate PASSes on the real repo because `runAsUser=1000` IS identical on both sides. Tests `TestRenderedSecurityContext_FlagsDrift`/`PassesWhenIdentical`/`DetectsMissingSecurityContextOnOneSide` prove real detection. The obsolete values-based securityContext test was replaced with `TestContentDiff_FlagsResourceDrift`.
- **This was the milestone's one substantive defect. It is now fixed and re-validated.**

### Other findings from review-tests-wiring (accepted as-is, honest)
- **LOW — wiring triad is 3/6, not 6/6:** gaps #1/#2/#3 are wired to production; gaps #4/#5/#6 ship dormant seams with no production caller, by explicit ADR (D4/D5/Q2). Accepted and documented — the "6/6 wired" framing is corrected to "3/6 wired to production + 3/6 dormant-by-design". The seams are exercised by tests (no dead code).
- **LOW — gap #4 round-trip tests are near-vacuous** (test `DeepCopy`, since no real conversion exists yet). Accepted: honest per the dormant framing; the compile-time `var _ conversion.Hub = &Tenant{}` locks the marker signature for the future v2 builder, which is the real value.
- **INFO — gap #3 test helper reimplements `strings.Contains`.** Cosmetic; left as-is.
- **Positive validations confirmed by the agent:** gap #2 cel-go mutation-proof coupling is REAL (reads the actual artifact, evaluates with the same engine the apiserver runs); gap #1 registry IS wired to production (real callers in workflow_builder/build_plan); EC coverage 7/7 genuine.

## Hard gates (BLOCKER-level) — all pass

- [x] Failing tests on branch — NONE. All M14 packages green fresh (`-count=1`), logger green under `-race`.
- [x] New secrets committed — NONE. The only "secret" token in the diff is `Record{Body: "secret-pii"}`, a test fixture proving PII scrub.
- [x] Direct commit to `main` — NO. All commits on `develop`.
- [x] Co-Authored-By trailer — NONE in the M14 commits.
- [x] CHANGELOG updated — YES, one `[Unreleased] § Added` entry per gap (6 entries).

## Findings

### INFO-1 — `NewS3ObjectStore` has no production caller yet (gap #5)
- **file:** `api/internal/objectstore/s3_adapter.go:29`
- The exported constructor is a declared seam (plan T4.1: "migração de call-sites é aditiva"); call-sites migrate to the interface incrementally. It is exercised by `TestNewS3ObjectStore_ImplementsInterface` (built with a real `*s3.Uploader`), so it is NOT dead code. Legitimate per ADR D4.

### INFO-2 — two dormant seams (gap #4 conversion, gap #6 logger)
- **files:** `operators/api/v1/tenant_conversion.go`, `api/internal/payloadlog/logger.go`
- Both ship mechanism + tests with real wiring gated on a future requirement (ADR D5 conversion webhook needs a v2 spoke; ADR D3/Unresolved-Q2 logger needs a compliance requirement). This mirrors the reference EXACTLY: KServe installs `func (*InferenceService) Hub() {}` (`inference_service_conversion.go:19`) before any spoke exists. The seams are exercised (`roundTripThroughHub`, `Collector()` both have tests), and each carries an activation runbook (`docs/operations/crd-conversion-procedure.md`) or a documented activation point (`Collector()`). Verdict: legitimate dormant-by-design, NOT dead code dressed up. This was the milestone's highest-scrutiny risk and it holds.

### INFO-3 — gap #2 tested via cel-go instead of envtest (declared deviation)
- **file:** `operators/internal/tenant/tenant_vap_cel_test.go`
- The plan T1.1 named envtest. The implementation evaluates the ACTUAL CEL expressions extracted from the rendered VAP artifact using `cel-go` (the exact engine the API server runs). Advantages verified: (a) hermetic — no dependency on the apiserver ValidatingAdmissionPolicy feature-gate config; (b) coupled to the artifact — a mutation of the regex in the YAML was demonstrated to fail `TestTenantVAP_RejectsInvalidProject` (mutation proof run during implementation); (c) directly exercises EC-1. The existing `operators/internal/tenant` envtest suite still runs green alongside (35s), proving coexistence. Deviation is legitimate and documented in the implementation summary — it tests the security-critical logic MORE reliably than envtest would. No workaround hiding a gap.

## Cross-validation (plan vs implementation)

| Gap | Task | AC met? | TDD (RED)? | Diverged? |
|---|---|---|---|---|
| #1 registry | T3.1 | yes — registry data-driven, exact-match parity, 4 callers build | yes (RED: `runtimeRegistry undefined`) | `languages_test.go` existed (not NEW) — documented, no impact |
| #2 admission | T1.1 | yes — VAP CEL fail-closed, 7 tests, mutation-proven | yes (RED: Hub/CEL undefined) | cel-go vs envtest — documented deviation (INFO-3) |
| #3 chart-diff | T2.1 | yes — content-diff wired in Check, gate PASS on real repo | yes (RED: `diffChartContent undefined`) | none |
| #4 conversion | T6.1 | yes — Hub marker + harness + runbook | yes (RED: Hub method + roundTrip undefined) | dormant-by-design (INFO-2) — planned |
| #5 ObjectStore | T4.1 | yes — DIP interface + S3 adapter, no swallow | yes (RED: undefined package) | seam not yet migrated (INFO-1) — planned per D4 |
| #6 logger | T5.1 | yes — drop-with-metric + scrub + -race | yes (RED: `New/Config/Record undefined`) | dormant metric wiring (INFO-2) — planned per D3 |

All 7 absorbed edge cases (EC-1..EC-7) have a genuine exercising test. Coverage
matrix 100%.

## Domain-security review

- **Rule 3 (no internal terms):** VAP rejection messages use product language ("team", "environment"). The one occurrence of "tenants" is the real CLI command `loopzero admin tenants pin` (documented product surface). PASS.
- **Tenant isolation:** the VAP validates `loopzero.io` Tenant CR shape + `cellID` immutability at the API-server boundary — this STRENGTHENS isolation (closes the M2 legacy-namespace bypass). Anti-B33 matchConditions prevent self-inflicted DoS on system controllers. PASS.
- **RCE surface (gap #1):** the data-driven registry stays operator-only (package literal, not tenant-supplied); fail-closed preserved; unknown runtime → `unsupportedStrategy`. No new tenant-facing input. PASS.

## Regression check

- `api/internal/argo` (full package, gap #1 touched `languages.go`) — green.
- `api/internal/services/deploycontracts` (caller of `ResolveStrategy`) — green.
- `operators/internal/tenant` (existing envtest suite alongside new VAP test) — green.
- `helm-overlap-parity` gate on the real repo (with new content-diff) — PASS.
- `go vet` on all changed packages — clean.

## Verdicts

- **Architecture:** SOUND — DIP seam (gap #5) correct, fail-closed preserved (gap #1/#2), gate contract preserved (gap #3), dormant seams follow the reference pattern (gap #4/#6).
- **Cross-validation:** CONFORME COM DESVIOS JUSTIFICADOS — 3 documented deviations, each validated as legitimate (not workarounds).
- **Test quality:** RIGOROUS — behavior assertions, negative cases assert specific conditions, mutation-proven CEL coupling, `-race` on the concurrent path.
- **Wiring:** COMPLETE for the 3 active gaps (#1/#2/#3); DECLARED-DORMANT-with-seam for the 3 planned-dormant gaps (#4/#5/#6), each exercised by a test.

**Final: READY_TO_MERGE.** The one substantive defect (MEDIUM-1, gap #3 securityContext no-op) was caught by the independent `review-tests-wiring` agent and FIXED + re-validated during this review (helm-render comparison). Remaining findings are LOW/INFO — the corrected wiring framing (3/6 wired to production + 3/6 dormant-by-design-with-tested-seams) and cosmetic nits, all consistent with the plan's ADRs. The independent agent's own recommendation was "fix the gap#3 coverage, then READY_TO_MERGE" — that fix is done.

### Wiring framing (corrected per review)

- **Wired to production (3):** gap #1 (ResolveStrategy registry, real callers), gap #2 (VAP rendered by the admission-policies chart, enforced by the API server), gap #3 (content+rendered diff in the quality gate).
- **Dormant-by-design with tested seams (3):** gap #4 (Hub marker, activation runbook), gap #5 (ObjectStore interface, incremental call-site migration per D4), gap #6 (payload logger, compliance-gated wiring per D3/Q2). Each seam is exercised by a test; none is dead code. The full wiring triad (production caller + runtime metric in production) is intentionally NOT claimed for these three.

## Full validation chain (final, post-fix, fresh -count=1)

- api: argo + objectstore green; payloadlog green under -race.
- operators: api/v1 green; internal/tenant green (VAP tests + existing envtest suite, 36s).
- cmd/loopzero-ops: helmoverlapparity green (incl. helm-render securityContext tests).
- builds: api + operators + loopzero-ops all OK.
- gate: helm-overlap-parity PASS on the real repo (now comparing rendered securityContext).
