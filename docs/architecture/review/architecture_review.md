# Architecture Review

**Date:** 2026-05-10
**Target:** /home/paulo/Projetos/loopzero/loopzero
**Reviewer:** architecture-analyst
**Branch:** develop

---

## Executive Summary

- **Overall architecture health:** NEEDS ATTENTION
- **Total findings:** 8 (critical: 0, high: 3, medium: 3, low: 2)
- **Key concern:** The `api/internal/` layering is mostly sound but has two structural cracks — the routes layer reaching into the services package for error sentinels, and the billing handler issuing raw SQL — both of which signal the layer contract is not yet fully enforced.

---

## Codebase Structure

Four planes are cleanly separated at the binary level:
- `cli/` — TypeScript, Commander
- `api/` — Go, chi router
- `operators/` — Go, kubebuilder (4 binaries: tenant, app, preview, platform)
- `infra/` — Pulumi (TS) + Helmfile + ArgoCD

Shared library: `pkg/` (statemachine, errors, jwt, naming, conditions, id, logging, enforcement, circuitbreaker).

---

## Layer Analysis

### api/internal/ structure

```
routes/       ← HTTP handlers (should only parse/validate/delegate)
services/     ← Business logic orchestration
db/           ← Persistence (stores + raw pgx)
argo/         ← Workflow construction (Argo Workflows)
argocd/       ← ArgoCD sync client
manifests/    ← K8s manifest rendering
domain/       ← Shared types and interfaces
adapters/     ← Anti-corruption layer (routes ↔ services type translation)
placement/    ← Cell routing and assignment
processing/   ← PG-backed distributed dedup claims
```

**Dependency direction (verified):**
- `routes/` → `domain/`, `middleware/`, `validation/`, `sse/`, `lock/` — correct.
- `services/` → `db/`, `domain/`, `manifests/`, `metrics/`, `placement/`, `cfkv/` — correct.
- `argo/` → `services/` (imports types and interfaces from services) — correct direction (argo is infrastructure, services defines contracts).
- `adapters/` → `routes/` + `services/` — correct, this is the wiring layer.
- No `services/` → `routes/` import — confirmed. No circular dependency on the happy path.

**The build orchestrator split is by responsibility, not size:**
- `build_orchestrator.go` — types + interface definitions (347 LOC)
- `build_orchestrator_submit.go` — submit flow with lock semantics (320 LOC)
- `build_orchestrator_complete.go` — completion + release promotion (226 LOC)
- `build_orchestrator_cancel.go` — cancellation (86 LOC)
- `build_orchestrator_retry.go` — retry policy (54 LOC)

This decomposition is architecturally correct. Each file maps to a single operation on the state machine.

---

## Findings (Detailed)

### ARCH-001: Routes layer imports services package for error sentinels (HIGH)

**Location:** `api/internal/routes/admin_pin_cell.go:33`

**Description:** `admin_pin_cell.go` imports `api/internal/services` to reference `services.ErrTenantNotFound`, `services.ErrCellNotFound`, `services.ErrCellNotPinnable`, and `services.ErrCellAtCapacity`. This is a direct downward coupling from the routes layer to the services layer. The file is aware of this (`// Local to routes/ to avoid the routes↔services↔db import cycle` comment in billing.go confirms the team knows this is a problem).

The danger: once `routes/` imports `services/`, the path is open for any handler to reach deeper into service internals over time. The correct pattern is already established elsewhere (the `adapters/` package handles cross-layer type translation). Error sentinels should live in `pkg/errors` or `domain/` — not in `services/`.

**Evidence:** `admin_pin_cell.go` line 33: `"github.com/loopzero/loopzero/api/internal/services"`. Lines 127-136 use `services.ErrTenantNotFound` etc. directly in the HTTP error mapping switch.

**Recommendation:** Move `ErrTenantNotFound`, `ErrCellNotFound`, `ErrCellNotPinnable`, `ErrCellAtTenantCapacity` from `services/cell_pinner.go` into `pkg/errors` or `api/internal/domain/`. The routes layer can then import only `domain/` for errors, preserving the layer contract.

