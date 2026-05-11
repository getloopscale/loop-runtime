# Deep Dive — Proposed State of Documentation Governance (PROPOSED)

**Generated:** 2026-05-11
**Scope:** Mode 3 (PROPOSED) per `/architecture-docs` skill
**Promotion gate:** Pending `/dogfood full` PASS (per to-plan skill Step 7)

<!-- PROPOSED STATE -->

---

## Executive Summary

Per ADR-0051, governance da documentação adota **claim-evidence-decay contract** com 8 camadas materiais de validação entre autoria e leitor. Versus AS-IS (zero camadas, drift inevitável), PROPOSED introduz:
- 1 hub canonical (`docs/STATUS.md`)
- 7 dimensões com TTL visível
- Per-claim evidence files (3 sources independentes)
- 3 adversarial personas com independence verificada
- 4 CI workflows + 1 composite action + 1 pre-merge gate
- 8 scripts de validação em `infra/scripts/governance/`
- 4 Excalidraw diagrams + 6 PlantUML files (3 AS-IS + 3 PROPOSED)

## Delta from AS-IS

### Quantitative

| Métrica | AS-IS (2026-05-11 baseline) | PROPOSED | Δ |
|---------|----------------------------:|---------:|---:|
| Status surfaces | 5+ (CLAUDE.md, maturity, plans, meetings, runbooks, memory) | 1 (STATUS.md) | -4+ |
| Claims with TTL | 0 / 348 (0%) | 348 / 348 (100%) | +348 |
| Claims with evidence schema | 0 / 348 | Target 50-80 (PARTIAL while Phase 3 pending) | +50-80 |
| Validation scripts | 0 | 8 | +8 |
| CI governance workflows | 0 | 5 | +5 |
| Pre-merge gates | 0 | 1 (STATUS.md edits) | +1 |
| Adversarial personas | 0 | 3 (Vercel/VC/SRE) with independence | +3 |
| Architecture docs (governance domain) | 0 | 6 (3 PlantUML AS-IS + 3 PROPOSED) + 4 Excalidraw | +10 |
| CLAUDE.md status section size | ~6000 chars | ~470 chars | -92% |
| Maturity-assessment.md role | Canonical (stale) | Redirect | -change |

### Qualitative

| Behavior | AS-IS | PROPOSED |
|----------|-------|----------|
| Claim added to STATUS | git commit, no checks | Pre-merge gate validates evidence file delta |
| Stale claim detection | Manual / never | Weekly CI decay-scan + auto issue |
| Score recalibration | Self-attestation prose | 3 personas blind → triangulation → meeting if std-dev > 0.3 |
| Bias detection | Implicit awareness | Material script `bias-check.sh` (6 patterns) |
| Evidence verification | Trust by default | Explicit 3-source schema (T1/T2/T3) + validator |
| Confidence claim | "3.9/5.0" with no methodology | "median(p1,p2,p3) with std-dev, capped 95-97%" |

## Why the Cap is 95-97%, Not 99%

Mathematical model from ADR D1:
- Each evidence source has p ≥ 0.9 individual reliability
- 3 sources under **full independence**: 1 − (1-0.9)³ = 99.9%
- **Real correlation residual** brings cap to **95-97%**:
  - Tests + LIVE runs may share fixtures (~0.2 correlation)
  - CI re-invokes same tests in different env (~0.3)
  - Same-model-family adversarial reviewers share training-data bias (~0.1-0.2)

Stating "99%" without cross-vendor models (codex/GPT/Claude diversity) would be mathematically dishonest. The 95-97% cap is the **honest** ceiling and is still 2-3 orders of magnitude better than single-source claims (which empirically run 60-80% reliable).

## Material Defense Layers (8 Total)

```
1. Author writes claim (free-form)
2. extract-claims.py captures into catalog
3. classify-evidence.py labels T1/T2/T3 (gaps reported)
4. init-decay-clock.py assigns TTL
5. validate-evidence.sh runs on PR (pre-merge gate)
6. 3 adversarial personas blind-score (T3.0 independence-verified)
7. triangulate-scores.py converges → STATUS.md
8. CI continuous loop (decay/CV/spotcheck/refresh) — drift detection
```

Each layer is **materially auditable** (script source, file output, workflow log). No layer depends on human discipline.

## Path Forward (Pending Steps)

| Phase | Status | Blockers |
|-------|--------|----------|
| Phase 0 (Foundation) | ✅ DONE | — |
| Phase 1 (Inventory) | ✅ DONE | — |
| Phase 2 (Code-Truth) | ⏳ Pending | Requires `/cross-validation`, `/dead-code`, `/code-audit` skill invocations |
| Phase 3 (Adversarial) | ⏳ Pending | Requires LIVE agent runs with cross-vendor diversity (codex available) |
| Phase 4 (Single SoT) | ✅ DONE | — |
| Phase 5 (CI Loop) | ✅ DONE | — |
| Phase 6 (Governance) | 🟡 In progress | T6.5 (this file) |
| Dogfood Gate | ⏳ Pending | Requires cleanroom — Ralph cannot run; operator runs `/dogfood full` |

## Promotion Criteria

This `docs/architecture/governance/diff/` directory promotes to `docs/architecture/governance/` after:
1. All Phase 0-6 tasks complete (excluding cross-vendor cost-dependent items)
2. `/dogfood full` PASS on cleanroom (operator)
3. User confirms promotion

Per `/to-plan` skill Step 7: "A implementação alterou a arquitetura do domínio governance. Os novos diagramas estão em docs/architecture/governance/diff/. Posso substituir os documentos principais em docs/architecture/governance/ com a versão atualizada?"

Until promotion, both AS-IS and PROPOSED coexist (audit trail).

## Risk Acceptance

Following risks are explicitly accepted in PROPOSED design:

| Risk | Acceptance reason |
|------|-------------------|
| Cross-vendor codex unavailable in some CI runners | Fallback: 3 Claude models (opus/sonnet/haiku) + temperature/seed variation. Logs which dimensions varied. |
| `bias-check.sh` FP rate ~30% (target <20%) | Calibration improvement deferred. WARN level until tightened. Meta-discussion plans are expected FP source. |
| Operational cost ~$30-60 setup + $15-25/mo | Documented in EC-12; cost is gating for adversarial runs only |
| 1 of 7 dimensions has only 2 evidence sources (PARTIAL_CONFIDENCE) | Plan D1 allows this — flagged with asterisk, not counted toward score |
| Decay scanner issue automation could create noise | Mitigation: only opens issue when expired_count > 0; uses single update-or-create pattern (not multiple issues) |

## Cross-References

- ADR-0051 (claim-evidence-decay contract)
- AS-IS deep dive: `docs/architecture/governance/deep-dive-current-state.md`
- Plan: `docs/plans/status-validation-and-doc-consolidation-plan.md` v1.2
- Edge cases: `docs/plans/status-validation-and-doc-consolidation-edge-cases.md`
- Visual: `docs/architecture/governance/concept-evidence-triangulation-pipeline.excalidraw` (central pipeline)
- Material proof: `docs/evidence/tests-confidence/2026-05-11-validate-evidence-script.md` (first EVIDENCED claim — proves pipeline)
