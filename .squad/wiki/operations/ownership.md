---
type: Operations
title: "Who owns this code — declared versus measured"
description: "CODEOWNERS names one handle that authored none of the 5,288 commits; git names a different person who authored most of them."
resource: ".github/CODEOWNERS"
tags:
  - ownership
  - codeowners
  - git
  - governance
sources:
  - id: ow-declared
    resource: ".github/CODEOWNERS#L5"
    note: "@paulosergio-jnr"
  - id: ow-measured
    resource: "command:git log --format='%an <%ae>' | sort | uniq -c | sort -rn"
    note: "paulohenriquevn <paulohenriquevn@gmail.com>"
  - id: ow-bot
    resource: "command:git log --format='%an <%ae>' | sort | uniq -c | sort -rn | grep loopzero"
    note: "LoopZero <admin@loopzero.example>"
  - id: ow-codeowners
    resource: ".github/CODEOWNERS#L22"
    note: "CODEOWNERS assigns ownership of two files that do not exist"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

The map recorded ownership **twice, from two independent sources**, and they do not agree.

| | Declared | Measured |
|---|---|---|
| Source | `.github/CODEOWNERS` | `git log`, 5,288 commits |
| Identity | `@paulosergio-jnr` | `paulohenriquevn <paulohenriquevn@gmail.com>` |
| Coverage | all 10 rules in the file | every top-level directory |
| Commits authored | **zero**[^ow-declared] | 901/1065 in `api`, 1559/1770 in `infra`, 340/342 in `cmd`[^ow-measured] |

# Schema

## Declared — a single node covering everything

`.github/CODEOWNERS` names exactly one handle, on all ten rules: the `*` default plus `/api/`,
`/operators/`, `/pkg/`, `/infra/`, `/.github/` and four governance documents. The declared
ownership graph is therefore **one node over the whole repository with no per-area
differentiation** — it is a review requirement, not a statement about who knows what.[^ow-declared]

| Declared rule | Target |
|---|---|
| `owns` | `loopzero` |
| `owns` | `mod.api` |
| `owns` | `mod.operators` |
| `owns` | `mod.pkg` |
| `owns` | `infra.helmfile` |

Two of the files CODEOWNERS assigns an owner to **do not exist** — including `MAINTAINERS.md`,
which the file itself points at.[^ow-codeowners]

## Measured — one human author, by a wide margin

| Directory | Dominance | Contributors |
|---|---|---|
| `cmd/` | 340/342 = **0.994** | 2 |
| `charts/` | 68/70 = 0.971 | 2 |
| `operators/` | 306/332 = 0.922 | 4 |
| `infra/` | 1559/1770 = 0.881 | 5 |
| `pkg/` | 118/139 = 0.849 | 4 |
| `api/` | 901/1065 = 0.846 | 6 |
| `tools/` | 11/17 = 0.647 | 3 |

Dominance never drops below two thirds. A second identity, `Paulo Henrique` on the same email,
is the same person under a different configured name; counted together, the repository has
**effectively one human author**.[^ow-measured]

## The third identity is automation

`LoopZero <admin@loopzero.example>` is the third-largest committer at 181 commits, plus two adjacent
addresses. It appears in every top-level directory's contributor list and is dominant in none.
The map records it as an **agent** rather than an owner, on a specific criterion: it is not a
person who can be asked a question about a file.[^ow-bot]

# Relationships

- The modules these owners cover are
  [`/components/go-workspace.md`](/components/go-workspace.md).
- The release cadence the same history shows is
  [`/operations/timeline.md`](/operations/timeline.md).
- The gap between what a document declares and what the repository does is the recurring shape
  of [`/operations/contradictions.md`](/operations/contradictions.md).

# Why both are recorded rather than one being picked

A CODEOWNERS entry is a **routing rule for reviews**; a commit count is **evidence of who has
touched something**. Neither substitutes for the other, and resolving them silently in favour of
either would lose the fact that matters: the repository's review gate points at somebody with no
commits in it.

# Evidence

[^ow-declared]: `.github/CODEOWNERS:5`
[^ow-measured]: `git log --format='%an <%ae>' | sort | uniq -c | sort -rn`
[^ow-bot]: `git log --format='%an <%ae>' | sort | uniq -c | sort -rn | grep loopzero`
[^ow-codeowners]: `.github/CODEOWNERS:22`
