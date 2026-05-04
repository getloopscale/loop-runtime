# ADR-0025 — kstatus Conditions (Ready/Reconciling/Stalled) Universal

## Status

Accepted — 2026-05-04 (MEET-2026-05-04-068)

## Context

LoopZero's 8 controllers emit per-step detail conditions (e.g., `TypeServiceResolved`, `TypeApplicationHealthy`, `TypePreviewBuildCompleted`) but lack a universal summary layer. Specifically:

- `internal/conditions/types.go` defines `ReasonReconciling` but no `TypeReconciling` or `TypeStalled`.
- No universal `TypeReady` summary condition across the 10 LoopZero CRDs.
- `kubectl wait --for=condition=Ready loopzeroapplications/<name>` returns `condition not found` because no controller emits the canonical Ready condition.
- External tooling (Argo Rollouts, Flux Helm releases, Crossplane composition functions) consume kstatus via `sigs.k8s.io/cli-utils/pkg/kstatus.Compute(obj)`. This function expects `Ready/Reconciling/Stalled` per the kpt CRD Status Convention. Without these, integrators must write LoopZero-specific status mappers.

Reference: kpt CRD Status Convention (https://kpt.dev/reference/schema/crd-status-convention/) defines:
- `Ready=True` — fully reconciled.
- `Reconciling=True` — controller actively working.
- `Stalled=True` — stuck, manual intervention needed.
- These three are mutually exclusive in steady state.

Roadmap §QW-2 + gate OP-G18 require this for FAANG-bar status semantics.

## Decision

All 10 LoopZero CRDs MUST expose `Ready`, `Reconciling`, and `Stalled` condition types in `status.conditions`, computed by a centralized helper `conditions.SetReadinessSummary(...)` from per-step detail conditions. The helper MUST be the LAST status mutation in every Reconcile (executed via `defer` for crash safety).

This ADR ratifies plan T2.2 (constants), T3.1 (helper), T3.2 (apply in 8 controllers), T3.3 (kstatus library integration test) with the following conditions integrated.

### Conditions (per k8s-controller-engineer + verification-architect)

1. **Crash safety via defer (per k8s-controller-engineer):** Each Reconcile uses `defer` pattern so `SetReadinessSummary` runs even on panic / early error return. If `retErr != nil` and not transient (per `errors.IsConflict || errors.IsTimeout || isContextCanceled`), helper stamps `Stalled=True` with reason `Failed`. Otherwise transient errors leave `Reconciling=True` with reason `Reconciling`.

   ```go
   func (r *FooReconciler) Reconcile(ctx, req) (res ctrl.Result, retErr error) {
       var foo loopzerov1.Foo
       if err := r.Get(ctx, req.NamespacedName, &foo); err != nil { ... }
       defer func() {
           conditions.SetReadinessSummary(&foo.Status.Conditions, foo.Generation,
               r.detailTypes(), retErr)
           if updateErr := conditions.UpdateStatusWithRetry(ctx, r.Client, &foo, identity); updateErr != nil {
               retErr = errors.Wrap(retErr, updateErr.Error())
           }
       }()
       // ... per-step reconcile logic, calling conditions.SetCondition for detail types
   }
   ```

2. **Idempotency test FIRST (per k8s-controller-engineer):** `Test_SetReadinessSummary_IdempotentNoop` — called twice with same input → second call produces zero diff (no `lastTransitionTime` bump). Without this, every reconcile becomes a status write, defeating predicates from ADR-0017 (predicate filtering).

3. **ClusterCredential polarity guard (per k8s-controller-engineer + verification-architect):** ClusterCredential's `TypeSecretNotFound=True` means BAD (inverted polarity). Helper accepts only positive-polarity detail types in `detailTypes` slice; ClusterCredential's list is `[TypeConnected]` only. Add `Test_ClusterCredential_SecretNotFoundExcludesReady` asserting that helper rejects or correctly handles inverted-polarity types. If someone adds `TypeSecretNotFound` to detailTypes later, summary semantics MUST not flip silently.

4. **Lifecycle stage tests (per verification-architect):** T3.3 `kstatus.Compute` integration test covers all 10 CRDs at lifecycle stages `Creating → Ready → Updating → Deleting`, asserting returned status `InProgress / Current / InProgress / Terminating`. Steady-state-only is insufficient.

### kstatus library compliance

`sigs.k8s.io/cli-utils/pkg/kstatus.Compute(obj)` returns:
- `Current` — `Ready=True` with matching `observedGeneration`.
- `InProgress` — `Reconciling=True`.
- `Failed` — `Stalled=True`.
- `NotFound` — object doesn't exist.
- `Terminating` — `metadata.deletionTimestamp` set.

LoopZero's adoption MUST match these semantics exactly. The helper algorithm:

- `Ready=True` iff all `detailTypes` have status `True` AND each detail's `observedGeneration == obj.Generation`.
- `Reconciling=True` iff any detail has status `False` with reason in {Reconciling, Progressing, Waiting} AND no detail has Failed/Stalled.
- `Stalled=True` iff any detail has status `False` with reason in {Failed, Stalled} (Stalled wins over Reconciling — failure takes priority).

## Rationale

### Why universal

Half of LoopZero's CRDs already emit per-step conditions; the other half emit nothing. External tooling can't distinguish "no conditions" from "still initializing" from "succeeded." Universal Ready/Reconciling/Stalled is the K8s-ecosystem convention; non-adoption forces every integrator to write LoopZero-specific status mappers.

### Why centralized helper

Without helper, each of 8 controllers reimplements the summary logic with subtle bugs (forgetting `observedGeneration`, wrong polarity for inverted-conditions, racing with `lastTransitionTime` semantics). Centralized helper = one place to fix bugs.

### Why defer pattern

A controller that sets `Reconciling=True` at entry then panics leaves the CR stuck Reconciling forever (kubebuilder's canonical kstatus failure mode). `defer SetReadinessSummary` ensures summary runs even on early return, panic, or cancellation. This is non-optional for kstatus correctness.

### Why kpt convention over alternatives

- **Argo Rollouts uses Ready/Stalled/Paused** (Paused doesn't apply to LoopZero).
- **Crossplane uses Ready/Synced** (Synced is composition-specific).
- **Knative uses Ready/Active/Routes** (Knative-specific).
- **kpt convention (Ready/Reconciling/Stalled)** is the most general; consumed by `cli-utils.kstatus`, `kubectl wait`, Flux Helm releases, ArgoCD health checks, and Crossplane functions.

## Alternatives considered

- **Ready-only:** Rejected — loses progress visibility (`Reconciling`) and stuck-state visibility (`Stalled`).
- **Per-controller bespoke summaries:** Rejected — already the de-facto state, recognized as anti-pattern.
- **Skip summary, push per-step conditions only:** Rejected — `kubectl wait --for=condition=Ready` is the most common user-facing query; not supporting it is hostile UX.

## Consequences

### Enables

- `kubectl wait --for=condition=Ready loopzeroapplications/<name> --timeout=300s` works for all 10 CRDs.
- External tooling (Argo Rollouts, Flux, ArgoCD health checks) consumes LoopZero CRDs without custom integration code.
- LoopZero CLI can emit progress/failure based on standard conditions (no parsing of bespoke fields).

### Constrains

- Every controller's Reconcile MUST be refactored to use `defer SetReadinessSummary` pattern.
- New controllers MUST follow this pattern from day 1 (CI gate via lint).
- Helper itself becomes a critical-path dependency — bugs here affect all controllers.

### Reversibility

- Helper can be removed; per-controller summary logic restored. Low-risk reversal but loses universal kstatus support.
- ADR itself reversible only via new MEET record + new ADR.

## Risks

- **MEDIUM:** Bug in helper algorithm (e.g., `Stalled` not winning over `Reconciling`) corrupts status across all 8 controllers. Mitigation: 10+ unit tests per T3.1 plus envtest spec per controller.
- **MEDIUM:** ClusterCredential inverted-polarity edge case — silently breaks if polarity guard test missing. Mitigation: `Test_ClusterCredential_SecretNotFoundExcludesReady` mandatory.
- **LOW:** Helper's idempotency requires careful `lastTransitionTime` handling. Tested via `Test_SetReadinessSummary_IdempotentNoop`.

## References

- `docs/plans/operators-sota-faang-grade-plan.md` v1.1 D3 + Phase 2 T2.2 + Phase 3 T3.1-T3.3
- `docs/technical/operators/improvement-roadmap.md` §QW-2
- `docs/technical/operators/sota-gates.md` OP-G18
- kpt CRD Status Convention (https://kpt.dev/reference/schema/crd-status-convention/)
- `sigs.k8s.io/cli-utils/pkg/kstatus` (https://github.com/kubernetes-sigs/cli-utils/blob/master/pkg/kstatus/README.md)
- ADR-0023 — D1 (helper lives in `internal/conditions/` shared across split binaries)
- ADR-0024 — D2 retry helper (composes with summary helper at status update site)
- MEET-2026-05-04-068
