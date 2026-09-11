---
type: architecture-view
title: The paved road
measured_on: 2026-09-11
render: paved-road.html
---

# The paved road

Three trust boundaries and the routes between them, drawn to the `diagram-design` contract:
**8 nodes, not 85**. Interactive view: [`paved-road.html`](paved-road.html).

Semantic pattern: **secure paved road** — labelled trust boundaries, a privileged moment, an
approved deploy path per destination. Brand tokens extracted from `loopzero-ui/dist/tokens.css`
(accent `oklch(0.621 0.132 39)` → `#c86747`, Geist and Geist Mono).

## Why so few nodes

The other views answer *what exists*. This one answers **which routes cross which boundary**, and
that question has an answer a reader can hold in one look. The 59 releases, the 11 Terragrunt units
and the managed-service table are all in [`engine-and-delivery.md`](engine-and-delivery.md).

## What changed on 2026-09-11

The drawing had **two** destinations in the customer's account and one forbidden path. Self-host was
dropped, and both went:

- **The self-host cluster is gone.** It was the destination the blocked path did not reach.
- **The forbidden path is gone with it.** It carried a `401` and a stop symbol at the boundary; its
  premise was that a customer must fetch our artifacts, and under BYOC nobody outside does.

What remains is the route that always worked: ArgoCD reaching the cell by `ClusterResourceSet`,
using the authority CAPI holds at **cell birth** — authority that does not survive the bootstrap.

**And the configuration that remains has no precedent** —
see [`../decisions/byoc-as-the-only-mode-has-no-precedent.md`](../decisions/byoc-as-the-only-mode-has-no-precedent.md).

## Related

- [`engine-and-delivery.md`](engine-and-delivery.md) — the same ground at component level
- [`as-is-to-be.md`](as-is-to-be.md) — what changes, concern by concern
