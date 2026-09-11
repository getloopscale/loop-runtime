---
type: design-drawing
title: D1 — the lifecycle an application moves through
date: 2026-09-10
status: draft
---

# D1 — The lifecycle of an Application

The central object is `Application` (`operators/api/v1/application_types.go`). Everything
the platform does to a customer's agentic app is a transition here.

```mermaid
stateDiagram-v2
    [*] --> Progressing : Application created
    Progressing --> Serving : workload ready
    Progressing --> Down : workload never came up
    Serving --> Reachable : route resolves from outside
    Reachable --> Degraded : partial failure
    Degraded --> Reachable : recovered
    Degraded --> Down : total failure
    Down --> Progressing : reconcile retries
    Progressing --> Cleaned : reaped — manifest gone past grace period
    Cleaned --> [*]
```

## Measured

The six phases are read from the CRD, not invented:

Counted with this, and the command is written down because a number nobody can reproduce is
a claim:

```bash
grep -rho "ApplicationPhase<Phase>\b" operators/internal --include="*.go" | wc -l
```

Occurrences of the qualified constant, **test files included** — they are where the phase
contract is asserted, so excluding them measures less than the phase is used.

| Phase | Written by (occurrences in `operators/internal`) |
|---|---|
| `Cleaned` | 6 |
| `Progressing` | 5 |
| `Serving` | 3 |
| `Reachable` | 3 |
| `Down` | 2 |
| `Degraded` | 2 |

`Serving` and `Reachable` are **separate phases**, and that separation is the design:
a workload can be up and not yet reachable from outside. Collapsing them would report
an app as available before its route resolves.

## Decided

- **`Cleaned` is the only terminal phase.** Everything else can be re-entered by a
  reconcile.
- **`Down` is recoverable, not terminal.** The controller retries; an app that failed
  to come up is not an app that is finished.

## Corrected 2026-09-11 by the design panel

**`Cleaned` is reachable from `Progressing` and from nowhere else.** This drawing had
`Reachable --> Cleaned` and `Down --> Cleaned`, both labelled *deleted*. `ShouldReap`
(`operators/internal/app/application_reaper.go:80-84`) refuses every other phase by name:

```go
if app.Status.Phase != loopzerov1.ApplicationPhaseProgressing {
    return false, fmt.Sprintf("phase %q is not Progressing", app.Status.Phase)
}
```

and `application_types.go` says the same in prose — `Cleaned` is set *"when the CR has been
Progressing for > grace period AND the GitOps source no longer contains its manifest"*. It is
the orphan reaper, not a deletion path.

**What that leaves genuinely open** is below: a `Reachable` app whose manifest disappears from
the source has no drawn transition at all. The reaper will not take it, because it is not
`Progressing`.

## Open — for the sign-off to resolve

- **A `Reachable` app whose manifest leaves the source has no transition.** The reaper only
  takes `Progressing`. Whether that app should be reaped, alerted on, or left standing was
  never decided — found 2026-09-11 by correcting the two deletion edges above.

- **Is there a suspended state?** Nothing in the CRD carries one. For long-running
  agents this is the question that decides whether an idle agent costs money.
- **What happens to a running app during a platform upgrade?** No `Draining` phase
  exists. Either upgrades do not touch running workloads, or the gap is real.

## Related

- [`../decisions/agent-state-never-lives-in-the-pod.md`](../decisions/agent-state-never-lives-in-the-pod.md) — the invariant this drawing's open questions resolved into

- [`trust.md`](trust.md) — D2, the boundary these phases cross
- [`durability.md`](durability.md) — D4, what survives a phase change
- [`sign-off.md`](sign-off.md) — the open questions this drawing leaves
