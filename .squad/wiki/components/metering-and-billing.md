---
type: Component
title: "Metering, aggregation and usage reporting"
description: "Bills the reservation rather than live usage: sums pod requests per cell, advances a guarded cursor, and threads one bounded quantity into the external feed."
resource: "api/internal/metering"
tags:
  - metering
  - billing
  - usage
  - kubernetes
sources:
  - id: me-met
    resource: "api/internal/metering/metering_loop.go#L3"
    note: "metering"
  - id: me-agg
    resource: "api/internal/timeline/aggregator/source_reader.go#L3"
    note: "aggregator"
  - id: me-proc
    resource: "api/internal/processing/claim_store.go#L3"
    note: "processing"
  - id: me-pay
    resource: "api/internal/payloadlog/logger.go#L3"
    note: "payloadlog"
  - id: me-scrub
    resource: "api/internal/scrub/scrub.go#L3"
    note: "scrub"
  - id: me-metrics
    resource: "api/internal/metrics/metrics.go#L3"
    note: "metrics"
  - id: me-pods
    resource: "api/internal/metering/collect.go#L39"
    note: "ListPods across a tenant's namespaces on each cell, to sum the reserved CPU that billing is computed from. A read failure here must produce a typed error and ne"
  - id: me-capture
    resource: "api/internal/metering/collect.go#L34"
    note: "Pod resource requests (live, per cell)"
  - id: me-cursor
    resource: "api/internal/db/schema.gen.sql#L624"
    note: "metering_cursor"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

The metering worker reads **pod resource requests** live from each cell's Kubernetes API and
never stores them — the platform bills the *reservation*, not observed consumption.[^me-pods][^me-capture]
It uses `component-helpers/resource.PodRequests` so the init/sidecar arithmetic
`max(max(init), sum(regular) + sum(native-sidecar))` is the upstream one rather than hand-rolled.

The trust rule is stated in the code: a list failure on **any** namespace short-circuits with a
typed error and zero totals, and the caller must not meter. A read failure is never swallowed
into `sum = 0`, because that would advance the cursor over an unmeasured interval and silently
under-bill.[^me-capture]

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `api.metering` | `api/internal/metering` | package | application | `api/internal/metering/metering_loop.go:3` | Leader-gated, single-flighted, panic-recovered, kill-switched usage collection loop. Reaches runtime cells through a per-cell client. |
| `api.aggregator` | `api/internal/timeline/aggregator` | package | application | `api/internal/timeline/aggregator/source_reader.go:3` | Per-deploy goroutine fanning in 5 source-reader adapters (ArgoCD app, Loki markers, release DB, URL probe, LoopZeroApplication informer) into one canonical 5-phase timeline. |
| `api.processing` | `api/internal/processing` | package | application | `api/internal/processing/claim_store.go:3` | Compare-and-swap claim on (processing_by, processing_since) replacing an in-process sync.Map, which stopped working once the controller became a separate binary. |
| `api.payloadlog` | `api/internal/payloadlog` | package | application | `api/internal/payloadlog/logger.go:3` | Dispatcher->worker payload logger. Its own doc states the production wiring is DORMANT, gated on a compliance requirement: the mechanism and its tests exist, the request-path caller does not. |
| `api.scrub` | `api/internal/scrub` | package | domain | `api/internal/scrub/scrub.go:3` | Regex denylist rewriting internal terms before they reach a client, applied at WRITE time so the SSE event store never holds an unscrubbed term. This is the mechanism behind the repository's Rule 3. |
| `api.metrics` | `api/internal/metrics` | package | infrastructure | `api/internal/metrics/metrics.go:3` | Prometheus collectors, the HTTP middleware that records them, and a surface inventory. Imported by 12 other api packages. |

## Measured size

| Component | loc | file count | afferent coupling | efferent coupling | instability | commits | commits last 90d |
|---|---|---|---|---|---|---|---|
| `api.metering` | 2201 | 16 | 1 | 2 | 0.6667 | 17 | 17 |
| `api.aggregator` | 2055 | 16 | 2 | 4 | 0.6667 | 13 | 13 |
| `api.processing` | 808 | 4 | 1 | 1 | 0.5 | 7 | 5 |
| `api.scrub` | 277 | 2 | 2 | 0 | 0 | 5 | 5 |
| `api.metrics` | 2713 | 18 | 16 | 0 | 0 | 45 | 17 |

# Relationships

- `api.metering` **reads** the Kubernetes API on every cell — `ListPods` across a tenant's
  namespaces.[^me-pods]
- It then offers the sum to the guarded cursor. The full four-tier promotion chain, with the
  five simultaneous predicates that hold or advance the cursor, is
  [`/entities/metering-ledger.md`](/entities/metering-ledger.md).[^me-cursor]
- `pkg.enforcement` in [`/components/shared-go-libraries.md`](/components/shared-go-libraries.md)
  reads `cost_counters` to decide whether a build is allowed, and that read path is deliberately
  **fail-open**.
- `api.scrub` and `api.payloadlog` sit on the request-recording path rather than the billing
  path; `api.metrics` is the Prometheus surface consumed by
  [`/components/platform-runtime-services.md`](/components/platform-runtime-services.md).
- Stripe itself is **not here** — checkout, webhooks and the subscription lifecycle were
  extracted to the sibling `../loopzero-cloud` repository. What remains is the two-route billing
  surface in [`/entities/api-capabilities.md`](/entities/api-capabilities.md) and the
  `deploy_events` outbox that repository drains.

# Evidence

[^me-met]: `api/internal/metering/metering_loop.go:3`
[^me-agg]: `api/internal/timeline/aggregator/source_reader.go:3`
[^me-proc]: `api/internal/processing/claim_store.go:3`
[^me-pay]: `api/internal/payloadlog/logger.go:3`
[^me-scrub]: `api/internal/scrub/scrub.go:3`
[^me-metrics]: `api/internal/metrics/metrics.go:3`
[^me-pods]: `api/internal/metering/collect.go:39`
[^me-capture]: `api/internal/metering/collect.go:34`
[^me-cursor]: `api/internal/db/schema.gen.sql:624`
