# Edge Case Review — status-validation-and-doc-consolidation

**Data:** 2026-05-11
**Plano:** `docs/plans/status-validation-and-doc-consolidation-plan.md` v1.0 (1933 linhas, 31 tasks, 7 fases)
**Tasks analisadas:** 31
**Edge cases encontrados:** 15 (MUST FIX: 4, SHOULD TEST: 7, DOCUMENT: 4)

---

## MUST FIX

### EC-1: 3 personas Claude = mesma família de modelo → erros correlacionados → matemática de 99% quebra

- **Task afetada:** T3.1 (Run 3 Personas Independently)
- **Família:** Concurrency (independence assumption)
- **Cenário:** O ADR D1 do plano deriva confidence 99% da matemática `1−(0.1)³ = 99.9%`. Esse cálculo **assume independência**. Mas T3.1 invoca 3 personas como `Agent({subagent_type: "general-purpose"})` — provavelmente todas instâncias do mesmo modelo Claude com a mesma training data. Errors correlacionados: se o modelo tem viés sistemático sobre LoopZero (overconfidence em "shipped" features, viés de recency embedded em training corpus), as 3 personas erram **juntas**. A probabilidade real de erro conjunto é muito acima de (0.1)³.
- **Impacto:** G1 do plano (99% confidence) é matematicamente falso. Score reportado parece triangulado mas tem mesmo viés do single-agent — só com 3× custo.
- **Fix sugerido:** Adicionar T3.0 (Independence Hardening) ANTES de T3.1 com 3 sub-tasks: (a) usar 2+ modelos diferentes (ex: P1=opus, P2=sonnet, P3=haiku ou GPT via Codex skill); (b) prompts com seeds explícitos diferentes para reduzir convergência; (c) ADR D1 atualizado: confidence cap = **95-97%** (não 99%) reconhecendo correlação residual de model family.

### EC-2: T4.2 reporta "19 files reference maturity-assessment.md" mas grep real retorna **6 files**

- **Task afetada:** T4.2 (Migrate `maturity-assessment.md` to History)
- **Família:** Input (planning bias — incorrect prior count)
- **Cenário:** O plano afirma "19 files reference maturity-assessment.md (grep beforehand)" no deep-file-dependency. Grep real (`grep -rl "maturity-assessment" docs/ CLAUDE.md README.md`) retorna **6 arquivos**: `docs/strategy/resilience-roadmap.md`, `docs/plans/chaos-mesh-experiments-plan.md`, `docs/plans/status-validation-and-doc-consolidation-plan.md` (esse próprio), `docs/meetings/records/MEET-2026-04-27-061.md`, `CLAUDE.md`, `README.md`. **Inflation de 3×.**
- **Impacto:** Meta-evidência de viés dentro do próprio plano de validação. Se o autor do plano (= eu, Claude) sobreestimou em 3× durante criação, isso valida a tese central do plano — e simultaneamente compromete sua execução se baseado no número errado.
- **Fix sugerido:** Em T4.2, substituir "19 files" por lista concreta de 6 arquivos enumerada. Executar `grep -rln "maturity-assessment" docs/ CLAUDE.md README.md > docs/governance/maturity-assessment-refs-2026-05-12.txt` ANTES de migração como evidence file.

### EC-3: T1.1 extrai markdown code blocks/tables como claims reais → FP cascateia em todo Phase 2-3

