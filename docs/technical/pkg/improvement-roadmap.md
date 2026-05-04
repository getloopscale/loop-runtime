# Shared Packages Improvement Roadmap

**Research date:** 2026-05-04
**Researcher:** Claude (SOTA Research Skill — Opus 4.7 1M)
**Current SOTA score:** 7.0/10
**Target SOTA score:** 9.0/10

## Executive Summary

`pkg/` é hoje um dos artefatos mais bem desenhados do LoopZero: 9 pacotes, 3052 LOC produção / 5562 LOC testes (1.82:1 ratio agregado), uma única dependência runtime (`golang-jwt/jwt/v5`), e uso real concentrado em `api/internal` (89 arquivos) e `operators/internal` (19 arquivos). A disciplina de "shared contract layer" é visível: erros tipados, state machines invariantes e geração canônica de nomes RFC 1123 estão centralizados.

O gap principal é entre **narrativa documentada** e **código de produção**. Três problemas concretos puxam a nota para baixo: (1) `pkg/health/aggregator.go` é descrito como contrato de plataforma mas tem **0 importadores em produção** — dead code com 379 LOC (componente + agregador + testes); (2) `pkg/jwt` não atende RFC 8725bis (2026) — falta whitelist explícita de algoritmos, validação de `typ`, e respeito a `Cache-Control` no JWKS; (3) `pkg/circuitbreaker` não usa generics nem `context.Context`, ficando atrás de `sony/gobreaker v2` (de-facto standard).

Acoplamentos transitórios também existem: `LoopZeroError.HTTPStatus` mistura camada de transporte com modelo de erro (viola PKG-G08), `pkg/enforcement` depende diretamente de `*sql.DB` (viola DIP), e `pkg/logging` mantém estado global mutável (viola Go CDK design principle "no global state"). Estes são "perdíveis" pré-1.0 mas precisam de plano.

A boa notícia: nenhum gap exige rewrite. Todos cabem em uma sequência de quick-wins (1-2 sessões cada) + um conjunto Sprint 9 de fôlego médio. Após aplicar este roadmap, `pkg/` chega ao patamar de bibliotecas-referência como `crossplane-runtime` e `google/go-cloud` na sua escala (PaaS contract layer).

## Competitive Position

Comparativo com referências SOTA. Os benchmarks foram lidos diretamente do código/docs públicos (não comparativo de marketing).

| Dimensão | LoopZero | Best-in-class | Gap | Esforço |
|---|---|---|---|---|
| Module structure (separate go.mod) | 5/5 | 5/5 (google/go-cloud) | — | — |
| Single-runtime-dep discipline | 5/5 | 5/5 (kubebuilder lib) | — | — |
| Test-to-code ratio | 4/5 | 5/5 (cockroachdb/errors) | falta property-based tests | M |
| Error model (typed + Is/As) | 3/5 | 5/5 (cockroachdb/errors) | sem stack capture, sem network portability, HTTP coupling | M |
| State machine clarity | 4/5 | 5/5 (qmuntal/stateless) | sem hierarchical states, sem visualization | L |
| State machine determinism guard | 5/5 | 5/5 (Temporal SDK) | — | — |
| Circuit breaker ergonomics | 3/5 | 5/5 (sony/gobreaker v2) | sem generics, sem `context.Context` | M |
| Health aggregation in production use | 1/5 | 5/5 (kstatus + Operator SDK) | dead code, parallel impl em api/internal | S |
| Conditions API (kstatus / metav1) | 0/5 | 5/5 (kstatus) | inexistente — cada controller faz manual | M |
| JWT validation (RFC 8725bis) | 3/5 | 5/5 (lestrrat-go/jwx v2) | sem `WithValidMethods` allowlist, sem `typ`, sem cache-control respect | S |
| Naming determinism + validation | 5/5 | 5/5 (Crossplane naming util) | — | — |
| ID generation (crypto/rand + prefix) | 4/5 | 5/5 (cockroachdb ULID) | sem validators (`IsValidBuildID`) | S |
| Logging trace extraction (no OTel coupling) | 4/5 | 5/5 (slog + otelslog) | global mutable state | M |
| Enforcement DIP (testability) | 2/5 | 5/5 (Open Policy Agent SDK) | direto em `*sql.DB`, sem interface | M |
| API surface (minimal exports) | 4/5 | 5/5 (Effective Go) | naming `Labels` poderia ser typed | S |
| Documentation alignment with code | 3/5 | 5/5 (kstatus README) | docs descrevem health/jwt como SOTA mas código tem gaps | S |

