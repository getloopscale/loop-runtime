# Plan: Status Validation & Documentation Consolidation (95-97% Confidence)

> **Version 1.2** — Resolve a divergência sistêmica entre status documentado e realidade do sistema através de um pipeline de validação evidence-driven com triangulação adversarial. O objetivo é estabelecer um único arquivo canonical de status (`docs/STATUS.md`) onde toda claim é (a) suportada por evidência verificável, (b) datada com TTL de decay, e (c) triangulada por ≥3 reviewers adversariais com **independência verificada**. A confiabilidade target é **95-97%** (cap honesto considerando correlação residual de model family). Conceitos abstratos do plano (viéses, decay lifecycle, triangulação) ganham **diagramas visuais Excalidraw** que argumentam o desenho, não só o ilustram. Pré-requisito explícito antes de Sprint 9 (Polish + Launch) ou qualquer customer pitch.
>
> **v1.2 changelog (post user feedback — usar skill excalidraw):** 4 novas tasks adicionam diagramas Excalidraw em pontos de alta densidade conceitual:
> - **T0.5b** — Bias Taxonomy + Decay Lifecycle (visualiza os 6 viéses e o ciclo de vida de uma claim)
> - **T3.0b** — Independence Matrix (visualiza as 3 dimensões variáveis: model/temp/seed × P1/P2/P3)
> - **T4.0** — Evidence Triangulation Pipeline (argumento visual central do plano: claim → 3 fontes → STATUS.md)
> - **T4.6** — STATUS.md Information Architecture (visualiza relação entre STATUS.md, evidence files, scorecards, decay)
> - Novo **ADR D8** clarifica trade-off PlantUML/C4 (formal) vs Excalidraw (conceitual)
>
> **v1.1 changelog (post `/edge-case-plan` review):** 4 MUST FIX incorporated:
> - **EC-1:** ADR D1 confidence cap revisado de 99% → 95-97%; nova task T3.0 (Independence Hardening) variando modelos/temperatures/seeds entre P1/P2/P3
> - **EC-2:** Sub-task T4.2.0 adiciona enumeração concreta dos 6 cross-refs (não 19 como v1.0 inflou)
> - **EC-3:** Sub-task T1.1.5 adiciona 3 regex exclusions obrigatórias (code-blocks, tables, examples) antes de aceitar claim
> - **EC-4:** T1.3 script usa `git log -1 --follow --format=%aI` (3 chars adicionais) para tracking de renames
>
> 7 SHOULD TEST + 4 DOCUMENT items documentados em `status-validation-and-doc-consolidation-edge-cases.md`.

## Context

A conversa de **2026-05-11** demonstrou empiricamente que o sistema de status documentado é vulnerável a viés:
- O agente Claude (com 1M context + acesso total ao codebase) viesou **+0.4 pontos** em <30 minutos somando ganhos sem somar dívidas correspondentes
- Score oficial `3.9/5.0` em `docs/maturity-assessment.md` é de **2026-05-03** (8 dias stale)
- 4 frentes pós-baseline (cleanroom 12/12, canary 80/80, build-mode-dispatch, deep-review 29/30) **não foram absorvidas**
- 7 dimensões somadas dão média aritmética **3.79** — o 3.9 declarado já é round-up

**Superfície documental atual (mapeada 2026-05-11):**

| Surface | Count | Risco |
|---------|------:|-------|
| `docs/*.md` total | 297 | Surface grande, sem TTL |
| Files com keywords de status (score/maturity/GREEN/DONE/✅) | 87 | 30% da surface carrega claims |
| `docs/plans/*.md` | 7 ativos | DoDs heterogêneos |
| `docs/meetings/adr/*.md` | 57 | Decisões — frescor variável |
| `docs/meetings/records/*.md` | 68 | Records, não revalidated |
| `docs/runbooks/*.md` | 47 | Procedimentos — drift de versão |
| `docs/learnings/*.md` | 1 | Sub-utilizado |
| `MEMORY.md` entries | 36 | Subjective |
| `CLAUDE.md` linha 11 (status narrative) | ~6000 chars | Duplica e desincroniza com `maturity-assessment.md` |

**Tipos de viés identificados (6, taxonomizados):**
1. **Counting bias** — "29/30 ✅" parece complete mas T5.8 é deferida; ✅ não distingue "código pronto" de "validado LIVE sem regressões"
2. **Aggregation bias** — média 7-dim esconde outliers (Product Completeness 2.9 mascarado por Deploy 4.7)
3. **Recency bias** — ganhos recentes super-vívidos; gaps antigos invisíveis
4. **Self-attestation bias** — autor da claim = juiz da claim (ADR-0016 §"Resolution" tentou prevenir e ainda assim acontece)
5. **Documentation lag bias** — claims de 2026-05-03 ainda citadas em 2026-05-11
6. **"Implemented but not wired"** — padrão sistêmico já em CLAUDE.md, só pego por dogfood real

**Evidência motivadora concreta:** durante a conversa do dia 2026-05-11, o agente reportou inicialmente "material acumulado justifica bump empírico para 4.1-4.3" e, ao ser confrontado, retraiu para "3.7-3.8 honesto". Mesma sessão, mesmo contexto, delta de 0.4. Sem ferramentas de proteção contra esse viés, qualquer score reportado a stakeholder externo carrega esse erro embedded.

## Objective

**Done = qualquer claim em `docs/STATUS.md` pode ser verificada em ≤2 minutos por humano externo, com evidência rastreável a artefato concreto (commit/log/CI run), e tem TTL de validade visível.**

Goals mensuráveis:
- **G1.** Score de maturity recalibrado com confidence **95-97%** (triangulação ≥3 fontes independentes verificada via T3.0; delta entre personas adversariais ≤0.3 pts/dimensão). Confidence cap de 95-97% reflete correlação residual entre fontes (mesma family de model agents, fixtures compartilhadas) — 99% seria matematicamente desonesto sem hardening adicional (cross-vendor models obrigatório para todas 3 personas).
- **G2.** Zero claims órfãs em `docs/STATUS.md` (toda claim → evidence file path)
- **G3.** 100% das claims com TTL visível; CI detecta stale (>30 dias sem revalidação)
- **G4.** `CLAUDE.md` linha 11 + `docs/maturity-assessment.md` redirecionam para `docs/STATUS.md` (zero duplicação de status)
- **G5.** Pre-merge gate: PR que altera score-affecting claim precisa updatar evidence file
- **G6.** CI workflow semanal: rerun cross-validation em todos planos abertos + decay scan + adversarial spot-check
- **G7.** `/dogfood full` GREEN pós-consolidação (no regression no produto)

## ADRs

### D1 — Evidence Triangulation (3 Sources + Independence Verification)
**Decision:** Toda claim com impacto em maturity score requer **3 fontes de evidência independentes**, cada uma classificada por tipo: (T1) commit + teste adicionado no mesmo commit, (T2) timestamp de LIVE run com log archived em `${HOME}/.loopzero/artifacts/` ou `docs/evidence/`, (T3) CI workflow run com URL verificável + status GREEN. As 3 fontes precisam ser refs **distintas** (mesmo file path em 3 slots viola independência).
**Rationale:** Matemática da redundância: cada fonte com p≥90% de ser correta isolada, *sob independência completa*, converge para 1−(0.1)³ = 99.9%. **Realisticamente alcançamos 95-97%** — não 99% — porque (a) testes e LIVE runs podem compartilhar fixtures, (b) CI invoca os mesmos testes em ambiente diferente (correlação ~0.3), (c) agentes Claude da mesma model family que avaliam evidência têm bias compartilhado. Cap de 95-97% é honesto e ainda 2-3 ordens de magnitude melhor que single-source.
**Consequences:** Claims com <3 fontes ficam em categoria *"claimed, not evidenced"* — separada, visível. Confidence reportado em `STATUS.md` cap em 97%, nunca 99-100% (proibido por design). Edge case EC-6: validator rejeita refs duplicados entre T1/T2/T3 slots.

### D2 — Single Source of Truth in `docs/STATUS.md`
**Decision:** `docs/STATUS.md` é o único arquivo canonical de status do sistema. `CLAUDE.md` linha 11 vira link curto (~3 linhas) referenciando STATUS.md. `docs/maturity-assessment.md` é renomeado para `docs/maturity-history/{YYYY-MM-DD}-snapshot.md` (read-only, datado).
**Rationale:** Múltiplos surfaces de status criam drift inevitável. Audit FAANG-grade requer single source. STATUS.md é versionado em git → toda mudança rastreável.
**Consequences:** Breaking change para quem lê CLAUDE.md como fonte de status. Mitigação: redirect explícito + 30 dias de coexistência durante migração.

### D3 — Decay TTL Per Claim Type
**Decision:** Toda claim em STATUS.md tem TTL visível. Política: **30 dias** para claims operacionais (test counts, LIVE validations), **60 dias** para claims de arquitetura (state machines, CRDs), **90 dias** para ADRs/decisões. Claim expirada vira `🔴 NEEDS REVALIDATION` automaticamente.
**Rationale:** Sem TTL, claims viram folclore. Decay força revalidação proativa. 30/60/90 calibrado por taxa empírica de drift observada (ex: test counts mudam toda semana; CRDs mudam por sprint; ADRs por trimestre).
**Consequences:** Workload contínuo de revalidação. Mitigação: CI automatiza o scan; humano só revalida o que CI marca expirado.

### D4 — Adversarial Personas (3 Reviewers with Verified Independence)
**Decision:** Recalibração de score usa **3 personas adversariais distintas** rodando em paralelo, blind para o score atual:
- **P1 — Vercel Due-Diligence Staff Engineer:** procura furos de DX/CLI/onboarding
- **P2 — Hostile VC Technical Auditor:** procura overclaim em pitch deck linguagem
- **P3 — Paranoid SRE / Production Readiness Reviewer:** procura "vai quebrar em produção"

**Independência verificada (T3.0 hardening per EC-1):** As 3 personas DEVEM rodar com **≥2 das 3 dimensões de independência variadas**:
- **Modelo:** opus vs sonnet vs haiku (ou cross-vendor via codex-rescue skill para 1 das 3)
- **Temperature/seed:** explicit seed diferente por persona (se model suporta)
- **System prompt:** persona files já são textualmente distintos (necessário, não suficiente)

Cada uma pontua 7 dimensões independentemente. Convergência: max delta ≤0.3 entre personas por dimensão. Se delta >0.3 → meeting de reconciliação obrigatório.

**Rationale:** Self-attestation bias só é defeitado por adversarial review. 3 personas com viéses opostos (entusiasmo DX vs ceticismo VC vs medo SRE) trianguam o "verdadeiro" score. Sem independence hardening, mesma model family produz scores correlacionados — viola premissa matemática de D1.

**Consequences:** Custo computacional alto (3 agentes review independente, possivelmente com 2 modelos diferentes). Mitigação: roda só na recalibração inicial + nightly spot-check de 1 dimensão random. Custo de cross-vendor (codex via API) absorvido no budget de EC-12 (~$30-60 setup + $15-25/mês).

### D5 — Evidence Files in Repo, Not External
**Decision:** Toda evidência mora em `docs/evidence/{dimension}/{claim-slug}.md` no próprio repo. Logs grandes (>1MB) ficam em `${HOME}/.loopzero/artifacts/` com referência ao path + checksum.
**Rationale:** Audit trail completo em git. Sem dependência de wiki externo, Notion, Google Docs (que evaporam). Checksum protege contra log tampering.
**Consequences:** Repo cresce ~50MB com evidências. Mitigação: rotação > 6 meses (move para `evidence-archive/` ou DO Spaces).

### D6 — Pre-Merge Gate on Score-Affecting Claims
**Decision:** PR que altera linha em STATUS.md tagged como `score-affecting` exige (a) link para evidence file novo OU atualizado, (b) ao menos um teste novo OU LIVE run timestamp atualizado, (c) approval label `evidence-verified` adicionada por reviewer diferente do author.
**Rationale:** Bloqueia drift no source. Sem gate, STATUS.md drift de volta para o estado atual em <30 dias.
**Consequences:** Fricção em PRs que tocam status. Aceitável — esse é exatamente o ponto de fricção que falta hoje.

### D8 — Diagram Tool Selection: PlantUML for Formal, Excalidraw for Conceptual
**Decision:** Diagramas no `docs/architecture/governance/` seguem duas trilhas paralelas: (a) **PlantUML/C4** para estrutura formal de containers e componentes (Mode 2 AS-IS + Mode 3 PROPOSED, conforme skill `architecture-docs`); (b) **Excalidraw** (`*.excalidraw` JSON via skill `excalidraw`) para argumentos conceituais que explicam *por que* o design existe — bias taxonomy, decay lifecycle, evidence triangulation pipeline, independence matrix, information architecture do STATUS.md.
**Rationale:** PlantUML é otimizado para "qual componente fala com qual" (structural truth). Excalidraw é otimizado para "diagrama argumenta visualmente uma decisão" — passa o **Isomorphism Test** (sem texto, estrutura ainda comunica) e o **Education Test** (leitor aprende algo concreto). Os conceitos centrais deste plano (viés, decay, triangulação) são abstratos demais para C4 e específicos demais para serem deixados só em prose. Excalidraw fica entre os dois.
**Consequences:** Custo de manutenção dual (2 toolchains de diagramas). Mitigação: PlantUML só para `c4-*.puml` (formal); Excalidraw só para `concept-*.excalidraw` (argumentativo). Convenção de naming força disjunção. Renderização: Excalidraw JSON renderiza diretamente em GitHub markdown previews quando referenciado por path; PlantUML precisa de extension/kroki.

