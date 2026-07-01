# Target Architecture Map — loopzero

**Identity:** CLI-first managed Kubernetes-backed cloud (PaaS) for monorepo deployments.
**Scale:** 3,274 source files · ~632K LOC. Dominant: Go (1,375), YAML (581 charts/manifests), Shell (476 ops scripts), TS (167 CLI), Python (204 — `.claude` tooling).
**Pitch:** "Vercel-quality DX for backends" — `loopzero login/init/deploy`, live URL + TLS + previews + rollback, no Kubernetes exposed.

## Four Planes (the organizing architecture)

| Plane | Components | Tech |
|---|---|---|
| Developer | `cli/` (TS Commander, primary interface) ↔ `api/` (Go chi) | TS + Go |
| Control | `operators/` (4 kubebuilder binaries) + Gitea + PG + MinIO + ArgoCD + Traefik + Ory Kratos/Hydra | Go |
| Build | Argo Workflows + BuildKit + cosign + syft | Go/YAML |
| Workload | user apps via ArgoCD + Argo Rollouts (canary) + KEDA, multi-cell | YAML |

## Component inventory (registered)

- **Operators** (`operators/internal/`): `tenant` (lifecycle + multi-cell placement), `app` (LoopZeroApplication deploy), `preview` (TTL preview envs), `platform` (PlatformBootstrap: Gitea/ArgoCD wiring). Split into 4 binaries (ADR-0023). Also: `argocd`, `gitea`, `harbor`, `minio`, `vault`, `metrics`, `conditions`, `httpclient` helpers.
- **API** (`api/internal/`, 33 subpkgs): `routes`, `services`, `placement` (RoundRobin + DynamicRouter), `lock` (pg_advisory_xact_lock), `argo`, `gitops`, `manifests`, `cellmgr`, `source`, `sse`, `billing`-adjacent, `scrub` (internal-term redaction), `admission`.
- **CLI** (`cli/`): 66 commands, `--json` stability contract, exit codes 0–5.
- **Cloud** (`cloud/`): dashboard (React TSX) + cloud-API: Stripe billing, metering, themory client.
- **Shared** (`pkg/`): `statemachine` (11 machines), `errors`/`errors/http` (LoopZeroError typed), `naming`, `enforcement` (plan caps), `vault`, `circuitbreaker`, `id`, `conditions`, `healthcheck`.
- **Infra**: `charts/` (13 OCI) + `infra/helm/` (35 bootstrap) + `infra/helmfile` (L2) + `infra/pulumi` (L1 → migrating to Terraform AWS).

## Cross-cutting invariants
- Every mutation: `withEnvironmentLock` + `pg_advisory_xact_lock` (transaction-scoped only).
- State transitions via state-machine functions only; invalid transition panics.
- Rule 3: no internal terms (ArgoCD/Gitea/CRD/tenant_id) in CLI/`--json` output → `api/internal/scrub`.
- Multi-cell: placement-once-route-many; `tenants.cell_id` canonical SoT; `DynamicRouter.Resolve` pure lookup.

## Entry points
- `cli/src/` → CLI; `api/cmd/` → API server; `operators/cmd/{tenant,app,preview,platform}/main.go` → 4 operator binaries; `cmd/` → `loopzero-ops`.
