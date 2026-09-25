# Decisions

The decision record for the engine. Each row is a decision **as it stood on its date** —
the reasoning is not rewritten to match the present. Where a decision was later
contradicted, the contradiction is a new row, not an edit to the old one.

The full ADR bodies — context, alternatives rejected, measurements — are in git at
`4f728eb7a:docs/decisions/`. Recover one with
`git show 4f728eb7a:docs/decisions/<name>.md`.

**`PROPOSED` does not authorise implementation.** Governance requires a meeting for T3
changes, and it is the move to `ACCEPTED` / `RATIFIED` that opens the gate. A scheduled
meeting date is not the meeting.

## Open — PROPOSED, not yet ratified

| Date | Decision | Tier | What it turns on |
|---|---|---|---|
| 2026-09-15 | **Whether the bootstrap loopzero-api ClusterRole keeps cluster-wide secrets delete and pods/exec create** | — | `infra/helm/loopzero-api` grants both cluster-wide; the published `charts/loopzero-api` withholds `secrets` delete under every value of `rbac.legacyClusterWideSecrets` and omits `pods/exec` entirely. No caller for either grant has been measured. `helm-drift` records both cells as `undecided` in `infra/quality/helm-drift-registry.yaml` and names them on every run until this is decided. Record: `.squad/wiki/decisions/B-001-bootstrap-grant-disposition.md`. |
| 2026-09-09 | **The secrets cluster is separate, and conditional** | T3 | Supersedes § Q2 of a RATIFIED ADR that placed OpenBao in a `vault-system` namespace on the core cluster. Three artifacts disagreed about where OpenBao runs and the disagreement was load-bearing. Because it supersedes a ratified decision, ratification is sponsor-direct — it is not implied by the file existing. |
| 2026-09-07 | **Source bundles keep going through the API**, and the edge bound is made real | T2 | `POST /api/v1/source/upload` is served by `loopzero-api` itself; every deploy pushes up to 500 MB (`SOURCE_MAX_BUNDLE_SIZE`) through a pod limited to 500m CPU / 256Mi across 2 replicas. The decision keeps the path and makes the edge limit real rather than nominal. |
| 2026-09-07 | **Fixed replicas for the control plane, autoscaling for tenant workloads** | T2 | The renderer emits an HPA — or a KEDA `ScaledObject` when `minReplicas` is 0 — for every tenant application. The platform that runs them does not get the same treatment. |
| 2026-09-07 | **Declare bundle retention on the production object store**, in the infrastructure that creates it | T2 | The audit first reported bundles were never expired. **That was false** — `infra/helm/minio/lifecycle-job.yaml` already expires them — and the refutation is why the ADR exists in this shape: the gap is that production declares retention nowhere its own infrastructure can see. |
| 2026-08-11 | **Who issues the Stripe portal session** | — | Material for a decision, not the decision. |
| 2026-08-07 | **Cluster API (CAPI) as the cell lifecycle** — internal infrastructure and BYOC through one mechanism | T3 | Interacts with sticky placement (ADR-0008 / ADR-0021 D3). Supersedes nothing. |

## Accepted

### Topology and placement

| Date | Decision |
|---|---|
| 2026-08-09 | **Unify cell and cluster 1:1, retiring `cell-default`.** Retroactively ratified 2026-09-06. |
| 2026-08-09 | **Cluster topology naming** — the `{core,build,cell-0,cell-1}` names on disk. The old `{mgmt,workload,runtime-2}` appear in no filename. |
| 2026-05-25 | **Live CA fetch from `kube-root-ca.crt` on `ClusterCredential` reconcile** (RATIFIED, sponsor-direct). Prefer fetching the live source at reconcile time over snapshotting at bootstrap: snapshots go stale, live sources self-heal. |

### Data layer

| Date | Decision |
|---|---|
| 2026-05-29 | **Adopt sqlc as the canonical database access layer**, replacing hand-rolled pgx. |
| 2026-05-29 | **Render `schema.gen.sql` by replaying golang-migrate against an ephemeral PostgreSQL 16**; golang-migrate stays the canonical migrator. |
| 2026-05-29 | **sqlc migration rollback playbook** — operator-witnessed criteria plus an atomic rollback mechanism. |

### Deploy model

| Date | Decision |
|---|---|
| 2026-06-15 | **`services.json` is the immutable app-shape**; secrets and scaling are operational commands, not shape. |
| 2026-06-15 | **Deploy supports only the fixed LoopZeroKit stack** (Python + Node). |
| 2026-07-03 | **J-06 Overview backend** — read-model boundaries and the price source of truth. |