### D7 — Bias Taxonomy as Active Defense
**Decision:** Os 6 viéses identificados (counting/aggregation/recency/self-attestation/lag/wired) viram **checklist obrigatório** em qualquer plano que afete score. Cada plan submission roda um *bias self-check* (script que escaneia o plan por padrões de cada viés).
**Rationale:** Awareness explícita > esperança implícita. Checklist consistente força detecção.
**Consequences:** Pequeno overhead em plan creation (~5 min). Mitigação: skill `to-plan` integra o check.

## Dependency Graph

```
Phase 0 ──▶ Phase 1 ──▶ Phase 2 ──▶ Phase 3 ──▶ Phase 4 ──▶ Phase 6
(Foundation) (Inventory) (Code-Truth) (Adversarial) (SoT)      (Governance)
   │                                       │              │
   │ T0.5b                                 │ T3.0b        │ T4.0
   │ (Excalidraw                           │ (Excalidraw  │ T4.6
   │  bias+decay)                          │  independ.)  │ (Excalidraw
   │                                       │              │  pipeline+IA)
   ▼                                       ▼              ▼
docs/architecture/governance/concept-*.excalidraw (4 diagrams)
                                                       │
                                                       ▼
                                                  Phase 5 (CI Loop)

Phase 0 — Defines rules. Blocks everything. T0.5b adds 2 conceptual diagrams.
Phase 1 — Catalogs current state. Parallel-safe with Phase 0 if rules pre-drafted.
Phase 2 — Code reality check. Depends on Phase 1 inventory.
Phase 3 — Adversarial blind scoring. T3.0 hardens independence; T3.0b visualiza matriz.
Phase 4 — Consolidation. T4.0 + T4.6 enclausuram STATUS.md em argumentos visuais.
Phase 5 — Automation. Depends on Phase 4 file structure existing.
Phase 6 — Governance hardening. Parallel-safe with Phase 5.

Excalidraw tasks (T0.5b/T3.0b/T4.0/T4.6) são parallel-safe DENTRO de suas fases —
podem iniciar assim que conceito subjacente está clear (early Phase 0 / mid Phase 3 / Phase 4 start).
```

**Critical path:** Phase 0 → 1 → 2 → 3 → 4 = **5 days minimum**. Phases 5+6 add 2 days. Excalidraw tasks add **~1.5 dias** distribuídos (não sequencial — overlap com phases pais).

**Total estimate:** 7-8 dias focados (vs 6-7 v1.1 — Excalidraw adiciona ~1 dia overhead líquido).

---

## Phase 0: Foundation — Rules, Personas, Policy

**Objective:** Estabelecer os contratos epistêmicos antes de tocar em qualquer doc. Sem foundation, fases seguintes drift.

### T0.1 — Evidence Bar Policy + Validator Script

#### Objective
Definir formalmente o que é "evidência aceitável" e criar validator script que classifica claims.

#### Evidence
Conversa 2026-05-11 expôs que claims tipo "X is implemented" não distinguem entre código mergeado vs LIVE-validated vs ainda só desenhado. Sem critério formal, classification é judgment do autor.

#### Files to edit
```
docs/governance/evidence-bar.md — (NEW) política formal de 3-tier
scripts/validate-evidence.sh — (NEW) validator que classifica claims em arquivo
docs/evidence/README.md — (NEW) convenção de naming + estrutura
```

#### Deep file dependency analysis
- `docs/governance/evidence-bar.md` é referenciado por todas fases subsequentes; mudanças aqui invalidam scoring
- `scripts/validate-evidence.sh` consumido por CI (Phase 5) e por dev local antes de commit
- `docs/evidence/README.md` define convenção que Phase 1 popular

#### Deep Dives
- **3 tipos de evidência:**
  - **T1 (Code+Test):** commit hash + `git show <hash> -- <test-file>` exit 0 + teste declara o comportamento da claim
  - **T2 (LIVE Run):** archived log em `docs/evidence/` ou `${HOME}/.loopzero/artifacts/` com timestamp + comando executado + output relevant
  - **T3 (CI Run):** GitHub Actions workflow URL + status conclusion=success + run ID
- **Claim → evidence mapping schema:**
  ```yaml
  claim_id: deploy-flow-e2e-passes
  dimension: Core Deploy Flow
  score_impact: 0.5
  evidence:
    - type: T1
      ref: commit:341181ae:api/internal/services/deploy_test.go
    - type: T2
      ref: docs/evidence/core-deploy/2026-05-05-cleanroom-12-12.md
    - type: T3
      ref: https://github.com/.../actions/runs/12345 (cell2-soaks.yml#42)
  ttl_days: 30
  last_validated: 2026-05-11
  expires: 2026-06-10
  ```
- **Edge case:** Claims sobre design ("arquitetura é coesa") não testáveis. Política: design claims movem para `docs/architecture/` não STATUS.md. STATUS.md só carrega claims testáveis.
- **Edge case:** Claim com 2 fontes — vai para *"partial confidence"* (60-90%), não entra no score oficial mas é citável com asterisco.

#### Tasks
1. Redigir `docs/governance/evidence-bar.md` com 3-tier + schema
2. Implementar `scripts/validate-evidence.sh` em Bash (parsing YAML frontmatter via `yq`)
3. Criar `docs/evidence/` directory + README convention
4. Adicionar `task validate:evidence` no Taskfile.yml

#### TDD
```
RED:     test_validator_rejects_zero_evidence_claim — yaml sem campo evidence → exit 1
RED:     test_validator_rejects_single_source_claim — só T1, sem T2/T3 → exit 1 (mas warn não-block)
RED:     test_validator_accepts_three_source_claim — T1+T2+T3 → exit 0
RED:     test_validator_detects_expired_ttl — last_validated + ttl_days < today → exit 2
RED:     test_validator_handles_missing_file — evidence ref aponta para arquivo inexistente → exit 1
GREEN:   Implementar scripts/validate-evidence.sh
REFACTOR: Extract YAML parsing para função separada (testável isoladamente)
VERIFY:  task validate:evidence path/to/sample.md
```

#### Acceptance Criteria
- [ ] `evidence-bar.md` cobre 3 tiers + schema + edge cases (design claims, partial confidence)
- [ ] `validate-evidence.sh` retorna exit codes 0/1/2 (pass/fail/expired)
- [ ] 5 testes integrados rodando via bats ou similar
- [ ] `task validate:evidence` integrado ao Taskfile
- [ ] Pass: shellcheck zero warnings
- [ ] Pass: doc tem exemplos para cada tier
- [ ] File ≤ 500 lines

#### DoD
- [ ] All tasks completed
- [ ] Validator script tested with sample inputs (5 scenarios)
- [ ] `task validate:evidence` exits 0 on valid sample, exits 1/2 on invalid
- [ ] shellcheck clean
- [ ] doc reviewed by team for completeness

---

### T0.2 — Adversarial Persona Library

#### Objective
Definir 3 personas adversariais reutilizáveis para scoring blind.

#### Evidence
Self-attestation bias documentado empiricamente na sessão 2026-05-11. Defesa = forçar perspectivas opostas a convergirem. 3 personas (DX entusiasta + VC cético + SRE paranoico) cobre o spectrum.

#### Files to edit
```
docs/governance/personas/vercel-dd-engineer.md — (NEW) persona P1
docs/governance/personas/hostile-vc-auditor.md — (NEW) persona P2
docs/governance/personas/paranoid-sre.md — (NEW) persona P3
docs/governance/personas/README.md — (NEW) como invocar
```

#### Deep file dependency analysis
- Cada persona file é prompt template consumido por Phase 3 (agent invocation)
- README documenta como rodar (`Agent({subagent_type: "general-purpose", prompt: load(persona_file) + scoring_target})`)
- Persona files versionados — versão referenciada em scoring output

#### Deep Dives
- **P1 — Vercel DD Engineer (DX bias):**
  - Background: 8 anos Vercel/Netlify/Railway
  - Foco: pode usuário deployar em <5min sem mentor? CLI tem stale spinner? `--json` quebra?
  - Penaliza: docs vagos, exit codes inconsistentes, error messages sem next action
- **P2 — Hostile VC Auditor (overclaim bias):**
  - Background: technical due-diligence em $10M+ rounds, foi queimado 3x
  - Foco: "shipped" significa demoed ou production-validated? quantos REAL users? gap entre pitch e código
  - Penaliza: "implemented but not wired" (não conta), zero external users (heavy hit), aggregation bias
- **P3 — Paranoid SRE (production-readiness bias):**
  - Background: 10y SRE FAANG, viu 4 incidents tier-1 cada
  - Foco: vai quebrar em produção? chaos coverage? failover testado? known-failing tests
  - Penaliza: testes failing (mesmo se "intentional"), zero chaos, runbooks stale, SLO sem alertas
- **Convergência rule:** Cada persona pontua 7 dimensões. Para cada dim, compute std-dev dos 3 scores. Se std-dev > 0.3 → meeting de reconciliação. Final score = mediana.
- **Edge case:** Persona pode "concordar" por razão errada (todas 3 dão 4.0 mas por causas diferentes). Mitigação: cada persona escreve 3 bullets justificando, comparison manual procura concordância semântica não só numérica.

#### Tasks
1. Redigir P1 prompt (~1500 palavras) com background + heurísticas + scoring rubric
2. Redigir P2 prompt
3. Redigir P3 prompt
4. Redigir README de invocação + convergência rule + edge cases

#### TDD
```
RED:     test_persona_files_parseable — cada .md tem sections: Background/Heuristics/Rubric/Output Schema
RED:     test_persona_output_schema_valid — output esperado é YAML com 7 scores 0-5 + 3 bullets justificativa
RED:     test_convergence_calculator — given 3 score sets, computa std-dev + flag se >0.3
GREEN:   Implementar via templates + small Python/Bash script para validação
REFACTOR: Extract output schema para schema.json reusável
VERIFY:  Run all 3 personas em sample dimension; verify output válido
```

#### Acceptance Criteria
- [ ] 3 persona files completos, ≥1200 palavras cada
- [ ] README documenta invocação + convergência
- [ ] Schema de output formalizado (YAML)
- [ ] Convergence calculator script testado
- [ ] Pass: shellcheck/markdownlint clean
- [ ] Files ≤ 500 lines

#### DoD
- [ ] All tasks completed
- [ ] Dry-run com 3 personas em 1 dimensão exemplo → output válido
- [ ] Convergence calculator detecta delta >0.3 corretamente
- [ ] README has clear invocation example

---

### T0.3 — Decay TTL Policy + Detector Script

#### Objective
Formalizar política de TTL (30/60/90 dias por tipo) e implementar detector que marca claims expiradas.

#### Evidence
`maturity-assessment.md` 2026-05-03 ainda citado 8 dias depois sem revalidação. Sem TTL, claims viram folclore.

#### Files to edit
```
docs/governance/decay-policy.md — (NEW) política 30/60/90
scripts/scan-decay.sh — (NEW) scanner que lista claims expiradas
.github/workflows/decay-scan.yml — (NEW) CI semanal
```

#### Deep file dependency analysis
- `decay-policy.md` define os 3 tiers de TTL
- `scan-decay.sh` consome frontmatter de evidence files + STATUS.md
- CI workflow agenda weekly + abre issue se decay >0

#### Deep Dives
- **3 TTL tiers:**
  - **30 dias** — operational claims (test counts, LIVE validations, journey results)
  - **60 dias** — architectural claims (state machines wired, CRD reconcile flows)
  - **90 dias** — ADRs/strategic decisions
- **Decay output format:**
  ```
  EXPIRED CLAIMS REPORT — 2026-06-11
  Dimension: Security
    - admin-secret-consolidation (T2.1) — last_validated 2026-05-10, expires 2026-06-09 (2 days overdue)
      Suggested revalidation: re-run T2.1 acceptance criteria, update evidence file
  ```
- **Edge case:** Claim "stable" pode pedir TTL longer. Mitigação: explicit `ttl_override: 180d` no frontmatter precisa de ADR-link para justificar.
- **Edge case:** Decay scan run em CI mas humano nunca lê issue. Mitigação: Slack/email notification + open PR auto-generated para claims criticas (Tier 1 = Critical findings em deep review).

#### Tasks
1. Redigir `decay-policy.md`
2. Implementar `scan-decay.sh` (parsing frontmatter + date arithmetic)
3. Criar workflow `decay-scan.yml` agendado weekly (Sundays 00:00 UTC, mesma janela que cell2-soaks)
4. Configurar notification (issue auto-created)

#### TDD
```
RED:     test_decay_detects_30day_expired — claim com last_validated=40dias atrás → reportado
RED:     test_decay_respects_60day_arch — claim arch com last_validated=45dias atrás → NÃO reportado
RED:     test_decay_handles_override — claim com ttl_override:180d → respeitado
RED:     test_decay_outputs_yaml — output parseable + tem todos campos required
GREEN:   Implementar scripts/scan-decay.sh
REFACTOR: Extract date-arithmetic helper
VERIFY:  Run em fixtures; verify expected vs actual claim list
```

#### Acceptance Criteria
- [ ] `decay-policy.md` cobre 3 tiers + override rule + edge cases
- [ ] `scan-decay.sh` rodando contra fixtures retorna lista correta
- [ ] Workflow `decay-scan.yml` validado via `act` ou similar
- [ ] Pass: shellcheck clean
- [ ] Pass: workflow lint
- [ ] File ≤ 500 lines

#### DoD
- [ ] All tasks completed
- [ ] Scanner detecta expired/non-expired corretamente em 5 fixtures
- [ ] Workflow runs em CI dry-mode
- [ ] Issue creation tested (manual ou via act)

---

