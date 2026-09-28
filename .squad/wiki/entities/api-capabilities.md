---
type: Entity
title: "What the API lets a user do — twelve capabilities"
description: "The 64 HTTP routes grouped by the user-visible thing they accomplish, rather than by package."
resource: "api/internal/routes"
tags:
  - api
  - http
  - capability
  - entity
sources:
  - id: cap-manage_projects
    resource: "api/internal/server/router.go#L389"
    note: "Create and operate projects"
  - id: cap-deploy
    resource: "api/internal/server/router.go#L487"
    note: "Deploy a revision and watch it go live"
  - id: cap-observe
    resource: "api/internal/server/router.go#L549"
    note: "See what the platform is doing"
  - id: cap-custom_domains
    resource: "api/internal/server/router.go#L633"
    note: "Attach a custom domain with TLS"
  - id: cap-manage_environments
    resource: "api/internal/server/router.go#L392"
    note: "Declare and remove environments"
  - id: cap-manage_secrets
    resource: "api/internal/server/router.go#L463"
    note: "Set per-environment secrets"
  - id: cap-manage_team
    resource: "api/internal/server/router.go#L254"
    note: "Invite and role-manage teammates"
  - id: cap-preview_environments
    resource: "api/internal/server/router.go#L389"
    note: "Get an ephemeral environment per pull request"
  - id: cap-admin_operate
    resource: "api/internal/server/router.go#L141"
    note: "Operate the fleet as an administrator"
  - id: cap-billing
    resource: "api/internal/server/router.go#L296"
    note: "See and change the subscription"
  - id: cap-rollback
    resource: "api/internal/server/router.go#L579"
    note: "Revert to a previous release in one call"
  - id: cap-upload_source
    resource: "api/internal/server/router.go#L359"
    note: "Hand the platform a source bundle"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

The HTTP surface read as **capabilities** — what a user can accomplish — rather than as route
packages. Twelve of them, covering 64 routes.

Project management alone is 22 routes, nearly a third of the whole API.

# Schema

| Capability | Routes | Anchor |
|---|---|---|
| **Create and operate projects** | 22 | `api/internal/server/router.go:389` |
| **Deploy a revision and watch it go live** | 9 | `api/internal/server/router.go:487` |
| **See what the platform is doing** | 6 | `api/internal/server/router.go:549` |
| **Attach a custom domain with TLS** | 5 | `api/internal/server/router.go:633` |
| **Declare and remove environments** | 4 | `api/internal/server/router.go:392` |
| **Set per-environment secrets** | 4 | `api/internal/server/router.go:463` |
| **Invite and role-manage teammates** | 4 | `api/internal/server/router.go:254` |
| **Get an ephemeral environment per pull request** | 4 | `api/internal/server/router.go:389` |
| **Operate the fleet as an administrator** | 2 | `api/internal/server/router.go:141` |
| **See and change the subscription** | 2 | `api/internal/server/router.go:296` |
| **Revert to a previous release in one call** | 1 | `api/internal/server/router.go:579` |
| **Hand the platform a source bundle** | 1 | `api/internal/server/router.go:359` |


## Create and operate projects

The largest surface in the API: list/create/get/delete a project, its builds, build-envs, status, metrics, logs stream, signals stream, previews, promote, rollback, scale, and the GitHub connect binding. 22 routes — nearly a third of the whole API.


## Deploy a revision and watch it go live

POST /api/v1/builds submits a build; GET /api/v1/builds/{id}/stream is the SSE channel the CLI blocks on until deploy.completed. Also: cancel, logs, logs/stream, attestation, sbom, stats. 9 routes.


## See what the platform is doing

health, health/ready, health/deep, the audit log, the per-deploy 5-phase timeline, and version. The deep health probe is what distinguishes 'the API is up' from 'the API can reach Postgres'.


## Attach a custom domain with TLS

list / get / create / verify / delete, plus DNS-record management for Cloudflare-managed zones. v2 works by nameserver delegation — the API returns the CF nameservers and the domain sits in pending_ns until the user changes them at their registrar.


