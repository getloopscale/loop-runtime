---
type: Operations
title: "Measured shape of the codebase"
description: "Lines by language and role, size and coupling per package, and when the commits actually happen."
resource: "."
tags:
  - metrics
  - loc
  - coupling
  - churn
sources:
  - id: cm-lang
    resource: "query:SELECT SUM(loc) FROM files_inventoried WHERE is_excluded=0 AND loc IS NOT NULL AND language='go'"
    note: "total 601347 LOC over 12 languages; is_excluded=0 drops vendored/node_modules"
  - id: cm-dir
    resource: "query:SELECT SUM(loc) FROM files_inventoried WHERE is_excluded=0 AND loc IS NOT NULL AND (path='<REPO_ROOT>/api/internal' OR path LIKE '<LOCAL_PATH>"
    note: "package = first two path segments; parent = top-level module. 40 largest of 129 total."
  - id: cm-loc
    resource: "query:SELECT SUM(loc) FROM files_inventoried WHERE is_excluded=0 AND loc IS NOT NULL AND path LIKE '<REPO_ROOT>/api/internal/db/store/%' AND path NOT LIKE '<LOCAL_PATH>"
    note: "direct LOC: files whose dirname equals the component's declared path, so a parent package is not credited with its children's lines."
  - id: cm-inst
    resource: "query:SELECT ROUND(CAST((SELECT COUNT(*) FROM relations WHERE kind IN ('imports','depends_on') AND from_key='api.db.store') AS REAL)/((SELECT COUNT(*) FROM relations WHERE kind IN ('imports','depends_on') A"
    note: "Martin instability I = Ce/(Ca+Ce) over the relation graph THIS cartography recorded, which is a property of the map rather than of every import in the tree — re"
  - id: cm-period
    resource: "command:python3 -c \"import subprocess,collections;print(collections.Counter(subprocess.run(['git','log','--format=%ad','--date=format:%H'],capture_output=True,text=True).stdout.split())['00'])\""
    note: "author date, %H = local hour 00..23. Total 5288 commits."
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

886 measurements recorded across five subject kinds. This document carries the ones that say
something about the shape of the system rather than about one file.

# Schema

## Lines by language

| Language | Lines |
|---|---|
| go | 380,717 |
| json | 72,964 |
| yaml | 70,112 |
| shell | 29,127 |
| typescript | 18,484 |
| sql | 10,864 |
| markdown | 6,562 |
| other | 6,124 |
| javascript | 3,288 |
| python | 2,343 |
| dockerfile | 675 |
| makefile | 87 |

**Test code outnumbers production Go by more than 1.5 to 1** — 219,829 lines of `go->test`
against 139,841 of `go->source`, plus 18,104 generated.[^cm-lang]

## Lines by language and role

| Language → role | Lines |
|---|---|
| `go->test` | 219,829 |
| `go->source` | 139,841 |
| `yaml->config` | 35,838 |
| `json->config` | 33,217 |
| `yaml->iac` | 31,480 |
| `json->generated` | 22,217 |
| `shell->source` | 21,721 |
| `go->generated` | 18,104 |
| `typescript->iac` | 12,369 |
| `json->iac` | 11,322 |
| `sql->schema` | 10,808 |
| `shell->test` | 7,406 |
| `markdown->doc` | 6,562 |
| `json->schema` | 5,515 |
| `typescript->test` | 5,383 |
| `go->entrypoint` | 2,943 |
| `javascript->test` | 2,741 |
| `other->generated` | 2,187 |
| `python->source` | 1,872 |
| `yaml->schema` | 1,846 |
| `other->config` | 1,575 |
| `other->other` | 921 |
| `typescript->fixture` | 732 |
| `other->manifest` | 687 |

## Lines by directory