### T0.4 — Bias Taxonomy + Self-Check Script

#### Objective
Codificar os 6 viéses + script que escaneia documento por padrões de cada viés.

#### Evidence
Sessão 2026-05-11 expôs 6 padrões de viés. Sem detection automatizada, awareness depende de consciência humana — falha em ~30% dos casos.

#### Files to edit
```
docs/governance/bias-taxonomy.md — (NEW) 6 viéses formalizados
scripts/bias-check.sh — (NEW) regex/heurística detector
.claude/skills/to-plan/SKILL.md — (EDIT) integrar bias-check na step de plan creation
```

#### Deep file dependency analysis
- `bias-taxonomy.md` é referencia educacional
- `bias-check.sh` é gate; pode rodar em PR ou plan creation
- `to-plan/SKILL.md` integra bias-check antes de salvar plan

#### Deep Dives
- **6 padrões detectáveis:**
  - **Counting bias:** padrões "X/Y ✅" sem distinção entre code-ready vs LIVE-validated; flag se ratio >90% sem evidence files linked
  - **Aggregation bias:** "média X.Y" sem mostrar componentes individuais; flag se média mencionada sem decomposition
  - **Recency bias:** claims dos últimos 7 dias dominantes (>50% das menções); flag para balance
  - **Self-attestation bias:** author do commit = author da claim; flag se 0 reviewers diferentes
  - **Documentation lag bias:** doc cita ref >30 dias old; cross-reference com decay scan
  - **"Implemented but not wired":** novo código sem call-site; cross-reference com `/dead-code` output
- **Output format:** report por viés com bullets de cada hit + suggested fix
- **Edge case:** Falsos positivos esperados (~15-20%). Mitigação: severity 0-3 + threshold configurável (default WARN para 0-1, FAIL para 2-3).

#### Tasks
1. Redigir `bias-taxonomy.md`
2. Implementar `bias-check.sh` com regex/heurística por viés
3. Editar `to-plan/SKILL.md` para integrar bias-check entre Step 4 (write plan) e Step 6 (edge-case)
4. Calibrar threshold (rodar em 5 planos existentes; ajustar FP rate)

#### TDD
```
RED:     test_counting_bias_detected — plan com "29/30 ✅" sem evidence links → flag
RED:     test_aggregation_bias_detected — texto "média 3.79" sem componentes → flag
RED:     test_recency_bias_detected — 80% das menções nas últimas 7 dias → flag
RED:     test_self_attestation_detected — commit author = claim author → flag
RED:     test_falsos_positivos_below_threshold — 5 plans existentes têm <20% FP
GREEN:   Implementar bias-check.sh
REFACTOR: Extract patterns para .lint-rules.yaml
VERIFY:  task bias:check docs/plans/sample-plan.md
```

#### Acceptance Criteria
- [ ] 6 viéses formalizados + exemplo de detection
- [ ] `bias-check.sh` rodando em 5 fixtures retorna esperado
- [ ] `to-plan/SKILL.md` atualizado com step de bias-check
- [ ] FP rate <20% calibrado em 5 plans existentes
- [ ] Pass: shellcheck clean
- [ ] File ≤ 500 lines

#### DoD
- [ ] All tasks completed
- [ ] Bias-check rodando em 5 fixtures com expected vs actual
- [ ] FP rate medido + documentado
- [ ] Skill update tested (re-invocação de /to-plan)

---

### T0.5 — Documentation Domain Architecture (`/architecture-docs` Mode 2 AS-IS)

#### Objective
Gerar snapshot AS-IS da arquitetura documental atual em `docs/architecture/governance/` para baseline.

#### Evidence
Skill `to-plan` mandata Mode 2 (Pre-Plan AS-IS). Documentation governance é meta-domain — não coberto pelos existing `docs/architecture/{application,database,observability,review,secrets,tenant}`.

#### Files to edit
```
docs/architecture/governance/README.md — (NEW) C4 overview
docs/architecture/governance/c4-context.puml — (NEW) System Context
docs/architecture/governance/c4-container.puml — (NEW) Container view (docs surfaces)
docs/architecture/governance/c4-component.puml — (NEW) Components (claims, evidence, decay)
docs/architecture/governance/deep-dive-current-state.md — (NEW) AS-IS analysis
```

#### Deep file dependency analysis
- Files são output do `/architecture-docs` skill Mode 2
- Documentam o caos atual: 297 docs, 87 com claims, 0 com TTL
- Baseline para comparar com Mode 3 (PROPOSED, gerado Phase 4)

#### Deep Dives
- **C4 Context:** Quem lê docs? CLI dev / API dev / Operator / Customer / Investor / Auditor. Cada um tem expectativa diferente.
- **C4 Container:** docs surfaces (CLAUDE.md, plans/, meetings/, runbooks/, learnings/, MEMORY.md). Show relationships (CLAUDE.md duplica maturity-assessment.md, etc.).
- **C4 Component:** Como uma claim flui hoje. Author escreve → commits → leitor consome 30 dias depois → sem revalidação.
- **Deep-dive:** Tipos de drift documentados, exemplos concretos do session 2026-05-11.

#### Tasks
1. Invocar `/architecture-docs` Mode 2 para domain=governance
2. Validar diagramas PlantUML rendem (kroki ou plantuml local)
3. Deep-dive escreve a story do session 2026-05-11 + diagnóstico

#### TDD
```
RED:     test_plantuml_renders — `plantuml -check docs/architecture/governance/*.puml` exit 0
RED:     test_c4_complete — files cobrem 3 levels (context/container/component)
RED:     test_deep_dive_has_evidence — deep-dive cita commit + line numbers
GREEN:   Generate via skill invocation
REFACTOR: None expected
VERIFY:  plantuml -check; render via kroki
```

#### Acceptance Criteria
- [ ] 4 PlantUML files render sem erro
- [ ] Deep-dive ≥1000 palavras com citations
- [ ] README sumariza domain em <300 palavras
- [ ] Pass: plantuml -check clean

#### DoD
- [ ] All tasks completed
- [ ] Architecture snapshot AS-IS committed
- [ ] Files render in markdown preview (mermaid/PlantUML)

---

### T0.5b — Conceptual Diagrams: Bias Taxonomy + Decay Lifecycle (Excalidraw)

#### Objective
Visualizar os 2 conceitos abstratos centrais do plano via Excalidraw: (a) **Bias Taxonomy** mostrando os 6 viéses e como se alimentam mutuamente; (b) **Decay Lifecycle** mostrando o ciclo de vida de uma claim desde criação até expiração/revalidação. Ambos passam Isomorphism Test (estrutura sem texto comunica) e Education Test (leitor aprende algo concreto).

#### Evidence
ADR D8 estabelece Excalidraw como tool para argumentos conceituais. Os 6 viéses (D7) e o sistema de decay (D3) são os 2 conceitos mais densos do plano. Sem visualização, prose-only força leitor a reconstruir o modelo mental — lossy.

#### Files to edit
```
docs/architecture/governance/concept-bias-taxonomy.excalidraw — (NEW) 6 viéses + arrows
docs/architecture/governance/concept-decay-lifecycle.excalidraw — (NEW) state machine visual
docs/architecture/governance/README.md — (EDIT from T0.5) link aos diagramas
```

#### Deep file dependency analysis
- Excalidraw JSON renderiza inline em GitHub markdown preview
- Referenciados de `bias-taxonomy.md` (T0.4) e `decay-policy.md` (T0.3) para visual complement
- Versionados em git como JSON; viewer pode abrir/editar via web (https://excalidraw.com)

#### Deep Dives
- **Diagram 1 — Bias Taxonomy:**
  - 6 nós (counting/aggregation/recency/self-attestation/lag/wired) arranjados em hexagon
  - Setas mostrando como um viés alimenta outro (ex: self-attestation → counting → aggregation)
  - Centro: "Status Drift" como sumidouro comum
  - Evidence artifacts: 1-2 quotes reais da conversa 2026-05-11 ("21/30 ✅", "média 3.79 ≠ 3.9 documentado")
  - Color palette: warning amber para viéses, red para sumidouro Status Drift
- **Diagram 2 — Decay Lifecycle:**
  - State machine visual: `Created → Fresh → Aging → Expiring → Expired → Revalidated → Fresh` (loop)
  - Cada state com TTL window indicado (30d/60d/90d branches)
  - Decision diamond: revalidação automática vs manual
  - Side panel: evidence artifact exemplo (yaml frontmatter com last_validated/expires)
- **Edge case:** Excalidraw JSON sem renderer falha silently no preview. Mitigação: README inclui link para excalidraw.com com instrução "abra para visualizar".
- **Edge case:** Diagrama envelhece (decay aplicado ao próprio diagrama). Mitigação: TTL=180d, revalidação trigger quando taxonomy mudar.

#### Tasks
1. Ler `references/color-palette.md` da skill para alinhamento de cores
2. Sketch bias taxonomy (papel ou markdown ASCII primeiro)
3. Invocar skill `excalidraw` para gerar JSON Bias Taxonomy
4. Sketch decay lifecycle
5. Invocar skill para gerar JSON Decay Lifecycle
6. Render & validate ambos (skill Step 6 mandatory)
7. Linkar de README + bias-taxonomy.md + decay-policy.md

#### TDD
```
RED:     test_excalidraw_files_valid_json — both files são JSON parseable
RED:     test_bias_diagram_has_six_nodes — 6 viéses representados
RED:     test_decay_diagram_has_state_machine — ≥5 states + ≥1 decision diamond
RED:     test_isomorphism_test — após strip de texto, estrutura comunica (manual review)
RED:     test_evidence_artifacts_present — ambos têm artifact concreto (quote/yaml snippet)
GREEN:   Generate via excalidraw skill
REFACTOR: None
VERIFY:  Open in excalidraw.com; render confirmed
```

#### Acceptance Criteria
- [ ] 2 `.excalidraw` files committed
- [ ] Both valid JSON (parseable)
- [ ] Bias diagram: 6 nodes + arrows + sumidouro central
- [ ] Decay diagram: state machine + TTL windows + decision points
- [ ] Evidence artifacts presentes (real quotes/yaml)
- [ ] Pass Isomorphism Test (manual review)
- [ ] README links both
- [ ] Cross-link from bias-taxonomy.md (T0.4) and decay-policy.md (T0.3)

#### DoD
- [ ] All tasks completed
- [ ] Both diagrams render correctly in excalidraw.com
- [ ] Cross-references bidirecionais (concept docs ↔ diagrams)
- [ ] Files ≤ 100KB each (Excalidraw best practice)

---

## Phase 1: Inventory & Classification

**Objective:** Catalogar exaustivamente toda claim existente nos 297 docs + classificar por evidence tier.

### T1.1 — Claim Extraction Script

#### Objective
Script que extrai toda "status claim" de `docs/**/*.md` + `CLAUDE.md` para um catalog estruturado.

#### Evidence
87 docs tem keywords de status (`score|maturity|GREEN|DONE|✅`). Extração manual = >40 horas. Script = ~30 minutos por full pass.

#### Files to edit
```
scripts/extract-claims.py — (NEW) extractor com regex + context window
docs/governance/claim-catalog.yaml — (NEW) output structured
docs/governance/extraction-patterns.md — (NEW) padrões de extração + edge cases
```

#### Deep file dependency analysis
- `extract-claims.py` é one-shot tool; consome docs/, emite YAML
- `claim-catalog.yaml` é fonte para Phase 2 (cross-validation) e Phase 3 (scoring)
- `extraction-patterns.md` justifica heurísticas

#### Deep Dives
- **Padrões a extrair:**
  - Score declarations: `Score: X.Y / 5.0`, `Maturity X.Y/5.0`, `Score: X.Y`
  - Status assertions: "✅ DONE", "🟡 in progress", "✅ GREEN", "✅ N/M passing"
  - Capability claims: "X works end-to-end", "supports Y", "validated LIVE"
  - Negative claims: "Not built", "❌", "Sprint 9", "deferred"
- **Output schema:**
  ```yaml
  claims:
    - id: cli-deploy-e2e
      source: CLAUDE.md:11
      text: "deploy happy path (full E2E dogfood validated)"
      dimension: Core Deploy Flow
      type: capability
      polarity: positive
      extracted_at: 2026-05-12T00:00:00Z
      evidence_tier: UNKNOWN
      ttl_default_days: 30
  ```
- **Edge case:** Claims duplicadas em multiple files. Mitigação: cluster por similarity (Levenshtein < 0.2) + merge.
- **Edge case:** Claims compound ("A e B funcionam"). Mitigação: split em N claims atômicas.

#### Tasks
1. Implementar `extract-claims.py` em Python (regex + heurística)
2. **T1.1.5 (per EC-3 MUST FIX):** Implementar 3 regex exclusions OBRIGATÓRIAS antes de `accept_claim()`:
   - **Exclusion A:** Skip linhas dentro de blocos triple-backtick (estado de máquina: in_code_block=True entre ```pares)
   - **Exclusion B:** Skip linhas em tabelas markdown onde mesmo padrão aparece em ≥2 cells da row (proxy para "header row" ou "exemplo enumerado")
   - **Exclusion C:** Skip linhas em blocos `<example>...</example>` ou após sentinelas "Exemplo:"/"Example:"/"e.g.:"/"Por exemplo:"
3. Documentar padrões + exclusions em `extraction-patterns.md`
4. Rodar em docs/ + CLAUDE.md; gerar `claim-catalog.yaml`
5. Manual review do output (sample 30 random claims; medir FP rate ≤15%)
6. **Calibration:** rodar em fixture controlada com 20 claims reais + 20 fake (em code-blocks/tables/examples); accuracy ≥90% antes de full run

