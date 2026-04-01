# CLI Commands — Implementation Index

> **Objetivo:** Implementar os 7 comandos CLI faltantes para levar o LoopZero de 59% para 100% do escopo funcional.
>
> **Total:** 530 DoD checkboxes, ~20-25 dias de trabalho + 2-3 sprints (preview)
>
> **Regra:** Cada comando requer `/meeting` antes da implementacao. Nenhum codigo sem reuniao.

---

## Ordem de Implementacao

| # | Comando | Plano | Complexidade | Esforço | Backend Pronto |
|---|---------|-------|-------------|---------|----------------|
| 1 | `loopzero cancel` | [cmd-cancel.md](cmd-cancel.md) | VERY LOW | 1 dia | 100% |
| 2 | `loopzero build` | [cmd-build.md](cmd-build.md) | LOW | 2 dias | 70% |
| 3 | `loopzero project` | [cmd-project.md](cmd-project.md) | LOW | 2-3 dias | 60% |
| 4 | `loopzero team` | [cmd-team.md](cmd-team.md) | MEDIUM | 3-4 dias | 30% |
| 5 | `loopzero env` | [cmd-env.md](cmd-env.md) | MEDIUM | 3-4 dias | 40% |
| 6 | `loopzero promote` | [cmd-promote.md](cmd-promote.md) | HIGH | 5-7 dias | 10% |
| 7 | `loopzero preview` | [cmd-preview.md](cmd-preview.md) | VERY HIGH | 2-3 sprints | 5% |

---

## Por Que Esta Ordem

**1-3 (cancel, build, project):** CLI puro sobre backend existente. Entrega funcionalidade imediata com risco minimo. Valida o padrao CLI (exit codes, --json, Rule 3) que os demais seguem.

**4 (team):** Desbloqueia uso em equipe. Sem team management, o LoopZero e single-player. Primeiro comando que adiciona store methods + API endpoints + autorizacao.

**5 (env):** Requer fix de bug no operator (garbage collection de Environment CRs). Mais arriscado que team porque toca operator + API + CLI.

**6 (promote):** Novo fluxo completo (state machine, orchestrator, gitops push, ArgoCD sync). Similar em complexidade ao deploy flow. Requer tudo que os anteriores validaram.

**7 (preview):** Sistema inteiro (webhook, controller, DNS, TTL, cleanup). Sprint 5 dedicado. So faz sentido com tenants reais usando deploy + rollback.

---

## Dependencias

```
cmd-cancel ──────────────────────────────────┐
cmd-build ───────────────────────────────────┤ Independentes
cmd-project ─────────────────────────────────┤ (podem ser paralelos)
                                              │
cmd-team ◄── precisa de API auth patterns ────┘

cmd-env ◄── precisa de operator GC fix

cmd-promote ◄── precisa de env (target env
                 deve existir)

cmd-preview ◄── precisa de env + promote
                 (preview = deploy em env
                  efemero)
```

---

## Progresso

| # | Comando | Meeting | Implementacao | Tests | DO Validation |
|---|---------|---------|---------------|-------|---------------|
| 1 | cancel | SKIPPED (T1) | DONE | 13/13 pass | — |
| 2 | build | SKIPPED (T1) | DONE | 32 new tests | — |
| 3 | project | SKIPPED (T1) | DONE | 46 new tests | — |
| 4 | team | PENDING | — | — | — |
| 5 | env | PENDING | — | — | — |
| 6 | promote | PENDING | — | — | — |
| 7 | preview | PENDING | — | — | — |
