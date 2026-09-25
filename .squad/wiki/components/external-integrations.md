---
type: Component
title: "Outbound integrations — Cloudflare, GitHub, Loki, tracing"
description: "The six client packages that leave the cluster, each wrapping one third-party API."
resource: "api/internal"
tags:
  - cloudflare
  - github
  - loki
  - tracing
sources:
  - id: ei-cfkv
    resource: "api/internal/builds/cfkv/client.go#L3"
    note: "cfkv"
  - id: ei-cfzone
    resource: "api/internal/domains/cfzone/types.go#L1"
    note: "cfzone"
  - id: ei-gh
    resource: "api/internal/github/appclient.go#L3"
    note: "github"
  - id: ei-ghwh
    resource: "api/internal/github/webhook/handler.go#L3"
    note: "github/webhook"
  - id: ei-loki
    resource: "api/internal/logs/loki/client.go#L3"
    note: "loki"
  - id: ei-trace
    resource: "api/internal/tracing/tracing.go#L3"
    note: "tracing"
  - id: ei-kv-write
    resource: "api/internal/builds/cfkv/client.go#L67"
    note: "HTTP PUT into a Cloudflare KV namespace. This is what lets the CF Worker serve a static app straight from DO Spaces with no cluster involvement, and it is writt"
  - id: ei-zone
    resource: "api/internal/domains/cfzone/client.go#L19"
    note: "cfAPIBase = https://api.cloudflare.com/client/v4"
  - id: ei-gh-call
    resource: "api/internal/github/appclient.go#L32"
    note: "AppClient targets DefaultGitHubAPIBase, overridable via GITHUB_API_BASE_URL"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

Six packages, each owning exactly one outbound surface. They are the only places in the API
module that speak a third-party protocol.

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `api.cfkv` | `api/internal/builds/cfkv` | package | infrastructure | `api/internal/builds/cfkv/client.go:3` | Cloudflare Workers KV client for the static-site delivery path. |
| `api.cfzone` | `api/internal/domains/cfzone` | package | infrastructure | `api/internal/domains/cfzone/types.go:1` | Cloudflare Zone API client for custom apex domains. |
| `api.github` | `api/internal/github` | package | infrastructure | `api/internal/github/appclient.go:3` | GitHub App client: source tarball download, commit status, PR comments, auto-deploy and preview-deploy drivers. |
| `api.github.webhook` | `api/internal/github/webhook` | package | interface | `api/internal/github/webhook/handler.go:3` | Inbound webhook receiver — an entry point into the system from outside, HMAC-verified. |
| `api.loki` | `api/internal/logs/loki` | package | infrastructure | `api/internal/logs/loki/client.go:3` | Loki query client; the source of build output shown by the logs route. |
| `api.tracing` | `api/internal/tracing` | package | infrastructure | `api/internal/tracing/tracing.go:3` | OTLP/gRPC tracer bootstrap: 1s export timeout, retry disabled, bounded queue 2048, flag LOOPZERO_TRACING_ENABLED. |

## Measured size

| Component | loc | file count | afferent coupling | efferent coupling | instability | commits | commits last 90d |
|---|---|---|---|---|---|---|---|
| `api.cfkv` | 823 | 3 | 2 | 1 | 0.3333 | 9 | 3 |
| `api.cfzone` | 1015 | 3 | 1 | 0 | 0 | 6 | 1 |
| `api.github` | 3367 | 9 | 2 | 5 | 0.7143 | 35 | 12 |
| `api.github.webhook` | 771 | 4 | 2 | 1 | 0.3333 | 11 | 5 |
| `api.loki` | 461 | 2 | 1 | 2 | 0.6667 | 13 | 6 |
| `api.tracing` | 337 | 3 | 1 | 0 | 0 | 4 | 0 |

# Relationships

- `api.cfkv` **writes** routing entries into a Cloudflare KV namespace by HTTP PUT. This is what
  lets the Cloudflare Worker serve a static app's new version, and it happens **before**
  container activation so the static half is never behind.[^ei-kv-write]
- `api.cfzone` **calls** the Cloudflare zone API — the v2 custom-domain path works by
  nameserver delegation, so the API hands back CF nameservers and the domain sits in
  `pending_ns` until the user changes them at their registrar.[^ei-zone] That handoff is trap
  three of [`/flows/developer-first-app-live.md`](/flows/developer-first-app-live.md), and the
  states are [`/entities/state-machine-domain.md`](/entities/state-machine-domain.md).
- `api.github` **calls** the GitHub App API at a base URL overridable by
  `GITHUB_API_BASE_URL`.[^ei-gh-call] `api.github.webhook` is the inbound half, feeding the
  auto-deploy path.
- `api.loki` **reads** build logs from the Loki instance on the build plane — see
  [`/components/platform-datastores.md`](/components/platform-datastores.md).
- The third-party systems themselves are catalogued in
  [`/components/external-dependencies.md`](/components/external-dependencies.md).

# Evidence

[^ei-cfkv]: `api/internal/builds/cfkv/client.go:3`
[^ei-cfzone]: `api/internal/domains/cfzone/types.go:1`
[^ei-gh]: `api/internal/github/appclient.go:3`
[^ei-ghwh]: `api/internal/github/webhook/handler.go:3`
[^ei-loki]: `api/internal/logs/loki/client.go:3`
[^ei-trace]: `api/internal/tracing/tracing.go:3`
[^ei-kv-write]: `api/internal/builds/cfkv/client.go:67`
[^ei-zone]: `api/internal/domains/cfzone/client.go:19`
[^ei-gh-call]: `api/internal/github/appclient.go:32`
