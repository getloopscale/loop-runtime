---
okf_version: "0.2"
---

# LoopZero engine — as-is knowledge bundle

LoopZero is a **CLI-first managed cloud on Kubernetes** for monorepo deployments: `loopzero login`,
`loopzero init`, `loopzero deploy`, and every service is live with TLS, preview environments per pull
request and one-command rollback, with no Kubernetes exposed to the user.

**This repository is the engine, not the product.** It holds the control-plane API, four
Kubernetes operators, shared Go libraries, Helm charts and the infrastructure definitions. The
CLI, the dashboard, the billing control plane and the shared contract types were extracted into
sibling repositories — `../loopzero-cli`, `../loopzero-cloud`, `../loopzero-contracts` — and looking for any
of them here is a dead end by design. One of those siblings, `loopzero-contracts`, must be checked
out beside this repository or the Go workspace does not build at all.

What the engine does, end to end: it accepts a source bundle and a deploy request, runs a signed
build on a separate cluster, renders Kubernetes manifests pinned by image digest, commits them to
a per-tenant Git repository, asks ArgoCD to sync, waits for the sync to be confirmed healthy, and
only then reports success on a server-sent-event stream the CLI is blocked on. Tenants are placed
into one of several runtime cells exactly once, and routed there by pure lookup afterwards.

Everything below was read from the code. Where the repository's own documents disagree with it,
the disagreement is recorded — see [`/operations/contradictions.md`](/operations/contradictions.md)
— not resolved in favour of either side.

## How this bundle was built

- **Mapped on:** 2026-09-11 against `<REPO_ROOT>` at commit `6c22c4ecf`
- **Coverage:** 2,591 of 3,621 in-scope files read in full (71.6%). The categorical gate reports
  **100% of every critical role** — 13/13 entrypoints, 67/67 manifests, 219/219 schema, 517/517
  IaC — and that figure is **narrower than it sounds**: 11 further critical-role files are
  excluded and unread, and an excluded row sits outside the gate's denominator. See
  `## Known gaps`
- **Evidence:** **3,139 anchors** recorded. After the last `verify_evidence.py` pass, which
  completed just after these documents were written: **2,905 re-verified, 233 failed, 1 never
  re-run** — and **every one of the 2,025 `file:line` anchors verifies clean.** Nearly all of the
  233 are an artefact of that pass's own database lock rather than drift; see `## Known gaps`
- **Producer:** `loop-system-cartography/0.1.0`
- **Documents:** 56 concepts — 21 components, 23 entities, 4 flows, 8 operations

The database behind it holds 201 components, 205 entities, 998 relations, 793 entity members,
746 permission grants, 886 metrics, 86 timeline events and 35 findings.

**Those counts are a snapshot, and the run was still executing around this phase.** While these
documents were being written the evidence table grew by 996 rows, the findings table grew from 30
to 35, and a re-verification pass ran to completion. Two of those late findings
are about this bundle's own blind spots and are folded into `## Known gaps` below rather than
left in the register alone.

Every figure here was re-read from the database immediately before the document that carries it,
and re-read again after that verification pass closed. A later phase of the same run may
legitimately report different totals; a difference is the run progressing, not the bundle being
wrong.

## Components

The 201 components in the map are grouped into 21 documents. Every component appears in
exactly one of them, in a table carrying its path and its evidence anchor — none is dropped for
being small.

**The engine as a whole**

- [`components/go-workspace`](/components/go-workspace.md) — The go.work union of nine Go modules, one of which is a sibling repository checked out beside this one.

**The control-plane API**

- [`components/control-plane-api`](/components/control-plane-api.md) — The chi-routed Go HTTP server that is the only interface the CLI and dashboard talk to.
- [`components/build-orchestration`](/components/build-orchestration.md) — Submits an Argo Workflow to the build cluster, polls it, and turns a succeeded workflow into signed images plus a manifest set.
- [`components/release-delivery`](/components/release-delivery.md) — Turns a succeeded build into a release row, a commit in the tenant GitOps repository, and an ArgoCD sync confirmed before activation.
- [`components/persistence`](/components/persistence.md) — The pgx pool, the sqlc-generated queries, the status-mutation store wrappers, the advisory-lock helper and the S3 uploader.
- [`components/multi-cell-routing`](/components/multi-cell-routing.md) — Assigns a tenant to a cell exactly once, then routes to it by pure lookup; plus the client-go adapters that reach each cell.
- [`components/metering-and-billing`](/components/metering-and-billing.md) — Bills the reservation rather than live usage: sums pod requests per cell, advances a guarded cursor, and threads one bounded quantity into the external feed.
- [`components/event-stream`](/components/event-stream.md) — Every deploy event the CLI sees is a Postgres row; the stream handler polls forward from a cursor. There is no broker.
- [`components/external-integrations`](/components/external-integrations.md) — The six client packages that leave the cluster, each wrapping one third-party API.
- [`components/audit-package`](/components/audit-package.md) — A closed audit-action enum written to stop a raw-SQL leak, which no code path calls.

