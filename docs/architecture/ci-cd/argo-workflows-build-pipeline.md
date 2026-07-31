---
name: argo-workflows-build-pipeline
description: AS-IS state of Argo Workflows in LoopZero — used today exclusively for tenant build pipeline (BuildKit + cosign + syft + push to DOCR). Pre-T0.2 Cenário B baseline before adding CI/PR-time enforcement.
type: architecture
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: T0.2 plan pre-implementation snapshot
---

# Argo Workflows AS-IS — Build Pipeline Only

> **Pre-T0.2 Cenário B baseline.** Captures the current Argo Workflows footprint before adding CI/PR-time `task quality:gates` enforcement per [ADR-2026-05-27-T0.2-RATIFIED](.claude/knowledge-base/decisions/2026-05-27-T0.2-cenario-b-ratified.md).

## System context (AS-IS)

```
┌─────────────────────────────────────────────────────────────────┐
│                        BUILD CLUSTER                            │
│                                                                 │
│   ┌──────────────────┐     ┌────────────────────────────────┐  │
│   │ argo-workflows   │     │ ClusterWorkflowTemplates       │  │
│   │ (Helm v1.0.5)    │ ◄── │  - loopzero-clone                  │  │
│   │ namespace: argo  │     │  - loopzero-cache-save             │  │
│   │                  │     │  - loopzero-cache-restore          │  │
│   └────────┬─────────┘     └────────────────────────────────┘  │
│            │                                                    │
│            │ Workflow CR submission                             │
│            ▼                                                    │
│   ┌──────────────────────────────────────────┐                  │
│   │ Tenant build pipeline (per `loopzero deploy`) │                  │
│   │   clone → cache-restore → buildkit       │                  │
│   │     → cosign sign → syft SBOM            │                  │
│   │     → push to DOCR                       │                  │
│   └──────────────────────────────────────────┘                  │
└─────────────────────────────────────────────────────────────────┘
                ▲
                │ Workflow submitted by API (via in-cluster client)
                │
┌───────────────┴─────────────────────────────────────────────────┐
│                       MGMT CLUSTER                              │
│                                                                 │
│   loopzero-api ──► api/internal/argo/workflow_submitter.go          │
│              ──► api/internal/argo/workflow_builder.go          │
│                                                                 │
│   (No CI/PR-time gates here — operator runs                     │
│    `task quality:gates` manually before merge)                  │
└─────────────────────────────────────────────────────────────────┘
```

## Components present today

| Component | Path | Purpose |
|-----------|------|---------|
| argo-workflows Helm release | `infra/helmfile/build.helmfile.yaml.gotmpl` (line ~argo-workflows) | Argo Workflows controller + server in build cluster `argo` namespace |
| ClusterWorkflowTemplates | `charts/loopzero-build-cwts/templates/cwt-*.yaml` | Reusable DAG steps (loopzero-clone, loopzero-cache-save, loopzero-cache-restore) |
| Workflow submitter | `api/internal/argo/workflow_submitter.go` | API submits Workflow CRs via in-cluster client |
| Workflow builder | `api/internal/argo/workflow_builder.go` | Composes per-tenant DAG from CWT refs + inline templates |
| Build images | DOCR `registry.digitalocean.com/loopzero-system/loopzero-{clone-runner,buildkit-runner,...}` | Custom images referenced by CWTs |

## Components ABSENT today (T0.2 Cenário B fills this gap)

| Component | Status | Purpose |
|-----------|--------|---------|
| **Argo Events** | NOT INSTALLED — `infra/helm/argo-events/` absent, no helmfile release | Webhook → workflow trigger bridge |
| **CI WorkflowTemplate** | NOT EXISTS | Parametrized `task quality:gates` runner |
| **EventSource (Gitea webhook)** | NOT EXISTS | Listen to PR open/sync events |
| **Sensor (PR trigger)** | NOT EXISTS | Translate webhook event → Workflow submission |
| **CronWorkflows** (drill watchdog, soaks, coverage, deadline) | NOT EXISTS | Replace removed GHA crons (2026-05-23) |
| **PR status reporter** | NOT EXISTS | Post Argo Workflow result back to Gitea PR check |

## Why this gap exists

- **2026-05-23 operator decision:** removed GitHub Actions entirely. 56 `cmd/loopzero-ops/internal/quality/*` gates re-homed to `task quality:gates` orchestrator running operator-driven locally.
- **Zero PR-time enforcement:** PRs can merge without `task quality:gates` running (branch protection has no required checks).
- **Scheduled crons orphaned:** drill watchdog (every 6h), weekly soak Sunday, coverage Monday, deadline check daily — all operator-mediated.

## Build pipeline invariants (preserved post-T0.2)

These MUST continue to hold after CI is added:

1. **Argo Workflows in `argo` namespace** — single controller serves both tenant builds + CI workflows
2. **CWTs cluster-scoped** — reusable across namespaces
3. **loopzero-build-sa ServiceAccount** — workload identity for tenant builds (createsd by argo-workflows release)
4. **Build cluster isolation** — mgmt cluster never runs Workflow CRs directly
5. **DOCR push from build cluster only** — credentials never leave build plane

## Files defining this AS-IS state

- `infra/helmfile/build.helmfile.yaml.gotmpl` — release definitions
- `infra/helm/argo-workflows/values.yaml` + `values.schema.json` — Helm values
- `charts/loopzero-build-cwts/` — published CWT chart
- `api/internal/argo/workflow_submitter.go` (~250 LOC)
- `api/internal/argo/workflow_builder.go` (299 LOC)
- `api/internal/argo/helpers.go`

## Related

- AS-IS state of CI gates: `cmd/loopzero-ops/internal/quality/` (56+ gates) + `taskfiles/quality.yml` orchestrator
- Companion PROPOSED: `docs/architecture/ci-cd/diff/real-ci-active.md`
- ADR ratified: `.claude/knowledge-base/decisions/2026-05-27-T0.2-cenario-b-ratified.md`
- Meeting: `.claude/knowledge-base/meetings/MEET-2026-05-27-batch-sponsor-approvals.md`
