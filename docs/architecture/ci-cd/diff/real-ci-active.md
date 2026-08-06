---
title: "Real CI Platform Active — CI/CD Diff (Engineering Complete)"
generated: { by: process:t0.2-plan-implementation-2026-05-27, at: 2026-05-27 }
name: real-ci-active
description: PROPOSED — Cenário B Argo Workflows + Argo Events CI ratified per ADR-2026-05-27-T0.2-RATIFIED + engineering shipped 2026-05-27 (3 Helm charts + WorkflowTemplate + EventSource + Sensor + 4 CronWorkflows + operator runbook).
type: decision
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: T0.2 plan implementation 2026-05-27
---

<!-- PROPOSED STATE — engineering shipped; operator activation HARD STOP -->

# Real CI Platform Active — CI/CD Diff (Engineering Complete)

> **Plan reference:** [`docs/plans/2026-05-27-t0.2-cenario-b-argo-workflows-ci-poc-plan.md`](../../../plans/2026-05-27-t0.2-cenario-b-argo-workflows-ci-poc-plan.md)
>
> **Status:** Engineering shipped 2026-05-27 in 3 Helm charts. Activation HARD STOP = operator runs helmfile sync + configures Gitea webhook (~30min, runbook [`docs/operations/ci-runbook.md`](../../../operations/ci-runbook.md)).

## AS-IS (post-GitHub-Actions removal 2026-05-23)

```
┌─────────────────────────────────────────────────────┐
│  Local / operator workstation                       │
│                                                     │
│  task quality:gates  ◄── operator runs manually    │
│  /dogfood full       ◄── operator runs manually    │
│  /chaos-test --full  ◄── operator runs manually    │
│  task drill:l2-scale ◄── operator runs manually    │
└─────────────────────────────────────────────────────┘
         │
         ▼
   PR review (GitHub UI) — no automated gates blocking merge
   Operator approval based on manual run results
```

**Gaps:**
- Zero automated PR-time gates (lint, tests, quality:gates)
- Drill watchdog cron + soaks weekly + coverage reports — operator-mediated
- Trivy + TruffleHog scan: no automated wiring (T4.7 deferred)
- Branch protection: configured but unenforced (no required status checks)

## PROPOSED (post-T0.2 — CI platform chosen)

Architecture varies by sponsor choice. Example (Argo Events + Workflows):

```
PR opened/updated
   │
   ▼
GitHub webhook ──► Argo EventBus ──► Argo Sensor
                                          │
                                          ▼
                                  Argo Workflow:
                                      ├─ task quality:gates     (~5min)
                                      ├─ task validate:manifests (~2min)
                                      ├─ go test ./...           (~10min)
                                      ├─ npm test (cli + dashboard) (~5min)
                                      ├─ trivy fs scan           (T4.7)
                                      └─ trufflehog scan         (T4.7)
                                  │
                                  ▼
                          Status posted back to GitHub PR
                                  │
                                  ▼
                          Branch protection enforces required checks
```

Scheduled CronWorkflows:
- Drill watchdog (every 6h) — kill orphan drill stacks
- Drill soak (weekly Sunday 02:00 UTC) — L2 + Stage 2 24h
- Coverage report (weekly Monday 09:00 UTC)
- Cosign cutover deadline check (daily 00:00 UTC)

## What changes

| Surface | AS-IS | PROPOSED |
|---------|-------|----------|
| PR-time lint | Operator runs locally | Auto-blocking required check |
| PR-time tests | Operator runs locally | Auto-blocking required check |
| PR-time security scan | Skipped | Trivy + TruffleHog blocking |
| PR-time quality gates | Operator runs locally | `task quality:gates` auto-runs |
| Drill watchdog | Operator notes every 6h | Cron auto-fires |
| Soaks | Operator runs weekly | Cron auto-fires |
| Coverage report | Operator generates | Cron auto-publishes |
| Required status checks | None (branch protection has no checks) | All quality gates required |

## Sponsor decision criteria (per T0.2 of plan + ADR-2026-05-27-T0.2)

ADR `.claude/knowledge-base/decisions/2026-05-27-T0.2-ci-platform-choice.md`
narrows 6 historical options to 3 viable cenarios with FAANG-grade SOTA review:

