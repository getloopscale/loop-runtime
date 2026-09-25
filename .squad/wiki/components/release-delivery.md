---
type: Component
title: "Release delivery — GitOps push, ArgoCD sync, activation"
description: "Turns a succeeded build into a release row, a commit in the tenant GitOps repository, and an ArgoCD sync confirmed before activation."
resource: "api/internal/services/release"
tags:
  - release
  - gitops
  - argocd
  - rollback
sources:
  - id: rd-rel
    resource: "api/internal/services/release/argo_app_label_reader.go#L1"
    note: "services/release"
  - id: rd-gitops
    resource: "api/internal/gitops/gitea_pusher.go#L3"
    note: "gitops"
  - id: rd-argocd
    resource: "api/internal/argocd/sync_client.go#L1"
    note: "argocd"
  - id: rd-man
    resource: "api/internal/manifests/renderer.go#L3"
    note: "manifests"
  - id: rd-dom
    resource: "api/internal/services/domain/domain_cf_reconciler.go#L1"
    note: "services/domain"
  - id: rd-prev
    resource: "api/internal/preview/preview_service.go#L1"
    note: "services/preview"
  - id: rd-push
    resource: "api/internal/gitops/gitea_pusher.go#L174"
    note: "git push origin HEAD:refs/heads/main into the {tenant}-gitops repository over HTTP with the credential supplied via http.extraHeader rather than in the URL. Thi"
  - id: rd-clone
    resource: "api/internal/gitops/gitea_pusher.go#L116"
    note: "git clone --depth 5 of the tenant gitops repo into a temp dir on every push; the rollback path additionally reads manifests at a specific historical commit, whi"
  - id: rd-sync
    resource: "api/internal/argocd/sync_client.go#L190"
    note: "POST /api/v1/applications/{app}/sync with {\"prune\": true} against the ArgoCD API, bearer-authenticated, preceded by a best-effort hard refresh to drop the repo-"
  - id: rd-outbox
    resource: "api/internal/services/release/release_activator.go#L230"
    note: "transactional outbox: one row per terminal deploy transition, written in the SAME transaction as the state change. loopzero-cloud drains it; this repository never r"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

The half of the deploy path that runs after the image exists. It writes a release row, renders
manifests, pushes them to the tenant's Gitea repository, asks ArgoCD to sync, and only then —
after a convergence loop confirms Synced + Healthy + the right revision — flips the release
`ACTIVE` and emits `deploy.completed`.[^rd-rel]

Delivery is deliberately split into three phases around the lock: **A** insert the release
under the lock, **B** push to git with no lock and a 60s budget, **C** record the result under
the lock again.

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `api.services.release` | `api/internal/services/release` | package | application | `api/internal/services/release/argo_app_label_reader.go:1` | Release convergence; imports db, db/store, events, metrics, ports, processing, deploycontracts. |
| `api.gitops` | `api/internal/gitops` | package | infrastructure | `api/internal/gitops/gitea_pusher.go:3` | Commits rendered manifests into the tenant's Gitea repo, plus a manifest gate. |
| `api.argocd` | `api/internal/argocd` | package | infrastructure | `api/internal/argocd/sync_client.go:1` | ArgoCD HTTP sync client plus a serializer and a token provider that mints a Kubernetes SA token rather than holding a static one. |
| `api.gitclient` | `api/internal/gitclient` | package | infrastructure | `api/internal/gitclient/runner.go:3` | The single place exec.CommandContext runs git, with credential sanitisation. Three consumers: buildplane/source, gitops, github. |
| `api.manifests` | `api/internal/manifests` | package | application | `api/internal/manifests/renderer.go:3` | Renders Rollout/Deployment, Services, IngressRoute/TraefikService and AnalysisTemplate from typed Go structs. Its only third-party import is sigs.k8s.io/yaml — no client-go, no cluster access. |
| `api.services.domain` | `api/internal/services/domain` | package | application | `api/internal/services/domain/domain_cf_reconciler.go:1` | Custom-domain services; imports db, manifests, metrics, ports, deploycontracts. |
| `api.services.preview` | `api/internal/preview` | package | application | `api/internal/preview/preview_service.go:1` | Preview environment services. |
| `api.services.platform` | `api/internal/services/platform` | package | application | `api/internal/services/platform/audit_log_pruner.go:1` | Platform-level services; imports cfkv, db, db/store, metrics, ports, deploycontracts. |
| `api.services.placement` | `api/internal/services/placement` | package | application | `api/internal/services/placement/cell_pinner.go:1` | Placement service. Distinct from api/internal/placement, and the name collision is real enough that api/cmd/setup_server.go aliases it as placementsvc in the one file that imports both. |