**Operators and shared code**

- [`components/operators`](/components/operators.md) — Tenant, App, Preview and Platform controllers — four binaries over a shared internal tree and one CRD API module.
- [`components/shared-go-libraries`](/components/shared-go-libraries.md) — Thirteen packages every module depends on: the state machines, typed errors, naming, domain refs, enforcement and the OpenBao client.
- [`components/loopzero-ops`](/components/loopzero-ops.md) — The CI substitute: a Go binary holding the repository quality gates plus seed, drill, pulumi and chaos subcommands.

**Infrastructure**

- [`components/helm-charts`](/components/helm-charts.md) — infra/helm bootstraps the platform through Helmfile; charts/ is the published OCI set ArgoCD consumes. Exactly two charts exist in both.
- [`components/helmfile-layer`](/components/helmfile-layer.md) — One helmfile per plane, declaring every platform release and the ordering constraints between them.
- [`components/pulumi-legacy`](/components/pulumi-legacy.md) — The DigitalOcean cloud-primitives layer the README calls AS-IS legacy, which git says is the most actively changed infrastructure tree in the repository.
- [`components/cluster-topology`](/components/cluster-topology.md) — Control, build, two runtime cells and a separate secrets cluster, each defined by its own k3d manifest.

**What runs on the clusters**

- [`components/platform-datastores`](/components/platform-datastores.md) — Postgres, Gitea, MinIO, Harbor, OpenBao, Loki, Tempo, Prometheus and two Redis instances — everything that holds state.
- [`components/platform-orchestrators`](/components/platform-orchestrators.md) — The six controllers that turn declared state into running workloads, split across the control, build and cell planes.
- [`components/platform-runtime-services`](/components/platform-runtime-services.md) — Ingress, certificates, secret sync, admission policy, observability, backup and identity — the nineteen releases that are neither datastore nor orchestrator.
- [`components/external-dependencies`](/components/external-dependencies.md) — Ten external surfaces, each reached from exactly one place in the code.

## Entities


**Data**

- [`entities/database-schema`](/entities/database-schema.md) — Every table, column and foreign key in the control-plane database, read from the generated schema dump.
- [`entities/metering-ledger`](/entities/metering-ledger.md) — How a live pod-request reading becomes a billable number, and the five predicates that stop it when anything is uncertain.

**The HTTP surface**

- [`entities/api-capabilities`](/entities/api-capabilities.md) — The 64 HTTP routes grouped by the user-visible thing they accomplish, rather than by package.

**Custom resources**

- [`entities/crd-tenant`](/entities/crd-tenant.md) — The Tenant CRD and the 7 supporting Go types it references, declared in operators/api/v1.
- [`entities/crd-environment`](/entities/crd-environment.md) — The Environment CRD and the 7 supporting Go types it references, declared in operators/api/v1.
- [`entities/crd-loopzeroapplication`](/entities/crd-loopzeroapplication.md) — The LoopZeroApplication CRD and the 3 supporting Go types it references, declared in operators/api/v1.
- [`entities/crd-exposedroute`](/entities/crd-exposedroute.md) — The ExposedRoute CRD and the 5 supporting Go types it references, declared in operators/api/v1.
- [`entities/crd-previewenvironment`](/entities/crd-previewenvironment.md) — The PreviewEnvironment CRD and the 3 supporting Go types it references, declared in operators/api/v1.
- [`entities/crd-platformbootstrap`](/entities/crd-platformbootstrap.md) — The PlatformBootstrap CRD and the 9 supporting Go types it references, declared in operators/api/v1.
- [`entities/crd-gitrepositorybinding`](/entities/crd-gitrepositorybinding.md) — The GitRepositoryBinding CRD and the 5 supporting Go types it references, declared in operators/api/v1.
- [`entities/crd-clustercredential`](/entities/crd-clustercredential.md) — The ClusterCredential CRD and the 6 supporting Go types it references, declared in operators/api/v1.