Score agregado atual: **53/80 = 6.6/10** → arredondado para **7.0/10** considerando a qualidade do que existe.

## Quick Wins (1-2 sessões cada)

### QW-1. Resolver o status de `pkg/health` (decisão binária: deletar ou wirear)
**Problema:** `pkg/health/aggregator.go`, `phase.go` e `component.go` somam 253 LOC + 480 LOC de testes, mas têm **0 importadores fora do próprio pacote**. `api/internal/health/prober.go` é uma implementação paralela com escopo diferente (HTTP probing). Os docs `architecture.md:69-78` e `research-basis.md:166-178` descrevem o pacote como "contrato de health compartilhado", o que não é verdade hoje.
**Ação:**
- Auditar os 8 controllers em `operators/internal/controllers/` e a route `/health` da API. Onde existe lógica de "phase aggregation", substituir por `health.AggregatePlatform`.
- Se o conceito não tem caso de uso real além de HTTP probing, **deletar `pkg/health`** completamente e atualizar a doc.
**Impacto esperado:** Elimina 379 LOC de dead code OU cria o primeiro consumidor real (PKG-G01 GREEN). Resolve a contradição doc↔código.
**Arquivos afetados:** `pkg/health/*`, `docs/technical/pkg/architecture.md`, `docs/technical/pkg/research-basis.md`, possivelmente `operators/internal/controllers/platformbootstrap_controller.go`.

### QW-2. Hardenize `pkg/jwt` para RFC 8725bis
**Problema:** `pkg/jwt/validator.go:78-99` faz validação ad-hoc do método de assinatura via type-assertion `*goJWT.SigningMethodRSA`. Funciona, mas não é a forma recomendada pela RFC 8725 — a forma correta é o whitelist explícito `goJWT.WithValidMethods([]string{"RS256"})`. Também faltam validação de `typ` header (cross-JWT confusion attack mitigation, RFC 8725bis §2.6) e validação case-sensitive de `alg` (RFC 8725bis §2.1).
**Ação:**
```go
// pkg/jwt/validator.go:78
token, err := goJWT.ParseWithClaims(tokenString, &Claims{}, keyFunc,
    goJWT.WithValidMethods([]string{"RS256"}),
    goJWT.WithIssuer(v.issuer),
    goJWT.WithAudience(v.audience),
    goJWT.WithExpirationRequired(),
)
// Validar typ explicitamente após parse:
if typ, _ := token.Header["typ"].(string); typ != "" && typ != "JWT" && typ != "at+jwt" {
    return nil, fmt.Errorf("unexpected typ header: %q", typ)
}
```
**Impacto esperado:** Fecha vetor de algorithm confusion (PKG-G11 reforçado). Alinha com `rfc8725bis-04`.
**Arquivos afetados:** `pkg/jwt/validator.go`, `pkg/jwt/validator_test.go` (adicionar testes para HS256-com-public-key e `none` algorithm).

