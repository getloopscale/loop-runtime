---
type: Component
title: "Control-plane API server"
description: "The chi-routed Go HTTP server that is the only interface the CLI and dashboard talk to."
resource: "api/cmd"
tags:
  - api
  - http
  - entrypoint
  - go
sources:
  - id: api-cmd
    resource: "api/cmd/main.go#L3-L4"
    note: "loopzero-api server binary"
  - id: api-server
    resource: "api/internal/server/router.go#L3"
    note: "server"
  - id: api-routes
    resource: "api/internal/httpio/writer.go#L1"
    note: "routes (HTTP surface)"
  - id: api-mw
    resource: "api/internal/middleware/auth/auth.go#L1"
    note: "middleware"
  - id: api-cfg
    resource: "api/internal/config/config.go#L3"
    note: "config"
  - id: api-val
    resource: "api/internal/validation/path.go#L3"
    note: "validation"
  - id: api-corr
    resource: "api/internal/correlation/reader.go#L3"
    note: "correlation (header reader)"
  - id: api-stale-corr
    resource: "api/internal/correlation/reader.go#L18"
    note: "api/internal/correlation cites pkg/quality/correlationheader; pkg/quality does not exist, and the regression-lock that forbids the citation only scans cmd/loopzero-"
  - id: api-routes-bypass
    resource: "api/internal/secrets/secretshttp/secrets.go#L41"
    note: "routes/secrets and routes/billing reach persistence directly, unlike the other 27 route packages"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

A single Go binary (`api/cmd`) that owns every user-facing operation. It is the **only**
interface: the CLI and dashboard never touch Kubernetes, Gitea, ArgoCD or Postgres directly.
Requests enter a chi router, pass an idempotency layer and two plan-enforcement caps, and reach
one of the route packages.[^api-server]

`api/internal/routes` holds **112 files** across 29 route packages — by file count the largest
single directory in the repository.[^api-routes]

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `api.cmd.server` | `api/cmd` | service | entrypoint | `api/cmd/main.go:3-4` | Composition root of the HTTP API. 25 non-test files, almost all named setup_*.go: each wires one concern (auth, database, k8s, github, orchestrator, transit, workflows, server) and returns a *Deps struct. It imports 21 routes/ packages and constructs every adapter, so the interface-to-infrastructure binding happens here and nowhere else. |
| `api.server` | `api/internal/server` | package | interface | `api/internal/server/router.go:3` | chi router assembly. The widest importer in the module: go list shows it importing 25 routes/ packages plus all 5 middleware packages. |
| `api.routes` | `api/internal/routes` | package | — | `api/internal/httpio/writer.go:1` | Directory grouping with NO Go files of its own; 29 sub-packages, one per resource group. Verified: `ls api/internal/routes/*.go` matches nothing and go list reports no package at that path. |
| `api.middleware` | `api/internal/middleware` | package | — | `api/internal/middleware/auth/auth.go:1` | Directory grouping with NO Go files of its own; 5 sub-packages split by WHAT they decide: auth (who is calling), policy (per-request rules), ratelimit (windowed quotas), audit, observability. |
| `api.contract` | `api/internal/contract` | package | interface | `api/internal/contract/engine_conformance_test.go:1` | A package with ZERO non-test Go files and 733 lines of tests, asserting that the OpenAPI spec, the CLI consumer and the dashboard consumer agree with the router. Cartographically a contract check, not a runtime component. |
| `api.correlation` | `api/internal/correlation` | package | interface | `api/internal/correlation/reader.go:3` | HTTP header reader, distinct from pkg/correlation (the context key). Deliberately placed outside the directory a quality gate scans so the backward-compat fallback is allowed exactly once. |
| `api.config` | `api/internal/config` | package | foundation | `api/internal/config/config.go:3` | One 1825-line file holding every env-derived setting. Zero in-repo imports. |
| `api.validation` | `api/internal/validation` | package | domain | `api/internal/validation/path.go:3` | Path/field validators. Zero in-repo imports. |

## Measured size

| Component | loc | file count | afferent coupling | efferent coupling | instability | commits | commits last 90d |
|---|---|---|---|---|---|---|---|
| `api.cmd.server` | 6174 | 33 | 0 | 60 | 1 | 249 | 107 |
| `api.server` | 1697 | 7 | 1 | 10 | 0.9091 | 120 | 54 |
| `api.correlation` | 169 | 2 | 1 | 0 | 0 | 6 | 1 |
| `api.config` | 1825 | 3 | 2 | 0 | 0 | 53 | 5 |
| `api.validation` | 285 | 2 | 1 | 0 | 0 | 4 | 1 |

Note that `api.routes`, `api.middleware` and `api.services` carry **no metric row**. Phase 3
recorded them as container nodes and measured their children instead; collapsing a tree into
one node is what manufactured a false dependency cycle in an earlier pass, recorded as a
cartography note rather than a defect in the code.

# Relationships

- `api.cmd.server` is the composition root: it has **efferent coupling 56 and afferent coupling
  0** (instability 1.0), which is exactly the shape a `main` package should have — it wires
  everything and nothing wires it.
- `api.server` dispatches into `api.routes`, which is step 1 of
  [`/flows/deploy-container-app.md`](/flows/deploy-container-app.md).
- The route packages call the orchestrators in
  [`/components/build-orchestration.md`](/components/build-orchestration.md) and
  [`/components/release-delivery.md`](/components/release-delivery.md); they are not supposed to
  reach persistence themselves.
- The twelve user-visible capabilities this surface exposes are enumerated in
  [`/entities/api-capabilities.md`](/entities/api-capabilities.md).
- `api.validation` is called at the boundary before any app value reaches a build script —
  `ValidateAppName` / `ValidateAppPath` reject path traversal and shell injection.[^api-val]

## Two route packages break the layering

`routes/secrets` and `routes/billing` reach persistence **directly**, unlike the other 27 route
packages which go through a service.[^api-routes-bypass] The map records this as an
undocumented boundary rather than as a bug: it may be deliberate, but nothing in the repository
says so.

# Where the code and the documents disagree

- `CLAUDE.md` says `api/internal/` holds **39 subpackages** and that `routes` holds **112
  files**. Both measured correct.
- `api/internal/correlation` cites `pkg/quality/correlationheader` in its own doc comment.
  **`pkg/quality` does not exist** — and the regression-lock that forbids exactly this citation
  only scans `cmd/loopzero-ops`, so it never sees this file.[^api-stale-corr]

# Evidence

[^api-cmd]: `api/cmd/main.go:3-4`
[^api-server]: `api/internal/server/router.go:3`
[^api-routes]: `api/internal/httpio/writer.go:1`
[^api-mw]: `api/internal/middleware/auth/auth.go:1`
[^api-cfg]: `api/internal/config/config.go:3`
[^api-val]: `api/internal/validation/path.go:3`
[^api-corr]: `api/internal/correlation/reader.go:3`
[^api-stale-corr]: `api/internal/correlation/reader.go:18`
[^api-routes-bypass]: `api/internal/secrets/secretshttp/secrets.go:41`
