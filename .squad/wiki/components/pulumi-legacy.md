---
type: Component
title: "Pulumi L1 — declared legacy, measured most-churned"
description: "The DigitalOcean cloud-primitives layer the README calls AS-IS legacy, which git says is the most actively changed infrastructure tree in the repository."
resource: "infra/pulumi"
tags:
  - pulumi
  - l1
  - digitalocean
  - legacy
sources:
  - id: pl-root
    resource: "infra/pulumi/README.md#L359"
    note: "Pulumi monolith (L1 cloud primitives, DigitalOcean)"
  - id: pl-aws
    resource: "infra/pulumi/src/providers/aws/index.ts#L1-L20"
    note: "Pulumi AWS provider (stub)"
  - id: pl-do
    resource: "infra/pulumi/src/providers/digitalocean/index.ts#L1-L25"
    note: "Pulumi DigitalOcean provider"
  - id: pl-shared
    resource: "infra/pulumi-shared/stack-outputs.ts#L1-L12"
    note: "pulumi-shared (StackReference contract)"
  - id: pl-cp
    resource: "infra/pulumi/docs/ADR-001-project-boundaries.md#L298"
    note: "Pulumi project: loopzero-control-plane"
  - id: pl-infra
    resource: "infra/pulumi/docs/ADR-001-project-boundaries.md#L298"
    note: "Pulumi project: loopzero-infra"
  - id: pl-plat
    resource: "infra/pulumi/docs/ADR-001-project-boundaries.md#L298"
    note: "Pulumi project: loopzero-platform"
  - id: pl-tf
    resource: "infra/pulumi/README.md#L359"
    note: "Terraform/AWS L1 (sibling repos) — target"
  - id: pl-churn
    resource: "infra/pulumi/README.md#L359"
    note: "infra/pulumi is declared AS-IS legacy yet is the most-churned infra directory, and no successor exists in this repo"
  - id: pl-stub
    resource: "infra/pulumi/src/providers/aws/index.ts#L14"
    note: "The only in-tree AWS path is a Pulumi provider stub, on neither the live nor the declared-target path"
  - id: pl-adr
    resource: "infra/pulumi/docs/ADR-001-project-boundaries.md#L3"
    note: "ADR-001 is still PROPOSED while its migration is half-executed on disk"
  - id: pl-ghost
    resource: "infra/pulumi/docs/ADR-001-project-boundaries.md#L298"
    note: "infra/pulumi-control-plane/ survives on disk as a node_modules-only directory with no tracked files"
  - id: pl-state
    resource: "infra/pulumi/README.md#L113-L125"
    note: "state lives at s3://loopzero-infra-pulumi-state on Spaces; no dependency on Pulumi Cloud"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

The L1 layer: cloud primitives — managed Kubernetes, database, registry, buckets, DNS —
expressed in TypeScript Pulumi against DigitalOcean. State lives at
`s3://loopzero-infra-pulumi-state` on Spaces, with no dependency on Pulumi Cloud.[^pl-state]