#### TDD
```
RED:     test_extracts_score_pattern — text "Score: 3.9/5.0" → claim with score=3.9
RED:     test_extracts_done_status — text "✅ DONE 2026-05-10" → claim type=status, polarity=positive
RED:     test_extracts_negative — text "Not built" → claim polarity=negative
RED:     test_handles_compound — text "A and B work" → 2 claims
RED:     test_excludes_code_block — "Score: X" dentro de ``` → NOT extracted (EC-3 A)
RED:     test_excludes_markdown_table — table header com "Score" repetido → NOT extracted (EC-3 B)
RED:     test_excludes_example_block — "Score: 3.9" após "Exemplo:" → NOT extracted (EC-3 C)
RED:     test_fp_rate_below_15pct — calibration fixture: 20 real + 20 fake; FP rate ≤15%
RED:     test_extraction_accuracy_above_85pct — sample 30 manual labels; script matches ≥26
GREEN:   Implementar extract-claims.py com exclusions
REFACTOR: Extract patterns + exclusions para .extraction-patterns.yaml
VERIFY:  python scripts/extract-claims.py docs/CLAUDE.md
```

#### Acceptance Criteria
- [ ] Extractor roda em <2 min para docs/ inteiro
- [ ] Output validates contra schema
- [ ] Manual sample ≥85% accuracy
- [ ] Documented patterns + edge cases
- [ ] Pass: pylint clean
- [ ] File ≤ 500 lines

#### DoD
- [ ] All tasks completed
- [ ] `claim-catalog.yaml` gerado com ≥200 claims
- [ ] Accuracy ≥85% medido em sample manual
- [ ] No `UNKNOWN` dimension entries (todas dimension-tagged)

---

### T1.2 — Evidence Tier Classifier

#### Objective
Classificar cada claim do catalog em T1/T2/T3/none baseado em referências existentes.

#### Evidence
Sem classification, não dá pra separar "actually evidenced" de "asserted with confidence".

#### Files to edit
```
scripts/classify-evidence.py — (NEW) classifier
docs/governance/claim-catalog.yaml — (EDIT) populate evidence_tier field
docs/governance/evidence-gaps-report.md — (NEW) auto-generated gap analysis
```

#### Deep file dependency analysis
- `classify-evidence.py` lê claim-catalog + grep no codebase
- Updates catalog in-place
- Gap report é input para Phase 2 (decide o que cross-validate primeiro)

#### Deep Dives
- **Classification heuristics:**
  - **T1 (Code+Test):** claim cita file path ou function name; grep file → exists; grep `test_<name>` → exists
  - **T2 (LIVE Run):** claim cita timestamp; grep `${HOME}/.loopzero/artifacts/` ou `docs/runbooks/` ou `docs/reports/` por log archived
  - **T3 (CI Run):** claim cita workflow name; grep `.github/workflows/` → file exists + grep last 10 runs
  - **None:** nenhuma das acima; mark UNEVIDENCED
- **Edge case:** Claim cita "commit XYZ" mas commit foi force-pushed/lost. Mitigação: validate git log; mark BROKEN if commit não existe.
- **Edge case:** Claim cita test que existe mas é `t.Skip()`. Mitigação: grep `t.Skip\|@skip` antes de classify T1.

#### Tasks
1. Implementar `classify-evidence.py` 
2. Rodar contra claim-catalog
3. Gerar `evidence-gaps-report.md` com claims UNEVIDENCED + BROKEN
4. Manual spot-check 20 random classifications

#### TDD
```
RED:     test_classifies_t1_when_test_exists — claim cita "deploy_test.go::TestE2E" → T1
RED:     test_classifies_t2_when_archived_log — claim cita "validation-20260427-200525.log" → T2
RED:     test_classifies_none — claim sem reference → UNEVIDENCED
RED:     test_detects_broken_commit — claim cita commit que não existe → BROKEN
RED:     test_detects_skipped_test — claim cita test que é t.Skip → reclassify
GREEN:   Implementar classify-evidence.py
REFACTOR: Cache git log lookups
VERIFY:  python scripts/classify-evidence.py
```

#### Acceptance Criteria
- [ ] Classifier roda em <5 min para catalog inteiro
- [ ] Output mostra distribution (T1/T2/T3/none/broken)
- [ ] Gap report identifies UNEVIDENCED claims
- [ ] Manual spot-check ≥85% accuracy
- [ ] Pass: pylint clean

#### DoD
- [ ] All tasks completed
- [ ] catalog.yaml has evidence_tier populated for every claim
- [ ] Gap report committed
- [ ] Phase 2 input ready

---

### T1.3 — Decay Clock Initialization

#### Objective
Assignar `last_validated` + `expires` timestamp para toda claim baseado em git log + heurística.

#### Evidence
Sem timestamps, decay scan da Phase 5 não funciona.

#### Files to edit
```
scripts/init-decay-clock.py — (NEW)
docs/governance/claim-catalog.yaml — (EDIT) populate timestamps
```

#### Deep file dependency analysis
- `init-decay-clock.py` lê catalog + faz `git log -1 --format=%aI <file>` para inferir last edit
- Updates catalog with `last_validated` + `expires`

#### Deep Dives
- **Heurística:** `last_validated = git_last_modified_date(claim.source.file)`. Conservador — assume claim foi revalidada quando arquivo foi tocado.
- **Better heurística (TODO):** claim revalidated when associated evidence file was touched. Mas evidence files ainda não existem (Phase 4 cria). Por agora, file-level é proxy.
- **Edge case:** File touched por commit que NÃO revalidou (typo fix). Mitigação: Phase 4 introduz explicit revalidation commits.
- **Edge case:** Claim sem source file (extracted from CLAUDE.md). Mitigação: use CLAUDE.md commit date.
- **Edge case EC-4 (MUST FIX):** Arquivos renomeados (frequente em LoopZero — `tenant-controller` deletado, etc.). `git log -1` sem `--follow` retorna apenas data desde o rename atual, escondendo staleness real. **Fix obrigatório:** usar `git log -1 --follow --format=%aI -- <file>` (3 chars adicionados).

#### Tasks
1. Implementar `init-decay-clock.py`
2. Rodar contra catalog
3. Validar distribution (quantas claims já expired? quantas frescas?)

#### TDD
```
RED:     test_assigns_last_validated_from_git — claim em file X → last_validated = git_last_modified(X)
RED:     test_assigns_expires_30day — operational claim → expires = last_validated + 30d
RED:     test_assigns_expires_60day — architectural claim → expires = last_validated + 60d
RED:     test_handles_missing_file — claim source.file não existe → ERROR not silent default
RED:     test_handles_renamed_file (EC-4 MUST FIX) — arquivo renomeado X→Y; data retornada é do conteúdo original, não do rename commit. Implementação DEVE usar --follow.
GREEN:   Implementar init-decay-clock.py usando git log -1 --follow --format=%aI
REFACTOR: None expected
VERIFY:  python scripts/init-decay-clock.py
```

#### Acceptance Criteria
- [ ] Toda claim tem `last_validated` + `expires`
- [ ] Distribution report: X% already expired, Y% in next 30d, Z% fresh
- [ ] Errors em missing files, não silent defaults
- [ ] Pass: pylint clean

#### DoD
- [ ] All tasks completed
- [ ] catalog.yaml has full timestamps
- [ ] Distribution report committed for visibility

---

### T1.4 — Stale Claims Report

#### Objective
Gerar relatório das claims já expiradas como input prioritário para Phase 2.

#### Evidence
Direct consumption: Phase 2 deve cross-validar claims expiradas primeiro.

#### Files to edit
```
docs/governance/stale-claims-2026-05-12.md — (NEW) auto-generated report
scripts/scan-decay.sh — (REUSE from T0.3)
```

#### Deep file dependency analysis
- Reusa scanner de T0.3
- Output é list of (claim_id, file, last_validated, days_overdue) ordenada by severity

#### Deep Dives
- **Severity:** days_overdue > 60 = CRITICAL, > 30 = HIGH, > 0 = MEDIUM
- **Edge case:** Claim arquivado mas ainda referenciado (e.g., Sprint 5 stuff). Mitigação: cross-reference com Phase 6 archival policy.

#### Tasks
1. Rodar `scan-decay.sh` (de T0.3) contra catalog
2. Gerar markdown report com tabela ordenada
3. Confirm Phase 2 priority queue derived from this report

#### TDD
```
RED:     test_report_sorted_by_severity — output rows in days_overdue DESC
RED:     test_report_markdown_valid — markdownlint passa
GREEN:   Reuse scan-decay.sh output → markdown converter
REFACTOR: None
VERIFY:  cat docs/governance/stale-claims-2026-05-12.md
```

#### Acceptance Criteria
- [ ] Report exists, markdown valid
- [ ] Sorted by severity
- [ ] Phase 2 priority queue extractable from report
- [ ] Pass: markdownlint clean

#### DoD
- [ ] All tasks completed
- [ ] Report committed
- [ ] Phase 2 can start with prioritized list

---

## Phase 2: Code-Truth Cross-Validation

**Objective:** Para cada claim do catalog, verificar se código realmente suporta. Usa skills existentes.

### T2.1 — Cross-Validation on All Open Plans

#### Objective
Rodar `/cross-validation` skill em cada plano com checkboxes (7 planos ativos).

#### Evidence
Skill `cross-validation` já existe e compara plano vs implementação. Não roda automaticamente em plans existentes.

#### Files to edit
```
docs/governance/cross-validation-2026-05-12/{plan-slug}.md — (NEW per plan) reports
docs/governance/cross-validation-summary.md — (NEW) aggregate findings
```

#### Deep file dependency analysis
- Cada cross-validation report é independente
- Summary aggregates findings (CRITICO/GRAVE count per plan)

#### Deep Dives
- **7 plans to validate:**
  1. `chaos-mesh-experiments-plan.md`
  2. `deep-review-2026-05-10-remediation-plan.md`
  3. `deep-review-remediation-plan.md`
  4. (others — list current state)
- **Output per plan:** report markdown with sections: tasks-vs-code, dod-vs-code, test-existence-vs-claim
- **Edge case:** Plan tem [x] mas DoD não validado. Mitigação: cross-validation skill detecta isso (já é a função primária).

#### Tasks
1. List all plans com checkboxes em `docs/plans/`
2. Para cada plano, invocar `/cross-validation {slug}`
3. Coletar outputs
4. Aggregate summary

#### TDD
```
RED:     test_every_plan_has_cv_report — for each plan, cv report file exists
RED:     test_summary_aggregates_findings — CRITICO + GRAVE counts present
GREEN:   Run skill invocations
REFACTOR: None
VERIFY:  Read summary; verify counts add up
```

#### Acceptance Criteria
- [ ] Every open plan has CV report
- [ ] Summary aggregates with per-plan rows
- [ ] CRITICO/GRAVE findings cataloged (input for fixes)
- [ ] Markdown valid

#### DoD
- [ ] All tasks completed
- [ ] 7 CV reports committed
- [ ] Summary report committed
- [ ] Findings list ready for Phase 3 score adjustment

---

### T2.2 — Dead Code & Wiring Audit

#### Objective
Rodar `/dead-code` skill em Go + TypeScript para detectar "implemented but not wired" pattern.

#### Evidence
CLAUDE.md documenta esse pattern explicitamente como Known Systemic Pattern #1.

#### Files to edit
```
docs/governance/dead-code-2026-05-12-go.md — (NEW)
docs/governance/dead-code-2026-05-12-ts.md — (NEW)
docs/governance/wiring-gaps-summary.md — (NEW) aggregated
```

#### Deep file dependency analysis
- Dead code reports são input para Phase 3 (deflacionar claims sobre features que estão implementadas mas não conectadas)

#### Deep Dives
- **Go:** `golang.org/x/tools/cmd/deadcode -test ./...`
- **TypeScript:** `knip` against `cli/` e `infra/pulumi/`
- **Edge case:** Test-only functions (intentional). Mitigação: skill já filtra.
- **Edge case:** Public API meant for SDK consumers. Mitigação: tag com `// nolint:dead` ou `@public` comment.

#### Tasks
1. Run `/dead-code` skill, Go scope
2. Run `/dead-code` skill, TS scope
3. Aggregate findings into wiring-gaps-summary

#### TDD
```
RED:     test_deadcode_reports_exist — both Go + TS reports present
RED:     test_wiring_gaps_correlate_claims — each gap maps to ≥1 catalog claim
GREEN:   Run skill
REFACTOR: None
VERIFY:  Read reports
```

#### Acceptance Criteria
- [ ] Both reports committed
- [ ] Wiring gaps cross-referenced with catalog claims
- [ ] Pass: markdownlint clean

#### DoD
- [ ] All tasks completed
- [ ] Reports identify N "implemented but not wired" findings
- [ ] Phase 3 has list of claims to deflate

---

### T2.3 — Code-Audit on Critical Paths

#### Objective
Rodar `/code-audit` skill em paths críticos (api/, operators/, cli/) para complexity/coverage/security.

#### Evidence
Maturity dimension "Tests/Confidence 3.6" precisa de quantitative backing. Code-audit fornece números reais.

#### Files to edit
```
docs/governance/code-audit-2026-05-12.md — (NEW) report card
```

#### Deep file dependency analysis
- Report card é input para Phase 3 Tests/Confidence dimension scoring

#### Deep Dives
- **Audit techniques:** 9 técnicas (cyclomatic, coverage, mutation, module size, deps, SCA, unit/integration test quality, secrets/SAST, orphaned wiring)
- **Edge case:** Audit takes >30min for full repo. Mitigação: scope to critical paths first.

