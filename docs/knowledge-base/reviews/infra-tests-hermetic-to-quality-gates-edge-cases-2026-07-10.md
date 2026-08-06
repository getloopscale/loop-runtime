# Discover Edge Case Review — infra-tests-hermetic-to-quality-gates

**Date:** 2026-07-10
**Discovery plan analyzed:** `knowledge-base/discoveries/plans/infra-tests-hermetic-to-quality-gates-plan.md`
**Research questions analyzed:** 8
**Edge cases found:** 7 (MUST FIX: 3, SHOULD TEST: 2, DOCUMENT: 2)

## MUST FIX

### EC-1: Q3.1 registry-name mismatch between Go package name and registered string

- **Affected question:** Q3.1
- **Family:** Method
- **Scenario:** During `/discover-execute`, the grep for "12 gate names" implicitly assumes the Go package name (`allowlistsunsets`) equals the registered string in `quality_registry.go`. Real precedent shows kebab-case strings (`"allowlist-sunsets"`) — grep with the wrong form returns 0 hits and blueprint fabricates a false "not-registered" finding.
- **Impact:** Blueprint concludes 12 gates unregistered when in fact they are all registered — false positive kills M-Migrate2 recipe.
- **Suggested fix:** Amend Q3.1 method to `grep -E '"[a-z-]+".*run[A-Z]' cmd/loopzero-ops/quality_registry.go` and cross-reference by function name (`run{PkgName}`) not by package literal.

### EC-2: Q4.2 sampled `Outcome` shapes may diverge — no fallback protocol

- **Affected question:** Q4.2
- **Family:** Interpretation
- **Scenario:** Plan samples 4 gates for the "canonical `Outcome` struct shape". If 2 gates use `type Outcome struct { Failures []string }` and 2 use `type Outcome struct { Errors []string; Warnings []string }`, blueprint has no rule to pick THE canonical shape.
- **Impact:** M-Migrate2 recipe becomes ambiguous — Waves 1+2+3 authors pick differently, drift resumes.
- **Suggested fix:** Amend Q4.2 method to "if the 4-sample check yields divergent shapes, extend sample to ALL 12 and select the modal shape; if bimodal, escalate to human via halt-loop BLOCKED report."

### EC-3: Q2.2 does not verify CI-runner binary availability

- **Affected question:** Q2.2
- **Family:** Reference path
- **Scenario:** Wave 2's 6 helm-template gates will invoke `helm template` via `os/exec`. If `taskfiles/quality.yml` invocation runs on a CI runner without `helm` binary on PATH, Wave 2 gates PANIC/FAIL, blocking `task quality:gates` GREEN which is a DoD criterion.
- **Impact:** M16 Wave 2 completion assertion is unmeasurable in CI — DoD gate "`task quality:gates` GREEN" cannot be evidence-satisfied.
- **Suggested fix:** Amend Q2.2 to also grep `taskfiles/quality.yml` for pre-flight checks (e.g., `command -v helmfile`) that precedent gates use, so Wave 2 gates can adopt the same pre-flight pattern.

## SHOULD TEST

### EC-4: Q3.1 grep completeness — halt-loop must verify 12/12 hits

- **Affected question:** Q3.1
- **Suggested halt-loop checkpoint:** "Q3.1 answer accepted only when grep-hit count equals 12 (matches the migrated gate count); < 12 hits → re-run with alternative delimiter (double-quote → backtick → single-quote) before concluding."

### EC-5: Per-question time budget missing (only 4h aggregate declared)

- **Affected question:** All (Q1.1–Q4.2)
- **Suggested halt-loop checkpoint:** "If any single question consumes > 45min wall-clock, halt-loop pauses and surfaces a BLOCKED-like advisory to the caller; the discovery time budget is 4h total and 8 questions × 30min average is the design point."

## DOCUMENT

### EC-6: Q4.1 "3 canonical test shapes" hypothesis may under-cover Wave 2

- **Accepted risk:** Hypothesis says Wave 1 has 3 subtypes (syntax / helm-template / file-layout). Wave 2's 6 helm-template gates might use `jq` heavily OR `yq` — subtype could be 4 or 5. Blueprint's "Migration recipe" section has room to enrich the categorization; not a discovery blocker. ADR-worthy note: "if `/discover-execute` finds > 3 subtypes, extend the categorization table without renumbering the plan."

### EC-7: Question dependency ordering not explicit (Q3.1 → Q3.2)

- **Accepted risk:** Q3.2 (taskfile wiring) is informed by Q3.1 (registry row format) since taskfile shell command references the registered string. `/discover-execute` sequential-by-convention behavior handles this naturally. No plan edit required; if `/discover-execute` runs Q3.2 before Q3.1, blueprint's "registered string" column will be missing — halt-loop should re-run in correct order. ADR-worthy: "execute questions in declared order Q1.1 → Q4.2."

## Summary

| Question | Edges found | MUST FIX | SHOULD TEST | DOCUMENT |
|----------|-------------|----------|-------------|----------|
| Q1.1 | 0 | 0 | 0 | 0 |
| Q1.2 | 0 | 0 | 0 | 0 |
| Q2.1 | 0 | 0 | 0 | 0 |
| Q2.2 | 1 (EC-3) | 1 | 0 | 0 |
| Q3.1 | 2 (EC-1, EC-4) | 1 | 1 | 0 |
| Q3.2 | 0 | 0 | 0 | 0 |
| Q4.1 | 1 (EC-6) | 0 | 0 | 1 |
| Q4.2 | 1 (EC-2) | 1 | 0 | 0 |
| ALL | 2 (EC-5, EC-7) | 0 | 1 | 1 |

**Verdict:** DISCOVERY PLAN NEEDS ADJUSTMENT (absorb 3 MUST FIX into v1.1)
