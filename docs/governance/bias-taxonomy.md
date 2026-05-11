# Bias Taxonomy

**Status:** Canonical (per plan `status-validation-and-doc-consolidation-plan.md` T0.4, ADR D7)
**TTL:** 90 days
**Last validated:** 2026-05-11

## Purpose

A conversa de 2026-05-11 demonstrou empiricamente que **awareness implícita não funciona** — o próprio agente Claude criando este plano viesou +0.4 pontos de maturity em <30 minutos. Defesa = codificar os padrões de viés + script que detecta cada um automaticamente. Sem detection material, viés sempre retorna.

## The 6 Bias Patterns

### 1. Counting Bias

**Definição:** Contar items completados (✅) como se fossem prova de completion sem distinguir entre "código pronto", "teste passou", "LIVE-validado", e "dogfood passed".

**Exemplos concretos:**
- `"29/30 ✅"` em plan progress — não distingue task com 3 evidências vs task com checkbox marcado sem testes
- `"83/83 PASS"` em journey — confunde "tests defined" com "tests covering real scenarios"
- `"All tasks completed"` em DoD — sem validar se DoD claims são testáveis

**Detection pattern:**
- Regex: `\d+/\d+ ✅` ou `\d+/\d+ DONE` sem evidence link na mesma linha ou parágrafo
- Heurística: se ratio é >90% sem nenhum link para evidence file, flag MEDIUM

**Why dangerous:** Counts hide qualidade. 29/30 com 0 evidências é equivalente epistêmico a 0/30 + 1 commit dizendo "shipped".

**Counter-defense:** validate-evidence.sh exige 3 fontes distintas para qualquer claim de score-impact.

### 2. Aggregation Bias

**Definição:** Reportar média (ex: "score 3.9/5.0") sem mostrar decomposition por dimensão. Média esconde outliers — dimensão alta mascara dimensão baixa.

**Exemplos concretos:**
- `"Score 3.9/5.0"` sem listar as 7 dimensões individualmente
- Média 3.79 reportada como 3.9 (round-up)
- "Customer satisfaction 4.5" sem distinguir tier (Starter pode estar 2.0, Pro 5.0)

**Detection pattern:**
- Regex: `(?:média|average|score) \d\.\d` sem table/list adjacente com decomposition
- Heurística: arquivo cita "score X.Y" mas tem <3 dimensões enumeradas → flag

**Why dangerous:** Stakeholder externo vê "3.9" e pensa "consistente". Realidade: 4.5 + 3.4 = 3.9 com dimensão crítica fracassando.

**Counter-defense:** STATUS.md schema exige 7 rows independentes; média é informativa, score por dimensão é canonical.

### 3. Recency Bias

**Definição:** Claims dos últimos 7-14 dias dominam narrative. Gaps antigos (>30 dias) ficam invisíveis mesmo se críticos.

**Exemplos concretos:**
- "Build mode dispatch DONE (2026-05-08)" reportado 5x em CLAUDE.md
- "10 failing tests em api/internal/k8s" mencionado 1x — apesar de ser red flag de qualidade
- Sprint 5 (Preview Envs) tem 6 menções por causa de recency; AppSecret CRD unwired tem 1

**Detection pattern:**
- Heurística: se >50% das menções de uma seção são de eventos nos últimos 14 dias, flag
- Heurística: gaps documentados em CLAUDE.md "Known issues" mencionados <2x vs sucessos >5x → imbalance

**Why dangerous:** Decisões de release são tomadas baseadas em "what's fresh in mind", não "what actually matters".

**Counter-defense:** Decay TTL força revalidation; STATUS.md "Open Gaps" section é mandatory (não buried in prose).

### 4. Self-Attestation Bias

**Definição:** Autor da claim é também juiz da claim. Sem reviewer independente, todo bias do autor é preservado.

**Exemplos concretos:**
- Maturity assessment escrito pelo team que construiu o sistema
- Plan submission com DoD marcado pelo author do plan
- Score reportado em pitch deck sem revisão externa

**Detection pattern:**
- Heurística: commit author == claim author (via git blame em STATUS.md)
- Heurística: 0 reviewers diferentes em PR que altera score-affecting claim