### QW-3. Adicionar validators a `pkg/id`
**Problema:** `pkg/id/id.go:14-39` gera IDs prefixados (`build-{16hex}`, `rel-{16hex}`, `prev-{16hex}`, `raa-{16hex}`) mas não expõe parsers/validators. Consumidores em `api/internal/db/` validam IDs com regex inline ou strings.HasPrefix.
**Ação:** Adicionar:
```go
func IsValidBuildID(s string) bool      // matches "build-[0-9a-f]{16}"
func IsValidReleaseID(s string) bool    // matches "rel-[0-9a-f]{16}"
func IsValidPreviewID(s string) bool
func IsValidAppAttestationID(s string) bool
```
**Impacto esperado:** Elimina parsing duplicado em consumidores (PKG-G06 reforçado, DRY). Adiciona uma única regexp pré-compilada.
**Arquivos afetados:** `pkg/id/id.go`, `pkg/id/id_test.go`, grep e refactor de consumidores.

### QW-4. Acelerar JWKS fetch usando `pkg/circuitbreaker`
**Problema:** `pkg/jwt/validator.go:249-287` faz HTTP GET direto contra JWKS endpoint. Em outage de Hydra, todas as chamadas autenticadas degradam para timeout de 5s antes de falhar. LoopZero já tem `pkg/circuitbreaker` para exatamente isso, mas `pkg/jwt` não o usa — DRY violation cross-pkg.
**Ação:** Wrap `fetchJWKS` em circuit breaker. Quando aberto, usar JWKS cache mesmo expirado (graceful degradation). Sentinel `ErrJWKSStale` quando keys são >TTL+grace.
**Impacto esperado:** Tolerância a Hydra outage. Fecha gap de `dogfood-eat-your-own-pkg`.
**Arquivos afetados:** `pkg/jwt/validator.go`, `pkg/jwt/validator_test.go`.

### QW-5. Tipar `naming.Labels` para segurança em compile-time
**Problema:** `pkg/naming/naming.go:240-249` retorna `map[string]string` com chaves hardcoded. Consumidor que escreve `labels["loopzero.io/tenent"]` (typo) não recebe erro — só descobre em runtime ou nunca.
**Ação:**
```go
type StandardLabels struct {
    Tenant      string
    Project     string
    Environment string
}
func (l StandardLabels) AsMap() map[string]string { ... }
const (
    LabelTenant      = "loopzero.io/tenant"
    LabelProject     = "loopzero.io/project"
    LabelEnvironment = "loopzero.io/environment"
    LabelManagedBy   = "loopzero.io/managed-by"
)
```
**Impacto esperado:** Compile-time safety para usos críticos (selectors de operator, consultas Prometheus por label). Mantém compat via `AsMap()`.
**Arquivos afetados:** `pkg/naming/naming.go`, todos os consumidores via `naming.Labels(...)`.

### QW-6. Capturar stack trace em `errors.Wrap`
**Problema:** `pkg/errors/errors.go:64-72` faz wrap mas perde contexto de chamada. Em produção, debugar erro com message "[INTERNAL_ERROR] validation failed: ..." obriga a abrir o code-path manualmente.
**Ação:** Adicionar campo opcional `Stack []runtime.Frame` capturado lazy via `runtime.Callers` em `Wrap()`. Expor `func (e *LoopZeroError) StackTrace() string` para logging estruturado. NÃO incluir stack em `ToJSON()` — é dado interno.
**Impacto esperado:** Reduz MTTR para bugs de produção. Alinha com cockroachdb/errors pattern. Cost ~150ns por wrap (negligenciável fora do hot path).
**Arquivos afetados:** `pkg/errors/errors.go`, `pkg/errors/errors_test.go`.

### QW-7. Atualizar `architecture.md` e `paas-benchmark.md` para refletir uso real
**Problema:** Os docs descrevem `pkg/health` como exemplo SOTA e dizem que `pkg/` serve "API, operators, cloud services" — mas `cloud/` tem apenas 4 imports (vs 89 da `api/`). A doc está em desacordo com a realidade.
**Ação:** Adicionar seção "Real Usage Matrix" em `architecture.md` com tabela `pkg/X → consumidores reais (count)`. Marcar dead code com aviso explícito.
**Impacto esperado:** Honestidade técnica. Onboarding mais rápido.
**Arquivos afetados:** `docs/technical/pkg/architecture.md`, `docs/technical/pkg/paas-benchmark.md`.

