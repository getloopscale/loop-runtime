---
type: Operations
title: "Quality gates and the test pyramid"
description: "The in-repo gate binary that replaced CI, what it measures, and the three ways the map established there is no dependency cycle."
resource: "cmd/loopzero-ops"
tags:
  - testing
  - quality-gate
  - ci
  - verification
sources:
  - id: qg-gates
    resource: "cmd/loopzero-ops/quality_registry.go#L36"
    note: "CLAUDE.md reports 145 registered / 126 wired gates; the oracle it names reports 146 registered, 127 task-attached"
  - id: qg-pyramid
    resource: "infra/tests/go.mod#L1"
    note: "Cartography note: the test pyramid's e2e tier is Ginkgo-driven, so its test_count (71) is a floor, not a count"
  - id: qg-nocycle
    resource: "cmd/loopzero-ops/quality_registry.go#L36"
    note: "No dependency cycle found, by three independent methods"
  - id: qg-parity
    resource: "infra/quality/chart-pairs.txt#L24-L25"
    note: "Verified: the two-chart parity claim holds, and the declared pair list matches disk"
  - id: qg-ci
    resource: ".github/workflows/ci.yml#L203"
    note: "ci.yml's post-mortem comment cites commit 696fe5c62, which exists in no reachable commit; the commit it means is 8c9a1d953"
  - id: qg-rego
    resource: "policies/charts/security_context.rego#L1"
    note: "policies/charts/*.rego are Conftest policies that nothing invokes"
  - id: qg-ops
    resource: "cmd/loopzero-ops/quality_registry.go#L36-L40"
    note: "quality gate registry"
  - id: qg-denom
    resource: ".claude/settings.json#L103"
    note: "Read-deny glob written for the rendered artifact matches the Helm template, leaving 11 critical-role files unmapped"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

GitHub Actions stopped being the pipeline on 2026-05-23. What enforces quality now is a Go
binary run from a **pre-push hook, on the pusher's machine and only there** — `ci.yml`'s own
header states the consequence: a repository with no GitHub gate has none for anyone else.[^qg-ops]

# Schema

## The gate inventory

| Source | Registered | Wired |
|---|---|---|
| `CLAUDE.md` | 145 | 126 |
| `loopzero-ops quality --list` (the oracle `CLAUDE.md` names) | **146** | **127** task-attached (123 `gates`, 4 `diff`) |

19 registered gates are attached to no task.[^qg-gates] The drift is one in each column; the
pattern is the finding, and the file itself records that this count has drifted three times
before.

```bash
task quality:build            # build the gate binary to /tmp/loopzero-ops — do this FIRST
/tmp/loopzero-ops quality --list  # the inventory: NAME / WIRED / SUMMARY
/tmp/loopzero-ops quality <gate>  # run one gate
task quality:gates            # full-repo scan
task quality:diff             # PR-diff scoped
```

A stray `./loopzero-ops` at the repository root is a **gitignored local build artifact**, can be
arbitrarily stale, and is absent in a fresh clone. It is not the oracle.

## The test pyramid

| Tier | Test files | `func Test*` | Ginkgo blocks |
|---|---|---|---|
| unit | 903 | **6,767** | 426 |
| integration | 191 | 571 | 146 |
| e2e | 98 | 71 | 225 |

**Read the e2e row as a floor, not a count.**[^qg-pyramid] `test_count` counts top-level test
functions, which is exact for the unit tier and wrong for a Ginkgo suite: Ginkgo puts many specs
inside one Go test function, so counting functions counts harnesses. `ginkgo_block_count`
overcounts in the other direction, because `Describe` and `Context` group rather than assert.
The true e2e spec count lies between 71 and 225; `CLAUDE.md` names 132 for one suite. The
base-to-apex ratio is a shape, not a measurement of e2e coverage.

Two operational traps the repository records about running that suite:

- `go test -timeout 120m` alone does **not** extend the Ginkgo suite timeout, whose own default
  is 60 minutes. Pass `-args -ginkgo.timeout=2h` as well.
- If `LOOPZERO_API_URL` is unset the suite defaults to `localhost:3001`, the `BeforeSuite` health
  probe fails, and **all specs SKIP without a single FAIL** — a green run that executed nothing.

# A gate that reports 100% over a denominator that excludes the gap

The cartography run's own coverage gate reports **517/517 IaC and 219/219 schema files read** —
and 11 further files carrying those same critical roles were **never opened**, because a
Read-deny glob matched them. An excluded row carries `is_excluded=1` and therefore sits outside
the denominator, so the gate is arithmetically right and substantively
incomplete.[^qg-denom]

It is worth carrying here, beside this repository's own gate numbers, because it is the same
failure shape those gates are built to prevent: **a measurement whose scope silently excludes
the thing being measured reads as coverage.** The five deliberately unwired gates in
[`/operations/work-in-flight.md`](/operations/work-in-flight.md) are the honest version of the
same problem — one of them is skipped precisely because wiring it *"would read as coverage while
asserting nothing"*.

# Three independent negatives, recorded rather than assumed

The most useful thing a map can record is a checked absence. Two here are worth carrying.

**There is no dependency cycle**, established three ways: Go itself (intra-module cycles do not
compile, and `go list ./...` across all eight in-repo modules exits 0 with empty stderr); the
repository's own gates (`acyclic-graph`, `routes-acyclic` over 29 route packages with **0**
sibling edges, `services-acyclic` over a frozen bounded-context DAG); and a Tarjan pass over the
188 import edges the map derived independently, which finds no strongly-connected component
larger than one.[^qg-nocycle]

**The two-chart parity claim holds.** Reading the `name:` field of all 36 `Chart.yaml` files, and
exactly two names occur in both trees.[^qg-parity] What does **not** hold is the contract that
pair carries — see [`/operations/permissions.md`](/operations/permissions.md).

# Relationships

- The binary is [`/components/loopzero-ops.md`](/components/loopzero-ops.md); the five deliberately
  unwired gates are tracked in
  [`/operations/work-in-flight.md`](/operations/work-in-flight.md).
- `task test:e2e` needs running clusters — [`/operations/deployment-topology.md`](/operations/deployment-topology.md).
- Adding a new `.sh` file is forbidden by default (the `no-new-bash` gate); new logic belongs in
  `cmd/loopzero-ops/internal/quality/`, `infra/helmfile/` or `infra/tests/e2e/`.

# Two gates that assert to nobody

`policies/charts/*.rego` are three Conftest policies with **no** runner anywhere in the
repository — no Taskfile target, no quality gate, no workflow.[^qg-rego] They encode real
invariants about the published charts and nothing executes them.

Separately, `ci.yml:203` explains a week-long pipeline outage by naming a commit —
`696fe5c62` — that **exists in no reachable commit**.[^qg-ci] The change it describes is real and
findable at `8c9a1d953`. A good post-mortem with one unusable pointer is the worst kind: a reader
who follows it concludes the history was rewritten.

# Evidence

[^qg-gates]: `cmd/loopzero-ops/quality_registry.go:36`
[^qg-pyramid]: `infra/tests/go.mod:1`
[^qg-nocycle]: `cmd/loopzero-ops/quality_registry.go:36`
[^qg-parity]: `infra/quality/chart-pairs.txt:24-25`
[^qg-ci]: `.github/workflows/ci.yml:203`
[^qg-rego]: `policies/charts/security_context.rego:1`
[^qg-ops]: `cmd/loopzero-ops/quality_registry.go:36-40`
[^qg-denom]: `.claude/settings.json:103`