#### Tasks
1. Invoke `/code-audit` skill, scope=`api/internal/services,operators/internal/controller,cli/src`
2. Capture report
3. Cross-reference findings with catalog claims about test coverage

#### TDD
```
RED:     test_audit_report_exists
RED:     test_audit_scores_present — 9 técnicas têm score
GREEN:   Run skill
REFACTOR: None
VERIFY:  Read report
```

#### Acceptance Criteria
- [ ] Audit report committed
- [ ] All 9 techniques have findings
- [ ] Pass: markdownlint clean

#### DoD
- [ ] All tasks completed
- [ ] Report committed
- [ ] Tests/Confidence dimension has hard numbers for Phase 3

---

### T2.4 — Divergence Report (Claim vs Reality)

#### Objective
Sintetizar Phase 2 outputs em single divergence report: which claims contradict reality?

#### Evidence
Sem aggregation, findings dispersam. Single divergence report é fonte para Phase 3 deflation.

#### Files to edit
```
docs/governance/divergence-report-2026-05-12.md — (NEW)
```

#### Deep file dependency analysis
- Consumes T2.1, T2.2, T2.3 outputs
- Produces input for Phase 3 (each persona starts with divergence list as context)

#### Deep Dives
- **Categories:**
  - Claim says "✅ DONE" but cross-validation finds task incomplete
  - Claim says "implemented" but dead-code finds unreachable
  - Claim says "tested" but audit finds zero coverage
  - Claim says LIVE but no archived log
- **Edge case:** Claim accurate but evidence missing (just paperwork). Mitigação: tag `EVIDENCE_GAP_ONLY` (low severity).

#### Tasks
1. Read T2.1/T2.2/T2.3 outputs
2. Categorize each finding
3. Produce divergence report sorted by severity
4. Tag each divergence with target dimension

#### TDD
```
RED:     test_divergence_report_has_severity_column
RED:     test_each_divergence_tagged_dimension
RED:     test_divergences_cross_reference_catalog — every divergence cites claim_id
GREEN:   Manual synthesis or aggregator script
REFACTOR: None
VERIFY:  Read report
```

#### Acceptance Criteria
- [ ] Report committed
- [ ] All findings categorized
- [ ] Phase 3 can consume as adversarial-context input

#### DoD
- [ ] All tasks completed
- [ ] Divergences enumerated with severity
- [ ] Cross-references to catalog claim IDs

---

## Phase 3: Adversarial Recalibration

**Objective:** Rodar 3 personas adversariais blind contra evidence + divergence data; triangular score por dimensão.

### T3.0 — Independence Hardening (per EC-1 MUST FIX)

#### Objective
Garantir que as 3 personas tenham independência verificada antes de rodar T3.1. Sem essa hardening, a matemática de D1 vira teatro.

#### Evidence
EC-1 do edge-case review: 3 agentes Claude da mesma model family têm erros correlacionados → matemática `1−(0.1)³ = 99.9%` é inválida sem hardening. Confidence real seria <90% mesmo com 3 personas.

#### Files to edit
```
docs/governance/personas/independence-config.yaml — (NEW) declara modelos/temps/seeds por persona
docs/governance/independence-verification.md — (NEW) protocolo de verificação
scripts/verify-independence.sh — (NEW) script que confirma config antes de T3.1 rodar
```

#### Deep file dependency analysis
- `independence-config.yaml` é consumido por T3.1 (invocation pattern lê model/temp/seed do config)
- `independence-verification.md` é referência human-readable
- `verify-independence.sh` é gate: T3.1 não roda se script falha

#### Deep Dives
- **3 dimensões de independência a variar** (mínimo 2 das 3):
  - **Modelo:** atribuir P1=opus, P2=sonnet, P3=haiku OU usar codex-rescue skill para uma das 3 (cross-vendor)
  - **Temperature/seed:** se modelo suporta, P1=temp 0.7, P2=temp 0.3, P3=temp 0.5; seeds distintos
  - **System prompt:** persona files já são textualmente distintos (não suficiente por si só)
- **Independence config schema:**
  ```yaml
  personas:
    p1_vercel_dd:
      subagent_type: general-purpose
      model: opus  # ou claude-opus-4-7
      temperature: 0.7
      seed: 42
      persona_file: docs/governance/personas/vercel-dd-engineer.md
    p2_hostile_vc:
      subagent_type: codex:codex-rescue  # cross-vendor (GPT) via Codex
      model: gpt-5-4
      temperature: 0.3
      seed: 137
      persona_file: docs/governance/personas/hostile-vc-auditor.md
    p3_paranoid_sre:
      subagent_type: general-purpose
      model: sonnet  # ou claude-sonnet-4-6
      temperature: 0.5
      seed: 99
      persona_file: docs/governance/personas/paranoid-sre.md
  ```
- **Verification protocol:** `verify-independence.sh` confirma (a) ≥2 modelos diferentes OU (b) ≥3 temperatures distintas OU (c) codex-rescue para 1 das 3. Pelo menos 2 das 3 dimensões variadas.
- **Edge case:** Codex-rescue não está acessível em CI runner. Mitigação: fall-back para 3 modelos Claude (opus/sonnet/haiku) + temperatures distintos. Documentar trade-off no log de provenance.

#### Tasks
1. Redigir `independence-config.yaml` com 3 personas + variation dimensions
2. Redigir `independence-verification.md` (protocolo + edge cases)
3. Implementar `verify-independence.sh`
4. Integrar como pre-flight de T3.1

#### TDD
```
RED:     test_rejects_same_model_no_temp_variation — config 3× opus + temp 0.7 → exit 1
RED:     test_accepts_two_models — opus + sonnet + sonnet com temps distintos → exit 0
RED:     test_accepts_cross_vendor — opus + codex + sonnet → exit 0
RED:     test_rejects_single_dim_variation — 3× opus com 3 temps → exit 0 (3 dimensões temp)
RED:     test_reports_independence_dims_used — output lista quais dims variadas
GREEN:   Implementar verify-independence.sh
REFACTOR: Extract validation rules para YAML schema
VERIFY:  bash scripts/verify-independence.sh
```

#### Acceptance Criteria
- [ ] `independence-config.yaml` declara 3 personas com variation explícita
- [ ] `verify-independence.sh` valida ≥2 dimensões variadas
- [ ] Verification protocol documented
- [ ] T3.1 invocation pattern reads from config (não hardcoded)
- [ ] Pass: shellcheck clean
- [ ] File ≤ 500 lines

#### DoD
- [ ] All tasks completed
- [ ] Verification rejects single-dim variation, accepts multi-dim
- [ ] Config used as input to T3.1
- [ ] Provenance log captura dimensions actually varied

---

### T3.0b — Independence Matrix Diagram (Excalidraw)

#### Objective
Visualizar a matriz 3×3 (P1/P2/P3 × model/temp/seed) como argumento visual de que "3 personas" é qualitativamente diferente de "3 invocações" — passa Isomorphism Test mostrando independência sem precisar de texto explicativo.

#### Evidence
EC-1 do edge-case review é o gap conceitual mais sutil do plano. Reviewers externos vão questionar "por que 3 Claudes ≠ 3 fontes independentes" — diagrama responde antes da pergunta.

#### Files to edit
```
docs/architecture/governance/concept-independence-matrix.excalidraw — (NEW)
docs/governance/personas/README.md — (EDIT from T0.2) link
docs/governance/independence-verification.md — (EDIT from T3.0) link
```

#### Deep file dependency analysis
- Diagrama é tool didático para reviewers do ADR D1/D4
- Referenciado de personas README + independence verification

#### Deep Dives
- **Layout proposto:**
  - 3 colunas (P1 Vercel DD / P2 VC Auditor / P3 SRE)
  - 3 linhas (Model / Temperature / Seed)
  - 9 células com valores concretos exemplo (opus 0.7 seed=42; sonnet 0.3 seed=137; haiku 0.5 seed=99 OU codex/gpt para 1 das 3)
  - Sidebar: "Why this matters" mostrando matemática da independência (com cap 95-97% destacado)
  - Counter-example panel: matriz "all-same" mostrando colapso de independência (cap drops para ~90%)
- **Evidence artifact:** quote real do EC-1 explicando o gap matemático
- **Edge case:** Cross-vendor (codex/GPT) pode não estar disponível em CI. Mitigação: diagram mostra 2 paths — "primary path" com cross-vendor + "fallback path" com Claude-only family + temp/seed variation.

#### Tasks
1. Sketch matriz 3×3 + sidebar
2. Invocar skill `excalidraw` para gerar JSON
3. Cross-link de personas/README + independence-verification.md
4. Render & validate

#### TDD
```
RED:     test_excalidraw_valid_json
RED:     test_matrix_3x3 — 9 cells visíveis
RED:     test_counter_example_present — "all-same" panel showing collapse
RED:     test_evidence_artifact — EC-1 quote presente
GREEN:   Generate
REFACTOR: None
VERIFY:  Open in excalidraw.com
```

#### Acceptance Criteria
- [ ] `.excalidraw` JSON committed
- [ ] 3×3 matrix visible
- [ ] Counter-example panel mostra collapse to ~90%
- [ ] Evidence artifact (EC-1 quote)
- [ ] Cross-linked from 2 docs
- [ ] Pass Isomorphism Test

#### DoD
- [ ] All tasks completed
- [ ] Diagram renders correctly
- [ ] Reviewer pode entender independence problem sem ler ADR D1 inteiro

---

### T3.1 — Run 3 Personas Independently

#### Objective
Cada persona pontua 7 dimensões, **blind** ao score atual + ao output das outras personas.

#### Evidence
D4 (ADR) mandates 3 independent runs. Sem isolation, personas viésam mutually.

#### Files to edit
```
docs/governance/scoring-2026-05-12/p1-vercel-dd.yaml — (NEW)
docs/governance/scoring-2026-05-12/p2-hostile-vc.yaml — (NEW)
docs/governance/scoring-2026-05-12/p3-paranoid-sre.yaml — (NEW)
docs/governance/scoring-2026-05-12/raw-runs.md — (NEW) provenance log
```

#### Deep file dependency analysis
- 3 independent files; ZERO cross-reading entre eles antes de Phase 3.2
- raw-runs.md captura: persona, timestamp, model, input artifacts, output

#### Deep Dives
- **Invocation pattern (per persona):**
  ```
  Agent({
    subagent_type: "general-purpose",
    description: "{persona} scoring",
    prompt: <load persona file> + 
            "\n\nINPUT ARTIFACTS:\n" +
            "1. catalog.yaml\n" +
            "2. evidence-gaps-report.md\n" +
            "3. divergence-report-2026-05-12.md\n" +
            "4. code-audit-2026-05-12.md\n" +
            "Do NOT read maturity-assessment.md or CLAUDE.md status text. " +
            "Score 7 dimensions 0-5 blind. Output: yaml per schema."
  })
  ```
- **Blinding:** prompt explicitly forbids reading current score sources
- **Edge case:** Agent may "cheat" by recalling training-data LoopZero references. Mitigação: prompt warns + manual spot-check post-hoc.

#### Tasks
1. Spawn 3 agents in parallel (single message, 3 Agent tool calls)
2. Wait for completion
3. Save outputs to per-persona files
4. Generate raw-runs.md provenance log

#### TDD
```
RED:     test_three_outputs_exist — 3 files committed
RED:     test_outputs_match_schema — yaml per defined schema
RED:     test_provenance_captured — raw-runs.md has timestamps + model versions
GREEN:   Invoke agents; capture outputs
REFACTOR: None
VERIFY:  Diff outputs; ensure independence (text similarity < 0.4)
```

#### Acceptance Criteria
- [ ] 3 yaml files committed
- [ ] All match schema
- [ ] Provenance log captured
- [ ] Text similarity between outputs < 40% (proxies for independence)

#### DoD
- [ ] All tasks completed
- [ ] 3 independent scores produced
- [ ] No cross-contamination evident

---

### T3.2 — Triangulation Analysis

#### Objective
Compute convergence metrics + identify dimensions where personas disagree >0.3.

#### Evidence
D4 (ADR) requires reconciliation meeting for high-delta dimensions.

#### Files to edit
```
scripts/triangulate-scores.py — (NEW)
docs/governance/scoring-2026-05-12/triangulation.yaml — (NEW)
```

#### Deep file dependency analysis
- `triangulate-scores.py` reads 3 persona outputs
- Outputs: median per dim, std-dev per dim, flag dims with std-dev > 0.3

#### Deep Dives
- **Math:** for each dim, compute median(p1, p2, p3) + std-dev. Final score = median if std-dev ≤ 0.3, else FLAGGED.
- **Edge case:** All 3 agree but reasoning differs. Mitigação: T3.3 reads bullets, not just numbers.
- **Edge case:** Outlier (1 persona radically different). Mitigação: don't auto-discard; investigate via T3.3 meeting.

#### Tasks
1. Implement `triangulate-scores.py`
2. Run against 3 outputs
3. Generate triangulation.yaml with median + std-dev + flags

#### TDD
```
RED:     test_median_correct — 3 scores [3.5, 4.0, 3.8] → median 3.8
RED:     test_stddev_flag — std-dev > 0.3 → flagged=true
RED:     test_all_dimensions_covered — output has 7 dims
GREEN:   Implement
REFACTOR: None
VERIFY:  Read triangulation.yaml
```

#### Acceptance Criteria
- [ ] Triangulation YAML committed
- [ ] Each dim has median + std-dev + flag
- [ ] Flagged dims listed separately

#### DoD
- [ ] All tasks completed
- [ ] Math verified manually for 1 sample dim
- [ ] Flagged dims clear for T3.3

---

### T3.3 — Reconciliation Meeting for High-Delta Dimensions

