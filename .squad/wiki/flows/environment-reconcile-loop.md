---
type: Flow
title: "Environment reconcile loop (level-triggered, self-requeuing)"
description: "Process flow, 17 steps, traced through real call sites."
resource: "operators/internal/environment/environment_controller.go"
tags:
  - flow
  - process
  - environment-reconcile-loop
sources:
  - id: flow-root
    resource: "operators/internal/environment/environment_controller.go#L450"
  - id: step-1
    resource: "operators/internal/environment/environment_controller.go#L120"
  - id: step-2
    resource: "operators/internal/environment/environment_controller.go#L135"
  - id: step-3
    resource: "operators/internal/environment/environment_controller.go#L144"
  - id: step-4
    resource: "operators/internal/environment/environment_controller.go#L157"
  - id: step-5
    resource: "operators/internal/environment/environment_controller.go#L161"
  - id: step-6
    resource: "operators/internal/environment/environment_controller.go#L179"
  - id: step-7
    resource: "operators/internal/environment/environment_controller.go#L182"
  - id: step-8
    resource: "operators/internal/environment/environment_controller.go#L209"
  - id: step-9
    resource: "operators/internal/environment/environment_controller.go#L220"
  - id: step-10
    resource: "operators/internal/environment/environment_quota.go#L24"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

**Environment reconcile loop (level-triggered, self-requeuing)** — a `process` flow of 17 steps, cyclic,
traced from the call site that starts it.[^flow-root]

| | |
|---|---|
| **Trigger** | a watch event on an Environment CR, or the RequeueAfter the previous pass returned |
| **Outcome** | the namespace, RBAC, NetworkPolicies and ResourceQuota on the runtime cluster match the CR's spec, status is merge-patched, and the loop schedules itself again in 5 minutes |


A level-triggered controller-runtime loop, and the clearest worked example of the
**single-status-writer invariant** in the repository.

The base snapshot is taken at step 6 — after the finalizer metadata write, before any status
mutation — and the single merge patch at step 16 is the only status write in the pass. A merge
patch carries no `resourceVersion`, so a concurrent spec write by the Tenant controller cannot
409 it.

The loop closes on itself at step 17 with `RequeueAfter: 5 * time.Minute`. That requeue is not
a heartbeat: it is what corrects drift on cross-cluster resources that nothing watches.


# Schema

The steps, in order. Every row is anchored to the line that performs it.


