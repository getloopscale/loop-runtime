---
slug: kserve-cross-validation-gaps
generated_by: roadmap-feature
date: 2026-07-06
status: completed
milestone_id: M14
---

# Feature grill — kserve-cross-validation-gaps

Origem: cross-validation loop loopzero vs KServe concluída 2026-07-06
(`cross-validation-output/final_report.md`, score 3.89/5.0, 6 gaps).
Sponsor verbatim: "Crie um roadmap para os TODOS os gaps, o roadmap deve ser
claro e mencionar a nossa referencia".

## Q1 — O que é e por que agora

**Resposta do sponsor:** Implementar todos os 6 gaps.

Feature: fechar os 6 gaps identificados pela cross-validation vs KServe
(referência CNCF em `knowledge-base/references/kserve/`). **Why now:** a
análise acabou de ser concluída com evidência file:line persistida em
`cross-validation-output/cross-validation.db`; os 3 gaps medium incluem uma
classe de incidente real já observada (M2 legacy-namespace — gap de admission
nos CRDs).

**Nota de honestidade (registrada):** a análise classificou os gaps #4/#5/#6
como YAGNI defensável com trigger de reativação. O sponsor decidiu
conscientemente implementá-los mesmo assim (override explícito registrado
aqui — não é scope creep silencioso).

## Q2 — Dependências

**Resposta:** M9 apenas (Engine Architecture Hardening, já [x]) — a superfície
que o M14 toca. A ordenação por ID do cycle-roadmap garante M10–M13 antes.

## Q3 — Definition of Done

**Resposta:** DoD completo 6 gaps — 6 entregas verificáveis, cada uma citando
o arquivo de referência do kserve (detalhadas no bloco M14 do ROADMAP.md).

## Q4 — Top 2 riscos novos

**Resposta:**
1. Registry de runtimes data-driven pode reabrir a superfície RCE que a
   remoção do BYO-Dockerfile fechou — mitigação: descriptors operator-only,
   nunca tenant-facing.
2. Webhooks failurePolicy=Fail no caminho crítico do API server podem
   deadlockar (classe B33 já vivida com VAPs) — mitigação: preferir CEL VAPs +
   matchConditions excluindo system controllers.

## Out-of-scope cross-check (Step 3)

Sem overlap real com a seção "Explicitly out of scope". Falso-positivo
considerado e descartado: "payload logger" (gap #6) ≠ "AIOps over
observability" (data-plane audit logging não é AIOps).
`out_of_scope_overlap_false_positive: LLM-judge/AIOps item — keyword
'observability' only; semantics differ.`

## SOTA delta (Step 5)

**Não** — a referência (KServe, Apache-2.0) já está clonada em
`knowledge-base/references/kserve/` e todos os 6 gaps citam arquivos dela.
Adicionada à tabela "State-of-the-art references" do ROADMAP.md com
`Supports: M14` (linha nova; nenhuma entrada existente tocada).