## Sprint 9 Targets (Polish + Launch)

### S9-1. Migrar `pkg/circuitbreaker` para generics + `context.Context`
**Por quê:** `sony/gobreaker v2` adotou `Execute[T any](req func() (T, error))` em 2024 e isso virou standard. LoopZero está em V1-style, sem type safety e sem cancellation propagation.
**Ação:** API nova:
```go
type Breaker[T any] struct { ... }
func New[T any](cfg Config, onChange ...func(name string, from, to State)) *Breaker[T]
func (b *Breaker[T]) Execute(ctx context.Context, fn func(ctx context.Context) (T, error)) (T, error)
```
Manter API antiga não-generic durante deprecation window de 1 release. `IsExcluded(err)` para ignorar `context.Canceled`/`context.DeadlineExceeded` (mercari pattern).
**Arquivos afetados:** `pkg/circuitbreaker/circuitbreaker.go`, todos consumidores em `api/internal/services/workflow_circuit_breaker.go` etc.

### S9-2. Introduzir `pkg/conditions` (kstatus-compatible)
**Por quê:** Os 8 operators reconciliam `metav1.Condition` ad-hoc. kstatus (`sigs.k8s.io/cli-utils/pkg/kstatus`) é o standard SOTA: define convenções `Reconciling`+`Stalled` com polaridade "abnormal-true" + `observedGeneration` discipline. Hoje cada controller reescreve a mesma lógica.
**Ação:** Novo pacote `pkg/conditions` que:
- Re-exporta `metav1.Condition` types
- Provê `SetCondition(conditions *[]metav1.Condition, name string, status, reason, message)` (wrapper de `meta.SetStatusCondition`)
- Provê `IsReady(conditions []metav1.Condition) bool`, `IsStalled(...)`, `IsReconciling(...)`
- Define constantes LoopZero-padrão: `ConditionReconciling`, `ConditionStalled`, `ConditionReady` com strings canônicas
- Provê `AggregateToPhase(conditions, observedGen, currentGen) Phase` — UNINDO `pkg/health` ao K8s native condition model
**Impacto esperado:** Elimina ~6 cópias de logic em controllers. Resolve QW-1 promovendo `pkg/health` (ou substituindo). Alinha LoopZero com Operator SDK convention.
**Arquivos afetados:** novo `pkg/conditions/`, refactor de 8 controllers.

### S9-3. Desacoplar `LoopZeroError` de HTTP transport
**Por quê:** `pkg/errors/errors.go:14-27` mistura `Code` (modelo) com `HTTPStatus` (transporte). Viola PKG-G08 e o princípio Google AIP "errors should be transport-agnostic". Quando exporta para CLI/operators/audit, o HTTPStatus é ruído.
**Ação:** Refactor em duas camadas (mantendo wrapper compat):
```go
// pkg/errors/errors.go
type LoopZeroError struct {
    Code, Message string
    Cause error
    Stack []runtime.Frame
}
// pkg/errors/http/mapping.go
func HTTPStatus(err error) int { ... } // lookup by code
func WriteJSON(w http.ResponseWriter, err error) { ... } // RFC 9457 problem+json
```
**Impacto esperado:** Operators e CLI não importam HTTP semantics. RFC 9457 problem+json fica natural na API. Migração transparente via deprecation field.
**Arquivos afetados:** `pkg/errors/errors.go`, novo `pkg/errors/http/`, refactor de routes em `api/internal/`.