#### Objective
Para cada dim flagged (std-dev > 0.3), invocar `/meeting` orchestrator com 3 personas + arbiter.

#### Evidence
D4 (ADR) mandates meeting for high-delta. Sem reconciliação, score reflete viés residual.

#### Files to edit
```
docs/meetings/records/MEET-2026-05-XX-XXX-{dim}.md — (NEW per flagged dim)
docs/governance/scoring-2026-05-12/reconciled.yaml — (NEW) final scores
```

#### Deep file dependency analysis
- Meeting records aggregate persona reasoning bullets
- Reconciled.yaml is final source of truth

#### Deep Dives
- **Meeting protocol:** Each persona presents 3 bullets justifying score. Arbiter (Principal Architect persona) asks: "Why the delta?" Convergence target: arrive at single number with all 3 personas agreeing.
- **Edge case:** No convergence (personas refuse to budge). Mitigação: arbiter decides, records dissenting opinions.

#### Tasks
1. For each flagged dim, invoke `/meeting`
2. Capture meeting record
3. Update reconciled.yaml
4. Document any dissents

#### TDD
```
RED:     test_meeting_records_exist — per flagged dim
RED:     test_reconciled_complete — all 7 dims have final score
RED:     test_dissents_documented — meetings with no convergence have dissent recorded
GREEN:   Run meetings
REFACTOR: None
VERIFY:  Compare reconciled vs triangulation; only flagged dims should differ
```

#### Acceptance Criteria
- [ ] Meeting per flagged dim recorded
- [ ] reconciled.yaml has final scores
- [ ] Dissents documented if any

#### DoD
- [ ] All tasks completed
- [ ] Final scores have meeting backing
- [ ] No dim left with std-dev > 0.3 unresolved

---

### T3.4 — Final Score per Dimension with Evidence Trail

#### Objective
Produce final dimension scorecards with full provenance.

#### Evidence
G1 requires triangulated scores with 99% confidence. Scorecard format makes provenance auditable.

#### Files to edit
```
docs/governance/scoring-2026-05-12/scorecard-{dim}.md — (NEW per dim, 7 total)
```

#### Deep file dependency analysis
- Each scorecard is one-page document with: dim name, final score, 3 persona scores, evidence files cited, divergences resolved, dissents

#### Deep Dives
- **Scorecard format:**
  ```markdown
  # Dimension: Core Deploy Flow
  
  **Final Score:** 4.5/5.0 (triangulated 2026-05-12, ttl 30 days)
  **Std-dev:** 0.14 (below threshold)
  
  ## Persona Scores
  - P1 (Vercel DD): 4.4 — "deploy works LIVE, rollback ~3s, canary smooth"
  - P2 (VC Auditor): 4.6 — "no overclaim; numbers match cleanroom log"
  - P3 (Paranoid SRE): 4.5 — "chaos coverage 0 but no production incidents"
  
  ## Evidence Cited
  - T1: api/internal/services/deploy_test.go (commit 341181ae)
  - T2: docs/evidence/core-deploy/2026-05-05-cleanroom-12-12.md
  - T3: github actions cell2-soaks.yml#42
  
  ## Divergences Resolved
  - None
  
  ## Open Concerns
  - Chaos coverage 0 (deferred to Sprint 9)
  ```

#### Tasks
1. For each dim, produce scorecard
2. Reference reconciled.yaml + evidence files
3. Validate every score has 3+ evidence references

#### TDD
```
RED:     test_seven_scorecards_exist
RED:     test_each_has_three_evidence_refs
RED:     test_each_has_persona_scores_bullets
RED:     test_dim_with_dissent_documents_it
GREEN:   Generate scorecards
REFACTOR: None
VERIFY:  Read all 7; cross-check vs reconciled.yaml
```

#### Acceptance Criteria
- [ ] 7 scorecards committed
- [ ] All have 3+ evidence refs
- [ ] All persona scores cited
- [ ] Open concerns enumerated

#### DoD
- [ ] All tasks completed
- [ ] 7 scorecards reviewed for completeness
- [ ] Evidence trail traceable for every claim

---

## Phase 4: Single Source of Truth — `docs/STATUS.md`

**Objective:** Consolidate everything into one canonical file with decay timers + auto-generated sections.

### T4.0 — Evidence Triangulation Pipeline Diagram (Excalidraw)

#### Objective
Diagrama central do plano: visualizar o pipeline completo desde **claim creation** até **STATUS.md inclusion**. Argumenta visualmente por que 3 fontes + adversarial review + decay + pre-merge gate compõem um sistema integrado, não componentes isolados.

#### Evidence
Este é o diagrama-de-um-slide do plano. Stakeholder externo (investidor, auditor, contratante) lê o diagrama em 60s e entende o sistema. Sem ele, o plano é 2059 linhas de prose denso. ADR D8 + EC central do plano demandam visualização.

#### Files to edit
```
docs/architecture/governance/concept-evidence-triangulation-pipeline.excalidraw — (NEW)
docs/STATUS.md — (EDIT from T4.1) embed link no header
README.md — (EDIT) link no quick-reference de governança
```

#### Deep file dependency analysis
- Diagrama é referenciado de STATUS.md e README — entrada visual para todo leitor novo
- Atualizado se sistema evoluir (TTL 180d via ADR D3 override)

#### Deep Dives
- **Layout proposto (Level 1 + Level 2 zoom):**
  - **Level 1 — Summary Flow (top):** `Claim Generated → Evidence Collected → Adversarial Review → Triangulation → STATUS.md → Decay Timer → Revalidation Loop`
  - **Level 2 — Section Boundaries:**
    - **Section A — Evidence Collection:** 3 sources (T1 code+test / T2 LIVE log / T3 CI run) feeding into validator
    - **Section B — Adversarial Review:** 3 personas (P1/P2/P3) with independence matrix mini-callout, feeding triangulator
    - **Section C — STATUS.md:** content sections + auto-gen + decay timer overlay
    - **Section D — Continuous Loop:** weekly CV + decay scan + nightly spotcheck + pre-merge gate
  - **Evidence artifacts (Level 3):**
    - Sample yaml frontmatter para claim
    - Sample triangulation output (3 scores + median + std-dev)
    - Sample STATUS.md row mostrando "Score 4.5 (validated 2026-05-11, expires 2026-06-10)"
  - **Color coding:** evidence sources = green, adversarial = red/amber, STATUS.md = blue (canonical), decay = orange (decay)
- **Edge case:** Diagrama grande (>10 sections) pode ficar ilegível. Mitigação: skill's "Section-by-Section Workflow" — start com Section A, expandir conforme valida.
- **Edge case:** PDF export pode quebrar layout. Mitigação: também exportar PNG via Excalidraw CLI; salvar em `docs/architecture/governance/exports/`.

#### Tasks
1. Ler skill `excalidraw` SKILL.md seções "Multi-Zoom Architecture" e "Large/Comprehensive Diagram Strategy"
2. Sketch Section A (Evidence Collection) standalone primeiro
3. Validar Section A renderiza
4. Adicionar Sections B, C, D iterativamente
5. Adicionar evidence artifacts (Level 3 details)
6. Render & validate complete diagram
7. Export PNG fallback
8. Embed em STATUS.md header + README quick-reference

#### TDD
```
RED:     test_excalidraw_valid_json
RED:     test_four_sections_present — A/B/C/D delimitados visualmente
RED:     test_three_evidence_sources_visible — T1/T2/T3 shapes distinct
RED:     test_three_personas_visible — P1/P2/P3 shapes distinct
RED:     test_evidence_artifacts_present — ≥3 sample yamls/snippets visible
RED:     test_isomorphism — strip text; flow ainda comunica
RED:     test_png_export_exists — fallback static asset
RED:     test_status_md_links_diagram — header tem inline reference
GREEN:   Generate seção por seção
REFACTOR: Trim text where shapes already communicate (per skill philosophy)
VERIFY:  Open in excalidraw.com; render confirmed; PNG matches
```

#### Acceptance Criteria
- [ ] `.excalidraw` JSON committed
- [ ] 4 sections (A/B/C/D) delimitadas
- [ ] Multi-zoom (Level 1 summary + Level 2 sections + Level 3 details)
- [ ] Evidence artifacts ≥3 (real yaml/snippets)
- [ ] PNG export committed em `exports/`
- [ ] STATUS.md header references diagram
- [ ] README quick-reference linked
- [ ] Pass Isomorphism Test
- [ ] Pass Education Test (leitor aprende algo concreto)
- [ ] File ≤ 200KB

#### DoD
- [ ] All tasks completed
- [ ] Diagram renders correctly
- [ ] Stakeholder externo entende sistema em <2min de leitura visual
- [ ] PNG fallback funciona em GitHub preview
- [ ] Cross-links bidirecionais

---

### T4.1 — STATUS.md Template + Initial Content

#### Objective
Create `docs/STATUS.md` with structured sections; populate from Phase 3 scorecards.

#### Evidence
G4 requires single source of truth.

#### Files to edit
```
docs/STATUS.md — (NEW) canonical status doc
docs/governance/status-template.md — (NEW) template spec
```

#### Deep file dependency analysis
- STATUS.md is THE source — all other docs link here
- Template spec ensures consistency over time

#### Deep Dives
- **Sections (mandatory):**
  1. **Overall Maturity** — single number + last_validated + expires
  2. **Per-Dimension Scores** — 7 rows, each linking to scorecard
  3. **Open Gaps** — enumerated, not buried in prose
  4. **Last 30 Days Activity** — auto-generated from git log
  5. **Decay Status** — count of expired/expiring/fresh claims
  6. **Evidence Index** — links to docs/evidence/
  7. **Adversarial Validation** — last 3-persona run date + std-dev summary
- **Edge case:** STATUS.md drift if humans edit ad-hoc. Mitigação: D6 pre-merge gate (Phase 6).

#### Tasks
1. Write template spec
2. Generate initial STATUS.md from Phase 3 scorecards
3. Validate against template
4. Commit

#### TDD
```
RED:     test_status_has_all_sections — 7 mandatory sections present
RED:     test_status_has_decay_visible — each claim has TTL
RED:     test_status_links_scorecards — each dim row links to scorecard
RED:     test_status_under_500_lines — readable in one screen
GREEN:   Write content
REFACTOR: None
VERIFY:  Open in markdown preview; read entirely
```

#### Acceptance Criteria
- [ ] STATUS.md exists with all sections
- [ ] Every claim has TTL visible
- [ ] Cross-references valid
- [ ] File ≤ 500 lines (target 300)
- [ ] Pass: markdownlint clean

#### DoD
- [ ] All tasks completed
- [ ] Reviewed by team
- [ ] Links validated (no 404)

---

### T4.2 — Migrate `maturity-assessment.md` to History

#### Objective
Move current `maturity-assessment.md` to `docs/maturity-history/2026-05-03-snapshot.md` (read-only). New file links to STATUS.md.

#### Evidence
D2 (ADR) mandates single source.

#### Files to edit
```
docs/maturity-history/2026-05-03-snapshot.md — (NEW, renamed)
docs/maturity-assessment.md — (EDIT) becomes pointer + history index
```

#### Deep file dependency analysis
- **6 files reference `maturity-assessment.md` (per EC-2 MUST FIX — enumerado, não estimado):**
  1. `docs/strategy/resilience-roadmap.md`
  2. `docs/plans/chaos-mesh-experiments-plan.md`
  3. `docs/plans/status-validation-and-doc-consolidation-plan.md` (este plano)
  4. `docs/meetings/records/MEET-2026-04-27-061.md`
  5. `CLAUDE.md`
  6. `README.md`
- Comando de verificação: `grep -rln "maturity-assessment" docs/ CLAUDE.md README.md`
- New shape: `maturity-assessment.md` is 2-line pointer to STATUS.md + history index
- **Nota meta:** v1.0 deste plano afirmava "19 files reference" — inflation de 3×. Foi a meta-evidência empírica do viés que o plano se propõe a combater. Fix trivial: enumeração concreta como acima.

#### Deep Dives
- **Migration steps:**
  1. Copy current content to `docs/maturity-history/2026-05-03-snapshot.md`
  2. Replace `docs/maturity-assessment.md` with redirect
  3. Update internal cross-references via sed (with diff review)
- **Edge case:** External readers bookmarked maturity-assessment.md. Mitigação: redirect maintained 90 days.

#### Tasks
1. **T4.2.0 (per EC-2 MUST FIX):** Executar `grep -rln "maturity-assessment" docs/ CLAUDE.md README.md > docs/governance/maturity-assessment-refs-2026-05-12.txt` e commitar como evidence file ANTES de migração
2. Copy current to history snapshot
3. Replace maturity-assessment.md with redirect
4. Update internal refs (sed contra a lista enumerada em T4.2.0; cada um com diff review)
5. Validate links (broken-link checker contra a lista de 6 arquivos)

#### TDD
```
RED:     test_history_snapshot_preserved — file exists with original content
RED:     test_redirect_short — new maturity-assessment.md < 30 lines
RED:     test_no_broken_internal_refs — no 404s
GREEN:   Migration script
REFACTOR: None
VERIFY:  grep -r maturity-assessment.md → all valid
```

#### Acceptance Criteria
- [ ] History snapshot intact
- [ ] Redirect file short and clear
- [ ] No broken refs
- [ ] Pass: markdownlint clean

#### DoD
- [ ] All tasks completed
- [ ] Migration verified
- [ ] Team informed (CHANGELOG entry)

---

### T4.3 — Refactor `CLAUDE.md` Status Line

#### Objective
`CLAUDE.md` linha 11 (status narrative, ~6000 chars) becomes ~3 linhas pointing to STATUS.md.