## Declare and remove environments

list / get / create / delete. Creation is also implicit: the first deploy to an unknown environment triggers EnsureEnvironment from inside the build handler.


## Set per-environment secrets

list / set / bulk-set / delete. Postgres is the source of truth; the Kubernetes ExternalSecret is written best-effort on the same call and its failure is logged rather than returned.


## Invite and role-manage teammates

list / add / change-role / remove members, plus team switching on the auth surface. 4 routes.


## Get an ephemeral environment per pull request

list / create / get / delete previews under a project, driven by the PreviewEnvironment CRD and the Preview state machine, with a single-slot source of truth in the preview_slot table.


## Operate the fleet as an administrator

POST /api/v1/admin/tenants/pin — the only path that moves a tenant between cells — and GET /api/v1/admin/releases/{id}/inspect. Gated behind LOOPZERO_ADMIN_API=true as defence in depth on top of service auth.


## See and change the subscription

GET /api/v1/billing and POST /api/v1/billing/portal. Thin by design — Stripe itself, the webhook handling and the checkout session live in the extracted ../loopzero-cloud repository, not here.


## Revert to a previous release in one call

POST /api/v1/projects/{name}/rollback. Backed by the Rollback state machine (8 states, 5 of them terminal) and by re-pushing the target release's manifests read out of Gitea history — the previous manifests are replayed rather than rebuilt.


## Hand the platform a source bundle

POST /api/v1/source/upload — multipart, hard-capped at 500MB by MaxBytesReader with 32MB held in memory and the rest spooled to disk. Carries services_json, the canonical LoopZeroKit wire field.


# Relationships

- Served by the chi router in
  [`/components/control-plane-api.md`](/components/control-plane-api.md); the 29 route packages
  are forbidden from importing each other, and a gate verifies that none does.
- `cap.deploy` is the entry point of
  [`/flows/deploy-container-app.md`](/flows/deploy-container-app.md); the experience of using
  this surface end to end, including the four places it answers 200 while work is outstanding,
  is [`/flows/developer-first-app-live.md`](/flows/developer-first-app-live.md).
- `cap.admin_operate` is the only path that moves a tenant between cells — see
  [`/flows/tenant-cell-placement.md`](/flows/tenant-cell-placement.md) — and it is gated behind
  `LOOPZERO_ADMIN_API=true` as defence in depth on top of service auth.
- `cap.billing` is deliberately thin: Stripe, the webhook handling and the checkout session live
  in the extracted `../loopzero-cloud` repository —
  [`/components/external-dependencies.md`](/components/external-dependencies.md).

## Contracts that hold across the whole surface

- **Exit codes are permanent**: `0` success, `1` usage, `2` build failure, `3` deploy failure,
  `4` auth, `5` partial failure. No others.
- **`--json` is a stability contract**: field names cannot change without a major bump; new
  fields may be added.
- **No internal vocabulary reaches the user**: ArgoCD, GitOps, CRD, `tenant_id`, advisory lock,
  Gitea, MinIO, kubebuilder, envtest and Kaniko are forbidden in CLI output, error messages and
  `--json`.
- **Every mutation takes the environment lock**, and mutating routes handle `Idempotency-Key`.

# Evidence

[^cap-manage_projects]: `api/internal/server/router.go:389`
[^cap-deploy]: `api/internal/server/router.go:487`
[^cap-observe]: `api/internal/server/router.go:549`
[^cap-custom_domains]: `api/internal/server/router.go:633`
[^cap-manage_environments]: `api/internal/server/router.go:392`
[^cap-manage_secrets]: `api/internal/server/router.go:463`
[^cap-manage_team]: `api/internal/server/router.go:254`
[^cap-preview_environments]: `api/internal/server/router.go:389`
[^cap-admin_operate]: `api/internal/server/router.go:141`
[^cap-billing]: `api/internal/server/router.go:296`
[^cap-rollback]: `api/internal/server/router.go:579`
[^cap-upload_source]: `api/internal/server/router.go:359`