- **Task afetada:** T1.1 (Claim Extraction Script)
- **Família:** Input (regex over-matching)
- **Cenário:** Este próprio plano contém literais como `"Score: 3.9/5.0"` dentro de blocos ```yaml e em quotes de exemplo. O extractor descrito em T1.1 (regex `Score: X.Y / 5.0`) vai capturar essas instâncias como claims reais. Pior: docs como `MEET-2026-04-27-061.md` citam scores em quotes de discussão de meeting (não claims). FP rate provável **>30%**, não os 15% esperados.
- **Impacto:** Catalog inicial inflado com falsas claims. T1.2 classifier (que requer evidence para T1/T2/T3) marca todas como UNEVIDENCED → T1.4 stale report explode com FP → T2 cross-validation gasta tempo investigando claims fantasmas.
- **Fix sugerido:** Adicionar sub-task T1.1.5 com 3 exclusões regex obrigatórias antes do `accept_claim()`: (a) skip lines dentro de blocos triple-backtick (`em código`); (b) skip linhas em tabelas markdown que aparecem em ≥2 cells da mesma row (proxy para "exemplo, não claim"); (c) skip linhas dentro de tags `<example>` ou após "Exemplo:"/"Example:"/"e.g.". Calibrar FP rate em fixture conhecida ANTES de rodar em docs/.

### EC-4: T1.3 `git log -1 --format=%aI` sem `--follow` retorna data errada para arquivos renomeados

- **Task afetada:** T1.3 (Decay Clock Initialization)
- **Família:** Format (git history walking)
- **Cenário:** O plano descreve heurística `last_validated = git_last_modified_date(claim.source.file)`. Implementação ingênua usa `git log -1 --format=%aI -- <file>`. Mas o repo LoopZero tem renames frequentes (`infra/helm/tenant-controller` deletado, `loopzero-controller` reorganizado, etc.). Sem `--follow`, git só vê a vida do arquivo a partir do rename atual — claims em arquivos renomeados aparecem como "recentes" mesmo que conteúdo seja de 6 meses atrás.
- **Impacto:** Decay clock subestima staleness. Claims que precisam de revalidação urgente não aparecem em T1.4 stale report.
- **Fix sugerido:** Em `scripts/init-decay-clock.py`, usar `git log -1 --follow --format=%aI -- <file>` (3 chars adicionados). Adicionar test RED: `test_handles_renamed_file` — arquivo renomeado mantém data original do conteúdo.

---

## SHOULD TEST

### EC-5: T0.1 — `last_validated` no futuro

- **Task afetada:** T0.1 (Evidence Bar Policy + Validator)
- **Teste sugerido:** `test_validator_rejects_future_timestamp` — frontmatter com `last_validated: 2099-01-01` retorna exit 1 com mensagem clara ("future timestamp not allowed").

### EC-6: T0.1 — mesma ref aparece em T1+T2+T3 slots (não independente)

- **Task afetada:** T0.1
- **Teste sugerido:** `test_validator_rejects_duplicate_evidence_refs` — yaml com `evidence: [{type:T1, ref:X}, {type:T2, ref:X}, {type:T3, ref:X}]` retorna exit 1 ("evidence refs must be distinct"). Sem isso, 99% confidence é trivialmente burlável.

### EC-7: T3.2 — std-dev exatamente = 0.3 (boundary)

- **Task afetada:** T3.2 (Triangulation Analysis)
- **Teste sugerido:** `test_stddev_boundary_inclusive` — 3 scores [3.5, 3.8, 3.5] dão std-dev≈0.17 (não-flag), mas [3.5, 4.0, 3.5] dão std-dev≈0.29 (não-flag), [3.5, 4.1, 3.5] dão std-dev≈0.35 (flag). Especificar `flagged = std_dev > 0.3` (estritamente maior) e testar valor exato 0.3.

### EC-8: T3.2 — 1 persona output malformado (parse fail)

- **Task afetada:** T3.2
- **Teste sugerido:** `test_triangulation_handles_one_malformed` — se p1.yaml não parsea, output deve ser ERROR (não silently median-of-2). Plano define triangulação como 3-source; 2-source é violação do contrato.

### EC-9: T2.2 — deadcode falha porque `api/internal/k8s` tem 10 testes failing (compile error?)

- **Task afetada:** T2.2 (Dead Code & Wiring Audit)
- **Teste sugerido:** `test_deadcode_handles_partial_build_failure` — se `go build ./...` retorna non-zero em algum package, skill emite report parcial + lista de packages skipped (não silent failure). CLAUDE.md documenta 10 failing tests em `api/internal/k8s` — risco real.

### EC-10: T5.3 — random selection sem seed declarado

- **Task afetada:** T5.3 (Nightly Adversarial Spot-Check)
- **Teste sugerido:** `test_spotcheck_reproducible_with_seed` — `SPOTCHECK_SEED=42` produz seleção determinística (mesmo dim + mesma persona). Sem seed visível em log, debug de "por que essa dim foi flagged ontem?" é impossível.

### EC-11: T4.4 — auto-gen idempotency quando arquivo é editado por humano entre auto-sections

- **Task afetada:** T4.4 (Auto-Generated Sections)
- **Teste sugerido:** `test_regenerate_preserves_human_edits_after_auto_section` — fixture: STATUS.md com `<!-- AUTO:START -->...<!-- AUTO:END -->` + texto humano DEPOIS do bloco AUTO. Regenerate rodando 2x não toca o texto humano. Especificar delimitadores ainda mais resilientes que comentários HTML pode ser necessário (alguns renderers strippam).

---

## DOCUMENT

### EC-12: Cost budget Phase 2-3 — agent invocations + meetings

- **Risco aceito:** Phase 2 invoca `/cross-validation` × 7 plans + `/dead-code` × 2 (Go+TS) + `/code-audit` (9 técnicas). Phase 3 invoca 3 personas + 0-7 meetings (1 por flagged dim) + T5.3 30 runs/mês perpetuamente. Estimativa rough: **~$30-60 setup inicial + ~$15-25/mês ongoing** (Anthropic API pricing at Opus rates). Aceitável dado o impacto na confiabilidade do score, mas operador precisa saber o número antes de aprovar. Documentar em STATUS.md (ou ADR T6.1) como "operational cost of evidence integrity".

### EC-13: Skill version pinning — sem freeze, output format changes quebram CI

- **Risco aceito:** T5.1 pin de versão de `/cross-validation` skill mencionado mas nenhuma task captura versão *atual* como baseline. Aceitar que primeiro CI run estabelece baseline; quaisquer skill updates futuros precisam re-pin manual. Documentar em T6.1 ADR.

### EC-14: T6.3 plan template DoD evidence checkbox — condicional por escopo

- **Risco aceito:** Plan template update adiciona "Evidence file in `docs/evidence/`" como DoD checkbox. Mas plans não-score-affecting (refactors internos, bug fixes operacionais) não precisam. Documentar que checkbox é CONDICIONAL: "marcar N/A se plan não toca STATUS.md". Sem essa nota, todo plano carrega fricção desnecessária. Sugestão: adicionar como bullet no T6.3 deep-dive.

### EC-15: T4.3 CLAUDE.md shortening é breaking change para sessões em flight

- **Risco aceito:** Encurtar status section de ~6000 chars → ~300 chars é breaking. Sessões Claude existentes podem ter contexto referenciando o texto antigo. Pre-release: aceitável (sem usuários externos). Pós-release: precisaria de migração coordenada. Documentar em T6.1 ADR como "breaking-but-bounded — pré-release window only".

---

## Padrões Sistêmicos Detectados

| Padrão | Encontrado? | Onde |
|--------|:-----------:|------|
| Implemented but not wired | Não | (Plano é meta-docs; sem código de produto) |
| Correct code in wrong place | Não | — |
| Project name vs project ID | N/A | — |
| ArgoCD `notifiers` vs `services` | N/A | — |
| Single ArgoCD App per tenant | N/A | — |
| CF scan imports conflicting apex | N/A | — |
| **Self-attestation bias (novo, detectado em EC-2)** | **Sim** | **T4.2 inflated count 19 vs real 6** |
| **Planning regex over-match (novo, EC-3)** | **Sim** | **T1.1 vai capturar exemplos como claims** |
| **Independence assumption shaky (novo, EC-1)** | **Sim** | **T3.1 mesma model family ≠ independent** |

**3 padrões novos identificados nessa review** que merecem entrada futura no `Known Systemic Patterns` table do CLAUDE.md após este plano executar.

---

## Resumo

| Task | Edges encontrados | MUST FIX | SHOULD TEST | DOCUMENT |
|------|:-----------------:|:--------:|:-----------:|:--------:|
| T0.1 | 2 | 0 | 2 (EC-5, EC-6) | 0 |
| T1.1 | 1 | 1 (EC-3) | 0 | 0 |
| T1.3 | 1 | 1 (EC-4) | 0 | 0 |
| T2.2 | 1 | 0 | 1 (EC-9) | 0 |
| T3.1 | 1 | 1 (EC-1) | 0 | 0 |
| T3.2 | 2 | 0 | 2 (EC-7, EC-8) | 0 |
| T4.2 | 1 | 1 (EC-2) | 0 | 0 |
| T4.3 | 1 | 0 | 0 | 1 (EC-15) |
| T4.4 | 1 | 0 | 1 (EC-11) | 0 |
| T5.3 | 1 | 0 | 1 (EC-10) | 0 |
| T6.1 (cross-cut) | 1 | 0 | 0 | 1 (EC-12) |
| T5.1 | 1 | 0 | 0 | 1 (EC-13) |
| T6.3 | 1 | 0 | 0 | 1 (EC-14) |
| **Total** | **15** | **4** | **7** | **4** |

---

## Veredicto: **PLANO PRECISA DE AJUSTE**

4 MUST FIX devem ser incorporados como sub-tasks no plano antes da execução começar. Em particular:

- **EC-1 é o mais crítico** — sem hardening de independência, o entregável central do plano (G1: 99% confidence) é matematicamente inválido. Sem fix, plano executado entrega "score triangulado com mesmo viés do single-agent". A solução simples (variar modelos OU temperatures OU seeds) + ajuste de ADR D1 capeando confidence em 95-97% mantém integridade matemática.

- **EC-2 é meta-evidência da tese do próprio plano** — fato de eu (autor) ter inflado contagem 19 vs real 6 mostra que viés operacional persiste mesmo em conversação focada em viés. Fix trivial (substituir prosa por lista enumerada de 6 arquivos + commit como evidence file).

- **EC-3 cascateia** — se T1.1 tem FP rate >30% (vs 15% esperado), Phases 1-4 inteiras trabalham com catalog corrompido. Triple-backtick exclusion + tabela exclusion + `<example>` exclusion é 3 linhas de Python.

- **EC-4 é one-liner** — adicionar `--follow` ao git log.

Os 7 SHOULD TEST são testes RED adicionais nos blocos TDD existentes (15-30 linhas extras no plano). Os 4 DOCUMENT são notas curtas nos deep-dives correspondentes.

**Estimativa de overhead para incorporar fixes:** ~2 horas de edição no plano + recalibração de ADR D1 (cap confidence em 95-97% vs 99%). Esse ajuste é essencial — sem ele, plano entrega "validação" com mesmo viés que tenta corrigir.

---

`★ Insight Educacional ─────────────────────────`
- O edge case mais valioso identificado (**EC-1**) é também o mais filosófico: a matemática `1−(1−p)ⁿ = confidence` só é válida sob *independência*. Estatística básica, mas frequentemente esquecida em pipelines de validação multi-agent. Em sistemas distribuídos isso aparece como "redundância correlacionada" (3 réplicas no mesmo rack ≠ 3 réplicas em 3 regiões). Aplicado a LLM agents, "mesma model family" é equivalente a "mesmo rack" — falham juntos.
- O **EC-2 é um "mirror moment"**: o plano se propõe a corrigir viés de auto-atestação e simultaneamente exibe o mesmo viés (inflação 19→6). Não é falha do autor — é demonstração empírica de que defesas implícitas (consciência) não funcionam. Por isso o plano correto não confia em mim para "ter cuidado"; ele coloca scripts e gates como defesa material.
- O **EC-3 é o trade-off clássico recall-vs-precision em extraction**: regex amplo captura mais (alto recall) com custo de FP (baixa precision). Em status validation, FP é particularmente caro porque cada FP gera trabalho downstream (T2 cross-validation gasta tempo investigando claim fantasma). Em geral, prefer-se precision > recall para fontes-de-verdade. Os 3 filtros sugeridos (code-blocks, tables, examples) são heurísticas básicas mas comprovadamente reduzem FP em extraction tasks similares.
`─────────────────────────────────────────────────`