**State machines**

- [`entities/state-machine-build`](/entities/state-machine-build.md) — 7 states and 14 transitions, declared in pkg/statemachine/build.go.
- [`entities/state-machine-release`](/entities/state-machine-release.md) — 12 states and 39 transitions, declared in pkg/statemachine/release.go.
- [`entities/state-machine-rollback`](/entities/state-machine-rollback.md) — 8 states and 7 transitions, declared in pkg/statemachine/rollback.go.
- [`entities/state-machine-promotion`](/entities/state-machine-promotion.md) — 7 states and 6 transitions, declared in pkg/statemachine/promotion.go.
- [`entities/state-machine-tenant`](/entities/state-machine-tenant.md) — 6 states and 14 transitions, declared in pkg/statemachine/tenant.go.
- [`entities/state-machine-environment`](/entities/state-machine-environment.md) — 5 states and 8 transitions, declared in pkg/statemachine/environment.go.
- [`entities/state-machine-application`](/entities/state-machine-application.md) — 7 states and 19 transitions, declared in pkg/statemachine/application.go.
- [`entities/state-machine-preview`](/entities/state-machine-preview.md) — 7 states and 10 transitions, declared in pkg/statemachine/preview.go.
- [`entities/state-machine-domain`](/entities/state-machine-domain.md) — 9 states and 22 transitions, declared in pkg/statemachine/domain.go.
- [`entities/state-machine-exposedroute`](/entities/state-machine-exposedroute.md) — 5 states and 7 transitions, declared in pkg/statemachine/exposedroute.go.
- [`entities/state-machine-platformbootstrap`](/entities/state-machine-platformbootstrap.md) — 7 states and 8 transitions, declared in pkg/statemachine/platformbootstrap.go.

**Identity**

- [`entities/rbac-roles`](/entities/rbac-roles.md) — Every ClusterRole, Role and ServiceAccount the map could read by name, and what each one exists to do.

## Flows

Four flows, each traced through real call sites rather than through a diagram.

- [`/flows/deploy-container-app.md`](/flows/deploy-container-app.md) — the whole of `loopzero deploy`
  in 40 steps: where the environment lock opens and closes, why the request returns at step 21
  while the work finishes at step 39, and why nothing is activated on a promise.
- [`/flows/environment-reconcile-loop.md`](/flows/environment-reconcile-loop.md) — a
  level-triggered controller loop in 17 steps, and the worked example of the single-status-writer
  invariant.
- [`/flows/tenant-cell-placement.md`](/flows/tenant-cell-placement.md) — a decision taken once
  per tenant inside one serialized transaction, and never revisited.
- [`/flows/developer-first-app-live.md`](/flows/developer-first-app-live.md) — the same API read
  as an experience, naming the four places it answers 200 while the work is not done.

## Operations

- [`operations/codebase-metrics`](/operations/codebase-metrics.md) — Lines by language and role, size and coupling per package, and when the commits actually happen.
- [`operations/contradictions`](/operations/contradictions.md) — All 35 findings the map recorded: contradictions, stale pointers, unwired code, undeclared boundaries, and the negatives that were checked rather than assumed.
- [`operations/deployment-topology`](/operations/deployment-topology.md) — The L1/L2/L3 layering and the component-to-cluster assignment, read from the helmfiles rather than from a diagram.
- [`operations/ownership`](/operations/ownership.md) — CODEOWNERS names one handle that authored none of the 5,288 commits; git names a different person who authored most of them.
- [`operations/permissions`](/operations/permissions.md) — Every (role, resource, verb) cell the map could read from source, with the file and line that grants it.
- [`operations/quality-gates-and-tests`](/operations/quality-gates-and-tests.md) — The in-repo gate binary that replaced CI, what it measures, and the three ways the map established there is no dependency cycle.
- [`operations/timeline`](/operations/timeline.md) — 80 semver tags in 66 days, then nothing for 24 days while 468 commits landed.
- [`operations/work-in-flight`](/operations/work-in-flight.md) — Thirteen first-party TODOs with an owner and an age — six blocked, five open, two deferred.

## Diagrams

[`diagrams/`](diagrams/) belongs to **phase 6 of this run, which began writing into it while
this bundle was being assembled and had not finished.** At the moment these documents were
written the database still reported **0 of 39 catalog types graded** and zero registered diagram
elements, so nothing here can tell you which types were found inapplicable or why.

