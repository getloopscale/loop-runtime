---
type: Component
title: "Shared Go libraries (pkg/)"
description: "Thirteen packages every module depends on: the state machines, typed errors, naming, domain refs, enforcement and the OpenBao client."
resource: "pkg"
tags:
  - go
  - library
  - state-machine
  - errors
sources:
  - id: sl-sm
    resource: "pkg/statemachine/machine.go#L3"
    note: "statemachine"
  - id: sl-enf
    resource: "pkg/enforcement/limiter.go#L3"
    note: "enforcement"
  - id: sl-err
    resource: "pkg/errors/errors.go#L3"
    note: "errors"
  - id: sl-nam
    resource: "pkg/naming/naming.go#L9"
    note: "naming"
  - id: sl-vault
    resource: "pkg/vault/doc.go#L3"
    note: "vault"
  - id: sl-dom
    resource: "pkg/domain/constants.go#L1"
    note: "domain"
  - id: sl-cb
    resource: "pkg/circuitbreaker/circuitbreaker.go#L3"
    note: "circuitbreaker"
  - id: sl-contracts
    resource: "pkg/enforcement/cost_cap.go#L7"
    note: "\"github.com/getloopscale/loopzero-contracts/plan\" — Plan, CostMetric, CapFor are type aliases re-exported from the sibling repo"
  - id: sl-bao
    resource: "pkg/vault/client.go#L1"
    note: "OpenBao SDK client plus the hand-rolled net/http transit client; reaches the separate secrets cluster"
  - id: sl-layers
    resource: "pkg/enforcement/limiter.go#L5"
    note: "pkg/enforcement is the one shared library that spans three layers"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

`pkg/` is the module three others `replace` to a local path. Its defining property is measured
rather than asserted: of the eleven packages that carry a coupling measurement, **seven have
efferent coupling 0** — they depend on nothing inside the workspace, which is what makes them
safe to share. `pkg.id` and `pkg.vault` carry no coupling measurement at all, so they are neither
in that set nor excluded from it.

`pkg/statemachine` is the largest at 6,951 lines over 48 files, and it holds all eleven state
machines the platform runs on.[^sl-sm]

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `pkg.circuitbreaker` | `pkg/circuitbreaker` | package | foundation | `pkg/circuitbreaker/circuitbreaker.go:3` | Per-process generic breaker with a generic Execute[T]. State is per-replica by design. Zero in-repo imports. |
| `pkg.conditions` | `pkg/conditions` | package | foundation | `pkg/conditions/conditions.go:3` | kstatus-shaped Condition mirroring metav1.Condition WITHOUT importing apimachinery, so pkg stays free of Kubernetes types; operators adapt 1:1. |
| `pkg.correlation` | `pkg/correlation` | package | foundation | `pkg/correlation/id.go:3` | One context key for the correlation id, shared so api and operators cannot diverge on the ctxKey type. Imports context + google/uuid only. |
| `pkg.domain` | `pkg/domain` | package | foundation | `pkg/domain/constants.go:1` | Product vocabulary: DefaultPermanentEnv, PlanTier, EnvRef value object. Imports only pkg/naming — no infrastructure. |
| `pkg.enforcement` | `pkg/enforcement` | package | domain | `pkg/enforcement/limiter.go:3` | Plan-limit and cost-cap rules. LAYER IS MIXED, verified by imports: the rules are domain logic, but store.go imports database/sql for the default sqlLimitsStore and middleware.go imports net/http. A LimitsStore interface (ADR-0033) provides the DIP seam. |
| `pkg.errors` | `pkg/errors` | package | foundation | `pkg/errors/errors.go:3` | LoopZeroError with typed codes and stack capture. Deliberately transport-agnostic: go list shows zero imports outside fmt/regexp/runtime/strings. |
| `pkg.errors.http` | `pkg/errors/http` | package | interface | `pkg/errors/http/mapping.go:3` | Transport adapter over pkg/errors. Imports net/http and pkg/errors; nothing in pkg/errors imports back — the dependency runs outward only. |
| `pkg.healthcheck` | `pkg/healthcheck` | package | interface | `pkg/healthcheck/probe.go:3` | Turns each operator binary into its own container HEALTHCHECK via a --healthcheck-probe flag, because the distroless base image has no shell or curl. Imports net/http and calls os.Exit. |
| `pkg.id` | `pkg/id` | package | foundation | `pkg/id/id.go:3` | build-/rel-/prv- prefixed ids from crypto/rand plus their validators. Zero imports beyond stdlib. |
| `pkg.logging` | `pkg/logging` | package | foundation | `pkg/logging/logger.go:3` | slog JSON logger factory plus a ContextLogger that pulls trace/span through a TraceExtractor func — the tracing dependency is inverted, so logging imports no OTel. |
| `pkg.naming` | `pkg/naming` | package | foundation | `pkg/naming/naming.go:9` | Deterministic RFC-1123 names for every structural resource (namespace, repo, hostname, ArgoCD app, cert secret, image tag). go list shows it imports only fmt/regexp/strings — zero in-repo and zero third-party imports. |
| `pkg.statemachine` | `pkg/statemachine` | package | foundation | `pkg/statemachine/machine.go:3` | Generic Machine plus 11 concrete machines, each with a *_typed.go companion. Zero imports outside fmt/time/errors/sort/strings. |
| `pkg.vault` | `pkg/vault` | package | infrastructure | `pkg/vault/doc.go:3` | OpenBao client: SDK wrapper for KV/auth, plus a hand-rolled net/http transit client (deliberately NOT the SDK, to keep singleflight + SPKI pinning narrow), a caching layer, log redaction and Zeroize. |

