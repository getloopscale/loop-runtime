# MEET-2026-05-04-PKG-SOTA — Async Amendment Record

**Date:** 2026-05-04
**Type:** ASYNC AMENDMENT (per ADR-0022 plan-amendment-policy)
**Plan:** `docs/plans/pkg-sota-faang-grade-plan.md` v1.0
**Tier:** T3 (architecture, contract layer cascading to 89+19 importadores)
**Status:** APPROVED_WITH_CONDITIONS pending human ratification

## Context

This MEET is an **async amendment** captured during Ralph Loop autonomous execution of `pkg-sota-faang-grade-plan` v1.0. Per ADR-0022 §"Async exception (ralph loops + autonomous research loops)":

> When agent runs in iterative loop without human in synchronous meeting, agent may emit MEET-ASYNC-AMENDMENT-NNN.md record that synthesizes specialist agent positions via `Agent` tool delegation (Sponsor + Challenger), documents decision logic + alternatives rejected, and is treated as **pending human ratification** before plan completion is declared.

Plan completion (final `<promise>` emit) requires either: (a) all amendments ratified by human, OR (b) explicit MEET-ASYNC-AMENDMENT records committed with ratification placeholder. **This document is option (b).**

The pkg-sota plan was synthesized from a comprehensive SOTA research pass (`docs/technical/pkg/improvement-roadmap.md`) that read all 9 packages in `pkg/` (3052 LOC production / 5562 LOC tests), benchmarked against `crossplane-runtime`, `google/go-cloud`, `sony/gobreaker v2`, `kstatus`, and surveyed 14 external sources. Score baseline: 7.0/10 → target 9.0/10.

## Specialists Synthesized

The Ralph Loop agent synthesized positions from the following Staff/Principal personas defined in `.claude/agents/`. Each was consulted for its area of expertise; full rationale captured per ADR.

| Specialist | Scope | Position |
|---|---|---|
| platform-architect | D1 (delete pkg/health), D5 (pkg/conditions intro) | APPROVED — dead code is dead code; conditions kstatus is the right K8s-native model |
| platform-security-engineer | D2 (JWT RFC 8725bis) | APPROVED — algorithm whitelist + typ validation are MANDATORY pre-launch |
| api-engineer | D3 (errors transport decoupling) | APPROVED with condition: BREAKING change documented in CHANGELOG and Pre-release governance ratified |
| k8s-controller-engineer | D5 (pkg/conditions) | APPROVED — meta.SetStatusCondition is the canonical pattern |
| distributed-systems-engineer | D4 (CB generics + context) | APPROVED — context.Context propagation is the de-facto standard since Go 1.21 |
| sre-engineer | D4, D7 (deprecation windows) | APPROVED with condition: 1-release deprecation window MUST have explicit fallback runbook in CHANGELOG |
| dx-engineer | D5, D7 (migration ergonomics) | APPROVED — typed Labels + ContextLogger improve consumer ergonomics |
| verification-architect | D8 (property-based tests) | APPROVED with condition: property tests MUST run ≥ 1000 iterations and be triggered in CI default |
| platform-sdk-engineer | D6 (LimitsStore DIP) | APPROVED — `*sql.DB` direto em pkg/ é design smell |

## Decisions Ratified (D1–D8)

Each decision below is formalized as an ADR (0028–0035). Full text in `docs/meetings/adr/`.

### D1 → ADR-0028: Delete pkg/health

- **Decision:** `pkg/health/` será DELETADO em vez de wireado.
- **Alternatives rejected:**
  - "Wire pkg/health into platformbootstrap controller" — rejected: aggregate phase will be served by `pkg/conditions.AggregateToPhase`; manter ambos = duplicação semântica.
  - "Mark pkg/health experimental" — rejected: 0 imports significa que não tem usuário para feedback de experimentação.
- **Rationale:** Pre-release allows breaks; YAGNI; gate PKG-G01 NOT_MET há ≥2 sprints.

### D2 → ADR-0029: JWT RFC 8725bis hardening

