# Scorecard Templates

**Status:** Skeletons awaiting Phase 3.1 LIVE adversarial run.

Per T3.4 of `status-validation-and-doc-consolidation-plan` v1.2.

## Lifecycle

1. **Pre-LIVE (current state):** 7 dimension scorecard templates exist with `_TEMPLATE` placeholders.
2. **Post-LIVE adversarial run (T3.1):** Operator copies templates to dated directory (e.g., `docs/governance/scoring-2026-MM-DD/`) and fills with persona outputs.
3. **Triangulation (T3.2):** `triangulate-scores.py` computes median + std-dev.
4. **Reconciliation (T3.3):** Flagged dims (std-dev > 0.3) trigger `/meeting` orchestrator.
5. **Final scorecards (T3.4):** Operator finalizes with reconciled scores → links from `docs/STATUS.md` §2.

## Required Operator Steps Pre-Phase-3.1

```bash
# 1. Verify independence config
bash infra/scripts/governance/verify-independence.sh
# Expected: PASS — independence verified

# 2. Pre-flight inputs check
ls docs/governance/claim-catalog.yaml docs/governance/evidence-gaps-report.md \
   docs/governance/divergence-report-2026-05-11.md docs/governance/code-audit-2026-05-11.md
# All 4 must exist

# 3. Choose run date
RUN_DATE=$(date +%Y-%m-%d)
mkdir -p docs/governance/scoring-${RUN_DATE}
```

## T3.1 LIVE Run Pattern

Spawn 3 Agents in parallel (single message, 3 Agent tool calls):

```python
# Pseudo-code for parallel Agent invocation
Agent({
  subagent_type: "general-purpose",
  description: "P1 Vercel DD scoring",
  model: "opus",  # per independence-config.yaml
  prompt: load("docs/governance/personas/vercel-dd-engineer.md") + INPUT_ARTIFACTS_PROMPT
}),
Agent({
  subagent_type: "codex:codex-rescue",  # cross-vendor — REQUIRES codex setup
  description: "P2 Hostile VC scoring",
  prompt: load("docs/governance/personas/hostile-vc-auditor.md") + INPUT_ARTIFACTS_PROMPT
}),
Agent({
  subagent_type: "general-purpose",
  description: "P3 Paranoid SRE scoring",
  model: "haiku",
  prompt: load("docs/governance/personas/paranoid-sre.md") + INPUT_ARTIFACTS_PROMPT
})
```

**Cost estimate (per ADR D1 EC-12):** ~$5-15 for full Phase 3.1 run (3 agents × ~10-15min each).

## Fallback Mode (Codex Unavailable)

If `codex:codex-rescue` not configured:
```bash
USE_FALLBACK=true bash infra/scripts/governance/verify-independence.sh
```
- P2 falls back to `claude-sonnet-4-6` (Claude-only family)
- Cap confidence downgrades from 95-97% → 90-95%
- Document in run provenance: `raw-runs.md`

## Per-Dimension Scorecards (7 Files)

| File | Dimension |
|------|-----------|
| `core-deploy.template.md` | Core Deploy Flow |
| `dx-cli.template.md` | DX / CLI |
| `infra-ops.template.md` | Infrastructure / Operations |
| `security.template.md` | Security |
| `scale-mt.template.md` | Scale / Multi-tenancy |
| `product-completeness.template.md` | Product Completeness |
| `tests-confidence.template.md` | Tests / Confidence |

## Cross-References

- Personas: `docs/governance/personas/{vercel-dd-engineer,hostile-vc-auditor,paranoid-sre}.md`
- Independence config: `docs/governance/personas/independence-config.yaml`
- Triangulator: `infra/scripts/governance/triangulate-scores.py`
- Plan T3.0-T3.4 sections