## Measured size

| Component | loc | file count | afferent coupling | efferent coupling | instability | commits | commits last 90d |
|---|---|---|---|---|---|---|---|
| `api.gitops` | 1105 | 4 | 2 | 3 | 0.6 | 28 | 4 |
| `api.argocd` | 3261 | 12 | 2 | 3 | 0.6 | 46 | 7 |
| `api.gitclient` | 455 | 4 | 3 | 0 | 0 | 5 | 0 |
| `api.manifests` | 5468 | 22 | 1 | 1 | 0.5 | 40 | 12 |

# Relationships

- `api.gitops` **clones** `{tenant}-gitops` at depth 5, writes
  `{environment}/{projectID}/manifests.yaml`, and **pushes** to `refs/heads/main`, rebasing
  and retrying up to 3 times on conflict.[^rd-clone][^rd-push]
- Before any git work it **validates** the manifest set and rejects resource kinds known to
  wedge an ArgoCD sync — a `Certificate` CR once caused a 50-minute production stall, and that
  incident is why the check exists.
- `api.argocd` **hard-refreshes** the application to drop the repo-server's cached comparison
  error, then POSTs `/api/v1/applications/{app}/sync` with `{"prune": true}`.[^rd-sync]
  The refresh is not cosmetic: without it ArgoCD returns `InvalidSpecError: project X does not
  exist` for about five minutes against a project that does exist.
- `api.manifests` renders a `Rollout` plus two Services, a `TraefikService` and an
  `AnalysisTemplate` when progressive delivery is on, and a plain `Deployment` when it is off —
  images pinned by **digest**, never by tag.[^rd-man]
- `api.services.release` **publishes** one row per terminal deploy transition into
  `deploy_events`, in the same transaction as the state change. This is a transactional outbox
  that the extracted `../loopzero-cloud` repository drains; **this repository never reads it
  back**.[^rd-outbox]
- ArgoCD reads the other side of that loop — see
  [`/components/platform-orchestrators.md`](/components/platform-orchestrators.md).
- Release state is the 12-state machine in
  [`/entities/state-machine-release.md`](/entities/state-machine-release.md), the largest in the
  system; rollback is [`/entities/state-machine-rollback.md`](/entities/state-machine-rollback.md).
- The full ordering is [`/flows/deploy-container-app.md`](/flows/deploy-container-app.md),
  steps 28 to 40.

## Rollback is a replay, not a rebuild

`api.gitops` also reads manifests at a **specific historical commit**, which is what makes a
revert a replay of what was already deployed rather than a fresh render.[^rd-clone] The call
waits for ArgoCD to confirm the sync before reporting success — a rule added after a live
incident where rollback returned success while pods still served the rolled-back code.

# Evidence

[^rd-rel]: `api/internal/services/release/argo_app_label_reader.go:1`
[^rd-gitops]: `api/internal/gitops/gitea_pusher.go:3`
[^rd-argocd]: `api/internal/argocd/sync_client.go:1`
[^rd-man]: `api/internal/manifests/renderer.go:3`
[^rd-dom]: `api/internal/services/domain/domain_cf_reconciler.go:1`
[^rd-prev]: `api/internal/preview/preview_service.go:1`
[^rd-push]: `api/internal/gitops/gitea_pusher.go:174`
[^rd-clone]: `api/internal/gitops/gitea_pusher.go:116`
[^rd-sync]: `api/internal/argocd/sync_client.go:190`
[^rd-outbox]: `api/internal/services/release/release_activator.go:230`
