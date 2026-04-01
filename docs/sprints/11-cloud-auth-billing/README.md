# Sprint 11 — LoopZero Cloud: Auth + Billing

> Modulo `cloud/` — autenticação, signup, billing e plan enforcement para o LoopZero.

**Objetivo:** Transformar o LoopZero de "GitHub PAT auth only" para uma plataforma SaaS completa com signup via email/GitHub/Google, billing via Stripe, e planos com limites enforced.

**Principio:** CLI-first. Dashboard vem depois (Fase 9). Todas as fases devem ser testáveis via CLI antes de qualquer UI.

---

## Status

| Fase | Nome | Status | DoD |
|------|------|--------|-----|
| [1](./phase-01-ory-infra.md) | Ory Infra (Kratos + Hydra) | Not started | Kratos + Hydra rodando no mgmt cluster, health OK |
| [2](./phase-02-device-code-flow.md) | Device Code Flow + JWT | Not started | `loopzero login` abre browser, recebe JWT, CLI autenticado |
| [3](./phase-03-social-login.md) | Social Login (GitHub + Google) | Not started | Signup/login via GitHub e Google OAuth funcionais |
| [4](./phase-04-email-password.md) | Email + Password | Not started | Signup com email+password, verificação, recovery |
| [5](./phase-05-stripe-integration.md) | Stripe Integration | Not started | Customer criado no signup, subscription ativa |
| [6](./phase-06-plan-enforcement.md) | Plan Enforcement | Not started | Limites por tier enforced, 402 ao exceder |
| [7](./phase-07-usage-metering.md) | Usage Metering | Not started | Builds e storage reportados ao Stripe, cobrança correta |
| [8](./phase-08-engine-migration.md) | Engine Migration (JWT) | Not started | API engine aceita JWT, PAT deprecated com aviso |
| [9](./phase-09-dashboard.md) | Dashboard (Next.js) | Not started | app.loopzero.example funcional com login, projetos, billing |

---

## Sequencia e Dependencias

```
Fase 1 (Ory Infra)
  └─► Fase 2 (Device Code Flow)
        ├─► Fase 3 (Social Login)
        │     └─► Fase 4 (Email + Password)
        └─► Fase 8 (Engine Migration)
              └─► Fase 5 (Stripe)
                    ├─► Fase 6 (Plan Enforcement)
                    └─► Fase 7 (Usage Metering)

Fase 9 (Dashboard) — depende de 1-6 completos
```

## Domínios

| Uso | Domínio |
|-----|---------|
| Dashboard | `app.loopzero.example` |
| API (engine + cloud) | `api.loopzero.example` |
| User apps | `*.loopzero.example` |
| Landing/Docs | `loopzero.example` |

## Planos

| | Starter | Pro | Team | Enterprise |
|---|---|---|---|---|
| Preço | Grátis | $19/seat/mês | $49/seat/mês | Custom |
| Projetos | 2 | 10 | Ilimitado | Ilimitado |
| Serviços/projeto | 3 | 10 | Ilimitado | Ilimitado |
| Ambientes | 1 | 3 | 10 + previews | Ilimitado |
| Builds/mês | 100 | 1.000 | Ilimitado | Ilimitado |
| RAM/serviço | 512 MB | 2 GB | 8 GB | Configurável |
| Seats | 1 | 5 | 20 | Ilimitado |

## Stack

| Componente | Tecnologia |
|------------|-----------|
| Identity | Ory Kratos (self-hosted, Helm) |
| OAuth2 | Ory Hydra (self-hosted, Helm) |
| Billing | Stripe Billing (Go SDK) |
| Cloud API | Go + chi (same stack as engine) |
| Dashboard | Next.js 15 + Tailwind (fase 9) |

## Regras

1. **TDD** — teste antes de código, como todo o LoopZero.
2. **Backward compatible** — GitHub PAT continua funcionando até deprecação formal (fase 8).
3. **Sem secrets hardcoded** — Stripe keys e Ory URLs via config/env/Pulumi secrets.
4. **Mesmo banco** — cloud/ usa o PostgreSQL existente, novas tabelas via migrations.
5. **Gate entre fases** — cada fase tem DoD explicito. Não avança sem cumprir.