**Why dangerous:** ADR-0016 §"Resolution" foi escrito para prevenir self-attestation em CHG approval. Mesmo com awareness explícita, padrão retorna.

**Counter-defense:** D6 pre-merge gate exige review label `evidence-verified` por author diferente. T3.0-T3.3 introducem adversarial personas.

### 5. Documentation Lag Bias

**Definição:** Claim cita referência stale (>30 dias) sem revalidação, mas é tratada como current.

**Exemplos concretos:**
- `docs/maturity-assessment.md` (2026-05-03) ainda citado 2026-05-11 sem update
- Runbook escrito em 2026-02 ainda referenciado para procedure em 2026-05
- "Test count 1210+" reportado quando real é 1456 (3 meses sem update)

**Detection pattern:**
- Cross-reference com decay scan: claims citando files >30 dias old
- Heurística: arquivo modified-date > X dias mas content sem `last_validated` field

**Why dangerous:** Stale documentation parece authoritative. Reader não sabe que dado é antigo.

**Counter-defense:** Decay scanner abre issue automaticamente. TTL visible em toda evidence file.

### 6. "Implemented but Not Wired"

**Definição:** Código que existe mas não está conectado ao caminho de produção. Documentado como Known Systemic Pattern #1 em CLAUDE.md.

**Exemplos concretos:**
- `AppSecret` CRD definido mas controller não implementado (CLAUDE.md menciona)
- `ScalingResolver` antes de B40 fix — função existia, mas Submit não invocava antes do enforceScaling
- Funções com 0 callers em call graph mas referenciadas em docs como "shipped"

**Detection pattern:**
- Cross-reference com `/dead-code` output (Go: `deadcode -test`, TS: `knip`)
- Cross-reference com call graph analysis

**Why dangerous:** Claim "feature X is implemented" tecnicamente verdadeira mas semanticamente falsa.

**Counter-defense:** T2.2 do plano roda `/dead-code` skill; STATUS.md claims só incluídas após cross-validation skill confirma wiring.

## Inter-Bias Causation (Why They Stack)

```
self-attestation → counting → aggregation → status drift
                       ↓
                  recency masks
                       ↓
                lag preserves status quo
                       ↓
            "implemented but not wired" undetected
```

Self-attestation é root cause. Outros 5 viéses são amplifiers — sem reviewer adversarial, autor sub-pontua os próprios gaps em todas as 5 dimensões.

## Bias-Check Script

`infra/scripts/governance/bias-check.sh <markdown-file>` rodando heurísticas:

| Bias | Severity 0-3 | Detection |
|------|--------------|-----------|
| Counting | 0=info, 1=warn, 2=fail | Ratio sem evidence link |
| Aggregation | 0=info, 1=warn, 2=fail | Score citado sem decomposition |
| Recency | 0=info, 1=warn | >50% mentions recent |
| Self-attestation | 1=warn, 2=fail | Author=claim_author, 0 reviewers |
| Lag | 1=warn | Source >30d old |
| Implemented-not-wired | 2=fail, 3=critical | Cross-ref deadcode output |

Default threshold: WARN at 0-1, FAIL at 2-3. Configurable via `.lint-rules.yaml`.

## False Positive Calibration

Run `bias-check.sh` against 5 existing plans (`docs/plans/*-plan.md`). FP rate target: <20%.

If FP rate >20%:
- Tighten patterns (more specific regex)
- Add exceptions list (e.g., known-good patterns like "78/82 PASS" in changelog é descritivo, não score-affecting)
- Document calibration in `extraction-patterns.md`

## Cross-References

- ADR D7 (plan): bias taxonomy as active defense
- ADR D4 (plan): adversarial personas defeat self-attestation
- `docs/governance/evidence-bar.md` — counter-defense for counting bias
- `docs/governance/decay-policy.md` — counter-defense for lag bias
- `infra/scripts/governance/bias-check.sh` — implementation
- **Visual:** `docs/architecture/governance/concept-bias-taxonomy.excalidraw` — 6 viéses em hexagon → Status Drift sumidouro (T0.5b)
