# Scorecard — Product Completeness

**Generated:** 2026-05-11
**TTL:** 30 days
**Expires:** 2026-06-10

## Final Score (Triangulated)

| Field | Value |
|-------|------:|
| **Median** | 2.2 |
| **Std-dev** | 0.802 |
| **Flagged (>0.3)?** | True |
| **Final score** | _Pending T3.3 reconciliation (flagged)_ |

## Persona Scores

| Persona | Model | Score | Justification |
|---------|-------|------:|---------------|
| P1 Vercel DD | claude-opus-4-7 | 1.5 | • AUTO -1.0 per scoring rules: ZERO external users. `deep-dive-current-state.md` §'Self-Attestation Bias' confirms this is pre-release self-scoring with no external review. `evidence-gaps-report.md` §product-completeness shows 29 gaps, 100% UNEVIDENCED. Until customer #1 deploys, 'production-ready' is theatre.<br>• 29 product-completeness gaps include `MEET-2026-04-04-030.md:14` 'approximately 85% production readiness' (UNEVIDENCED) AND `MEET-2026-04-04-030.md:437` '10-14h to production-ready' (UNEVIDENCED). The platform has been '10-14h away' for 37 days. Vercel Pro tier launched with 50 paying customers in week 1; here we have 0/N.<br>• `evidence-gaps-report.md` cleanroom rows: README.md:127 BROKEN + CLAUDE.md:176 BROKEN. The dogfood loop IS the product-completeness proof for a pre-release PaaS, and both citations point at missing commits. A customer's first 5 minutes hit `loopzero init → deploy` — and that path's only proof artifact is broken. |
| P2 Hostile VC | claude-sonnet-4-6 | 2.2 | • Auto-penalty triggered: 'Shipped' with 0 external users (-0.5). The product is pre-release per its own governance docs. Sprint 9 (Polish + Launch) is marked NOT STARTED. Product Completeness at 3.4/5 in the team's own assessment, but with 0 paying customers, 0 external NPS data, 0 churn data. deep-dive-current-state.md §Aggregation Bias confirms Product Completeness 3.4 is masked in the 3.9 overall by Core Deploy 4.5. I do not fund averages; I fund businesses.<br>• evidence-gaps-report.md §product-completeness: 29 UNEVIDENCED, 0 EVIDENCED. AppSecret CRD is defined but has no controller — documented in CLAUDE.md 'Known issues.' This is a named known gap in a product being positioned for launch. ESO works as a workaround but 'workaround' is not 'product feature.' SSO/SAML absent. Loki cross-cluster streaming absent. Each absent feature is a prospect objection in a sales call.<br>• MEET-2026-04-04-030:437 estimated '10-14h to production-ready' — that was on 2026-04-04, over 5 weeks ago. If it was truly 10-14h of work, why is Sprint 9 (Polish + Launch) still NOT STARTED on 2026-05-11? Either the estimate was wrong (trust deficit) or the work was done but not validated (evidence gap). Both answers concern me. |
| P3 Paranoid SRE | claude-haiku-4-5 | 3.1 | • Core features shipped: deploy, promote, rollback, cancel, logs, status, scale, preview envs, secrets, custom domains, GitHub auto-deploy. 25/30 E2E tests pass. But 'shipped' means 'code exists', not 'customer can use without discovering a crash on day 2'. Zero production customers have used these features. Demo on internal dogfood infra ≠ customer production. FAANG ships with 0.1% error budget; LoopZero has no error budget because there's no paying customer to measure against.<br>• Incident response plan for customer-impacting outage has never been drilled. What's the page-on-call threshold? Is there a war room rotation? Does the on-call SRE have kubectl access to customer cells (cell-2+)? Does the SRE have ability to trigger manual sync or revert a deployment without 30-minute approval cycle? Runbooks say 'contact support' — but support itself is not built (Stripe customer creation deferred, SRE roles not staffed). Without operational readiness, 'shipped product' is a liability.<br>• Cancelable deployments (cancel) were validated in journey (E2E PASS) but cancel under in-flight image build + security policy validation + ArgoCD sync-in-progress has never been chaos-tested. Cancel is a state machine: if it fails mid-state, can it resume? Or does the deployment hang forever? The journey tests happy path; the failure paths are code-only. |

## Evidence Cited

Cross-reference with `docs/evidence/product-completeness/`.

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
- Linked from: `docs/STATUS.md` §2 row `product_completeness`