---

### ARCH-002: Billing handler issues raw SQL queries directly (HIGH)

**Location:** `api/internal/routes/billing.go:154-176`

**Description:** `BillingHandler.fetchPlan()` and `fetchStripeCustomerID()` execute raw SQL (`SELECT plan FROM subscriptions WHERE tenant_id = $1`) directly inside a route handler. This is a clear separation-of-concerns violation: the route layer owns HTTP concerns, not data access.

The file's own comment acknowledges this: *"Local to routes/ to avoid the routes↔services↔db import cycle blocked by find_arch_001 / T1.3."* The fix was deferred as a known technical debt item. That's acceptable for a pre-release, but it is a registered architectural violation.

**Evidence:** `billing.go` imports `github.com/jackc/pgx/v5` directly. Lines 154-164 contain inline SQL in a handler method. A `BillingPool` interface is defined locally in `routes/` as a workaround.

**Recommendation:** Create `api/internal/db/billing_store.go` with a `BillingStore` interface and `PostgresBillingStore` implementation. Wire through `adapters/` or inject via `BillingHandler`. Remove raw SQL from the handler. This is exactly the pattern used everywhere else in the codebase.

---

### ARCH-003: `services/cell_pinner.go` directly imports K8s client-go (HIGH)

**Location:** `api/internal/services/cell_pinner.go:27-30`

**Description:** The services layer imports `k8s.io/apimachinery`, `k8s.io/apimachinery/pkg/runtime/schema`, `k8s.io/apimachinery/pkg/types`, and `k8s.io/client-go/dynamic` to directly patch a Tenant CR on the management cluster. This violates the Dependency Inversion Principle: the services layer (business logic) should not depend on infrastructure (K8s client). The correct pattern — used for ArgoCD sync, Argo Workflow submission, and GitOps — is to define an interface in `services/` and inject the concrete implementation at wiring time via `adapters/` or `server/router.go`.

The comment in the file explicitly justifies the workaround: *"Hardcoded here (instead of importing operators/api/v1) so this service has no operator-package dependency."* Avoiding one coupling (operators) by introducing another (raw K8s dynamic client) is not an improvement.

**Evidence:** `cell_pinner.go` lines 27-30 import K8s client libraries. The `k8sDynamic dynamic.Interface` field on `CellPinner` struct is the concrete infrastructure type.

**Recommendation:** Define a `TenantCRPatcher` interface in `services/cell_pinner.go` (or `domain/`):
```go
type TenantCRPatcher interface {
    PatchCellID(ctx context.Context, tenantID, cellID string) error
}
```
Implement it in `api/internal/k8s/` or `api/internal/adapters/`. Inject via constructor. The services layer becomes independent of K8s client-go again.

---

### ARCH-004: `manifests/renderer_types.go` manually redeclares K8s types (MEDIUM)

**Location:** `api/internal/manifests/renderer_types.go` (527 LOC, 47 custom types)

**Description:** The manifests package defines 47 hand-rolled Go structs (`k8sDeployment`, `k8sRollout`, `k8sIngressRoute`, etc.) that duplicate types from `k8s.io/api` and the Argo Rollouts API. The motivation is documented: the package was converted from `fmt.Sprintf` string concatenation to typed structs for YAML determinism. That was the right move. However, importing the real SDK types would provide the same determinism with less maintenance burden and automatic compatibility with upstream changes.

The risk is asymmetric: if Argo Rollouts changes a field (e.g., adds a required `progressDeadlineSeconds` behavior), the hand-rolled types will silently produce invalid YAML until someone notices in production.

**Evidence:** `renderer_types.go` line 6 comment: *"K8s resource struct types"*. The file imports zero K8s SDK packages. `renderer.go` imports only `sigs.k8s.io/yaml` for marshaling.

