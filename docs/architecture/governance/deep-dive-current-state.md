# Deep Dive — Current State of Documentation Governance (AS-IS)

**Captured:** 2026-05-11
**Scope:** Mode 2 snapshot per `/architecture-docs` skill
**Status:** Baseline para Mode 3 PROPOSED (gerado em T6.5 do plano após implementation)

---

## Executive Summary

A documentação de status do LoopZero está **funcionalmente correta mas epistemicamente vulnerável**. Funcionalmente: cada surface (CLAUDE.md, maturity-assessment.md, plans/, meetings/) cumpre seu papel imediato. Epistemicamente: não há nada que impeça drift, viés, ou stale folklore. A conversa de 2026-05-11 demonstrou empiricamente que **o próprio agente Claude com 1M context viesou +0.4 pontos de maturity em <30 minutos** — prova material de que awareness implícita não funciona.

## Surface Inventory (Quantificada)

| Surface | Count | Tamanho aproximado | Atualização típica | TTL declarado |
|---------|------:|--------------------:|--------------------|---------------|
| `CLAUDE.md` linha 11 (status narrative) | 1 bloco | ~6000 chars | Por plano grande | ❌ Nenhum |
| `docs/maturity-assessment.md` | 1 file | 250 LOC | A cada cell-2-activation / sprint major | ❌ Nenhum |
| `docs/plans/*.md` | 7 active | ~1500-2500 LOC each | Por commit durante execução | ❌ Nenhum |
| `docs/meetings/adr/*.md` | 57 | ~500 LOC each | Por meeting (~weekly) | ❌ Nenhum |
| `docs/meetings/records/*.md` | 68 | ~800 LOC each | Por meeting | ❌ Nenhum |
| `docs/runbooks/*.md` | 47 | ~300-1000 LOC each | Por CHG ou incident | ❌ Nenhum |
| `docs/learnings/*.md` | 1 | ~500 LOC | Raramente (underused) | ❌ Nenhum |
| `docs/reports/*.md` | 3 | ~800 LOC each | Por audit | ❌ Nenhum |
| `MEMORY.md` index + memory/*.md | 36 entries | ~100-300 LOC each | Por sessão | ❌ Nenhum |
| `docs/architecture/{domain}/` | 6 domains | C4 + deep-dives | Por arquitetural change | ❌ Nenhum |

**Total surface:** 297 .md files. 87 contém keywords de status-claim. **Zero arquivos têm TTL declarado**.

## The 6 Bias Patterns Observed in This Sample

Baseline empírica colhida durante a conversa de 2026-05-11:

### 1. Counting Bias (observed)
- Plano `deep-review-2026-05-10-remediation-plan.md` reportou "29/30 ✅" sem distinguir tasks com 3 evidências vs tasks com apenas commit
- T5.8 deferida não foi descontada do count "29/30"
- Stakeholder externo lendo "29/30" assume 97% completion; realidade tem 1 task deferida explicitamente + nenhuma com evidence file

### 2. Aggregation Bias (observed)
- Maturity score reportado como `3.9 / 5.0`. Média aritmética das 7 dimensões: (4.5+4.2+3.8+3.5+3.5+3.4+3.6)/7 = **3.79** (round-up para 3.9)
- Product Completeness 3.4 e Scale/MT 3.5 (com zero external users) ficam mascarados pelo Core Deploy 4.5

### 3. Recency Bias (observed)
- CLAUDE.md linha 11 menciona "build mode dispatch" (2026-05-08) e "dogfood 2026-05-09" 5+ vezes cada
- AppSecret CRD unwired mencionado 1 vez como "Known issues"
- 10 failing tests em `api/internal/k8s` mencionados 1 vez

### 4. Self-Attestation Bias (observed)
- `maturity-assessment.md` escrito pelo team engineering. Nenhum revisor externo
- Score 3.9 representa "what team thinks", não "what audit would find"
- CHG-2026-04-27-061 (Sprint 8 split) tentou prevenir self-attestation institucionalmente mas o pattern retornou em narrative documentation

### 5. Documentation Lag Bias (observed)
- `maturity-assessment.md` última atualização 2026-05-03 (8 dias antes do capture)
- Material novo (cleanroom 12/12, canary 80/80, build-mode-dispatch, deep-review 29/30) acumulado mas **NÃO absorvido no score**
- Resultado: score 3.9 documentado é entre v2026-05-03 e ~v2026-05-08 — temporally incoherent

### 6. "Implemented but Not Wired" (CLAUDE.md acknowledges)
- AppSecret CRD definido mas controller não implementado
- ScalingResolver pré-B40: existia mas Submit não invocava antes do enforceScaling
- CLAUDE.md table "Known Systemic Patterns" lista esse pattern como #1 — auto-awareness sem defesa material

## Concrete Drift Events

### Event 1: 19 vs 6 File Cross-References (Captured Mid-Plan)

Em `status-validation-and-doc-consolidation-plan.md` v1.0, task T4.2 afirmou:
> "19 files reference maturity-assessment.md (grep beforehand)"

Grep real (`grep -rln "maturity-assessment" docs/ CLAUDE.md README.md`) retornou **6 files**:
1. `docs/strategy/resilience-roadmap.md`
2. `docs/plans/chaos-mesh-experiments-plan.md`
3. `docs/plans/status-validation-and-doc-consolidation-plan.md` (the plan itself — self-reference)
4. `docs/meetings/records/MEET-2026-04-27-061.md`
5. `CLAUDE.md`
6. `README.md`

Inflação de **3.16x** dentro do próprio plano que se propõe a corrigir viés. **Meta-evidência empírica de drift**.

### Event 2: Maturity Score Drift +0.4 in 30 min

Agent reportou inicialmente:
> "Material acumulado [pós-2026-05-03] justifica bump empírico para 4.1-4.3"

Confrontado, retraiu para:
> "3.7-3.8 honesto é mais defensável"

Mesmo agente, mesma sessão, **0.4 pontos de delta**. Numerically equivalent to "between competent and incompetent" em assessment de maturity FAANG-grade.

## Architectural Gaps (Mapped for Mode 3 PROPOSED)

| Gap | AS-IS State | Mode 3 PROPOSED |
|-----|-------------|-----------------|
| Evidence requirement | No formal schema | 3-tier (T1/T2/T3) per evidence-bar.md |
| TTL on claims | None | 30/60/90 day tiers per decay-policy.md |
| Adversarial review | None | 3 personas with independence verified |
| Validation script | None | infra/scripts/governance/validate-evidence.sh |
| Decay scanner | None | infra/scripts/governance/scan-decay.sh + CI weekly |
| Single source of truth | Scattered 5+ surfaces | docs/STATUS.md canonical |
| Pre-merge gate | None | .github/workflows/status-pr-gate.yml |
| Bias detection | Implicit awareness only | infra/scripts/governance/bias-check.sh + taxonomy |
| CI continuous loop | None | 3 workflows (decay/cross-validation/spotcheck) |
| Architectural domain | 6 domains exist (no governance) | docs/architecture/governance/ added |

## Recommendation

**Proceed with plan execution.** AS-IS state cannot self-correct — every defense designed today (review meetings, peer review, CLAUDE.md rules) has been demonstrated to fail under self-attestation. Material defenses (scripts, gates, CI) are the only path forward.

Mode 3 PROPOSED architecture will be generated in T6.5 of plan, in `diff/` subdirectory, ready for promotion after `/dogfood full` passes.

## Cross-References

- ADR D8 — diagram tool dual-track (PlantUML + Excalidraw)
- `docs/governance/bias-taxonomy.md` — formal definition of 6 patterns observed here
- `docs/governance/evidence-bar.md` — proposed 3-tier schema
- `docs/governance/decay-policy.md` — proposed 30/60/90 TTL
- `docs/plans/status-validation-and-doc-consolidation-plan.md` v1.2 — the plan
- `docs/plans/status-validation-and-doc-consolidation-edge-cases.md` — edge case review documenting the 4 MUST FIX items
