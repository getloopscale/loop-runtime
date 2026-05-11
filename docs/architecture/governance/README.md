# Documentation Governance — Architecture Domain

**Status:** Canonical for governance domain architecture. Consolidates Mode 2 (AS-IS) + Mode 3 (PROPOSED) deep-dives.
**TTL:** 60 days (architectural)
**Last validated:** 2026-05-11
**Related:** [`docs/GOVERNANCE.md`](../../GOVERNANCE.md) (policies) · [`docs/STATUS.md`](../../STATUS.md) (current scores) · ADR-0051

---

## Purpose

This domain documents the **information architecture of LoopZero's maturity status governance system**. Different from other domains (`application`, `tenant`, `secrets`, etc.) which describe software components, this domain describes the **meta information system** about the project itself.

A conversa de 2026-05-11 expôs que docs de status estavam espalhadas em 5+ surfaces (CLAUDE.md, maturity-assessment.md, plans/, meetings/, runbooks/, memory/). Sem domain dedicado, ninguém tinha visão de hub-and-spoke. Sem visão hub-and-spoke, drift inevitável.

---

## §1. Mode 2 — AS-IS (Pre-Plan State, Captured 2026-05-11)

### Executive Summary

Documentação de status do LoopZero estava **funcionalmente correta mas epistemicamente vulnerável**. Funcionalmente: cada surface cumpria seu papel imediato. Epistemicamente: não havia nada que impedisse drift, viés, ou stale folklore. A sessão de 2026-05-11 demonstrou empiricamente que **o próprio agente Claude com 1M context viesou +0.4 pontos de maturity em <30 minutos** — prova material de que awareness implícita não funciona.

### Surface Inventory (Quantificada)