#### Evidence
D2 (ADR) mandates no duplication.

#### Files to edit
```
CLAUDE.md — (EDIT) shorten status section to redirect
```

#### Deep file dependency analysis
- CLAUDE.md is loaded by Claude on every session — short = better context budget
- Saved tokens get redirected to actually useful guidance

#### Deep Dives
- **Before:** ~6000 chars status narrative dense with claims
- **After:** ~300 chars: "Status: see `docs/STATUS.md`. Sprint plan: see table below. Hardenings H1-H7 complete."
- **Edge case:** Claude session needs status without tool calls. Mitigação: STATUS.md is small enough (≤500 lines) to Read in one shot.

#### Tasks
1. Replace status narrative with redirect
2. Preserve sprint table (orthogonal info)
3. Validate context budget (token count)

#### TDD
```
RED:     test_claude_md_status_under_500_chars — status section short
RED:     test_status_md_referenced — link present
RED:     test_sprint_table_preserved — table intact
GREEN:   Edit
REFACTOR: None
VERIFY:  Read CLAUDE.md; verify still useful
```

#### Acceptance Criteria
- [ ] Status section ≤ 500 chars
- [ ] STATUS.md linked
- [ ] Sprint table preserved
- [ ] Pass: markdownlint clean

#### DoD
- [ ] All tasks completed
- [ ] CLAUDE.md reviewed
- [ ] Next Claude session loads correctly

---

### T4.4 — Auto-Generated Sections

#### Objective
Make sections "Last 30 Days Activity" + "Decay Status" + "Test Counts" auto-update.

#### Evidence
Manual updates drift. Auto-gen prevents drift.

#### Files to edit
```
scripts/regenerate-status.sh — (NEW) generator
.github/workflows/status-refresh.yml — (NEW) daily refresh
```

#### Deep file dependency analysis
- `regenerate-status.sh` reads catalog + git log + test outputs
- CI workflow runs daily; commits if delta

#### Deep Dives
- **Auto-gen sections:**
  - **Last 30 Days Activity:** `git log --since=30d --oneline | head -20` formatted
  - **Decay Status:** call `scan-decay.sh` + format counts
  - **Test Counts:** call `task test:counts` (new helper) + format
- **Edge case:** Auto-commit conflicts with human edits. Mitigação: only update auto-sections; preserve `<!-- HUMAN-EDIT -->` marked sections.

#### Tasks
1. Implement `regenerate-status.sh`
2. Mark auto-sections with HTML comments
3. Workflow runs daily 00:00 UTC, commits if delta
4. Test idempotency (run twice → no diff)

#### TDD
```
RED:     test_regenerate_preserves_human_edits — human section unchanged
RED:     test_regenerate_updates_auto_sections — auto sections refreshed
RED:     test_idempotent — run twice → no diff second time
RED:     test_workflow_commits_only_on_delta — no spam commits
GREEN:   Implement
REFACTOR: Extract section helpers
VERIFY:  Run twice; verify diff only first time
```

#### Acceptance Criteria
- [ ] Generator idempotent
- [ ] Workflow runs daily
- [ ] Human edits preserved
- [ ] Pass: shellcheck clean

#### DoD
- [ ] All tasks completed
- [ ] Workflow tested (manual trigger)
- [ ] Idempotency verified

---

### T4.5 — Evidence File Population

#### Objective
Create `docs/evidence/{dimension}/{claim-slug}.md` for every claim that has T1/T2/T3 sources.

#### Evidence
G2 requires zero orphan claims. Evidence files materialize the link.

#### Files to edit
```
docs/evidence/{dim}/{claim}.md — (NEW many) one per claim
docs/evidence/README.md — (EDIT from T0.1) index
```

#### Deep file dependency analysis
- Each evidence file has: claim text, 3 sources cited, last_validated, expires, notes
- Index links to all

#### Deep Dives
- **Volume estimate:** ~50-80 claims with valid 3-source evidence. Other claims stay in catalog but not in STATUS.md.
- **Template per evidence file:**
  ```yaml
  ---
  claim_id: deploy-flow-e2e
  dimension: Core Deploy Flow
  last_validated: 2026-05-11
  expires: 2026-06-10
  evidence:
    - type: T1
      ref: ...
    - type: T2
      ref: ...
    - type: T3
      ref: ...
  ---
  
  # Claim: loopzero deploy works end-to-end on real infrastructure
  
  ## Why this claim matters
  ...
  
  ## How to revalidate
  Run `task dogfood:journey-01` against fresh cleanroom.
  ```
- **Edge case:** Claim valid but evidence partial (2/3 sources). Mitigação: include in `docs/evidence/` but flag `partial_confidence: true`; not counted toward final score.

#### Tasks
1. From catalog, filter claims with ≥3 evidence
2. Generate evidence file per claim
3. Update README.md with index

#### TDD
```
RED:     test_every_status_claim_has_evidence_file — STATUS.md refs link to existing files
RED:     test_every_evidence_file_has_three_sources — frontmatter validates
RED:     test_revalidation_instructions_present — "How to revalidate" section
GREEN:   Generate files
REFACTOR: None
VERIFY:  validate-evidence.sh on each file
```

#### Acceptance Criteria
- [ ] Evidence files for all 50-80 valid claims
- [ ] Index README links all
- [ ] validate-evidence.sh passes
- [ ] Pass: markdownlint clean

#### DoD
- [ ] All tasks completed
- [ ] STATUS.md links valid (no 404)
- [ ] Evidence revalidation instructions present

---

### T4.6 — STATUS.md Information Architecture Diagram (Excalidraw)

#### Objective
Visualizar como STATUS.md se relaciona com evidence files, scorecards, claim catalog, decay reports — a "constellation" da documentação canonical. Argumenta que STATUS.md é hub, não silo.

#### Evidence
ADR D2 estabelece single source of truth, mas em prose. Diagrama mostra hub-and-spoke visualmente. Útil para onboarding de novos contributors que precisam entender "onde mora a verdade".

#### Files to edit
```
docs/architecture/governance/concept-status-md-information-architecture.excalidraw — (NEW)
docs/STATUS.md — (EDIT) link no footer "How this works"
docs/governance/personas/README.md — (EDIT) link
```

#### Deep file dependency analysis
- Diagrama explicita hub-spoke entre STATUS.md (hub) e: scorecards (7), evidence files (50-80), claim catalog, decay reports, adversarial scoring outputs, CI workflow status

#### Deep Dives
- **Layout proposto — hub-and-spoke radial:**
  - **Center node:** `docs/STATUS.md` (large, primary color)
  - **First ring (mandatory references):** 7 scorecards (1 por dimension), evidence files (cluster grouping per dim), claim catalog yaml
  - **Second ring (auto-generated context):** decay report, last-30-days activity (git log), test counts
  - **Outer ring (CI feedback):** weekly CV results, nightly spotcheck, decay scan, pre-merge gate status
  - **Anti-pattern callout (sidebar):** "What NOT to do" — diagram showing the OLD scattered surface (CLAUDE.md duplicating + maturity-assessment.md drifting + plans/ with stale ✅) crossed out
- **Evidence artifacts:**
  - Real file paths (`docs/evidence/core-deploy/2026-05-05-cleanroom-12-12.md`)
  - Sample STATUS.md row mostrando link para scorecard
  - Sample auto-gen comment marker `<!-- AUTO:START -->`
- **Edge case:** Radial layout fica denso com 80+ evidence files. Mitigação: cluster grouping ("Core Deploy evidence (12 files)"), não plot individual.

#### Tasks
1. Sketch hub-spoke layout
2. Adicionar anti-pattern sidebar
3. Invocar skill `excalidraw`
4. Render & validate
5. Cross-link de STATUS.md footer

#### TDD
```
RED:     test_excalidraw_valid_json
RED:     test_hub_centered — STATUS.md visualmente dominant
RED:     test_three_rings_present — mandatory/auto/CI rings distinct
RED:     test_anti_pattern_sidebar — old scattered surface visible com strike-through visual
RED:     test_evidence_artifacts — ≥3 real paths/snippets
RED:     test_isomorphism — sem texto, hub-spoke ainda comunica centralidade
GREEN:   Generate
REFACTOR: Cluster evidence files (não plot 80 individuais)
VERIFY:  Open in excalidraw.com
```

#### Acceptance Criteria
- [ ] `.excalidraw` JSON committed
- [ ] Hub-spoke layout visível
- [ ] 3 rings (mandatory/auto/CI) distintos
- [ ] Anti-pattern sidebar showing rejected old design
- [ ] Evidence artifacts ≥3 (real paths)
- [ ] STATUS.md footer linka diagram
- [ ] Pass Isomorphism Test
- [ ] File ≤ 150KB

#### DoD
- [ ] All tasks completed
- [ ] Diagram renders correctly
- [ ] New contributor entende information architecture em <90s

---

## Phase 5: Continuous Re-Validation Loop

**Objective:** Make the validation system self-sustaining via CI automation.

### T5.1 — CI Cross-Validation Weekly

#### Objective
Workflow runs `/cross-validation` against all open plans weekly; opens issue if CRITICO/GRAVE found.

#### Evidence
Phase 2 was one-shot; without recurring, drift returns.

#### Files to edit
```
.github/workflows/cross-validation-weekly.yml — (NEW)
```

#### Deep file dependency analysis
- Workflow scheduled Sunday 02:00 UTC (after cell2-soaks)
- Calls cross-validation skill per plan
- Opens GitHub issue per plan with CRITICO findings