### S9-4. Injetar `LimitsStore` em `pkg/enforcement`
**Por quê:** `pkg/enforcement/limiter.go:14-23` usa `*sql.DB` direto. Viola DIP. Testes em `limiter_test.go` (991 LOC) usam `go-sqlmock` para contornar — sintoma de design errado.
**Ação:**
```go
type LimitsStore interface {
    GetTenantPlan(ctx context.Context, tenantID string) (string, error)
    GetLimits(ctx context.Context, plan, metric string) (hardLimit, softLimit int, err error)
}
type PlanLimiter struct { store LimitsStore; logger *slog.Logger }
```
Adapter em `api/internal/db/limits_store.go` (`*sql.DB` → `LimitsStore`).
**Impacto esperado:** Testes podem usar implementação in-memory. CLI/operators podem usar plan limits sem importar `database/sql`. ISP cleaner.
**Arquivos afetados:** `pkg/enforcement/limiter.go`, `pkg/enforcement/counters.go`, novo adapter, `api/cmd/main.go`.

### S9-5. Eliminar global state em `pkg/logging`
**Por quê:** `pkg/logging/context.go:17-20` mantém `globalExtractor` global mutable + `extractorMu`. Google CDK design doc explicitamente proíbe global state em libs ("Push responsibility to the application"). Risco real: race condition ao trocar tracing backend.
**Ação:** Trocar registry global por construtor explícito:
```go
type ContextLogger struct { extractor TraceExtractor; base *slog.Logger }
func NewContextLogger(extractor TraceExtractor, base *slog.Logger) *ContextLogger
func (c *ContextLogger) FromContext(ctx context.Context) *slog.Logger { ... }
```
Manter `RegisterTraceExtractor` deprecated por 1 release com warning.
**Impacto esperado:** Compatível com `slog`-style chaining. Múltiplos extractors coexistindo (api+operators no mesmo binário).
**Arquivos afetados:** `pkg/logging/context.go`, todos os consumidores (10 imports).

### S9-6. Property-based tests para state machines + naming
**Por quê:** `pkg/statemachine/*_test.go` (~1500 LOC) testa transições explicitamente listadas. Falta verificar invariantes globais (e.g., "todo terminal state é alcançável", "todo evento da spec está implementado"). `pkg/naming/naming_test.go` (671 LOC) testa cases listados, mas `Sanitize` deveria sempre retornar RFC-1123 ou empty.
**Ação:** Usar `gopter` ou `testing/quick` para gerar inputs aleatórios e verificar invariantes. State machines: aleatorizar sequências de eventos, garantir que terminais não emitem mais eventos. Naming: gerar strings aleatórias, garantir `ValidateKubernetesName(Sanitize(s))` é sempre `nil` ou empty.
**Impacto esperado:** Detecta classes inteiras de bugs (e.g., naming.Sanitize com unicode). PKG-G05 levantado para nível Crossplane.
**Arquivos afetados:** `pkg/statemachine/machine_test.go`, `pkg/naming/naming_test.go`.

### S9-7. Geração de diagramas SVG das state machines
**Por quê:** Operators e API compartilham 9 state machines (~870 LOC em `pkg/statemachine/`). Doc atual descreve em texto. `looplab/fsm.Visualize` gera Graphviz; cada PR que toca state machine deveria atualizar o SVG automaticamente.
**Ação:** Adicionar `func (m *Machine) Visualize() string` retornando DOT format. CI gera SVGs em `docs/state-machines/*.svg`. PR diff destaca mudanças.
**Impacto esperado:** Documentation stays in sync with code (PKG-G13). Code review de transições fica visual.
**Arquivos afetados:** `pkg/statemachine/machine.go`, novo gen-script em `infra/tools/`, CI step.

## Post-Launch (após Sprint 9)

### PL-1. Adotar `cockroachdb/errors` para network portability
**Por quê:** Multi-cell architecture (CHG-2026-04-30-INFRA-NORMALIZE) eventualmente terá errors trafegando entre cells. `errors.RegisterTypeMigration` permite renaming sem quebrar wire compat. Stack traces preservados cross-process.
**Tradeoff:** +1 dep externa, +5MB binary, mas elimina classe de bugs distributed.

### PL-2. RFC 9457 problem+json auto-generation
**Por quê:** Após S9-3 (separação HTTP), gerar OpenAPI 3.1 com `application/problem+json` schemas a partir do registry de LoopZeroError codes. Cliente CLI consome shape estável.
**Tradeoff:** +1 codegen step. Reduz drift CLI↔API contracts.

