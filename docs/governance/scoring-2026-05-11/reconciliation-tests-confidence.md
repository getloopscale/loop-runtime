# Reconciliation Meeting — Tests Confidence

**Date:** 2026-05-11
**Trigger:** std-dev = 0.551 > 0.3 in T3.2 triangulation
**Plan task:** T3.3 of `docs/plans/status-validation-and-doc-consolidation-plan.md`
**Status:** AWAITING OPERATOR — auto-generated from T3.1 LIVE run

## Pre-Meeting Inputs

| Persona | Model | Score |
|---------|-------|------:|
| P1 Vercel DD | claude-opus-4-7 | 2.0 |
| P2 Hostile VC | claude-sonnet-4-6 | 2.9 |
| P3 Paranoid SRE | claude-haiku-4-5 | 3.0 |
| **Median** | — | **2.9** |
| **Std-dev** | — | **0.551** |

**Spread:** 1.0

## Persona Justifications (raw bullets)

### P1 Vercel DD

- 0/348 EVIDENCED, 92.5% UNEVIDENCED, 11 BROKEN per `evidence-gaps-report.md` headline distribution. Of 48 tests-confidence claims, only ONE evidence file exists (`docs/evidence/tests-confidence/2026-05-11-validate-evidence-script.md`) and even that has T3 = 'pending first scheduled Sunday run' — a placeholder. The validation system itself has not yet validated anything in CI.
- Per `deep-dive-current-state.md` §'Event 2: Maturity Score Drift +0.4 in 30 min' — same agent, same session moved the score from 4.1-4.3 down to 3.7-3.8 under challenge. This is the test-confidence equivalent of flaky tests: results depend on who's asking. Tier 1 means reproducible numbers across blind runs; we just demonstrated empirically that doesn't hold.
- `code-audit-2026-05-11.md` admits 6 of 9 audit techniques deferred (mutation testing, coverage measurement, SCA, secrets scan, cyclomatic per-fn, integration test quality). The lightweight audit that DID run only covers governance scripts (2324 LOC) — not the 41 Go packages + 1210 CLI tests + 200 envtest specs that the README brags about. We're scoring confidence on confidence-scoring tooling, not on the product.

### P2 Hostile VC

- Auto-penalty triggered: test counts cited without coverage % (-0.3). '1210+ CLI tests' '41 Go packages' '200+ envtest specs' '310+ Pulumi vitest specs' — zero coverage numbers. deep-dive-current-state.md confirms this is a known bias pattern observed empirically in this codebase. I need: (a) % of lines covered in api/internal/services/, (b) % of operator reconcile paths covered by envtest, (c) branch coverage on state machine transitions.
- evidence-gaps-report.md §tests-confidence: 46 UNEVIDENCED + 2 BROKEN. The '12/12 GREEN' cleanroom tag (dogfood-cleanroom-converged-20260505) appears in 2 BROKEN claims because commit_missing:20260505. The flagship dogfood evidence is a broken reference. If I cannot verify the git tag exists, the 12/12 number is unverifiable. Show me: git tag -v dogfood-cleanroom-converged-20260505 with the signed output.
- code-audit-2026-05-11.md explicitly lists 6 audit techniques deferred: mutation testing, coverage measurement, cyclomatic complexity per function, SCA, secrets scan, integration test quality. These are not optional extras — they are the techniques that catch production bugs. A platform positioning itself as FAANG-grade with mutation testing explicitly deferred is claiming a bar it has not measured against. 'We planned to measure it' is not measurement.

### P3 Paranoid SRE

- 340 known-failing/skip tests across repo (grep of skip/xfail/Ignore pattern yields 340 hits). CLAUDE.md 'Known issues' section lists AppSecret unwired, local k3d letsencrypt issue, HTTP 000 probes. But 10+ tests in api/internal/k8s are explicitly failing — no incident link, no issue ticket. Tests.Confidence -0.4 per paranoid-SRE rules. That's 70 basis points of trust deficit right there.
- Zero mutation testing executed. Coverage metrics exist (some files report 60-80%) but coverage ≠ resilience. A test can achieve 100% coverage and still miss the failure case that happens 1 in 10k times in production. No mutation testing framework (go-mutator, stryker equivalent) has ever shown 'what % of production bugs would mutation testing have caught?' Until that's measured, confidence is aspirational.
- Chaos tests exist (9 manifests in infra/tests/chaos/, chaos_test.go with 387 LOC) but none have ever successfully executed end-to-end on live infrastructure. chaos_test.go references 'FIXME: IOChaos fails on k3d without fuse kernel support' — meaning the tests were written, discovered not to work, then abandoned. Tests that don't run are negative confidence signals. Fix or delete.

## Arbiter Discussion (TO BE COMPLETED BY OPERATOR)

**Why the delta?**

_[Manual analysis: which persona is over/under-scoring? Different criteria? Different evidence weighting?]_

### Arbiter Questions

1. Are P1/P2/P3 looking at the same evidence?
2. Is the delta semantic (different lens) or factual (one persona has wrong data)?
3. What concrete artifact would shift the lowest-scoring persona up, or vice versa?

### Convergence Path

_[Document the final agreed score or note dissent]_

## Final Decision (TO BE COMPLETED)

| Field | Value |
|-------|------:|
| **Reconciled score** | _N.N_ |
| **Decision method** | _consensus / arbiter-decided / dissent-recorded_ |
| **Dissenting opinions** | _if any, document them_ |

## Action Items

- [ ] Update `docs/STATUS.md` §2 row `tests_confidence` with reconciled score
- [ ] Update `docs/governance/scoring-2026-05-11/scorecard-tests-confidence.md` with final
- [ ] Document open work items if dim has actionable gaps

## Cross-References

- T3.2 output: `triangulation.yaml`
- T3.4 scorecard: `scorecard-tests-confidence.md`
- Per-persona files: `p1-vercel-dd.yaml`, `p2-hostile-vc.yaml`, `p3-paranoid-sre.yaml`
- Plan ADR D4 (adversarial personas) + ADR-0051 (canonical contract)