#### Deep Dives
- **Trigger:** `schedule: cron: "0 2 * * 0"`
- **Action:** for plan in docs/plans/*.md, run cross-validation, parse output, open issue if CRITICO/GRAVE > 0
- **Edge case:** Skill output format may change. Mitigação: pin skill version + integration test.

#### Tasks
1. Write workflow YAML
2. Test via `act`
3. Manual trigger to validate

#### TDD
```
RED:     test_workflow_valid — yaml schema check
RED:     test_workflow_handles_zero_plans — graceful no-op
RED:     test_workflow_opens_issue_on_critico — verified manually
GREEN:   Write
REFACTOR: None
VERIFY:  Manual trigger
```

#### Acceptance Criteria
- [ ] Workflow committed
- [ ] Schema valid
- [ ] Manual trigger succeeds
- [ ] Pass: yamllint clean

#### DoD
- [ ] All tasks completed
- [ ] Workflow tested
- [ ] First scheduled run succeeds

---

### T5.2 — CI Decay Scanner Weekly

#### Objective
Reuse `scan-decay.sh` from T0.3 in CI; opens issue with expired claims.

#### Evidence
T0.3 created the workflow file; this task verifies it runs.

#### Files to edit
```
.github/workflows/decay-scan.yml — (REVIEW from T0.3)
```

#### Tasks
1. Verify workflow from T0.3 is correct
2. Manual trigger
3. Validate issue created

#### TDD
```
RED:     test_workflow_runs — manual trigger succeeds
RED:     test_issue_created_when_expired — fixture has expired claim → issue opens
GREEN:   Test against fixtures
REFACTOR: None
VERIFY:  Inspect issue
```

#### Acceptance Criteria
- [ ] Workflow runs
- [ ] Issue created on expired claims
- [ ] Pass: yamllint clean

#### DoD
- [ ] All tasks completed
- [ ] Workflow operational

---

### T5.3 — Nightly Adversarial Spot-Check

#### Objective
Each night, pick 1 random dimension; rerun 1 persona on it; flag if score delta > 0.3 from STATUS.md.

#### Evidence
Phase 3 was full triangulation. Spot-check is cheaper continuous validation.

#### Files to edit
```
.github/workflows/adversarial-spotcheck.yml — (NEW)
scripts/spotcheck-dimension.sh — (NEW)
```

#### Deep file dependency analysis
- Workflow scheduled nightly
- Picks random dim + random persona
- Rerun + compare

#### Deep Dives
- **Cost:** 1 agent invocation/night × 30 nights = 30 agent runs/month. Cheaper than full triangulation monthly.
- **Edge case:** Random choice may avoid weak dim. Mitigação: weight by ttl_remaining (closer to expiry = higher prob).

#### Tasks
1. Write spotcheck script
2. Write workflow
3. Test manually
4. Validate alert threshold

#### TDD
```
RED:     test_random_selection_weighted — claims close to expiry selected more
RED:     test_alert_on_delta — score delta > 0.3 → issue opens
GREEN:   Implement
REFACTOR: None
VERIFY:  Manual trigger
```

#### Acceptance Criteria
- [ ] Workflow committed
- [ ] Spotcheck script tested
- [ ] Alerts trigger on delta
- [ ] Pass: shellcheck + yamllint clean

#### DoD
- [ ] All tasks completed
- [ ] Workflow operational
- [ ] First nightly run succeeds

---

### T5.4 — Notification Channel

#### Objective
Configure Slack/email notification for CI workflow alerts.

#### Evidence
CI opens issues; nobody reads issues until break. Active notification needed.

#### Files to edit
```
.github/workflows/{all-3-workflows}.yml — (EDIT) add notify step
```

#### Tasks
1. Add notify step to 3 workflows
2. Configure secret (SLACK_WEBHOOK_URL or similar)
3. Test delivery

#### TDD
```
RED:     test_notify_step_present — all 3 workflows have notify
RED:     test_notify_only_on_alert — no spam if zero findings
GREEN:   Configure
REFACTOR: Extract notify into reusable action
VERIFY:  Manual trigger; verify delivery
```

#### Acceptance Criteria
- [ ] Notify configured on 3 workflows
- [ ] No spam on clean runs
- [ ] Delivery verified

#### DoD
- [ ] All tasks completed
- [ ] First alert delivered to channel

---

## Phase 6: Governance Hardening

**Objective:** Codify rules so the system doesn't degrade post-launch.

### T6.1 — ADR-XXXX Documenting Claim-Evidence Contract

#### Objective
Formal ADR registering the new claim-evidence-decay contract.

#### Evidence
ADRs are how LoopZero records architectural decisions. Without ADR, this work is reversible by future committer.

#### Files to edit
```
docs/meetings/adr/00XX-claim-evidence-decay-contract.md — (NEW)
docs/meetings/records/MEET-2026-05-XX-XXX.md — (NEW) meeting record
```

#### Deep file dependency analysis
- ADR is reference for all future maturity claims
- Meeting record captures the discussion that produced ADR

#### Tasks
1. Convene meeting (or write up async amendment per ADR-0022)
2. Draft ADR
3. Get approvals (Principal Architect + Platform Architect + SRE)
4. Commit

#### TDD
```
RED:     test_adr_has_all_sections — Context/Decision/Rationale/Consequences
RED:     test_meeting_record_references_adr
GREEN:   Write
REFACTOR: None
VERIFY:  Read; cross-check with template
```

#### Acceptance Criteria
- [ ] ADR committed
- [ ] Meeting record committed
- [ ] Approvals captured

#### DoD
- [ ] All tasks completed
- [ ] ADR linked from STATUS.md
- [ ] ADR linked from governance docs

---

### T6.2 — CLAUDE.md Rule 14 (Claim Evidence)

#### Objective
Add Rule 14 to CLAUDE.md formalizing claim-evidence contract.

#### Evidence
CLAUDE.md Rules section is canonical for Claude session behavior. Without Rule, future sessions don't enforce.

#### Files to edit
```
CLAUDE.md — (EDIT) add Rule 14
```

#### Deep Dives
- **Rule 14 text (draft):**
  > **14. Claims need evidence** — Any claim affecting maturity score in `docs/STATUS.md` requires 3 evidence sources (T1 code+test, T2 LIVE log, T3 CI run). Claims without 3 sources stay in `docs/governance/claim-catalog.yaml` but NOT in STATUS.md. TTL applies (30d operational, 60d arch, 90d ADR). PR touching STATUS.md must update `docs/evidence/{dim}/{claim}.md`.

#### Tasks
1. Add Rule 14
2. Update related sections (cross-link)
3. Validate Claude session loads it

#### TDD
```
RED:     test_rule_14_present
RED:     test_rule_14_references_status_md
GREEN:   Edit
REFACTOR: None
VERIFY:  Read CLAUDE.md as new session would
```

#### Acceptance Criteria
- [ ] Rule 14 present
- [ ] Cross-references valid
- [ ] Pass: markdownlint clean

#### DoD
- [ ] All tasks completed
- [ ] Rule visible in next session

---

### T6.3 — Plan Template Update

#### Objective
Update `.claude/skills/to-plan/SKILL.md` to require evidence declaration in DoD.

#### Evidence
Future plans should bake evidence-thinking from start. Without template change, drift back to prose claims.

#### Files to edit
```
.claude/skills/to-plan/SKILL.md — (EDIT) add evidence requirement to DoD
```

#### Deep Dives
- **New DoD checkbox:**
  > [ ] Evidence file(s) in `docs/evidence/{dim}/` created or updated, linked from STATUS.md if score-affecting

#### Tasks
1. Edit SKILL.md
2. Validate next /to-plan invocation respects new template

#### TDD
```
RED:     test_skill_requires_evidence — next plan from skill has the checkbox
GREEN:   Edit
REFACTOR: None
VERIFY:  Manual /to-plan dry-run
```

#### Acceptance Criteria
- [ ] Skill updated
- [ ] Next /to-plan produces plan with evidence checkbox
- [ ] Pass: markdownlint clean

#### DoD
- [ ] All tasks completed
- [ ] Skill change tested

---

### T6.4 — Pre-Merge Gate

#### Objective
GitHub Action that blocks PRs touching STATUS.md without evidence file delta.

#### Evidence
D6 (ADR) requires gate.

#### Files to edit
```
.github/workflows/status-pr-gate.yml — (NEW)
```

#### Deep Dives
- **Logic:** if PR diff touches `docs/STATUS.md`, check that PR also touches at least one `docs/evidence/**` file. If not, fail with comment.
- **Edge case:** PR removes a claim (deflation). Mitigação: allow if evidence file moves to archive/.

#### Tasks
1. Write workflow
2. Test via PR
3. Document override (manual approval label)

#### TDD
```
RED:     test_blocks_status_change_without_evidence — PR fails
RED:     test_allows_status_change_with_evidence — PR passes
RED:     test_override_label_works — `evidence-waiver` label bypasses
GREEN:   Implement
REFACTOR: None
VERIFY:  Test PR
```

#### Acceptance Criteria
- [ ] Workflow committed
- [ ] Blocks correctly
- [ ] Override works
- [ ] Pass: yamllint clean

#### DoD
- [ ] All tasks completed
- [ ] Tested with sample PR
- [ ] Documentation updated

---

### T6.5 — Documentation Architecture PROPOSED (`/architecture-docs` Mode 3)

#### Objective
Generate Mode 3 (PROPOSED) architecture docs reflecting post-implementation state.

#### Evidence
Skill `to-plan` mandates Mode 3 post-plan.

#### Files to edit
```
docs/architecture/governance/diff/c4-context.puml — (NEW)
docs/architecture/governance/diff/c4-container.puml — (NEW)
docs/architecture/governance/diff/c4-component.puml — (NEW)
docs/architecture/governance/diff/deep-dive-proposed-state.md — (NEW)
```

#### Tasks
1. Invoke `/architecture-docs` Mode 3 for domain=governance
2. Diagrams reflect STATUS.md + evidence + decay + adversarial loop
3. Deep dive explains delta from AS-IS

#### TDD
```
RED:     test_diff_diagrams_render
RED:     test_deep_dive_compares_as_is_proposed
GREEN:   Generate
REFACTOR: None
VERIFY:  Render PlantUML
```

#### Acceptance Criteria
- [ ] 4 diff files committed
- [ ] PlantUML renders
- [ ] Delta from AS-IS documented

#### DoD
- [ ] All tasks completed
- [ ] Architectural diff visible
- [ ] Ready for promotion post-dogfood

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | No formal evidence bar — claims classification ad-hoc | T0.1 | 3-tier policy + validator script |
| 2 | Self-attestation bias — author = judge | T0.2, T3.1, T3.2, T3.3 | 3 adversarial personas + triangulation |
| 3 | No TTL on claims — stale folklore | T0.3, T1.3, T5.2 | Decay policy + scanner + CI weekly |
| 4 | Bias awareness only implicit | T0.4 | Taxonomy + self-check script |
| 5 | No baseline architecture doc for governance | T0.5 | Mode 2 AS-IS architecture |
| 6 | Claims scattered across 87+ docs | T1.1, T1.2 | Extract + classify catalog |
| 7 | Time of last validation unknown per claim | T1.3, T1.4 | Decay clock + stale report |
| 8 | Plans marked ✅ without code-validation | T2.1 | Cross-validation per open plan |
| 9 | "Implemented but not wired" undetected | T2.2 | Dead-code skill Go + TS |
| 10 | Tests/Confidence dim has no hard data | T2.3 | Code-audit 9 techniques |
| 11 | No synthesis of code-vs-claim divergence | T2.4 | Divergence report |
| 12 | Score recalibration uses biased data | **T3.0** (NEW v1.1), T3.1, T3.2 | Independence hardening + 3 personas blind + triangulação |
| 12b | **3 same-model agents ≠ independent (EC-1)** | **T3.0** (NEW v1.1), ADR D1 cap 95-97% | Variar ≥2 dimensões: model/temp/seed |
| 12c | **T4.2 inflated cross-ref count 19 vs 6 (EC-2)** | **T4.2.0** (NEW sub-task) | Grep concreto antes de migração |
| 12d | **T1.1 extrai exemplos em code-blocks como claims (EC-3)** | **T1.1.5** (NEW sub-task) | 3 regex exclusions (code/table/example) |
| 12e | **T1.3 git log sem --follow esconde rename staleness (EC-4)** | T1.3 (in-place fix) | `git log -1 --follow --format=%aI` |
| 26 | **Conceitos abstratos (viéses/decay) só em prose** | T0.5b (NEW v1.2) | Excalidraw: Bias Taxonomy + Decay Lifecycle |
| 27 | **Independence matrix 3×3 difícil de comunicar verbalmente** | T3.0b (NEW v1.2) | Excalidraw: Independence Matrix com counter-example panel |
| 28 | **Pipeline central do plano é 2059 linhas de prose** | T4.0 (NEW v1.2) | Excalidraw: Evidence Triangulation Pipeline (one-slide visual) |
| 29 | **Hub-and-spoke STATUS.md ↔ evidence/scorecards/CI invisível em prose** | T4.6 (NEW v1.2) | Excalidraw: STATUS.md Information Architecture |
| 30 | **Trade-off PlantUML formal vs Excalidraw conceitual sem doctrina** | ADR D8 (NEW v1.2) | Convenção de naming: `c4-*.puml` (formal) vs `concept-*.excalidraw` (argumentativo) |
| 13 | High-delta dims unresolved | T3.3 | Reconciliation meeting per flagged dim |
| 14 | No traceable evidence per score | T3.4, T4.5 | Scorecards + evidence files |
| 15 | No single source of truth | T4.1 | STATUS.md canonical |
| 16 | maturity-assessment.md drifts | T4.2 | Migrate to dated history; redirect |
| 17 | CLAUDE.md duplicates status info | T4.3 | Short redirect; context budget saved |
| 18 | Auto-sections drift if manual | T4.4 | Generator + daily CI |
| 19 | Validation is one-shot | T5.1, T5.2, T5.3 | Weekly CV + decay + nightly spotcheck |
| 20 | Issues never read | T5.4 | Slack/email notify |
| 21 | New contract reversible by future committer | T6.1 | ADR formalizing |
| 22 | Claude session doesn't enforce evidence | T6.2 | CLAUDE.md Rule 14 |
| 23 | Future plans drift back to prose claims | T6.3 | Plan template DoD update |
| 24 | STATUS.md edits without evidence | T6.4 | Pre-merge gate |
| 25 | No PROPOSED architecture for new governance | T6.5 | Mode 3 architecture docs |

**Coverage: 34/34 gaps covered (100%) — v1.1 adds 4 MUST FIX gaps from edge-case review; v1.2 adds 5 visual-argument gaps (4 Excalidraw diagrams + ADR D8).**

## Global Definition of Done

- [ ] All 7 phases completed (Phase 0-6, **36 tasks total** — was 32, +4 Excalidraw tasks in v1.2: T0.5b + T3.0b + T4.0 + T4.6)
- [ ] All tests passing (validator scripts + workflows)
- [ ] Zero lint warnings (shellcheck + yamllint + markdownlint + pylint)
- [ ] `docs/STATUS.md` single source of truth, ≤500 lines, decay timers visible
- [ ] Every claim in STATUS.md has evidence file in `docs/evidence/{dim}/`
- [ ] 3 adversarial personas runnable + tested
- [ ] CI workflows armed (3 new: weekly CV, weekly decay, nightly spotcheck)
- [ ] Pre-merge gate operational
- [ ] ADR + CLAUDE.md Rule 14 + plan template updated
- [ ] Architecture docs Mode 2 (AS-IS) + Mode 3 (PROPOSED) generated
- [ ] **`/cross-validation status-validation-and-doc-consolidation-plan` PASSED** (zero CRITICO, zero GRAVE)
- [ ] **`/dogfood full` PASSED** on clean infrastructure (no regression in product)
- [ ] Plan-specific: **95-97% confidence math verified** (not 99% — see ADR D1 v1.1) — every status claim has 3 evidence sources com refs distintas (EC-6) OR é explicitamente tagged `partial_confidence: true`
- [ ] Independence verified per T3.0: 3 personas variaram ≥2 dimensões (model/temp/seed); provenance log captura which dims varied
- [ ] Edge-case review report (`status-validation-and-doc-consolidation-edge-cases.md`) committed alongside plan
- [ ] **4 Excalidraw diagrams** committed em `docs/architecture/governance/concept-*.excalidraw` (Bias Taxonomy, Decay Lifecycle, Independence Matrix, Triangulation Pipeline, Information Architecture — todos passam Isomorphism + Education Test)
- [ ] PNG exports em `docs/architecture/governance/exports/` para fallback (GitHub markdown preview robustness)

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.** Esse plano não muda código do produto (deveria, por construção). Mas mudanças em CLAUDE.md afetam comportamento de futuras sessões Claude, então `/dogfood` em cleanroom valida que o produto ainda funciona sob o novo regime.

### Execution

```bash
# 1. Fresh infrastructure
task teardown && task setup && tilt up
task doctor

# 2. Run full dogfood
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean infrastructure
- [ ] Pre-Flight Check: ALL checks PASS
- [ ] Zero S0 findings
- [ ] Zero S1 findings (this plan should not introduce product regressions; if it does, that's a critical bug in scope changes)
- [ ] Prova de Vida passes for every deploy
- [ ] Prova de Morte passes for every delete
- [ ] No regressions in previously passing flows
- [ ] Observability check passes
- [ ] Rule 3 clean: zero internal terms in CLI output

### On Failure

If S0 or S1 issues found:
1. Fix all S0/S1 findings (likely caused by CLAUDE.md changes affecting session behavior)
2. Re-run `/dogfood full` on fresh infrastructure
3. Repeat until zero S0/S1

S2-S4 findings documented; don't block plan completion.
