---
type: Component
title: "loopzero-ops — the quality-gate binary"
description: "The CI substitute: a Go binary holding the repository quality gates plus seed, drill, pulumi and chaos subcommands."
resource: "cmd/loopzero-ops"
tags:
  - quality-gate
  - ci
  - go
  - tooling
sources:
  - id: to-cli
    resource: "cmd/loopzero-ops/main.go#L4-L19"
    note: "loopzero-ops binary"
  - id: to-q
    resource: "cmd/loopzero-ops/quality_registry.go#L36-L40"
    note: "quality gate registry"
  - id: to-seed
    resource: "cmd/loopzero-ops/internal/seed/secrets_seed_dev.go#L1"
    note: "seed"
  - id: to-leak
    resource: "cmd/loopzero-ops/internal/leak/edge.go#L1"
    note: "leak (public-edge probe)"
  - id: to-shell
    resource: "cmd/loopzero-ops/internal/shellrun/runner.go#L1"
    note: "shellrun"
  - id: to-pr
    resource: "cmd/loopzero-ops/internal/planregression/doc.go#L1"
    note: "planregression"
  - id: to-gates
    resource: "cmd/loopzero-ops/quality_registry.go#L36"
    note: "CLAUDE.md reports 145 registered / 126 wired gates; the oracle it names reports 146 registered, 127 task-attached"
  - id: to-todo
    resource: "cmd/loopzero-ops/leak_subcmd.go#L100"
    note: "36% of first-party TODO hits are the Portuguese word for \"every\", so the TODO marker cannot be used as a debt signal here"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

GitHub Actions was removed as the pipeline in 2026-05-23. What replaced it is this binary, run
from a pre-push hook **on the pusher's machine and only there**. `ci.yml`'s own header says so.
The consequence is stated plainly rather than implied: a repository with no GitHub gate has none
for anyone else.

By line count `cmd/loopzero-ops` is the second-largest directory in the repository at **103,011
lines** — larger than the operators, the charts and the helmfile layer combined.

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `ops.cli` | `cmd/loopzero-ops` | cli | entrypoint | `cmd/loopzero-ops/main.go:4-19` | Subcommands: quality, pulumi, drill, chaos, seed (plus leak, per the leak package). Exit codes 0/1/2/3/4/5 are declared PERMANENT in the doc comment. |
| `ops.quality` | `cmd/loopzero-ops/internal/quality` | package | application | `cmd/loopzero-ops/quality_registry.go:36-40` | 150 gate sub-directories under internal/quality plus thematic dispatch files quality_gates_*.go at the module root. The registry is one slice of {Name, Summary, Run}; a build-time parity test keeps --list honest about which gates exist. |
| `ops.seed` | `cmd/loopzero-ops/internal/seed` | package | application | `cmd/loopzero-ops/internal/seed/secrets_seed_dev.go:1` | 27 files / 10,854 lines. Seeds Hydra/Kratos clients, ArgoCD and Gitea tokens, MinIO, the secret broker, cross-cluster Loki and CoreDNS. |
| `ops.leak` | `cmd/loopzero-ops/internal/leak` | package | application | `cmd/loopzero-ops/internal/leak/edge.go:1` | Live HTTP probe asserting every public edge route answers 401/302 without a credential. Deliberately NOT a quality gate: the doc says every gate is hermetic by contract and this one needs a live target. Doc comment is written in Portuguese. |
| `ops.shellrun` | `cmd/loopzero-ops/internal/shellrun` | package | application | `cmd/loopzero-ops/internal/shellrun/runner.go:1` | Process runner with unix/windows variants, a mock and signal handling — the one sanctioned way loopzero-ops shells out. |
| `ops.planregression` | `cmd/loopzero-ops/internal/planregression` | package | application | `cmd/loopzero-ops/internal/planregression/doc.go:1` | Sub-packages opencost, phase2, phase3. |

## Measured size

None

# Relationships

- `ops.quality` holds the gate registry. The oracle for what is actually wired is
  `/tmp/loopzero-ops quality --list`, never the stray `./loopzero-ops` at the repository root — that one
  is a gitignored local build artifact and can be arbitrarily stale.
- `ops.seed` is what populates a fresh standalone cluster; `ops.leak` scans for secret leakage;
  `ops.shellrun` is the guarded shell runner; `ops.planregression` locks plan-tier behaviour.
- The gate inventory, the test pyramid and the measured counts are in
  [`/operations/quality-gates-and-tests.md`](/operations/quality-gates-and-tests.md).
- Five gates are registered and deliberately **not wired**, each with a reason; they are tracked
  as work items in [`/operations/work-in-flight.md`](/operations/work-in-flight.md).

# Where the code and the documents disagree

`CLAUDE.md` reports **145 registered / 126 wired** quality gates, and names
`loopzero-ops quality --list` as the oracle. Run, the oracle reports **146 registered / 127
task-attached**.[^to-gates] The file has drifted by one in both columns — and it records, in its
own text, that this count has drifted three times before.

A second measurement worth carrying: **36% of first-party TODO hits are the Portuguese word
"todo"** ("every"), so a raw TODO count cannot be used as a debt signal in this
repository.[^to-todo]

# Evidence

[^to-cli]: `cmd/loopzero-ops/main.go:4-19`
[^to-q]: `cmd/loopzero-ops/quality_registry.go:36-40`
[^to-seed]: `cmd/loopzero-ops/internal/seed/secrets_seed_dev.go:1`
[^to-leak]: `cmd/loopzero-ops/internal/leak/edge.go:1`
[^to-shell]: `cmd/loopzero-ops/internal/shellrun/runner.go:1`
[^to-pr]: `cmd/loopzero-ops/internal/planregression/doc.go:1`
[^to-gates]: `cmd/loopzero-ops/quality_registry.go:36`
[^to-todo]: `cmd/loopzero-ops/leak_subcmd.go:100`
