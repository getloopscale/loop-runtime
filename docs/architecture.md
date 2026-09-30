# Architecture

The shape of the engine. `CLAUDE.md` at the repo root is the authoritative operating
contract; this document is the system view that does not fit there.

**The code is authoritative.** Where this file and the code disagree, the code wins.

## What this repository is

LoopZero is a CLI-first managed cloud for monorepo deployments. **This repo is the ENGINE** —
API, operators, shared Go libs, Helm charts, infra. It is not the whole product.

| Extracted | Lives in | At |
|---|---|---|
| TypeScript CLI | `../loopzero-cli` | M2 |
| Control plane, billing, dashboard | `../loopzero-cloud` | M4 |
| `jwt` · `serviceauth` · `plan` contracts | `../loopzero-contracts` | M3 |

If you are looking for a CLI command, a dashboard screen or Stripe billing, it is not
here. Do not re-implement it.

## The four planes

```
DEVELOPER PLANE   ../loopzero-cli (TypeScript)  <->  api/ (Go)
CONTROL PLANE     core cluster: 4 operators, Gitea, PG, MinIO, ArgoCD, Traefik, cert-manager
BUILD PLANE       build cluster: Argo Workflows + BuildKit + cosign signing
WORKLOAD PLANE    cell cluster(s): user apps via ArgoCD, Traefik, cert-manager, KEDA,
                  Argo Rollouts (canary)
SECRETS           separate loopzero-secrets cluster: OpenBao (Transit) + ESO. CONDITIONAL.
```

`task setup` creates **4** k3d clusters (`infra/k3d/{core,build,cell-0,cell-1}-cluster.yaml`).
OpenBao runs in a 5th, separate cluster. Installing it into core is a topology violation, not a
shortcut: the `k3d:secrets:bootstrap-in-core` task that did it was **removed on 2026-08-12**
(B-047), and the `openbao-bootstrap-in-core-task` gate now fails on its existence — or on the
equivalent effect under another name.

**The 5th cluster is conditional.** Three releases in `secrets.helmfile.yaml.gotmpl`
(`openbao-cert`, `openbao`, `openbao-init`) are gated on `openbaoEnabled`, set in **1 of 8**
environment files (`vault-local.yaml`). A fourth, `openbao-snapshot-agent`, is gated on a
**different** flag — `snapshotAgentEnabled` — also defaulting to false. The secret backend is per-installation
by contract (`charts/loopzero-api/SECRETS.md`) — a self-hoster bringing their own store runs a
4-cluster topology. Decision: `docs/decisions.md` § secrets cluster.

## Multi-module Go workspace

`go.work` (Go 1.26.1) unions **9 modules**, one of which is a sibling repository:

```
../loopzero-contracts   <- SIBLING REPO. Absent -> the workspace does not build.
./api  ./pkg  ./operators  ./operators/api  ./cmd/loopzero-ops
./infra/scripts  ./infra/tests  ./tools/gen-service-auth-ed25519
```

There is **no `go.mod` at the repo root**. `go build ./...` from the root does not cover
the repo — run per module, or use the Taskfile.

| Path | What |
|---|---|
| `api/internal/` | Organized by feature (`docs/decisions.md`, 2026-09-25): 25 features, each a root package with `<name>http` handlers and a `<feature>adapter` package, over a shared kernel (`db`, `ports`, `deploycontracts`, `middleware`, `sse`, `lock`, `k8s`, …) and two composition roots (`server`, `backgroundloops`); `loopzero-ops quality feature-fence` enforces the registry |
| `operators/` | 4 split binaries (`cmd/{tenant,app,preview,platform}-operator`), kubebuilder + controller-runtime |
| `operators/api/` | CRD Go types (own module) |
| `pkg/` | Shared libs: `statemachine`, `errors`, `naming`, `conditions`, `enforcement`, `circuitbreaker`, `healthcheck`, `id`, `logging`, `domain`, `events`, `vault`, `shellrun`, `pulumi`, `correlation`. The only module with no in-repo dependencies; a change here reaches four modules. |
| `cmd/loopzero-ops/` | Ops + quality binary: the wired quality gates plus `seed` / `drill` / `pulumi` / `chaos`. A leaf — nothing imports it. |
| `infra/helm/` | Bootstrap charts, installed by Helmfile L2 |
| `charts/` | Published OCI charts, consumed by ArgoCD L3; use the `loopzero-common` library |
| `infra/tests/` | Ginkgo e2e, chaos, load (k6), unit. Imports four other modules — first place a cross-module break shows. |

## Deploy flow (happy path)