### PL-3. Audit events package (`pkg/audit`)
**Por quê:** `audit_log` table é populada via strings ad-hoc em routes/services. Tipar via `pkg/audit.Event` types compartilhados garante shape estável + dedupe entre api/operators.

### PL-4. Observability primitives (`pkg/observability`)
**Por quê:** Tracing/metrics/logging context propagation hoje espalhados. Novo pacote unifica `tracing.WithSpan`, `metrics.Counter`, `logging.WithFields` atrás de OTel.

### PL-5. ULID-style IDs em `pkg/id`
**Por quê:** `build-{16hex}` é random sem ordering. Ordenação por `created_at` em queries. ULID sortable + globally unique resolve ambos. Migration path: novos IDs ULID, antigos coexistem.

## Anti-Patterns a Eliminar

1. **Dead code documentado como SOTA** (`pkg/health/aggregator.go`, `phase.go`, `component.go` — 0 imports). Solução: QW-1.
2. **HTTP transport leakage** (`LoopZeroError.HTTPStatus` em `pkg/errors/errors.go:22-23`). Solução: S9-3.
3. **Direct `*sql.DB` dependency** (`pkg/enforcement/limiter.go:14-23`, `counters.go:10-21`). Solução: S9-4.
4. **Global mutable state** (`pkg/logging/context.go:17-20` `globalExtractor`). Solução: S9-5.
5. **Não usar nosso próprio circuit breaker** (`pkg/jwt/validator.go:249-287` faz HTTP direto). Solução: QW-4.
6. **Stringly-typed labels** (`pkg/naming/naming.go:240-249` retorna `map[string]string`). Solução: QW-5.
7. **Doc/code drift** (architecture.md descreve `cloud/` como consumidor major; realidade é 4/89 imports). Solução: QW-7.
8. **Não-validators em `pkg/id`** (consumidores reimplementam regex). Solução: QW-3.
9. **JWT sem RFC 8725bis hardening** (`pkg/jwt/validator.go:78-99`). Solução: QW-2.
10. **State machine `Visualize()` ausente** (impossível diff visual). Solução: S9-7.

## New SOTA Gates Proposed

### Gate PKG-G14: Documentation Reflects Real Usage

**Objetivo:** Eliminar drift entre docs e código.

**Critério de aprovação:**
- Cada pacote em `pkg/` listado em `architecture.md` tem ≥ 1 importador real fora de `pkg/` em produção.
- Tabela "Real Usage Matrix" em `architecture.md` é regenerada e atualizada quando a contagem muda em ±20%.
- Pacotes sem consumidor são marcados `experimental` ou removidos.

**Evidência esperada:**
- Script `task pkg:usage-matrix` que gera a tabela via `grep -r "github.com/loopzero/loopzero/pkg/X" --include="*.go"`.
- CI step que falha se a doc estiver out-of-date.

**FAANG reference:** kstatus README publica tabela de "supported resource types" e CI valida contra package code. google/go-cloud marca APIs `EXPERIMENTAL` quando ainda em flux.

### Gate PKG-G15: No Transport Coupling in Domain Errors

**Objetivo:** Manter `pkg/errors` portável entre HTTP, gRPC, CLI, audit, log.

**Critério de aprovação:**
- `LoopZeroError` não tem campos específicos de transport (HTTPStatus removido ou movido para sub-package).
- Mapeamento Code→HTTPStatus vive em `pkg/errors/http/`.
- Consumidores não-HTTP (CLI, operators) não precisam importar `pkg/errors/http`.

**Evidência esperada:**
- Imports de `pkg/errors` por operators NÃO referenciam `HTTPStatus`.
- RFC 9457 problem+json schema gerado via lookup table.

**FAANG reference:** google.rpc.Status (gRPC) separa `code int32`, `message string`, `details []google.protobuf.Any`. HTTP status mapping é feito em `runtime/grpc-gateway` separadamente.