| Surface | Count | Tamanho aproximado | TTL declarado |
|---------|------:|--------------------:|---------------|
| `CLAUDE.md` linha 11 (status narrative) | 1 bloco | ~6000 chars | ❌ Nenhum |
| `docs/maturity-assessment.md` | 1 file | 250 LOC | ❌ Nenhum |
| `docs/plans/*.md` | 7 active | ~1500-2500 LOC each | ❌ Nenhum |
| `docs/meetings/adr/*.md` | 57 | ~500 LOC each | ❌ Nenhum |
| `docs/meetings/records/*.md` | 68 | ~800 LOC each | ❌ Nenhum |
| `docs/runbooks/*.md` | 47 | ~300-1000 LOC each | ❌ Nenhum |
| `docs/learnings/*.md` | 1 | ~500 LOC | ❌ Nenhum |
| `MEMORY.md` + memory/*.md | 36 entries | ~100-300 LOC each | ❌ Nenhum |

**Total surface:** 297 .md files. 87 contém keywords de status-claim. **Zero arquivos tinham TTL declarado**.

### 6 Bias Patterns (observed empirically)

1. **Counting bias:** Plano `deep-review-2026-05-10-remediation` reportou "29/30 ✅" sem distinguir tasks com 3 evidências vs apenas commits
2. **Aggregation bias:** Score reportado como `3.9 / 5.0`. Média aritmética: (4.5+4.2+3.8+3.5+3.5+3.4+3.6)/7 = **3.79** (round-up)
3. **Recency bias:** CLAUDE.md menciona "build mode dispatch" (2026-05-08) 5+ vezes; AppSecret CRD unwired mencionado 1x
4. **Self-attestation bias:** `maturity-assessment.md` escrito pelo team que construiu o sistema; nenhum revisor externo
5. **Documentation lag:** `maturity-assessment.md` última atualização 2026-05-03 (8 dias antes do capture); 4 frentes acumuladas não absorvidas
6. **"Implemented but not wired":** `AppSecret` CRD definido sem controller; CLAUDE.md auto-admite mas sem defesa material

### Concrete Drift Events

**Event 1: 19 vs 6 File Cross-References (Mid-Plan)**

Plan v1.0 T4.2 afirmou "19 files reference maturity-assessment.md". Grep real retornou **6** files. Inflação 3.16× dentro do próprio plano que se propõe a corrigir viés.

**Event 2: Maturity Score Drift +0.4 in 30 min**

Agente reportou "Material acumulado justifica bump para 4.1-4.3" → confrontado, retraiu para "3.7-3.8 honesto" → 0.4 ponto delta no mesmo agente, mesma sessão.

### Architectural Gaps Identified

| Gap | AS-IS State |
|-----|-------------|
| Evidence requirement | No formal schema |
| TTL on claims | None (zero arquivos) |
| Adversarial review | None |
| Validation script | None |
| Decay scanner | None |
| Single source of truth | Scattered 5+ surfaces |
| Pre-merge gate | None |
| Bias detection | Implicit awareness only |
| CI continuous loop | None |
| Architectural domain | 6 domains exist (no governance) |

---

## §2. Mode 3 — PROPOSED (Post-Implementation, 2026-05-11)

### Deltas from AS-IS

| Métrica | AS-IS | PROPOSED | Δ |
|---------|------:|---------:|---:|
| Status surfaces | 5+ (CLAUDE.md, maturity, plans, meetings, runbooks, memory) | 1 (STATUS.md) | -4+ |
| Claims with TTL | 0 / 348 (0%) | 348 / 348 (100%) | +348 |
| Claims with evidence schema | 0 / 348 | Target 50-80 (PARTIAL while Phase 3 pending) | +50-80 |
| Validation scripts | 0 | 14 | +14 |
| CI governance workflows | 0 | 5 | +5 |
| Pre-merge gates | 0 | 1 (STATUS.md edits) | +1 |
| Adversarial personas | 0 | 3 (Vercel/VC/SRE) with independence | +3 |
| Architecture docs (governance domain) | 0 | 4 (this README + 3 PlantUML C4 + 5 Excalidraw) | +4 |
| CLAUDE.md status section size | ~6000 chars | ~470 chars | -92% |
| maturity-assessment.md role | Canonical (stale) | Redirect | -change |
| Total governance md files | (scattered) | 4 canonical docs | -90% |

### Behavior Changes

| Behavior | AS-IS | PROPOSED |
|----------|-------|----------|
| Claim added to STATUS | git commit, no checks | Pre-merge gate validates evidence file delta |
| Stale claim detection | Manual / never | Weekly CI decay-scan + auto issue |
| Score recalibration | Self-attestation prose | 3 personas blind → triangulation → meeting if std-dev > 0.3 |
| Bias detection | Implicit awareness | Material script `bias-check.sh` (6 patterns) |
| Evidence verification | Trust by default | Explicit 3-source schema (T1/T2/T3) + validator |
| Confidence claim | "3.9/5.0" with no methodology | "median(p1,p2,p3) with std-dev, capped 90-97%" |

### Material Defense Layers (8 Total)

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

---

## §3. Concept Diagrams (Excalidraw — 5)

Conceptual arguments about why the design is what it is. Open in https://excalidraw.com or VSCode Excalidraw extension.

| File | What it visualizes |
|------|---------------------|
| [`concept-bias-taxonomy.excalidraw`](concept-bias-taxonomy.excalidraw) | 6 viéses em hexagon → Status Drift sumidouro central |
| [`concept-decay-lifecycle.excalidraw`](concept-decay-lifecycle.excalidraw) | State machine Created→Fresh→Aging→Expired→Revalidated + decision diamond + YAML artifact |
| [`concept-evidence-triangulation-pipeline.excalidraw`](concept-evidence-triangulation-pipeline.excalidraw) | Central plan diagram: claim → evidence (3 sources) → adversarial → STATUS.md → continuous loop (4 sections multi-zoom) |
| [`concept-status-md-information-architecture.excalidraw`](concept-status-md-information-architecture.excalidraw) | Hub-and-spoke radial: STATUS.md center + 3 rings (mandatory/auto-gen/CI feedback) + anti-pattern sidebar |
| [`concept-independence-matrix.excalidraw`](concept-independence-matrix.excalidraw) | 3x3 matrix (P1/P2/P3 × model/temp/seed) + counter-example panel showing collapse if independence ignored |

---

## §4. C4 Diagrams (PlantUML — 6 files in 2 modes)

Formal structural diagrams. Render via `plantuml -check *.puml` or kroki.

### Mode 2 — AS-IS (this directory)

- [`c4-context.puml`](c4-context.puml) — System Context (6 readers + 6 surfaces + drift annotations)
- [`c4-container.puml`](c4-container.puml) — Container view (10 containers, AS-IS scattered)
- [`c4-component.puml`](c4-component.puml) — Component view (claim lifecycle, 0 validation layers)

### Mode 3 — PROPOSED (in `diff/`)

- [`diff/c4-context.puml`](diff/c4-context.puml) — Post-implementation context (STATUS.md hub)
- [`diff/c4-container.puml`](diff/c4-container.puml) — 10 containers, 8 NEW vs AS-IS
- [`diff/c4-component.puml`](diff/c4-component.puml) — Claim lifecycle with 8 validation layers

**Promotion:** `diff/` files promote to this directory after `/dogfood full` PASS. Until then, AS-IS preserved alongside PROPOSED for audit trail.

---

## §5. Path Forward

| Phase | Status | Blocker |
|-------|--------|---------|
| Phase 0 (Foundation) | ✅ DONE | — |
| Phase 1 (Inventory) | ✅ DONE — 348 claims cataloged | — |
| Phase 2 (Code-Truth) | ✅ DONE (lightweight) | Full `/cross-validation`, `/dead-code`, `/code-audit` skill runs available to operator if more depth needed |
| Phase 3 (Adversarial) | ✅ DONE LIVE 2026-05-11 — median 2.84, 4 dims flagged for reconciliation | Operator T3.3 reconciliation for 4 flagged dims |
| Phase 4 (Single SoT) | ✅ DONE | — |
| Phase 5 (CI Loop) | ✅ DONE | — |
| Phase 6 (Governance) | ✅ DONE | — |
| Dogfood Gate | ⏳ Pending | Requires cleanroom — operator runs `task teardown && task setup && tilt up && /dogfood full` (30-45min) |
| Promote diff/ → parent | ⏳ Pending | After Dogfood Gate PASS |

---

## §6. Risk Acceptance

Following risks explicitly accepted per ADR-0051:

| Risk | Acceptance reason |
|------|-------------------|
| Cross-vendor codex unavailable in some CI runners | Fallback: 3 Claude models + temp/seed variation. Cap downgrades 95-97% → 90-95%. Logs which dims varied. |
| `bias-check.sh` FP rate ~30% (target <20%) | Calibration deferred. WARN level until tightened. Meta-discussion plans expected FP source. |
| Operational cost ~$30-60 setup + $15-25/mo | Documented in plan EC-12; cost is gating for adversarial runs only |
| 1+ dim has only 2 evidence sources (PARTIAL_CONFIDENCE) | Plan D1 allows — flagged with asterisk, not counted toward score |
| Decay scanner issue automation noise | Mitigation: only opens issue when expired_count > 0; single update-or-create pattern |

---

## §7. Material Proof-of-Concept

**`docs/evidence/tests-confidence/2026-05-11-validate-evidence-script.md`** is the first real EVIDENCED claim file. It passes `validate-evidence.sh` with 3 distinct refs (T1 commit+test, T2 LIVE log, T3 CI workflow). This proves the end-to-end pipeline works: extract → classify → decay-init → validate → file → STATUS.md link.

Without this proof, the entire system would be speculation. With it, the contract is materially established.

---

## §8. Cross-References

- **Policies:** [`docs/GOVERNANCE.md`](../../GOVERNANCE.md)
- **Current status:** [`docs/STATUS.md`](../../STATUS.md)
- **Canonical ADR:** [`docs/meetings/adr/0051-claim-evidence-decay-contract.md`](../../meetings/adr/0051-claim-evidence-decay-contract.md)
- **Plan:** [`docs/plans/status-validation-and-doc-consolidation-plan.md`](../../plans/status-validation-and-doc-consolidation-plan.md)
- **Live Phase 3 outputs:** `docs/governance/scoring-2026-05-11/`
