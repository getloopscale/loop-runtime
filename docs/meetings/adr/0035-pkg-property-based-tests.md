# ADR-0035 — Property-based tests for pkg/statemachine and pkg/naming

## Status

Accepted (async, pending human ratification) — 2026-05-04 (MEET-2026-05-04-PKG-SOTA-ASYNC-AMENDMENT)

## Context

`pkg/statemachine/*_test.go` (~1500 LOC) testa transições explicitamente listadas. `pkg/naming/naming_test.go` (671 LOC) testa cases listados. Ambos pacotes são **invariant-heavy** — exemplos não cobrem o espaço de inputs.

Invariantes não testadas:

- **statemachine:** terminais não aceitam events (panic-on-invalid OU TryTransition retorna error).
- **statemachine:** transições são únicas — mesmo (state, event) sempre vai para mesma destination (não-determinismo é bug per ADR design).
- **statemachine:** toda terminal é alcançável a partir de algum estado inicial.
- **naming:** para qualquer string `s`, `Sanitize(s)` retorna empty OU passes `ValidateKubernetesName`.
- **naming:** `ValidateKubernetesName(name)` é deterministic (mesma string → mesma resposta).

Crossplane-runtime usa `gopter`; CockroachDB usa `randgen`. `testing/quick` (stdlib) é suficiente para nossos invariants.

## Decision

Adotar `testing/quick` (stdlib, alinhado com PKG-G03 zero-deps) em:

1. **`pkg/statemachine/properties_test.go` (NEW):**
   - `TestProperty_TerminalStatesRejectAllEvents` — para cada terminal de cada machine, 1000 random events → TryTransition retorna error.
   - `TestProperty_TransitionsAreDeterministic` — (state, event) → mesma destination em N invocações.
   - `TestProperty_ValidEventsListMatchesTransitionMap` — `ValidEvents(s)` retorna exatamente as keys do internal map.

2. **`pkg/naming/properties_test.go` (NEW):**
   - `TestProperty_SanitizeAlwaysProducesValidOrEmpty` — para 10000 random strings, `Sanitize(s)` empty OR `ValidateKubernetesName(Sanitize(s)) == nil`.
   - `TestProperty_ValidateIsDeterministic` — same input → same response.
   - `TestProperty_TenantNamespaceFormatStable` — given (tenantName) válido, output sempre `loopzero-{tenantName}` formato.

CONDITION-3 (verification-architect) aplica: tests rodam em `task test:unit` default (não atrás de tag `+experimental`). Cap: ≤ 1000 iterations por property para manter test runtime razoável.

## Alternatives Considered

1. **Use `gopter`.** REJECTED — dep externa; testing/quick suficiente.
2. **Use `go-fuzz`.** REJECTED — orientado a parser/format inputs; menos adequado para state machine invariants.
3. **Skip property tests, manter only example-based.** REJECTED — gate PKG-G18 não atendido; bugs invariant-related continuam latentes.

## Consequences

### Enables

- Gate PKG-G18 GREEN.
- Detecta classes inteiras de bugs (e.g., naming.Sanitize com unicode multi-byte; statemachine event de terminal).
- Confiança em modificações futuras (refactor de Sanitize, novas state machines).

### Constrains

- ~150 LOC de tests novos em ambos pacotes.
- Test runtime aumenta marginalmente (~2-5s para 1000+1000 iterations).

## Implementation Reference

- Plan task: T2.6 em `docs/plans/pkg-sota-faang-grade-plan.md`.
- Files: `pkg/statemachine/properties_test.go` (NEW), `pkg/naming/properties_test.go` (NEW).

## References

- [testing/quick package](https://pkg.go.dev/testing/quick)
- Crossplane-runtime use of gopter
- `docs/technical/pkg/improvement-roadmap.md` § S9-6 + Gate PKG-G18