| Directory | Lines |
|---|---|
| `api/internal` | 162,474 |
| `cmd/loopzero-ops` | 103,011 |
| `infra/tests` | 55,155 |
| `infra/helm` | 40,006 |
| `operators/internal` | 28,629 |
| `api/tests` | 24,278 |
| `landscape.yml` | 20,109 |
| `infra/scripts` | 20,008 |
| `infra/pulumi` | 13,132 |
| `charts/loopzero-monitoring` | 12,621 |
| `infra/pulumi-platform` | 10,214 |
| `.schemathesis/default` | 9,427 |
| `infra/helmfile` | 7,640 |
| `api/cmd` | 7,595 |
| `infra/pulumi-infra` | 7,393 |
| `pkg/statemachine` | 7,204 |
| `infra/pulumi-drill` | 6,047 |
| `operators/api` | 4,272 |
| `pkg/vault` | 3,764 |
| `pkg/enforcement` | 3,512 |

`cmd/loopzero-ops` at 103,011 lines is the second-largest directory in the
repository[^cm-dir] — the quality-gate binary is larger than the operators, the charts and the
helmfile layer combined. That is what replacing a CI pipeline with an in-repo binary costs in
code.

## The twenty largest packages

| Package | Lines | Files |
|---|---|---|
| `api.db.store` | 12,248 | 70 |
| `operators.tenant` | 9,613 | 30 |
| `api.db.generated` | 7,321 | 26 |
| `pkg.statemachine` | 6,951 | 48 |
| `api.cmd.server` | 6,174 | 33 |
| `api.db` | 5,653 | 25 |
| `api.manifests` | 5,468 | 22 |
| `operators.platform` | 4,653 | 15 |
| `operators.api.v1` | 4,170 | 22 |
| `pkg.enforcement` | 3,512 | 19 |
| `operators.app` | 3,452 | 10 |
| `api.github` | 3,367 | 9 |
| `api.argocd` | 3,261 | 12 |
| `api.metrics` | 2,713 | 18 |
| `api.sse` | 2,666 | 6 |
| `api.adapters` | 2,444 | 16 |
| `api.metering` | 2,201 | 16 |
| `operators.preview` | 2,065 | 7 |
| `api.aggregator` | 2,055 | 16 |
| `api.placement` | 1,998 | 10 |

## Coupling and instability

Instability is `efferent / (afferent + efferent)`: **1.0** means it depends on others and nothing
depends on it; **0.0** means the reverse.[^cm-inst]

**Maximally unstable — 15 packages at ≥ 0.9.** Every composition root is here, which
is correct:

| Package | Instability | Efferent | Afferent |
|---|---|---|---|
| `api.cellmgr` | 1.00 | 1 | 0 |
| `api.cmd.controller` | 1.00 | 18 | 0 |
| `api.cmd.server` | 1.00 | 60 | 0 |
| `api.objectstore` | 1.00 | 1 | 0 |
| `mod.infra.tests` | 1.00 | 4 | 0 |
| `operators.cmd.app` | 1.00 | 7 | 0 |
| `operators.cmd.platform` | 1.00 | 8 | 0 |
| `operators.cmd.preview` | 1.00 | 7 | 0 |
| `operators.cmd.tenant` | 1.00 | 14 | 0 |
| `operators.tenant` | 0.94 | 16 | 1 |
| `api.server` | 0.91 | 10 | 1 |
| `operators.app` | 0.91 | 10 | 1 |
| `operators.environment` | 0.90 | 9 | 1 |
| `operators.platform` | 0.90 | 9 | 1 |
| `operators.preview` | 0.90 | 9 | 1 |

**Maximally stable — 29 packages at 0.0**, depending on nothing in the workspace:

`api.archive`, `api.cfzone`, `api.config`, `api.correlation`, `api.db.dbtx`, `api.db.generated`, `api.events`, `api.gitclient`, `api.metrics`, `api.ports`, `api.s3`, `api.scrub`, `api.tracing`, `api.validation`, `mod.infra.scripts`, `mod.operators.api`, `operators.api.v1`, `operators.constants`, `operators.metrics`, `operators.operatorboot`, `operators.tracing`, `operators.vault`, `pkg.circuitbreaker`, `pkg.conditions`, `pkg.correlation`, `pkg.errors`, `pkg.healthcheck`, `pkg.naming`, `pkg.statemachine`