**Recommendation:** Replace `k8sDeployment`, `k8sRollout` etc. with `k8s.io/api/apps/v1.Deployment` and `github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1.Rollout`. The serialization and determinism guarantees come from the SDK's own marshaling. Custom-only types (e.g., `k8sTraefikIngressRoute`) can remain as they have no upstream SDK equivalent.

---

### ARCH-005: `api/internal/domain/types.go` is a mixed-concern aggregate (MEDIUM)

**Location:** `api/internal/domain/types.go` (382 LOC)

**Description:** `domain/types.go` aggregates types from five distinct bounded contexts: custom domains, team management, audit logging, secrets management, and project management. Each bounded context has its own types AND its own store interface AND its own K8s sync interface all in one file. This makes the file a "types dumping ground" rather than a coherent domain package.

This is a cohesion issue, not a critical bug. The file does serve its stated purpose (prevent import cycles between routes and db), but it grows unboundedly as new bounded contexts are added.

**Evidence:** `domain/types.go` lines 30-382 contain: `CustomDomain`, `TeamMemberInfo`, `AuditFilter`, `SecretMetadata`, `CreateProjectInput` — five distinct domains with no shared behavior.

**Recommendation:** Split into per-context files within `domain/`: `domain/custom_domain.go`, `domain/team.go`, `domain/audit.go`, `domain/secret.go`, `domain/project.go`. This is a non-breaking refactor. The package import path stays the same; only internal organization changes. Each file becomes independently navigable.

---

### ARCH-006: `pkg/errors` and `pkg/jwt` are single-consumer packages (LOW)

**Location:** `pkg/errors/`, `pkg/jwt/`, `pkg/id/`, `pkg/enforcement/`

**Description:** `pkg/errors` is imported 43 times by `api/` and zero times by `operators/`. `pkg/jwt`, `pkg/id`, and `pkg/enforcement` are exclusively consumed by `api/`. `pkg/circuitbreaker` is exclusively consumed by `operators/`. These packages live in `pkg/` (signaling "shared across all consumers") but are in practice single-consumer libraries.

This is a LOW severity finding because Go's module system makes this costless — there is no runtime overhead and no coupling is introduced. The concern is purely cognitive: `pkg/` signals "universal shared library" but half its packages are not universal.

**Recommendation:** No urgent action required. When the next architectural boundary is drawn (e.g., a control plane binary is extracted), consider whether `pkg/errors` and `pkg/jwt` belong in `api/internal/foundation/` instead. `pkg/statemachine`, `pkg/naming`, and `pkg/logging` — genuinely shared — should remain in `pkg/`.

---

### ARCH-007: `argo/languages.go` concentrates all language strategies in one 537-LOC file (LOW)

**Location:** `api/internal/argo/languages.go` (537 LOC)

**Description:** All language build strategies (Node, Go, Python, Ruby, PHP, Rust, Java, static/React, UserDockerfile) are implemented in a single file. The `LanguageStrategy` interface is clean, but the file grows by one large struct block per new supported language. It is not a god class — each strategy struct is small and independent — but the file organization is by containment rather than by responsibility.

This is purely a navigability issue. There are no SRP violations and no coupling problems.

**Recommendation:** Split into `languages_node.go`, `languages_go.go`, `languages_python.go`, etc., with `languages.go` retaining only `LanguageStrategy` interface and `ResolveStrategy`/`ResolveStrategyForApp` dispatch functions. This makes adding a new language a one-file change.

---

### ARCH-008: `isUnknownFramework` framework allowlist lives in routes layer (MEDIUM)

**Location:** `api/internal/routes/builds.go:427-444`

**Description:** `isUnknownFramework()` encodes a list of supported frameworks as a switch statement inside the routes package. This is business logic — a framework allowlist is a product rule, not an HTTP concern. If the CLI and the API need to agree on valid frameworks, there is now only one authoritative source, and it is in the wrong layer.

The same logic likely exists (or should exist) in `argo/languages.go` via `ResolveStrategy()`. If both lists diverge, the API will reject a framework that the build pipeline would successfully handle, or vice versa.