Read the directory itself for what was drawn; read nothing into what is absent from it. A type
missing from `diagrams/` may have had no substrate, or may simply not have been reached yet —
and only the graded catalog, once it exists, tells those two apart.

Several concepts carry their own Mermaid diagrams inline, drawn from the same rows a catalog
diagram would use: state diagrams in each of the eleven state-machine documents, an `erDiagram`
of the foreign-key graph in [`/entities/database-schema.md`](/entities/database-schema.md), a
sequence diagram in [`/flows/deploy-container-app.md`](/flows/deploy-container-app.md), the
promotion chain in [`/entities/metering-ledger.md`](/entities/metering-ledger.md), and a
timeline in [`/operations/timeline.md`](/operations/timeline.md).

## Known gaps

What this map does not cover. A bundle that omits this section reads as complete, and a reader
who believes a map is complete treats its silence as absence rather than as ignorance.

### Regions deliberately not walked

| Path | Why | Cost |
|---|---|---|
| `.claude/` (two copies) | an **installed kit**: 1,025 files on disk, **0 tracked by git**, owned by a separate repository (`paulohenriquevn/squad`). Mapping it would assert that this repository *contains* a governance kit; it *has one installed* | the governance rules that shape how this repository is worked on are not described here |
| `bin/`, `operators/bin/` | gitignored build output holding a 77 MB compiled ELF of `cmd/loopzero-ops` | none — the source is mapped, the artifact is not |
| 5 × `node_modules/`, 3 × `__pycache__/`, `.pytest_cache`, `.hypothesis`, `.tilt-rendered` | installed or regenerated, not authored here | none |
| `.git/` (two) | version-control internal storage | none |
| `cartography-output/` | this run's own workspace | none |

### Files registered without their bytes being read — and the glob is wrong

**12 files** match a Read-deny rule in the project's own `settings.json`. All 33 excluded rows
carry **`sha256: null`**, the flag that says *this file was never opened*.

**The deny glob is over-broad, and that is a finding in its own right** (35). It ends in
`/secret.yaml` — the name of the Kubernetes Secret a chart **renders**, not the name of chart
source — so it also matches four Helm **templates** (`charts/loopzero-api`,
`charts/loopzero-tenant-operator`, `charts/loopzero-cloud`, `infra/helm/openbao-init`, each at
`templates/secret.yaml`), plus `minio-credentials-secret.yaml`, three cluster-secret-store
manifests, and an **ArgoCD `Application`** that is not a Secret at all.

**11 of the 33 excluded rows carry a critical role** (`iac` / `config`). That matters for how the
coverage figure above should be read: an excluded row carries `is_excluded=1` and therefore sits
**outside the gate's denominator**, so the gate reports 517/517 IaC and 219/219 schema while
those 11 files were never opened. The gap is in the inventory and **invisible in the gate** —
which is exactly why it is stated here.

**And the honest limit on the inference.** The bytes were deliberately not read, so *"these files
hold no credential"* is a deduction from the path plus the project's own declared convention —
`charts/loopzero-api/SECRETS.md`, which says secrets are set per installation in the target's native
store. It is **not an observation of the content.** A Helm template can hardcode a value and
nothing here rules that out.

The consequence for a reader: the secret **contracts** those manifests declare — which keys a
chart expects, which store they come from — appear in this bundle only where another file
mentions them. Nothing here derives from the bytes of those twelve files.

### Files inventoried and not read

**886 of 3,654 files remain `pending`** — inventoried, classified, never opened:

| Role | Files | Mostly |
|---|---|---|
| source | 764 | 710 Go, 111 shell |
| generated | 90 | lockfiles and generated output |
| asset | 17 | binary assets |
| other | 15 | — |

This is the honest reading of the 71.6% deep-read figure: the entrypoints, manifests, schemas and
IaC were read exhaustively, and a large share of the Go source under them was not. A claim in
this bundle about a package rests on the files that *were* read in it — the package doc comment,
the entry points, and the call sites the flows traced — not on every file it contains.

### Anchors that did not re-verify

Of 3,139 recorded anchors, after the last verification pass: **2,905 re-verified, 233 failed, 1
never re-run.** The split by method is the whole story, because 233 failures sounds like drift and
is almost entirely something else.

| Method | Verified | Failed | Never re-run | Total |
|---|---|---|---|---|
| `file_line` | **2,025** | **0** | 0 | 2,025 |
| `git` | 165 | 0 | 0 | 165 |
| `command` | 173 | 9 | 0 | 182 |
| `derived_query` | 542 | 224 | 1 | 767 |

