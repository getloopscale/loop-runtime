# Reconciliation Meeting — Security

**Date:** 2026-05-11
**Trigger:** std-dev = 0.361 > 0.3 in T3.2 triangulation
**Plan task:** T3.3 of `docs/plans/status-validation-and-doc-consolidation-plan.md`
**Status:** AWAITING OPERATOR — auto-generated from T3.1 LIVE run

## Pre-Meeting Inputs

| Persona | Model | Score |
|---------|-------|------:|
| P1 Vercel DD | claude-opus-4-7 | 3.0 |
| P2 Hostile VC | claude-sonnet-4-6 | 2.5 |
| P3 Paranoid SRE | claude-haiku-4-5 | 3.2 |
| **Median** | — | **3.0** |
| **Std-dev** | — | **0.361** |

**Spread:** 0.7

## Persona Justifications (raw bullets)

### P1 Vercel DD

- 32 security gaps, 31 UNEVIDENCED + 1 BROKEN (`action-sha-pinning.md:27` cosign-installer commit_missing:cad07c2e89 per `evidence-gaps-report.md`). A broken commit ref ON THE COSIGN PINNING RUNBOOK is the worst possible failure mode — it makes the supply-chain story unverifiable. Tier 1 SLSA pins are CI-asserted, not markdown-asserted.
- Per `evidence-gaps-report.md` §security row 4 (MEET-2026-04-04-030:223), 'Security veto is ABSOLUTE per governance rules. Tasks 7.1, 7.2, 7.3, and 7.4 are MANDATORY before production' — UNEVIDENCED. So we have an explicit Security-veto gate with no current evidence file proving it cleared. From a customer DD standpoint: production-ready claims with unproven security gates = bounce.
- `MEET-2026-04-27-061.md:52` (UNEVIDENCED in gaps-report §security) — 'Sprint 6 / supply chain shipped 2026-04-26 (cosign + SBOM + policy-controller Audit). Deny-mode flip pending'. This is exactly the 'demo-mode security' Vercel DD calls out: the policy is AUDIT-only, not enforcing. Customer who reads only the 'shipped' bullet gets misled.

### P2 Hostile VC

- evidence-gaps-report.md §security: 31 UNEVIDENCED + 1 BROKEN. Zero external pen-test report. Zero independent security audit in last 12 months. MEET-2026-04-04-030:223 required tasks 7.1/7.2/7.3/7.4 before production-ready declaration — these are RunAsRoot and PSA enforcement. The claim 'Sprint 6 security shipped' coexists with 'deny-flip + cosign keyless verification pending Sprint 9.' The policy-controller is in warn mode. Warn mode is not security.
- ADR-0018:11 defers RBAC tightening to Sprint 9 (ADR-0019). Sprint 9 is explicitly marked not started. DO_TOKEN as credentials (no OIDC replacement) noted in MEET-2026-05-03-062:138 as future work. SBOM+attestation parity ADR-0040 shows '❌ NO SBOM / ❌ NO attestation' for multiple charts. A $10M platform with static long-lived DO_TOKEN credentials and no RBAC tightening is a breach waiting to happen.
- The only security claim with PARTIAL_CONFIDENCE (not UNEVIDENCED) is cosign in-pipeline signing — but cosign keyless via Fulcio is pending Sprint 9. Key-based signing means the cosign private key must be rotated on a schedule. Evidence of key rotation schedule: absent from all 6 input artifacts. Show me the rotation runbook or I price this as a latent compliance liability.

### P3 Paranoid SRE

- Supply chain (Sprint 6 shipped): cosign image signing + syft SBOM + policy-controller in deny-by-default. But policy-controller runs in 'warn' mode as of 2026-05-08 (not enforce). Deny-flip is Sprint 9 deferred. In 'warn' mode, a malicious image can be deployed; warn-mode creates false confidence. Deny-mode validates have never run under real load. What if policy-controller itself fails (OOMKilled)? Fallback mode is undefined. Infra has not been stress-tested for supply-chain resilience.
- Secrets managed via External Secrets Operator (ESO) consuming Cloudflare KV. ESO CRD is wired but AppSecret CRD (LoopZero's own abstraction) is defined but unwired — no controller. This means the codebase has an orphan CRD that users could loopzeroretically claim but nobody maintains. Schizophrenic secret story: some via ESO, some via CRD (stub), some missing entirely. Multi-year code smell.
- Cosign keys are key-based, not keyless (Fulcio pending Sprint 9). Key rotation procedure exists in docs/runbooks/ but no incident has ever triggered key rotation. Operator training (SRE on-call rotation) doesn't link 'what happens if cosign key is compromised' to the runbook. Rotation is untested.

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

- [ ] Update `docs/STATUS.md` §2 row `security` with reconciled score
- [ ] Update `docs/governance/scoring-2026-05-11/scorecard-security.md` with final
- [ ] Document open work items if dim has actionable gaps

## Cross-References

- T3.2 output: `triangulation.yaml`
- T3.4 scorecard: `scorecard-security.md`
- Per-persona files: `p1-vercel-dd.yaml`, `p2-hostile-vc.yaml`, `p3-paranoid-sre.yaml`
- Plan ADR D4 (adversarial personas) + ADR-0051 (canonical contract)