**Admission runs in deny-flip mode where we own the cluster, and warns where the consumer does** (2026-09-16).

`clusterImagePolicy.mode: enforce` in `infra/helm/admission-policies/values.yaml:40` —
the bootstrap chart, Helmfile L2, our own local and dev clusters. `mode: warn` in
`charts/loopzero-admission-policies/values.yaml` — the published OCI chart a self-hoster
installs. `infra/helmfile/environments/drill.yaml:85` runs enforce for drills, which is
what validates the deny path against something shaped like production.

The asymmetry IS the decision: an unsigned image is rejected at admission in a cluster
where breaking it costs us an afternoon, and is logged but admitted in a cluster that
belongs to someone who did not choose this policy. `staticAction: pass` admits what
cannot be verified yet rather than failing closed on a transient Fulcio outage, and
`imageGlobs` scopes the whole thing to two registries we publish to — an image from
anywhere else is outside the policy, not denied by it.

**Who is affected.** Anyone running `task setup` or `tilt up`: an unsigned image in one
of the two globbed registries will not be admitted. Nobody installing `charts/`, where
the mode is `warn`. Drill runs, deliberately.

**What would break.** A local build pushed to `registry.digitalocean.com/loopzero-system/**`
or `harbor.dev.loopzero.example/loopzero-platform/**` without a cosign signature. Nothing else:
images outside those globs are not matched.

**What was rejected.** `warn` everywhere — it makes the deny path untested until the day
it matters, which is the argument the drill environment already settles. `enforce`
everywhere including the published chart — that decides for a self-hoster what their
cluster admits, which the AM6.5 anti-lock-in ADR refuses in the same breath as it makes
ESO recommended rather than required.

**How this came to be recorded.** It was not, until now. The configuration was live and
the `clusterimagepolicy-mode` gate demanded a sign-off it could not find — all three
directories it searched are absent, so no operator action could have satisfied it. The
comment in `values.yaml` that names one of them exists specifically to record that two
EARLIER references were phantom; the replacement was phantom of the same kind, because
the fix replaced the instance and not the class. Recorded here under
`.claude/rules/autonomy-envelope.md` § *A structural decision the contract wants
recorded*, which makes the record the decision rather than a note about one, and
requires it to name who is affected, what breaks and what was rejected — above.
### Code structure

| Date | Decision |
|---|---|
| 2026-09-06 | **Rename `api/internal/domain` to `api/internal/ports`**, with the coupled DIP-layer cleanup. |
| 2026-09-06 | **`services/platform` admission criterion — divide by concern.** A package's admission criterion must be able to REFUSE. `api/internal/k8s` shrank from 7 non-test files / 1,520 lines to 3 / 111, with `k8s/{tenancy,workload,secretsync}` as homes, enforced by `loopzero-ops quality package-admission`. `api/internal/adapters` closed via `adapter-ports`. **`api/internal/services/platform` is OPEN** — measurement refuted the suggested name, since every member runs in the api binary rather than the controller, so renaming versus splitting is undecided. |
| 2026-06-11 | **`pkg/domain.EnvRef` value object** (D2) · **`pkg/domain.DefaultPermanentEnv` canonical constant** (D3) · **plan-tier canonical resolution** (D4) · **hot-path complexity decomposition** (D6). |
| 2026-05-21 | **Naming conventions.** Prefer the longer, unambiguous name: `user_metadata` over `metadata`, `content_type` over `type`, `from_date` over `from`. |

### Testing and tooling

