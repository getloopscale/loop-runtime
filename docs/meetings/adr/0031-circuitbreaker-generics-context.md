# ADR-0031 — Circuit breaker generics + context (Breaker[T])

## Status

Accepted (async, pending human ratification) — 2026-05-04 (MEET-2026-05-04-PKG-SOTA-ASYNC-AMENDMENT)

## Context

`pkg/circuitbreaker/circuitbreaker.go` (188 LOC) é v1-style:

- `func (b *Breaker) PreCheck() error; func (b *Breaker) RecordResult(err error)` — caller cobra/registra manualmente.
- Sem generics — `result, err := doStuff()` requer assertion ou closure local.
- Sem `context.Context` propagation — cancellation durante deploy não tem fastpath.
- Sem `IsExcluded(err)` — `context.Canceled` conta como failure (falsa-saturação).

`sony/gobreaker v2` (2024+) adotou:

```go
func New[T any](st Settings) *CircuitBreaker[T]
func (cb *CircuitBreaker[T]) Execute(req func() (T, error)) (T, error)
```

`mercari/go-circuitbreaker` adicionou context-awareness por default. Ambos viraram referência Go ecosystem.

5 importadores existentes em `api/internal/services/` + 1 (após T1.4) em `pkg/jwt`:
- `pg_circuit_breaker.go`, `workflow_circuit_breaker.go`, `provisioner_circuit_breaker.go`, etc.

## Decision

Migrar `pkg/circuitbreaker` para v2 API com 1-release deprecation window:

1. **Breaker[T any] type:** novo struct exportado.
2. **Execute method:** `func (b *Breaker[T]) Execute(ctx context.Context, fn func(ctx context.Context) (T, error)) (T, error)`.
3. **Config additions:**
   - `IsExcluded func(error) bool` — default exclui `context.Canceled` e `context.DeadlineExceeded` das failure counts.
4. **v1 API permanece funcional:** `PreCheck`/`RecordResult` recebem `// Deprecated:` comments.
5. **Migration ordering:** pg_circuit_breaker → workflow_circuit_breaker → outros api/internal → pkg/jwt (após T1.4 já ter usado v1).

## Alternatives Considered

1. **Adopt `sony/gobreaker v2` as external dep.** REJECTED — `pkg/circuitbreaker` é battle-tested em produção; refactor é small (~100 LOC); manter zero deps externas em pkg/.
2. **Skip generics, only add context.** REJECTED — type safety é metade do valor; consumers se beneficiam de inference.
3. **Force breaking migration sem window.** REJECTED — 5 importadores ativos; risco de regression.
4. **Manter v1 e adicionar v2 paralelo permanente.** REJECTED — long-term tech debt; CONDITION-1 (sre-engineer) exige deprecation runbook.

## Consequences

### Enables

- Gate PKG-G17 (Generics + Context-Aware Resilience Primitives) GREEN.
- Cancellation propagation correta — `loopzero deploy --cancel` não satura CB falsamente.
- Type-safe consumer code: `result, err := cb.Execute(ctx, fn)` retorna T concreto.
- Eat-your-own-dogfood (gate PKG-G16) — `pkg/jwt` JWKS migra para v2 em T2.1.

### Constrains

- 1-release deprecation window — durante transição, ambas APIs coexistem.
- CHANGELOG Migration Notes section mandatória (CONDITION-1).
- Internal `breakerImpl` não-exportado para shared state entre Breaker[T] genérico e v1.

## Implementation Reference

- Plan task: T2.1 em `docs/plans/pkg-sota-faang-grade-plan.md`.
- Files affected: `pkg/circuitbreaker/circuitbreaker.go`, `circuitbreaker_test.go`, 5+ consumers em `api/internal/services/` + `pkg/jwt/validator.go`.

## References

- [sony/gobreaker v2](https://github.com/sony/gobreaker)
- [mercari/go-circuitbreaker](https://github.com/mercari/go-circuitbreaker)
- [Martin Fowler — Circuit Breaker](https://martinfowler.com/bliki/CircuitBreaker.html)
- `docs/technical/pkg/improvement-roadmap.md` § S9-1
