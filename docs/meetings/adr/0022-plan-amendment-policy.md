# ADR-0022 — Plan Amendment Policy

## Status

Accepted — 2026-05-03

## Context

`infra-sota-hardening-plan.md` foi emendado de v1.0 para v1.1 durante a execução do Ralph loop iter1 (2026-05-03). Três amendments foram aplicadas pelo agente sem MEET record, ADR ou aprovação isolada:

1. **Track-2 (P4.1 in-cluster Argo cosign keyless) deferido** para Sprint 9+ por ausência de OIDC infra cluster aceito por Fulcio público.
2. **P1.3 DoD redefinido** — "Taskfile ≤ 500 linhas" virou "foundation prepared + iter-2 mechanical migration documented" mid-execution. Posteriormente o Taskfile foi mecanicamente migrado para 150 linhas, mas o DoD foi temporariamente afrouxado.
3. **P1.1 11 actions com `# pragma: allow-tag-pin`** — gate GREEN porque pragma marca como exempt, mas resolução real dos SHAs aguarda Renovate Bot first run.

Cada amendment teve justificativa contextual válida, mas o pattern de **agente self-amendment durante execução** sem governance contraditório estabelece precedente perigoso. Ralph loops + autonomous research loops tendem a "negociar com si mesmos" o escopo quando encontram blockers — sem regra explícita, planos perdem força como contrato.

A revisão pós-iter1 (concern #5 da revisão honesta) identificou isso como item a endereçar antes de qualquer plano subsequente.

## Decision

Plan amendments durante execução de planos formais (sob `.claude/rules/pre-release-governance.md`) seguem regra explícita por categoria:

| Tipo | Trigger | Aprovação requerida |
|---|---|---|
| **Trivial** | Typo, comment, doc clarification dentro de uma task | Direto |
| **Scope refinement** | Task split em sub-tasks; mesmo DoD | Novo MEET record (lightweight) |
| **DoD redefinition** | Acceptance criteria muda dentro da task | MEET + nova ADR + plan version bump |
| **Task deferral** | Task moved para plano futuro com BLOCKER documentado | MEET + nova ADR explicando BLOCKER |
| **Plan abandon** | Plan inviável | Full T3 meeting + Sponsor revoke + new plan |

Workflow obrigatório quando amendment é necessária:

1. Agente identifica necessidade de amendment durante execução.
2. Agente STOPA implementação relacionada à amendment.
3. Para `Trivial` ou `Scope refinement`, agente proceed com decisão local + commit explicando.
4. Para `DoD redefinition`, `Task deferral` ou `Plan abandon`, agente invoca `meeting-orchestrator` com contexto de amendment.
5. Meeting produz MEET record (em `docs/meetings/records/MEET-YYYY-MM-DD-NNN.md`) + nova ADR formalizando.
6. Plan version é bumped (v1.0 → v1.1) em commit explícito.
7. Plan retomada com escopo emendado.

**Exceção ralph-loop:** quando agente está dentro de loop iterativo sem acesso humano para meeting síncrono, agente pode emitir "MEET-ASYNC-AMENDMENT" record que: (a) sintetiza posições de specialist agents via `Agent` tool delegation; (b) documenta Sponsor + Challenger + decision logic; (c) é tratado como pendente de ratificação humana antes de plan completion declared. Não substitui meeting real, apenas permite progress do loop sem self-grant invisível.

## Rationale

**Por que regra explícita:** sem regra, agentes self-amendment de forma invisível. Iter1 do infra-sota fez isso 3 vezes — todas justificáveis em contexto, mas none teve registro formal. A ausência de regra força agentes a "decidir sozinhos" o escopo, que conflita com pre-release governance.

**Por que tabela por tipo:** trivial vs scope vs DoD vs deferral vs abandon têm risco crescente. Trivial (typo) custaria mais governance overhead do que valor; abandon (plano vazio) merece full T3. Linear table evita falsa simetria.

**Por que async exception:** ralph loops + autonomous research loops são parte da capacidade do projeto. Banir amendments dentro deles inviabiliza loops de descoberta. Exception explícita preserva loop utility mas exige rastreabilidade ("MEET-ASYNC-AMENDMENT" como first-class artifact).

**Alternativas rejeitadas:**

- **"Forbid all amendments — plans são imutáveis":** quebra realidade da execução. Impossível implementar plan grande sem ajuste local.
- **"Permitir todas amendments via comment commit":** efetivamente o status quo broken. Sem rastreabilidade.
- **"Single approval level (sempre MEET)":** governance overhead alto demais para amendments triviais.

## Consequences

**Habilita:**

- Plan v1.x bumps são auditáveis com ADR per amendment.
- Ralph loops continuam funcionais via async-amendment path.
- "DONE" recupera força como contract — significa "DONE per latest ADR-approved version", not "DONE per agent's revised view".

**Constrange:**

- Amendments custam governance overhead (10-30min de meeting orchestration por amendment não-trivial).
- Ralph loops podem ficar "stuck" em fases que requerem amendment se async path mal-implementado.
- Plans pre-release com ambitious DoDs forçam amendment workflow se realidade diverge.

**Migration:**

- Iter1 do `infra-sota-hardening-plan.md` é grandfathered: as 3 amendments retroativamente documentadas via Section "Status Summary (Iter 1 closure 2026-05-03)" do plano. Não viola ADR-0022 retroativamente.
- Amendments futuras seguem ADR-0022 sem grandfather.

## References

- `.claude/rules/pre-release-governance.md` (gains "Plan Amendment Policy" section cross-referencing this ADR)
- `docs/plans/pre-launch-gaps-plan.md` D6 (decision summary)
- `docs/plans/infra-sota-hardening-plan.md` v1.1 (precedent justifying this ADR)
- `docs/meetings/records/MEET-2026-05-03-{062,063,064}.md` (MEET-B Track-2 deferral as positive example)
