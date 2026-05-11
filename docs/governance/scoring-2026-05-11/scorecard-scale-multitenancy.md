# Scorecard — Scale / Multi-tenancy

**Generated:** 2026-05-11
**TTL:** 60 days
**Expires:** 2026-07-10

## Final Score (Triangulated)

| Field | Value |
|-------|------:|
| **Median** | 2.9 |
| **Std-dev** | 0.208 |
| **Flagged (>0.3)?** | False |
| **Final score** | **2.9** |

## Persona Scores

| Persona | Model | Score | Justification |
|---------|-------|------:|---------------|
| P1 Vercel DD | claude-opus-4-7 | 3.0 | • `docs/maturity-assessment.md:26` Scale/MT 3.5/5 claim is UNEVIDENCED twice in `evidence-gaps-report.md` §scale-mt (duplicate entries — itself a counting-bias red flag). 64 scale-mt gaps total, 100% UNEVIDENCED. Cell architecture might work; the docs don't prove it.<br>• `MEET-ASYNC-AMENDMENT-002-e2e-ginkgo-rewrite-governance.md:101` per gaps-report — '\| 20 \| Multi-cell routing \| ❌ \| ✅ \|' — a table cell flips ❌→✅ with no evidence ref. A customer can't validate 'we route across cells' from this primary source. Vercel multi-region routing has CI-asserted geo-probes per region per PR.<br>• Per `deep-dive-current-state.md` §'Aggregation Bias' — the 3.79 raw average rounded up to 3.9 in `maturity-assessment.md:26` hides that Product Completeness (3.4) and Scale/MT (3.5) sit far below the 4.5 Core Deploy headline. A DD reader sees '3.9' and skips the dimension table; that's exactly the bias the catalog is meant to expose, and currently the doc remains the loudest signal. |
| P2 Hostile VC | claude-sonnet-4-6 | 2.6 | • Auto-penalty triggered: 'Multi-cell' with 1 cell ever loaded with real production traffic (-0.5). cell-2 'LIVE-validated' means 1 synthetic tenant pinned via loopzero admin tenants pin + 1h ADR-compliant 120 probes. That is a synthetic soak, not a customer. deep-dive-current-state.md explicitly acknowledges 'cell-2 LIVE-validated 2026-05-02' in the claim-catalog but evidence_tier=UNEVIDENCED. I am being asked to believe 'multi-cell production ready' on the basis of one synthetic tenant and 120 HTTP probes.<br>• evidence-gaps-report.md §scale-mt: 64 UNEVIDENCED, 0 EVIDENCED, 0 BROKEN (all gaps are just absent evidence). 'Per-project ArgoCD Applications deferred to post-100-tenants' (MEET-2026-04-09-044:129) — so the isolation model changes at 100 tenants. How many tenants are on the platform today? If the answer is 0 external users, the scaling architecture has never been stress-tested at any real tenant density. Deferred architectural risk is investor risk.<br>• MEET-ASYNC-AMENDMENT-002 row 20 explicitly marks 'Multi-cell routing' as NOT covered by the E2E Ginkgo rewrite (❌ in the test matrix). The very test suite claimed to be '76/76 GREEN' does not cover multi-cell routing. A claim of 76/76 on a suite that excludes the highest-risk dimension is aggregation bias. -0.5 aggregation penalty applied. |
| P3 Paranoid SRE | claude-haiku-4-5 | 2.9 | • Multi-cell topology deployed: 3/3 cells connected (cell-default + cell-1 + cell-2, last activated 2026-05-02). But 'cell-2 LIVE-validated' means: 1 tenant pinned, 1 deploy observed, 120 HTTP probes over 1 hour (T4.3 E2E journey), isolation confirmed (pod running on correct cell), namespace isolation verified (cell-1 NotFound from cell-2 runtime). That's not production validation. That's controlled fixture. Production stages 4-5 (1 customer 7-day soak, then 10/50/100% canary) are Sprint 9 scope — explicitly deferred. Real multi-tenancy under sustained load has never been demonstrated.<br>• Zero load test executed for multi-cell failover: if cell-1's control plane fails, tenants on cell-1 are stuck. No chaos scenario tests 'kill all API servers on cell-1 cluster' + 'verify tenants failover to cell-2 or degrade gracefully'. What's the blast radius? How many tenants? How long is outage? Unknowns. Chaos-mesh-experiments plan has E8+E9 (multi-cell failover) but they're Phase 1 tasks in a deferred plan.<br>• KEDA autoscaling targets Rollout Kind via ScaleTargetRef. Default to Rollout when LOOPZERO_PROGRESSIVE_DELIVERY=true. But scale-to-zero test coverage is thin: did it actually work when Pod count hit zero and had to scale back up? Or was it lucky that load generator didn't starve capacity? 'Tested locally in k3d' is not 'tested under 1000 concurrent user load'. |

## Evidence Cited

Cross-reference with `docs/evidence/scale-multitenancy/`.

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
- **Expires:** 2026-07-10 (60d TTL per `docs/governance/decay-policy.md`)
- **Revalidation:** rerun T3.1 with new dated `scoring-YYYY-MM-DD/`

## Cross-References

- Plan task: T3.4 of `docs/plans/status-validation-and-doc-consolidation-plan.md`
- Triangulation: `/triangulation.yaml`
- Raw runs: `/raw-runs.md`
- Linked from: `docs/STATUS.md` §2 row `scale_multitenancy`
