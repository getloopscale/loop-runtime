# Scorecard Template — Scale / Multi-tenancy

**Dimension:** scale-mt
**Status:** TEMPLATE — operator copies to `docs/governance/scoring-{YYYY-MM-DD}/` and populates with LIVE persona outputs.

---

## Scope (what claims fall under this dim)

3 cells active, ClusterCredential per cell, admin pin, DynamicRouter, multi-cell observability

See `docs/governance/claim-catalog.yaml` filter `dimension: scale-mt` for full list (\~N claims).

## Persona Scores (post-T3.1 LIVE run)

| Persona | Model | Temp | Seed | Score | Justification (3 bullets) |
|---------|-------|-----:|-----:|------:|---------------------------|
| P1 Vercel DD | _opus_ | 0.7 | 42 | _N.N_ | _• bullet 1_<br>_• bullet 2_<br>_• bullet 3_ |
| P2 Hostile VC | _gpt-5-4_ | 0.3 | 137 | _N.N_ | _• bullet 1_<br>_• bullet 2_<br>_• bullet 3_ |
| P3 Paranoid SRE | _haiku_ | 0.5 | 99 | _N.N_ | _• bullet 1_<br>_• bullet 2_<br>_• bullet 3_ |

## Triangulation Output (post-T3.2)

| Metric | Value |
|--------|------:|
| Median | _N.N_ |
| Std-dev | _N.NNN_ |
| Flagged (>0.3)? | _yes/no_ |
| **Final score** | _**N.N**_ (if not flagged) or _pending T3.3_ |

## Evidence Cited

- T1 (code+test): _commit:<sha>:<test-file>_
- T2 (LIVE log): _live:<timestamp>:<log-path>_
- T3 (CI run): _ci:<workflow>:<run-id>_

## Divergences from Other Personas (post-T3.3 if std-dev > 0.3)

_E.g., "P1 scored 4.5, P3 scored 3.4 — gap = chaos coverage. Reconciliation meeting concluded 3.8 with P3 dissent."_

## Open Concerns

- _List specific dim concerns surfacing across multiple personas_

## TTL

- **Validated:** _YYYY-MM-DD_
- **Expires:** _YYYY-MM-DD_ (per `docs/governance/decay-policy.md` tier)

## Cross-References

- Plan task: T3.4 of `docs/plans/status-validation-and-doc-consolidation-plan.md`
- Evidence files: `docs/evidence/scale-mt/`
- Linked from: `docs/STATUS.md` §2 row scale-mt
