# Reconciliation Meeting — {DIMENSION}

**Date:** {YYYY-MM-DD}
**Trigger:** Std-dev > 0.3 in T3.2 triangulation for dimension `{dimension_key}`
**Plan task:** T3.3 of `docs/plans/status-validation-and-doc-consolidation-plan.md`

---

## Pre-Meeting Inputs

- P1 (Vercel DD) score: {p1_score}
- P2 (Hostile VC) score: {p2_score}
- P3 (Paranoid SRE) score: {p3_score}
- Median: {median}
- Std-dev: {std_dev}

## Persona Justifications (raw)

### P1 Vercel DD
{bullets from p1.yaml}

### P2 Hostile VC
{bullets from p2.yaml}

### P3 Paranoid SRE
{bullets from p3.yaml}

## Discussion

**Why the delta?**

{Manual analysis: which persona is over/under-scoring? Different criteria? Different evidence weighting?}

### Arbiter (Principal Architect role) questions

1. Are P1/P2/P3 looking at the same evidence?
2. Is the delta semantic (different lens) or factual (one persona has wrong data)?
3. What concrete artifact would shift the lowest-scoring persona up, or vice versa?

### Convergence path

{Document the final agreed score or note dissent}

## Final Decision

| Field | Value |
|-------|------:|
| **Reconciled score** | {N.N} |
| **Decision method** | _consensus / arbiter-decided / dissent-recorded_ |
| **Dissenting opinions** | {if any, document them} |

## Action Items (if any)

- [ ] Update `docs/STATUS.md` §2 row `{dimension_key}` with reconciled score
- [ ] Update `docs/governance/scoring-{YYYY-MM-DD}/scorecard-{dim}.md` with final
- [ ] Document open work items if dim has actionable gaps

## Cross-References

- T3.2 output: `docs/governance/scoring-{YYYY-MM-DD}/triangulation.yaml`
- T3.4 scorecard: `docs/governance/scoring-{YYYY-MM-DD}/scorecard-{dim}.md`
- Per-persona files: `p1-vercel-dd.yaml`, `p2-hostile-vc.yaml`, `p3-paranoid-sre.yaml`
- Plan ADR D4 (adversarial personas) + ADR-0051 (canonical contract)
