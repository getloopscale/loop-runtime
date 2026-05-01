# Shared Packages Research Basis

## Objetivo

Este documento registra a base de pesquisa usada para definir os gates SOTA da pasta `pkg/`.

Ele combina:

- fontes oficiais de Go e padrões de observabilidade/resiliência;
- evidências do código atual do LoopZero;
- interpretação de `pkg/` como contract layer de uma PaaS.

## Fontes Externas Principais

### 1. Effective Go

Fonte:

- https://go.dev/doc/effective_go

Pontos relevantes:

- interfaces pequenas e concretas;
- APIs minimalistas;
- package design pragmático;
- erros como valores.

Gates fundamentados:

- `PKG-G02`
- `PKG-G03`
- `PKG-G08`

### 2. Go Package Names

Fonte:

- https://go.dev/blog/package-names

Pontos relevantes:

- nomes de pacote são parte crítica do design;
- pacotes genéricos demais (`api`, `types`, `interfaces`) são frequentemente erro de design.

Gates fundamentados:

- `PKG-G02`
- `PKG-G12`

### 3. Go Doc Comments

Fonte:

- https://go.dev/doc/comment

Pontos relevantes:

- package docs devem explicar a visão geral da API;
- documentação faz parte do contrato público da biblioteca.

Gates fundamentados:

- `PKG-G13`
- `PKG-G12`

### 4. OpenTelemetry Context

Fonte:

- https://opentelemetry.io/docs/specs/otel/context/

Pontos relevantes:

- `Context` é mecanismo de propagação transversal;
- concerns cross-cutting devem poder trafegar por boundaries sem acoplamento errado.

Gates fundamentados:

- `PKG-G09`

### 5. OpenTelemetry Go / Propagation

Fontes:

- https://opentelemetry.io/docs/languages/go/
- https://pkg.go.dev/go.opentelemetry.io/otel/propagation

Pontos relevantes:

- propagação e enrich de observabilidade devem seguir contratos bem definidos;
- implementação concreta pode variar por processo.

Gates fundamentados:

- `PKG-G09`

### 6. Circuit Breaker Pattern

Fonte:

- https://martinfowler.com/bliki/CircuitBreaker.html

Pontos relevantes:

- circuit breaker existe para conter cascata de falha;
- estados e thresholds precisam ser explícitos.

Gates fundamentados:

- `PKG-G10`

## Evidências Internas Do LoopZero

### State contracts

Arquivos:

- `pkg/statemachine/machine.go`
- `pkg/statemachine/*.go`

O que sustentam:

- transição inválida como erro de programação;
- variante `TryTransition` para estado vindo de fonte externa;
- machines específicas por domínio.

### Error contracts

Arquivos:

- `pkg/errors/errors.go`
- `pkg/errors/errors_test.go`

O que sustentam:

- erro tipado com `Code`, `Message`, `HTTPStatus`;
- `Is`, `Unwrap`, `Wrap` e `ToJSON`;
- compromisso com compatibilidade entre consumidores.

### Naming and IDs

Arquivos:

- `pkg/naming/naming.go`
- `pkg/id/id.go`

O que sustentam:

- centralização de nomes RFC 1123;
- IDs prefixados e estáveis;
- eliminação de composição manual espalhada.

### Observability decoupling

Arquivos:

- `pkg/logging/context.go`
- `pkg/logging/logger.go`

O que sustentam:

- enrich de logger via extractor registrável;
- shared layer sem dependência direta obrigatória de OTel.

### Health contracts

Arquivos:

- `pkg/health/aggregator.go`
- `pkg/health/component.go`
- `pkg/health/phase.go`

O que sustentam:

- health como semântica compartilhada;
- thresholds temporais explícitos;
- agregação determinística.

### Resilience primitives

Arquivos:

- `pkg/circuitbreaker/circuitbreaker.go`
- `pkg/circuitbreaker/circuitbreaker_test.go`

O que sustentam:

- primitive pequena, em memória, reutilizável;
- transições claras `Closed/Open/HalfOpen`.

### Security and policy

Arquivos:

- `pkg/jwt/validator.go`
- `pkg/enforcement/limiter.go`

O que sustentam:

- validação de issuer/audience/algorithm;
- emissão e rotação de session token;
- enforcement compartilhado com fail-closed em cenários sensíveis.

## Conclusão

Os gates em [sota-gates.md](./sota-gates.md) existem porque:

1. São coerentes com o design idiomático de bibliotecas em Go.
2. São necessários para manter contratos uniformes entre os módulos do LoopZero.
3. Já encontram apoio claro no desenho atual do código da pasta `pkg/`.