- **Decision:** `WithValidMethods` whitelist explícito + `typ` header validation + alg case-sensitive verification + JWKS via `pkg/circuitbreaker`.
- **Alternatives rejected:**
  - "Defer to Sprint 9" — rejected: algorithm confusion attacks são exploitable hoje em deployments expostos.
  - "Migrar para `lestrrat-go/jwx/v2`" — rejected: ganho marginal; aumenta deps; `golang-jwt/jwt/v5` já suporta as practices se usado corretamente.
- **Rationale:** Security is non-negotiable; gate PKG-G11 reforçado.

### D3 → ADR-0030: Errors transport decoupling

- **Decision:** `LoopZeroError.HTTPStatus` field REMOVIDO; mapping fica em `pkg/errors/http.Status(err) int`. Helper `pkg/errors/http.WriteJSON(w, err)` emite RFC 9457 problem+json.
- **Alternatives rejected:**
  - "Manter HTTPStatus mas marcar deprecated" — rejected: deprecation sem migration window real apenas adia o break; pre-release is exactly when to break.
  - "Adicionar gRPC status field paralelo" — rejected: YAGNI até LoopZero precisar de gRPC.
- **Rationale:** gate PKG-G15; google.rpc.Status precedent; CLI/operators não devem importar HTTP semantics.
- **BREAKING:** documented in CHANGELOG.

### D4 → ADR-0031: Circuit breaker generics + context

- **Decision:** New `Breaker[T any]` with `Execute(ctx, fn) (T, error)`. v1 API deprecated com 1-release window. `IsExcluded` default ignora context errors.
- **Alternatives rejected:**
  - "Adopt sony/gobreaker v2 as dep" — rejected: já temos código battle-tested; refactor é small.
  - "Skip generics, only add context" — rejected: type safety é metade do valor.
- **Rationale:** sony/gobreaker v2 é the de-facto standard.

### D5 → ADR-0032: pkg/conditions kstatus-compatible

- **Decision:** New `pkg/conditions/` package. SetCondition wrappers + AggregateToPhase + standard names (Ready/Reconciling/Stalled).
- **Alternatives rejected:**
  - "Import sigs.k8s.io/cli-utils/pkg/kstatus directly" — rejected: dep heavy; ~50 LOC wrapper cobre 95% do uso.
  - "Continue ad-hoc per controller" — rejected: 8 controllers reescrevem a mesma lógica.
- **Rationale:** ADR-0025 (operator kstatus universal) já estabeleceu intent; this completes the pkg infrastructure.

### D6 → ADR-0033: Enforcement DIP via LimitsStore

- **Decision:** `LimitsStore` interface; SQL adapter em `api/internal/db`.
- **Alternatives rejected:**
  - "Manter `*sql.DB` direto" — rejected: viola DIP; tests usam 991 LOC de go-sqlmock.
  - "Use full repository pattern" — rejected: over-engineering para 2 query types.
- **Rationale:** Pre-release governance §13 (Code Review Checklist).

### D7 → ADR-0034: Logging no global state

- **Decision:** `ContextLogger` explicit constructor; `RegisterTraceExtractor` deprecated.
- **Alternatives rejected:**
  - "Manter global state" — rejected: viola Google CDK design principle.
  - "Use sync.OnceValue" — rejected: ainda é estado global, com aparência de safe.
- **Rationale:** Multiple extractors coexisting em api+operators é caso real; race-safety.

### D8 → ADR-0035: Property-based tests via testing/quick

- **Decision:** Adotar `testing/quick` (stdlib) em statemachine + naming. ≥ 1000 iterations.
- **Alternatives rejected:**
  - "Use gopter" — rejected: dep externa; testing/quick suficiente para nossos invariants.
  - "Use go-fuzz" — rejected: orientado a parser/format inputs, não invariants.
- **Rationale:** Stdlib alignment com PKG-G03 (no consumer leakage); 1000 iterations é threshold estatístico aceito.

## Conditions Imposed (Binding)

These conditions are BINDING amendments to the v1.0 task descriptions. Where condition contradicts task text, condition wins.

### CONDITION-1 (sre-engineer): Deprecation runbook for D4 + D7

T2.1 (CB v2 migration) and T2.5 (logging ContextLogger) MUST include in their PR a section in `CHANGELOG.md` Migration Notes describing:
- Exact callsite migration steps (before → after).
- Sentinel error behavior preserved.
- Rollback procedure if v2 breaks consumers.

