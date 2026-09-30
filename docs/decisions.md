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
| 2026-09-29 | **The edge asks the engine which cell serves a host.** Full record below the table. |
| 2026-09-29 | **The production edge reads the host → cell index from its KV store.** Full record below the table. |
| 2026-09-29 | **The secret-fetch broker authenticates each cell by its own token.** Full record below the table. |


**The edge asks the engine which cell serves a host** (2026-09-29, #447).

Placement is decided once and stored in `tenants.cell_id`; an app's route is published
on that cell's ingress. The dev-public edge forwarded every `*.prod-dev` and
`*.preview-dev` host to cell-0's ingress, so an app placed on cell-1 served on its cell
and answered 404 at its public URL.

The engine now answers `GET /internal/edge/route`: the edge sends the host in
`X-Forwarded-Host` with a bearer (`LOOPZERO_EDGE_ROUTE_TOKEN`), and gets `200` plus
`X-LoopZero-Cell`, `404` for a host no route publishes, or `503` while the index has never
loaded. The index is built in memory from the `ExposedRoute` CRs on the management
cluster (hostname, `loopzero.io/tenant`) and `tenants.cell_id` on the cross-tenant pool,
refreshed every 30s and on a miss at most every 5s. A label two tenants publish is
refused rather than guessed. The edge maps the cell to the ingress it knows and proxies
there; Caddy does it with `forward_auth`, a bodyless subrequest, so request bodies are
never replayed.

Rejected:
- **Try cell-0, fall back to cell-1 on 404.** Measured on the droplet: the fallback
  loses a POST body even with request buffering, and it doubles the latency of every
  cell-1 request. It also routes by trial, so a stale route on the wrong cell wins.
- **Encode the tenant in the hostname and look it up at the edge.** `naming.AppHostname`
  joins tenant, project, app and environment with hyphens, each of which may contain
  hyphens, so the tenant cannot be parsed back out. The production Cloudflare Worker
  assumes an `{app}.{tenant}` shape the platform does not emit, filed separately.
- **Publish every route on every cell.** It duplicates ingress state across cells and
  breaks the isolation placement exists for.

Cost: the engine is on the request path of the first lookup for a host after each
refresh window, and an engine outage turns into 503 at the edge for hosts not yet
cached by the edge — Caddy caches nothing, so every request pays one local subrequest.

**The production edge reads the host → cell index from its KV store** (2026-09-29, #448).

The Cloudflare Worker routed a dynamic app by parsing `{app}.{tenant}` out of the host and
reading `cell:{tenant}` → `{cellId, ingressIp}`. The platform never emits that shape, nothing
wrote those keys, and a Worker cannot `fetch` a raw IP — so the path could not route anything.

The engine's edge router now mirrors its index into the same KV namespace the Worker reads:
`route:<label>` for a platform host and `route:<fqdn>` for a custom domain, each →
`{"cellId"}`. It writes only what changed, and removes a route only after a refresh that read
every tenant's placement — a transient read failure never deletes a live route. The Worker
maps the cell to its ingress with `ingress:<cellId>` → `{"resolveOverride"}` and fetches the
request unchanged with `cf.resolveOverride`, so the cell still routes by `Host`. A route whose
cell has no ingress entry answers 503; no route falls through to the default origin.

**Who writes `ingress:<cellId>`:** cell provisioning, once per cell, because the ingress
hostname is a property of the cell's infrastructure and not of any tenant. No provisioner
writes it yet; until one does, a published route answers 503 at the production edge — the
honest state, rather than traffic sent to the wrong cell. The dev-public edge does not use the
Worker and is unaffected (it asks `/internal/edge/route`).

Rejected:
- **The Worker calls `/internal/edge/route`.** Puts the engine on the request path of every
  cold edge location, and exposes an internal endpoint to the public internet.
- **Store the ingress per route.** Duplicates one cell fact into every route and makes an
  ingress change a rewrite of the whole index.
- **Keep parsing the tenant from the host.** Impossible: `naming.AppHostname` joins
  hyphenated parts with hyphens.

Cost: routing converges on the refresh interval (30s) after a placement or route change, and
the KV namespace now holds one key per published host.

**The secret-fetch broker authenticates each cell by its own token** (2026-09-29, #456).

External Secrets on a cell fetches a tenant's secret from loopzero-api's broker, and the broker
may answer only for tenants placed on that cell (EC-07). It stamped every request with
cell-0: the caller's `X-Cell-ID` is rightly ignored (#13), and there was no other source for
the cell, so every tenant on another cell was refused.

Each cell now holds its own bearer (`LOOPZERO_BROKER_TOKENS`, `cell=token` pairs; the old
`LOOPZERO_BROKER_TOKEN` stays the default cell's). The broker compares the presented token with
every configured one in constant time and binds the request to the cell that owns it. loopzero-api
refuses at boot a malformed list, a cell named twice, and two cells sharing a token. The dev
seed reads the cells from the core's ClusterCredentials, issues one token per cell, keeps the
ones already issued, and gives each cell's External Secrets only its own.

Rejected:
- **Trust `X-Cell-ID`.** Any holder of the broker credential could claim any cell (#13).
- **A TokenReview of the calling service account against its cell's API server.** Stronger
  identity, but the broker would need a credential for every cell's API and a network path to
  it on every fetch; a per-cell secret carries the same binding with neither.
- **One shared token with a cell allowlist.** A leaked token would still read every cell.

Cost: one secret per cell to rotate, and a new cell needs its token issued before its External
Secrets can fetch.

**One control plane, and MCP behind agentgateway on the core cluster** (2026-09-30, #470,
RATIFIED sponsor-direct in session: "migrate to the cluster … always the most state-of-the-art
level; no backward compatibility").

dev-public ran a second platform from compose directories that no longer exist on the host:
its own loopzero-cloud, Kratos and Postgres, a Traefik whose forward-auth the repo edge used for
`/v1/memory`, `/v1/rag` and `/v1/lens`, an MCP gateway whose two upstreams were both down,
and promptly, skills, trust and storage. A reboot would have lost all of it, identity was
split between two user databases, and memory data between two stores.

Everything moves to the core cluster through Helmfile, and the compose control plane is
retired. MCP follows the 2026-07-28 authorization spec: every MCP endpoint is an OAuth 2.1
resource server that publishes RFC 9728 metadata and accepts only tokens whose audience is
itself. The gateway is **agentgateway** (Linux Foundation / AAIF, Gateway API conformant):
it serves the protected-resource metadata, validates Hydra-issued JWTs against the audience
of the route, federates the memory, RAG, promptly and skills MCP servers, and authorises
per tool in CEL, hiding from `tools/list` what a caller may not call. Hydra does not
implement RFC 8707, so loopzero-cloud's consent step grants the requested `resource` as the
token audience.

Rejected:
- **Re-anchor the compose stack from a fresh loopzero-workspace clone.** Versioned again, but
  still two control planes, two identity stores and a split memory — and it contradicts
  Rule 10 of CLAUDE.md ("no Docker Compose shortcuts").
- **Finish and deploy our own `loopzero-traefik-mcp` gateway.** Its README lists every milestone
  (MG1–MG4, including RFC 9728 metadata and JWT) as planned. agentgateway ships all four;
  building them is the reinvention Unbreakable Principle 9 forbids.
- **Envoy AI Gateway.** Also spec-compliant, but it needs Envoy Gateway as its base; the
  core cluster runs neither, and agentgateway carries its own data plane.

Cost: a new control plane component (agentgateway, Gateway API CRDs 1.6) on the core
cluster; every MCP server must accept a Hydra JWT and derive its tenant from a claim;
loopzero-cloud's consent step gains audience binding; and clients holding `loopzero_live_*` keys
for MCP must switch to OAuth — there is no backward compatibility by decision.

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
### Identity

| Date | Decision |
|---|---|
| 2026-09-30 | **One token issuer: Hydra, asymmetric and audience-bound — retire the HS256 session token** (#470) **RATIFIED 2026-09-30, sponsor-direct** (proposed and ratified the same day; T3). MCP (2026-07-28 spec) needs every resource server to validate a token issued for it on its own. Measured today: `loopzero-contracts/jwt` accepts RS256 Hydra access tokens (`AllowedAccessAlgs`) **and** HS256 session tokens loopzero-cloud signs with a shared secret (`AllowedSessionAlgs`, `SessionTokenIssuer`); the CLI's stored token is the HS256 one (`iss: loopzero-cloud`, top-level `tenant_id`), which nothing outside loopzero-cloud can verify without holding its secret. Proposal: every user and machine token comes from Hydra (RS256 + JWKS; device grant for the CLI, already wired in `loopzero-cloud/internal/routes/device.go`; authorization code + PKCE for the dashboard; client credentials for agents), carrying `tenant_id` via consent (mirrored top-level with `oauth2.allowed_top_level_claims`) and an audience per resource — loopzero-cloud's consent grants the RFC 8707 `resource` as audience, since Hydra does not; the HS256 issuer and validator are deleted. Third-party MCP clients (consent screen, Client ID Metadata Documents) are a follow-up on top. Rejected: loopzero-cloud signing its session tokens with ES256 and publishing a JWKS — two authorization servers, two revocation paths, two places audiences are decided. It changes the token every client holds, and there is no backward compatibility by decision. |

### Registry

| Date | Decision |
|---|---|
| 2026-09-30 | **Two image classes, two homes** — platform images are built once by our CI and replicated, signed and by digest, into the registry of every region that runs a cell; tenant (application) images are built and stored only in the registry of the tenant's own region. The runtime registry is always the region's cloud registry (ECR, Artifact Registry, ACR); GHCR is never a runtime dependency and never holds tenant images. Full record below the table. |
| 2026-09-25 | **Retire Harbor; the registry is managed** — ECR on AWS, provisioned in `../loopzero-infra-live`; a self-hoster brings their own registry through `REGISTRY_URL`; locally `k3d-loopzero-registry` stays. Harbor leaves the engine entirely: the Helmfile releases, the wrapper chart, the api dual-push mirror, the tenant-operator provisioner, the admission allowlist entries, the monitoring rules and the Harbor-only resources of the legacy Pulumi stack. **Supersedes MEET-2026-04-11-048** (the DOCR→Harbor migration). Full record below the table. |

**Two image classes, two homes** (2026-09-30, #470). Ratified by the project owner.

The platform runs in several regions, each on its cloud — Europe on AWS, Brazil on Google Cloud,
the United States on Azure — and its images are private. Two kinds of image cross that boundary,
and their requirements point in opposite directions, so they are decided separately.

| | **Platform images** | **Tenant images** |
|---|---|---|
| What | our software: loopzero-api, the operators, loopzero-cloud, the dashboard, the data plane (memory, rag, lens, promptly, skills, trust, storage) | the application a customer deploys, built by the build plane on each `loopzero deploy` |
| Who builds | our CI (GitHub Actions), once per version | the build plane (BuildKit + cosign) of the tenant's region |
| How many | a few, versioned | one per deploy per tenant, short-lived |
| Where they must live | in **every** region that runs a cell | **only** in the tenant's region — data residency |
| Who may write | the CI, through OIDC, from `develop`/`main` only | only that region's build plane, into that tenant's namespace |
| Retention | by release | by deploy and retention policy |

**Decision.**
- **The runtime registry is the region's cloud registry** — ECR in an AWS region, Artifact
  Registry in a Google Cloud region, ACR in an Azure region — for both classes. A cluster pulls
  only from the registry of its own region, with the cloud's native workload identity; no
  cluster depends on GitHub to start a pod.
- **Platform images are replicated.** The CI builds once and pushes the same digest to each
  regional registry, federated through OIDC (no stored registry credential), and signs and
  verifies each copy with keyless cosign. Admission verifies by digest.
- **Tenant images are resident.** The build plane of a region pushes to that region's registry
  and nowhere else. They never go to GHCR.
- **Inside a regional registry the classes are separate namespaces** — `platform/*` written only
  by the CI, `tenants/<tenant>/*` written only by that region's build plane — each with its own
  access policy and lifecycle rules.
- **GHCR stays a development copy of the platform images**, not a runtime dependency. Removing it
  is a separate decision, taken after measuring what still pulls from it (the dev-public
  `loopzero-db` image and the loopzero-cloud edge e2e did on 2026-09-30).
- A self-hoster still brings their own registry through `REGISTRY_URL` (2026-09-25 below).

**What was rejected.**
- *Centralise on GHCR and pull from it at runtime.* With private images every regional cluster —
  or a pull-through cache in front of it — needs a GitHub credential, and GHCR accepts only a
  classic personal access token for packages: long-lived, tied to a user, stored in every
  cloud. Production would also depend on GitHub's availability to start a pod. And tenant code
  would leave its region.
- *A pull-through cache per region with GHCR upstream.* It suits public images; with private
  ones it carries the same stored credential into every region. Pushing from CI with OIDC
  stores none.
- *One global registry for tenant images.* It breaks data residency and puts every tenant's
  code behind one blast radius.

**Consequences.**
- Today the CI publishes platform images to GHCR and to ECR (the AWS region). A new region adds
  its registry as one more target of `.github/workflows/build-publish.yml` when that region is
  provisioned — not before (the input is `ecr_registry` today and becomes a list then).
- Tenant images on dev-public still go to `k3d-loopzero-registry`; the per-region tenant registry is
  provisioned with the cells (#448, `../loopzero-infra-live`).

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

Measured at `33285e697`: the top level of `api/internal` mixed layer folders (`services/` with 7
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
| 2026-09-25 | **One verdict contract for the quality gates.** A gate's check returns its own outcome, which states its verdict (`Pass`, `Fail`, `Skip`, `NotVerified`) through `verdict.Result`; the error it returns means only "could not measure"; the quality runner is the one place a verdict becomes an exit status. Full record below the table. |
| 2026-09-26 | **A gate over the gitignored kit skips when the kit is absent.** `skill-suites`, `skill-script-paths`, `records-tree-split` and `ecosystem-validators` check `.claude/` and `.squad/`, which `.gitignore` excludes, so every fresh clone lacks them. Three of the four reported that as NOT VERIFIED (exit 5) and one as advisory (exit 0), so `task quality:gates` failed on every clone for something the repository does not contain. Owner decision: all four `Skip` (exit 0) and print the reason, never a pass; on a machine with the kit they run in full. Rejected: exit 5 in all four (red forever outside machines with the kit, which teaches people to ignore red); dropping them from `quality:gates` (nobody would run them). Kit validation proper belongs to the kit repository's own CI. |
| 2026-05-21 | **External tooling evaluations.** Recommendations recorded and adopted per tool. |

**One verdict contract for the quality gates** (2026-09-25).

*Context.* Measured at `f87f2e3b6`: `loopzero-ops quality` registers 143 gates, one adapter file
each in `cmd/loopzero-ops/internal/qualitycli`, all run by one runner (`Gate.Run`, since
`83a931f08`). The runner was already single; the verdict was not. The 137 check packages under
`internal/quality/` expose their result in 16 return shapes — `(Outcome, error)` ×67,
`(*Outcome, error)` ×49, `*Outcome` ×5, `(Verdict, error)` ×3, `(Report, error)` ×3 and eleven
more — and carry the verdict through five channels: a `Pass` boolean (read by 70 adapters), a
violation reported as a sentinel **error** (42 adapters list the sentinels by hand through
`isViolation`), a `Skipped` flag (12), a `Warn` flag (8), and `OK()` / `Failed()` methods (3
each). Every adapter re-derived the verdict from its check's channel and returned `Pass` or
`Fail` by hand: 173 `return Pass` and 145 `return Fail` sites. What "skipped" means was decided
per adapter — 19 adapters carry a SKIP or ADVISORY path that exits 0, three gates over the gitignored kit exit 5 when
it is absent, and `ecosystem-validators` exits 0 on the same absence. Two defects sat in the
runner path itself: the legacy `Outcome` zero value **was** `Pass`, so an outcome nobody set
passed; and a check that panicked exited with the Go runtime's status 2, outside loopzero-ops'
exit table (0/1/5). No adapter swallowed an error into exit 0 — the runner already mapped every
error to 5.

*Decision.*
- `internal/quality/verdict` holds the vocabulary: `Pass` (exit 0), `Fail` (1), `Skip` (0 — the
  rule was not evaluated here for a reason the gate's policy accepts and its report states: an
  absent optional subject, or an advisory gate's missing tool), `NotVerified` (5 — a subject
  that exists could not be verified on this machine). The zero value is not a verdict and exits 5.
- A check keeps its own outcome type — its tests assert on it and its report prints it — and
  that type implements `verdict.Result` (`Verdict() verdict.Verdict`) in the check's own
  package, tested there. The check's `error` means only "could not measure": a violation is a
  result whose verdict is `Fail`, never a sentinel error.
- `qualitycli.Check` returns `(verdict.Result, error)`. `verdict.Verdict` does not implement
  `Result`, so an adapter cannot return a verdict it chose; it returns the outcome the check
  produced and only renders it. The runner is the one place a verdict becomes an exit status; a
  nil result, an undecided verdict and a panic all exit 5.

*Alternatives rejected.*
- *One `Result` struct for every check* (`Check(ctx) Result` with findings and a summary line,
  rendered by the runner). The 143 reports are not one shape — headers, remediation footers,
  PARTIAL notes, markdown audits, `::error::` annotations — and they are read by people and
  scripts today. A common struct either loses that information or grows a free-form text field
  that re-opens the hand-written verdict. It would also discard the typed outcomes the check
  tests assert on.
- *Keep the per-gate outcome and map it in the adapter with a helper.* That is the state
  measured above: the mapping stays hand-written 143 times, next to the rendering instead of the
  data, and a new sentinel in a check silently becomes an exit 5 in its adapter.
- *A fifth `Advisory` verdict.* Its exit status and its report are those of `Skip`; the
  difference is the reason the report prints, which the gate already states.

*Migration.* The contract landed with the runner and the vocabulary; every adapter compiled
against it through a transitional `Outcome` (Pass/Fail) that implemented `Result`, and the gates
still deciding by hand were listed exactly in a test ledger that could only shrink. All 143 gates
moved in six batches, each running the previous and the new binary on the same tree and comparing
exit code, stdout and stderr; the transitional `Outcome`, `isViolation`, `errNotVerified` and the
ledger were then deleted. What keeps the contract is now structural: a `verdict.Verdict` is not a
`verdict.Result`, so a `Check` cannot return one, and `qualitycli/contract_test.go` refuses a
`Verdict` method declared in the adapter package. Reports stayed byte-identical except where the old
behaviour was a defect, each fixed test-first: five gates printed `FAIL` over a run that exited 5
(three with a missing chart, `render-schema-gen-sql --render`, `route-auth-coverage` with no route
enumerated), and `skill-script-paths` and `skill-suites` reported a kit file they could not read as
a violation (exit 1) instead of an incomplete check (exit 5). `coverage-merge` and
`helm-diff-structural` report a missing required flag through the runner's usage path (same exit 1).
One inconsistency is left as found, because the check documents it as a choice: with the kit absent,
`ecosystem-validators` is advisory (`Skip`, exit 0) while the three other kit gates are
`NotVerified` (exit 5).

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
