---
type: Component
title: "Stateful platform services"
description: "Postgres, Gitea, MinIO, Harbor, OpenBao, Loki, Tempo, Prometheus and two Redis instances — everything that holds state."
resource: "infra/helmfile"
tags:
  - postgres
  - gitea
  - minio
  - harbor
  - openbao
  - storage
sources:
  - id: pd-pg
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L220-L231"
    note: "PostgreSQL"
  - id: pd-gitea
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L259-L264"
    note: "Gitea (GitOps git server)"
  - id: pd-minio
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L299-L304"
    note: "MinIO (S3-compatible object store)"
  - id: pd-harbor
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L438-L448"
    note: "Harbor (container registry)"
  - id: pd-bao
    resource: "infra/helmfile/secrets.helmfile.yaml.gotmpl#L118-L122"
    note: "OpenBao (secrets + Transit)"
  - id: pd-loki
    resource: "infra/helmfile/build.helmfile.yaml.gotmpl#L157-L160"
    note: "Loki (log store)"
  - id: pd-tempo
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L404-L407"
    note: "Tempo (trace store)"
  - id: pd-prom
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L369-L372"
    note: "Prometheus (metrics TSDB)"
  - id: pd-pool
    resource: "api/internal/db/pool.go#L38"
    note: "pgxpool over DATABASE_URL"
  - id: pd-argo-read
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L356"
    note: "ArgoCD L3 syncs tenant Day-2 state FROM the Gitea repositories the API pushes to — the two halves of the GitOps loop"
  - id: pd-needs
    resource: "infra/helmfile/core.helmfile.yaml.gotmpl#L265-L282"
    note: "helmfile `needs:` when inClusterPostgres is true"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