### CONDITION-2 (api-engineer + platform-architect): T2.3 staged rollout

T2.3 (errors HTTP decoupling) BREAKING change MUST be staged:
- Stage A: introduzir `pkg/errors/http` lookup + `WriteJSON` helper.
- Stage B: migrate consumers (`api/internal/middleware/error_handler.go` + routes) to use helper.
- Stage C: remove `HTTPStatus` field from `LoopZeroError` + update sentinels.

If Stages A/B fail in any consumer test, abort C and revert.

### CONDITION-3 (verification-architect): Property tests in CI default

T2.6 property tests MUST run em `task test:unit` default (não atrás de tag `+experimental`). 1000 iterations cap.

### CONDITION-4 (platform-security-engineer): T1.2 NÃO pode ser deferred além de iter 2

JWT RFC 8725bis hardening é blocker para Sprint 9 launch readiness. Pode ser deferred 1 iter mas não mais. Loop iteration counter MUST track this.

### CONDITION-5 (sre-engineer): T3.2 dogfood NÃO pode ser short-circuited

`/dogfood full` em fresh infra é absolute gate. Tests passing ≠ system works. Only dogfood proves the contract layer mudanças não regridem produto.

## Amendments After Initial Capture

### Amendment A1 — ADR-0031 implementation pivot to package-level `Execute[T]` (2026-05-04)

**Original ADR-0031 decision:** introduce `Breaker[T any]` generic type as primary
v2 surface (sony/gobreaker v2 style).

**Pivot:** during T2.1 implementation we discovered that introducing
`Breaker[T any]` would require either: (a) renaming current `*Breaker` to a
non-generic helper, breaking 5 importers in `operators/internal/{argocd,gitea,
harbor,minio}/circuitbreaker_provisioner.go` plus `operators/cmd/tenant-operator/
main.go`; or (b) shipping two parallel types, growing API surface and
maintenance burden.

**Adopted alternative:** package-level generic function `Execute[T any](ctx,
*Breaker, fn) (T, error)`. This preserves 100% of v1 API (zero callsite
churn), adds context-aware cancellation handling and typed return values,
and matches mercari/go-circuitbreaker's "Execute" idiom — Go-idiomatic
because methods cannot have type parameters.

**Equivalence:** the four ADR-0031 success criteria still hold:
- Generic typed return value: ✅ (`func Execute[T any] ... (T, error)`).
- Context-aware: ✅ (`ctx context.Context` propagates into `fn`).
- IsExcluded for cancellation: ✅ (`Config.IsExcluded` field with
  `defaultIsExcluded` filter; `context.Canceled` and `DeadlineExceeded` do
  not advance failure counter).
- Migration window (1 release): ✅ (zero break — both styles coexist).

**Consequences:** ADR-0031 remains canonical decision; this amendment is
the implementation note. ADR-0031 status updated to "Accepted (implementation
pivot to package-level Execute via MEET amendment A1)".

## Outstanding Items Pending Human Ratification

1. ADRs 0028–0035 são draft `Status: Accepted (async, pending human ratification)`. Quando humano operador validar, mudar para `Accepted (ratified by <USER>)`.
2. Plan v1.0 → v1.1 quando todas as condições forem absorvidas. Atualmente: v1.0 ainda válido com este MEET como appendix.
3. Conditions binding listadas acima são tracking explicitamente em ralph-loop iteration log.

## Ratification Placeholder

> **NOTE TO HUMAN OPERATOR:** Antes do `<promise>` final, revise este MEET + as 8 ADRs. Se aprovado, edite este arquivo trocando `pending human ratification` por `Ratified by <NAME> on <DATE>`.

## References

- Plan: `docs/plans/pkg-sota-faang-grade-plan.md` v1.0
- Source research: `docs/technical/pkg/improvement-roadmap.md`
- Amendment policy: `docs/meetings/adr/0022-plan-amendment-policy.md`
- Precedent: `docs/meetings/adr/0023-split-tree-canonical.md` (operators-sota T3 meeting MEET-068)
