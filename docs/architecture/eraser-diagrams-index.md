---
title: "Architecture Diagrams — Eraser Workspace Index"
generated: { by: process:architecture-diagram-cross-validation-session, at: 2026-07-01 }
name: eraser-diagrams-index
description: Index of the Eraser architecture diagrams (17 views, incl. a unified System Landscape entry point + multi-cell placement/routing + AWS-first target runtime & IaC decomposition + exposure surfaces). AWS-first target is the default frame. with per-view scope + code evidence; cross-validated 2026-07-01. Core deploy flow + supply-chain/observability/product-surface/memory/MCP-gateway/promptly + META Model B fabric + LoopZeroKit build-source seam + CI (Argo Events) + DR (Velero).
type: index
created_at: 2026-07-01
updated_at: 2026-07-01
updated_by: architecture diagram cross-validation session
---

# Architecture Diagrams — Eraser Workspace Index

Living index of the LoopZero architecture diagrams maintained as **diagram-as-code** in
the Eraser workspace **"LoopZero"**. Each view is a scoped projection of the system,
cross-validated against the codebase (evidence cited as `file:line` or chart path).

> **Workspace:** <https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX>
> **Format:** Eraser cloud-architecture / flowchart DSL (editable, versionable).
> **Last cross-validation:** 2026-07-01 — see [§ Cross-validation status](#cross-validation-status).

## Why multiple views

A single "everything" diagram at this system's size (~25 core nodes + 6 subsystems)
becomes unreadable. Each diagram is a **projection with a declared scope** — the
Core shows the deploy flow, the numbered views zoom into one subsystem each. Every
component is detailed in **exactly one** view; the others reference it (`see View N`)
rather than duplicating it (DRY applied to diagrams — one source of truth per
component, so a refactor updates one place).

## Conventions

- **Arrow color = data-flow phase** (Core diagram): `1 Request` blue · `2 Auth/State`
  purple · `3 Build` green · `4 GitOps` red · `5 Serve` orange · `D Dev-public (Caddy)` gray.
- **Node color = component family** across all views: blue = data/network/frontend ·
  purple = auth/broker · green = build/service · red = store/security/admission ·
  orange = edge/Argo tooling · gray = actor/placeholder.
- **Icons:** only icons verified to render in Eraser are used; where a brand logo is
  absent from Eraser's set (e.g. Gitea, AWS ECR), a generic icon carries the visual
  and the **label carries the exact identity**. Never a blank ("careca") box.

## The diagrams

| # | View | Scope | Eraser link |
|---|------|-------|-------------|
| 0 | **View 0 — System Landscape** (START HERE — unified) | one node per subsystem + backbone flows; each node links to its detailed view | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=s-ci3Adv491YD3aVX2VZ) |
| 1 | **Core — Four Planes** | End-to-end deploy across Developer/Control/Build/Workload planes + Cloudflare edge + Caddy dev-public overlay | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=tn6Qr6K22quNtVtfQ7iB) |
| 2 | **Deploy Flow** (flowchart) | `loopzero deploy` happy path with decision points (409 concurrent, canary abort, static vs container) | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=7OnhuAetxBfLdhYbelaV) |
| 3 | **View 2 — Supply Chain + Admission + Secrets** | sign → SBOM → registry → admission gate → workload; OpenBao → ESO → Secret | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=lJyqHQeqkpMLEedFdlwp) |
| 4 | **View 3 — Observability** | metrics + logs + traces + SLO alerts + SSE | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=JAhyAPVr9o6-m1ButgCf) |
| 5 | **View 4 — Product Surface** | Engine API vs loopzero-cloud BFF path-split + Stripe + GitHub App | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=osacG3oh3zhF-pDPVB4Y) |
| 6 | **View 5 — LoopZero Memory** | proxy + Model B credential broker + MCP server | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=tfF0r9troda6hc1omuB7) |
| 7 | **View 6 — MCP Gateway** (`loopzero-traefik-mcp`) | MCP-protocol-aware reverse proxy behind Traefik | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=8Vxm72xcQMolVhaReb1Z) |
| 8 | **View 7 — Promptly** | symmetric twin of View 5 (same Model B fabric) | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=bwSIu0fHzwMVtFY2-hDr) |
| 9 | **View 8 — META: Model B Fabric** | the shared credential-broker pattern; Memory + Promptly + future as instances | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=QKKFg9LcX87mMKJityhr) |
| 10 | **View 9 — LoopZeroKit ↔ LoopZeroCloud seam** | the build-source contract (`.loopzero/services.json` v2 on the wire — the only LoopZeroKit↔LoopZeroCloud coupling) | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=9yBkYmHJA4YLLfJ3QVEm) |
| 11 | **View 10 — CI (Argo Events)** | Gitea-webhook-triggered quality gates + 4 CronWorkflows (replaced GitHub Actions) | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=_7SRLue771vwW8XPZMFb) |
| 12 | **View 11 — DR / Backup (Velero)** | 5 backup schedules → S3 (CRDs / ArgoCD / Gitea / Secrets / PV snapshots) | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=Zv9bXzxScKz2TXByu6qJ) |
| 13 | **View 12 — Multi-Cell Placement & Routing** | placement-once (RoundRobin + advisory lock) / route-many (DynamicRouter) over N cells via ClusterCredential — ENABLED, always-on | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=zYXKMb-xBs_0PeZ6Fj89) |
| 14 | **View 13 — AWS-first Runtime (TARGET)** | DO→AWS runtime mapping (EKS/RDS+Proxy/ElastiCache/S3/ECR+GHCR/native-AWS edge) with Class A vs Class B distinction | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=4tCeWmxL1v3_tEMxzUWN) |
| 15 | **View 14 — AWS-first IaC Decomposition** | loopzero-infra-live Terragrunt units (bootstrap→security→networking→data/compute→edge→dns); per-component state; RDS own protected state | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=qpJBL6yNB_jpR12h6zzW) |
| 16 | **View 15 — Exposure (AWS target)** | control-plane hub (BFF single front door, engine internal) + customer-app fan-out (Traefik Class B over N cells); OD7 custom-domain deferred | [open](https://app.eraser.io/workspace/Wnkh9NNFB3yV6712CgTX?diagram=K7mtHXx0SZ0DzrXIdjT7) |

## Per-view code evidence

Evidence verified during the 2026-07-01 cross-validation. Paths are relative to the
`loopzero/` repo unless marked as a sibling repo.

### 1 — Core (Four Planes)
- 8 operators: `operators/internal/{tenant,environment,application,preview,exposure,platformbootstrap,gitrepositorybinding,clustercredential}_controller.go`
- env lock: `pg_advisory_xact_lock` — `api/internal/lock/` (LoopZero Design Rule 7)
- build: `api/internal/argo/{buildkit,cosign}.go`
- source bundle store = MinIO; static assets store = **DO Spaces** (`api/internal/s3/uploader.go:4`, `api/internal/routes/build_sbom.go:63` bucket `loopzero-static-assets`) — the two are distinct (B1 fix)
- registry: `RegistryURL` primary (`api/internal/argo/workflow_submitter.go:58`), Harbor secondary dual-push (`:61`, MEET-048); **ECR** is the AWS-first target (ADR 2026-06-17, OD5)
- Caddy dev-public: `infra/local/Caddyfile` (behind CF Tunnel, LE TLS)

### 3 — View 2 (Supply Chain + Admission + Secrets)
- cosign keyed/keyless: `api/internal/argo/cosign.go:46-48` (`Keyless bool`, Fulcio/Rekor deferred)
- admission: `infra/helm/policy-controller/`, `infra/helm/admission-policies/` (`Chart.yaml:10` — `loopzero-images-warn` mode: warn), `infra/helm/network-policies/`
- ESO: `infra/helm/external-secrets/`, `infra/helm/loopzero-external-secrets/`

### 4 — View 3 (Observability)
- instrumentation: `api/internal/{metrics,sse,tracing,correlation,loki}`
- stack charts: `infra/helm/{monitoring,loki,promtail,tempo,otel,blackbox-exporter}` (monitoring = Prometheus + Grafana + kube-state-metrics)
- metric count: ~112 `loopzero_*` (CLAUDE.md measurement, not re-counted here)

### 5 — View 4 (Product Surface)
- engine API: `api/` (served at `/engine/*`)
- loopzero-cloud BFF: sibling repo `../loopzero-cloud/` (served at `/v1/*`) — path-split at edge per `infra/local/Caddyfile`
- billing/metering: `../loopzero-cloud/internal/billing/`, `../loopzero-cloud/internal/metering/{tracker,reporter,reconciler}.go`
- GitHub App: `api/internal/github/{webhook,autodeploy,preview_deploy,status_notifier}.go`

### 6 — View 5 (LoopZero Memory)
- BFF proxy: `../loopzero-cloud/internal/routes/memory.go` (List/Metrics/Forget/Entities/Graph), `../loopzero-cloud/internal/themory/{broker,admin_client,client}.go`
- Model B broker: `../loopzero-cloud/internal/themory/broker.go` (LRU+TTL, singleflight, fail-closed; `minter` interface = DIP)
- config: `../loopzero-cloud/internal/config/config.go:95-116` (`THEMORY_BASE_URL`, `THEMORY_PLATFORM_ADMIN_KEY`)
- product: sibling repo `../loopzero-memory/` (TS, Postgres+pgvector, 4-verb API, REST+MCP+CLI surfaces; MCP OpenMemory-compat over SSE)

### 7 — View 6 (MCP Gateway)
- sibling repo `../loopzero-traefik-mcp/` — "LoopZero MCP Gateway", Go reverse proxy behind Traefik OSS ([`README.md`](../README.md))
- flow: `MCP client → Traefik (routes /mcp/*) → Gateway [authenticate → inspect JSON-RPC → authorize per-tool/per-arg → filter tools/list] → upstream MCP servers`
- reuses Model B credential fabric; `defaultAction = deny`; fail-closed; **pre-v1.0**

### 8 — View 7 (Promptly)
- BFF proxy: `../loopzero-cloud/internal/promptly/{broker,admin_client,scope_translate}.go` — mirrors themory (M11, Model B **L2**)
- differences vs memory: native `/admin/keys` (no `/v1`), `promptly_live_*`, scope `promptly:read`, `scope_translate.go`, MintResult `{id, secret, keyPrefix}`
- forward-auth: `../loopzero-cloud/internal/routes/forward_auth.go:21-25` (Traefik `forward-auth-promptly` middleware → `/internal/authz/forward-promptly`)
- config: `../loopzero-cloud/internal/config/config.go:118-130` (`PROMPTLY_BASE_URL` empty disables the broker)

### 9 — View 8 (META — Model B Fabric)
- generic reuse: `ForwardAuthHandler` reused per product (`../loopzero-cloud/internal/routes/forward_auth.go`), `minter` interface (DIP) in each broker
- OCP at platform level: a new proxied product = a new **instance** (config), not a fabric change

### 10 — View 9 (LoopZeroKit ↔ LoopZeroCloud seam)
- wire contract: `api/internal/routes/source.go:119` (multipart `services_json` part MUST be first)
- schema: `api/internal/source/services.schema.json` (v2)
- CLI producer: `../loopzero-cli/src/adapters/{services-json,source-bundle,source-upload,app-unit}.ts`
- renderer: `api/internal/manifests/renderer.go` + `services_input_mapping.go`
- canonical doc: [`docs/architecture/loopzerokit-loopzerocloud-integration.md`](./loopzerokit-loopzerocloud-integration.md)

### 11 — View 10 (CI — Argo Events)
- charts: `infra/helm/argo-events`, `infra/helm/loopzero-ci-workflows` (`Chart.yaml` — EventSource Gitea webhook + Sensor PR trigger + quality-gate WorkflowTemplate + 4 CronWorkflows; ADR-2026-05-27-T0.2)
- HMAC webhook secret via ExternalSecret (`gitea-webhook-secret`); mirrors `task quality:gates`
- replaced GitHub Actions (removed 2026-05-23)

### 12 — View 11 (DR / Backup — Velero)
- chart: `infra/helm/velero` (`Chart.yaml` — CRDs / ArgoCD state / Gitea metadata / Secrets / full snapshots)
- `infra/helm/velero/values-schedules.yaml` — 5 backup schedules (ADR D3), retention-differentiated, to S3

### 13 — View 12 (Multi-Cell Placement & Routing)
- placement (once): `api/internal/placement/engine.go:77` (RoundRobinEngine, bin-packing; `maxPerCell` **=5000** via `config.go:497` — o `500` em `engine.go:91` é o fallback para entrada `<= 0`, não o valor entregue), `:61-70` (`pg_advisory_xact_lock` `PlacementLockID` + SERIALIZABLE)
- routing (many): `api/internal/placement/dynamic_router.go` (`Resolve` pure hash lookup; `:100-105` refuse-to-clear-on-0-cells invariant)
- discovery: `api/internal/placement/k8s_cell_store.go:52` (`LoadCells` from ClusterCredential CRDs where `status.connected=true`; lookup by `spec.cellID`)
- cells seeded: `api/internal/db/migrations/000006_cells.up.sql:22` (cell-default + cell-1 + cell-2; `tenants.cell_id` DEFAULT cell-default)
- **enabled, always-on** (not feature-flagged); wired in `api/cmd/{main,setup_background,setup_server,setup_workflows}.go`. Closes cross-validation finding M1.

### 14 — View 13 (AWS-first Runtime — TARGET)
- ADR: `decisions/2026-06-17-infra-state-decomposition-aws.md` (D5 native-AWS edge; Class A = Terraform-created vs Class B = controller-created in-cluster)
- `ROADMAP_AWS.md` AM5 (EKS + node groups + LB Controller IRSA), AM4 (RDS + **RDS Proxy** + ElastiCache + S3), AM6 (ECR runtime + GHCR distribution — OD5), AM7 (edge ALB + CloudFront + WAF + ACM us-east-1 + Route53; dashboard static S3+CloudFront, **Cloudflare rejected**, decision 2026-06-18)
- RDS Proxy pinning-safe: loopzero is pooler-safe by design (`environment_lock.go:88`, `SET LOCAL`/`set_config(...,true)` only) — `ROADMAP_AWS.md` AM4 DoD
- **This is the TARGET** (strangler-fig AM0–AM9); DO stays LIVE until the AM8 cutover.

### 15 — View 14 (AWS-first IaC Decomposition)
- ADR D3: per-component Terragrunt state isolation — units `bootstrap · security/{kms,iam,secrets} · networking/vpc · data/{rds,cache,storage} · compute/eks · edge · dns`; **RDS = own protected state** (`prevent_destroy` + deletion protection)
- repos: `loopzero-infra-modules/modules/{networking,security}`, `loopzero-infra-live/{bootstrap,iam-bootstrap,sandbox/us-east-1/sandbox/{networking,security}}` — early (AM1–AM3 in progress); full unit set is the target
- state backend: S3 SSE-KMS + native lock + strict IAM + secrets-as-references (values never in state) — ADR D6

### 16 — View 15 (Exposure — AWS target)
- **Control-plane hub** — BFF is the SINGLE front door: serves `/v1/*` itself + PROXIES `/engine/*` to the engine API (`loopzero-cloud/cmd/main.go:724,821`; `config.go:56` `EngineAPIURL`). Engine API is a ClusterIP — never internet-facing. Auth: Kratos session → JWT. (Corrects the earlier V4/V0 depiction that showed `Edge → Engine` directly — it's `Edge → BFF → Engine` proxy.)
- **Customer-app fan-out** — shared public ALB (Class A) → CloudFront → in-cluster Traefik ingress (Class B, `operators/.../exposedroutes.yaml` + `application_controller.go`) → per-tenant route → wildcard `*.{cell-domain}` per cell. Per-service routing stays Class B (ADR D5 — controller-created, changes every deploy).
- **Open gap:** custom-domain-per-customer on AWS (Route53+ACM equivalent of the DO CF-Zone flow, `api/internal/domain/types.go:49-57`) is **OD7 DEFERRED** (ADR line 62) — not yet decided.

## Cross-validation status

Verdict (2026-07-01): the diagram set is **cross-validated against code and internally
consistent**. History:

- **Initial Core verdict:** `NOT_UP_TO_DATE` (~65%) — 1 factual error + 5 missing subsystems.
- **B1 (factual error)** — static assets were drawn to MinIO; corrected to **DO Spaces**
  (MinIO re-wired to its real source-bundle role).
- **H1–H5 (missing subsystems)** — observability, engine/BFF split, ESO, admission,
  GitHub App — each now covered by a dedicated view (V2–V4).
- **loopzero-memory / promptly / MCP gateway** — added as V5–V8 after a review-scope gap
  was surfaced (the domain matrix had no "memory/MCP" reviewer).

### Known transitional drift (conscious, not an error)

The Core diagram is a **hybrid**: registry = ECR (AWS-first, ADR 2026-06-17), remaining
infra = DigitalOcean (LIVE until the AM8 cutover, strangler-fig `ROADMAP_AWS.md`
AM0–AM9). It is neither "100% current DO" nor "100% AWS target" by design. The AWS-first
target is now realized as **View 13 (runtime architecture)** + **View 14 (IaC decomposition)** —
the Core stays the "current state (transition)" while V13/V14 depict where the AM8 cutover lands.

## Maintaining these diagrams

Because they are diagram-as-code, "is the diagram up to date?" is a **verifiable**
question, not an opinion: fetch each diagram's DSL from Eraser and re-grep the cited
`file:line` evidence. Re-run the cross-validation when a subsystem changes materially
(new subsystem, registry cutover, broker pattern change).
