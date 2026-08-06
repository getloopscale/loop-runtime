---
type: decision
title: "ADR D6 — Hot-path complexity decomposition pattern"
---

# ADR D6 — Hot-path complexity decomposition pattern

**Status:** Accepted
**Date:** 2026-06-11
**Plan:** v1.2 § ADR D6 + Tasks T4.1-T4.5
**Tier:** T2 (code architecture; refactor pattern adoption) per `rules/pre-release-governance.md`

## Context

Complexity-scanner (gocyclo + lizard) measured **20 CRITICAL functions with cyclomatic complexity CC ≥ 26** (worst: `(Config).Validate` CC=46). Distribution:

- **8 findings in `api/internal/routes/`** HTTP handlers — hot path, latency-sensitive
- **5 findings in controllers/orchestrators** — state-machine + side-effect orchestration
- **4 findings in CLI dispatchers** — multi-form command dispatch (scale, preview, platform)
- **3 findings in bootstrap/config** — sequential setup chains

Severity is `consensus` per McCabe NIST 1976 threshold (CC ≥ 15 is empirically validated risk threshold). CC ≥ 26 means each function has > 26 independent paths — beyond what a human reviewer can hold in working memory + beyond what unit tests can practically cover all branches.

The pre-existing P0 findings (`main` CC=44, `registerProtectedRoutes` CC=41) are documented in CLAUDE.md as already-known. The 18 NEW CRITICAL findings are unaddressed.

## Decision

Decomposition strategy varies by location pattern — apply the right canonical pattern, not "extract function" universally:

| Location pattern | Canonical pattern | Why |
|---|---|---|
| **HTTP handlers** (Bulk, Upload, Connect, Create, Scale) | **Command pattern** — extract each branch as `Command struct { Execute(ctx) }`; handler dispatches via `commandRegistry[op].Execute()` | HTTP handlers ARE dispatch tables for sub-operations; explicit registry makes the dispatch testable + extensible (OCP — new ops add files, not edit switches) |
| **CLI multi-form dispatch** (scale.ts, preview.ts, platform.ts) | **Strategy pattern** — extract each form as a strategy class; CLI registers + dispatches via lookup | CLI forms are interchangeable algorithms behind a stable contract — textbook Strategy use case |
| **Controllers/orchestrators** (reconcileDelete, phaseACreateRelease, Reconcile) | **State machine pattern** — leverage existing `pkg/statemachine/`; extract phases as states with explicit transition rules | Already canonical in LoopZero — Build/Release/Tenant/Application state machines. New decompositions use the SAME registry, no new pattern invention. |
| **Validators** (Config.Validate) | **Validator chain** — replace mega switch with iteration over `[]Validator` slice | Each validator has ONE reason to change (SRP). Order encoded explicitly. Adding a validator = appending to slice (OCP). |
| **Mega-switches** (deriveAction, registerProtectedRoutes) | **Dispatch table** — replace switch with `map[Key]Action` lookup | When the switch has > 10 cases, the lookup table is more legible + O(1) instead of O(n) match. |

**Pattern adoption discipline (DRY across phase 4 tasks):** Each T4.x commit MUST reuse an existing pattern from `pattern-suggester` positive reference #61 (LanguageStrategy / state machines / db Repository / SSE adapter / placement Router / vault Decorator) rather than invent novel patterns mid-implementation.

## Alternatives

### (a) Decompose by LOC threshold (every function > 100 LOC gets split) — REJECTED

Long functions with low CC (e.g., a 200-LOC sequence of straightforward steps) are fine — they read top-to-bottom without branching. The risk is BRANCHING (CC), not LENGTH (LOC). Decomposing low-CC long functions adds indirection without reducing reasoning load.

### (b) Defer all decomposition to "future hardening sprint" — REJECTED

20 CRITICAL findings is CURRENT debt. Deferring 100% means adding to the existing 16 ungroomed CC ≥ 26 in CLAUDE.md (`main`, `registerProtectedRoutes`) — the debt compounds. Plan splits: top 5 in T4.1-T4.5, remaining 15 allowlisted with sunset 60d (T5.x batch + future plan).

### (c) Apply same pattern (e.g., "always extract sub-functions") universally — REJECTED

Different problem shapes require different patterns. Generic "extract function" without semantic grouping creates a code base of 5-line stubs with no architectural meaning. Strategy/Command/State-machine each carry MEANING — they tell future readers WHAT the abstraction represents, not just THAT it was extracted.

### (d) Introduce dependency injection framework (e.g., wire) to manage decomposition — REJECTED

LoopZero already has explicit constructor-injection per [`rules/architecture.md`](../technical/pkg/architecture.md). A DI framework would replace explicit wiring (testable, debuggable) with reflection-based wiring (opaque, runtime errors). Net negative for a codebase that ALREADY uses DIP cleanly.

## Rationale

Cited principles:
- **SRP** (each Command/Strategy/State has ONE reason to change)
- **OCP** (new variations add new files, not edit existing switch arms)
- **DIP** (handlers depend on Command interface, not concrete implementations)
- **DRY** (reuse existing patterns from pattern-suggester #61 positive reference)

Cited rule: `rules/design-principles.md` § SOLID + Design Patterns ("use established patterns when the problem matches; do NOT invent novel patterns mid-implementation").

McCabe NIST 1976 — cyclomatic complexity threshold (CC ≥ 15 is consensus risk; CC ≥ 25 is consensus CRITICAL).

Memory: `feedback_canonical_vs_codified_workaround` (avoids inventing novel patterns that codify the problem).

## Consequences

### Positive
- Top 5 CRITICAL CC ≥ 32 functions decomposed to CC ≤ 15 each (gocyclo gate enforces post-fix)
- Pattern adoption establishes precedent — future complexity hot-spots use the same five-pattern menu (Command/Strategy/State/Validator/Dispatch)
- Test pyramid improvement: each extracted Command/Strategy is unit-testable in isolation (was previously embedded in 30+ branch handler)
- Closes 5 of 20 CRITICAL findings in Phase 4; remaining 15 deferred with sunset 60d

### Negative
- Initial refactor effort substantial (5 tasks × 1-2 days each = ~10 days)
- More files in `api/internal/routes/` + `cli/src/commands/` (mitigated by package structure — sub-package per handler family)
- Diff in PR review is large (mitigated by per-pattern atomic commits, behavior-preserving)

### Neutral
- Net LoC slightly increases (Command structs add wrapping); CC decreases dramatically (the right metric)

## References

- Plan v1.2 § ADR D6 + Tasks T4.1-T4.5
- Complexity-scanner findings C1-C20 (`code-review-output/findings/phase3-complexity.md`)
- Pattern-suggester #61 positive reference (existing canonical patterns)
- ADRs D1-D5 (sibling Phase 0 ADRs)
