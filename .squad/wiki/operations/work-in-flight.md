---
type: Operations
title: "Declared unfinished work"
description: "Thirteen first-party TODOs with an owner and an age — six blocked, five open, two deferred."
resource: "."
tags:
  - todo
  - debt
  - work-in-progress
sources:
  - id: wi-first
    resource: "infra/pulumi-platform/src/components/bootstrap-shared.ts#L234"
    note: "Rename the Traefik Helm release to ${role}-traefik on the next destroy+recreate cycle"
  - id: wi-todo-noise
    resource: "cmd/loopzero-ops/leak_subcmd.go#L100"
    note: "36% of first-party TODO hits are the Portuguese word for \"every\", so the TODO marker cannot be used as a debt signal here"
  - id: wi-index-ts-129
    resource: "infra/pulumi-infra/src/index.ts#L129"
    note: "Re-add the resource with proper import handling once the pipeline is green"
  - id: wi-tenant_controller-go-145
    resource: "operators/internal/tenant/tenant_controller.go#L145"
    note: "requiredProvisioningConditions() omits Harbor — add a runtime-conditional detailTypes selector"
  - id: wi-auth_failure_audit-go-143
    resource: "api/internal/middleware/auth/auth_failure_audit.go#L143"
    note: "Emit a metric on the auth audit-log fail-open path (EC-NEW-12)"
  - id: wi-sync_callback_activator-go-157
    resource: "api/internal/release/sync_callback_activator.go#L157"
    note: "TODO(T6.4 + T1.3): replace context.Background() once OTel trace propagation reaches the ArgoCD callback handler"
  - id: wi-sync_callback_activator-go-264
    resource: "api/internal/release/sync_callback_activator.go#L264"
    note: "TODO(sprint-5): read tenant/env from ArgoCD labels instead of parsing the app name"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

Thirteen items the code itself declares unfinished, classified by whether something else has to
happen first. Age is measured from the commit that introduced the comment.

**These are the first-party TODOs only.** 36% of raw TODO matches in this repository are the
Portuguese word *todo* — "every" — so a grep count is not a debt signal here, and the map
filtered on that before recording anything.[^wi-todo-noise]

# Schema

## Blocked — 6 items waiting on something else

| Age (days) | Item | Anchor |
|---|---|---|
| 154 | Rename the Traefik Helm release to ${role}-traefik on the next destroy+recreate cycle | `infra/pulumi-platform/src/components/bootstrap-shared.ts:234` |
| 153 | Re-add the resource with proper import handling once the pipeline is green | `infra/pulumi-infra/src/index.ts:129` |
| 125 | TODO(T6.4 + T1.3): replace context.Background() once OTel trace propagation reaches the ArgoCD callback handler | `api/internal/release/sync_callback_activator.go:157` |
| 31 | TODO(#255) failingGate — gate works and FAILS on a real finding; fix the finding, then wire | `cmd/loopzero-ops/quality_gate_wiring_test.go:147` |
| 31 | TODO(#255) slowGate — exceeds the 25s budget; needs profiling or a narrower scan before it can gate every push | `cmd/loopzero-ops/quality_gate_wiring_test.go:150` |
| 31 | TODO(#255) brokenGate — produces a wrong verdict; must be repaired before it can be wired | `cmd/loopzero-ops/quality_gate_wiring_test.go:170` |

Four of the six are a single cluster: `cmd/loopzero-ops/quality_gate_wiring_test.go` records, in
code, **five quality gates that exist and are deliberately not wired**, each with the reason.
One fails on a real finding, one exceeds the 25-second budget, one produces a wrong verdict, one
passes vacuously, and one is simply unwired debt awaiting a blocking-versus-advisory decision.

That file is unusually honest: wiring the vacuous gate would read as coverage while asserting
nothing, and the comment says so. It is the opposite of the pattern in
[`/components/audit-package.md`](/components/audit-package.md), where an unwired mechanism reads
as active.

## Open — 5 items nothing blocks

| Age (days) | Item | Anchor |
|---|---|---|
| 133 | Add the go vet rule that fails builds when the single-cell stub constructor is called from production paths | `api/internal/placement/router.go:47` |
| 122 | Emit a metric on the auth audit-log fail-open path (EC-NEW-12) | `api/internal/middleware/auth/auth_failure_audit.go:143` |
| 93 | requiredProvisioningConditions() omits Harbor — add a runtime-conditional detailTypes selector | `operators/internal/tenant/tenant_controller.go:145` |
| 31 | TODO(#255) unwiredDebt — gates nobody wired; needs a decision on blocking vs advisory | `cmd/loopzero-ops/quality_gate_wiring_test.go:140` |
| 31 | TODO(#255) vacuousGate — passes vacuously; wiring it would read as coverage while asserting nothing | `cmd/loopzero-ops/quality_gate_wiring_test.go:163` |

## Deferred — 2 items explicitly out of scope

| Age (days) | Item | Anchor |
|---|---|---|
| 166 | TODO(sprint-5): read tenant/env from ArgoCD labels instead of parsing the app name | `api/internal/release/sync_callback_activator.go:264` |
| 118 | Migrate meta to flat metaDomain/metaExpiresAtIso — documented TODO, explicitly out of scope here | `infra/pulumi-drill/src/__tests__/structure.test.ts:339` |

# Relationships

- The gates these items describe are
  [`/operations/quality-gates-and-tests.md`](/operations/quality-gates-and-tests.md); the binary
  is [`/components/loopzero-ops.md`](/components/loopzero-ops.md).
- The two oldest items — 153 and 154 days — are both in the Pulumi layer described in
  [`/components/pulumi-legacy.md`](/components/pulumi-legacy.md), which is the layer declared
  frozen.
- The placement `go vet` rule at `api/internal/placement/router.go:47` would guard the
  single-cell stub constructor described in
  [`/components/multi-cell-routing.md`](/components/multi-cell-routing.md).
- Defects the map found that the code does **not** declare are
  [`/operations/contradictions.md`](/operations/contradictions.md) — a different list, and a
  longer one.

# Evidence

[^wi-first]: `infra/pulumi-platform/src/components/bootstrap-shared.ts:234`
[^wi-todo-noise]: `cmd/loopzero-ops/leak_subcmd.go:100`
[^wi-index-ts-129]: `infra/pulumi-infra/src/index.ts:129`
[^wi-tenant_controller-go-145]: `operators/internal/tenant/tenant_controller.go:145`
[^wi-auth_failure_audit-go-143]: `api/internal/middleware/auth/auth_failure_audit.go:143`
[^wi-sync_callback_activator-go-157]: `api/internal/release/sync_callback_activator.go:157`
[^wi-sync_callback_activator-go-264]: `api/internal/release/sync_callback_activator.go:264`