### Gate PKG-G16: Contract Layer Eats Its Own Dogfood

**Objetivo:** Pacotes em `pkg/` USAM outros pacotes em `pkg/` quando aplicável.

**Critério de aprovação:**
- `pkg/jwt` chamadas externas (JWKS) são protegidas por `pkg/circuitbreaker`.
- `pkg/enforcement` erra com `*errors.LoopZeroError`, não com strings.
- `pkg/statemachine` em transições usa `errors.NewInvalidTransition` em `TryTransition`.
- Nenhum pacote em `pkg/` reimplementa primitive já existente em outro pacote `pkg/`.

**Evidência esperada:**
- Grep de `errors.New` ou `fmt.Errorf` dentro de `pkg/` retorna apenas casos justificados (validation, internal).
- HTTP calls em `pkg/` passam por circuit breaker registrado.

**FAANG reference:** crossplane-runtime usa `crossplane-runtime/pkg/errors` em todos seus sub-pacotes; Kubernetes apimachinery usa apimachinery types em todo o code-tree.

### Gate PKG-G17: Generics + Context-Aware Primitives

**Objetivo:** Resilience e shared primitives modernos seguem standard Go 1.21+.

**Critério de aprovação:**
- `pkg/circuitbreaker.Breaker` é genérico em retorno (`Breaker[T any]`).
- `Execute(ctx, fn)` aceita `context.Context` e propaga `context.Canceled`/`DeadlineExceeded` corretamente.
- Cancellation errors são excluídos das contagens de failure (`IsExcluded`).

**Evidência esperada:**
- `pkg/circuitbreaker_test.go` cobre cancellation propagation.
- API nova ergonômica: `cb.Execute(ctx, func(ctx context.Context) (Result, error) { ... })`.