**Evidence:** `builds.go` line 427 defines a local `isUnknownFramework` function with a 14-case switch. `argo/languages.go` defines `ResolveStrategy` with equivalent framework matching.

**Recommendation:** Remove `isUnknownFramework` from `routes/`. Call `argo.ResolveStrategy(framework)` (or a factored-out `validation.IsSupportedFramework()` in `api/internal/validation/`) from the route handler. Single source of truth for the framework allowlist.

---

## Dependency Analysis

**Clean directions confirmed:**
- `operators/` has zero imports from `api/internal/` — correct.
- `cli/` has zero imports from `api/internal/` or `operators/` — correct.
- `services/` has zero imports from `routes/` — no cycle.
- `db/` has zero imports from `services/` — no cycle.

**One quasi-cycle (not a real cycle but a smell):**
- `routes/admin_pin_cell.go` imports `services/` for error sentinels (ARCH-001).
- `adapters/build_adapter.go` imports both `routes/` and `services/` — this is intentional (the adapters package exists precisely to bridge these two layers from the wiring side).

**No actual circular import detected.** The Go compiler would reject compilation if a cycle existed; the codebase compiles, so all import graphs are DAGs.

---

## God Classes / Files

| File | LOC | Verdict |
|------|-----|---------|
| `api/internal/metrics/metrics.go` | 697 | Not a god class. Pure metric registry — all global `var` declarations. Large but cohesive. |
| `api/internal/db/release_store.go` | 615 | Not a god class. One struct, one interface, 15 query methods for one entity. High LOC from query boilerplate. |
| `operators/internal/preview/preview_controller.go` | 636 | Borderline. Contains reconciler + 6 reconcile phase methods + helpers. Cohesive around the PreviewEnvironment lifecycle but could be split into `preview_reconcile_phases.go`. |
| `api/internal/services/domain_provisioner.go` | 541 | Borderline. Three distinct provisioner phases (pending_ns, ns_active, verified) + removal. A single `ProcessOnce` orchestrator calling three phase methods. Could extract `domain_provisioner_phases.go`. |
| `api/internal/services/release_convergence_loop.go` | 530 | Acceptable. One loop, multiple phases (poll PENDING + monitor ACTIVE). Similar to build reconcile loop — the pattern is consistent. |
| `api/internal/config/config.go` | 529 | Acceptable. Config struct with 60+ fields + `Load()` + `Validate()`. Large by nature — configuration aggregation is inherently wide. No SRP violation. |
| `api/internal/manifests/renderer_types.go` | 527 | See ARCH-004. This is a type registry, not a god class, but it manually redeclares SDK types. |
| `api/internal/argo/languages.go` | 537 | See ARCH-007. One strategy per language, all in one file. No god class, just poor file organization. |

**No true god classes found.** The largest files are either pure type registries (metrics.go, renderer_types.go), query repositories with boilerplate (release_store.go), or orchestrators with documented multi-phase structure (domain_provisioner.go, release_convergence_loop.go).

---

## Pattern Usage Assessment

| Pattern | Usage | Verdict |
|---------|-------|---------|
| Strategy (LanguageStrategy) | Build pipeline per-language dispatch | Correct. Interface well-defined, dispatch function clean. |
| Repository (db/\*\_store.go) | Persistence abstraction | Correct. All stores implement interfaces; services depend on interfaces only. |
| Adapter (adapters/\*\_adapter.go) | routes ↔ services type translation | Correct. Explicit anti-corruption layer prevents direct type coupling. |
| State machine (pkg/statemachine) | Build, Release, Preview lifecycle | Correct. Transitions are explicit; invalid transitions panic as documented. |
| Observer (SSE stream_handler) | Deploy/rollout event streaming | Correct. Publisher/subscriber via channels; no direct handler coupling. |
| Circuit breaker (pkg/circuitbreaker) | Operator resilience | Correct. Used only in operators where transient K8s API failures are expected. |
| Singleton (metrics.go global vars) | Prometheus metric registry | Pragmatic. `promauto` requires package-level var registration. Not a design smell here. |

