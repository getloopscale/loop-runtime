# ADR-0024 — `retry.RetryOnConflict` Mandatory for All Status Updates

## Status

Accepted — 2026-05-04 (MEET-2026-05-04-068)

## Context

LoopZero's 8 controllers contain 70+ call sites of `r.Status().Update(ctx, obj)` without retry-on-conflict wrapping. Only `operators/internal/argocd/real_provisioner.go:237` uses the canonical kubebuilder pattern `retry.RetryOnConflict(retry.DefaultRetry, func() error {...})`.

In production, 30+ concurrent webhooks (GitHub auto-deploy ~30/h baseline) frequently hit the same controller targeting the same CR. Each Status update is `GET → mutate → PUT` against the same `resourceVersion` — concurrent updates produce `409 Conflict` errors that surface as log noise. Worse: the entire reconcile is requeued on conflict (~50-500ms wasted per occurrence) instead of the much cheaper `client-go` retry loop (~1.6s ceiling for 5 attempts).

Reference: kubebuilder book §"Reference / Good Practices / Conflict Errors" explicitly endorses `retry.RetryOnConflict(retry.DefaultRetry, ...)` for status writes. SIG controller-runtime maintainers recommend the pattern.

Anti-pattern AP-1 (per `improvement-roadmap.md`): inconsistent retry handling masks real bugs in operator logs and degrades P95 reconcile latency under contention.

## Decision

Every `r.Status().Update(ctx, obj)` call site in `operators/internal/{tenant,app,preview,platform}/` MUST be wrapped in `retry.RetryOnConflict`. A new helper `conditions.UpdateStatusWithRetry[T client.Object]` in `operators/internal/conditions/retry.go` becomes the canonical entry point. Bare `Status().Update()` calls are forbidden in new code (CI gate via static analysis after migration completes).

This ADR ratifies plan T2.3 (helper introduction) and T3.2 (apply during kstatus rollout) with the following conditions integrated.

### Conditions (per k8s-controller-engineer + sre-engineer)

1. **SSA evaluation note:** ADR explicitly defers Server-Side Apply (SSA) to post-launch. RetryOnConflict is the launch-window choice; SSA with field manager `loopzero-<controller>` is the post-launch refinement path. Document this in the helper's package doc comment.
2. **Helper signature constraints:**
   - Accept `client.Object` typed instances only (no `*unstructured.Unstructured` without explicit GVK guard).
   - `mutate` function operates on a freshly-fetched copy each retry attempt.
   - `defer ctx.Err()` short-circuit between attempts so retry honors context cancellation (e.g., manager shutdown).
   - Max-attempts cap via `wait.Backoff{Steps: 5, Cap: 2*time.Second}` to bound worker saturation.
   - Spec-mutation guard: helper deep-copies the object pre-mutate, asserts post-mutate that `Spec` is unchanged. If `mutate` accidentally edits Spec, helper returns explicit error (Spec changes via `Status().Update()` are silently dropped — common footgun).
3. **Observability requirement:** `loopzero_status_update_conflicts_total{controller}` counter introduced in same PR as helper. Alert on `rate(loopzero_status_update_conflicts_total[5m]) > N` (threshold TBD post-baseline observation). Without this metric, helper effectiveness is unmeasurable.
4. **Staged CI gate enablement:** `semgrep` / `go-ruleguard` rule rejecting bare `r.Status().Update(` in `operators/internal/{tenant,app,preview,platform}/` ENABLED only after all 70 sites migrated. Migration in 8 batches (one PR per controller) with envtest assertion per batch simulating a conflict. Enabling the gate before migration creates a flag-day where every unrelated PR turns red.

### Test coverage requirements

Per k8s-controller-engineer review, T2.3 plan lists 4 unit tests; missing:

- `Test_UpdateStatusWithRetry_MutateModifiesSpec_IsRejected` — guard for spec-mutation footgun.
- `Test_UpdateStatusWithRetry_ContextCanceledMidLoop` — ctx.Err() short-circuit.
- `Test_UpdateStatusWithRetry_GenericNonConflictError_NoRetry` — single attempt on transient `Unauthorized`.
- `Test_UpdateStatusWithRetry_RaceWith3Writers` (envtest, not unit) — 3 goroutines mutating same Tenant; assert no lost update, final state matches last logical write per `lastTransitionTime`.

## Rationale

### Why mandatory

Inconsistent application of retry-on-conflict is worse than no application: 1 controller has it, 7 don't. Anyone refactoring without knowing the convention picks the wrong path. Mandatory + helper + CI gate eliminates the convention question.

### Why generic helper

Without helper, every call site grows from 1 line to ~5 lines of boilerplate. Boilerplate dies in code review fatigue. Single helper with type-safe generics (`T client.Object`) keeps call sites readable.

### Why NOT SSA in launch window

Server-Side Apply requires careful field manager design (each controller becomes an "owner" of specific fields). Migration impact is multi-week. Launch window prioritizes log-noise reduction + measurable conflict-rate visibility. SSA is a follow-up Sprint 9+.

## Alternatives considered

- **Status quo (do nothing):** Rejected — known anti-pattern, log noise masks bugs, P95 latency suffers under contention.
- **Server-Side Apply directly:** Rejected for launch — bigger refactor, requires field-ownership design across all 8 controllers. Punted to post-launch ADR.
- **Custom retry wrapper inline in each controller:** Rejected — duplicates retry logic, no central observability, no enforcement gate.

## Consequences

### Enables

- Clean operator logs (no spurious `409 Conflict` noise).
- Lower P95 reconcile latency under contention (avoids full reconcile requeue).
- Measurable conflict rate via `loopzero_status_update_conflicts_total` — basis for capacity planning.
- Foundation for SSA migration post-launch (call-site shape is identical; only helper internals change).

### Constrains

- Migration: 70 call sites refactored in 8 batches (~1-2 days of work each).
- CI gate enabled only after migration complete (bundled with last batch PR).
- Observability requirement: new metric must ship before helper deployed (chicken-and-egg avoided by deploying together).

### Reversibility

- Per-controller revert PR. Helper stays; controllers revert to bare `Status().Update()`. Low-risk, no data loss.
- Helper itself can be removed after SSA migration (post-launch).

## Risks

- **MEDIUM:** Helper hides errors if `RetryOnConflict` treats unrelated transient errors as retriable. Mitigation: helper explicitly checks `errors.IsConflict` and returns immediately on other error classes.
- **MEDIUM:** Worker saturation under retry storm if backoff cap too high. Mitigation: `Cap: 2*time.Second` + worker count alert (`controller_runtime_active_workers` per binary).
- **LOW:** Spec-mutation guard adds 1 deep-copy per call (~10µs). Net positive (saves ~50ms reconcile retry on conflict).

## References

- `docs/plans/operators-sota-faang-grade-plan.md` v1.1 D2 + Phase 2 T2.3 + Phase 3 T3.2
- `docs/technical/operators/improvement-roadmap.md` §QW-5
- `docs/technical/operators/sota-gates.md` OP-G17 + OP-G18 (kstatus rollout depends on this helper)
- kubebuilder book §"Reference / Good Practices / Conflict Errors" (https://book.kubebuilder.io/reference/good-practices.html)
- `client-go` `k8s.io/client-go/util/retry` package
- `operators/internal/argocd/real_provisioner.go:237` (existing pattern reference)
- ADR-0023 — D1 split tree canonical (helper lives in `internal/conditions/` shared across split binaries)
- MEET-2026-05-04-068
