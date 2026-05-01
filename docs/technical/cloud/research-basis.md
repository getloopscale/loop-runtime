# Cloud Research Basis

## Objetivo

Este documento registra a base de pesquisa usada para definir os gates SOTA da pasta `cloud/`.

Ele combina:

- padrões oficiais de OAuth device flow, segurança de sessão e webhooks;
- evidências do código atual do módulo;
- interpretação do `cloud/` como camada de identidade/comercial de uma PaaS.

## Fontes Externas Principais

### 1. RFC 8628 OAuth 2.0 Device Authorization Grant

Fonte:

- https://www.rfc-editor.org/rfc/rfc8628

Pontos relevantes:

- device flow é apropriado para clientes com input limitado ou UX de autorização fora da própria sessão local;
- authorization e token polling têm semânticas específicas.

Gates fundamentados:

- `CLO-G03`

Por que importa no LoopZero:

- o LoopZero é CLI-first, então esse flow é central para a experiência do produto.

### 2. Stripe Webhooks

Fonte:

- https://docs.stripe.com/webhooks

Pontos relevantes:

- webhook signatures precisam ser verificadas;
- provedores externos repetem entrega, então handlers precisam tolerar retry;
- raw payload integrity importa na verificação.

Gates fundamentados:

- `CLO-G05`

### 3. OWASP ASVS / Session Management Guidance

Fonte:

- https://devguide.owasp.org/en/11-security-gap-analysis/01-guides/02-asvs/

Pontos relevantes:

- sessão deve ter escopo, expiração e validação explícitos;
- boundary entre autenticação e autorização precisa ser claro.

Gates fundamentados:

- `CLO-G01`
- `CLO-G02`
- `CLO-G09`

### 4. OpenTelemetry Go

Fonte:

- https://opentelemetry.io/docs/languages/go/

Pontos relevantes:

- serviços HTTP devem propagar contexto e emitir spans úteis para fluxos distribuídos.

Gates fundamentados:

- `CLO-G11`

## Evidências Internas Do LoopZero

### Serviço cloud e boundary com engine

Arquivo:

- `cloud/cmd/main.go`

O que sustenta:

- inicialização de tracing;
- DB opcional e modo degradado explícito;
- Hydra device flow;
- session token issuer;
- boundary `/engine` autenticado pelo cloud service.

### Identity and auth

Arquivos:

- `cloud/internal/auth/*`
- `cloud/internal/routes/device.go`
- `cloud/internal/routes/session_token.go`
- `cloud/internal/routes/me.go`

O que sustentam:

- middleware e papéis;
- device flow;
- exchange de sessão para JWT;
- contexto do usuário no dashboard.

### Billing

Arquivos:

- `cloud/internal/billing/*`
- `cloud/internal/routes/billing.go`

O que sustentam:

- planos, provisionamento, Stripe e webhook handling;
- billing como domínio separado.

### Metering

Arquivos:

- `cloud/internal/metering/*`

O que sustentam:

- tracking;
- reporting;
- scheduler;
- reconciler.

### Dashboard

Arquivos:

- `cloud/dashboard/src/pages/*`
- `cloud/dashboard/src/lib/api.ts`
- `cloud/dashboard/src/lib/auth.ts`

O que sustentam:

- dashboard como consumidor dos contratos do backend;
- páginas de conta, billing, projects, environments e team.

### Test matrix

Arquivos:

- `cloud/tests/unit/billing_test.go`
- `cloud/tests/unit/device_handler_test.go`
- `cloud/tests/unit/enforcement_test.go`
- `cloud/tests/unit/metering_test.go`
- `cloud/tests/unit/tenant_test.go`

O que sustentam:

- cobertura direcionada das áreas sensíveis do módulo.

## Conclusão

Os gates em [sota-gates.md](./sota-gates.md) existem porque:

1. São coerentes com padrões oficiais de identidade, sessão e webhooks.
2. São necessários para uma camada cloud que governa conta, billing e dashboard numa PaaS.
3. Já encontram apoio concreto no desenho atual do módulo `cloud/`.
