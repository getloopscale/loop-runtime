# Architecture Review

**Date:** 2026-03-29
**Target:** /home/paulo/loopzero
**Reviewer:** architecture-analyst

---

## Executive Summary

- **Overall architecture health: GOOD with specific structural debts**
- **Total findings: 10** (critical: 0, high: 1, medium: 4, low: 5)
- **Key concern:** Subdomain generation logic is duplicated in 3 places, with one copy containing a bug (missing TrimRight step in build_orchestrator.go) that can cause silent static frontend routing failures.

The architecture is well-designed for its problem domain. The four-plane separation (developer, control, build, workload) is cleanly reflected in the code structure. Layer separation (routes/services/db) is sound, the Dependency Inversion Principle is applied consistently via local interfaces, and state machines are properly enforced through the store layer. No circular dependencies between `api/`, `operators/`, and `pkg/` exist. The identified issues are structural debts accumulated during fast feature delivery, not fundamental design failures.

---

## Codebase Structure

```
loopzero/
├── api/          (Go) — 258 .go files, HTTP server + business services
├── cli/          (TypeScript) — 113 .ts files, Commander CLI
├── operators/    (Go) — kubebuilder controllers
├── pkg/          (Go) — shared zero-dep library (errors, naming, statemachine, id, logging, health)
├── infra/pulumi/ (TypeScript) — Pulumi IaC
├── loopzero-forge/   (Go) — Argo Workflows SDK
└── loopzero-packs/   (Go) — Framework detection + Dockerfile generation
```

**Language breakdown:** 258 Go files, 113 TypeScript files, 68 YAML manifests.

**Module boundary (go.work):** api, operators, pkg, infra/scripts, infra/tests — no cross-module imports except via pkg.

---

## Layer Analysis

### Routes/Handlers Layer

The routes layer is architecturally clean. Every handler file defines local interfaces for its dependencies (DIP applied via local interface definitions). No route handler imports `services/` or `db/` directly — they depend on their own locally-defined interfaces. Validation belongs in the handler (request parsing, environment name regex, path safety checks). Business logic does not leak into routes.

Specific observations:
- `routes/builds.go`: Clean. Delegates entirely to `BuildOrchestratorService` interface after input validation.
- `routes/rollback.go`: Clean. Delegates to `RollbackService` interface. The project lookup in the handler (before calling the service) is appropriate — it converts a name to an ID, which is a handler responsibility.
- No route handler contains SQL queries, ORM calls, or external API calls.

**Result: PASSES — no business logic in handlers.**

### Services Layer

The services layer correctly owns all business logic. Services access the DB via `db.BuildStore/ReleaseStore` interfaces (not raw SQL). Services emit SSE events, manage state transitions, and coordinate external systems (Argo Workflows, ArgoCD, Gitea, CF KV).

Services do NOT import `net/http` for HTTP concerns (confirmed by grep — no `http.ResponseWriter` or `http.Request` in services). The one HTTP usage is via injected `SyncClient` and `WorkflowSubmitter` interfaces.

**Result: PASSES — services do not contain HTTP concerns.**

### DB/Store Layer

The store layer is clean. `UpdateStatus` in both `PostgresBuildStore` and `PostgresReleaseStore` validates transitions through the state machine before executing SQL. Direct status field assignment (`build.Status = ...`) only occurs AFTER a validated state machine transition, not as a bypass.

State machine validation is centralized: `buildMachine` and `releaseMachine` singletons enforce all transitions. No service bypasses this — they all call `UpdateStatus(ctx, tx, buildID, string(event))` which routes through the state machine.

**Result: PASSES — state machines enforced at store boundary.**

### Adapters Layer

`adapters/adapters.go` (312 lines) serves as the anti-corruption layer between route-level types and service-level types. This is intentional DIP enforcement to prevent import cycles. The layer is mechanical (field-by-field copying) and does exactly one thing. The cost is verbosity; the benefit is clean layer isolation.

---

## Dependency Analysis

### Module-level dependency direction

```
cli/ → (HTTP only, no Go imports)
api/ → pkg/errors, pkg/id, pkg/naming, pkg/logging, pkg/statemachine
operators/ → pkg/naming, pkg/statemachine
pkg/ → (zero external Go module deps)
```