| Cenário | Plataforma | Pros | Cons | Cost | Risk |
|---------|-----------|------|------|------|------|
| **A (RECOMMENDED)** | Gitea Actions self-hosted runner | Reuses existing Gitea install in mgmt cluster, GHA YAML syntax compat, scale-to-zero via KEDA, $0 infra cost | Smaller ecosystem (~2yr GA), Gitea web UI engineer-oriented | $0 | Medium |
| **B** | Argo Workflows + Argo Events | Max K8s alignment, LoopZero team already manages Argo Workflows | Steep YAML curve, ~1wk setup for GitHub webhook integration | $0 | High setup |
| **C (fallback)** | GitHub Actions SaaS (CI only, NOT CD) | Lowest setup (~30min), free tier sufficient, native PR Checks | Reverses 2026-05-23 decision (must explicitly rescope CI vs CD) | $0 (free tier) | Low engineering, requires re-litigation |

**Adopter precedents:**
- Cenário A: FOSDEM, internal corporate Git-first orgs, indie devs
- Cenário B: Intuit, Adobe (control plane pattern), CNCF reference
- Cenário C: Vast majority of OSS projects (default standard)

**Engineering-ready POC for Cenário A** shipped this commit:
- `infra/helm/gitea/values-actions-poc.yaml.disabled` (rename to `.yaml` to enable)
- `.gitea/workflows/quality.yml.example` (rename to `.yml` to enable)
- Sponsor flips files + adds values to helmfile mgmt stanza ≈ 1h operator work

## Engineering shipped 2026-05-27

| Component | Path | Purpose |
|-----------|------|---------|
| `argo-ci-namespace` chart | `infra/helm/argo-ci-namespace/` | Namespace + ServiceAccount `loopzero-ci-runner` + Role/RoleBinding (NOT ClusterRoleBinding per ADR D3) + NetworkPolicy (default-deny + explicit allow Gitea/DOCR/Argo) |
| `argo-events` chart | `infra/helm/argo-events/` | Upstream Argo Events 2.4.4 + EventBus JetStream NATS (persistence enabled for buffering per EC-1) |
| `loopzero-ci-workflows` chart | `infra/helm/loopzero-ci-workflows/` | WorkflowTemplate `quality-gate` (parametrized per ADR D2) + EventSource Gitea webhook (HMAC SHA256 validation + PR event filter) + Sensor PR trigger (idempotent name via commit SHA) + ConfigMap status-posting script (retries with exponential backoff) + 4 CronWorkflows (drill watchdog 6h, weekly soak Sun 02:00, coverage Mon 09:00, cosign deadline daily 00:00) |
| Helmfile releases | `infra/helmfile/build.helmfile.yaml.gotmpl` | 3 releases wired with explicit `needs:` ordering |
| Hermetic render test | `infra/tests/helm/render-loopzero-ci-workflows_test.sh` | 30/30 PASS: lint + render + gate parity (values↔taskfile) + ADR D2/D3/D4/D5 compliance + 4 CronWorkflow schedules |
| Operator runbook | [`docs/operations/ci-runbook.md`](../../../operations/ci-runbook.md) | Activation, daily operation, troubleshooting, rollback to Cenário C |

## Operator HARD STOP (post-engineering activation)

1. Sponsor approves T2.7 budget (already T3-ratified per `MEET-2026-05-27-batch-sponsor-approvals`)
2. Operator creates `gitea-webhook-secret` + `gitea-api-token` Secrets ([`docs/operations/ci-runbook.md`](../../../operations/ci-runbook.md) Step 2)
3. Operator runs `helmfile -e standalone-multi sync --selector name=argo-ci-namespace,name=argo-events,name=loopzero-ci-workflows` (sequence per Step 3 of runbook)
4. Operator configures Gitea webhook UI (Step 4)
5. Operator updates branch protection: require `quality` status check (Step 5)
6. Operator opens test PR → verifies status posts → cycles green/red
7. 30-day trial gate per ADR D5 — if execution fails, swap to Cenário C via runbook "Rollback" section (Cenário A POC preserved as fallback)

## Operator quick-fallback (if decision delayed > 4 weeks)

Per [`customer-trial-onboarding-plan.md`](../../../operations/customer-trial-onboarding-plan.md) EC-15 (DOCUMENT) — default fallback after timeout: restore self-hosted GitHub Actions runners (status quo of pre-removal 2026-05-23). Tactical preservation; revisit at next quarter.

## Related

- ADR 2026-05-23 (GitHub Actions removal)
- ADR-2026-05-27-T0.2 (`.claude/knowledge-base/decisions/2026-05-27-T0.2-ci-platform-choice.md`) — formal 3-cenário comparison + recommendation
- `taskfiles/quality.yml` (current gate harness)
- `cmd/loopzero-ops/quality_subcmd.go` (gate CLI)
- T0.2 of plan (decision driver)
- `infra/helm/gitea/values-actions-poc.yaml.disabled` (POC for Cenário A)
- `.gitea/workflows/quality.yml.example` (POC workflow)