```
loopzero deploy -> CLI validates -> upload bundle -> API locks env -> submit Argo Workflow
  -> build (BuildKit) -> cosign sign -> push image -> generate manifests
     (Rollout + 2 Services + TraefikService + AnalysisTemplate when ProgressiveDelivery=true;
      legacy Deployment when LOOPZERO_PROGRESSIVE_DELIVERY=false)
  -> push to Gitea -> ArgoCD sync -> Argo Rollouts orchestrates canary steps
     (per-plan: Starter 50->100, Pro 20->Analysis->50->100,
      Team/Enterprise 10->Analysis->30->60->100)
  -> SSE rollout.step during canary; rollout.aborted (exit 3) on Degraded/Aborted
  -> app Healthy -> SSE deploy.completed -> CLI shows URL
  (static apps: build -> upload S3 -> write CF KV -> CF Worker serves from DO Spaces)
```

## CRDs (8, all wired)

Tenant · Environment · LoopZeroApplication · ExposedRoute · PlatformBootstrap ·
GitRepositoryBinding · ClusterCredential · PreviewEnvironment.

The 2 historical orphan CRDs (AppSecret, ReconciliationReport) were deleted 2026-05-13.

## State machines (11, `pkg/statemachine/`)

Build · Release · Tenant · PlatformBootstrap · Preview · Promotion · Rollback · Domain ·
Environment · Application · ExposedRoute. Each has a `*_typed.go` companion.

**The code is the spec** — read it for the full state set. Never set status directly; use
`Transition(event)`. Invalid transitions **panic** (programming error, not runtime error).
The `.dot` diagram sources were removed in the 2026-09-11 consolidation; recover them with
`git show 4f728eb7a:docs/state-machines/`.

## Single-status-writer invariant (operator reconcilers)

A CRD's `.status` has exactly ONE writer, persisting with a **merge patch off a base
snapshot** taken right after the initial `Get`:

```go
base := obj.DeepCopy()          // right after r.Get, BEFORE mutating .Status
// ... mutate obj.Status ...
r.Status().Patch(ctx, &obj, client.MergeFrom(base))
```

A merge patch carries no `resourceVersion`, so it never 409s against a concurrent
spec/metadata write by another controller. **Never** use the refetch-then-restore
`Status().Update` dance (lost update), and **never** `return ctrl.Result{}, nil` on a
status-write failure — return the error and requeue.

Reference: `operators/internal/environment/environment_controller.go` + its
`_conflict_test.go`.

## Multi-cell — placement once, route many

- **Placement is sticky and one-time.** `loopzero bootstrap` runs
  `RoundRobinEngine.AssignAndExecute` (`api/internal/placement/engine.go`) inside a
  SERIALIZABLE txn with `pg_advisory_xact_lock`; bin-packing picks the cell with fewest
  tenants under `maxPerCell`. Writes `tenants.cell_id` (canonical SoT) + Tenant CR
  `spec.cellID`.
- **Routing is pure lookup.** `DynamicRouter.Resolve(cellID)`
  (`api/internal/placement/dynamic_router.go`) is a hash lookup returning kubeconfig,
  ArgoCD URL+token and base domain.
- **`loopzero admin tenants pin` is the only path that moves a tenant** — DB update + Tenant CR
  MergePatch atomically, writes `audit_log`.
- **`ClusterCredential` CRDs are looked up by `spec.cellID`, NOT `metadata.name`.** A CR
  named `workload` with `spec.cellID=cell-0` is valid.
- **No automatic rebalancing, by design**: DNS continuity, state affinity, hot-spot as an
  upgrade-tier signal, auditability.

**Capacity default:** `MAX_TENANTS_PER_CELL` is `envInt("MAX_TENANTS_PER_CELL", 5000)`
(`api/internal/config`) plus `charts/loopzero-api/values.yaml`. The `maxPerCell = 500` in
`api/internal/placement/engine.go` is the constructor **fallback**, not the delivered value. Citing the
guard instead of the config once inflated a capacity simulation tenfold and invented a
`cell_count` bottleneck that does not exist.

## Helm chart dual system

