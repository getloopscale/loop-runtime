# ADR D4 — Plan tier canonical resolution pattern

**Status:** Accepted
**Date:** 2026-06-11
**Plan:** v1.2 § ADR D4 + Task T2.3
**Tier:** T3 (architecture — billing/plan boundary; new public domain type) per `rules/pre-release-governance.md`

## Context

Code-reviewer finding H7 (CRITICAL): `api/internal/routes/projects.go:233` hardcodes `Plan: "starter"` in the `EnsureTenant(...)` call within the `Connect` handler. Risk: paying Pro/Team tenants get **silently downgraded** to Starter on auto-deploy reconnect — billing failure mode invisible from the handler's perspective.

This is the textbook case of `feedback_swallowed_silent_errors_pattern`: code compiles, returns nil error, silently violates the customer's purchased plan. Without an enforced resolution discipline, the next handler that needs a Plan tier will reach for the same literal `"starter"` fallback.

## Decision

NEW `pkg/domain/plan_tier.go`:

```go
package domain

type PlanTier string

const (
    PlanTierUnknown    PlanTier = ""
    PlanTierStarter    PlanTier = "starter"
    PlanTierPro        PlanTier = "pro"
    PlanTierTeam       PlanTier = "team"
    PlanTierEnterprise PlanTier = "enterprise"
)

// ResolvePlanTier queries the plans table for the tenant's current tier.
// Returns:
//   - (PlanTierStarter, nil) for brand-new tenant with NO row (onboarding fallback ONLY)
//   - (<resolved tier>, nil) when row exists with recognized Plan value
//   - (PlanTierUnknown, ErrUnknownPlanTier) when row exists but Plan value is unrecognized
//   - (PlanTierUnknown, <db-error>) on transport failure
//
// Callers MUST handle ErrUnknownPlanTier — typically 503 + audit log + ops alert.
// Silent downgrade is forbidden by the contract.
func ResolvePlanTier(ctx context.Context, db pgxQuerier, tenantID string) (PlanTier, error)
```

`EnsureTenant` accepts `PlanTier` (not `string`); handler in `projects.go:233` calls `ResolvePlanTier(ctx, h.db, tenantID)` first, propagates error to client on `ErrUnknownPlanTier`.

**Error-over-default codification (EC-8 absorbed):** When DB row's Plan column contains an UNRECOGNIZED value (future tier added to DB but not yet to enum, manual DB edit, data corruption), `ResolvePlanTier` returns `(PlanTierUnknown, ErrUnknownPlanTier)` — **NEVER** defaults to `PlanTierStarter`. This forbids silent downgrade of paying tenants BY CONSTRUCTION.

Brand-new tenants with NO row (onboarding flow): returns `(PlanTierStarter, nil)` explicitly — that's the ONLY fallback path, documented in godoc + tested.

## Alternatives

### (a) Default to `nil` PlanTier and require explicit caller handling — REJECTED

Forces every caller to handle resolution + dispatch logic. 8+ handlers would need identical retry/default code. DRY violation. Bug-prone: one handler forgets the resolution call and silent downgrade reappears.

### (b) Default to `PlanTierStarter` only when DB row is missing (brand-new tenant) AND silently log warning on unknown values — REJECTED

The "silently log warning" is exactly the `feedback_swallowed_silent_errors_pattern` we're trying to eliminate. Logs are not alerts. Logs are not enforcement. Customer downgrade with a log entry is still customer downgrade.

### (c) Embed Plan in JWT claims (no DB lookup) — REJECTED

Plan changes mid-session (customer upgrades from Starter → Pro via dashboard) require JWT refresh to take effect. Caching plan in JWT introduces consistency lag. DB lookup with optional caching layer is safer; caching can be added later behind the same interface (Repository pattern per ADR D6 hot-path decomposition).

### (d) Use string type + runtime switch in each caller — REJECTED

Untyped strings are exactly the structural problem that ADR D2 (EnvRef) solves. Same anti-pattern at the Plan tier layer. Domain types over primitives — DDD canonical guidance.

## Rationale

Cited principles:
- **Domain-Driven Design** § Value Object + Enum-style domain type
- **Fail-fast over silent default** per `rules/design-principles.md` § Error Handling (error returns are part of the contract; defaults that hide violation are anti-pattern)
- **Twelve-Factor App § XI (Logs as event streams)** — logs report; alerts/errors enforce; we need enforcement at the call site

Memory: `feedback_swallowed_silent_errors_pattern` (4-bug pattern eliminated); `feedback_no_hardcode_gate_2026_06_09` (closes literal `"starter"` hardcode).

## Consequences

### Positive
- Closes H7 (silent downgrade — single line bug, max impact)
- Type-safe Plan tier propagation across `api/`, `cloud/`, `operators/`
- Future tiers added to enum trigger compile errors in switch statements (exhaustiveness check via `exhaustruct` lint or manual switch coverage test)
- Pre-empts the same pattern from appearing in 8+ other handlers

### Negative
- DB query on every project-create call path (mitigated by `ResolvePlanTier` accepting cached `db pgxQuerier` interface — caching layer can wrap)
- Breaking change to `EnsureTenant` signature — every existing caller must migrate

### Neutral
- Brand-new tenant fallback path documented + tested; only ONE legitimate default path remains

## References

- Plan v1.2 § ADR D4 + T2.3 + edge case EC-8
- Code-reviewer finding H7 (`code-review-output/findings/phase3-code-review.md`)
- ADR D2 (`EnvRef`) — sibling pattern (typed identity over strings)
