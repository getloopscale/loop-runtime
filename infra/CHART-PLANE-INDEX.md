# Chart → plane index

Which plane each chart in `charts/` belongs to, and **how that was proven**. Delivers the third
bullet of `B-048` and the consequence of the ADR
`2026-08-12-umbrella-chart-plane-separation` (read it with
`git show 4f728eb7a:docs/wiki/decisions/2026-08-12-umbrella-chart-plane-separation.md`).

## Why this file exists

The development path spends **four helmfiles** building the plane separation
(`infra/helmfile/{core,build,cell-0,secrets}.helmfile.yaml.gotmpl`). Without an index, the only way
to answer *"which plane does this run on?"* was to open all four and search — and the wrong answer
does not fail, it installs into the wrong cluster.

## How to read the Proof column

Plane assignment is **measurable**, and the measurement has a step that must not be erased:

- **proven** — a helmfile declares `chart: ../../charts/<name>`, so the line is about the **same
  artifact** this index is about.
- **inferred** — the plane comes from the name, not from a declaration. This repository has **two
  charts sharing a name across two trees** (`infra/helm/` for bootstrap, `charts/` for OCI —
  `CLAUDE.md` § Helm chart dual system, item `B-056`), and a `chart: ../helm/<name>` line proves the
  plane of the *bootstrap* chart, not of this one. Confusing the two was the error v1.1 of the ADR
  corrected.

## The index

| Chart | Plane | Proof |
|---|---|---|
| `loopzero-crds` | core | **proven** — `infra/helmfile/core.helmfile.yaml.gotmpl:173` |
| `loopzero-tenant-operator` | core | **proven** — `core.helmfile.yaml.gotmpl:551` |
| `loopzero-app-operator` | core | **proven** — `core.helmfile.yaml.gotmpl:576` |
| `loopzero-preview-operator` | core | **proven** — `core.helmfile.yaml.gotmpl:596` |
| `loopzero-platform-operator` | core | **proven** — `core.helmfile.yaml.gotmpl:616` |
| `loopzero-build-cwts` | **build** | **proven** — `build.helmfile.yaml.gotmpl:70-72` |
| `loopzero-api` | core (expected) | **inferred** — no helmfile declares `chart: ../../charts/loopzero-api` |
| `loopzero-cloud` | core (expected) | **inferred** — same |
| `loopzero-admission-policies` | core (expected) | **inferred** — same |
| `loopzero-monitoring` | — | **declared nowhere**; it has no counterpart in `infra/helm/` |
| `loopzero-dashboard` | — | **declared nowhere**; same |
| `loopzero-common` | — | a library (`type: library`); it installs nothing on its own |
| ~~`loopzero-platform`~~ | — | **removed 2026-08-19** — see below |

## The umbrella was REMOVED on 2026-08-19, and why

`charts/loopzero-platform` declared 12 dependencies spanning **core**, **build** and **cell**, installed
as a single release. `B-048` decided that was **deliberate** single-cluster composition, not an
oversight.

**It was removed because it did not install.** Measured 2026-08-19: `helm template` exits `rc=1`
with **zero lines** — with no profile, with `helmfile/environments/dev.yaml`, and with
`helmfile/environments/prod.yaml`. The subcharts' `values.schema.json` refuse `global` and `enabled`,
which are exactly the keys an umbrella composition injects.

Broken since `1ce505758` (**2026-06-19**) — two months, unnoticed, because no gate rendered it. It
was in neither the ArgoCD bootstrap nor the publish matrix, and its only install path was a manual
taskfile target, also broken.

**The `B-048` decision did not die with it:** it lives in the ADR
`2026-08-12-umbrella-chart-plane-separation` and in git history. What was removed is the
non-functional implementation.

**A measured consequence, for whoever decides next:** `loopzero-monitoring` and `loopzero-dashboard` had the
umbrella as their **only** declared consumer. With it gone, they have none. Removing them is a
separate decision, and `B-073` did not ask for it.

Record: `MEET-ASYNC-AMENDMENT-2026-08-19-remove-umbrella-chart.md`.

## Maintenance

A new row lands with its **proof** or with the word **inferred**. An index that erases the
difference between the two is worse than no index, because the reader starts believing everything
was verified.