## Measured size and direction

| Component | loc | file count | afferent coupling | efferent coupling | instability | commits | commits last 90d |
|---|---|---|---|---|---|---|---|
| `pkg.circuitbreaker` | 552 | 3 | 5 | 0 | 0 | 7 | 5 |
| `pkg.conditions` | 838 | 7 | 1 | 0 | 0 | 1 | 0 |
| `pkg.correlation` | 151 | 3 | 9 | 0 | 0 | 2 | 0 |
| `pkg.domain` | 738 | 7 | 9 | 1 | 0.1 | 6 | 5 |
| `pkg.enforcement` | 3512 | 19 | 6 | 2 | 0.25 | 25 | 16 |
| `pkg.errors` | 1879 | 3 | 45 | 0 | 0 | 36 | 13 |
| `pkg.errors.http` | 579 | 4 | 3 | 1 | 0.25 | 10 | 8 |
| `pkg.healthcheck` | 334 | 2 | 4 | 0 | 0 | 2 | 0 |
| `pkg.logging` | 532 | 4 | 4 | 1 | 0.2 | 11 | 3 |
| `pkg.naming` | 1760 | 10 | 16 | 0 | 0 | 14 | 6 |
| `pkg.statemachine` | 6951 | 48 | 15 | 0 | 0 | 46 | 7 |

# Relationships

- `pkg.statemachine` is imported by the API, the operators and the ops binary; its eleven
  machines and 80 states are documented one per machine, starting at
  [`/entities/state-machine-build.md`](/entities/state-machine-build.md).
- `pkg.enforcement` **imports** the sibling `loopzero-contracts` repository and re-exports `Plan`,
  `CostMetric` and `CapFor` as **type aliases** rather than copies — the one boundary-crossing
  import in the whole package graph.[^sl-contracts]
- It also reads `cost_counters` to answer "may this build run?", and that read is deliberately
  fail-open — see [`/entities/metering-ledger.md`](/entities/metering-ledger.md).
- `pkg.vault` **connects to** OpenBao in the separate secrets cluster, with both the SDK client
  and a hand-rolled `net/http` transit client.[^sl-bao]
- `pkg.naming` is what resolves an ArgoCD application name during delivery; `pkg.domain` is what
  computes the canonical namespace `loopzero-<tenant>-<project>-<env>` in step 8 of
  [`/flows/environment-reconcile-loop.md`](/flows/environment-reconcile-loop.md).
- `pkg.errors` is the `LoopZeroError` type — `errors.New` is not used for anything a caller must
  distinguish.

## The one library that spans three layers

`pkg.enforcement` is recorded as the single shared library whose contents sit across three
architectural layers at once.[^sl-layers] Everything else in `pkg/` stays in one.

# Evidence

[^sl-sm]: `pkg/statemachine/machine.go:3`
[^sl-enf]: `pkg/enforcement/limiter.go:3`
[^sl-err]: `pkg/errors/errors.go:3`
[^sl-nam]: `pkg/naming/naming.go:9`
[^sl-vault]: `pkg/vault/doc.go:3`
[^sl-dom]: `pkg/domain/constants.go:1`
[^sl-cb]: `pkg/circuitbreaker/circuitbreaker.go:3`
[^sl-contracts]: `pkg/enforcement/cost_cap.go:7`
[^sl-bao]: `pkg/vault/client.go:1`
[^sl-layers]: `pkg/enforcement/limiter.go:5`