Seven of the thirteen `pkg/` libraries are in that list, and two more carry no coupling
measurement at all. A shared library that depends on nothing is a shared library you can move.

## Churn — the last 90 days

| Package | Commits (90d) | Commits (all) |
|---|---|---|
| `mod.api` | 412 | 1065 |
| `mod.infra.tests` | 358 | 769 |
| `mod.infra.scripts` | 150 | 191 |
| `api.db` | 118 | 225 |
| `api.cmd.server` | 107 | 249 |
| `mod.pkg` | 74 | 139 |
| `mod.operators` | 67 | 332 |
| `api.server` | 54 | 120 |
| `api.db.store` | 37 | 37 |
| `api.adapters` | 30 | 57 |
| `operators.tenant` | 25 | 27 |
| `api.db.generated` | 25 | 27 |
| `operators.app` | 19 | 48 |
| `api.metrics` | 17 | 45 |
| `api.metering` | 17 | 17 |

## When the work happens

Commits by hour of day:[^cm-period]

| Hour | Commits | | Hour | Commits |
|---|---|---|---|---|
| 00h | 159 | | 12h | 332 |
| 01h | 114 | | 13h | 326 |
| 02h | 98 | | 14h | 294 |
| 03h | 100 | | 15h | 302 |
| 04h | 96 | | 16h | 329 |
| 05h | 66 | | 17h | 344 |
| 06h | 103 | | 18h | 254 |
| 07h | 116 | | 19h | 260 |
| 08h | 193 | | 20h | 278 |
| 09h | 265 | | 21h | 293 |
| 10h | 256 | | 22h | 208 |
| 11h | 318 | | 23h | 184 |

By weekday:

| Mon | Tue | Wed | Thu | Fri | Sat | Sun |
|---|---|---|---|---|---|---|
| 689 | 808 | 936 | 740 | 991 | 554 | 570 |

**Weekend commits are 1,124 of 5,288 — 21%.** The distribution has no dead hours: the quietest
is 05h with 66 commits and the busiest 17h with 344, a ratio of about five. This is the profile
of a repository with one primary author rather than a team on a shared schedule, which is what
[`/operations/ownership.md`](/operations/ownership.md) measured independently.

# Relationships

- The packages measured here are described in
  [`/components/go-workspace.md`](/components/go-workspace.md) and the documents it links to.
- The test counts behind `go->test` are
  [`/operations/quality-gates-and-tests.md`](/operations/quality-gates-and-tests.md).
- The churn measurement that contradicts a README is in
  [`/components/pulumi-legacy.md`](/components/pulumi-legacy.md).

# Evidence

[^cm-lang]: `SELECT SUM(loc) FROM files_inventoried WHERE is_excluded=0 AND loc IS NOT NULL AND language='go'`
[^cm-dir]: `SELECT SUM(loc) FROM files_inventoried WHERE is_excluded=0 AND loc IS NOT NULL AND (path='<REPO_ROOT>/api/internal' O`
[^cm-loc]: `SELECT SUM(loc) FROM files_inventoried WHERE is_excluded=0 AND loc IS NOT NULL AND path LIKE '<REPO_ROOT>/api/interna`
[^cm-inst]: `SELECT ROUND(CAST((SELECT COUNT(*) FROM relations WHERE kind IN ('imports','depends_on') AND from_key='api.db.store') AS REAL)/((SELECT COUNT(*) FROM `
[^cm-period]: `python3 -c "import subprocess,collections;print(collections.Counter(subprocess.run(['git','log','--format=%ad','--date=format:%H'],capture_output=True`
