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

### Health contracts (DELETED 2026-05-04 per ADR-0028)

> `pkg/health/` foi removido em 2026-05-04. Era dead code com 0 importadores em produção. Phase aggregation conceitualmente correta migra para `pkg/conditions` per ADR-0032 (kstatus-compatible). Esta seção é mantida como histórico para audits.

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

## Fontes Externas Adicionais (SOTA Research 2026-05-04)

### 7. cockroachdb/errors

Fonte:

- https://github.com/cockroachdb/errors
- https://github.com/cockroachdb/cockroach/blob/master/docs/RFCS/20190318_error_handling.md

Pontos relevantes:

- erros estruturados com network portability (errors.Is funciona após cruzar wire);
- type migration via `RegisterTypeMigration` para forward compat;
- separação `hint` (next action) vs `detail` (context);
- stack capture em wrap.

Gates fundamentados:

- `PKG-G08`, `PKG-G15`, `PKG-G13`

### 8. RFC 9457 — Problem Details for HTTP APIs

Fonte:

- https://datatracker.ietf.org/doc/html/rfc9457

Pontos relevantes:

- `application/problem+json` é o standard wire format para erros HTTP;
- supersede RFC 7807 mantendo backward-compat;
- registry de problem types padroniza categorias;
- extension members permitem campos custom.

Gates fundamentados:

- `PKG-G15`

### 9. RFC 8725 / draft-ietf-oauth-rfc8725bis-04

Fonte:

- https://datatracker.ietf.org/doc/html/rfc8725
- https://datatracker.ietf.org/doc/draft-ietf-oauth-rfc8725bis/

Pontos relevantes:

- algorithm whitelist explícito (defesa contra `none`/HMAC-com-public-key);
- validação `typ` header (cross-JWT confusion mitigation);
- alg case-sensitive verification (rfc8725bis update);
- PBES2 count limits para evitar DoS;
- jku/jwk header attacks mitigation.

Gates fundamentados:

- `PKG-G11`

### 10. sigs.k8s.io/cli-utils/pkg/kstatus

Fonte:

- https://pkg.go.dev/sigs.k8s.io/cli-utils/pkg/kstatus
- https://github.com/kubernetes-sigs/cli-utils/blob/master/pkg/kstatus/README.md

Pontos relevantes:

- abnormal-true polarity para conditions;
- `Reconciling` + `Stalled` standard conditions;
- `observedGeneration` discipline para distinguir "not yet" de "fully reconciled";
- fallback em `Ready` para CRDs sem standard conditions.

Gates fundamentados:

- novo gate (não-numerado) — informa o roadmap de `pkg/conditions` (S9-2).

### 11. sony/gobreaker v2

Fonte:

- https://github.com/sony/gobreaker
- https://github.com/mercari/go-circuitbreaker

Pontos relevantes:

- generics em retorno (`Breaker[T any]`);
- `IsExcluded(err)` para ignorar `context.Canceled`/`DeadlineExceeded`;
- de-facto Go ecosystem standard.

Gates fundamentados:

- `PKG-G17`

### 12. google/go-cloud Design Principles

Fonte:

- https://github.com/google/go-cloud/blob/master/internal/docs/design.md

Pontos relevantes:

- "no global state" salvo URL scheme registration;
- portable type + driver pattern;
- escape hatch via `As()` functions;
- service-based naming (não cloud-based).

Gates fundamentados:

- `PKG-G02`, `PKG-G03`, conflita com estado global em `pkg/logging` (ver S9-5).

### 13. Google Style Guide for Go

Fonte:

- https://google.github.io/styleguide/go/best-practices.html

Pontos relevantes:

- function signatures pequenas; option struct quando crescem;
- variadic options pattern com tipos não-exportados (encapsular);
- experimental APIs marcadas explicitamente.

Gates fundamentados:

- `PKG-G02`, `PKG-G14`

### 14. heptio/healthcheck (archived) e GlobalWebIndex/healthcheck

Fonte:

- https://github.com/vmware-archive/healthcheck
- https://pkg.go.dev/github.com/GlobalWebIndex/healthcheck

Pontos relevantes:

- separação estrita liveness vs readiness;
- async checks para dependências caras;
- liveness NUNCA depende de external services (anti-pattern catastrófico).

Gates fundamentados:

- referência arquitetural — informa decisão sobre `pkg/health` (QW-1) e desenho de `pkg/conditions` (S9-2).

## Conclusão

Os gates em [sota-gates.md](./sota-gates.md) existem porque:

1. São coerentes com o design idiomático de bibliotecas em Go (Effective Go, Google Style Guide).
2. São necessários para manter contratos uniformes entre os módulos do LoopZero (api, operators, cloud).
3. Já encontram apoio claro no desenho atual do código da pasta `pkg/`.
4. Foram validados contra o estado-da-arte 2025-2026 em pesquisa SOTA — gates G14-G18 adicionados em 2026-05-04 para fechar gaps detectados (dead code documentado, JWT pré-RFC 8725bis, CB sem generics, drift doc/código, transport coupling em errors).
