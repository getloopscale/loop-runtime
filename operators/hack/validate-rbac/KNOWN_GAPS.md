# validate-rbac — Known Blind Spots

The static AST analyzer (originally scoped by ADR-0026, removed in
2026-05-11 governance consolidation) has documented gaps where it
CANNOT detect RBAC violations.
Each gap has a corresponding negative test fixture (planned) so
regressions in the analyzer's coverage become visible.

## 0. A call with no marker covering it

**Pattern:** a controller calls `r.Get(ctx, key, &corev1.Secret{})` and no
`+kubebuilder:rbac` marker grants `get` on `secrets`.
**Why missed:** the analyzer does not resolve the type of the object argument,
so it cannot tell which (group, resource) a call touches. It checks markers
for shape and wildcards, and `--strict` checks markers against call verbs,
but never calls against markers.
**Mitigation:** none in this analyzer; the missing permission surfaces as
`Forbidden` at runtime, and PR review of each new call is the only check.

## 1. Dynamic client usage

**Pattern:** `dynamic.NewForConfig(cfg).Resource(gvr).Namespace(ns).Get(...)`.
**Why missed:** call site has no `r.Client.Get` shape; verb is a
runtime string from `gvr`. Pure AST cannot resolve.
**Mitigation:** runtime monitoring via `loopzero_status_update_conflicts_total`
(ADR-0024) + manual review of any `dynamic.NewForConfig` call site.
**LoopZero callers:** none currently. Tracked.

## 2. Helper indirection

**Pattern:** wrapping `r.Client.Patch` inside `shared.applyServerSide(ctx, obj)`.
**Why missed:** analyzer only walks the immediate call expression in
the controller file; it doesn't follow the call graph into helper
packages.
**Mitigation:** all `shared/*.go` exports must declare what verb-class
they invoke (doc comment "// Calls: r.Client.Patch"). PR review.

## 3. Cross-cluster clients

**Pattern:** `r.RuntimeClient.X` and `r.BuildClient.X` calls.
**Why missed by THIS analyzer:** by design — markers cover only the
LOCAL (mgmt cluster) RBAC. Cross-cluster RBAC is a separate concern
covered by per-ADR-0026 condition #2: `kubectl auth can-i` matrix
against kind/k3d cluster in CI.
**Mitigation:** distinct CI gate (planned in T4.3 follow-up).

## 4. Watches and informers

**Pattern:** `Owns(&corev1.Secret{})` in `SetupXController` requires
implicit `list+watch` but generates no `r.Client.X` site.
**Why missed:** Setup functions aren't per-Reconciler markers.
**Mitigation:** controller-gen's own RBAC generation handles Owns/Watches
correctly; the analyzer doesn't need to.

## 5. Field selectors / label selectors

**Pattern:** `r.Client.List(..., client.MatchingLabels{...})` triggers
implicit `list` against the full collection at the API server.
**Why missed:** marker says `verbs=list` so this is correctly covered.
This entry exists for completeness.

## 6. EventRecorder calls

**Pattern:** `r.EventRecorder.Eventf(obj, ...)` internally `Patch`es
events.
**Why missed:** call shape is on EventRecorder, not r.Client.
**Mitigation:** controller-gen v0.16+ recognizes EventRecorder and
emits the events RBAC rule automatically.

## 7. Subresource access (status/finalizers)

**Pattern:** `r.Status().Update(ctx, obj)` requires `<resource>/status`
in marker.
**Why missed:** the analyzer doesn't follow `Status()` chained calls.
**Mitigation:** every marker for a primary resource MUST also include
the matching `/status` and `/finalizers` markers. PR review checklist.

## How to add fixtures

When adding a new blind spot:

1. Add a `testdata/false-positive/<name>.go` fixture mirroring the gap.
2. Add a comment block describing what the analyzer SHOULD detect but doesn't.
3. Document the manual mitigation here.
4. If/when the analyzer learns to detect this pattern, move the
   fixture to `testdata/positive/` and assert non-zero exit.
