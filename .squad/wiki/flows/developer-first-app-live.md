---
type: Flow
title: "A developer gets their first app live, and then keeps it running"
description: "Journey flow, 12 steps, traced through real call sites."
resource: "api/internal/auth/authhttp/auth.go"
tags:
  - flow
  - journey
  - developer-first-app-live
sources:
  - id: flow-root
    resource: "api/internal/auth/authhttp/auth.go#L173"
  - id: step-1
    resource: "api/internal/auth/authhttp/auth.go#L173"
  - id: step-2
    resource: "api/internal/projects/projectshttp/projects.go#L208"
  - id: step-3
    resource: "api/internal/secrets/secretshttp/secrets.go#L835"
  - id: step-4
    resource: "api/internal/source/sourcehttp/source.go#L142"
  - id: step-5
    resource: "api/internal/routes/builds/builds.go#L494"
  - id: step-6
    resource: "api/internal/routes/builds/builds.go#L392"
  - id: step-7
    resource: "api/internal/sse/stream_handler.go#L49"
  - id: step-8
    resource: "api/cmd/deploy_timeline_spawn.go#L38"
  - id: step-9
    resource: "api/internal/routes/rollback/rollback.go#L126"
  - id: step-10
    resource: "api/internal/domains/domainshttp/domains_cfzone.go#L82"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

**A developer gets their first app live, and then keeps it running** — a `journey` flow of 12 steps,
traced from the call site that starts it.[^flow-root]

| | |
|---|---|
| **Trigger** | a developer with a monorepo and an account, holding a token, facing the 72 routes this API exposes |
| **Outcome** | an app on a custom domain with TLS, that they can redeploy, revert and scale — having hit four places where the system answers 200 while the work is not actually done |


The same API read as an experience rather than as a call graph: what a developer does, in order,
and where the system answers 200 while the work is not actually done.

**Four traps, and the flow names each one where it happens.** They are not bugs in the sense of
something failing — every one of them is a documented, deliberate design choice whose cost lands
on the user later:

1. **Secrets return success before the Kubernetes write can possibly succeed.** Postgres is the
   source of truth and the ExternalSecret sync is best-effort; before the first deploy the target
   namespace does not exist. The symptom arrives much later as a pod in
   `CreateContainerConfigError`.
2. **The 202 is not the deploy.** Eighteen more steps run behind one SSE channel.
3. **The custom domain needs the user to change nameservers at their registrar.** The one step
   the API cannot do for them, and the domain sits in `pending_ns` until they do.
4. **The old verification endpoint still answers, to explain that it is deprecated.** Better than
   a 404, and it means the surface teaches two mental models at once.

The two steps this flow rates highest are `EnsureEnvironment` — which removed an eternal
`SYNC_FAILED` with no explanation — and rollback, which waits for ArgoCD to confirm before
reporting success, a rule added after a live incident where it did not.


# Schema

The steps, in order. Every row is anchored to the line that performs it.


