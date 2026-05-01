# API Research Basis

## Objetivo

Este documento registra a base de pesquisa usada para definir os gates SOTA da pasta `api/`.

Ele combina:

- padrões oficiais de HTTP, problem details, tracing e locking;
- evidências do código atual da API do LoopZero;
- leitura da API como control plane síncrono de uma PaaS.

## Fontes Externas Principais

### 1. RFC 9110 HTTP Semantics

Fonte:

- https://www.ietf.org/rfc/rfc9110.html

Pontos relevantes:

- status codes como `202 Accepted` e `409 Conflict` carregam semântica precisa;
- HTTP precisa distinguir processamento assíncrono, conflito e erro de cliente/servidor.

Gates fundamentados:

- `API-G06`
- `API-G07`

### 2. RFC 9457 Problem Details for HTTP APIs

Fonte:

- https://www.rfc-editor.org/rfc/rfc9457

Pontos relevantes:

- erros HTTP devem ser machine-readable e semanticamente claros;
- problem details é o modelo de referência para erro estruturado.

Gates fundamentados:

- `API-G07`

Por que importa no LoopZero:

- mesmo com envelope próprio hoje, a disciplina de erro estruturado é essencial para CLI e dashboard.

### 3. PostgreSQL Advisory Locks

Fonte:

- https://www.postgresql.org/docs/current/explicit-locking.html

Pontos relevantes:

- advisory locks podem ser session-level ou transaction-level;
- transaction-level é liberado ao fim da transação;
- session-level não respeita rollback e é perigoso com pool.

Gates fundamentados:

- `API-G04`
- `API-G05`

Por que importa no LoopZero:

- a API do LoopZero depende diretamente de advisory locks por environment.

### 4. W3C Trace Context

Fonte:

- https://www.w3.org/TR/trace-context/

Pontos relevantes:

- trace context padroniza propagação entre componentes distribuídos.

Gates fundamentados:

- `API-G08`

### 5. OpenTelemetry Trace API / Propagators

Fontes:

- https://opentelemetry.io/docs/specs/otel/trace/api
- https://opentelemetry.io/docs/specs/otel/context/api-propagators/

Pontos relevantes:

- `SpanContext` e propagators padronizam correlação entre spans;
- W3C TraceContext é propagator oficial importante.

Gates fundamentados:

- `API-G08`

## Evidências Internas Do LoopZero

### Process wiring e bootstrap do servidor

Arquivos:

- `api/cmd/main.go`
- `api/cmd/setup_*`

O que sustentam:

- configuração, tracing, database, stores, workflows, GitHub, health, cell router e background loops são compostos explicitamente;
- a API já trata bootstrap como wiring deliberado.

### HTTP surface e middleware chain

Arquivos:

- `api/internal/server/router.go`
- `api/internal/routes/*`
- `api/internal/middleware/*`

O que sustentam:

- chain explícita de security headers, CORS, maintenance, metrics, tracing, correlation e logging;
- rotas públicas, internas, admin e protegidas com separação clara.

### Idempotência e contrato de replay

Arquivos:

- `api/internal/middleware/idempotency.go`
- `api/internal/db/idempotency_store.go`
- testes de idempotência em `api/tests/unit/*`

O que sustentam:

- replay de mutação com request hash;
- mismatch explícito;
- store dedicado.

### Concurrency control

Arquivos:

- `api/internal/lock/environment_lock.go`
- `api/tests/integration/concurrent_builds_test.go`
- `api/tests/integration/lock_crash_recovery_test.go`

O que sustentam:

- uso explícito de `pg_advisory_xact_lock`;
- boundary transacional do lock;
- preocupação real com crash recovery.

### Build/release orchestration

Arquivos:

- `api/internal/services/build_orchestrator*.go`
- `api/internal/argo/*`
- `api/internal/gitops/*`
- `api/internal/argocd/*`

O que sustentam:

- orquestração desacoplada por interfaces;
- caminho assíncrono de build/release;
- integração com GitOps e runtime.

### Multi-cell e placement

Arquivos:

- `api/internal/placement/*`
- `api/internal/cellmgr/*`

O que sustentam:

- a API já pensa em discovery, routing e scoping por cell;
- multi-cell não é apenas ideia futura, já influencia o desenho.

### Test matrix sistêmica

Arquivos:

- `api/tests/integration/*`
- `api/tests/unit/*`
- `api/apitest/server.go`

O que sustentam:

- riscos sistêmicos importantes já recebem cobertura explícita.

## Conclusão

Os gates em [sota-gates.md](./sota-gates.md) existem porque:

1. São coerentes com padrões oficiais de HTTP, tracing e locking.
2. São necessários para uma API que orquestra uma PaaS multi-plano.
3. Já encontram evidência clara no desenho atual do módulo `api/`.
