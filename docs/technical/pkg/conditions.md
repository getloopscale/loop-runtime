# pkg/conditions Migration Guide

> Per pkg-sota-faang-grade-plan v1.2 T2.2 closure (Adoption Follow-up #1).
> Companion to ADR-0032 (Introduce pkg/conditions kstatus-compatible)
> and ADR-0025 (Operators kstatus universal — already adopted in
> operators/internal/conditions).

## Why this guide exists

`pkg/conditions` provides kstatus-compatible condition contracts
**without depending on `k8s.io/apimachinery`**. This keeps `pkg/go.mod`
lean (single runtime dep: `golang-jwt/jwt/v5`) and lets non-K8s
consumers (cloud, audit, CLI) share the same algorithmic core.

`operators/internal/conditions` has been the operator's canonical
implementation since ADR-0025. It uses `metav1.Condition` directly —
the right choice for operators because kubectl, kstatus.Compute, and
ArgoCD's health controllers expect that shape on `status.conditions`.

The migration goal is **not** to replace `metav1.Condition` in
operator status fields. The goal is to make `pkg/conditions` the
**single algorithmic core** so future controllers, CLI inspectors,
and audit tooling share the same Stalled-wins-over-Reconciling-wins-
over-Ready cascade and the same observedGeneration discipline.

## The adapter pattern

`operators/internal/conditions/pkgadapter.go` provides the bridge:

```go
// fromMetav1 / toMetav1 — unexported lossless conversion.
// SetReadinessSummaryViaPkg — public delegating wrapper.
```

Controllers that want to migrate replace the call to
`conditions.SetReadinessSummary` with `conditions.SetReadinessSummaryViaPkg`.
Behavior is **identical** (same algorithm, just running through a
metav1 ↔ portable conversion at the boundary). The conversion is
verified lossless by `pkgadapter_test.go`.

## Migration template (1 controller at a time)

Existing pattern (operator-local algorithm):

```go
defer func() {
    latest := &loopzerov1.MyResource{}
    if err := r.Get(ctx, req.NamespacedName, latest); err != nil { return }
    conditions.SetReadinessSummary(&latest.Status.Conditions, latest.Generation, detailTypes)
    _ = r.Status().Update(ctx, latest)
}()
```

After migration (delegates to pkg/conditions):

```go
defer func() {
    latest := &loopzerov1.MyResource{}
    if err := r.Get(ctx, req.NamespacedName, latest); err != nil { return }
    conditions.SetReadinessSummaryViaPkg(&latest.Status.Conditions, latest.Generation, detailTypes)
    _ = r.Status().Update(ctx, latest)
}()
```

**That's the whole change at the controller call site.** No
modification to detail types, no struct schema change, no new imports
in CRD types.

## Migration order

Per ADR-0032 + plan v1.2 Adoption Follow-up #1:

| # | Controller | File | Status |
|---|---|---|---|
| 1 | tenant | `operators/internal/tenant/tenant_controller.go` | ✅ migrated (iter 5) — reference impl |
| 2 | environment | `operators/internal/controller/environment_controller.go` | ⏳ next |
| 3 | application | `operators/internal/app/application_controller.go` | ⏳ |
| 4 | exposed-route | `operators/internal/controller/exposedroute_controller.go` | ⏳ |
| 5 | preview | `operators/internal/preview/preview_controller.go` | ⏳ |
| 6 | platform-bootstrap | `operators/internal/platform/platformbootstrap_controller.go` | ⏳ |
| 7 | git-repository-binding | `operators/internal/controller/gitrepositorybinding_controller.go` | ⏳ |
| 8 | cluster-credential | `operators/internal/controller/clustercredential_controller.go` | ⏳ |

Each migration is a single-line edit at the deferred-status-update
site. Each can ship in its own commit, with envtest verification.

## Verification per controller

After migrating a controller:

1. `cd operators && go build ./internal/<package>/...` — compiles clean.
2. `cd operators && go test ./internal/<package>/...` — Ginkgo specs pass.
3. `kubectl apply` a sample CR, then `kubectl get <kind> <name> -o yaml | yq '.status.conditions'` — Ready/Reconciling/Stalled triplet present with correct cascade.
4. ArgoCD app health check (when applicable) reflects the kstatus result.

## Why incremental is safe

Both call paths produce **byte-identical output** for the same input
(verified by `Test_SetReadinessSummary_*` operator-local + adapter
tests). Migration is therefore semantically a no-op; the only
difference is *where the algorithm lives*. Mixed-mode operation (some
controllers use local, some use Pkg) is supported indefinitely — the
adapter is not a deprecation gate, just a hosting choice.

## What if a downstream consumer wants `pkg/conditions.Condition` directly?

That's fully supported — `pkg/conditions` is a complete API surface
for callers that don't already commit to apimachinery. Examples:

```go
import "github.com/loopzero/loopzero/pkg/conditions"

var conds []conditions.Condition
conditions.SetCondition(&conds, conditions.TypeReady, conditions.StatusTrue,
    conditions.ReasonReady, "all good", 1)
phase := conditions.AggregateToPhase(conds, observedGen, currentGen)
// phase: PhasePending | PhaseProvisioning | PhaseReady | PhaseStalled
```

CLI inspectors, audit emitters, and pkg/-internal tooling can use
this directly. Operators with metav1 status fields use the adapter.

## References

- ADR-0025 — Operators kstatus universal (operator-local origins)
- ADR-0032 — Introduce pkg/conditions kstatus-compatible
- pkg-sota-faang-grade-plan v1.2 T2.2 closure
- kstatus library: <https://pkg.go.dev/sigs.k8s.io/cli-utils/pkg/kstatus>
- kpt CRD Status Convention: <https://kpt.dev/reference/schema/crd-status-convention/>
