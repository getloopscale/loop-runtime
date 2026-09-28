---
type: Component
title: "Go workspace and its nine modules"
description: "The go.work union of nine Go modules, one of which is a sibling repository checked out beside this one."
resource: "go.work"
tags:
  - go
  - module
  - workspace
  - build
sources:
  - id: ws-gowork
    resource: "go.work#L5-L15"
    note: "LoopZero engine (this repository)"
  - id: ws-api
    resource: "api/go.mod#L1"
    note: "API module (github.com/getloopscale/loopzero/api)"
  - id: ws-pkg
    resource: "pkg/go.mod#L1"
    note: "Shared Go libraries (github.com/getloopscale/loopzero/pkg)"
  - id: ws-ops
    resource: "cmd/loopzero-ops/go.mod#L1"
    note: "loopzero-ops module (github.com/getloopscale/loopzero/cmd/loopzero-ops)"
  - id: ws-contracts
    resource: "api/go.mod#L91"
    note: "loopzero-contracts (sibling repo)"
  - id: ws-replace
    resource: "api/go.mod#L121"
    note: "go.mod replace directive resolving to a local path"
  - id: ws-sibling
    resource: "go.work#L6"
    note: "The Go workspace compiles only because a sibling repository is checked out next to this one"
  - id: ws-tiltfile
    resource: "Tiltfile#L35"
    note: "The Tiltfile still sends developers to getloopscale/loopzero-contracts@v0.1.0 — the org the repo left in August, and a version three minors behind"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

`go.work` unions **nine** Go modules. Eight live in this repository; the ninth,
`../loopzero-contracts`, is a **separate repository** that must be checked out as a sibling
directory for the workspace to resolve at all.[^ws-gowork][^ws-sibling]

There is no `go.mod` at the repository root. `go build ./...` or `go test ./...` from the
root therefore covers **none** of the code — each module is built and tested on its own, or
through the Taskfile. A reviewer who runs a root-level build and reports "builds clean" has
measured nothing.

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `loopzero` | `.` | other | — | `go.work:5-15` | Repository scope. Go multi-module workspace (8 in-repo modules + 1 sibling unioned by go.work) plus Helm charts, Helmfile, k3d and Pulumi IaC. |
| `mod.api` | `api` | service | — | `api/go.mod:1` | HTTP control API plus a separate controller binary. 349 non-test .go files under api/internal across 39 subpackages. |
| `mod.pkg` | `pkg` | library | — | `pkg/go.mod:1` | 12 shared packages imported by api, operators and cmd/loopzero-ops. |
| `mod.operators` | `operators` | service | — | `operators/go.mod:1` | kubebuilder/controller-runtime reconcilers, built into 4 separate binaries. |
| `mod.operators.api` | `operators/api` | library | — | `operators/api/go.mod:1` | Own module whose only non-test deps are apimachinery and controller-runtime, so consumers can import CRD types without the reconciler tree. |
| `mod.ops` | `cmd/loopzero-ops` | cli | — | `cmd/loopzero-ops/go.mod:1` | Ops + quality-gate binary. 235 non-test .go files. |
| `mod.infra.scripts` | `infra/scripts` | library | — | `infra/scripts/go.mod:1` | Seed/ops helpers in Go; 4 non-test files, and every require in its go.mod is marked // indirect. |
| `mod.infra.tests` | `infra/tests` | module | — | `infra/tests/go.mod:1` | Ginkgo e2e / chaos / load / parity suites. Requires api, operators, pkg and infra/scripts via local replace directives. |
| `mod.tools.genauth` | `tools/gen-service-auth-ed25519` | cli | — | `tools/gen-service-auth-ed25519/go.mod:1` | Single-file key generation tool. go.mod declares zero require entries. |
| `ext.contracts` | `../loopzero-contracts` | external | — | `api/go.mod:91` | Shared contract module (jwt, serviceauth, plan packages) at v0.4.0, built from a SEPARATE repository. go.work unions it, so it compiles from source rather than being downloaded. |

