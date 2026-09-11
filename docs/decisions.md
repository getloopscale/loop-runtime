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
| 2026-09-06 | **Test-pyramid e2e band — ratify the ratchet**, amending ADR D7. |
| 2026-06-11 | **Wiring-triad pillar-(b) test contract** (D5). |
| 2026-06-11 | **Tilt-owned YAML to Helm ConfigMap + Secret mount** as the canonical pattern (D1). |
| 2026-09-06 | **Restore the governance gate and agents** deleted in `8cb41f660` — selectively, not wholesale. |
| 2026-05-21 | **External tooling evaluations.** Recommendations recorded and adopted per tool. |

## Decisions recorded outside this file

Some decisions that bind this repo live elsewhere, deliberately:

| Decision | Where |
|---|---|
| Monorepo decomposition (what moved to which sibling repo) | `../loopzero-workspace/docs/decisions/2026-06-16-monorepo-decomposition.md` |
| AWS infra-state decomposition (DO/Pulumi to AWS/Terraform) | `../loopzero-workspace/docs/decisions/2026-06-17-infra-state-decomposition-aws.md` |
| LoopZeroKit does not know Kubernetes (ADR-0015) | recorded on the LoopZeroKit side; its consequence for this repo is in [`loopzerokit-loopzerocloud-seam.md`](loopzerokit-loopzerocloud-seam.md) |
| Orphan CRD deletion (AppSecret, ReconciliationReport) | 2026-05-13; the CRDs are gone and [`architecture.md`](architecture.md) records the outcome |

## Related

- [`architecture.md`](architecture.md) — the shape these decisions produced
- [`loopzerokit-loopzerocloud-seam.md`](loopzerokit-loopzerocloud-seam.md) — the seam invariants
