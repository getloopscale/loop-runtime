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
    Op--xDev: Degraded — route does not resolve
    Op-->>Dev: Reachable
```

## Decided

- **One route, no fork.** Every failure returns to the developer naming what to change,
  rather than surfacing as a phase they have to interpret.
- **`Serving` is not the end.** The sequence continues to the route check, because an
  app that is up and unreachable is not delivered.

## Open — for the sign-off to resolve

- **Where does build run?** `PIECE-10` is a piece; whether it executes in the customer's
  cell or on platform infrastructure was not established. It decides whether a heavy
  build can starve a running agent.
- **Is `loopzero up` the only entry?** The drawing assumes one. A second path — a CI push,
  an API call — would break the no-fork requirement and is worth naming if it exists.

## Related

- [`trust.md`](trust.md) — D2, the boundary these calls cross
- [`durability.md`](durability.md) — D4, what survives a failed step
- [`sign-off.md`](sign-off.md) — D3 was answered on 2026-09-11: the build runs on the customer's
  infrastructure, decided by C1 of the extraction RFC