## Module metrics

| Component | loc | file count | afferent coupling | efferent coupling | instability | commits | commits last 90d |
|---|---|---|---|---|---|---|---|
| `mod.api` | 652 | 5 | 1 | 3 | 0.75 | 166 | 412 |
| `mod.pkg` | 122 | 2 | 4 | 1 | 0.2 | 28 | 74 |
| `mod.operators` | 738 | 9 | 1 | 2 | 0.6667 | 87 | 67 |
| `mod.operators.api` | 102 | 2 | 2 | 0 | 0 | 20 | 13 |
| `mod.ops` | — | — | — | — | — | 7 | — |
| `mod.infra.scripts` | 111 | 3 | 1 | 0 | 0 | 22 | 150 |
| `mod.infra.tests` | 866 | 5 | 0 | 4 | 1 | 20 | 358 |
| `mod.tools.genauth` | — | — | — | — | — | 3 | — |

`instability` is efferent / (afferent + efferent) — 1.0 means the module depends on others and
nothing depends on it; 0.0 means the reverse.

# Relationships

- The workspace root `loopzero` **contains** the eight in-repo modules plus the five cluster zones
  and the three infrastructure trees — 17 direct children in all, which is the widest
  containment edge in the map.
- `mod.api` and `mod.pkg` both **depend on** the sibling `../loopzero-contracts` at `v0.4.0`; the
  `plan` package is re-exported through [`/components/shared-go-libraries.md`](/components/shared-go-libraries.md)
  as type aliases rather than copied.[^ws-contracts]
- `mod.api`, `mod.operators`, `mod.ops` and `mod.infra.tests` reach the in-repo modules through
  `replace` directives resolving to **local paths**, not to published versions.[^ws-replace]
- `mod.infra.tests` is the only module that depends on four others at once — it is the e2e
  harness and imports the API, the operators, the shared libraries and the infra scripts.
- The API module's own packages are documented at
  [`/components/control-plane-api.md`](/components/control-plane-api.md); the operator module's at
  [`/components/operators.md`](/components/operators.md); the ops binary at
  [`/components/loopzero-ops.md`](/components/loopzero-ops.md).

# Where the code and the documents disagree

`CLAUDE.md` states the workspace unions nine modules including the sibling repository, and lists
them. **Measured, that is correct** — the count, the names and the sibling dependency all hold.
What the file does not say, and what the map records as a boundary finding, is that the build is
*silently* coupled to a checkout outside the repository: nothing in this tree fails with a
message naming `../loopzero-contracts` when it is absent.[^ws-sibling]

**And the one place that tries to help a blocked developer sends them somewhere that no longer
exists.** `Tiltfile:35` tells them to generate a token with `Contents: read` on
`getloopscale/loopzero-contracts`, and `Tiltfile:183` documents the dependency as
`github.com/getloopscale/loopzero-contracts@v0.1.0`. Both `go.mod` files that actually require the
module say **`github.com/getloopscale/loopzero-contracts v0.4.0`** — a different organisation and three
minor versions on.[^ws-tiltfile]

Neither Tiltfile line is executable; they are an error message and a comment, so nothing breaks
at build time. What breaks is the reader — who is, by construction, already blocked, and who
will go and create a token on a repository that is not there. The same drift in `ci.yml` blocked
every pipeline run from 2026-07-31 to 2026-08-07; the repair landed in `b919cc135` and **did not
reach the Tiltfile**, which `CLAUDE.md` names as the canonical day-to-day loop.

# Evidence

[^ws-gowork]: `go.work:5-15`
[^ws-api]: `api/go.mod:1`
[^ws-pkg]: `pkg/go.mod:1`
[^ws-ops]: `cmd/loopzero-ops/go.mod:1`
[^ws-contracts]: `api/go.mod:91`
[^ws-replace]: `api/go.mod:121`
[^ws-sibling]: `go.work:6`
[^ws-tiltfile]: `Tiltfile:35`
