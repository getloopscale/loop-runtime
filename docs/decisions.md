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
| 2026-09-25 | **Only the api writes the api's tables — the data-plane readiness poller moves from loopzero-app-operator into the api.** Full record below the table. |

**Only the api writes the api's tables** (2026-09-25).

The dashboard's project badge reads `environment_targets.last_replicas_ready_{value,at}`. The
app-operator filled them: a leader-elected Runnable queried Prometheus and ran `UPDATE
environment_targets` over a DSN of its own (`LOOPZERO_DATA_PLANE_POLLER_PG_URL`, a plaintext
password in the dev-public ConfigMap). A workload controller depended on another component's
table layout with no contract; a schema change in `api` broke it at runtime only, and a
cross-tenant `UPDATE` under a role other than `loopzero_worker` succeeds only if that role
bypasses the table's tenant-isolation policy.

*Decision.* The poller is an api background loop (`environments.ReadinessPoller`), started by
`backgroundloops.Build` on the elected leader in either binary. It reads Prometheus through
`environmentsadapter.PrometheusReplicasReadySource` and writes through a sqlc query on the
background pool. The operator has no database access at all. Configuration moved with it:
`dataPlanePoller.{enabled,prometheusUrl,interval}` on `charts/loopzero-api`, same
`LOOPZERO_DATA_PLANE_POLLER_*` keys, no DSN.

*No environment lock.* The write touches only the two observation columns, never the release
pointers `withEnvironmentLock` guards, so it cannot interleave with a deploy mutation beyond
the row lock Postgres already takes.

*What was rejected.*
- *The operator reports through a CR's status and the api copies it.* The only CR keyed by
  (tenant, project, environment) is `Environment`, whose status has one writer, the environment
  controller; a second writer breaks the single-status-writer invariant. The api would also
  need an informer to copy the status back into the table the dashboard query joins.
- *An internal authenticated api endpoint the operator calls.* No operator calls the api
  today: it would add a service identity, key distribution and an operator → api network
  dependency, all to relay a Prometheus reading the api can take itself.

**Who is affected.** Operators of an install that enabled `dataPlanePoller` on
`loopzero-app-operator`: the block now lives on `loopzero-api`, without `pgUrl`. **What would break.**
Nothing at the badge; a values file still setting `dataPlanePoller` on `loopzero-app-operator`
fails that chart's schema, so the move cannot be missed silently.

### Deploy model

| Date | Decision |
|---|---|
| 2026-06-15 | **`services.json` is the immutable app-shape**; secrets and scaling are operational commands, not shape. |
| 2026-06-15 | **Deploy supports only the fixed LoopZeroKit stack** (Python + Node). |
| 2026-07-03 | **J-06 Overview backend** — read-model boundaries and the price source of truth. |


**Admission runs in deny-flip mode where we own the cluster, and warns where the consumer does** (2026-09-16).

`mode: warn` in `charts/loopzero-admission-policies/values.yaml` — the chart a self-hoster
installs. Our own clusters take the mode from the environment's `policyMode`, which the
cell release sets as `clusterImagePolicy.mode` (`infra/helmfile/cell.helmfile.yaml.gotmpl`):
`enforce` in `prod.yaml` and `drill.yaml` — drills are what validate the deny path against
something shaped like production — and `warn` in the dev and standalone environments.

