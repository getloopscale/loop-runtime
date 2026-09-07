# Storage growth projection

**Created:** 2026-09-07 · **Source:** infrastructure system-design audit, finding SD-02.2

Every store that grows now has a declared bound and an arithmetic behind it. Before this document
the retention existed and the projection did not, so nobody could answer "how big does this get?"
without reading four values files and a Taskfile.

## What bounds each store

| Store | Holds | Retention | Where the retention is declared |
|---|---|---|---|
| MinIO / Spaces `loopzero-builds` | one source bundle per commit | **30 days** | `infra/helm/minio/lifecycle-job.yaml:87-88` (in-cluster) and `expireAfterDays: 30` on the Spaces bucket (`infra/pulumi-infra/src/index.ts`, ADR 2026-09-07) |
| MinIO / Spaces `loopzero-artifacts` | released artifacts | **none, deliberately** | versioned bucket; a rollback may still need old objects. Asserted as an intentional absence by `storage-lifecycle.test.ts` |
| Harbor | built tenant images | **daily GC of untagged** | `infra/helm/harbor/values.yaml:188-191` |
| Loki | build and tenant logs | **7 days**, 90 for the forensic stream | `infra/helm/loki/values.yaml:39,46` |
| Tempo | traces | **7 days** | `infra/helm/tempo/values.yaml:14` |
| Prometheus | metrics | **15 days** | `infra/helm/monitoring/values-prometheus.yaml:3` |
| Velero | cluster backups | **30 days** (ttl 720h × 5 schedules) | `infra/helm/velero/values-schedules.yaml` |

## The bundle store, which is the only one that grows with customer activity

The other stores are bounded by time and by the platform's own volume. This one is bounded by time
**and** by how much customers deploy, so it is the one worth projecting.

**Inputs, each traceable:**

| Input | Value | Source |
|---|---|---|
| max bundle size | 500 MB | `api/internal/config/config.go:469` (`SOURCE_MAX_BUNDLE_SIZE`) |
| retention | 30 days | ILM rule + Spaces lifecycle above |
| deploys per active tenant per day | 2 | `docs/operations/loopzero-capsim.mjs` — labelled **ESTIMATE** |
| active fraction of tenants | 8% | same file, same label |
| tenants per cell | 5000 | `charts/loopzero-api/values.yaml` `maxTenantsPerCell` |

**Steady state = bundles retained × mean bundle size**, where bundles retained = active tenants ×
deploys/day × 30.

Per cell at the declared ceiling: `5000 × 0.08 = 400` active tenants → `400 × 2 × 30 = 24,000`
bundles resident at any time.

| Mean bundle size | Steady state per cell |
|---|---|
| 5 MB (small service repo) | ~120 GB |
| 25 MB (monorepo with lockfiles) | ~600 GB |
| 100 MB | ~2.4 TB |
| 500 MB (the cap, every deploy) | ~12 TB |

## What this says, and what it does not

**Says:** the store is bounded. At 30 days it converges rather than growing without limit, and the
spread across plausible bundle sizes is two orders of magnitude — which is why the mean matters far
more than the cap.

**Does not say:** what the mean actually is. **Nobody has measured it.** Two of the five inputs are
labelled ESTIMATE in their own source, and the mean bundle size is not declared anywhere at all.
The table above is arithmetic over an unmeasured input, and it should be read as a shape, not as a
figure to provision against.

## The one measurement that would replace most of this

```bash
mc du --recursive local/loopzero-builds
mc ls --recursive local/loopzero-builds | wc -l
```

Object count and total size, taken once, give the real mean and turn every row above into a number.
Until then the honest planning statement is: *bounded at 30 days, between roughly 100 GB and several
TB per cell at the declared tenant ceiling, and nobody has measured which.*

## Related

- ADR: `docs/decisions/2026-09-07-production-bucket-retention.md`
- Capacity model: `docs/operations/loopzero-capsim.mjs` (models the AWS target at 50k tenants)
- Audit: `system-design-output/final_report.md` § SD-02.2
