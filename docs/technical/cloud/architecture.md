# Cloud Architecture

## Escopo

`cloud/` reúne o serviço cloud e o dashboard web do LoopZero.

Em uma PaaS como a nossa, isso forma a camada de produto voltada à conta e ao operador humano, acima do engine API. Essa camada precisa traduzir identidade, billing e experiência web sem duplicar a lógica operacional do deploy.

## Mapa Por Pasta

| Pasta | Responsabilidade principal | Domínio |
|---|---|---|
| `cloud/cmd` | bootstrap do processo e wiring do serviço cloud | process/runtime |
| `cloud/internal/auth` | OAuth/OIDC, middleware de JWT, tenant provisioning e papéis | identity/access |
| `cloud/internal/routes` | surface HTTP do cloud service | HTTP/product surface |
| `cloud/internal/billing` | planos, Stripe, provisionamento comercial e webhook handling | billing |
| `cloud/internal/metering` | tracking, reporting, scheduler e reconciliação de uso | metering |
| `cloud/internal/consent` | callbacks e fluxo de consentimento/login | auth UX flow |
| `cloud/internal/ratelimit` | proteção por conta/rota | abuse control |
| `cloud/internal/config` | configuração e validação do serviço | runtime config |
| `cloud/internal/metrics` | métricas do serviço cloud | observability |
| `cloud/internal/tracing` | tracing do serviço | observability |
| `cloud/dashboard/src` | dashboard React do produto | UI |
| `cloud/tests` | testes unitários do módulo | verification |

## Domínios Funcionais

### `identity-and-session`

Responsável por:

- Hydra device flow;
- exchange de sessão do dashboard para JWT;
- middleware de autenticação;
- associação entre usuário, tenant e papel.

Arquivos âncora:

- `cloud/internal/auth/hydra.go`
- `cloud/internal/auth/middleware.go`
- `cloud/internal/routes/device.go`
- `cloud/internal/routes/session_token.go`

### `billing-and-plan-state`

Responsável por:

- catálogo de planos;
- integração com Stripe;
- provisionamento comercial;
- estado de assinatura e suas implicações.

Arquivos âncora:

- `cloud/internal/billing/plans.go`
- `cloud/internal/billing/stripe.go`
- `cloud/internal/billing/provisioner.go`
- `cloud/internal/billing/webhook.go`
- `cloud/internal/routes/billing.go`

### `metering-and-reconciliation`

Responsável por:

- coleta e reporte de uso;
- scheduler e reconciler de medição;
- consistência eventual entre uso observado e estado comercial.

Arquivos âncora:

- `cloud/internal/metering/tracker.go`
- `cloud/internal/metering/reporter.go`
- `cloud/internal/metering/reconciler.go`
- `cloud/internal/metering/scheduler.go`

### `product-surface`

Responsável por:

- endpoints de `me`, billing, auth e proxy do engine;
- experiência de navegação e páginas do dashboard;
- consumo consistente dos contratos do sistema.

Arquivos âncora:

- `cloud/internal/routes/me.go`
- `cloud/internal/routes/billing.go`
- `cloud/dashboard/src/pages/*`
- `cloud/dashboard/src/lib/api.ts`

### `engine-boundary`

Responsável por:

- autenticar o usuário na borda cloud;
- chamar o engine com identidade/service-auth apropriada;
- manter separação entre produto cloud e control plane engine.

Arquivo âncora:

- `cloud/cmd/main.go`

## Fronteiras Arquiteturais

### O que pertence ao `cloud/`

- login e sessão do usuário;
- contexto de conta/tenant para UX web;
- cobrança, plano e capacidades comerciais;
- medição de uso;
- dashboard e páginas de produto;
- proxy/auth boundary entre dashboard e engine.

### O que não pertence ao `cloud/`

- orquestração de deploy e release;
- locking ambiental do engine;
- rendering de manifests;
- reconciliação de CRDs;
- bootstrap de plataforma.

## Relação Com A Engine API

O `cloud/` é consumidor e borda do engine, não substituto do engine.

Isso implica:

- o cloud service autentica o usuário e emite contexto adequado;
- a engine continua sendo a autoridade do control plane operacional;
- o dashboard deve refletir o mesmo modelo mental que a CLI, não um modelo paralelo.

## Regra De Ouro

Em uma PaaS como o LoopZero, o módulo `cloud/` deve tornar conta, billing e dashboard simples para o usuário sem assumir controle indevido da lógica do engine.
