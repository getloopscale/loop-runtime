---
type: grill
title: "Feature grill — operator-glue-go-migration (M15)"
generated: { by: process:roadmap-feature, at: 2026-07-08 }
status: stable
slug: operator-glue-go-migration
generated_by: roadmap-feature
date: 2026-07-08
workflow_status: completed
answered_via: sponsor delegation — /goal 2026-07-08 ("Execute o CYCLE discover, plan, implement e review para M15 … máximo de rigor … TODOS OS DODS E CRITÉRIOS DE ACEITE VALIDADOS")
original_slug_rejected: bash-strangler-residual-completion (colisão substring com M13 "Bash Strangler Completion" — re-slug per Step 1 opção a)
out_of_scope_overlap: none (itens da seção: SSO/SAML, multi-cell auto-rebalance, multi-URL preview, CLI parity, LLM-judge/AIOps — zero keyword overlap)
---

# Feature grill — operator-glue-go-migration (M15)

## Q1 — O que é e por que AGORA?

**Feature:** completar o strangler de shell scripts até o alvo **literal** `allowlist ≤ 30`
(hoje: 142 entries / 189 scripts reais fora de archive), migrando os seeds canônicos
post-Cleanroom + operator tools (`infra/scripts/local` 57, `infra/helmfile/scripts` 14+11,
`infra/scripts/drill` 12, resíduo de `infra/tests` 49) para **subcomandos Go do `loopzero-ops`**
(precedente M9: `quality_subcmd.go` 2909→253 LOC) e deletando o bash migrado. Residual
final = somente glue operador irredutível (categoria KEEP-OPERATOR re-justificada).

**Why now:** (a) CTO mandate 2026-07-08 — eliminar shell scripts, substituir por tooling
FAANG-level; (b) M13 fechou o bloco M10–M13 com o DoD#3 reinterpretado como *trajetória*
(500+ → 393 na onda; hoje 142 após m-migrate2/m-migrate3) — o alvo literal ≤ 30 do
`shell-script-strategic-plan` (2026-05-22) permanece aberto; (c) o gate `no-new-bash`
invertido (ADR 2026-07-06, ACCEPTED) estanca o crescimento mas não elimina o estoque.

## Q2 — Dependências (milestones que precisam estar [x])

**M13** (Chaos Declarative + Bash Strangler Completion — a onda que este milestone
completa; inclui a inversão do gate) e **M9** (Engine Architecture Hardening — o
precedente `loopzero-ops` subcommand em Go). Ambos `[x]` — M15 é elegível imediatamente.

## Q3 — Definition of Done (verificável)

1. `grep -cE '^\| .*\.sh ' .claude/rules/shell-scripts-allowlist.md` retorna **≤ 30** (KPI literal, invertido — encolhe, não cresce).
2. Seeds canônicos post-Cleanroom migrados para subcomandos Go `loopzero-ops seed <nome>` com paridade **LIVE `--verify` verde no Go ANTES do `git rm` do `.sh`** correspondente (strangler por-área, padrão M13).
3. Zero asserção de comportamento em bash: residual 100% KEEP-OPERATOR irredutível, cada entry re-auditada com justificativa D4 válida (Necessary + Simple + Operator-time + Allowlisted).
4. `task quality:gates` + `go test ./...` GREEN após cada lote de DELETE; zero referência quebrada a caminho deletado (grep sweep em `taskfiles/`, `docs/` vivos, `.claude/skills/`, seeds master = 0 hits).
5. CHANGELOG `[Unreleased]`: `§ Removed` lista os scripts deletados por lote; `§ Added` os subcomandos Go novos.

## Q4 — Top 2 riscos NOVOS

1. **Perda de semântica de orquestração live-cluster na migração bash→Go** (kubectl exec/port-forward com trap de cleanup, retry/backoff, idempotência de seeds) — mitigação: paridade por-seed (`--verify` LIVE verde no subcomando Go antes do delete do bash); nunca big-bang (precedente da falha 2026-05-23).
2. **Referências penduradas** (runbooks, memories, Taskfile targets, `seed-post-cleanroom-all.sh` chain, skills) apontando para scripts deletados — mitigação: gate de referência (grep sweep DoD#4) + atualização dos consumidores no MESMO commit de cada lote.
