---
type: Component
title: "Server-sent events — the table is the bus"
description: "Every deploy event the CLI sees is a Postgres row; the stream handler polls forward from a cursor. There is no broker."
resource: "api/internal/sse"
tags:
  - sse
  - events
  - postgres
sources:
  - id: ev-sse
    resource: "api/internal/sse/stream_handler.go#L3"
    note: "sse"
  - id: ev-events
    resource: "api/internal/events/deployment.go#L3"
    note: "events"
  - id: ev-ctrl
    resource: "api/cmd/controller/main.go#L1"
    note: "loopzero-api controller binary"
  - id: ev-pub
    resource: "api/internal/sse/event_store.go#L69"
    note: "INSERT INTO sse_events (build_id, event_type, data) — every deploy event the CLI sees (build.started, build.succeeded, deploy.verifying, deploy.completed, rollo"
  - id: ev-sub
    resource: "api/internal/sse/event_store.go#L114"
    note: "SELECT ... FROM sse_events WHERE build_id = $1 AND id > $2 ORDER BY id ASC LIMIT 1000 — the stream handler polls forward from a cursor, which is how Last-Event-"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

The CLI blocks on one SSE channel from `POST /api/v1/builds` until `deploy.completed`. Behind
that channel there is **no message broker**: `sse_events` is an ordinary Postgres table, and the
table *is* the event bus.[^ev-pub]

The stream handler reads forward from a cursor —
`SELECT ... WHERE build_id = $1 AND id > $2 ORDER BY id ASC LIMIT 1000` — which is exactly what
makes `Last-Event-ID` resume work.[^ev-sub]

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `api.sse` | `api/internal/sse` | package | interface | `api/internal/sse/stream_handler.go:3` | SSE hub, Postgres-backed event store and pruner. The only --json contract observable mid-flight. |
| `api.events` | `api/internal/events` | package | domain | `api/internal/events/deployment.go:3` | SSE payload schemas. Its own doc records that the TypeScript mirror in the dashboard is kept in sync by hand: 'There is no codegen today; the contract is honor-system.' |
| `api.cmd.controller` | `api/cmd/controller` | service | entrypoint | `api/cmd/controller/main.go:1` | Second binary built from the same module. Runs the leader-elected background loops; go list shows it importing internal/leader, internal/metering, services/build, services/platform, services/release and operators/api/v1 — a set the server binary does not import. |

## Measured size

| Component | loc | file count | afferent coupling | efferent coupling | instability | commits | commits last 90d |
|---|---|---|---|---|---|---|---|
| `api.sse` | 2666 | 6 | 6 | 6 | 0.5 | 20 | 5 |
| `api.events` | 413 | 5 | 2 | 0 | 0 | 1 | 1 |
| `api.cmd.controller` | 1421 | 8 | 0 | 18 | 1 | 32 | 15 |

## Event types carried

`build.started` · `build.succeeded` · `build.failed` · `deploy.verifying` ·
`deploy.completed` · `rollout.step` · `rollout.aborted`[^ev-pub]

# Relationships

- `api.sse` **publishes** into and **subscribes** from `sse_events` — both directions are the
  same table.[^ev-pub][^ev-sub]
- `deploy.completed` is emitted with a 3× retry and 100 ms backoff precisely because the CLI
  hangs until timeout if it is dropped — step 39 of
  [`/flows/deploy-container-app.md`](/flows/deploy-container-app.md).
- The developer-facing consequence of an asynchronous 202 plus one stream is trap two of
  [`/flows/developer-first-app-live.md`](/flows/developer-first-app-live.md).
- `api.cmd.controller` is the second binary in the API module — the reconcile-loop host, with
  efferent coupling 16 and afferent coupling 0, the same composition-root shape as the server.

# Evidence

[^ev-sse]: `api/internal/sse/stream_handler.go:3`
[^ev-events]: `api/internal/events/deployment.go:3`
[^ev-ctrl]: `api/cmd/controller/main.go:1`
[^ev-pub]: `api/internal/sse/event_store.go:69`
[^ev-sub]: `api/internal/sse/event_store.go:114`