No cargo cult patterns identified. Patterns are applied where complexity justifies them.

---

## Domain Model Assessment

**Positive:** Domain models (`domain/types.go`, `db/*.go`) are separate from persistence models. The `db.Release`, `db.Build` structs carry ORM-like fields (timestamps, status strings) but are only used in `db/` and handed to `services/` as plain data. Services receive domain inputs (`services.AppSpec`, `services.SubmitBuildRequest`) and delegate persistence to injected store interfaces.

**Concern:** The `domain/types.go` file bundles both domain types (entities, value objects) and store interfaces (repository contracts). The interfaces belong here by necessity (to break import cycles), but it blurs the distinction between "what the domain IS" and "how the domain is STORED." The split recommended in ARCH-005 would clarify this.

**No anemic domain model.** `domain.CustomDomain` has `IsCFManaged()` behavior. State machines in `pkg/statemachine` carry all transition logic. This is appropriate for a Go codebase — the alternative (heavy domain objects with embedded state) is less idiomatic.

---

## Module Cohesion

| Package | Cohesion | Note |
|---------|----------|------|
| `pkg/statemachine` | HIGH | All state machine logic for all entities |
| `pkg/naming` | HIGH | All naming conventions for K8s resource names |
| `api/internal/argo` | HIGH | All Argo Workflows construction and submission |
| `api/internal/argocd` | HIGH | All ArgoCD sync client logic |
| `api/internal/placement` | HIGH | Cell assignment and routing |
| `api/internal/processing` | HIGH | PG-backed distributed dedup claims |
| `api/internal/domain` | MEDIUM | Types from 5 bounded contexts — see ARCH-005 |
| `api/internal/services` | MEDIUM | 40+ files — cohesive by layer but wide |
| `pkg/errors` | MEDIUM | API-only in practice despite `pkg/` placement — see ARCH-006 |

---

## Recommendations (Prioritized)

1. **[HIGH, 2h] ARCH-001** — Move `ErrTenantNotFound` / `ErrCellNotFound` / `ErrCellNotPinnable` / `ErrCellAtTenantCapacity` from `services/cell_pinner.go` to `pkg/errors` or `api/internal/domain/`. Remove the `services` import from `routes/admin_pin_cell.go`. This closes the only confirmed routes→services coupling and enforces the layer contract cleanly.

2. **[HIGH, 4h] ARCH-003** — Extract a `TenantCRPatcher` interface from `services/cell_pinner.go` and move the K8s dynamic client implementation to `api/internal/k8s/`. Inject at wiring time in `server/router.go`. This removes the only K8s client-go import from the services layer.

3. **[HIGH, 3h] ARCH-002** — Create `api/internal/db/billing_store.go`. Move `fetchPlan` and `fetchStripeCustomerID` SQL into a `PostgresBillingStore`. Wire via existing `BillingHandler` constructor. Remove `pgx` import from `routes/billing.go`.

4. **[MEDIUM, 2h] ARCH-008** — Delete `isUnknownFramework` from `routes/builds.go`. Consolidate framework validation into `api/internal/validation/frameworks.go` (or reuse `argo.ResolveStrategy`). Single source of truth prevents allowlist drift.

5. **[MEDIUM, 1h] ARCH-005** — Split `domain/types.go` into per-bounded-context files. Non-breaking, zero functional impact, significant navigability improvement.

6. **[MEDIUM] ARCH-004** — Evaluate replacing hand-rolled K8s structs in `manifests/renderer_types.go` with SDK types. Assess whether the SDK dependency cost (larger binary, more transitive deps) is justified by the reduced maintenance burden.

7. **[LOW] ARCH-007** — Split `argo/languages.go` by language (`languages_node.go`, `languages_go.go`, etc.). Pure organizational change.

8. **[LOW] ARCH-006** — No action required now. Track for future modular extraction decision.