| # | The developer… | Against | How it goes |
|---|---|---|---|
| 1 | confirms who they are and which team they are acting as | `GET /api/v1/auth/whoami`<br/>`api/internal/auth/authhttp/auth.go:173` | reassuring — one call answers identity, membership and active team together |
| 2 | creates a project | `POST /api/v1/projects`<br/>`api/internal/projects/projectshttp/projects.go:208` | fine, and also optional — the first deploy creates the project anyway via EnsureProject, so a developer who skips this step never learns it existed |
| 3 | sets the secrets the app needs | `POST /api/v1/secrets`<br/>`api/internal/secrets/secretshttp/secrets.go:835` | THE FIRST TRAP. The call returns success once Postgres has the row. The Kubernetes ExternalSecret write happens on the same request and its failure is logged, not returned — 'Log but don't fail — PG is source of truth, K8s sync is eventual'. Before the first deploy the target namespace does not exist yet, so this sync cannot succeed, and the developer is told nothing. The symptom arrives much later as a pod in CreateContainerConfigError. |
| 4 | uploads the source bundle | `POST /api/v1/source/upload`<br/>`api/internal/source/sourcehttp/source.go:142` | a hard 500MB ceiling with a clean typed 413; the 32MB in-memory threshold means larger bundles spool to disk rather than failing. This step behaves. |
| 5 | deploys | `POST /api/v1/builds`<br/>`api/internal/routes/builds/builds.go:494` | the request can be refused four different ways before any build starts — 400 for a missing field, 402 from the builds-per-month cap, 402 again from the build-minutes cost cap, 429 from the cell admission gate with Retry-After. Each says something different and only the 429 tells them when to come back. |
| 6 | deploying to an environment name they have never used before | `EnsureEnvironment`<br/>`api/internal/routes/builds/builds.go:392` | genuinely good: the environment, its namespace and its ArgoCD App are created on the fly. Before this existed the deploy produced an eternal SYNC_FAILED with no explanation. |
| 7 | watches the stream and waits | `GET /api/v1/builds/{id}/stream`<br/>`api/internal/sse/stream_handler.go:49` | THE SECOND TRAP, and it is a waiting problem. The POST returns 202 in seconds; everything real happens afterwards across a reconcile loop that ticks on an interval, a gitops push, an ArgoCD sync and a convergence loop polling every 10s with a 10-minute reaper behind it. The developer's only view of all that is one SSE channel, and deploy.completed is retried three times precisely because if it is dropped the CLI hangs until timeout. |
| 8 | the deploy is slow or stuck and they want to know why | `GET /api/v1/deploys/{deployID}/timeline`<br/>`api/cmd/deploy_timeline_spawn.go:38` | the timeline exists and is the right idea, but only ONE of its five planned source adapters is wired in production (release_db), and that one walks a synthetic wall-clock ladder rather than reading real release state — so the phases are approximately right rather than observed. |
| 9 | the new version is bad and they revert | `POST /api/v1/projects/{name}/rollback`<br/>`api/internal/routes/rollback/rollback.go:126` | the best-designed path in the journey. The old manifests are read back out of Gitea history and re-pushed rather than rebuilt, and the call waits for ArgoCD to confirm the sync before reporting success — a rule added after a LIVE incident where rollback returned success while pods still served the rolled-back code. |
| 10 | attaches their own domain | `POST /api/v1/domains`<br/>`api/internal/domains/domainshttp/domains_cfzone.go:82` | THE THIRD TRAP, and it is not the platform's fault but it is the developer's problem. The call succeeds and returns status pending_ns plus a list of Cloudflare nameservers. Nothing else can happen until the developer goes to their registrar, changes the nameservers for the whole zone, and waits for propagation. It is the one step in the journey the API cannot do for them, and the domain sits in pending_ns until they do it. |
| 11 | tries the documented verify endpoint they remember from before | `POST /api/v1/domains/{domain}/verify`<br/>`api/internal/domains/domainshttp/domains_verify.go:27` | THE FOURTH TRAP, and this one is honest about itself: the endpoint still exists and returns a message saying TXT verification is deprecated and to use nameserver delegation instead. A dead route kept alive to explain itself is better than a 404, but it means the surface teaches two different mental models at once. |
| 12 | scales the app for real traffic | `POST /api/v1/projects/{name}/scale`<br/>`api/internal/services/build/build_orchestrator_submit.go:88` | two sharp edges, both refusing loudly rather than guessing: setting replicas and autoscaling together is a typed error, and asking for scale-to-zero on a cluster without KEDA is SCALE_TO_ZERO_UNAVAILABLE with the remedy in the message. The cap that bites silently is the plan replica limit, enforced deep inside SubmitBuild rather than at this endpoint. |

# Relationships


- Every step lands on a capability in
  [`/entities/api-capabilities.md`](/entities/api-capabilities.md).
- Step 5 opens [`/flows/deploy-container-app.md`](/flows/deploy-container-app.md).
- Trap 1 is the best-effort write in
  [`/components/multi-cell-routing.md`](/components/multi-cell-routing.md); trap 2 is the single
  channel in [`/components/event-stream.md`](/components/event-stream.md); traps 3 and 4 are the
  two generations of [`/entities/state-machine-domain.md`](/entities/state-machine-domain.md),
  reached through [`/components/external-integrations.md`](/components/external-integrations.md).
- Step 9's rollback is the replay path in
  [`/components/release-delivery.md`](/components/release-delivery.md).


# Evidence

Every step in the table above carries the `file:line` that performs it. The `sources` block below lists the flow's own anchor and the first ten steps; steps 11 to 12 are anchored in the table only.

[^flow-root]: `api/internal/auth/authhttp/auth.go:173`
[^step-1]: `api/internal/auth/authhttp/auth.go:173`
[^step-2]: `api/internal/projects/projectshttp/projects.go:208`
[^step-3]: `api/internal/secrets/secretshttp/secrets.go:835`
[^step-4]: `api/internal/source/sourcehttp/source.go:142`
[^step-5]: `api/internal/routes/builds/builds.go:494`
[^step-6]: `api/internal/routes/builds/builds.go:392`
[^step-7]: `api/internal/sse/stream_handler.go:49`
[^step-8]: `api/cmd/deploy_timeline_spawn.go:38`
[^step-9]: `api/internal/routes/rollback/rollback.go:126`
[^step-10]: `api/internal/domains/domainshttp/domains_cfzone.go:82`