*Corrected 2026-09-25.* This paragraph cited `mode: enforce` in
`infra/helm/admission-policies/values.yaml` as what our local and dev clusters run. That
default was overridden by the environment's `policyMode` on every cell install, so dev
already ran `warn`; the bootstrap chart was then deleted ("One chart per
bootstrap/published pair"), and the rendered policy of every environment is unchanged.

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

*Corrected 2026-09-25.* The `harbor.dev.loopzero.example/loopzero-platform/**` glob left both
ClusterImagePolicies when Harbor was retired ("Retire Harbor", below); the policies now
match the DOCR glob alone.

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
### Registry

| Date | Decision |
|---|---|
| 2026-09-25 | **Retire Harbor; the registry is managed** — ECR on AWS, provisioned in `../loopzero-infra-live`; a self-hoster brings their own registry through `REGISTRY_URL`; locally `k3d-loopzero-registry` stays. Harbor leaves the engine entirely: the Helmfile releases, the wrapper chart, the api dual-push mirror, the tenant-operator provisioner, the admission allowlist entries, the monitoring rules and the Harbor-only resources of the legacy Pulumi stack. **Supersedes MEET-2026-04-11-048** (the DOCR→Harbor migration). Full record below the table. |

**Retire Harbor; the registry is managed** (2026-09-25). Decided by the project owner.

**Context — what was measured** (#381, `design_pattern_findings#30`). Helmfile installed the
upstream `harbor/harbor` 1.16.2 chart with `infra/helm/harbor/values.yaml`, a values file
written for the wrapper chart and therefore nested under `harbor:`. The upstream chart reads
top-level keys, so every value was ignored: each environment with `profile.harbor: true` —
dev, dev-public, standalone, standalone-multi, standalone-public and **prod** — ran Harbor with
the vendor's documented default admin password, an in-cluster database and Redis, and an
Ingress. None of the hardening MEET-048 described (external `harbor_db`, S3 storage, admin
password from ESO, ClusterIP behind Traefik) was applied. And nothing used it: the api
dual-push mirror (`config.harborRegistryUrl`) and the tenant-operator robot-account
provisioner (`config.harborUrl`) were empty in every environment. A reachable,
default-credential service with zero consumers.

**What was rejected.**
- *Fix Harbor as MEET-048 intended* — point Helmfile at the wrapper chart, move the values,
  wire the admin password from ESO. It repairs the install and still leaves a stateful
  registry (database, Redis, object storage, GC, upgrades, robot-account rotation) that the
  platform operates for no consumer. The AWS re-platform ADR already makes the registry a
  managed L1 primitive.
- *Keep it as a pull-through mirror or DR copy* — the mirror existed and was never turned on;
  a second copy of every image is a need nobody has stated, and ECR replication covers it
  if one appears, without a second system to run.
- *A lighter self-hosted registry (Zot, CNCF distribution)* — the same operational burden in
  a smaller package, and still a registry this repository would have to harden, back up and
  patch. A self-hoster who wants one runs it and hands the engine `REGISTRY_URL`.

**Consequences.**
- The engine carries no registry code: tenant images go to `REGISTRY_URL` only. ECR is
  provisioned in `../loopzero-infra-live` (AWS re-platform ADR, AM5); no ECR code or Terraform
  lives here.
- A values file still setting a retired key (`config.harborRegistryUrl` on loopzero-api,
  `config.harbor*` / `secrets.harborAdminPassword` on loopzero-tenant-operator,
  `registries.harborPrefix` on loopzero-admission-policies) fails at install: the chart schemas
  refuse unknown keys. The binaries no longer read any `HARBOR_*` variable.
- **Vulnerability scanning is a follow-up, not implemented here.** The only image scanner
  anywhere near the pipeline was Harbor's bundled Trivy — disabled in the intended values, and
  running only because those values were ignored; nothing consumed its results. The
  recommendation, in order: first a Trivy scan of the built image in the build pipeline —
  the pipeline already produces a CycloneDX SBOM with syft, which Trivy can scan; later ECR
  enhanced scanning, owned by the registry owner in `../loopzero-infra-live`.
- The per-plan registry storage quota (`HarborQuotaBytes`: 5 GB starter, 50 GB paid,
  unlimited enterprise) was enforced only through Harbor projects and leaves with them. A
  per-tenant storage bound on ECR, if wanted, is a lifecycle policy in `../loopzero-infra-live`.
- **Removing a release from a helmfile does not uninstall it.** On every cluster where Harbor
  ran, the cleanup is manual: `helm uninstall harbor harbor-redis -n loopzero-system`, then delete
  the PVCs the two releases left (`kubectl -n loopzero-system get pvc | grep -i harbor`), the
  Secrets `harbor-admin` and `harbor-redis-credentials`, and any Harbor Ingress still present;
  treat the admin credential as exposed wherever Harbor was reachable. On the legacy Pulumi
  stack the next `pulumi up` plans to delete the container-images bucket (empty it first), the
  `harbor_db` database and the Harbor Valkey cluster; in prod that cluster is `protect: true`
  with `retainOnDelete`, so unprotect it (`pulumi state unprotect`), let `up` drop it from
  state, and delete it in DigitalOcean.

### Code structure

| Date | Decision |
|---|---|
| 2026-09-25 | **`api/internal` is organized by feature.** One axis: each feature's service, HTTP handlers, ports and driven adapters live under `api/internal/<feature>/`; the layer folders `services/`, `routes/`, `adapters/` and `buildplane/` are dissolved into the features. Supersedes the open half of the 2026-09-06 `services/platform` row: its nine concerns are split among the features they serve. Full record below the table. |
| 2026-09-06 | **Rename `api/internal/domain` to `api/internal/ports`**, with the coupled DIP-layer cleanup. |
| 2026-09-06 | **`services/platform` admission criterion — divide by concern.** A package's admission criterion must be able to REFUSE. `api/internal/k8s` shrank from 7 non-test files / 1,520 lines to 3 / 111, with `k8s/{tenancy,workload,secretsync}` as homes, enforced by `loopzero-ops quality package-admission`. `api/internal/adapters` closed via `adapter-ports`. **`api/internal/services/platform` is OPEN** — measurement refuted the suggested name, since every member runs in the api binary rather than the controller, so renaming versus splitting is undecided. |
| 2026-06-11 | **`pkg/domain.EnvRef` value object** (D2) · **`pkg/domain.DefaultPermanentEnv` canonical constant** (D3) · **plan-tier canonical resolution** (D4) · **hot-path complexity decomposition** (D6). |
| 2026-05-21 | **Naming conventions.** Prefer the longer, unambiguous name: `user_metadata` over `metadata`, `content_type` over `type`, `from_date` over `from`. |

**`api/internal` is organized by feature** (2026-09-25).

Measured at `0db5c3dc8`: the top level of `api/internal` mixed layer folders (`services/` with 7
contexts, `routes/` with 26 handler families and 4 shared leaves, `adapters/` with 28 production
files, `ports/`, `buildplane/`) with feature folders (`placement/`, `metering/`). One feature was
reachable under two paths: placement under `placement/` and `services/placement/`, the build under
`services/build/`, `routes/builds/`, `buildplane/` and `adapters/`. The owner chose one axis —
package-by-feature.

*Layout.* A feature is a capability the engine serves. Its root package `api/internal/<feature>`
holds the service and the ports only that feature consumes; its HTTP handlers live in
`<feature>/<name>http` (one package per handler family, so `release` has four); its driven
adapters — code that satisfies a port with persistence, Kubernetes or another feature's service —
live in `<feature>/<feature>adapter`. A single-consumer integration moves under its feature.

| Feature | Root | HTTP | Adapter | Also under it |
|---|---|---|---|---|
| `audit` | action enum, audit-log pruner, `AuditStore` | `audithttp` | — | — |
| `auth` | — | `authhttp` | — | — |
| `billing` | — | `billinghttp` | — | — |
| `bootstrap` | `CellPlacementEngine` | `bootstraphttp` | — | — |
| `builds` | orchestrator, reconcile, reaper, GC, KV reconciler, `AttestationStore` | `buildshttp` | `buildsadapter` | `admission`, `argo`, `shellgen`, `stepwatch`, `cfkv` |
| `domains` | provisioner, health checker, CF reconciler, domain ports | `domainshttp` | `domainsadapter` | `cfzone` |
| `environments` | environment ensurer | `environmentshttp` | `environmentsadapter` | — |
| `health` | — | `healthhttp` | — | — |
| `integrations` | — | `integrationshttp` | `integrationsadapter` | — |
| `logs` | log query ports | `logshttp` | `logsadapter` | `loki` |
| `metering` | usage metering (unchanged) | — | — | — |
| `placement` | engine, router, cell pinner, tenant evictor, orphan detector (`placement/` + `services/placement/` merged) | `placementhttp` | `placementadapter` | — |
| `preview` | preview service, `PreviewService` | `previewhttp` | `previewadapter` | — |
| `projectmetrics` | — | `projectmetricshttp` | — | — |
| `projects` | project deleter and its ports | `projectshttp` | — | — |
| `release` | activator, convergence, promotion, rollback, reaper | `promotehttp`, `rollbackhttp`, `argocdhttp`, `releasehttp` | `releaseadapter` | `argocd` |
| `repositories` | — | `repositorieshttp` | `repositoriesadapter` | — |
| `scaling` | scaling resolver | `scalinghttp` | — | — |
| `secrets` | secret resyncer, `SecretStore`, `SecretK8sSync` | `secretshttp` | — | — |
| `source` | bundle intake (was `buildplane/source`) | `sourcehttp` | — | — |
| `status` | — | `statushttp` | `statusadapter` | — |
| `teams` | `TeamStore` | `teamshttp` | — | — |
| `tenants` | — | `tenantshttp` | — | — |
| `timeline` | `DeployTimelineEvent` | `timelinehttp` | `timelineadapter` | `aggregator` |
| `version` | — | `versionhttp` | — | — |

`routes/admin` was not a feature: its three endpoints are operator surfaces of two features, so
eviction and pinning moved to `placementhttp` and the release inspection to `releasehttp`.
`routes/metrics` became `projectmetrics` because `metrics` is the instrumentation kernel. The build
feature is `builds`, not `build`: this repository treats a directory named `build/` as an artifact
directory — `.dockerignore` excludes `**/build/`, and ten quality gates prune it by name, which is
how `services/build` stayed invisible to them — and a Go package must not share that name.

*Shared kernel* — horizontal by nature, each consumed by more than one feature or by the router:

| Package | Why it is shared |
|---|---|
| `db` (+ `store`, `generated`, `dbtx`, `dbconv`, `dbtest`, `raw`) | one schema, and sqlc generates ONE package that only persistence may import; a build row is read by builds, release, status, logs and preview, so per-feature stores would turn every cross-table read into a feature edge |
| `ports` | value types that cross the persistence ↔ feature seam (db/store implements them), and interfaces consumed by more than one feature |
| `deploycontracts`, `manifests` | the deploy contract every deploying feature and `gitops` share; the manifests it renders |
| `events`, `processing`, `sse` | event payloads, dedup claims and the event stream that build and release write and three HTTP families read |
| `lock`, `leader` | the environment lock (Rule 6) and the fencing primitive |
| `middleware/*`, `httpio`, `pagination`, `plantier`, `routestest` | the request pipeline and the HTTP helpers several families share; `routestest` is test-only |
| `correlation`, `validation`, `scrub` | request correlation, input validation, user-facing scrubbing |
| `metrics`, `surfaces`, `tracing`, `config` | instrumentation and configuration |
| `k8s` (+ `tenancy`, `workload`, `secretsync`) | Kubernetes plumbing and resource managers used by placement, environments, build and preview; the homes are fixed by `package-admission` |
| `gitops`, `gitclient`, `github`, `archive`, `objectstore` | integrations consumed by more than one feature |
| `orchestrationbreaker` | circuit breakers around the Argo Workflows submitter (builds) and the ArgoCD sync client (release), which share one breaker policy |
| `projectstatus` | the project-status mapping the persistence layer applies |

*Composition roots:* `server` (the router) and `backgroundloops` may import any feature and are
imported by nothing inside `api/internal`. `archtest` and `contract` hold tests only.

*The dependency graph is a DAG*, enforced by `loopzero-ops quality feature-fence`:

```
builds   → release, scaling
release  → placement
metering → placement
every other feature → kernel only
kernel   → kernel only
server, backgroundloops → any feature
```

Inside a feature: an HTTP package imports its own feature and the kernel, never another feature,
and nothing but its own subtree, its feature's adapter package and a composition root imports it;
an adapter package is imported by composition roots only; the root never imports its HTTP or
adapter packages. `db/generated` stays reachable from exactly the set that reached it before —
`db/**` and adapter packages — and hand-written `db` stays forbidden in `server` and every HTTP
package, as it was for `routes/`.

**Who is affected.** Anyone editing `api/`: every import path under `api/internal` outside `db`,
`middleware`, `k8s` and the other kernel packages changes. `api/Dockerfile`'s `-ldflags -X` now
names `version/versionhttp`. The gates that named layer paths follow the move.

**What would break.** Nothing at runtime: the change is moves, import rewrites and renames, with no
logic changed. Out-of-tree tooling that hard-codes an old path breaks at build time.

**What was rejected.**
- *Package-by-layer* — keep `services/`, `routes/`, `adapters/` and fold `placement/` and
  `buildplane/` into them. It removes the duplicate paths but keeps one feature spread across
  three trees. The owner chose feature.
- *Handlers in the feature root* — puts `net/http` in the service package, and the fence could no
  longer forbid the service from depending on its transport.
- *Adapters in the HTTP package* — 9 of the 26 adapter files name persistence types (`db.Build`,
  `db.Release`) or call generated queries; since slice-6 `db-generated-boundary` refuses
  hand-written `db` in the HTTP layer, so this would loosen a gate. Moving each port into the root
  instead would put response DTOs such as `BuildStatusResponse` in the service package.
- *Stores per feature* — see the `db` row above.
- *A `platform` feature* — `services/platform` held nine concerns serving six features (audit,
  environments, projects, secrets, placement, builds) plus two breakers and an idempotency cleaner;
  they shared nothing but the directory.

### Testing and tooling

| Date | Decision |
|---|---|
| 2026-09-25 | **One chart per bootstrap/published pair** — every chart that existed twice (a bootstrap copy under `infra/helm/`, installed by Helmfile L2, and a published chart under `charts/`, consumed by ArgoCD L3) becomes the `charts/` chart alone. Helmfile installs it with bootstrap values; the `infra/helm` copy is deleted. The pairs were not two renderings of one design but two charts: the bootstrap `loopzero-api` and `loopzero-cloud` took a free-form env map, owned RBAC the published side withheld, and mounted objects their own Deployments never read. Supersedes the row below for every pair it closes: a divergence between two copies stops being recordable once there is one copy. Pairs closed: `admission-policies`↔`loopzero-admission-policies` (templates were already byte-identical; rendered output of every environment unchanged); `loopzero-cloud` (Helmfile installs `charts/loopzero-cloud` with `values-bootstrap.yaml`; the Deployment selector changes, so an existing Deployment is recreated once); `loopzero-api` (Helmfile installs `charts/loopzero-api` with `values-bootstrap.yaml`; the Deployment selector changes the same way). With no pair left, `helm-drift` and `helm-overlap-parity` lost their subject and were retired with `infra/quality/helm-drift-registry.yaml` and `infra/quality/chart-pairs.txt`; `chart-pairs` now refuses any chart present in both trees, by name or as a template-identical copy. **This also decides the open question of 2026-09-15** — whether the bootstrap loopzero-api ClusterRole keeps cluster-wide `secrets` delete and `pods/exec` create: neither is kept. No code path execs into a pod, so `pods/exec` is gone; `secrets` delete is granted only in the API's own namespace, through a Role behind `rbac.controlPlaneSecrets`, and ArgoCD `applications` patch/delete behind `rbac.argocdApplications`. Both values default off, so an ArgoCD install keeps the published chart's grant; the bootstrap values turn them on. |
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
| ADR-0006 | `api/internal/projects/projectshttp/projects.go:262` | **not in the tree even before the consolidation** |
| ADR-0018 | `api/cmd/controller/setup_metering.go:102` | `metering-compute-loop-plan` |
| ADR-0020 | `cmd/loopzero-ops/internal/quality/prodimagetags/check.go:5` | `2026-05-22-shell-script-strategic-plan` |
| ADR-0022 | `operators/cmd/registration_test.go:125` | **not in the tree even before the consolidation** |
| ADR-0023 | `operators/cmd/cache_scoping_test.go:23` | `2026-05-26-T2.2-cosign-keyless-change-governor-diagnostic` |
| ADR-0024 | `cmd/loopzero-ops/internal/quality/docsrefs/check.go:15` | `docs/decisions.md` |
| ADR-0025 | `pkg/conditions/summary.go:34` | `03-operators` |
| ADR-0028 | `pkg/conditions/aggregate.go:7` | `docs/architecture.md` |
| ADR-0030 | `api/internal/httpio/writer.go:45` | `04-pkg` |
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
