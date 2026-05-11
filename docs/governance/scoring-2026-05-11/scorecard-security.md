# Scorecard — Security

**Generated:** 2026-05-11
**TTL:** 60 days
**Expires:** 2026-07-10

## Final Score (Triangulated)

| Field | Value |
|-------|------:|
| **Median** | 3.0 |
| **Std-dev** | 0.361 |
| **Flagged (>0.3)?** | True |
| **Final score** | _Pending T3.3 reconciliation (flagged)_ |

## Persona Scores

| Persona | Model | Score | Justification |
|---------|-------|------:|---------------|
| P1 Vercel DD | claude-opus-4-7 | 3.0 | • 32 security gaps, 31 UNEVIDENCED + 1 BROKEN (`action-sha-pinning.md:27` cosign-installer commit_missing:cad07c2e89 per `evidence-gaps-report.md`). A broken commit ref ON THE COSIGN PINNING RUNBOOK is the worst possible failure mode — it makes the supply-chain story unverifiable. Tier 1 SLSA pins are CI-asserted, not markdown-asserted.<br>• Per `evidence-gaps-report.md` §security row 4 (MEET-2026-04-04-030:223), 'Security veto is ABSOLUTE per governance rules. Tasks 7.1, 7.2, 7.3, and 7.4 are MANDATORY before production' — UNEVIDENCED. So we have an explicit Security-veto gate with no current evidence file proving it cleared. From a customer DD standpoint: production-ready claims with unproven security gates = bounce.<br>• `MEET-2026-04-27-061.md:52` (UNEVIDENCED in gaps-report §security) — 'Sprint 6 / supply chain shipped 2026-04-26 (cosign + SBOM + policy-controller Audit). Deny-mode flip pending'. This is exactly the 'demo-mode security' Vercel DD calls out: the policy is AUDIT-only, not enforcing. Customer who reads only the 'shipped' bullet gets misled. |
| P2 Hostile VC | claude-sonnet-4-6 | 2.5 | • evidence-gaps-report.md §security: 31 UNEVIDENCED + 1 BROKEN. Zero external pen-test report. Zero independent security audit in last 12 months. MEET-2026-04-04-030:223 required tasks 7.1/7.2/7.3/7.4 before production-ready declaration — these are RunAsRoot and PSA enforcement. The claim 'Sprint 6 security shipped' coexists with 'deny-flip + cosign keyless verification pending Sprint 9.' The policy-controller is in warn mode. Warn mode is not security.<br>• ADR-0018:11 defers RBAC tightening to Sprint 9 (ADR-0019). Sprint 9 is explicitly marked not started. DO_TOKEN as credentials (no OIDC replacement) noted in MEET-2026-05-03-062:138 as future work. SBOM+attestation parity ADR-0040 shows '❌ NO SBOM / ❌ NO attestation' for multiple charts. A $10M platform with static long-lived DO_TOKEN credentials and no RBAC tightening is a breach waiting to happen.<br>• The only security claim with PARTIAL_CONFIDENCE (not UNEVIDENCED) is cosign in-pipeline signing — but cosign keyless via Fulcio is pending Sprint 9. Key-based signing means the cosign private key must be rotated on a schedule. Evidence of key rotation schedule: absent from all 6 input artifacts. Show me the rotation runbook or I price this as a latent compliance liability. |
| P3 Paranoid SRE | claude-haiku-4-5 | 3.2 | • Supply chain (Sprint 6 shipped): cosign image signing + syft SBOM + policy-controller in deny-by-default. But policy-controller runs in 'warn' mode as of 2026-05-08 (not enforce). Deny-flip is Sprint 9 deferred. In 'warn' mode, a malicious image can be deployed; warn-mode creates false confidence. Deny-mode validates have never run under real load. What if policy-controller itself fails (OOMKilled)? Fallback mode is undefined. Infra has not been stress-tested for supply-chain resilience.<br>• Secrets managed via External Secrets Operator (ESO) consuming Cloudflare KV. ESO CRD is wired but AppSecret CRD (LoopZero's own abstraction) is defined but unwired — no controller. This means the codebase has an orphan CRD that users could loopzeroretically claim but nobody maintains. Schizophrenic secret story: some via ESO, some via CRD (stub), some missing entirely. Multi-year code smell.<br>• Cosign keys are key-based, not keyless (Fulcio pending Sprint 9). Key rotation procedure exists in docs/runbooks/ but no incident has ever triggered key rotation. Operator training (SRE on-call rotation) doesn't link 'what happens if cosign key is compromised' to the runbook. Rotation is untested. |

## Evidence Cited

Cross-reference with `docs/evidence/security/`.

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
- Linked from: `docs/STATUS.md` §2 row `security`