Ten stateful releases. Eight sit on the control plane, Loki on the build plane, OpenBao alone in
the secrets cluster.

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `ds.postgres` | `infra/helmfile/core.helmfile.yaml.gotmpl` | datastore | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:220-231` | Relational store for the platform (loopzero, gitea, kratos, hydra DBs). Helmfile release 'postgresql', chart bitnami/postgresql 18.6.2, installed only when .Values.inClusterPostgres is true — the cloud profiles point at a managed Postgres instead.  [merged from duplicate infra.postgres: bitnami/postgresql 18.6.2 in loopzero-system. CONDITIONAL: `installed: {{ .Values.inClusterPostgres \| default false }}` — the comment above it states the in-cluster chart exists only for the standalone/self-hosting profile and that LoopZero Cloud uses DO Managed PG via Pulumi L1. It is the system of record: 47 tables and 33 foreign keys per the phase-1 schema extract.] |
| `ds.gitea` | `infra/helmfile/core.helmfile.yaml.gotmpl` | datastore | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:259-264` | Helmfile release 'gitea', chart gitea-charts/gitea 10.6.0. Holds the generated deployment manifests that ArgoCD syncs from. needs: traefik, gitea-redis, and nfs-server-provisioner on the HA profiles.  [merged from duplicate infra.gitea: gitea-charts/gitea 10.6.0. Holds the per-tenant GitOps repositories ArgoCD syncs from. Declares needs on traefik, gitea-redis and (when inClusterPostgres) postgresql.] |
| `ds.minio` | `infra/helmfile/core.helmfile.yaml.gotmpl` | datastore | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:299-304` | Helmfile release 'minio', chart minio/minio 5.4.0. Source-bundle and artifact storage; in cloud profiles the same role is played by DigitalOcean Spaces.  [merged from duplicate infra.minio: minio/minio 5.4.0. Stores uploaded source bundles under loopzero-builds/{tenant}/. Distributed mode (4 pods x 4 drives EC:2) applies only to the dev/prod environments; standalone runs single-node with a deterministic root password.] |
| `ds.harbor` | `infra/helmfile/core.helmfile.yaml.gotmpl` | datastore | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:438-448` | Helmfile release 'harbor', chart harbor/harbor 1.16.2. Image scanning + offline cache. Disabled in drill because its RWO PVCs block rolling helm upgrades.  [merged from duplicate infra.harbor: Per-tenant projects and robot accounts, provisioned by operators/internal/harbor.] |
| `ds.openbao` | `infra/helmfile/secrets.helmfile.yaml.gotmpl` | datastore | l2-platform | `infra/helmfile/secrets.helmfile.yaml.gotmpl:118-122` | Helmfile release 'openbao', chart openbao/openbao 0.28.3, gated on `installed: {{ .Values \| get "openbaoEnabled" false }}` — default OFF. Runs in a dedicated cluster by topology decision.  [merged from duplicate infra.openbao: Lives in the separate secrets cluster. Releases in secrets.helmfile.yaml.gotmpl carry conditional `installed:` flags, so the composition is gated rather than always on.] |
| `ds.loki` | `infra/helmfile/build.helmfile.yaml.gotmpl` | datastore | l2-platform | `infra/helmfile/build.helmfile.yaml.gotmpl:157-160` | Helmfile release 'loki', chart grafana/loki 6.24.0, installed on the build cluster in the monitoring namespace; Promtail ships into it.  [merged from duplicate infra.loki: Build-plane log aggregation; api/internal/logs/loki queries it for build output.] |
| `ds.tempo` | `infra/helmfile/core.helmfile.yaml.gotmpl` | datastore | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:404-407` | Helmfile release 'tempo', chart grafana/tempo 1.14.0. |
| `ds.prometheus` | `infra/helmfile/core.helmfile.yaml.gotmpl` | datastore | l2-platform | `infra/helmfile/core.helmfile.yaml.gotmpl:369-372` | Helmfile release 'prometheus', chart prometheus-community/prometheus 25.27.0. |
| `infra.redis.gitea` | `infra/helmfile/core.helmfile.yaml.gotmpl` | cache | infrastructure | `infra/helmfile/core.helmfile.yaml.gotmpl:245` | bitnami/redis 19.6.4. Not installed in the `drill` environment, which uses DO Managed Valkey instead. [reconciled: a duplicate coarse node ds.redis, which stood for both redis releases at once, was removed; its edges were repointed to the release each one actually belongs to] |
| `infra.redis.harbor` | `infra/helmfile/core.helmfile.yaml.gotmpl` | cache | infrastructure | `infra/helmfile/core.helmfile.yaml.gotmpl:426` | A SECOND bitnami/redis release, for Harbor, with the same drill exclusion as gitea-redis. [reconciled: a duplicate coarse node ds.redis, which stood for both redis releases at once, was removed; its edges were repointed to the release each one actually belongs to] |

# Relationships

- **Postgres is the source of truth** for every user-visible fact. The API reaches it through a
  `pgxpool` over `DATABASE_URL`;[^pd-pool] the 48 tables are
  [`/entities/database-schema.md`](/entities/database-schema.md).
- **Gitea is the GitOps store, and both halves of the loop are here**: the API pushes rendered
  manifests into `{tenant}-gitops`, and ArgoCD reads them back out.[^pd-argo-read] Neither side
  talks to the other directly.
- **MinIO** is the standalone S3 target for source bundles and static assets; the same uploader
  targets DigitalOcean Spaces in the cloud profile.
- **Harbor** is the image registry, with per-tenant projects and robot accounts provisioned by
  the tenant operator.
- **OpenBao** provides Transit; `pkg/vault` and `operators/internal/vault` are its two clients,
  and it is the one datastore outside the four-cluster core — see
  [`/components/cluster-topology.md`](/components/cluster-topology.md).
- **Loki, Tempo and Prometheus** back the observability surface in
  [`/components/platform-runtime-services.md`](/components/platform-runtime-services.md);
  `api/internal/logs/loki` queries Loki for build logs.
- Ordering is declared, not assumed: Gitea `needs` Redis, Traefik and Postgres; Harbor `needs`
  MinIO and its own Redis.[^pd-needs] The two Redis releases are separate instances of the same
  chart, one per consumer.

# Evidence

[^pd-pg]: `infra/helmfile/core.helmfile.yaml.gotmpl:220-231`
[^pd-gitea]: `infra/helmfile/core.helmfile.yaml.gotmpl:259-264`
[^pd-minio]: `infra/helmfile/core.helmfile.yaml.gotmpl:299-304`
[^pd-harbor]: `infra/helmfile/core.helmfile.yaml.gotmpl:438-448`
[^pd-bao]: `infra/helmfile/secrets.helmfile.yaml.gotmpl:118-122`
[^pd-loki]: `infra/helmfile/build.helmfile.yaml.gotmpl:157-160`
[^pd-tempo]: `infra/helmfile/core.helmfile.yaml.gotmpl:404-407`
[^pd-prom]: `infra/helmfile/core.helmfile.yaml.gotmpl:369-372`
[^pd-pool]: `api/internal/db/pool.go:38`
[^pd-argo-read]: `infra/helmfile/core.helmfile.yaml.gotmpl:356`
[^pd-needs]: `infra/helmfile/core.helmfile.yaml.gotmpl:265-282`