No circular dependencies. No `api/` importing `operators/` or vice versa. The modules are fully independent except for `pkg/`.

### Package-level dependency direction within api/

```
routes/ → (local interfaces only, no services/ or db/ imports)
services/ → db/, argo/, argocd/, gitops/, cfkv/, manifests/
db/ → pkg/id, pkg/statemachine, pkg/errors
adapters/ → routes/, services/, db/, lock/, sse/  ← correct: adapters bridge all
cmd/ → all internal packages (composition root)
```

The adapters package imports both routes and services, which is the only place this cross-layer import is permitted. This is the intended DIP bridge pattern.

**Result: No import direction violations found.**

---

## God Classes / Files

| File | Lines | Assessment |
|------|-------|-----------|
| `infra/pulumi/src/components/bootstrap.ts` | 2459 | GOD FILE — all 3 cluster bootstrapping in one file |
| `api/internal/services/build_orchestrator.go` | 736 | BORDERLINE — 2 responsibilities |
| `api/internal/manifests/renderer.go` | 641 | ACCEPTABLE — complex but single concern |
| `api/internal/services/build_reconcile_loop.go` | 510 | ACCEPTABLE — single concern |
| `operators/internal/controller/environment_controller.go` | 532 | ACCEPTABLE — controller reconcile |

**bootstrap.ts at 2459 lines is the clear outlier.** It is 10x the size of the next-largest Pulumi file and covers mgmt cluster, build cluster, and workload cluster bootstrapping in a single file. This is purely a maintainability concern — there is no functional problem.

**build_orchestrator.go at 736 lines** contains a secondary responsibility: writing CF KV routing entries for static apps (`writeStaticRoutingEntries`, lines 696-736). This logic properly belongs in `StaticDeliveryActivator.phaseBDeliver`, which already exists.

---

## Pattern Usage

### Dependency Inversion Principle

Applied correctly throughout `api/`. Every handler defines local interfaces for its service dependencies. Example from `builds.go`:

```go
type BuildOrchestratorService interface {
    SubmitBuild(ctx context.Context, req SubmitBuildInput) (*SubmitBuildResult, error)
    ...
}
```

The adapters package bridges the gap between these local interfaces and the concrete service implementations. This pattern adds translation overhead but achieves full layer isolation.

One DIP violation: `BuildReconcileLoop.orch` is typed as `*BuildOrchestrator` (concrete) rather than an interface. Since both are in the same `services` package, this was expedient but reduces testability.

### State Machine Pattern

Used correctly. Both `PostgresBuildStore.UpdateStatus` and `PostgresReleaseStore.UpdateStatus` validate transitions through `statemachine.NewBuildMachine()` and `statemachine.NewReleaseMachine()` before executing SQL. Invalid transitions return `ErrInvalidTransition` rather than panicking (via `safeTransition` wrapper).

### Strategy Pattern (DeliveryActivator)

Well-applied. The `DeliveryActivator` interface (delivery.go) enables `BuildOrchestrator` to dispatch to either `ContainerDeliveryActivator` or `StaticDeliveryActivator` without if/else branches. Both activators follow the identical 5-phase semantic contract.

### Repository Pattern

Applied correctly. Store interfaces (`BuildStore`, `ReleaseStore`, etc.) in `db/` abstract all SQL. Services depend on these interfaces. No service contains raw SQL.

### Circuit Breaker Pattern

`services/pg_circuit_breaker.go` and `services/workflow_circuit_breaker.go` implement circuit breakers for PostgreSQL and Argo Workflows respectively. These appear to be correctly isolated single-concern files.

---

## Domain Model Assessment

The domain models (`db.Build`, `db.Release`, etc.) are the persistence models — they are annotated with JSON tags for PostgreSQL scanning. There are no separate domain entities vs persistence entities; the same struct serves both purposes.

This is a pragmatic trade-off appropriate for this codebase scale. The `db.Build` and `db.Release` structs do not contain ORM annotations, business methods are in services (not on the model), and the statemachine validation lives in the store layer rather than on the entity.

No ORM annotations (Column, Entity, Table) appear in business logic. The domain is not anemic in the bad sense — business rules live in services, and the state machine is enforced.