| Date | Decision |
|---|---|
| 2026-09-25 | **One chart per bootstrap/published pair** — every chart that existed twice (a bootstrap copy under `infra/helm/`, installed by Helmfile L2, and a published chart under `charts/`, consumed by ArgoCD L3) becomes the `charts/` chart alone. Helmfile installs it with bootstrap values; the `infra/helm` copy is deleted. The pairs were not two renderings of one design but two charts: the bootstrap `loopzero-api` and `loopzero-cloud` took a free-form env map, owned RBAC the published side withheld, and mounted objects their own Deployments never read. Supersedes the row below for every pair it closes: a divergence between two copies stops being recordable once there is one copy. Pairs closed: `admission-policies`↔`loopzero-admission-policies` (templates were already byte-identical; rendered output of every environment unchanged); `loopzero-cloud` (Helmfile installs `charts/loopzero-cloud` with `values-bootstrap.yaml`; the Deployment selector changes, so an existing Deployment is recreated once). |
| 2026-09-25 | **Chart-pair drift is recorded, never blessed per pair** — every resource kind and every RBAC (apiGroup, resource, verb) cell on which a bootstrap chart and its published twin differ is an entry in `infra/quality/helm-drift-registry.yaml` saying what was observed; `helm-drift` fails on any divergence without one. An entry is `declared` (intended and reviewed) or `undecided` (live, waiting on an open decision). Kinds were measured 2026-08-16, cells 2026-09-15. |
| 2026-09-06 | **Test-pyramid e2e band — ratify the ratchet**, amending ADR D7. |
| 2026-06-11 | **Wiring-triad pillar-(b) test contract** (D5). |
| 2026-06-11 | **Tilt-owned YAML to Helm ConfigMap + Secret mount** as the canonical pattern (D1). |
| 2026-09-06 | **Restore the governance gate and agents** deleted in `8cb41f660` — selectively, not wholesale. |
| 2026-05-21 | **External tooling evaluations.** Recommendations recorded and adopted per tool. |

## Numbered ADRs cited by the code

Production comments cite decisions by number (`ADR-NNNN`). Those numbers were assigned in
documents spread across the old tree; the 2026-09-11 consolidation removed the documents and left
the citations pointing at nothing. **This table restores the pointer, not the document** — each row
says where that ADR's text is in git, and none of it is reconstructed from memory.

| ADR | Cited from | Text at `git show 4f728eb7a:<path>` |
|---|---|---|
| ADR-0006 | `api/internal/routes/projects/projects.go:259` | **not in the tree even before the consolidation** |
| ADR-0018 | `api/cmd/controller/setup_metering.go:102` | `metering-compute-loop-plan` |
| ADR-0020 | `cmd/loopzero-ops/internal/quality/prodimagetags/check.go:5` | `2026-05-22-shell-script-strategic-plan` |
| ADR-0022 | `operators/cmd/registration_test.go:125` | **not in the tree even before the consolidation** |
| ADR-0023 | `operators/cmd/cache_scoping_test.go:23` | `2026-05-26-T2.2-cosign-keyless-change-governor-diagnostic` |
| ADR-0024 | `cmd/loopzero-ops/internal/quality/docsrefs/check.go:15` | `docs/decisions.md` |
| ADR-0025 | `pkg/conditions/summary.go:34` | `03-operators` |
| ADR-0028 | `pkg/conditions/aggregate.go:7` | `docs/architecture.md` |
| ADR-0030 | `api/internal/routes/httpio/writer.go:45` | `04-pkg` |
| ADR-0031 | `pkg/circuitbreaker/circuitbreaker.go:74` | `03-operators` |
| ADR-0032 | `pkg/conditions/aggregate.go:10` | `docs/architecture.md` |
| ADR-0033 | `pkg/enforcement/limiter.go:8` | `docs/architecture.md` |
| ADR-0034 | `pkg/logging/context.go:20` | `2026-05-12-active-execution-plan` |
| ADR-0041 | `cmd/loopzero-ops/internal/quality/helmdrift/check.go:6` | `2026-05-21-deep-review-naming-credentials-toolchain` |
| ADR-0048 | `cmd/loopzero-ops/internal/quality/agplisolation/check.go:11` | `2026-05-22-shell-script-strategic-status` |
| ADR-2026 | `api/cmd/setup_server.go:530` | `docs/architecture.md` |

Two of these did not resolve before the consolidation either — they are recorded as such rather
than quietly dropped. The numbering is historical and not continuous; new decisions are recorded in
this file by date, not by number.

## Decisions recorded outside this file

Some decisions that bind this repo live elsewhere, deliberately:

| Decision | Where |
|---|---|
| Monorepo decomposition (what moved to which sibling repo) | `../loopzero-workspace/docs/decisions.md` |
| AWS infra-state decomposition (DO/Pulumi to AWS/Terraform) | `../loopzero-workspace/docs/decisions.md` |
| LoopZeroKit does not know Kubernetes (ADR-0015) | recorded on the LoopZeroKit side; its consequence for this repo is in [`loopzerokit-loopzerocloud-seam.md`](loopzerokit-loopzerocloud-seam.md) |
| Orphan CRD deletion (AppSecret, ReconciliationReport) | 2026-05-13; the CRDs are gone and [`architecture.md`](architecture.md) records the outcome |

## Related

- [`architecture.md`](architecture.md) — the shape these decisions produced
- [`loopzerokit-loopzerocloud-seam.md`](loopzerokit-loopzerocloud-seam.md) — the seam invariants