| # | Actor | Step | Hands to | Condition | Data | Anchor |
|---|---|---|---|---|---|---|
| 1 | `operators.environment` | controller-runtime delivers a reconcile request for one Environment CR — from a watch event or from the previous pass's RequeueAfter | `operators.environment` | — | `ctrl.Request{NamespacedName}` | `operators/internal/environment/environment_controller.go:120` |
| 2 | `operators.environment` | Get the Environment CR from the control-plane cluster | `ext.k8sapi` | IsNotFound -> return without requeue; the CR is gone | `loopzerov1.Environment` | `operators/internal/environment/environment_controller.go:135` |
| 3 | `operators.environment` | lift loopzero.io/correlation-id off the CR annotations into the reconcile context | `pkg.correlation` | — | `correlation id` | `operators/internal/environment/environment_controller.go:144` |
| 4 | `operators.environment` | branch to reconcileDelete — the verify-then-remove finalizer path | `operators.environment` | !env.DeletionTimestamp.IsZero() | `the CR being deleted` | `operators/internal/environment/environment_controller.go:157` |
| 5 | `operators.environment` | EnsureFinalizer writes loopzero.io/environment onto the CR metadata before any owned resource is created | `ext.k8sapi` | finalizer not already present | `finalizer string` | `operators/internal/environment/environment_controller.go:161` |
| 6 | `operators.environment` | DeepCopy the CR as the merge-patch base — taken AFTER the finalizer metadata write and BEFORE any status mutation; this is the single-status-writer invariant | `operators.environment` | — | `base *loopzerov1.Environment` | `operators/internal/environment/environment_controller.go:179` |
| 7 | `operators.environment` | drive the empty phase to Pending through the Environment state machine — direct status assignment is forbidden | `pkg.statemachine` | env.Status.Phase == "" | `EPhEmpty + EPEventCreate -> Pending` | `operators/internal/environment/environment_controller.go:182` |
| 8 | `operators.environment` | compute the canonical namespace loopzero-<tenant>-<project>-<env> from spec.project, falling back to spec.namespace.name for pre-T3.1 CRs | `pkg.domain` | env.Spec.Project != "" | `EnvRef -> namespace name` | `operators/internal/environment/environment_controller.go:209` |
| 9 | `operators.environment` | set NamespaceReady=False with reason InvalidEnvRef or NameTooLong, merge-patch it, and return WITHOUT requeue — the name is structurally invalid until the user changes the spec | `ext.k8sapi` | NewEnvRef or ref.Namespace() returns an error | `metav1.Condition` | `operators/internal/environment/environment_controller.go:220` |
| 10 | `operators.environment` | reconcileNamespace creates/updates the namespace on the cell's runtime cluster, resolved through runtimeClientForEnv | `operators.cellrouting` | — | `corev1.Namespace` | `operators/internal/environment/environment_quota.go:24` |
| 11 | `operators.environment` | reconcileRBAC provisions the per-environment Role/RoleBinding set, gated by the tenant's plan tier | `ext.k8sapi` | — | `rbacv1 objects` | `operators/internal/environment/environment_rbac.go:26` |
| 12 | `operators.environment` | reconcileNetworkPolicies applies deny-all plus allow-ingress/allow-egress isolation | `ext.k8sapi` | spec.networkPolicy.isolate | `networkingv1.NetworkPolicy` | `operators/internal/environment/environment_network.go:22` |
| 13 | `operators.environment` | reconcileQuota applies the CPU/memory/storage ResourceQuota | `ext.k8sapi` | spec.resourceQuota != nil | `corev1.ResourceQuota` | `operators/internal/environment/environment_quota.go:58` |
| 14 | `operators.environment` | on any step error: set that step's condition False with reason ReconcileFailed, merge-patch, and return the error so controller-runtime requeues with backoff | `ext.k8sapi` | step.fn() returned an error | `the failing condition` | `operators/internal/environment/environment_controller.go:279` |
| 15 | `operators.environment` | all four conditions True — drive the phase to Active through the state machine and record status.namespace + observedGeneration | `pkg.statemachine` | env.Status.Phase != Active | `EPEventProvisioned -> Active` | `operators/internal/environment/environment_controller.go:288` |
| 16 | `operators.environment` | persist phase + conditions + namespace + observedGeneration with a single merge patch off the base snapshot — no refetch, no resourceVersion, so a concurrent Tenant-controller spec write cannot 409 it | `ext.k8sapi` | — | `.status merge patch` | `operators/internal/environment/environment_controller.go:301` |
| 17 | `operators.environment` | return RequeueAfter 5 minutes — this closes the loop back to step 1 and is what corrects drift on the cross-cluster resources nothing watches | `operators.environment` | — | `ctrl.Result{RequeueAfter: 5 * time.Minute}` | `operators/internal/environment/environment_controller.go:307` |

# Relationships


- Driven by the Environment reconciler in [`/components/operators.md`](/components/operators.md).
- The CR it reconciles is [`/entities/crd-environment.md`](/entities/crd-environment.md); its
  phases are [`/entities/state-machine-environment.md`](/entities/state-machine-environment.md).
- Step 10 resolves the runtime cluster through
  [`/components/multi-cell-routing.md`](/components/multi-cell-routing.md).
- Step 11 creates the per-environment identities in
  [`/entities/rbac-roles.md`](/entities/rbac-roles.md), plan-tiered by `rbacRulesForPlan`.
- It is invoked implicitly on first deploy — step 5 of
  [`/flows/deploy-container-app.md`](/flows/deploy-container-app.md).


# Evidence

Every step in the table above carries the `file:line` that performs it. The `sources` block below lists the flow's own anchor and the first ten steps; steps 11 to 17 are anchored in the table only.

[^flow-root]: `operators/internal/environment/environment_controller.go:450`
[^step-1]: `operators/internal/environment/environment_controller.go:120`
[^step-2]: `operators/internal/environment/environment_controller.go:135`
[^step-3]: `operators/internal/environment/environment_controller.go:144`
[^step-4]: `operators/internal/environment/environment_controller.go:157`
[^step-5]: `operators/internal/environment/environment_controller.go:161`
[^step-6]: `operators/internal/environment/environment_controller.go:179`
[^step-7]: `operators/internal/environment/environment_controller.go:182`
[^step-8]: `operators/internal/environment/environment_controller.go:209`
[^step-9]: `operators/internal/environment/environment_controller.go:220`
[^step-10]: `operators/internal/environment/environment_quota.go:24`