---

## Module Cohesion

| Module | Cohesion | Assessment |
|--------|---------|-----------|
| `pkg/naming` | HIGH | All naming functions for K8s/Gitea resources |
| `pkg/statemachine` | HIGH | All state machine definitions |
| `api/internal/routes` | HIGH | All HTTP handlers |
| `api/internal/services` | MEDIUM-HIGH | Business logic, some routing concerns leaking (CF KV) |
| `api/internal/db` | HIGH | All store interfaces + PostgreSQL implementations |
| `api/internal/argo` | HIGH | Argo Workflows build pipeline |
| `api/internal/adapters` | HIGH | Pure translation layer |
| `operators/internal/controller` | MEDIUM | Platform setup + build/deploy controllers mixed |
| `infra/pulumi/src/components` | LOW | bootstrap.ts covers 3 clusters in 1 file |

---

## Findings (Detailed)

### ARCH-004: Subdomain generation bug in build_orchestrator.go (HIGH)

- **Severity:** high
- **Location:** `api/internal/services/build_orchestrator.go:710-712`
- **Description:** `writeStaticRoutingEntries` generates the subdomain key with inline `fmt.Sprintf` + truncation but is missing the `strings.TrimRight(subdomain, "-")` step present in `cfkv.Subdomain`. When a subdomain key truncated to 63 characters ends with a hyphen, the KV key written here differs from the key used by `StaticDeliveryActivator.phaseBDeliver` (which calls `cfkv.Subdomain` correctly). This produces a broken routing entry.
- **Evidence:** `cfkv/client.go:114-117` has TrimRight; `build_orchestrator.go:710-712` does not.
- **Recommendation:** Replace inline logic with `cfkv.Subdomain(build.TenantID, build.ProjectID, app.Name, build.Environment)`.

### ARCH-003: ArgoCD app name duplicated in 4+ places (MEDIUM)

- **Severity:** medium
- **Location:** `api/internal/services/container_delivery.go:103,156`, `release_convergence_loop.go:151`, `project_deleter.go:137,213`
- **Description:** `fmt.Sprintf("%s-%s", tenantID, environment)` appears at least 5 times. `pkg/naming.ArgoAppName` exists but uses a different 3-parameter signature (team, project, env) that produces a different result. The API services and pkg/naming are inconsistent on what an ArgoCD app name is.
- **Recommendation:** Align `pkg/naming.ArgoAppName` with the actual 2-component `{tenant}-{environment}` formula or add a dedicated function. Use it at all 5 call sites.

### ARCH-001: build_orchestrator.go mixes orchestration with CF KV routing (MEDIUM)

- **Severity:** medium
- **Location:** `api/internal/services/build_orchestrator.go:696-736`
- **Description:** `writeStaticRoutingEntries` performs CF KV writes inside `BuildOrchestrator.OnBuildCompleted`, creating a second KV write path alongside `StaticDeliveryActivator.phaseBDeliver`. The orchestrator should dispatch entirely to the `DeliveryActivator` for delivery-specific concerns.
- **Recommendation:** Remove `writeStaticRoutingEntries` from `BuildOrchestrator` and rely exclusively on `StaticDeliveryActivator.phaseBDeliver`.

### ARCH-005: API services bypass pkg/naming for Gitea repo names (MEDIUM)

- **Severity:** medium
- **Location:** `api/internal/gitops/gitea_pusher.go:62,159,380`, `api/internal/source/gitea_store.go:79-81`
- **Description:** `pkg/naming.GitOpsRepoName` and `pkg/naming.RepoName` are not used. Instead, inline `fmt.Sprintf` patterns are used. The operators use `pkg/naming`; the API does not. If the scheme changes in one place, the other produces a different repo name for the same tenant.
- **Recommendation:** Migrate all repo name generation to `pkg/naming` functions.

### ARCH-006: bootstrap.ts is a 2459-line monolith (MEDIUM)

- **Severity:** medium
- **Location:** `infra/pulumi/src/components/bootstrap.ts:1-2459`
- **Description:** All three cluster bootstrapping concerns in one file. This is a pure maintainability finding with no runtime impact.
- **Recommendation:** Split into bootstrap-mgmt.ts, bootstrap-build.ts, bootstrap-workload.ts.

