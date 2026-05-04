# ADR-0034 — Logging no global state (ContextLogger)

## Status

Accepted (async, pending human ratification) — 2026-05-04 (MEET-2026-05-04-PKG-SOTA-ASYNC-AMENDMENT)

## Context

`pkg/logging/context.go:17-20` mantém:

```go
var (
    globalExtractor TraceExtractor
    extractorMu     sync.RWMutex
)
```

E expõe `RegisterTraceExtractor(fn TraceExtractor)` como API global. `FromContext(ctx, logger)` consulta esse estado global.

Problemas:

1. **Google CDK design principle violado:** [google/go-cloud/internal/docs/design.md](https://github.com/google/go-cloud/blob/master/internal/docs/design.md): "the Go CDK should not introduce global state. Global state is difficult to reason about in large codebases".
2. **Race-prone:** trocar tracing backend em runtime é safe contra CDC mas leitor concorrente pode ver state inconsistente entre `extractor` e `_test`.
3. **Múltiplos extractors impossíveis:** quando api e operators rodam no mesmo binary (caso real em alguns deploys), não é possível ter extractors diferentes por contexto.
4. **Tests precisam reset global:** flakiness latente.

10 importadores externos consultam o registry global.

## Decision

Substituir registry global por construct explícito:

```go
// pkg/logging/context.go (refactor)
type ContextLogger struct {
    base      *slog.Logger
    extractor TraceExtractor
}
func NewContextLogger(base *slog.Logger, extractor TraceExtractor) *ContextLogger
func (c *ContextLogger) FromContext(ctx context.Context) *slog.Logger
```

Migration window: 1 release.

- v1 `RegisterTraceExtractor` recebe `// Deprecated:` comment.
- v1 `FromContext(ctx, logger)` package-level continua funcional via global (deprecated).
- 10 importadores migram em ordem definida (api/cmd primeiro, depois operators/cmd, depois services).

CONDITION-1 (sre-engineer) aplica: CHANGELOG Migration Notes mandatória.

## Alternatives Considered

1. **Manter global state.** REJECTED — viola Google CDK principle; race-prone.
2. **Use `sync.OnceValue[T]`.** REJECTED — ainda é estado global, com aparência de safe.
3. **Pass extractor via context.Context.** REJECTED — anti-pattern (`context.Value` para deps); explicit struct é mais idiomatic Go.

## Consequences

### Enables

- Tests sem reset global.
- Múltiplos extractors coexistindo (api+operators no mesmo binary).
- Gate PKG-G16 + Go CDK design alignment.
- Race detector clean sem mutex.

### Constrains

- 10 importadores migram (definidos em T2.5).
- 1-release deprecation window — durante transição, ambas APIs coexistem.
- Tests em `pkg/logging/context_test.go` ganham casos para isolation entre instances.

## Implementation Reference

- Plan task: T2.5 em `docs/plans/pkg-sota-faang-grade-plan.md`.
- Files: `pkg/logging/context.go`, `context_test.go`, `api/cmd/main.go`, 4 `operators/cmd/*/main.go`.

## References

- [google/go-cloud design principles](https://github.com/google/go-cloud/blob/master/internal/docs/design.md) §"No global state"
- `docs/technical/pkg/improvement-roadmap.md` § S9-5 + Anti-Patterns #4
