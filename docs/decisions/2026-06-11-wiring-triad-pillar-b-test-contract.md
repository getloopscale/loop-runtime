---
type: decision
title: "ADR D5 — Wiring triad pillar-(b) test contract"
---

# ADR D5 — Wiring triad pillar-(b) test contract

**Status:** Accepted
**Date:** 2026-06-11
**Plan:** v1.2 § ADR D5 + Task T3.6
**Tier:** T2 (testing contract; cross-cutting policy) per `rules/pre-release-governance.md`

## Context

Phase 4 (test-auditor) surfaced **3 bug-class proofs** where existing tests gave false confidence because they didn't assert config-driven behavior:

1. `cli/src/commands/login.ts:100` dev URL hardcoded — 8 tests exist, **0** assert URL flows from config
2. `cloud/internal/routes/device.go:17` `dashboardURL` field dead-wired — 12 tests exist, **0** assert the field is READ
3. `cloud/dashboard/src/pages/secrets.tsx:25` env filter hardcoded — 6 tests exist, **0** create a custom J-10 env and assert rendering

**Universal formula confirmed by triangulation across 6 specialists:**

> `tests_exist > 0 AND tests_assert_behavior == 0 → bug shipping inevitably`

This is the structural root of `feedback_post_feature_live_regression_workflow_2026_06_09` — sponsor mandate "engineering-side GREEN != feature done" is empirically true because tests routinely don't pin the behaviors they claim to verify.

## Decision

Every public symbol where "config-driven" applies (field source is env var, Helm value, `valueFrom.secretRef`, ConfigMap, or Pulumi output) MUST have an integration test asserting the source.

**Test name convention:** `Test{Symbol}_ReadsConfigSource_{ConfigKey}`

**Test shape (mandatory dual assertion):** Every pillar-(b) test MUST include BOTH:
1. **POSITIVE assertion** — injected config value flows through to observable output
2. **NEGATIVE anti-assertion** — literal value from the ORIGINAL bug evidence is absent

**Example for login.ts (T2.1 absorbs):**
```typescript
test('TestLogin_ReadsConfigSource_LOOPZERO_DASHBOARD_URL', () => {
  process.env.LOOPZERO_DASHBOARD_URL = 'https://injected.example';
  const result = login.resolveDashboardURL();
  expect(result).toBe('https://injected.example');       // positive
  expect(result).not.toContain('app.dev.loopzero.example');   // ANTI-ASSERTION (bug evidence)
});
```

**Why the anti-assertion (EC-13 absorbed):** Without it, a future regression that re-introduces a DIFFERENT hardcoded literal (e.g., `'app.staging.loopzero.example'`) passes the positive assertion silently. The anti-assertion makes the test a literal regression guard tied to the original bug shape — not just a behavioral test that any URL satisfies.

**Codified as `loop-code-review` gate (deferred enforcement):** When ratio `tests_with_pillar_b / new_public_config_symbols < 1.0`, severity=HIGH. Per § Drawbacks, enforcement has 30d grace period to retrofit existing journeys; CI warns but doesn't block during that window. After 30d, enforces as HIGH severity (blocks merge).

## Alternatives

### (a) Require unit-level mock + integration-level assertion (both) — REJECTED

Redundant when integration test covers the same contract. Doubles test count for the same signal. YAGNI per `rules/design-principles.md`.

### (b) Snapshot-test all config-driven output — REJECTED

Snapshot churn on every config addition. Snapshot tests assert "current value equals frozen value", which doesn't add regression protection beyond what the type system + integration test provides. Snapshot files become diff noise in PR review.

### (c) Property-based test (random URLs) instead of named anti-assertion — REJECTED

Property tests are excellent for ALGORITHMIC correctness (parser round-trip, encoding invariants). But the regression we're guarding against is SPECIFIC: a particular hardcoded literal returning. A targeted anti-assertion is more legible than a property test that might or might not generate the offending literal.

### (d) Static analysis lint detecting hardcoded URL literals in source — REJECTED

Catches the OBVIOUS form (literal URL inline) but misses the SUBTLE form (URL stored in a const, then used). Misses non-URL config (timeouts, retry counts, namespace names). The test contract catches BEHAVIOR; lint catches SHAPE. Behavior > shape because behavior is what ships.

## Rationale

Cited rules:
- **`rules/cycle-implement.md` § Wiring triad** — pillar (b) integration test is non-negotiable per cycle contract; this ADR refines what "exercising the boundary" means
- **`rules/tdd-workflow.md` § AAA pattern** — anti-assertion is a third "A" (Anti-assert): a negative invariant complements the positive AAA cycle
- **Mutation testing principle** — a test that doesn't fail when the original bug returns is a test that adds zero regression value

Memory: `feedback_post_feature_live_regression_workflow_2026_06_09` (structural root); `feedback_endpoint_contract_narrow_consumer_audit` (sibling pattern of silent contract narrowing).

## Consequences

### Positive
- 3 known bug-class proofs (login.ts URL / dashboardURL dead / secrets env) get regression guards retroactively
- Universal formula `tests_exist > 0 AND tests_assert_behavior == 0` becomes detectable + enforceable
- Closes `feedback_post_feature_live_regression_workflow_2026_06_09` structurally — features ship with PROOF of config-source pinning

### Negative
- Adds test maintenance: anti-assertions reference original bug evidence (literal value); literal stays in test file forever as regression sentinel
- 30d grace period during which loop-code-review warns but doesn't block — temporary CI noise

### Neutral
- Test name convention `Test{Symbol}_ReadsConfigSource_{ConfigKey}` is grep-friendly + auto-discoverable by future audit tools

## References

- Plan v1.2 § ADR D5 + T3.6 + edge case EC-13
- Test-auditor findings (3 bug-class proofs) (`code-review-output/findings/phase4-test-audit.md`)
- ADR D6 (complexity decomposition) — sibling; both raise quality bar via structural enforcement
