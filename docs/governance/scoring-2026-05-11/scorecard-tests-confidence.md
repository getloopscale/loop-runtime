# Scorecard — Tests / Confidence

**Generated:** 2026-05-11
**TTL:** 30 days
**Expires:** 2026-06-10

## Final Score (Triangulated)

| Field | Value |
|-------|------:|
| **Median** | 2.9 |
| **Std-dev** | 0.551 |
| **Flagged (>0.3)?** | True |
| **Final score** | _Pending T3.3 reconciliation (flagged)_ |

## Persona Scores

| Persona | Model | Score | Justification |
|---------|-------|------:|---------------|
| P1 Vercel DD | claude-opus-4-7 | 2.0 | • 0/348 EVIDENCED, 92.5% UNEVIDENCED, 11 BROKEN per `evidence-gaps-report.md` headline distribution. Of 48 tests-confidence claims, only ONE evidence file exists (`docs/evidence/tests-confidence/2026-05-11-validate-evidence-script.md`) and even that has T3 = 'pending first scheduled Sunday run' — a placeholder. The validation system itself has not yet validated anything in CI.<br>• Per `deep-dive-current-state.md` §'Event 2: Maturity Score Drift +0.4 in 30 min' — same agent, same session moved the score from 4.1-4.3 down to 3.7-3.8 under challenge. This is the test-confidence equivalent of flaky tests: results depend on who's asking. Tier 1 means reproducible numbers across blind runs; we just demonstrated empirically that doesn't hold.<br>• `code-audit-2026-05-11.md` admits 6 of 9 audit techniques deferred (mutation testing, coverage measurement, SCA, secrets scan, cyclomatic per-fn, integration test quality). The lightweight audit that DID run only covers governance scripts (2324 LOC) — not the 41 Go packages + 1210 CLI tests + 200 envtest specs that the README brags about. We're scoring confidence on confidence-scoring tooling, not on the product. |
| P2 Hostile VC | claude-sonnet-4-6 | 2.9 | • Auto-penalty triggered: test counts cited without coverage % (-0.3). '1210+ CLI tests' '41 Go packages' '200+ envtest specs' '310+ Pulumi vitest specs' — zero coverage numbers. deep-dive-current-state.md confirms this is a known bias pattern observed empirically in this codebase. I need: (a) % of lines covered in api/internal/services/, (b) % of operator reconcile paths covered by envtest, (c) branch coverage on state machine transitions.<br>• evidence-gaps-report.md §tests-confidence: 46 UNEVIDENCED + 2 BROKEN. The '12/12 GREEN' cleanroom tag (dogfood-cleanroom-converged-20260505) appears in 2 BROKEN claims because commit_missing:20260505. The flagship dogfood evidence is a broken reference. If I cannot verify the git tag exists, the 12/12 number is unverifiable. Show me: git tag -v dogfood-cleanroom-converged-20260505 with the signed output.<br>• code-audit-2026-05-11.md explicitly lists 6 audit techniques deferred: mutation testing, coverage measurement, cyclomatic complexity per function, SCA, secrets scan, integration test quality. These are not optional extras — they are the techniques that catch production bugs. A platform positioning itself as FAANG-grade with mutation testing explicitly deferred is claiming a bar it has not measured against. 'We planned to measure it' is not measurement. |
| P3 Paranoid SRE | claude-haiku-4-5 | 3.0 | • 340 known-failing/skip tests across repo (grep of skip/xfail/Ignore pattern yields 340 hits). CLAUDE.md 'Known issues' section lists AppSecret unwired, local k3d letsencrypt issue, HTTP 000 probes. But 10+ tests in api/internal/k8s are explicitly failing — no incident link, no issue ticket. Tests.Confidence -0.4 per paranoid-SRE rules. That's 70 basis points of trust deficit right there.<br>• Zero mutation testing executed. Coverage metrics exist (some files report 60-80%) but coverage ≠ resilience. A test can achieve 100% coverage and still miss the failure case that happens 1 in 10k times in production. No mutation testing framework (go-mutator, stryker equivalent) has ever shown 'what % of production bugs would mutation testing have caught?' Until that's measured, confidence is aspirational.<br>• Chaos tests exist (9 manifests in infra/tests/chaos/, chaos_test.go with 387 LOC) but none have ever successfully executed end-to-end on live infrastructure. chaos_test.go references 'FIXME: IOChaos fails on k3d without fuse kernel support' — meaning the tests were written, discovered not to work, then abandoned. Tests that don't run are negative confidence signals. Fix or delete. |