`infra/helm/` (bootstrap, Helmfile L2) and `charts/` (published OCI, ArgoCD L3) coexist by
design, and **no chart exists in both trees**. `loopzero-api`, `loopzero-cloud` and the admission
policies are one chart each under `charts/`; Helmfile installs them with
`charts/<name>/values-bootstrap.yaml` (`values-mgmt.yaml` for the admission policies on
core), and ArgoCD consumes the same chart with its defaults (`decisions.md`, "One chart
per bootstrap/published pair"). The `chart-pairs` gate refuses a chart present in both
trees, by name or as a template-identical copy under another name.

`monitoring` / `loopzero-monitoring` are complementary (infra vs config), not overlapping.

<!-- BEGIN GENERATED TABLE -->
| Chart | Cluster(s) | Nature | Version | Conditional |
|---|---|---|---|---|
| `argo-ci-namespace` | build | ours | — | `always` |
| `argo-events` | build | ours | — | `always` |
| `argo-events-bus` | build | ours | — | `always` |
| `argo-rollouts` | cell | ours | — | `always` |
| `argo-workflows` | build | upstream | 1.0.5 | `always` |
| `argocd` | core | upstream | 7.3.11 | `always` |
| `blackbox-exporter` | core | upstream | 9.2.0 | `always` |
| `cert-manager` | cell, core | upstream | — | `always` |
| `chaos-mesh` | core | ours | — | `{{ .Values \| get "installChaosMesh" false }}` |
| `cloudflared` | cell | ours | — | `always` |
| `cluster-issuer` | cell, secrets | ours | — | `always` |
| `data-plane-service` | core | ours | — | `{{ has $svc ($.Values \| get "dataPlane.services" list) }}` |
| `external-secrets` | cell, core | upstream | 0.14.3 | `always` |
| `gateway-api-crds` | core | ours | — | `always` |
| `gitea` | core | upstream | 10.6.0 | `always` |
| `k6-operator` | — | undeclared | — | `always` |
| `keda` | cell | upstream | 2.16.1 | `always` |
| `keda-http-addon` | cell | upstream | 0.8.0 | `{{ .Values.profile.kedaHttpAddon }}` |
| `loki` | build | upstream | 6.24.0 | `always` |
| `minio` | core | upstream | 5.4.0 | `always` |
| `monitoring` | cell, core | upstream | 3.12.1 | `{{ .Values \| get "installMetricsServer" false }}` |
| `network-policies` | cell | ours | — | `always` |
| `nfs-server-provisioner` | core | upstream | 1.8.0 | `always` |
| `openbao-cert` | secrets | ours | — | `{{ .Values \| get "openbaoEnabled" false }}` |
| `openbao-init` | secrets | ours | — | `{{ .Values \| get "openbaoEnabled" false }}` |
| `openbao-snapshot-agent` | secrets | ours | — | `{{ .Values \| get "snapshotAgentEnabled" false }}` |
| `opencost` | core | ours | — | `always` |
| `ory-hydra` | core | upstream | 0.61.0 | `always` |
| `ory-kratos` | core | upstream | 0.61.0 | `always` |
| `otel` | core | upstream | 0.96.0 | `always` |
| `policy-controller` | cell | upstream | 0.8.1 | `always` |
| `postgresql` | core | upstream | 18.6.2 | `{{ .Values.inClusterPostgres \| default false }}` |
| `promtail` | build, cell | upstream | 6.16.6 | `always` |
| `redis` | core | upstream | 19.6.4 | `{{ not .Values.profile.managedRedis }}` |
| `sigstore-extras` | — | undeclared | — | `always` |
| `smoke-test` | core | ours | — | `{{ and .Values.profile.smokeTest (ne (.Values \| get "clusterRole" "dev") "standalone") }}` |
| `static-assets` | cell | ours | — | `{{ .Values.profile.staticAssets }}` |
| `tempo` | core | upstream | 1.14.0 | `always` |
| `loopzero-admission-policies` | cell, core | ours | — | `always` |
| `loopzero-api` | core | ours | — | `{{ ne (.Values \| get "clusterRole" "dev") "standalone" }}` |
| `loopzero-app-operator` | core | ours | — | `{{ ne (.Values \| get "clusterRole" "dev") "standalone" }}` |
| `loopzero-build-cwts` | build | ours | — | `always` |
| `loopzero-cloud` | core | ours | — | `{{ and .Values.profile.loopzeroCloud (ne (.Values \| get "clusterRole" "dev") "standalone") }}` |
| `loopzero-crds` | core | ours | — | `always` |
| `loopzero-external-secrets` | cell | ours | — | `{{ .Values.profile.secretFetchBroker }}` |
| `loopzero-platform-operator` | core | ours | — | `{{ ne (.Values \| get "clusterRole" "dev") "standalone" }}` |
| `loopzero-preview-operator` | core | ours | — | `{{ ne (.Values \| get "clusterRole" "dev") "standalone" }}` |
| `loopzero-tenant-operator` | core | ours | — | `{{ ne (.Values \| get "clusterRole" "dev") "standalone" }}` |
| `traefik` | cell, core | upstream | 28.3.0 | `always` |
| `velero` | core | ours | — | `always` |
<!-- END GENERATED TABLE -->

## Container images — two classes, two homes

One region per cloud (Europe on AWS, Brazil on Google Cloud, the United States on Azure), private
images, and two classes with opposite requirements. The full record, with the alternatives
rejected, is in `docs/decisions.md` ("Two image classes, two homes", 2026-09-30).

| | Platform images | Tenant images |
|---|---|---|
| What | our software (loopzero-api, operators, loopzero-cloud, dashboard, data plane) | the customer's application, one per `loopzero deploy` |
| Built by | our CI, once per version | the build plane of the tenant's region |
| Lives in | **every** region's cloud registry (replicated, same digest, signed) | **only** the tenant's region's cloud registry (data residency) |
| Namespace | `platform/*`, written only by CI via OIDC | `tenants/<tenant>/*`, written only by that region's build plane |

A cluster pulls only from its own region's cloud registry, with native workload identity; the CI
pushes platform images there through OIDC and signs each copy with keyless cosign, and admission
verifies by digest. GHCR is a development copy of the platform images, never a runtime dependency,
and never holds tenant images.

## Infrastructure — AWS re-platform in progress

CTO mandate 2026-06-17. The **L1 cloud-primitives layer** is being re-platformed
DigitalOcean/Pulumi -> AWS/Terraform, into two sibling repos: `../loopzero-infra-modules`
(versioned blueprints) + `../loopzero-infra-live` (per-env Terragrunt units). Tooling is
**Terraform**, not OpenTofu, not Pulumi.

`infra/pulumi*` is **AS-IS legacy — live until the AM9 cutover, and retired at AM10**, per
`../loopzero-workspace/docs/decisions/2026-06-17-infra-state-decomposition-aws.md`. Do not cite
Pulumi as the forward path, and do not add new Pulumi for target infra. This rule is
unconditional and carries no date. The in-cluster platform layer is unchanged:
Helmfile L2 + ArgoCD L3 + charts + k3d dev all stay.

Layering today: **L1** cloud primitives (Pulumi/DO legacy -> Terraform/AWS target) ·
**L2** Helmfile (SoT for the platform releases) · **L3** ArgoCD (tenant Day-2 GitOps,
manual sync, from Gitea).

## Cross-repo contracts

The decomposition seams are HTTP/OpenAPI contracts, each with a consumer-driven test.

| Seam | Consumer | Provider | Contract |
|---|---|---|---|
| cli↔engine | loopzero-cli | loopzero | `../loopzero-cli/tests/contract/engine.yaml` (vendored OpenAPI snapshot) + engine-side `api/internal/contract/cli_consumer_test.go` |
| cloud↔engine | loopzero-cloud | loopzero | opaque `/engine/*` proxy + Ed25519 service-auth |
| cloud↔loopzero-data | loopzero-cloud | loopzero-data | `../loopzero-cloud/openapi/themory.yaml` |
| engine self-conformance | — | loopzero | `api/openapi/engine.yaml` |

```bash
task quality:contract:cross-repo
```

Absent siblings are SKIPPED with an explicit message, never a false PASS. The
`acyclic-graph` gate enforces dependency DIRECTION; this matrix enforces seam CONTRACTS.

<!-- USAGE-MATRIX-START -->

## Real Usage Matrix

Table generated by grepping for `import "github.com/getloopscale/loopzero/pkg/X"`.
Regenerate with `task pkg:usage-matrix:write` (`loopzero-ops quality pkg-usage-matrix --write`).
The `task pkg:usage-matrix:check` gate fails when the table is out of date (gate PKG-G14).

| Package | Total imports | api/internal | operators/internal | cloud | cli | Other |
|---|---:|---:|---:|---:|---:|---:|
| `pkg/circuitbreaker` | 10 | 2 | 6 | 0 | 0 | 2 |
| `pkg/conditions` | 3 | 0 | 2 | 0 | 0 | 1 |
| `pkg/correlation` | 13 | 3 | 8 | 0 | 0 | 2 |
| `pkg/domain` | 34 | 19 | 3 | 0 | 0 | 12 |
| `pkg/enforcement` | 24 | 19 | 0 | 0 | 0 | 5 |
| `pkg/errors` | 144 | 116 | 0 | 0 | 0 | 28 |
| `pkg/gitearepo` | 14 | 8 | 3 | 0 | 0 | 3 |
| `pkg/healthcheck` | 5 | 0 | 0 | 0 | 0 | 5 |
| `pkg/id` | 7 | 6 | 0 | 0 | 0 | 1 |
| `pkg/logging` | 12 | 7 | 0 | 0 | 0 | 5 |
| `pkg/naming` | 58 | 40 | 13 | 0 | 0 | 5 |
| `pkg/statemachine` | 88 | 72 | 11 | 0 | 0 | 5 |
| `pkg/vault` | 7 | 2 | 0 | 0 | 0 | 5 |

<!-- USAGE-MATRIX-END -->

## Related

- [`loopzerokit-loopzerocloud-seam.md`](loopzerokit-loopzerocloud-seam.md) — **mandatory pre-read** before
  touching `services_json`, the upload contract or the manifest emitter
- [`api-contracts.md`](api-contracts.md) — wire protocols
- [`decisions.md`](decisions.md) — the ADR record