**Every `file:line` anchor in the map verifies.** 2,025 of 2,025, and every `git` anchor too.
That is the half of the evidence base this bundle's claims actually rest on.

**223 of the 224 `derived_query` failures read `query no longer executes: database is locked`.**
A derived-query anchor re-runs a query *against the cartography database itself*, and the verifier
holds a write transaction on that database while it runs — so it blocked its own re-execution.
Those queries were **not run**; they did not return a different answer. That is a defect in the
verifier's own concurrency, and it is worth knowing before reading the failure count as evidence
rot.

**The remaining 10 are serialisation mismatches, not drift.** Nine `command` anchors and one
`derived_query` recorded a value joining several fields — `'top: 901 paulohenriquevn …; total
commits touching api …'` — and the replay returns only the first field. The underlying facts
hold; the string comparison does not.

**One anchor that WAS genuinely stale has been repaired mid-run.** A `file_line` span recorded
`234-240` against a 239-line file; it now reads `234-239` and verifies. The bundle says so where
it matters, in [`/operations/permissions.md`](/operations/permissions.md), rather than pretending
the defect never existed.

**Two citations in the written bundle were repaired, and both repairs are named** in
[`/operations/contradictions.md`](/operations/contradictions.md). One finding declared a line past
the end of its file; one had a finding anchored on a different file from its supporting evidence.
They were found by re-resolving every citation in the bundle against the filesystem — **918
file-based citations, all of which now resolve** — and **not** by `verify_evidence.py`, which
re-reads the *evidence* table and never looks at the `file`/`line` columns a **finding** declares.
That is a real hole in the verification surface, stated here rather than left for the next reader
to fall into.

This is also why this bundle carries **no `verified` block anywhere**. Re-running a checker
verifies an *anchor* — that the cited line still exists and still says what was recorded. That is
a weaker and different claim from a person having confirmed the *content*, and conflating the two
would let machine re-execution stand in for human review.

### Two defects in this run, not in the repository

Two structure-cartographers were run in parallel over the same helmfile releases under different
key prefixes, and **17 infrastructure components were recorded twice**. Four pairs disagreed on
`kind`, so a kind-filtered query returned a different set depending on which row it found. They
have been reconciled — 16 pairs merged, keeping the row with the fuller anchor — and both the
defect and the reconciliation are recorded, as findings 23 and 24.

Separately, an earlier pass collapsed `buildplane/*` and `services/*` into one node each and
**manufactured a dependency cycle that does not exist**; the map keeps those subpackages distinct
for that reason, recorded as finding 22.

### What only exists at runtime

- **Five ClusterRoles have no readable name.** `metadata.name` is a Helm template expression, so
  the identity comes into being only after `helm template` runs with values. Their grants are in
  [`/operations/permissions.md`](/operations/permissions.md); the name they will carry in a
  cluster is not derivable from source.
- **`loopzero-ctrl-runtime-rbac-admin` is created imperatively**, piped into `kubectl apply` from a
  heredoc in a bootstrap script. It exists on a cell only if that script ran there, and nothing
  declarative reconciles it.
- **No cluster was stood up for this map.** Everything here is read from source and from git. No
  deploy was run, no pod was observed, no ArgoCD application was queried. Claims about runtime
  behaviour are claims about what the code *does when it runs*, established by reading it.

### A shape that was checked and found absent

The medallion / staged-data question was asked of the schema and answered **no**, on evidence
rather than by omission: Redis here is a nonce store and not a cache tier, `canonical` in this
codebase names identifiers rather than trust levels, a mirror is not a tier, and the schema
carries **no staged-data vocabulary at all**. What it does carry is a four-tier **billing ledger**
with one monotonic cursor, which is a different thing and is documented as such in
[`/entities/metering-ledger.md`](/entities/metering-ledger.md).

### What this bundle does not attempt

- **Component-level metrics exist for 82 of 201 components.** Size, coupling and churn were
  measured for the Go packages and modules; charts, helmfile releases, zones and external systems
  carry none. A `—` in those columns means *not measured*, not *zero*.
- **Findings are recorded, never resolved.** 6 high, 13 medium, 12 low and 4 informational
  findings are listed with both anchors and no verdict on which side should change.
- **Conformance:** every concept here carries a non-empty `type`, which is OKF v0.2's one hard
  requirement. The validator run and the exit code actually obtained are recorded in
  [`log.md`](log.md).