`ext.digitalocean` is the **only implemented** `CloudProviderFactory`.[^pl-do]

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `infra.pulumi` | `infra/pulumi` | module | l1-cloud-primitives | `infra/pulumi/README.md:359` | Three-phase TypeScript Pulumi program (createInfra / bootstrapCluster / exposePlatform) provisioning VPC, 2 DOKS clusters, Gitea VM, runner VMs, managed PostgreSQL and Spaces on DigitalOcean. Its own README states infra/pulumi* is AS-IS legacy until the AM8 cutover to Terraform. Measured: 351 commits, most recent 2026-09-11 — the most-churned infra directory in the repo. |
| `infra.pulumi.provider_aws` | `infra/pulumi/src/providers/aws/index.ts` | module | l1-cloud-primitives | `infra/pulumi/src/providers/aws/index.ts:1-20` | 66-line stub with an implementation guide. The repo's only in-tree AWS path is Pulumi, while the declared successor tooling is Terraform in the sibling repos loopzero-infra-modules / loopzero-infra-live — so this stub is on neither the live path nor the declared target path. |
| `infra.pulumi.provider_do` | `infra/pulumi/src/providers/digitalocean` | module | l1-cloud-primitives | `infra/pulumi/src/providers/digitalocean/index.ts:1-25` | The only implemented provider: network (VPC), kubernetes (DOKS), storage (Spaces), dns, compute (Droplets), database (managed PostgreSQL), registry. |
| `infra.pulumi.controlplane` | `infra/pulumi-control-plane` | module | infrastructure | `infra/pulumi/docs/ADR-001-project-boundaries.md:298` | Third project; StackReference on loopzero-infra + loopzero-platform. Born 2026-04-10, LAST COMMIT 2026-05-14, 17 commits — untouched for nearly four months while both siblings were edited on 2026-09-11. |
| `infra.pulumi.infra` | `infra/pulumi-infra` | module | infrastructure | `infra/pulumi/docs/ADR-001-project-boundaries.md:298` | First of the three projects ADR-001 splits the monolith into. Born 2026-04-10, 25 commits, still touched 2026-09-11.  [merged from duplicate infra.pulumi_infra: Cloud-primitives project carved out of the monolith per ADR-001: VPC, 3 K8s clusters, PostgreSQL + 4 DBs, 4 Spaces buckets, CDN, container registry. Measured git: first 2026-04-10, last 2026-09-11, 25 commits.] |
| `infra.pulumi.platform` | `infra/pulumi-platform` | module | infrastructure | `infra/pulumi/docs/ADR-001-project-boundaries.md:298` | Second project; StackReference on loopzero-infra. Born 2026-04-10, 39 commits, still touched 2026-09-11.  [merged from duplicate infra.pulumi_platform: Platform-services project carved out of the monolith per ADR-001, consuming loopzero-infra outputs via StackReference. Measured git: first 2026-04-10, last 2026-09-11, 39 commits.] |
| `infra.pulumi_shared` | `infra/pulumi-shared/stack-outputs.ts` | library | l1-cloud-primitives | `infra/pulumi-shared/stack-outputs.ts:1-12` | Typed stack-output contract shared by the split Pulumi projects; the seam ADR-001 named as the connection between loopzero-infra and loopzero-platform. |
| `ext.infra.terraform` | `../loopzero-infra-live` | external | external | `infra/pulumi/README.md:359` | The replacement L1 named by infra/pulumi/README.md:359. Built in TWO SIBLING REPOSITORIES, not here: ../loopzero-infra-modules (versioned blueprints) and ../loopzero-infra-live (per-env Terragrunt units — root.hcl, _envcommon, bootstrap, iam-bootstrap, sandbox, tests all present on disk). Nothing in this repository builds or tests it. |
| `ext.digitalocean` | `infra/pulumi/src/providers/digitalocean` | external | external | `infra/pulumi/package.json:22` | The cloud the Pulumi L1 layer provisions against: VPC, DOKS clusters, Spaces buckets (also the Pulumi state backend at s3://loopzero-infra-pulumi-state), managed PostgreSQL, Droplets for Gitea and runners, DNS records, container registry. Anchored on two sides: the dependency declaration in infra/pulumi/package.json and the provider implementation under infra/pulumi/src/providers/digitalocean. Its object-storage face is recorded separately as ext.dospaces, which the Go uploader talks to at runtime. |

# Relationships

- L2 **consumes** this layer's stack outputs — see
  [`/components/helmfile-layer.md`](/components/helmfile-layer.md).
- `infra.pulumi_shared` is the `StackReference` contract that connects the split projects.
- The declared successor is Terraform in the sibling repositories `../loopzero-infra-modules` and
  `../loopzero-infra-live`, both present on disk beside this one and **neither inside it**.[^pl-tf]

# Where the code and the documents disagree

This is the largest disagreement the map found, and it has four distinct parts.

**1. "AS-IS legacy" versus the churn.** `infra/pulumi/README.md:359` declares
*"`infra/pulumi*` is AS-IS legacy until the AM8 cutover to Terraform."* Measured on
2026-09-11:[^pl-churn]

| Tree | Commits | Most recent |
|---|---|---|
| `infra/pulumi` | **351** | 2026-09-11 |
| `infra/helm` | 214 | — |
| `infra/helmfile` | 62 | — |
| `infra/k3d` | 28 | — |

`infra/pulumi` has more commits than the other three **combined** (304). Six commits touched
`infra/pulumi/src` since 2026-08-01, four of them refactors — cluster renames, cell identity
merge, package moves — rather than freeze maintenance. A layer being actively refactored is not
being held as-is.

**2. No `.tf` file exists anywhere in this repository.** The successor tooling is not here.

**3. The only AWS artifact is a stub.** `infra/pulumi/src/providers/aws/index.ts` is 66 lines
opening with *"AWS provider stub — not implemented yet"* and a seven-step implementation guide.
It is on **neither** the live path (DigitalOcean/Pulumi) nor the declared target path
(AWS/Terraform). A reader following the README's portability paragraph would start implementing
it.[^pl-stub]

**4. ADR-001 is PROPOSED while its migration is half-executed.** The ADR (2026-04-10) decides a
split into three Pulumi projects with a 13-step migration ending at *"13. Deprecate monolith"*.
Measured: two of the three exist and are live (`infra/pulumi-infra`, 25 commits;
`infra/pulumi-platform`, 39 commits; both last touched 2026-09-11) and the StackReference
contract exists. The third does not — `infra/pulumi-control-plane`'s last commit, on 2026-05-14,
is literally *"delete pulumi-control-plane stub"*. Step 13 never ran.[^pl-adr]

That deleted project **still appears on disk**: `git ls-files infra/pulumi-control-plane`
returns nothing, and `ls` returns exactly one entry, `node_modules`. A fresh clone has no such
directory; anyone reading an existing checkout sees a third Pulumi project that does not
exist.[^pl-ghost]

# Evidence

[^pl-root]: `infra/pulumi/README.md:359`
[^pl-aws]: `infra/pulumi/src/providers/aws/index.ts:1-20`
[^pl-do]: `infra/pulumi/src/providers/digitalocean/index.ts:1-25`
[^pl-shared]: `infra/pulumi-shared/stack-outputs.ts:1-12`
[^pl-cp]: `infra/pulumi/docs/ADR-001-project-boundaries.md:298`
[^pl-infra]: `infra/pulumi/docs/ADR-001-project-boundaries.md:298`
[^pl-plat]: `infra/pulumi/docs/ADR-001-project-boundaries.md:298`
[^pl-tf]: `infra/pulumi/README.md:359`
[^pl-churn]: `infra/pulumi/README.md:359`
[^pl-stub]: `infra/pulumi/src/providers/aws/index.ts:14`
[^pl-adr]: `infra/pulumi/docs/ADR-001-project-boundaries.md:3`
[^pl-ghost]: `infra/pulumi/docs/ADR-001-project-boundaries.md:298`
[^pl-state]: `infra/pulumi/README.md:113-125`