### ARCH-002: Parallel DTO types in routes and services create mechanical translation overhead (LOW)

- **Severity:** low
- **Location:** `api/internal/adapters/adapters.go:108-210`
- **Description:** `AppInput/AppSpec`, `SubmitBuildInput/SubmitBuildRequest`, `SubmitBuildResult/SubmitBuildResponse`, `BuildCallbackInput/BuildCallbackRequest` are structurally identical pairs defined in two layers. The adapters file is 100 lines of field copying.
- **Recommendation:** Introduce a shared `api/internal/domain` subpackage for DTOs shared across the boundary.

### ARCH-007: manifests/renderer.go has third subdomain copy (LOW)

- **Severity:** low
- **Location:** `api/internal/manifests/renderer.go:603-610`
- **Description:** A third correct implementation of subdomain generation. Moving this to `pkg/naming.StaticSubdomain` would create a single source of truth across all three locations.
- **Recommendation:** Add `pkg/naming.StaticSubdomain` and use it in cfkv, manifests, and build_orchestrator.

### ARCH-008: PlatformBootstrap controller constructs internal HTTP client bypassing injection (LOW)

- **Severity:** low
- **Location:** `operators/internal/controller/platformbootstrap_controller.go:158-162`
- **Description:** `httpClient()` method creates a new `http.Client` instead of using the injected `r.HTTPClient` field. Partially undermines the injection point.
- **Recommendation:** Consolidate to always return `r.HTTPClient`, set default in Setup function if nil.

### ARCH-009: BuildReconcileLoop holds concrete *BuildOrchestrator instead of interface (LOW)

- **Severity:** low
- **Location:** `api/internal/services/build_reconcile_loop.go:52-56`
- **Description:** The `orch` field is typed as `*BuildOrchestrator` (concrete) rather than a minimal interface. Unit tests require full orchestrator instantiation.
- **Recommendation:** Define a `ReconcileOrchestrator` interface with the 3 methods actually called.

### ARCH-010: CLI index.ts mixes command registration with business logic (LOW)

- **Severity:** low
- **Location:** `cli/src/index.ts:1-794`
- **Description:** 794 lines combining: command registration, cleanup registry, withTimeout utility, inline token validation in action handlers.
- **Recommendation:** Extract cleanup.ts, move withTimeout to utils.ts, ensure each action handler is 5-10 lines max (option parse + execute call).

---

## Recommendations (Prioritized)

1. **Fix the subdomain TrimRight bug** (ARCH-004, high) — Replace inline subdomain generation in `build_orchestrator.go:710-712` with `cfkv.Subdomain(...)`. This is a one-line fix for a real correctness bug affecting static frontend routing.

2. **Align ArgoCD app naming** (ARCH-003, medium) — Reconcile `pkg/naming.ArgoAppName` with actual usage. The current inconsistency between the library and the 5 call sites in services is a latent maintenance bug.

3. **Remove duplicate KV write in BuildOrchestrator** (ARCH-001, medium) — `writeStaticRoutingEntries` in `OnBuildCompleted` creates a parallel KV write path that undermines the `DeliveryActivator` abstraction.

4. **Migrate gitops/source naming to pkg/naming** (ARCH-005, medium) — Centralize Gitea repo name generation to eliminate divergence risk between API and operators.

5. **Split bootstrap.ts** (ARCH-006, medium) — Break the 2459-line monolith into per-cluster files. Low risk, high future maintainability gain.

6. **Extract shared DTOs** (ARCH-002, low) — Create `api/internal/domain` package to eliminate the parallel type hierarchy between routes and services.

7. **Add pkg/naming.StaticSubdomain** (ARCH-007, low) — Single source of truth for subdomain generation, used by cfkv, manifests, and build_orchestrator.

8. **Fix PlatformBootstrap HTTP client injection** (ARCH-008, low) — One-line fix to make the injected HTTPClient fully effective.

9. **Define ReconcileOrchestrator interface** (ARCH-009, low) — Improves unit testability of BuildReconcileLoop.

10. **Refactor CLI index.ts** (ARCH-010, low) — Improve separation between framework wiring and command logic.
