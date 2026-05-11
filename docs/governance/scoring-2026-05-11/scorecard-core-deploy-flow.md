# Scorecard — Core Deploy Flow

**Generated:** 2026-05-11
**TTL:** 30 days
**Expires:** 2026-06-10

## Final Score (Triangulated)

| Field | Value |
|-------|------:|
| **Median** | 3.0 |
| **Std-dev** | 0.289 |
| **Flagged (>0.3)?** | False |
| **Final score** | **3.0** |

## Persona Scores

| Persona | Model | Score | Justification |
|---------|-------|------:|---------------|
| P1 Vercel DD | claude-opus-4-7 | 3.0 | • Headline claim 'Score 3.9/5.0' at `docs/maturity-assessment.md:6` is BROKEN per `evidence-gaps-report.md` line 20 (commit_missing:b04e0905c1) — the literal evidence cited for the deploy maturity score points at a commit nobody can resolve. Vercel-tier DD would auto-reject the entire scorecard at this line; we don't ship URLs that 404.<br>• `loopzero scale` claim — 'fully functional E2E (B40 fixed 2026-05-06, tag scale-b40-converged-20260506)' in CLAUDE.md:19 — listed BROKEN three times in `evidence-gaps-report.md` (commit_missing:20260506). When the user runs `loopzero scale --replicas 3` and discovers it half-works, the 'WORKS in CHANGELOG' lie kills the trial. Vercel pins every CLI claim to a `vercel scale --json` golden snapshot in CI.<br>• Per `divergence-report-2026-05-11.md` row 'Phase 2+3 expected gaps', the LIVE Dogfood Gate (cleanroom destroy + setup + 12 journeys) has never been run in this validation cycle — operator-deferred. For DEPLOY this is fatal: claim 'cleanroom 12/12 GREEN' at `README.md:127` is BROKEN per gaps-report, meaning the headline 'first deploy works' proof is currently unverifiable. Tier 1 = green pipeline button you can point at; we have a tag from 6 days ago and no replay. |
| P2 Hostile VC | claude-sonnet-4-6 | 3.0 | • evidence-gaps-report.md §core-deploy: 60 UNEVIDENCED + 5 BROKEN claims. The flagship claim 'Deploy happy path: Done, E2E validated on DO' (maturity-assessment.md:93) is classified UNEVIDENCED with last_validated=2026-05-03. That is 8 days before this audit. Show me the DO access log with timestamps, HTTP 200 responses, and the git commit SHA of the bundle deployed — not a markdown table that a founder wrote.<br>• claim-catalog.yaml entry for maturity score 3.9 is tagged evidence_tier=BROKEN because commit b04e0905c1 is missing from the repo. The primary score anchor is a broken reference. Any VC deck citing 3.9 is citing a number backed by a commit that does not exist — that is material misrepresentation, not a minor gap.<br>• MEET-2026-05-03-066-ASYNC: 'loopzero scale config persistence shipped but actuation requires loopzero deploy to apply' — this is a half-shipped feature presented as complete. 'Fully functional E2E (B40 fixed)' in claim-catalog.yaml but the meeting record admits operator actuation is pending a follow-up plan. Pitch says shipped; code says half-wired. -0.5 on this dimension alone. |
| P3 Paranoid SRE | claude-haiku-4-5 | 3.5 | • Deploy happy path works end-to-end on DigitalOcean (E2E validated). BUT zero failure-mode chaos coverage: Sprint 8 Phase 5 (Chaos validation) was DEFERRED to unscheduled CHG-002c. Chaos Mesh CRDs installed across 4 clusters (2026-04-28) but zero of 9 documented chaos scenarios (E1-E9) have ever executed against live infra. Without pod-kill-under-deploy (E6) or lock-contention-failure (E2) drills, the '3-second rollback' claim is untested in adversarial conditions. Production stage 1 soak (120 probes/1h) is fixture data, not real traffic.<br>• Build-to-ArgoCD pipeline has documented single-failure recovery: ArgoCD repo-server timeout overrun (F-01/2026-05-08) and Gitea credentials collision (F-02/2026-05-08) were both post-incident fixes, not pre-incident predictions. Neither scenario appears in a runbook's known-failure section. Recovery was empirical (discovered via cleanroom), not engineered. Runbooks exist (docs/runbooks/) but none link to the actual failure mode + detection SLO + escalation path.<br>• Concurrent deploy rejection (1×202+1×409) works in tests but relies on pg_advisory_xact_lock within withEnvironmentLock. Lock timeout is 5 seconds — untested under contention >100 QPS. No load test (task loadtest:run LEVEL=l1) shows lock behavior under sustained parallelism. What happens at >1000 concurrent deploy attempts? Unknown. |

## Evidence Cited

Cross-reference with `docs/evidence/core-deploy-flow/`.

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
- **Expires:** 2026-06-10 (30d TTL per `docs/GOVERNANCE.md (§2 Decay Policy)`)
- **Revalidation:** rerun T3.1 with new dated `scoring-YYYY-MM-DD/`

## Cross-References

- Plan task: T3.4 of `docs/plans/status-validation-and-doc-consolidation-plan.md`
- Triangulation: `/triangulation.yaml`
- Raw runs: `/raw-runs.md`
- Linked from: `docs/STATUS.md` §2 row `core_deploy_flow`
