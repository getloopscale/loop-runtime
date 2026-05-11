# Scorecard — Infrastructure / Operations

**Generated:** 2026-05-11
**TTL:** 60 days
**Expires:** 2026-07-10

## Final Score (Triangulated)

| Field | Value |
|-------|------:|
| **Median** | 2.8 |
| **Std-dev** | 0.115 |
| **Flagged (>0.3)?** | False |
| **Final score** | **2.8** |

## Persona Scores

| Persona | Model | Score | Justification |
|---------|-------|------:|---------------|
| P1 Vercel DD | claude-opus-4-7 | 3.0 | • 68 infra-ops gaps, 66 UNEVIDENCED + 2 BROKEN (MEET-2026-04-27-061 and CLAUDE.md:176 cleanroom claim, both commit_missing per `evidence-gaps-report.md`). The cleanroom 12/12 success is the load-bearing proof for ops maturity — and its commit ref is broken. Not my domain (per persona scope) but it cascades: if ops can't show a fresh green run, my customer's first deploy is gambling.<br>• `divergence-report-2026-05-11.md` Category B explicitly states Dogfood Gate 'requires cleanroom (operator-only)' — i.e., the very test that would prove ops works hasn't run in this validation cycle. Tier 1 PaaS runs continuous cleanroom rebuilds in CI on schedule; here it's a manual operator gesture documented as TODO.<br>• `docs/architecture/governance/deep-dive-current-state.md` line 29 — 'Zero arquivos têm TTL declarado' across 297 .md surface — meaning ops claims have no expiration policy. Yesterday's 'Sprint 6 shipped' could be stale folklore; the system has no way to know. Vercel uses 7-day on-call rotation rotating-eyes plus auto-archive on doc TTL expiry. |
| P2 Hostile VC | claude-sonnet-4-6 | 2.8 | • evidence-gaps-report.md §infra-ops: 66 UNEVIDENCED + 2 BROKEN. Sprint 8 Phase 1+3+4+5+6 explicitly deferred to CHG-002b/c — that is the hardening sprint. The platform is claiming production-readiness while the hardening tasks are NOT done. MEET-2026-04-04-030:82 issued an SRE CONDITIONAL VETO on declaring production-ready without Sprint 7 tasks 7.1+7.2. Show me the sign-off that those veto conditions were satisfied.<br>• SLO definitions exist (maturity-assessment mentions SLO P50=648ms / P95=705ms) but the denominator window is unstated in any evidence file. Was this measured over 5 minutes or 5 days? With synthetic traffic or real tenants? deep-dive-current-state.md confirms zero TTL on any claim — I cannot know if the SLO measurement is from yesterday or from the initial 1h soak on 2026-05-02.<br>• MEET-2026-04-15-049:38 + 050:67 both record 'Single Pulumi stack for now, split deferred to post-launch' — both have last_validated 2026-04-15 and expire 2026-05-15 (expired by audit date). The IaC split is a known tech-debt item with an expired TTL and no evidence it was ever resolved. Infra debt deferred to post-launch is infra debt I carry as investor. |
| P3 Paranoid SRE | claude-haiku-4-5 | 2.8 | • DigitalOcean infrastructure (3 DOKS clusters, managed PG 16, managed Redis) has zero backup-restore drill in last 30 days. Backup exists (DO Spaces retention) but 'actually restored' is out of scope per incident response training (FMEA not done). What's the RTO if primary PG fails? Is it tested? Unknown. No incident postmortem ever mentions 'we restored from backup' — that's the proof it's been drilled. Until it's drilled, it doesn't exist operationally.<br>• CF Tunnel maintains zero public IPs but has single point of failure: Tunnel token. If the token rotates unexpectedly or CF edge goes down, recovery procedure (docs/runbooks/) exists but has never been executed in an incident. No runbook links to SLO (e.g., 'Tunnel down = page on-call within 2min'). Without alert→runbook→execution chain, it's documentation theater.<br>• task k3d:heal encodes 15 self-healing recoveries (H1-H15) as operator-driven scripts. None has automated alert trigger. When H14 (cell-2 ClusterCredential refresh) fails in production, who runs the script? When? What's the SLO? The artifact (heal script) exists; the operational ownership doesn't. Until there's a Prometheus rule that fires 'CELL_CREDENTIAL_STALE' → page → runbook says 'run H14' → script is triggered automatically, it's not production-hardened. |

## Evidence Cited

Cross-reference with `docs/evidence/infra-ops/`.

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
- Linked from: `docs/STATUS.md` §2 row `infra_ops`
