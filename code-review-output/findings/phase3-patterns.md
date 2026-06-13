# Phase 3 — Design Pattern Audit (9 delivered dashboard journeys)

**Run date:** 2026-06-11
**Auditor role:** pattern-suggester (phase 3, code_review)
**DB:** `code-review-output/code-review.db` — `category=design_pattern`, IDs 56-63 (one POSITIVE reference + seven actionable)
**Surface audited:** `api/internal/routes/`, `api/internal/services/`, `api/internal/github/`, `api/cmd/setup_secret_fetch.go`, `cloud/internal/routes/`, `operators/internal/`, `pkg/vault/`, `cloud/dashboard/src/pages/`, `cloud/dashboard/src/hooks/`

All findings carry `severity_source=heuristic` and `blocking=0` per the DB invariant for design_pattern. Severity is `medium` for actionable maintainability gaps, `low` for advisory/structural cleanup, `low` for the POSITIVE reference card.

## Top 7 actionable findings + 1 POSITIVE reference

| ID | Sev | Pattern | Surface | Title |
|---|---|---|---|---|
| 56 | medium | Repository (missing) | `cloud/internal/routes/` 4 handlers | SQL embedded in api_keys/memory_*/billing handlers; sibling `api/internal/db/` already has canonical Repository |
| 57 | medium | Functional Options / Builder (missing) | `api/internal/routes/secrets.go` | `SecretHandler` has 6 void `Set*` mutators after `New()` — telescoping; siblings (`BuildHandler`, `EnvironmentHandler`) use fluent `With*` |
| 58 | medium | Value Object (missing) | 34 files across api/internal/ | `(tenantID, project, envName)` triple as 3 positional strings; pkg/naming.Namespace centralizes the FORMULA but not the IDENTITY |
| 59 | medium | Custom Hook / Strategy (missing) | `cloud/dashboard/src/pages/project-connect.tsx` | 615 LOC / 22 hook calls / 1 component; sibling `use-deployment-stream.ts` is the local canonical pattern |
| 60 | low | Long method | `cloud/dashboard/src/pages/api-keys.tsx` | 702 LOC, 7 nested components, page body 209 LOC |
| 62 | low | Factory (partial) | `cloud/internal/routes/{api_keys,memory_*,billing}.go` | Constructors accept raw `*sql.DB`; pairs with #56 |
| 63 | low | Null Object (misplaced) | `api/internal/routes/environments.go` | `StubEnvironmentManager` test stub colocated with production routes |
| 61 | low (POS) | — | repo-wide | POSITIVE reference card — see § Pattern adoption matrix below |

Max 8 emitted (within the 5-suggestions-per-turn FAANG etiquette when you net out the POSITIVE reference + the misplaced-stub which is a janitorial item).

## Pattern adoption matrix — what's canonical vs what's missing

| Pattern | Canonical site | Status | Notes |
|---|---|---|---|
| Strategy | `api/internal/argo/languages.go` — `LanguageStrategy` interface + 9 concrete (Node/Go/Python/Ruby/Java/PHP/Rust/Custom/UserDockerfile) | ADOPTED WELL | Build mode dispatch is textbook. Plan added `UserDockerfileStrategy` cleanly. |
| State Machine / Reconciler | `pkg/statemachine/` (11 machines) + every operator uses `*SM.Transition(event)` (`operators/internal/preview/preview_controller.go:350,402,413,466,576`) | ADOPTED WELL | Invalid transitions panic by design — programming-error contract honored. |
| Repository | `api/internal/db/` — 20+ `<Name>Store interface` + `Postgres<Name>Store struct` | ADOPTED WELL in api/ | NOT adopted in `cloud/internal/` — finding #56. |
| Adapter | `cloud/dashboard/src/hooks/use-sse-stream.ts` — single `new EventSource()` site; siblings (log, deployment, deployments-list) all delegate | ADOPTED WELL | Plan J-17 T2.4 explicitly rewrote `use-log-stream` as a thin wrapper. |
| Adapter (Go) | `pkg/vault/` — `Transit` / `K8sEngineClient` / `DatabaseClient` interfaces + `cached_client.go` Decorator | ADOPTED WELL | Decorator layered over Client; clean. |
| Strategy + Factory (placement) | `api/internal/placement/` — `Router interface` + 3 impls (Passthrough/Static/Dynamic) + `RoundRobinEngine` | ADOPTED WELL | Multi-cell placement-once-route-many honored. |
| Factory (HTTP handler) | api/ side uses interface deps; cloud/ side accepts raw `*sql.DB` | PARTIAL | Finding #62. Pairs with #56. |
| Functional Options / fluent Builder | `api/internal/routes/builds.go` + `environments.go` use `With*` returning `*Handler` | INCONSISTENT | `secrets.go` uses void `Set*` — finding #57. |
| Value Object | `pkg/naming` has the FORMULA (`Namespace`) but no `EnvRef` identity type | MISSING | Finding #58. Primitive obsession contagious across 34 files. |
| Custom Hook (React) | `use-deployment-stream`, `use-deployments-list-stream`, `use-onboarding-state` | INCONSISTENT | `project-connect.tsx` predates the convention — finding #59. |
| Null Object | `StubEnvironmentManager` exists | MISPLACED | Finding #63 — production file. |
| Template Method | `ContainerDeliveryActivator.Activate` → `phaseA/B/C/E` private steps | ADOPTED WELL | SRP-clean orchestration. |

