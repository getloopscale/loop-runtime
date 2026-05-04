# ADR-0033 — Enforcement DIP via LimitsStore interface

## Status

Accepted (async, pending human ratification) — 2026-05-04 (MEET-2026-05-04-PKG-SOTA-ASYNC-AMENDMENT)

## Context

`pkg/enforcement/limiter.go:14-23` e `counters.go:10-21` acoplam diretamente a `*sql.DB` (driver do Postgres):

```go
type PlanLimiter struct {
    db *sql.DB  // ← acoplamento de banco em shared lib
    ...
}
```

Sintomas:

- Tests em `limiter_test.go` (991 LOC) usam `github.com/DATA-DOG/go-sqlmock` para contornar — sintoma claro de design errado per Pre-release Governance §13 #11 ("Não usar nosso próprio circuit breaker — chamada externa em pkg/X faz HTTP direto sem usar pkg/circuitbreaker").
- Operators ou CLI que precisem de plan limits importariam `database/sql` desnecessariamente.
- Backend de limits hipotético (in-memory cache, OPA policy) é impossível sem refactor.
- Viola DIP — pacote shared não deve depender de driver específico.

`pkg/go.sum` tem `DATA-DOG/go-sqlmock` apenas para tests do enforcement. Removendo o coupling, podemos eliminar essa dep externa.

## Decision

Refactor `pkg/enforcement` para aceitar `LimitsStore` interface:

```go
// pkg/enforcement/store.go (NEW)
type LimitsStore interface {
    GetTenantPlan(ctx context.Context, tenantID string) (string, error)
    GetLimits(ctx context.Context, plan, metric string) (hardLimit, softLimit int, err error)
}

// pkg/enforcement/limiter.go
type PlanLimiter struct {
    store  LimitsStore
    logger *slog.Logger
}
func NewPlanLimiter(store LimitsStore, logger *slog.Logger) *PlanLimiter
```

Implementação SQL move para `api/internal/db/limits_store.go`. Wireup em `api/cmd/main.go`.

Tests em `pkg/enforcement` usam `inMemoryLimitsStore` map-based (drop go-sqlmock). Tests em `api/internal/db/limits_store_test.go` usam sqlmock (driver-specific test fica em api/).

## Alternatives Considered

1. **Manter `*sql.DB` direto.** REJECTED — viola DIP; tests com 991 LOC de go-sqlmock são red flag.
2. **Use full repository pattern (LimitsRepository com 10+ métodos).** REJECTED — over-engineering para 2 queries; ISP cleaner com interface focada.
3. **Use generic `Storage[T]` pattern.** REJECTED — YAGNI; só temos 1 use case (limits).

## Consequences

### Enables

- Pacote `pkg/enforcement` testável sem deps externas.
- Operators ou CLI podem usar enforcement com backend in-memory ou OPA.
- `pkg/go.sum` perde dep `DATA-DOG/go-sqlmock` (somente test dep, mas reduz noise).
- Pre-release Governance §13 #11 GREEN.

### Constrains

- `api/cmd/main.go` precisa wireup explicit do adapter (one-time cost).
- Adapter SQL em `api/internal/db/limits_store.go` é nova superfície de teste.

## Implementation Reference

- Plan task: T2.4 em `docs/plans/pkg-sota-faang-grade-plan.md`.
- Files: `pkg/enforcement/store.go` (NEW), `pkg/enforcement/limiter.go` (refactor), `pkg/enforcement/limiter_test.go` (rewrite), `api/internal/db/limits_store.go` (NEW), `api/cmd/main.go` (wireup), `pkg/go.mod` (drop test dep).

## References

- Pre-release Governance §13 (Code Review Checklist)
- `docs/technical/pkg/improvement-roadmap.md` § S9-4
- ADR-0006 (eliminate state duality — same DIP discipline)
