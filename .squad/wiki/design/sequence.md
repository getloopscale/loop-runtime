---
type: design-drawing
title: D3 — call order, including what happens when a step fails
date: 2026-09-10
status: draft
---

# D3 — From nothing to running, including what fails

`PIECE-6` states the requirement this drawing exists to hold: *"that the route from
nothing to running is **one** route with no fork, and that every failure along it names
what she must change."* So the failure paths are the point here, not the decoration.

```mermaid
sequenceDiagram
    participant Dev as Developer
    participant Entry as PIECE-6 Entry path
    participant Build as PIECE-10 Build path
    participant Pub as PIECE-7 Publishing
    participant Dist as PIECE-8 Distribution
    participant Op as app-operator
    participant App as Application

    Dev->>Entry: loopzero up
    Entry->>Build: build request
    Build--xEntry: build failed — names the file
    Build-->>Pub: artifact
    Pub--xEntry: publish rejected — names the missing field
    Pub-->>Dist: published version
    Dist->>Op: Application desired state
    Op->>App: reconcile
    App-->>Op: Progressing
    Op--xDev: Down — workload never came up
    App-->>Op: Serving
    Op->>Op: check route
    Op-->>Dev: Reachable — route resolves
    Note over Op,Dev: route fails -> phase stays Serving.<br/>Degraded is unreachable while endpoints are ready.
```

## Decided

- **One route, no fork.** Every failure returns to the developer naming what to change,
  rather than surfacing as a phase they have to interpret.
- **`Serving` is not the end.** The sequence continues to the route check, because an
  app that is up and unreachable is not delivered.

**Corrected 2026-09-11 by the design panel.** This drawing had the route failure reporting
`Degraded`. It does not. `computePhase`
(`operators/internal/app/application_controller.go:387-404`) tests in strict order —
`Status.Reachable` first, then `Status.EndpointsReady`, and only then the conditions. So a
workload whose endpoints are ready and whose route is broken returns **`Serving`**, and the
`Degraded` branch is unreachable in that state.

**That is a finding about the product, not only about the drawing.** The developer whose app
is up and unreachable sees a phase that says it is serving. `PIECE-6` requires *"every failure
names what she must change"*, and this failure names nothing. Whether the fix is a new phase, a
condition surfaced separately, or `EndpointsReady` ceasing to outrank a failed route is a
product decision — recorded below as open rather than drawn as settled.

## Open — for the sign-off to resolve

- **An app that is up and unreachable reports `Serving`.** Measured above. **Registered as `B-229`.** `PIECE-6` promises
  a failure that names the fix, and this one is silent in the phase. Which of the three fixes
  is right was not decided.

- ~~**Where does build run?**~~ **ANSWERED — the customer's infrastructure.** It was never
  open: C1 of the extraction RFC states it in one line — *"Build plane — Included. The customer
  builds on their own infrastructure"* — and `REQ-11` makes it a network-capture criterion. Left
  struck because a question a drawing keeps open while a signed RFC answers it invites somebody to
  decide it again, differently.
- ~~**Is `loopzero up` the only entry?**~~ **ANSWERED 2026-09-11 — there is a second TRIGGER and it
  is not a second PATH.** A GitHub push webhook enters at `api/cmd/setup_github.go:181`
  (`wh.OnPush`), and `buildSubmitterAdapter.SubmitBuildFromWebhook`
  (`api/cmd/autodeploy_adapters.go:56`) calls `orchestrator.SubmitBuild` — **the same
  `BuildOrchestrator` the CLI drives**. Two triggers, one pipeline, so `REQ-1`'s *one route, no
  fork* holds. What a second trigger DOES change is who sees the failure: a push that fails
  builds has no terminal to print to, and `REQ-4` (*a failure names the fix*) has no audited
  surface there. That is narrower than the original question and is the part worth watching.

## Related

- [`trust.md`](trust.md) — D2, the boundary these calls cross
- [`durability.md`](durability.md) — D4, what survives a failed step
- [`sign-off.md`](sign-off.md) — D3 was answered on 2026-09-11: the build runs on the customer's
  infrastructure, decided by C1 of the extraction RFC