**FAANG reference:** [sony/gobreaker v2](https://github.com/sony/gobreaker) (Sony) e [mercari/go-circuitbreaker](https://github.com/mercari/go-circuitbreaker) (Mercari) — ambos generics + context-aware.

### Gate PKG-G18: Property-Based Tests for Invariant-Heavy Pkgs

**Objetivo:** State machines e naming são invariant-heavy. Testes de invariante via property-based.

**Critério de aprovação:**
- `pkg/statemachine` tem property-based tests verificando: (a) terminais não aceitam eventos; (b) toda transição é unique (não-determinística é bug); (c) toda terminal é alcançável a partir de `Initial`.
- `pkg/naming` tem property-based tests verificando: para qualquer string `s`, `Sanitize(s)` é vazio OU `ValidateKubernetesName(Sanitize(s)) == nil`.

**Evidência esperada:**
- Uso de `testing/quick` ou `gopter` em `*_test.go`.
- ≥ 1000 random iterations por property.

**FAANG reference:** Crossplane-runtime usa `gopter` em `pkg/resource`. CockroachDB usa property-based tests via `randgen` para SQL parser invariants.

## Sources

### Foundational
- [Effective Go](https://go.dev/doc/effective_go) — Go idiom and minimal API surface
- [Go Package Names](https://go.dev/blog/package-names) — package naming guidance
- [Google Style Guide for Go](https://google.github.io/styleguide/go/best-practices.html) — minimal API surface, option structs
- [Cloud API Design Guide](https://cloud.google.com/apis/design) — Google AIP

### Shared Library Design
- [google/go-cloud design.md](https://github.com/google/go-cloud/blob/master/internal/docs/design.md) — portable type+driver pattern, no global state, As-functions for escape hatches
- [Eleven Tips for Structuring Your Go Projects (Alex Edwards, 2025)](https://www.alexedwards.net/blog/11-tips-for-structuring-your-go-projects) — internal/ pattern, package size

### Errors
- [cockroachdb/errors README](https://github.com/cockroachdb/errors) — network portability, type migration, hint+detail pattern
- [CockroachDB Error Handling RFC](https://github.com/cockroachdb/cockroach/blob/master/docs/RFCS/20190318_error_handling.md) — origin and design rationale
- [RFC 9457 — Problem Details for HTTP APIs](https://datatracker.ietf.org/doc/html/rfc9457) — wire format for HTTP errors
- [HTTP Error Handling in Go (Blackwell Systems, 2025)](https://blog.blackwell-systems.com/posts/err-envelope-http-errors/) — chi/gin/echo error envelopes

### State Machines
- [looplab/fsm](https://github.com/looplab/fsm) — event-driven FSM with visualization
- [qmuntal/stateless](https://github.com/qmuntal/stateless) — UML statechart, hierarchical states, external storage
- [Temporal Go SDK Workflow](https://pkg.go.dev/go.temporal.io/sdk/workflow) — workflow-as-code, Saga pattern
- [Temporal: Workflow Engine Design Principles](https://temporal.io/blog/workflow-engine-principles) — determinism rationale
- [GFSM: Simple and Fast FSM for Go (2024)](https://sysdev.me/2024/11/25/gfsm-a-simple-and-fast-finite-state-machine-for-go/) — performance benchmark

### Circuit Breaker
- [sony/gobreaker v2](https://github.com/sony/gobreaker) — generics + Settings struct, de-facto Go standard
- [mercari/go-circuitbreaker](https://github.com/mercari/go-circuitbreaker) — context-aware by default
- [Martin Fowler — Circuit Breaker](https://martinfowler.com/bliki/CircuitBreaker.html) — pattern foundation
- [Circuit Breaker Patterns in Go Microservices (DEV, 2025)](https://dev.to/serifcolakel/circuit-breaker-patterns-in-go-microservices-n3) — production patterns

### Conditions / Health
- [kstatus package](https://pkg.go.dev/sigs.k8s.io/cli-utils/pkg/kstatus) — abnormal-true polarity, observedGeneration discipline
- [cli-utils kstatus README](https://github.com/kubernetes-sigs/cli-utils/blob/master/pkg/kstatus/README.md) — Reconciling/Stalled standard conditions
- [kpt CRD Status Convention](https://kpt.dev/reference/schema/crd-status-convention/) — phase fallback to Ready
- [Kubebuilder Good Practices](https://book.kubebuilder.io/reference/good-practices) — meta.SetStatusCondition usage
- [Operator SDK Advanced Topics](https://sdk.operatorframework.io/docs/building-operators/golang/advanced-topics/) — apimachinery condition helpers
- [heptio/healthcheck (archived)](https://github.com/vmware-archive/healthcheck) — liveness/readiness separation, async checks
- [GlobalWebIndex/healthcheck](https://pkg.go.dev/github.com/GlobalWebIndex/healthcheck) — active fork with gRPC support
- [What the heck are Conditions in Kubernetes controllers? (Mael Vallès)](https://maelvls.dev/kubernetes-conditions/)

### JWT Security
- [RFC 8725 — JWT Best Current Practices](https://datatracker.ietf.org/doc/html/rfc8725) — IETF baseline
- [draft-ietf-oauth-rfc8725bis-04](https://datatracker.ietf.org/doc/draft-ietf-oauth-rfc8725bis/) — 2026 update with PBES2 limits, format confusion mitigation
- [PortSwigger — Algorithm Confusion Attacks](https://portswigger.net/web-security/jwt/algorithm-confusion) — exploitation path
- [Curity — JWT Security Best Practices](https://curity.io/resources/learn/jwt-best-practices/) — JWKS rotation, audience validation
- [Vaadata — JWT Vulnerabilities & Best Practices](https://www.vaadata.com/blog/jwt-json-web-token-vulnerabilities-common-attacks-and-security-best-practices/) — `none` algo, `jku` attacks

### Observability
- [OpenTelemetry Go Propagation](https://pkg.go.dev/go.opentelemetry.io/otel/propagation) — context propagation contract
- [OpenTelemetry Context Spec](https://opentelemetry.io/docs/specs/otel/context/) — cross-cutting context model