## Evidence Cited

Cross-reference with `docs/evidence/tests-confidence/`.

## Open Concerns (synthesis)

- 11 BROKEN evidence refs in headline claims (maturity score, cleanroom 12/12, loopzero scale E2E, cosign pinning) — these are the FOUR proofs a Vercel-tier customer DD would open first, and all four 404. The remediation queue in evidence-gaps-report.md should treat BROKEN refs as P0 blockers; UNEVIDENCED is fixable, BROKEN is actively misleading.
- Zero external user signal compounds with documented +0.4 maturity drift in same agent session (deep-dive §Event 2). Self-attestation by 1M-context agent fails empirically; introducing the 3-persona triangulation pipeline (this scorecard) is the right material defense — but the pipeline itself has 1 proof-of-concept evidence file. Until LIVE dogfood replays under new governance prove a stable score across personas + reviews, the platform is 'demo-tier convincing, DD-tier exposed'.
- Internal terms (AUDIT-27/28 in cli-reference.md:17, ArgoCD/Gitea/CRD acknowledged via CLAUDE.md Rule 3 enforcement) keep leaking into customer-facing surfaces. Bias #6 'Implemented but Not Wired' is self-admitted in CLAUDE.md's own Systemic Patterns table — that auto-awareness without material defense is the exact pattern that ships AppSecret CRDs without controllers. A customer hitting `loopzero secrets rotate` discovers it isn't wired AFTER paying for Pro.
- SYSTEMIC SELF-ATTESTATION: 0 of 348 claims are EVIDENCED per the governance system's own classifier. 92.5% are UNEVIDENCED. The team built a sophisticated evidence framework (claim catalog, decay clocks, bias taxonomy) and then used it to prove that every single claim they make is unverified. This is the most honest form of overclaim I have encountered: the founders know their claims are unevidenced and built a system to track it. The system is not the evidence.
- AGGREGATION BIAS CONFIRMED BY OWN TOOLING: deep-dive-current-state.md §Aggregation Bias documents that the reported 3.9 maturity score is a round-up of the actual arithmetic mean 3.79. The same document records that the authoring agent drifted +0.4 points in a single 30-minute session under self-attestation pressure. This is not a loopzeroretical risk; it is a measured, documented instance of the bias the governance system was designed to prevent.
- BROKEN COMMIT REFERENCES ON KEY CLAIMS: 11 BROKEN claims across 5 dimensions cite commits that cannot be resolved. The maturity score anchor (b04e0905c1), the cleanroom dogfood tag (20260505), and the scale B40 fix (20260506) are all BROKEN. In a $10M due diligence, broken evidence is not a minor documentation issue — it is a data integrity failure that invalidates the claims downstream.
- SPRINT 9 NOT STARTED = PRE-RELEASE PRODUCT: The team's own sprint table marks Sprint 9 (Polish + Launch) as NOT STARTED. The product is pre-release. Yet the pitch language ('shipped,' 'LIVE-validated,' 'production-ready,' 'E2E validated on DO') reads as a launched product. The gap between pitch language and sprint status is the clearest signal of overclaim in this audit.
- SECURITY POSTURE STRUCTURALLY INCOMPLETE: Policy-controller in warn mode, cosign keyless pending Sprint 9, RBAC tightening deferred to ADR-0019/Sprint 9, DO_TOKEN (static credential) replacing OIDC deferred to future. An investor in a platform-as-a-service handling multi-tenant workloads must price the probability of a security incident before Sprint 9 is completed — because the product will be launched into Sprint 9 still incomplete.

## TTL

- **Last validated:** 2026-05-11 (T3.1 LIVE adversarial run)
- **Expires:** 2026-06-10 (30d TTL per `docs/governance/decay-policy.md`)
- **Revalidation:** rerun T3.1 with new dated `scoring-YYYY-MM-DD/`

## Cross-References

- Plan task: T3.4 of `docs/plans/status-validation-and-doc-consolidation-plan.md`
- Triangulation: `/triangulation.yaml`
- Raw runs: `/raw-runs.md`
- Linked from: `docs/STATUS.md` §2 row `tests_confidence`
