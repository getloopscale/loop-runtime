---
type: decision
title: "ADR D2 — `pkg/domain.EnvRef` Value Object"
---

# ADR D2 — `pkg/domain.EnvRef` Value Object

**Status:** Accepted
**Date:** 2026-06-11
**Plan:** v1.2 § ADR D2 + Task T1.3
**Tier:** T3 (architecture — new public package `pkg/domain`) per `rules/pre-release-governance.md`

## Context

Pattern-suggester finding #58 surfaced: the triple `(tenant_id, project, env_name) string` appears in **34 files / 70+ call sites** across `api/internal/`, `cloud/internal/`, `operators/internal/` without encapsulation. Argument-order swaps compile silently (Go primitive string types provide zero protection); adding a 4th field (e.g., `cell_id`) is shotgun surgery across 34 files.

This is the structural root of `feedback_no_hardcode_gate_2026_06_09` — when devs have no Value Object, they reach for string literals as the convenient escape hatch.

## Decision

NEW `pkg/domain/env_ref.go` exports:
- `EnvRef` struct with named fields `TenantID, Project, EnvName, CellID`
- Constructor `NewEnvRef(tenant, project, env string, cell ...string) (EnvRef, error)` validating each field
- Parser `ParseEnvRef(string) (EnvRef, error)`
- Formatter `(EnvRef) String() string`

**Separator codification (EC-4 absorbed):** `String()` uses ASCII Unit Separator `\x1f` (U+001F) — guaranteed absent from valid RFC 1123 identifier chars. Constructor + each field setter validates `\x1f` is NOT present; returns `ErrInvalidEnvRefField` if so. Rationale: hyphen separator fails round-trip on values containing `-` (e.g., env name `staging-eu-1` from H6).

Migration target: 34 call sites of `pkg/naming.Namespace(...)` and equivalents accept `EnvRef`. Existing variadic-string overload kept as DEPRECATED shim during migration; removed in T1.3 follow-up commit.

## Alternatives

### (a) Keep raw strings; add static analysis lint — REJECTED

Lint can't catch all 70+ call sites at compile time; doesn't prevent future `cell_id` shotgun surgery. Lint is a CI gate, not a type-system guarantee — the structural problem persists.

### (b) Use type aliases (`type TenantID string`) — REJECTED

Go type aliases don't prevent argument-order swap when both args are `string`-derived types: `func f(t TenantID, p ProjectID)` still accepts `f("foo", "bar")` because Go converts untyped string constants. Only NAMED struct types provide the compile-time safety we need.

### (c) Use struct embedding (`type EnvRef struct { naming.NamespaceArgs }`) — REJECTED

Leaks the `naming` package's concern (computing K8s namespace) into the domain Value Object. DIP violation per [`rules/architecture.md`](../technical/pkg/architecture.md) — domain depends on naming, not the inverse.

### (d) Functional Options pattern (`NewEnvRef(WithTenant("x"), WithProject("y"))`) — REJECTED

Verbose at call site (34 files × multi-line construction); doesn't compose with existing call sites that pass positional strings. Cost > benefit for fields that are ALL required.

## Rationale

Cited principles:
- **Domain-Driven Design (Evans 2003)** § Value Object pattern — identity is by value composition, not reference
- **DIP** per [`rules/architecture.md`](../technical/pkg/architecture.md) — high-level domain abstraction; handlers depend on `pkg/domain`, not `pkg/naming`
- **Twelve-Factor App § III (Config in environment)** — typed identity, not stringly-typed

Memory: `feedback_no_hardcode_gate_2026_06_09` (structural root); `feedback_endpoint_contract_narrow_consumer_audit` (related pattern of string-form silent drift).

## Consequences

### Positive
- 34 files protected from arg-order swap at COMPILE time (not runtime, not lint)
- Adding `cell_id` to `EnvRef` becomes single-file change (constructor + parser) instead of shotgun surgery
- Closes pattern-suggester #58 + structural root of hardcode tendency
- DDD Value Object pattern adopted establishes precedent for future domain types (`PlanTier` D4 uses same pattern)

### Negative
- Initial migration touches 34 files (T1.3 effort: split into per-package commits per Drawbacks § R3 mitigation)
- Adds a `pkg/domain` dependency to packages that previously had none (audit: zero cycles introduced — `pkg/domain` depends on nothing else under `pkg/`)

### Neutral
- ASCII Unit Separator `\x1f` is unusual but unambiguous; documented in package godoc

## References

- Plan v1.2 § ADR D2 + T1.3 + edge case EC-4
- Pattern-suggester finding #58 (`code-review-output/findings/phase3-patterns.md`)
- ADR D1 (Tilt YAML mount) — sibling Phase 0 ADR
- ADR D3 (DefaultPermanentEnv) — sibling, consumes EnvRef indirectly