## Why each finding cleared the 2-signals minimum

| ID | Signal 1 | Signal 2 | Signal 3 |
|---|---|---|---|
| 56 (Repository) | 4 cloud handlers embed raw SQL (19 / 6 / 6 / 4 / 3 statements) | Sibling `api/internal/db/` already proves the canonical shape | Adding a 2nd backend (read replica, cache) requires touching every handler |
| 57 (Functional Options) | 6 void `Set*` calls in a row after `New()` | Siblings in same package (`BuildHandler`, `EnvironmentHandler`) use the fluent `With*` idiom | Two-phase construction creates an observable intermediate state |
| 58 (Value Object) | Triple `(tenantID, project, envName) string` appears in 34 files | RFC-1123 validation duplicated across boundaries (per `feedback_no_hardcode_gate_2026_06_09`) | Argument-order swap is silent — compiler does not catch |
| 59 (Custom Hook) | 615 LOC / 22 hook calls / 1 component | Sibling hooks (`use-deployment-stream`) prove the local canonical pattern | Plan J-04 explicitly proved cookie-stateful FSMs need Playwright LIVE because hooks were inlined |
| 60 (Long Method) | `ApiKeysPage` 209 LOC body | 7 components in one 702-LOC file | Page body mixes 3 mutation handlers + 5 dialog open-states |
| 62 (Factory partial) | 4 handlers accept `*sql.DB` directly | Tests forced to use sqlmock or live DB | Pairs with #56 — same root cause, different fix-surface |
| 63 (Null Object misplaced) | Production file contains a test stub | Risk of `/code-quality` dead-code FP | Janitorial; ship in next sweep |

## What I deliberately did NOT flag (anti-hallucination discipline)

- `resolveLanguageStrategy` switch (api/internal/services/build_plan.go:184) — IS a 9-arm switch on a string discriminator BUT it is the *registry* into the existing Strategy interface, not a missed Strategy. Single function, well-tested, no shotgun-surgery risk.
- `buildTenantCR` (api/internal/k8s/tenant_cr_manager.go:358) — single direct map literal. The comment-driven empty-`environments` decision is already a documented design (per `feedback_no_hardcode_gate_2026_06_09`). Not a Builder candidate — no chained setters, no 2-site reuse.
- `ContainerDeliveryActivator` (564 LOC) — large but cleanly decomposed into Template Method phases. Not god-class.
- `ReleaseConvergenceLoop` (614 LOC) — 1 public + 7 private methods on one cohesive reconcile lifecycle. Single Responsibility intact.
- `EnvironmentHandler` `With*` chain — already uses the fluent idiom. Healthy.
- Multi-cell `Router` interface + 3 implementations — POSITIVE example, used as evidence in #56's remediation.
- React `useSSEStream` adapter — single `new EventSource()` site in `src/`. Adapter pattern proven.

## Operating notes

- Every finding cites ≥ 2 structural signals (line + sibling pattern site or call-count + reused-formula).
- Every Strategy/Factory/Repository suggestion points at an EXISTING sibling implementation as remediation evidence — no greenfield pattern fetishism.
- The POSITIVE reference (finding #61) is intentional: when the next reviewer asks "where's the canonical Strategy here?", the answer is one query away.

## Cross-references

- `.claude/rules/design-principles.md` — SOLID + KISS + YAGNI + DRY underwrite the severity floor (medium for actionable, low for advisory).
- `feedback_canonical_vs_codified_workaround` — guided rejection of fake-pattern suggestions (e.g., do NOT propose a fake Strategy interface where the canonical answer is a registry function).
- `feedback_no_hardcode_gate_2026_06_09` — informed the Value Object remediation (validation belongs at construction).
- `feedback_swallowed_silent_errors_pattern` — informed scope for #56 (Repository surface widening risks the same swallowed-error class if not migrated by atom).
