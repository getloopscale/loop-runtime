# LoopZero — System Design

> **Version:** 8.1 — Sprint Plan Restructured: CLI-first delivery order, product exit gates per Sprint
> **Principle:** Tests define the system. Implementation follows tests. Infrastructure follows behavior. Failure modes define operations. Concurrency defines correctness.

---

## Table of Contents

- [§0 — Product & CLI Contract](#0-product--cli-contract)
  - [§0.1 Core CLI Workflows](#01-core-cli-workflows)
  - [§0.2 CLI Product Contract](#02-cli-product-contract)
  - [§0.3 CLI Output & Error Contract](#03-cli-output--error-contract)
- [§1 — North Star](#1-north-star)
- [§2 — Design Philosophy](#2-design-philosophy)
  - [§2.1 TDD as behavior specification](#21-tdd-as-behavior-specification)
  - [§2.2 Boundaries are contracts, not conventions](#22-boundaries-are-contracts-not-conventions)
  - [§2.3 Local infra must be indistinguishable from production](#23-local-infra-must-be-indistinguishable-from-production)
  - [§2.4 Errors are contracts, not surprises](#24-errors-are-contracts-not-surprises)
  - [§2.5 Design for failure, not for success](#25-design-for-failure-not-for-success)
  - [§2.6 Isolation is not optional in multi-tenancy](#26-isolation-is-not-optional-in-multi-tenancy)
  - [§2.7 Every mutation is serialized and idempotent](#27-every-mutation-is-serialized-and-idempotent)
  - [§2.8 CLI-first, Vercel-quality DX](#28-cli-first-vercel-quality-dx)
- [§3 — Source of Truth & State Model](#3-source-of-truth--state-model)
  - [§3.1 The problem: 5 systems with independent state](#31-the-problem-5-systems-with-independent-state)
  - [§3.2 Source of truth hierarchy](#32-source-of-truth-hierarchy)
  - [§3.3 PostgreSQL: critical operational dependency, not just cache](#33-postgresql-critical-operational-dependency-not-just-cache)
  - [§3.4 State precedence by flow](#34-state-precedence-by-flow)
- [§4 — Concurrency, Serialization & Idempotency](#4-concurrency-serialization--idempotency)
  - [§4.1 The problem](#41-the-problem)
  - [§4.2 Environment lock](#42-environment-lock)
  - [§4.3 Idempotency](#43-idempotency)
  - [§4.4 Conflict matrix](#44-conflict-matrix)
  - [§4.5 Retry semantics](#45-retry-semantics)
- [§5 — State Machines](#5-state-machines)
  - [§5.1 Build state machine](#51-build-state-machine)
  - [§5.2 Release state machine](#52-release-state-machine)
  - [§5.3 PreviewEnvironment state machine](#53-previewenvironment-state-machine)
  - [§5.4 Promotion state machine](#54-promotion-state-machine)
  - [§5.5 Rollback state machine](#55-rollback-state-machine)
- [§6 — Architecture](#6-architecture)
  - [§6.1 Four planes, strictly separated](#61-four-planes-strictly-separated)
  - [§6.2 Why four planes](#62-why-four-planes)
  - [§6.3 Control plane self-protection](#63-control-plane-self-protection)
- [§7 — System Components](#7-system-components)
  - [§7.1 loopzero CLI](#71-loopzero-cli-packagescli--typescript)
  - [§7.2 loopzero API](#72-loopzero-api-packagesapi--typescriptfastify)
  - [§7.3 LoopZero Operators](#73-loopzero-operators-go--kubebuilder)
  - [§7.4 Argo Workflows (Build Engine)](#74-argo-workflows-build-engine)
  - [§7.5 Gitea (Source Store + GitOps Store)](#75-gitea-source-store--gitops-store)
  - [§7.6 MinIO (Artifact Store)](#76-minio-artifact-store)
  - [§7.7 Traefik (Ingress and TLS)](#77-traefik-ingress-and-tls)
  - [§7.8 PostgreSQL (Operations Database)](#78-postgresql-operations-database)
- [§8 — Responsibility Boundaries (ADR)](#8-responsibility-boundaries-adr)
  - [§8.1 Responsibility matrix](#81-responsibility-matrix)
  - [§8.2 What the platform does NOT solve](#82-what-the-platform-does-not-solve)
- [§9 — Data Model](#9-data-model)
  - [§9.1 Core entities](#91-core-entities)
  - [§9.1.1 loopzero.yaml schema](#911-loopzeroyaml-schema)
  - [§9.2 CRDs](#92-crds)
- [§10 — Core Flows](#10-core-flows)
  - [§10.1 loopzero deploy — Complete flow with serialization](#101-loopzero-deploy--complete-flow-with-serialization)
  - [§10.2 loopzero promote (advanced workflow)](#102-loopzero-promote-advanced-workflow)
  - [§10.3 loopzero rollback](#103-loopzero-rollback)
  - [§10.4 Preview environment](#104-preview-environment)
- [§11 — Cancellation & Compensation](#11-cancellation--compensation)
  - [§11.1 Build cancellation](#111-build-cancellation)
  - [§11.2 Compensation mechanisms](#112-compensation-mechanisms)
  - [§11.3 What the platform does NOT compensate](#113-what-the-platform-does-not-compensate)
- [§12 — Failure Modes & Recovery](#12-failure-modes--recovery)
  - [§12.1 Failure matrix](#121-failure-matrix)
  - [§12.2 Recovery invariants](#122-recovery-invariants)
- [§13 — State Reconciliation (Detailed)](#13-state-reconciliation-detailed)
  - [§13.1 StateReconciler design](#131-statereconciler-design)
  - [§13.2 Reconciliation algorithm](#132-reconciliation-algorithm)
  - [§13.3 What StateReconciler does NOT do](#133-what-statereconciler-does-not-do)
- [§14 — Multi-Tenancy & Isolation](#14-multi-tenancy--isolation)
  - [§14.1 Isolation across five dimensions](#141-isolation-across-five-dimensions)
  - [§14.2 Noisy neighbor protection](#142-noisy-neighbor-protection)
  - [§14.3 PriorityClass: scheduling priority, not fairness](#143-priorityclass-scheduling-priority-not-fairness)
  - [§14.4 Network isolation](#144-network-isolation)
  - [§14.5 Capacity planning thresholds](#145-capacity-planning-thresholds)
- [§15 — Observability](#15-observability)
  - [§15.1 Three pillars: metrics, traces, logs — correlated](#151-three-pillars-metrics-traces-logs--correlated)
  - [§15.2 Key metrics](#152-key-metrics)
  - [§15.3 Correlation ID](#153-correlation-id)
  - [§15.4 SLOs](#154-slos)
  - [§15.5 Alerting rules](#155-alerting-rules)
  - [§15.6 Dashboards](#156-dashboards)
- [§16 — Security Model](#16-security-model)
  - [§16.1 Management plane security](#161-management-plane-security)
  - [§16.2 Secret lifecycle](#162-secret-lifecycle)
  - [§16.3 RBAC model](#163-rbac-model)
- [§17 — Supply Chain Security](#17-supply-chain-security)
  - [§17.1 Threat model](#171-threat-model)
  - [§17.2 Ephemeral credentials per build](#172-ephemeral-credentials-per-build)
  - [§17.3 Artifact signing and verification](#173-artifact-signing-and-verification)
  - [§17.4 Cache isolation](#174-cache-isolation)
  - [§17.5 Git commit integrity](#175-git-commit-integrity)
- [§18 — TDD/BDD Strategy](#18-tddbdd-strategy)
  - [§18.1 Test hierarchy](#181-test-hierarchy)
  - [§18.2 State machine tests](#182-state-machine-tests)
  - [§18.3 Serialization tests](#183-serialization-tests)
  - [§18.4 E2E chaos tests](#184-e2e-chaos-tests)
- [§19 — Local Infrastructure (Inner Loop)](#19-local-infrastructure-inner-loop)
  - [§19.1 Topology](#191-topology)
  - [§19.2 Taskfile](#192-taskfile)
  - [§19.3 k3d Configuration](#193-k3d-configuration)
- [§20 — Test Contracts by Layer](#20-test-contracts-by-layer)
- [§21 — Quality Pipeline](#21-quality-pipeline)
- [§22 — Toolchain Decision Log](#22-toolchain-decision-log)
- [§23 — Implementation Order](#23-implementation-order)
  - [Sprint 0 — Foundation + Minimal Observability](#sprint-0--foundation--minimal-observability-week-1-2)
  - [Sprint 1 — loopzero login + loopzero init + TenantController](#sprint-1--loopzero-login--loopzero-init--tenantcontroller-week-3-5)
  - [Sprint 2 — loopzero deploy happy path end-to-end](#sprint-2--loopzero-deploy-happy-path-end-to-end-week-6-9)
  - [Sprint 3 — loopzero logs + loopzero status + loopzero secret](#sprint-3--loopzero-logs--loopzero-status--loopzero-secret-week-10-12)
  - [Sprint 4 — loopzero rollback + loopzero promote + Cancel](#sprint-4--loopzero-rollback--loopzero-promote--cancel-week-13-15)
  - [Sprint 5 — Preview Environments + GitHub Webhook](#sprint-5--preview-environments--github-webhook-week-16-18)
  - [Sprint 6 — Security + Supply Chain](#sprint-6--security--supply-chain-week-19-21)
  - [Sprint 7 — Observability Complete](#sprint-7--observability-complete-week-22-23)
  - [Sprint 8 — Hardening + Chaos](#sprint-8--hardening--chaos-week-24-25)
  - [Sprint 9 — Polish + Launch](#sprint-9--polish--launch-week-26-27)
  - [Ready for first paying user — product exit criteria](#ready-for-first-paying-user--product-exit-criteria)
  - [Note on Dashboard](#note-on-dashboard)
  - [Sprint summary](#sprint-summary)
- [§24 — User Experience & Onboarding](#24-user-experience--onboarding)
  - [§24.1 Product positioning](#241-product-positioning)
  - [§24.2 User onboarding flow](#242-user-onboarding-flow)
  - [§24.3 Interface parity: CLI and dashboard](#243-interface-parity-cli-and-dashboard)
  - [§24.4 Terminology mapping (internal to user-facing)](#244-terminology-mapping-internal--user-facing)
- [§25 — CLI Reference](#25-cli-reference)
  - [§25.1 Authentication](#251-authentication)
  - [§25.2 Project initialization](#252-project-initialization)
  - [§25.3 Deploy](#253-deploy)
  - [§25.4 Status and inspection](#254-status-and-inspection)
  - [§25.5 Logs](#255-logs)
  - [§25.6 Rollback](#256-rollback)
  - [§25.7 Promote (advanced workflow)](#257-promote-advanced-workflow)
  - [§25.8 Secrets / Environment variables](#258-secrets--environment-variables)
  - [§25.9 Environments and previews](#259-environments-and-previews)
  - [§25.10 Teams and projects](#2510-teams-and-projects)
  - [§25.11 Build management](#2511-build-management)
  - [§25.12 Error output examples](#2512-error-output-examples)
- [§26 — loopzero.yaml — Complete Specification](#26-loopzeroyaml--complete-specification)
  - [§26.1 Minimal example](#261-minimal-example--nestjs--vite-monorepo-generated-by-loopzero-init)
  - [§26.2 Example with secrets declared](#262-example-with-secrets-declared)
  - [§26.3 Example with worker](#263-example-with-worker)
  - [§26.4 Field reference](#264-field-reference)
  - [§26.5 Supported frameworks (v1)](#265-supported-frameworks-v1)
  - [§26.6 Relationship to section 9.1.1](#266-relationship-to-section-911)
- [Appendix A — Directory Structure](#appendix-a--directory-structure)
- [Appendix B — Review Response Matrix](#appendix-b--review-response-matrix)
- [Appendix C — Gap Resolution Matrix (v3.0 to v3.1)](#appendix-c--gap-resolution-matrix-v30--v31)
- [Appendix D — Conceptual Corrections Matrix (v3.1 to v4.0)](#appendix-d--conceptual-corrections-matrix-v31--v40)
- [Appendix E — Architectural Decoupling Matrix (v4.0 to v5.0)](#appendix-e--architectural-decoupling-matrix-v40--v50)
- [Appendix F — v1 Known Risks (Accepted Trade-offs)](#appendix-f--v1-known-risks-accepted-trade-offs)
- [Appendix G — Staff+ Review Action Items Matrix (v5.0 to v5.1)](#appendix-g--staff-review-action-items-matrix-v50--v51)
- [Appendix H — Maximum Rigor Review Matrix (v5.1 to v5.2)](#appendix-h--maximum-rigor-review-matrix-v51--v52)
- [Appendix I — Review Round 2 Matrix (v5.2 to v5.3)](#appendix-i--review-round-2-matrix-v52--v53)
- [Appendix J — Review Round 3 Matrix (v5.3 to v5.4)](#appendix-j--review-round-3-matrix-v53--v54)
- [Appendix K — External Review Matrix (v5.4 to v5.5)](#appendix-k--external-review-matrix-v54--v55)
- [Appendix L — Product Positioning Alignment (v5.5 to v6.0)](#appendix-l--product-positioning-alignment-v55--v60)
- [Appendix M — CTO Directive: CLI-first Alignment (v6.0 to v7.0)](#appendix-m--cto-directive-cli-first-alignment-v60--v70)
- [Appendix N — Alignment Checklist Resolution (v7.0 to v7.1)](#appendix-n--alignment-checklist-resolution-v70--v71)
- [Appendix O — Product & CLI Contract (v7.1 to v8.0)](#appendix-o--product--cli-contract-v71--v80)
- [Appendix P — Sprint Plan Restructure (v8.0 to v8.1)](#appendix-p--sprint-plan-restructure-v80--v81)

---

## 0. Product & CLI Contract

> This section defines the product from the user's perspective. Everything below — architecture, state machines, failure modes — exists to serve these workflows. If a platform decision contradicts the experience defined here, the platform decision must change.

### 0.1 Core CLI Workflows

These are the five workflows that define LoopZero as a product. Each must work exactly as described before v1 ships.

**Workflow 1 — First deploy (the "5-minute promise")**

```
$ loopzero login
✓ Authenticated as lucas@acme.dev (team: acme)

$ cd my-monorepo/
$ loopzero init
Detected: Turborepo monorepo
  apps/api    → NestJS (port 3000)
  apps/web    → Vite (port 5173)
  apps/worker → Node.js worker (no port)

Generated loopzero.yaml with 3 apps.
Review and edit if needed, then run: loopzero deploy

$ loopzero deploy
✓ Building api...    [45s]
✓ Building web...    [38s]
✓ Building worker... [22s]
✓ Deploying...       [12s]

🚀 Live:
  api    → https://api-production.my-saas.acme.loopzero.example
  web    → https://production.my-saas.acme.loopzero.example
  worker → (no URL — background worker)
```

Three commands. Nothing else required. After this, `git push` to main auto-deploys, and opening a PR creates a preview environment automatically.

**Workflow 2 — View logs**

```
$ loopzero logs --app api --follow
2026-03-12T14:32:01Z [api] Server listening on port 3000
2026-03-12T14:32:05Z [api] GET /health 200 2ms
2026-03-12T14:32:11Z [api] POST /api/users 201 45ms
^C
```

No cluster access. No `kubectl`. Logs stream directly from the running service.

**Workflow 3 — Rollback a bad deploy**

```
$ loopzero rollback
Current deploy: d-8f3a (deployed 3 min ago by lucas@acme.dev)
Revert to: d-7c2e (deployed 2h ago)

⚠ Side effects that cannot be reverted:
  - Database migrations run by this deploy (if any)

Revert? [Y/n] y

✓ Reverted to d-7c2e
  api    → https://api-production.my-saas.acme.loopzero.example
  web    → https://production.my-saas.acme.loopzero.example
```

One command. Inline context. No separate `--list` step required for the common case.

**Workflow 4 — Manage secrets**

```
$ loopzero secret set DATABASE_URL=postgresql://...
✓ Secret set: DATABASE_URL (production)

$ loopzero secret pull --env staging
✓ Pulled 4 secrets to .env.local (staging)

$ loopzero secret list
NAME            ENVIRONMENT   LAST UPDATED
DATABASE_URL    production    2h ago
JWT_SECRET      production    3d ago
QUEUE_URL       production    3d ago
DATABASE_URL    staging       1d ago
```

Secrets are set via CLI, pulled to `.env.local` for local development, and injected into running services automatically. Values are never displayed in `list` output.

**Workflow 5 — Preview environments**

```
$ git push origin feat/payments
# GitHub webhook fires automatically
# LoopZero builds and deploys a preview environment

# PR comment appears:
# 🔗 Preview ready:
#   api → https://api-feat-payments.my-saas.acme.loopzero.example
#   web → https://feat-payments.my-saas.acme.loopzero.example
```

No CLI command needed. Opening a PR is the trigger. Previews are destroyed automatically when the PR is merged or closed. Manual creation is available via `loopzero deploy --preview` for branches without PRs.

### 0.2 CLI Product Contract

The CLI is the primary product interface. This section defines stability guarantees that engineering must respect.

**Hierarchy of interfaces:**

```
1. CLI (primary — all features available here first)
2. API (powers CLI and dashboard — stable, versioned)
3. Dashboard (secondary — built after CLI is validated)
```

Any feature that exists in the dashboard but not in the CLI is a product bug. The reverse is acceptable — some operations (e.g., `loopzero secret pull`) are CLI-only by design.

**Backward compatibility rules:**

| Rule | Description |
|------|-------------|
| Commands are permanent | A shipped command cannot be removed. It can be deprecated with a 6-month warning, but must continue to work. |
| Flags are permanent | A shipped flag cannot change semantics. New flags can be added. |
| Default behavior is frozen | If `loopzero deploy` defaults to `production`, it always defaults to `production`. |
| Output format is a contract | Human-readable output can change (colors, wording). Machine-readable output (`--json`) cannot change shape without a major version bump. |
| Exit codes are permanent | Once defined, exit codes cannot change meaning (see section 0.3). |

**Versioning:**

The CLI follows semver. Major version bumps (2.0, 3.0) are the only mechanism for breaking changes. The current target is v1 — no breaking changes until v2.

**Deprecation protocol:**

```
v1.5:  loopzero promote → prints: "Warning: 'loopzero promote' is deprecated. Use 'loopzero deploy --from staging --to production'. Will be removed in v2.0."
v1.6+: continues to work, continues to warn
v2.0:  removed
```

### 0.3 CLI Output & Error Contract

Every CLI command produces structured output that is predictable, scriptable, and CI-friendly.

**Exit codes:**

| Code | Meaning | When |
|------|---------|------|
| `0` | Success | Operation completed successfully |
| `1` | CLI usage error | Invalid command, missing required argument, malformed flag |
| `2` | Build failure | Build step failed (compilation error, dependency resolution, Dockerfile issue) |
| `3` | Deploy failure | Build succeeded but deployment failed (sync timeout, health check failure) |
| `4` | Authentication error | Not logged in, token expired, insufficient permissions |
| `5` | Partial failure | Some apps succeeded, some failed (monorepo-specific — see below) |

Exit code `5` is critical for monorepo deploys. It means the operation partially succeeded and requires attention.

**Machine-readable output (`--json`):**

Every command supports `--json` for CI/CD integration. The JSON output is a stability contract — shape changes require a major version bump.

```
$ loopzero deploy --json
{
  "deploy_id": "d-8f3a",
  "status": "success",
  "duration_seconds": 97,
  "apps": [
    {
      "name": "api",
      "status": "deployed",
      "url": "https://api-production.my-saas.acme.loopzero.example",
      "build_duration_seconds": 45
    },
    {
      "name": "web",
      "status": "deployed",
      "url": "https://production.my-saas.acme.loopzero.example",
      "build_duration_seconds": 38
    }
  ]
}
```

```
$ loopzero status --json
{
  "project": "my-saas",
  "team": "acme",
  "environment": "production",
  "apps": [
    {
      "name": "api",
      "status": "running",
      "deploy_id": "d-8f3a",
      "url": "https://api-production.my-saas.acme.loopzero.example",
      "deployed_at": "2026-03-12T14:30:00Z",
      "deployed_by": "lucas@acme.dev"
    }
  ]
}
```

**CI mode:**

When `CI=true` is set in the environment (automatically detected in GitHub Actions, GitLab CI, CircleCI, Jenkins), the CLI adjusts its behavior:

| Behavior | Interactive (default) | CI mode (`CI=true`) |
|----------|-----------------------|---------------------|
| Spinners/progress bars | Yes | No — line-by-line output |
| Colors | Yes (if terminal supports) | No — plain text |
| Confirmations (`[Y/n]`) | Yes — waits for input | No — aborts with exit code 1. Use `--force` to skip. |
| `loopzero init` interactive wizard | Yes — prompts for each app | No — requires `--yes` flag or existing `loopzero.yaml` |
| Output format | Human-readable | Human-readable (use `--json` for machine) |

**Partial failure semantics (monorepo-specific):**

When a monorepo deploy partially succeeds (e.g., `api` deploys but `worker` build fails), the behavior is:

```
$ loopzero deploy
✓ Building api...    [45s]
✓ Building web...    [38s]
✗ Building worker... FAILED

Build failed for app "worker":
  error TS2307: Cannot find module '@shared/types'
  Full log: loopzero logs --build b-4a2c

⚠ Partial deploy:
  api    → deployed (https://api-production.my-saas.acme.loopzero.example)
  web    → deployed (https://production.my-saas.acme.loopzero.example)
  worker → NOT deployed (build failed)

Exit code: 5 (partial failure)
```

**Partial failure rules:**

| Rule | Behavior |
|------|----------|
| Build independence | Each app builds independently. One app's build failure does not block others. |
| Deploy independence | Apps that build successfully are deployed. Failed apps retain their previous version. |
| No automatic rollback | Successful apps are NOT rolled back when a sibling fails. The user decides. |
| Exit code | `5` (partial failure) — distinguishable from full success (`0`) and full failure (`3`). |
| `--json` output | Each app has its own `status` field: `"deployed"`, `"failed"`, or `"skipped"`. |
| Rollback granularity | `loopzero rollback` reverts ALL apps to the previous release. `loopzero rollback --app worker` is not supported in v1 — rollback is atomic per environment. |
| Preview environments | Partial failure in preview: preview is created with available apps. Missing apps show "build failed" in PR comment. |

**Partial failure JSON output:**

```
$ loopzero deploy --json
{
  "deploy_id": "d-9b4f",
  "status": "partial_failure",
  "duration_seconds": 52,
  "apps": [
    {
      "name": "api",
      "status": "deployed",
      "url": "https://api-production.my-saas.acme.loopzero.example",
      "build_duration_seconds": 45
    },
    {
      "name": "worker",
      "status": "failed",
      "error": "Build failed: TS2307 Cannot find module '@shared/types'",
      "build_log": "loopzero logs --build b-4a2c"
    }
  ]
}
```

**Timeout behavior:**

| Timeout | Duration | CLI output |
|---------|----------|------------|
| Build timeout | 10 min (configurable per app) | `✗ Building api... TIMEOUT (exceeded 10m)` |
| Deploy timeout (ArgoCD sync) | 2 min | `✗ Deploying... TIMEOUT (service not healthy after 2m)` |
| CLI connection timeout | 30s | `✗ Cannot reach LoopZero API. Check your internet connection or run: loopzero whoami` |

**Retry hints:**

The CLI never retries automatically. On transient failures, it suggests retry:

```
✗ Deploy failed: platform temporarily unavailable (503)

This is usually temporary. Retry with: loopzero deploy
If the problem persists, check: loopzero status
```

---

## 1. North Star

**What the product does, in one sentence:**

> The developer installs LoopZero, runs three commands — `loopzero login`, `loopzero init`, `loopzero deploy` — and in less than 5 minutes every service is live at its own URL, with TLS, preview environments on every PR, and rollback with one command. No infrastructure YAML. No Kubernetes. All other commands are optional operational tooling.

**Product context:** LoopZero is the Vercel for developers who have a backend. The target user already uses Vercel — and loves the experience. The problem is that Vercel doesn't support multiple services: they have a monorepo with frontend, API, workers, and database, and Vercel only solves the frontend. LoopZero solves the entire monorepo with the same DX, via CLI.

**Target persona — Lucas:**

Lucas is a fullstack tech lead at a 15-person startup. Stack: NestJS API + Vite frontend + BullMQ workers, all in a Turborepo monorepo. Today he deploys via a 400-line GitHub Actions pipeline he wrote and maintains alone. His daily pain: every config change (new env var, new service, preview for QA) means editing YAML he barely understands, waiting 20 minutes for CI, and debugging Kubernetes issues he shouldn't need to know about. He uses Vercel for the marketing site and wishes the rest of the stack was that simple. His measure of success: `git push` and everything works — or `loopzero deploy` and everything works. He doesn't want to learn Kubernetes. He wants to ship.

**The primary user is a backend engineer who prefers terminal workflows over web dashboards.** The dashboard exists for visibility and onboarding, but the daily workflow lives in the terminal. Any feature that requires the dashboard but cannot be done via CLI is a product bug.

**The strategy is CLI-first.** The dashboard will exist, but the primary interface is the terminal. The SLO of "5 minutes" is measured from `loopzero deploy` to live URL.

**LoopZero vs. Vercel — feature comparison:**

| Capability | Vercel | LoopZero |
|-----------|--------|------|
| Frontend (React, Next.js, Vite, etc.) | ✅ | ✅ |
| Backend / API server | ❌ Limited to serverless functions | ✅ Dedicated service, any framework |
| Workers / background jobs | ❌ | ✅ |
| Multiple services in one repository | ❌ | ✅ |
| Preview environments per PR | ✅ | ✅ |
| Rollback with one command | ✅ | ✅ |
| Environment variables per environment | ✅ | ✅ |
| Auto-deploy on push to main | ✅ | ✅ |
| Real-time logs | ✅ | ✅ |
| CLI as complete interface | ✅ | ✅ |
| Managed databases | ❌ (external integration) | 🔜 (roadmap) |
| Multi-region | ✅ | 🔜 v3 |
| Custom domains | ✅ | 🔜 v3 |

Every ✅ in the LoopZero column is a v1 feature requirement. Every 🔜 is documented roadmap.

**"5 minutes" phase breakdown (p95 targets):**

| Phase | Warm cache | Cold cache | What's included |
|-------|-----------|------------|-----------------|
| Pre-deploy validation | < 2s | < 2s | Secret check, loopzero.yaml parse |
| Code ingestion | < 5s | < 5s | Bundle upload or webhook mirror |
| Build (compile + image) | < 90s | < 180s | Clone, deps, build, Kaniko push |
| Release creation + gitops-push | < 10s | < 10s | Artifacts record, Git commit |
| Deployment activation (ArgoCD sync) | < 60s | < 60s | Pointer update, sync, health check |
| **Total** | **< 3 min** | **< 5 min** | |

The "5 minutes" target is an **end-to-end p95 for cold cache builds**. Warm cache deploys are significantly faster (~3 min). The build phase dominates — all other phases combined are < 90s. Build time depends on application size, dependency count, and cache hit rate.

**What the 5-minute target does NOT include:**

- Image pull time on first deploy to a new node (one-time cost, mitigated by pre-pull DaemonSet)
- DNS propagation for new environments (TTL-dependent, not platform-controlled)
- Database migrations (application responsibility, not platform)

**Reference hardware for SLO targets:**

The p95 targets above assume the following build node specification:

| Resource | Spec | Notes |
|----------|------|-------|
| Build node CPU | 4 vCPU (e.g., c5.xlarge) | Kaniko is CPU-bound during compilation |
| Build node RAM | 8 GB | Sufficient for Node.js/Go/Java builds with < 500 deps |
| Build node disk IOPS | 3000 IOPS (gp3 or equivalent) | Layer caching is I/O-intensive |
| Network to registry | > 100 Mbps | Image push is bandwidth-bound |

Build time scales roughly linearly with CPU for compilation-bound workloads. On 2 vCPU nodes, expect cold cache builds to take ~8-10 min (exceeding the 5-min target). On 8 vCPU nodes, expect ~2-3 min cold cache. The 4 vCPU reference is the minimum for meeting the SLO.

**What the user never needs to do:**

- Configure Kubernetes, Helm, or any infrastructure
- Set up a Git remote or deployment pipeline
- Understand how builds or deployments work internally
- Manually configure URLs between frontend and backend apps
- Think about rollback infrastructure — it's always one command away
- Learn new terminology to use the platform — no "ArgoCD", "GitOps", or "CRDs" in the user experience

**Platform engineering guarantees (internal):**

- Holds authoritative state only in CRDs and Git — never in ephemeral systems
- Never silently enters an inconsistent state without detection and alerting
- Never allows a single account to degrade the platform for others
- Serializes all concurrent mutations on the same `(tenant, project, environment)`
- Never marks a release as "active" without confirmed sync from the cluster

**What the platform explicitly does NOT solve:**

- Database migration rollback (the platform can revert deployments, not schemas)
- Application-level data repair after a bad deploy
- Custom DNS or certificate management outside `*.loopzero.example`
- Multi-region replication (v3 scope)
- Cost allocation per tenant (v3 scope)

---

## 2. Design Philosophy

### 2.1 TDD as behavior specification

Every feature starts as a Ginkgo test describing the expected behavior from the user or operator perspective. Implementation exists to make that test pass — not the other way around.

```
Behavior (Ginkgo BDD spec)
    └── Contract (Go interface)
          └── Implementation
                └── Integration test
                      └── E2E test
```

Production code is never written before a failing test.

### 2.2 Boundaries are contracts, not conventions

Every component exposes a strict Go or TypeScript interface. Communication between components is always via interface, never direct concrete struct. This enables:

- Full mocking in unit tests without infrastructure
- Implementation replacement without cascading refactors
- Testability of each layer in isolation

### 2.3 Local infra must be indistinguishable from production

The local loop with k3d, Tilt, and Taskfile must reproduce exactly the production behavior. No "development mode" with different behavior. Same Helm charts, same operators, same build pipeline.

### 2.4 Errors are contracts, not surprises

Every operation that can fail returns a typed error. Never `errors.New("something went wrong")`. Errors carry context, classification code, and are tested explicitly.

### 2.5 Design for failure, not for success

Every flow describes two paths: the happy path and the failure path. If a failure mode is not described, the system is not designed — it's sketched. Every asynchronous boundary is a potential split-brain point and must have a reconciliation strategy.

### 2.6 Isolation is not optional in multi-tenancy

Tenant isolation is a security and reliability invariant, not a feature. A single tenant must never be able to affect the availability, performance, or data of another tenant. This applies to compute, storage, network, and build resources.

### 2.7 Every mutation is serialized and idempotent

No two operations may concurrently mutate the same `(tenant, project, environment)`. Every operation carries an idempotency key. Retries produce the same result as the first attempt. This is not a performance optimization — it is a correctness requirement.

### 2.8 CLI-first, Vercel-quality DX

LoopZero is CLI-first. The dashboard will be built, but the primary interface is the terminal. Every feature is first specified as a CLI command before being implemented as an API endpoint or UI element.

The DX quality bar is the Vercel CLI. Every LoopZero command must be compared against the Vercel equivalent:

| Vercel | LoopZero | Purpose |
|--------|------|---------|
| `vercel` | `loopzero deploy` | Deploy |
| `vercel env add` | `loopzero secret set` | Environment variables |
| `vercel env pull` | `loopzero secret pull` | Download secrets to .env.local |
| `vercel logs` | `loopzero logs` | Real-time logs |
| `vercel rollback` | `loopzero rollback` | Revert to previous deploy |
| `vercel inspect` | `loopzero inspect` | Deploy details |
| `vercel ls` | `loopzero status` | List deployments |

If the output of a LoopZero command is more verbose, more confusing, or requires more arguments than the Vercel equivalent, the command must be revised before implementation.

---

## 3. Source of Truth & State Model

### 3.1 The problem: 5 systems with independent state

The deploy pipeline crosses:

```
CLI → API → Argo Workflows → Gitea → ArgoCD → Kubernetes
```

Each system maintains its own state. Without an explicit source of truth, the system will inevitably reach inconsistent states where Git says "deployed" but the cluster says "not found".

### 3.2 Source of truth hierarchy

The system has **two sources of truth**, each authoritative for a different concern:

| Concern | Source of Truth | Why |
|---------|----------------|-----|
| **Desired state** (what should be running) | Git (Gitea repos) | GitOps model: Git is the declarative intent |
| **Operational state** (what is actually happening) | CRDs (Kubernetes etcd) | Operators reconcile and report real status |

**Everything else is derived or cached:**

| System | Role | Not source of truth for |
|--------|------|------------------------|
| PostgreSQL | **Critical operational dependency**: build history, release queries, audit log, serialization locks, idempotency keys, SSE event journal | Desired state or cluster state |
| Argo Workflow CRDs | Build execution runtime | Release status after build completes |
| ArgoCD Application status | Sync state | Desired state (Git owns that) |
| MinIO | Artifact storage | Any state — purely content-addressed blobs |

**Release activation is independent of Git:**

A Release is an operational entity that exists when build artifacts are ready and signed — not when Git accepts a commit. Git stores the manifests for auditoria and for ArgoCD to converge the cluster, but the activation of a release does not depend on the success of `gitops-push`. The `environment_targets` table in PostgreSQL is the operational pointer that determines which release is desired for each environment.

```
Release lifecycle:     artifacts ready → Release READY → gitops-push → PENDING → ACTIVE
Activation mechanism:  environment_targets.desired_release_id (PostgreSQL)
Convergence executor:  ArgoCD (notified via API, not polling)
Git role:              Audit mirror of desired state, not activation mechanism
```

**Canonical precedence rule:**

When `environment_targets` (PostgreSQL) and Git (Gitea) disagree about which release should be running, `environment_targets` is authoritative. Git is the audit mirror — it converges to match `environment_targets`, not the other way around.

| Scenario | `environment_targets` | Git state | ArgoCD synced to | Resolution |
|----------|----------------------|-----------|-----------------|------------|
| Normal operation | release B | release B commit | release B | In sync. No action. |
| gitops-push pending | release B (READY) | release A (old) | release A | ArgoCD will be notified to sync release B once gitops-push completes. `EXPECTED_PENDING_MIRROR`. |
| gitops-push failed | release B (GIT_MIRROR_FAILED) | release A (old) | release B via direct apply | Async retry of gitops-push. When Git catches up, ArgoCD state matches naturally. |
| Git SHA diverges from desired_release_id | release B | unknown SHA (manual push, corruption) | unknown | **environment_targets wins.** API re-triggers gitops-push for release B to overwrite Git. Alert `LoopZeroGitDesiredStateDivergence`. |
| PostgreSQL restored from PITR | release A (stale) | release B | release B | **Git wins in this case only** (PITR is a disaster recovery event). API startup reconciler detects: `environment_targets.active != latest Git commit`. Reconciler updates `environment_targets` from Git. Alert `LoopZeroEnvironmentTargetReconciled`. |

**Invariant:** During normal operations, `environment_targets` is canonical. During disaster recovery (PITR), Git is canonical because it was not affected by the restore. The startup reconciler handles the transition.

### 3.3 PostgreSQL: critical operational dependency, not just cache

**Correction from v2:** PostgreSQL is not "just audit/query cache." Loss of PostgreSQL is an **operational incident**, not mere degradation.

**What depends on PostgreSQL being available and consistent:**

| Capability | Without PostgreSQL |
|-----------|-------------------|
| Serialization locks per (tenant, project, env) | **Cannot accept new deploys** — risk of concurrent mutations |
| Idempotency key deduplication | **Cannot safely retry** — risk of duplicate operations |
| Build history and release queries | CLI `loopzero status` returns empty or stale data |
| SSE event journal for replay | CLI reconnect after disconnect returns nothing |
| Audit trail | Compliance violation — cannot prove who did what |
| Side effect acknowledgement records | Cannot verify if irreversible warnings were already accepted |
| API crash recovery reconciliation | API restart takes longer, must rebuild state from Argo + Git |

**Therefore:**

- PostgreSQL has its own backup strategy (WAL archiving, point-in-time recovery)
- PostgreSQL health is part of the deep health check
- PostgreSQL failure triggers a `LoopZeroPostgresDown` critical alert
- While PostgreSQL is down: API rejects new mutations with `503 Service Unavailable` (reads continue from cached state where possible, but no new deploys/promotes/rollbacks)
- PostgreSQL is NOT the source of truth for desired state or cluster state — but it IS required for safe operation

**What can be reconstructed if PostgreSQL is lost:**

| Data | Reconstruction source | Completeness |
|------|----------------------|-------------|
| Release history (which releases, which commits) | Git log from Gitea tenant repos | Full |
| Build history (steps, timing, errors) | Argo Workflow CRDs (if not garbage-collected) | Partial — old workflows are pruned |
| Audit trail (who did what, when) | **Not reconstructible** | Lost |
| Idempotency keys | **Not reconstructible** (but safe — keys expire, new operations get new keys) | Acceptable loss |
| Side effect acknowledgements | **Not reconstructible** | Lost — require re-acknowledgement on next rollback |
| SSE event journal | **Not reconstructible** | Acceptable loss — only affects in-flight CLI reconnections |

### 3.4 State precedence by flow

When systems disagree, this is the resolution order:

| Flow | Conflict | Resolution |
|------|----------|------------|
| Deploy | Git has commit but ArgoCD shows OutOfSync | Wait up to `sync-timeout` (2 min). If still OutOfSync → release stays `pending`, alert fires. ArgoCD will retry sync autonomously. |
| Deploy | Argo Workflow succeeded but API DB has no record | API startup reconciler creates record from Argo CRD + Git commit. |
| Promote | Two promote commands with same idempotency key | Second is deduplicated, returns result of first. |
| Promote | Promote and rollback concurrent on same env | Serialization lock prevents this. Second command gets `409 OPERATION_IN_PROGRESS`. |
| Rollback | Git revert succeeded but ArgoCD sync fails | Release stays `rolling-back`. StateReconciler monitors. If ArgoCD retries fix it → release becomes `rolled-back`. If not → alert after `drift-threshold`. |
| Rollback | Two rollback commands | Serialization lock. Second gets `409`. |
| Preview | Preview expires during active build | Build cancellation triggered. Cleanup waits for cancellation to complete (or timeout after 60s). |
| Deploy | `environment_targets` says release B, Git still has release A | `environment_targets` is canonical. Git catches up via async gitops-push or retry. ArgoCD notified to sync release B directly. |
| Deploy | Git has commit for release B but `environment_targets` still points to release A | Stale Git state from a previous failed deploy. `environment_targets` is canonical. Git commit is orphaned — sweeper cleans after 24h. |
| Disaster Recovery | PostgreSQL PITR restores `environment_targets` to release A, but Git has release B | Git wins (PITR is disaster recovery). API startup reconciler updates `environment_targets` to match Git. Alert fires. |
| Rollback | Rollback changes `environment_targets` to release A while Git still has release B manifests | `environment_targets` is canonical. ArgoCD syncs release A manifests. Git mirror updated async. |

---

## 4. Concurrency, Serialization & Idempotency

### 4.1 The problem

Deploy, promote, rollback, cancel, and preview operations all mutate shared state (Git repos, namespaces, ArgoCD Applications, releases). Without serialization, concurrent operations on the same target produce undefined results: release reordering, conflicting commits, orphaned resources.

### 4.2 Environment lock

Every mutating operation on a `(tenant, project, environment)` tuple acquires an **advisory lock** in PostgreSQL before proceeding.

```sql
-- Advisory lock using hash of (tenant, project, environment)
SELECT pg_advisory_xact_lock(hashtext('acme:my-saas:production'));
```

**`hashtext` collision risk:** `hashtext()` returns `int4` (2³¹ values). With ~2000 active environments (500 tenants × 4 envs), the birthday paradox probability of collision is ~0.001% — acceptable for v1. If two unrelated environments hash to the same value, the only impact is **false serialization** (one waits for the other's lock), not data corruption. This is a liveness impact, not a safety impact.

Should collision become observable (monitor `loopzero_lock_wait_duration_seconds` for cross-tenant waits), upgrade to a two-int4 advisory lock:

```sql
-- Two-int4 advisory lock: zero collision risk
SELECT pg_advisory_xact_lock(
  hashtext(tenant_id || ':' || project_id),
  hashtext(environment)
);
```

**Properties:**

- Lock is held for the duration of the database transaction that wraps the mutation
- Lock is automatically released on transaction commit, rollback, or connection loss
- Lock is NOT held for the entire duration of the build — only for the critical section (status transitions, Git commits, release creation)
- If the lock cannot be acquired within 5 seconds, the operation returns `409 Conflict` with error code `OPERATION_IN_PROGRESS`

**Implementation note: transaction-scoped locks only**

The system uses `pg_advisory_xact_lock` (transaction-scoped), NOT `pg_advisory_lock` (session-scoped). This is a critical distinction:

- `pg_advisory_xact_lock`: Released automatically on transaction commit, rollback, or connection loss. Safe with connection pools.
- `pg_advisory_lock`: Persists across transactions within the same connection. NOT safe with connection pools — a reused connection could inherit stale locks.

If the API pod is killed while holding the lock, the PostgreSQL connection drops and the lock is released immediately. The next operation on the same environment acquires the lock without waiting.

**Drizzle ORM integration:**

```typescript
// Advisory lock within Drizzle transaction scope
async function withEnvironmentLock<T>(
  db: DrizzleClient,
  tenant: string,
  project: string,
  env: string,
  fn: (tx: DrizzleTransaction) => Promise<T>
): Promise<T> {
  return db.transaction(async (tx) => {
    // Set lock_timeout BEFORE acquiring advisory lock (enforces 5s wait limit)
    await tx.execute(sql`SET LOCAL lock_timeout = '5s'`);
    // Lock acquired — held until transaction commits or rolls back
    await tx.execute(
      sql`SELECT pg_advisory_xact_lock(hashtext(${`${tenant}:${project}:${env}`}))`
    );
    // All mutations inside fn() are serialized for this (tenant, project, env)
    return fn(tx);
  }, {
    isolationLevel: 'read committed',
    accessMode: 'read write',
  });
}
```

**Critical:** All queries that depend on the lock (build creation, release transitions, environment_targets updates) MUST execute within the same `tx` passed to `fn()`. Creating a separate transaction or using `db` directly would bypass the lock.

**What is serialized (holds the lock):**

- Build submission (check concurrent → create build record → submit workflow)
- Release creation (create release record → Git commit)
- Promote (validate source release → create target release → Git commit)
- Rollback (validate current release → create rollback record → Git revert)
- Cancel (validate build state → cancel workflow → update record)
- Secret hash update (AppSecretOperator → API → gitops-push with updated annotation)

**Concurrent build prevention (partial unique index):**

In addition to the advisory lock, a partial unique index enforces at the database level that only one build per `(tenant, project, environment)` can be in `QUEUED` or `RUNNING` status:

```sql
CREATE UNIQUE INDEX idx_one_active_build_per_env
  ON builds (tenant_id, project_id, environment)
  WHERE status IN ('QUEUED', 'RUNNING');
```

The validation in step 5 of the deploy flow (section 10.1) MUST execute inside the advisory lock transaction:

```sql
-- Inside pg_advisory_xact_lock transaction:
SELECT id FROM builds
  WHERE tenant_id = $1
    AND project_id = $2
    AND environment = $3
    AND status IN ('QUEUED', 'RUNNING')
  FOR UPDATE NOWAIT;
-- If row found → 409 DEPLOY_IN_PROGRESS
```

This guards against the race window between the two lock acquisitions in the deploy flow (steps 4-11 and steps 14b-14f), where the lock is released during Argo Workflow execution.

**What is NOT serialized (does not need the lock):**

- Build execution (Argo Workflow runs independently)
- SSE streaming (read-only)
- Status queries (read-only)
- Log streaming (read-only)

### 4.3 Idempotency

Every mutating API endpoint requires an `Idempotency-Key` header (UUID, generated by CLI).

```
POST /api/v1/builds
Idempotency-Key: 550e8400-e29b-41d4-a716-446655440000
```

**Mechanism:**

```sql
CREATE TABLE idempotency_keys (
    key         UUID PRIMARY KEY,
    endpoint    TEXT NOT NULL,
    tenant_id   TEXT NOT NULL,
    request_hash TEXT NOT NULL,      -- SHA256 of request body
    response    JSONB,               -- cached response
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '24 hours'
);
```

**Behavior:**

1. API receives request with `Idempotency-Key`
2. Check if key exists in `idempotency_keys`:
   - **Key exists, same request_hash:** Return cached response (HTTP 200/202 with original body)
   - **Key exists, different request_hash:** Return `422 Unprocessable Entity` — idempotency key reuse with different payload
   - **Key does not exist:** Proceed with operation, store key + response on success
3. Keys expire after 24 hours (cron job cleanup)

**Why this matters:**

- CLI can safely retry on network timeout without causing double deploys
- Webhook retries from external systems (GitHub PR events) are deduplicated
- `promote` retried after API crash returns the same release ID, not a second promotion

### 4.4 Conflict matrix

What happens when two operations collide on the same `(tenant, project, environment)`:

| First operation | Second operation | Result |
|----------------|-----------------|--------|
| deploy (building) | deploy | `409 DEPLOY_IN_PROGRESS` |
| deploy (building) | promote | `409 OPERATION_IN_PROGRESS` — cannot promote while build is running |
| deploy (building) | rollback | `409 OPERATION_IN_PROGRESS` — cannot rollback while deploy is in-flight |
| deploy (building) | cancel | **Allowed** — cancel acquires lock, cancels workflow, releases lock |
| promote (committing) | deploy | `409 OPERATION_IN_PROGRESS` |
| promote (committing) | rollback | `409 OPERATION_IN_PROGRESS` |
| rollback (reverting) | deploy | `409 OPERATION_IN_PROGRESS` |
| rollback (reverting) | promote | `409 OPERATION_IN_PROGRESS` |
| preview build | preview delete (same PR) | Delete waits for build cancel to complete |

### 4.5 Retry semantics

| Operation | Safe to retry? | Mechanism | Notes |
|-----------|---------------|-----------|-------|
| deploy | Yes | Idempotency key | Same key → same build ID returned |
| promote | Yes | Idempotency key | Same key → same release ID returned |
| rollback | Yes | Idempotency key | Same key → same revert commit |
| cancel | Yes | Idempotent by nature | Cancelling an already-cancelled build is a no-op |
| SSE reconnect | Yes | `Last-Event-ID` | Replay from journal |

---

## 5. State Machines

Every stateful entity has a formally defined state machine with valid transitions, timeout transitions, and terminal states. Invalid transitions are programming errors and crash the operation with a clear error.

### 5.1 Build state machine

```
                    ┌──────────────────────────────────────┐
                    │                                      │
                    ▼                                      │
 ┌─────────┐   ┌─────────┐   ┌───────────┐   ┌──────────┴──┐
 │ QUEUED   │──►│ RUNNING │──►│ SUCCEEDED │──►│ (terminal)   │
 └────┬────┘   └────┬────┘   └───────────┘   └─────────────┘
      │              │
      │              │        ┌───────────┐
      │              ├───────►│  FAILED   │──► (terminal)
      │              │        └───────────┘
      │              │
      │              │        ┌────────────┐
      │              └───────►│ CANCELLING │──► CANCELLED (terminal)
      │                       └────────────┘
      │
      │                       ┌────────────┐
      └──────────────────────►│ CANCELLED  │──► (terminal)
                              └────────────┘

Transitions:
  QUEUED → RUNNING           : Argo Workflow starts executing
  QUEUED → CANCELLED         : Cancel before workflow starts
  RUNNING → SUCCEEDED        : All workflow steps complete successfully
  RUNNING → FAILED           : Any workflow step fails (after retries exhausted)
  RUNNING → CANCELLING       : Cancel requested while running
  CANCELLING → CANCELLED     : Argo confirms workflow terminated
  CANCELLING → CANCELLED     : Timeout (60s) — force-mark as cancelled

Timeout transitions:
  QUEUED for > 5 minutes     : → FAILED (reason: QUEUE_TIMEOUT)
  RUNNING for > 30 minutes   : → FAILED (reason: BUILD_TIMEOUT)
  CANCELLING for > 60 seconds: → CANCELLED (reason: CANCEL_TIMEOUT)

Invalid transitions (programming errors):
  SUCCEEDED → anything
  FAILED → anything
  CANCELLED → anything
  RUNNING → QUEUED
```

### 5.2 Release state machine

```
 ┌─────────┐   ┌──────────┐   ┌───────────────┐   ┌──────────┐
 │  READY  │──►│ PENDING  │──►│ SYNC_FAILED   │──►│ (retry)  │──► PENDING
 └────┬────┘   └────┬────┘   └───────┬───────┘   └──────────┘
      │              │                │
      │              │                │ (drift-threshold exceeded)
      │              │                ▼
      │              │        ┌───────────────┐
      │              │        │ DRIFT_ALERT   │──► (manual intervention required)
      │              │        └───────────────┘
      │              │
      │              │        ┌───────────────┐
      │              ├───────►│    ACTIVE     │
      │              │        └───────┬───────┘
      │              │                │
      │              │                ├── superseded by new release ──► SUPERSEDED (terminal)
      │              │                │
      │              │                ├── rollback initiated ──► ROLLING_BACK
      │              │                │                              │
      │              │                │                              ├──► ROLLED_BACK (terminal)
      │              │                │                              │
      │              │                │                              └──► ROLLBACK_FAILED
      │              │                │                                        │
      │              │                │                                        └──► (manual)
      │              │                │
      │              │                └── cancel during sync wait ──► CANCELLED (terminal)
      │              │
      │              └── build failed before commit ──► (no release created)
      │
      └── gitops-push failed ──► GIT_MIRROR_FAILED
                                       │
                                       ├── async retry succeeds → PENDING
                                       └── deployment via fallback → PENDING

Transitions:
  (new) artifacts ready        → READY             : Build completed, image signed, SBOM generated
  READY → PENDING              : gitops-push succeeded, ArgoCD notified
  READY → GIT_MIRROR_FAILED    : gitops-push failed after retries
  GIT_MIRROR_FAILED → PENDING  : async retry of gitops-push succeeds
  PENDING → ACTIVE             : ArgoCD confirms sync success
  PENDING → SYNC_FAILED        : ArgoCD reports sync error
  PENDING → CANCELLED          : Timeout or explicit cancel
  SYNC_FAILED → PENDING        : ArgoCD retries automatically (up to 3 times)
  SYNC_FAILED → DRIFT_ALERT    : Drift persists beyond drift-threshold (5 min)
  ACTIVE → SUPERSEDED          : New release becomes ACTIVE for same env
  ACTIVE → ROLLING_BACK        : Rollback initiated (environment_target pointer updated)
  ROLLING_BACK → ROLLED_BACK   : environment_target pointer updated + ArgoCD sync confirmed
  ROLLING_BACK → ROLLBACK_FAILED : ArgoCD sync failed

Timeout transitions:
  READY for > 5 minutes        : → GIT_MIRROR_FAILED (reason: GIT_PUSH_TIMEOUT)
  PENDING for > 2 minutes      : → SYNC_FAILED (reason: SYNC_TIMEOUT)
  ROLLING_BACK for > 2 minutes : → ROLLBACK_FAILED (reason: ROLLBACK_TIMEOUT)

Terminal states: SUPERSEDED, ROLLED_BACK, CANCELLED
Requires manual intervention: DRIFT_ALERT, ROLLBACK_FAILED, GIT_MIRROR_FAILED (after retry exhaustion and EXPECTED_PENDING_MIRROR timeout)
```

### 5.3 PreviewEnvironment state machine

```
 ┌──────────┐   ┌──────────┐   ┌──────────┐   ┌──────────┐
 │ CREATING │──►│  ACTIVE  │──►│ EXPIRING │──►│ DELETED  │ (terminal)
 └────┬─────┘   └────┬─────┘   └────┬─────┘   └──────────┘
      │               │              │
      │               │              └── cleanup failed ──► CLEANUP_FAILED
      │               │                                          │
      │               │                                          └──► (GC catches)
      │               │
      │               └── explicit delete ──► DELETING ──► DELETED
      │                                           │
      │                                           └──► CLEANUP_FAILED
      │
      └── build failed ──► FAILED ──► DELETING ──► DELETED

Transitions:
  CREATING → ACTIVE          : Build succeeded + namespace + ingress ready
  CREATING → FAILED          : Build failed
  ACTIVE → EXPIRING          : TTL reached (ExpiresAt < now)
  ACTIVE → DELETING          : Explicit DELETE request
  EXPIRING → DELETED         : Cleanup complete (namespace + Git + MinIO + IngressRoute)
  EXPIRING → CLEANUP_FAILED  : Any cleanup step failed
  DELETING → DELETED         : Cleanup complete
  DELETING → CLEANUP_FAILED  : Any cleanup step failed
  FAILED → DELETING          : Automatic — failed previews are cleaned up
  CLEANUP_FAILED → DELETED   : Garbage collector catches orphaned resources

Cleanup order (each step tolerates failure of subsequent steps):
  1. Cancel active build (if any) — wait 60s or timeout
  2. Delete ArgoCD Application
  3. Delete Git manifests from Gitea
  4. Delete MinIO assets
  5. Delete namespace
  6. Delete PreviewEnvironment CR
```

**ArgoCD capacity note:**

Preview environments create one ArgoCD Application each. With 100 tenants and 10 previews per tenant, the system can have up to 1,000 preview Applications plus ~300 production/staging Applications (~1,300 total). The `argocd-application-controller` with 2 CPU / 4Gi can manage ~500 Applications at 60s reconciliation interval.

**Scaling strategy:**
- **< 500 total Applications:** Single controller, no sharding. Default v1 configuration.
- **500–1,500 Applications:** Increase controller resources to 4 CPU / 8Gi. Increase reconciliation interval for previews to 120s (previews are less latency-sensitive).
- **> 1,500 Applications:** Enable Application controller sharding with `--application-namespaces`. Shard by tenant groups. Each shard handles a subset of namespaces.

Metric to monitor: `loopzero_argocd_app_count` (added to section 15.2).

### 5.4 Promotion state machine

```
 ┌────────────┐   ┌──────────────┐   ┌───────────┐
 │ VALIDATING │──►│  COMMITTING  │──►│  SYNCING  │──► COMPLETED (terminal)
 └─────┬──────┘   └──────┬───────┘   └─────┬─────┘
       │                  │                  │
       │                  │                  └──► SYNC_FAILED
       │                  │
       │                  └──► COMMIT_FAILED (terminal — nothing to clean up)
       │
       └──► REJECTED (terminal — policy violation or missing acknowledgement)

Transitions:
  VALIDATING → COMMITTING    : Source release valid, policy passed, side effects acknowledged
  VALIDATING → REJECTED      : Policy violation or unacknowledged side effects
  COMMITTING → SYNCING       : environment_target pointer updated + ArgoCD notified via API
  COMMITTING → COMMIT_FAILED : PostgreSQL update or ArgoCD notification failed
  SYNCING → COMPLETED        : ArgoCD sync confirmed
  SYNCING → SYNC_FAILED      : ArgoCD sync failed or timed out (2 min)

Note: "COMMITTING" no longer means "Git commit". It means "committing the environment_target
pointer change". Git mirror happens asynchronously for audit.
```

### 5.5 Rollback state machine

```
 ┌────────────┐   ┌──────────────┐   ┌───────────┐
 │ VALIDATING │──►│  REVERTING   │──►│  SYNCING  │──► COMPLETED (terminal)
 └─────┬──────┘   └──────┬───────┘   └─────┬─────┘
       │                  │                  │
       │                  │                  └──► SYNC_FAILED
       │                  │
       │                  └──► REVERT_FAILED (terminal)
       │
       └──► REJECTED (terminal — unacknowledged irreversible side effects)
       └──► BLOCKED  (terminal — no previous release to rollback to)

Transitions:
  VALIDATING → REVERTING     : Previous release found, side effects acknowledged (or --force)
  VALIDATING → REJECTED      : Irreversible side effects without --force
  VALIDATING → BLOCKED       : No previous release exists or target release has no artifacts
  REVERTING → SYNCING        : environment_target pointer updated + ArgoCD notified via API
  REVERTING → REVERT_FAILED  : PostgreSQL update or ArgoCD notification failed
  SYNCING → COMPLETED        : ArgoCD sync confirmed
  SYNCING → SYNC_FAILED      : ArgoCD sync failed or timed out

Note: "REVERTING" no longer means "Git revert". It means "reverting the environment_target
pointer to the previous release". No git revert is issued. Git mirror is updated asynchronously.
```

---

## 6. Architecture

### 6.1 Four planes, strictly separated

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          DEVELOPER PLANE                                │
│                                                                         │
│   loopzero CLI  ──────────────────────────────────►  loopzero API (Fastify)    │
│   (TypeScript)           REST/JSON               (TypeScript)           │
│                          + Idempotency-Key                              │
│                          + Correlation-ID                               │
└────────────────────────────────────────────────────────────────────────┘
                                                         │
                                                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                          CONTROL PLANE                                  │
│                     (Management Cluster — k3d:mgmt)                     │
│                                                                         │
│  Platform components run with PriorityClass: loopzero-platform (10000)     │
│  All namespaces have ResourceQuota + LimitRange                        │
│                                                                         │
│  ┌──────────────────┐   ┌──────────────────┐   ┌──────────────────┐   │
│  │  LoopZero Operators   │   │  Gitea           │   │  PostgreSQL      │   │
│  │  (Go/kubebuilder) │   │  (GitOps Store)  │   │  (Operations DB) │   │
│  │                   │   │                  │   │  WAL archiving   │   │
│  │ TenantController  │   │ tenant repos     │   │  PITR enabled    │   │
│  │ AppSecretOp       │   │ helm charts      │   │                  │   │
│  │ ClusterCredsOp    │   │                  │   │                  │   │
│  │ StateReconciler   │   │                  │   │                  │   │
│  └──────────────────┘   └──────────────────┘   └──────────────────┘   │
│                                                                         │
│  ┌──────────────────┐   ┌──────────────────┐   ┌──────────────────┐   │
│  │  MinIO            │   │  Traefik         │   │  ArgoCD          │   │
│  │  (Artifact Store) │   │  (Ingress/TLS)   │   │  (GitOps Sync)   │   │
│  └──────────────────┘   └──────────────────┘   └──────────────────┘   │
│                                                                         │
│  ┌──────────────────┐   ┌──────────────────┐                           │
│  │  Prometheus       │   │  OTel Collector  │                           │
│  │  (Metrics)        │   │  (Traces/Logs)   │                           │
│  └──────────────────┘   └──────────────────┘                           │
└─────────────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                          BUILD PLANE                                    │
│                    (Build Cluster — k3d:build)                          │
│                                                                         │
│  Argo Workflows — per-tenant namespace with ResourceQuota              │
│  Credentials: ephemeral per-build (see section 17)                     │
│  Network: egress restricted to Gitea, MinIO, registry only             │
│  No access to: mgmt API, workload API, secrets                         │
└─────────────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                          WORKLOAD PLANE                                 │
│                    (Workload Cluster — k3d:workload)                    │
│                                                                         │
│  Per-namespace: ResourceQuota, LimitRange, NetworkPolicy, PriorityClass│
│  NetworkPolicy: deny-all ingress+egress, explicit allow-list           │
│  Egress: DNS restricted, no metadata endpoint, no external registry    │
│  Managed exclusively by ArgoCD (no direct kubectl from operators)      │
└─────────────────────────────────────────────────────────────────────────┘
```

### 6.2 Why four planes

| Separation | Reason |
|-----------|--------|
| **Build Plane** | Builds consume unpredictable CPU/memory. A tenant's heavy build must never degrade the API, operators, or other tenants' deploys. Build cluster can be independently scaled. Builds have zero access to workload or management cluster credentials. |
| **Control Plane** | Runs only platform components. Predictable resource usage. All components run with `loopzero-platform` PriorityClass. Platform ResourceQuotas protect components from each other. |
| **Workload Plane** | User apps only. ArgoCD has RBAC-scoped access. No direct access from operators. |
| **Observability** | Co-located on mgmt cluster. In production, can be externalized. |

**Locally:** 3 k3d clusters (mgmt, build, workload). In production: 3 separate clusters.

### 6.3 Control plane self-protection

Platform components are protected from tenant-induced load:

```yaml
# loopzero-system namespace limits
apiVersion: v1
kind: ResourceQuota
metadata:
  name: loopzero-system-quota
  namespace: loopzero-system
spec:
  hard:
    requests.cpu: "16"
    requests.memory: "32Gi"

---
# Per-component resource limits (examples)
# loopzero-api: 2 CPU, 4Gi (HPA 2-5 replicas)
# loopzero-tenant-controller: 1 CPU, 2Gi (single replica + leader election)
# loopzero-state-reconciler: 500m CPU, 1Gi (single replica + leader election)
# gitea: 2 CPU, 4Gi (single replica, PVC for data)
# postgresql: 2 CPU, 4Gi (single replica, PVC with WAL archiving)
# minio: 1 CPU, 2Gi (single replica, PVC for data)
# argocd-server: 1 CPU, 2Gi (HA mode with 2 replicas)
# argocd-application-controller: 2 CPU, 4Gi (single leader, shardable)
#   Capacity: ~500 Applications at 60s sync interval with 2 CPU
#   At 1000 Applications (100 tenants × 10 previews): controller sharding required
#   Sharding: --application-namespaces per tenant group (see section 14.5)
# prometheus: 1 CPU, 2Gi (with retention policy)
# otel-collector: 500m CPU, 1Gi
```

**Indirect load protection:**

- Gitea: rate limiting per API token (50 req/s per tenant)
- Gitea aggregate throughput budget (all tenants combined):
  - With 2 CPU / 4Gi: ~200 concurrent git operations (clone + push + API combined)
  - Budget allocation: 60% builds (clone + gitops-push), 20% ArgoCD (fetch), 20% mirrors + API
  - At 100 tenants with 2 concurrent builds each: ~200 clone operations = near capacity
  - Mitigation: builds are sequential per environment (advisory lock), reducing real concurrency
  - Scaling trigger: `loopzero_gitea_concurrent_operations > 160` (80% of capacity)
- Argo Workflows controller: workflow submission rate limited by API (not Argo itself)
- ArgoCD: sync frequency capped at 1 sync per 60s per Application
- PostgreSQL: connection pool per API instance (max 20 connections per replica)

---

## 7. System Components

### 7.1 loopzero CLI (`packages/cli` — TypeScript)

**Single responsibility:** Transform user intent into HTTP calls to the loopzero API. Zero business logic.

```typescript
interface LoopZeroClient {
  deploy(opts: DeployOptions): Promise<BuildStream>
  promote(opts: PromoteOptions): Promise<Release>
  rollback(opts: RollbackOptions): Promise<Release>
  cancel(opts: CancelOptions): Promise<void>
  preview(opts: PreviewOptions): Promise<PreviewEnvironment>
  status(opts: StatusOptions): Promise<AppStatus[]>
  logs(opts: LogOptions): AsyncIterable<LogLine>
  secretSet(opts: SecretSetOptions): Promise<void>
  secretList(opts: SecretListOptions): Promise<SecretMetadata[]>
  secretDelete(opts: SecretDeleteOptions): Promise<void>
}
```

**What the CLI never does:**

- Call kubectl directly
- Read the user's kubeconfig
- Expose internal infrastructure to the user (Gitea, ArgoCD, Helm, Kubernetes)

**Code ingestion**

The CLI supports two modes of code ingestion. In both modes, the Gitea internal store is completely invisible to the user.

**Mode A — GitHub Mirror (integration with existing repository)**

The user authorizes LoopZero via GitHub OAuth or GitHub App during `loopzero init`. LoopZero registers a webhook on the GitHub repository. On every `git push` to GitHub, the webhook notifies the LoopZero API, which mirrors the commit to the internal Gitea. The pipeline triggers from Gitea — GitHub is a transparent upstream.

```
git push origin main              ← user does this normally
  → GitHub webhook → LoopZero API (POST /internal/hooks/github)
    → mirror sync to internal Gitea
      → pipeline triggers from Gitea
```

**Mode B — Direct push via CLI (without GitHub)**

The user runs `loopzero deploy` in their local repository. The CLI detects the latest commit, packages the code (via `git bundle`), and uploads it to the LoopZero API over HTTPS using the token from `loopzero login`. The API pushes the bundle to the internal Gitea. The user never configures a git remote.

```
loopzero deploy                       ← user does this
  → CLI detects HEAD commit
  → CLI packages code (git bundle)
  → CLI uploads to API (POST /api/v1/source/upload)
    → API pushes to internal Gitea
      → pipeline triggers
```

**Pre-receive hooks (both modes)**

Before accepting any code, Gitea executes pre-receive hooks:
- Secret scan (e.g., truffleHog) — rejects commits with exposed credentials
- Structural validation of `loopzero.yaml`
- Maximum file size check
- Branch protection enforcement

If any validation fails, the code is rejected before entering the pipeline. The error is returned to the CLI with a clear message.

**Idempotency key generation:**

The CLI generates a UUID v4 `Idempotency-Key` for every mutating request. On retry (network error, timeout), the CLI reuses the same key. This guarantees at-most-once delivery with idempotency-key deduplication at the API level.

**Pre-deploy validation**

Before submitting any build, the CLI validates that all secrets declared in `loopzero.yaml` exist for the target environment. This prevents wasted build time when secrets are missing.

```
loopzero deploy --env production
  1. CLI reads loopzero.yaml, extracts secrets list per app
  2. CLI calls GET /api/v1/secrets?env=production&project=my-saas
  3. API returns list of existing secret names for that project/env
  4. CLI compares declared vs existing:
     ✓ DATABASE_URL    present
     ✗ STRIPE_KEY      not found
     ✗ JWT_SECRET      not found
  5. If any missing → abort with clear message:
     "Deploy cancelled. Define missing secrets:
      loopzero secret set STRIPE_KEY="..." --env production
      loopzero secret set JWT_SECRET="..." --env production"
```

**Secret management commands:**

```
loopzero secret set KEY=value --env production     → Create/update secret
loopzero secret list --env production              → List secret names (never values)
loopzero secret delete KEY --env production        → Delete secret
```

**SSE reconnection protocol:**

```
1. CLI connects to GET /builds/:id/stream
2. API sends events with incrementing numeric IDs from the SSE event journal
3. If connection drops:
   a. CLI waits 1s, then reconnects with Last-Event-ID header
   b. API looks up Last-Event-ID in journal:
      - Found: replay all events after that ID
      - Not found (journal expired): send JOURNAL_EXPIRED event,
        CLI falls back to polling GET /builds/:id every 5s
4. Journal retention: 1 hour per build (covers any reasonable disconnect)
5. After build completes: journal is retained for 10 more minutes, then pruned
6. Duplicate detection: CLI tracks last seen event ID, ignores events <= last seen
7. Ordering guarantee: events are ordered by journal sequence number, not wall clock
```

**SSE event journal (PostgreSQL):**

```sql
CREATE TABLE sse_events (
    id          BIGSERIAL PRIMARY KEY,
    build_id    TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    data        JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    INDEX idx_sse_events_build (build_id, id)
);

-- Pruning: events older than 1 hour after build completion
-- Runs via pg_cron every 15 minutes
```

---

### 7.2 loopzero API (`packages/api` — TypeScript/Fastify)

**Responsibility:** Control plane HTTP. Orchestrates internal components. Exposes REST for CLI. Emits SSE for streaming. **Requires PostgreSQL for safe mutation.**

```
POST   /api/v1/builds                    → Submit build/deploy (requires Idempotency-Key)
GET    /api/v1/builds/:id/stream         → SSE stream of logs (supports Last-Event-ID)
GET    /api/v1/builds/:id                → Build status
POST   /api/v1/builds/:id/cancel         → Cancel build (requires Idempotency-Key)
POST   /api/v1/releases/:id/promote      → Promote release (requires Idempotency-Key)
POST   /api/v1/releases/:id/rollback     → Rollback release (requires Idempotency-Key)
POST   /api/v1/previews                  → Create preview (requires Idempotency-Key)
DELETE /api/v1/previews/:id              → Delete preview (requires Idempotency-Key)
GET    /api/v1/apps/:app/status          → App runtime status
GET    /api/v1/apps/:app/logs            → SSE stream of runtime logs
GET    /api/v1/releases                  → Release history
GET    /api/v1/health                    → Liveness
GET    /api/v1/health/deep               → Deep check (Gitea, DB, Argo, ArgoCD)
GET    /api/v1/secrets                   → List secret names for project/env (never values)
POST   /api/v1/secrets                   → Create/update secret (requires tenant-admin)
DELETE /api/v1/secrets/:name             → Delete secret (requires tenant-admin)
POST   /api/v1/source/upload             → Receive code bundle from CLI (Mode B ingestion)
POST   /internal/hooks/github            → Receive GitHub webhook, trigger mirror sync (Mode A)
POST   /internal/hooks/gitea             → Receive Gitea post-receive hook, trigger pipeline
POST   /internal/secret-hash-changed     → Receive secret hash change from AppSecretOperator, serialized gitops-push
```

**What the API never does:**

- Apply manifests directly to the cluster (ArgoCD's responsibility)
- Accept mutations when PostgreSQL is unreachable
- Expose internal Kubernetes errors without translation
- Allow concurrent mutations on the same (tenant, project, environment)

**Internal contracts:**

```typescript
interface BuildOrchestrator {
  submit(req: BuildRequest): Promise<Build>
  stream(buildId: string): AsyncIterable<BuildEvent>
  cancel(buildId: string): Promise<void>
  reconcileRunningBuilds(): Promise<number>  // on startup
}

interface GitOpsStore {
  ensureTenantRepo(tenantId: string): Promise<Repo>
  commitRelease(release: Release, idempotencyKey: string): Promise<Commit>
  revertRelease(releaseId: string, idempotencyKey: string): Promise<Commit>
}

interface WorkflowEngine {
  submit(workflow: WorkflowSpec, credentials: EphemeralCredentials): Promise<WorkflowHandle>
  watch(workflowName: string): AsyncIterable<WorkflowEvent>
  cancel(workflowName: string): Promise<void>
  getStatus(workflowName: string): Promise<WorkflowStatus>
}

interface ReleaseStore {
  save(release: Release): Promise<void>
  list(filter: ReleaseFilter): Promise<Release[]>
  get(releaseId: string): Promise<Release>
  transition(releaseId: string, from: ReleaseState, to: ReleaseState): Promise<void>
  rebuildFromGit(tenantId: string): Promise<number>
}

interface EnvironmentLock {
  acquire(tenant: string, project: string, env: string, timeout: Duration): Promise<Lock>
  release(lock: Lock): Promise<void>
  // Lock is also auto-released on transaction commit/rollback
}

interface SourceIngestion {
  uploadBundle(tenantId: string, projectId: string, bundle: ReadableStream): Promise<Commit>
  mirrorFromGitHub(webhook: GitHubPushEvent): Promise<Commit>
  validatePreReceive(tenantId: string, commitSHA: string): Promise<ValidationResult>
}

interface DeploymentActivator {
  activate(release: Release, env: Environment): Promise<SyncHandle>
  // Encapsulates: update environment_targets + notify ArgoCD via API + poll sync result
  // If release.gitMirrorStatus = SYNCED: ArgoCD syncs from Git commit
  // If release.gitMirrorStatus = FAILED: emergency fallback path (see below)
}

**Emergency fallback path (gitMirrorStatus = FAILED):**

When a release must be activated but its manifests are not yet in Git, the DeploymentActivator follows this sequence:

```
1. Disable ArgoCD auto-sync on the Application:
   PATCH /api/v1/applications/{app}
   spec.syncPolicy.automated = null

2. Apply deploymentSpec directly to cluster:
   kubectl apply -f <rendered-manifests> --server-side --field-manager=loopzero-fallback

3. Update environment_targets.desired_release_id

4. Mark Application with annotation:
   loopzero.io/fallback-active = "true"
   loopzero.io/fallback-release-id = "{release-id}"

5. When gitMirrorStatus transitions to SYNCED (async retry succeeds):
   a. Restore ArgoCD auto-sync:
      PATCH spec.syncPolicy.automated = {prune: true, selfHeal: true}
   b. Trigger ArgoCD sync from Git commit (now available)
   c. Remove loopzero.io/fallback-* annotations
   d. Verify: ArgoCD-applied state matches fallback-applied state

6. If git mirror retry exhausted (6 retries, ~2.5h):
   a. Auto-sync remains disabled
   b. Alert LoopZeroFallbackStuck fires
   c. Operator must: fix Gitea → retry mirror → auto-sync re-enables automatically
```

**Invariant:** ArgoCD auto-sync is NEVER active while `loopzero.io/fallback-active = "true"`. This prevents ArgoCD from reverting the fallback-applied state to stale Git state.

**Field manager:** `loopzero-fallback` (distinct from `argocd-controller`) ensures server-side apply does not conflict with ArgoCD's managed fields. When ArgoCD resumes sync, it takes ownership of all fields via its own field manager.

interface SecretStore {
  list(tenantId: string, projectId: string, env: string): Promise<SecretMetadata[]>
  set(tenantId: string, projectId: string, env: string, name: string, value: string): Promise<void>
  delete(tenantId: string, projectId: string, env: string, name: string): Promise<void>
  // Values are stored in AppSecret CRs, synced by AppSecretOperator
  // This interface never returns secret values — only metadata (name, createdAt, rotationDueAt)
}
```

**API crash recovery (startup reconciler):**

```
On API startup:
  1. PITR DETECTION (must run first):
     a. For each (tenant, project, environment) in environment_targets:
        - Read environment_targets.active_release_id (PostgreSQL — possibly stale from PITR)
        - Read latest Git commit SHA for that env path (Gitea)
        - Read ArgoCD Application sync commit
     b. Extract Release-ID from latest Git commit via trailer:
        `git log -1 --format='%(trailers:key=Release-ID,valueonly)' -- {env-path}`
        If Release-ID from Git != environment_targets.desired_release_id
        (i.e., PostgreSQL was restored to an older state):
        - Mark this environment as PITR_DIVERGED
        - Update environment_targets.desired_release_id from Git
        - Update environment_targets.active_release_id from ArgoCD current sync
        - Alert LoopZeroEnvironmentTargetReconciled
        - Log: "PITR divergence detected for {tenant}/{project}/{env}:
                DB had release {A}, Git has release {B}, ArgoCD synced to {C}"
     c. If no divergence detected → normal startup, proceed to step 2

  2. BUILD RECONCILIATION:
     a. Query all Argo Workflows in state Running/Succeeded/Failed
        that have no corresponding completed build record in PostgreSQL
     b. For each:
        - RUNNING: re-attach watcher, create/update build record
        - SUCCEEDED: check if Git commit exists → create release record if missing
        - FAILED: create/update build record with failure status

  3. RELEASE RECONCILIATION:
     a. Query all releases in state PENDING older than sync-timeout
     b. Check ArgoCD sync status → transition to ACTIVE or SYNC_FAILED

  4. FALLBACK RECONCILIATION:
     a. Query all ArgoCD Applications with annotation loopzero.io/fallback-active = "true"
     b. For each: check if release.gitMirrorStatus is now SYNCED
        - If SYNCED: restore auto-sync, trigger ArgoCD sync from Git, remove annotations
        - If still FAILED: verify fallback-applied state is still correct in cluster

  5. Log count of reconciled items per category for audit
```

---

### 7.3 LoopZero Operators (Go / kubebuilder)

Four operators, distinct responsibilities. Each is an independent controller with its own CRD.

#### 7.3.1 TenantController

**CRD:** `Tenant` (loopzero.io/v1)

```yaml
apiVersion: loopzero.io/v1
kind: Tenant
metadata:
  name: acme
spec:
  displayName: "Acme Corp"
  plan: starter             # starter | growth | enterprise
  environments:
    - name: production
      cluster: workload-1
    - name: staging
      cluster: workload-1
    - name: dev
      cluster: workload-1
  gitops:
    repoName: loopzero-tenant-acme
    branch: main
  preview:
    maxEnvironments: 10
    ttlHours: 48
  limits:
    maxCPU: "4"
    maxMemory: "8Gi"
    maxStorage: "50Gi"
    maxBuildConcurrency: 2
status:
  phase: Ready
  gitopsRepo: https://gitea.loopzero-system/loopzero-admin/loopzero-tenant-acme
  conditions:
    - type: GitRepoReady
      status: "True"
    - type: NamespacesReady
      status: "True"
    - type: RBACReady
      status: "True"
    - type: QuotaEnforced
      status: "True"
    - type: NetworkPolicyReady
      status: "True"
    - type: BuildNamespaceReady
      status: "True"
```

**Reconcile loop:**

```
Observe Tenant CR
  → Ensure Gitea repo exists (idempotent: create if not exists)
  → Ensure namespaces exist per environment (idempotent)
  → Ensure ResourceQuota + LimitRange per namespace (idempotent)
  → Ensure RBAC (Role, RoleBinding, ServiceAccount) per namespace (idempotent)
  → Ensure NetworkPolicy per namespace (deny-all + allow-list) (idempotent)
  → Ensure ArgoCD Application per environment (idempotent)
  → Ensure build namespace + ResourceQuota in build cluster (idempotent)
  → Ensure PriorityClass for tenant workloads (idempotent)
  → Ensure ReconciliationReport CR per environment (idempotent)
  → Update Tenant.Status with Conditions
```

Every step is idempotent. Controller crash mid-reconcile resumes on restart. `ObservedGeneration` tracks which spec version has been fully reconciled.

#### 7.3.2 AppSecretOperator

**CRD:** `AppSecret` (loopzero.io/v1)

```yaml
apiVersion: loopzero.io/v1
kind: AppSecret
metadata:
  name: acme-my-saas-production-api
  namespace: loopzero-system
spec:
  tenantId: acme
  projectId: my-saas
  environment: production
  app: api
  secretRef:
    name: user-secrets-acme-my-saas-production-api
    namespace: loopzero-system
  rotation:
    enabled: true
    maxAgeHours: 720
    notifyBeforeHours: 168
status:
  syncedAt: "2025-01-01T12:00:00Z"
  targetNamespace: acme-my-saas-production
  hash: sha256:abc123
  workloadHash: sha256:abc123    # hash of actual Secret in workload namespace
  hashMatch: true                # mgmt hash == workload hash
  rotationDueAt: "2025-01-31T12:00:00Z"
  conditions:
    - type: Synced
      status: "True"
    - type: RotationCurrent
      status: "True"
```

**Hash verification invariant:**

On every reconcile loop (not just on change), the AppSecretOperator:

1. Reads the source secret from the management cluster and computes `hash(source)`.
2. Reads the target Secret from the workload namespace and computes `hash(target)`.
3. Compares:
   - If `hash(source) == hash(target)`: sets `status.hashMatch = true`, `status.workloadHash = hash(target)`.
   - If `hash(source) != hash(target)`: re-synchronizes immediately. Sets `status.hashMatch = false`.
   - If re-sync fails: increments `loopzero_secret_sync_hash_mismatch_total`. After 3 consecutive failures → alert `LoopZeroSecretSyncFailed`.
4. If target Secret does not exist in workload namespace: creates it. This handles cases where the Secret was deleted externally.

This periodic verification catches silent desync caused by:
- Operator crash between mgmt write and workload write
- External deletion of the workload Secret
- etcd restore that rolled back the workload cluster but not the mgmt cluster

#### 7.3.3 ClusterCredentialsOperator

**CRD:** `ClusterCredential` (loopzero.io/v1)

Manages workload cluster kubeconfigs. Distributes minimum-scope credentials to ArgoCD only.

#### 7.3.4 StateReconciler

**CRD:** `ReconciliationReport` (loopzero.io/v1)

Detailed design in section 13.

---

### 7.4 Argo Workflows (Build Engine)

**Build isolation:**

Builds run in the **build cluster**, not the management cluster.

**Credentials per build (ephemeral):**

Each build workflow receives short-lived, scoped credentials (see section 17.2). No long-lived tokens.

**Caching strategy (per-tenant isolated):**

```yaml
cache:
  # Layer 1: dependency cache
  dependencies:
    key: "{{ tenant }}/{{ hashFiles('pnpm-lock.yaml') }}"
    path: /workspace/.pnpm-store
    storage: minio://loopzero-builds/{{ tenant }}/cache/pnpm/

  # Layer 2: build output cache
  buildCache:
    key: "{{ tenant }}/{{ hashFiles('turbo.json', 'packages/*/src/**') }}"
    path: /workspace/.turbo
    storage: minio://loopzero-builds/{{ tenant }}/cache/turbo/

  # Layer 3: container layer cache (Kaniko)
  containerLayers:
    storage: minio://loopzero-builds/{{ tenant }}/cache/kaniko/
```

**Cache isolation:** Each tenant's cache is under its own MinIO prefix. Cache keys include tenant ID. A build for tenant A cannot read/write cache of tenant B (enforced by MinIO bucket policy + ephemeral credentials scoped to tenant prefix).

**Cache poisoning protection:** Cache is content-addressed. Build cache hit is verified by comparing the cache key hash with stored metadata. If the hash doesn't match, cache is treated as miss (not error).

**WorkflowTemplate: `loopzero-build-nodejs-v1`**

```yaml
steps:
  setup:
    - clone-repo
    - restore-cache
    - detect-changeset

  parallel (after setup):
    - security-scan
    - install-deps

  build (after install-deps):
    - build-apps

  publish (after build):
    - upload-frontend        # MinIO — content-addressed by release ID
    - build-image            # Kaniko — signed (see section 17.3)

  release (after publish):
    - generate-manifests
    - validate-manifests     # kubeconform
    - sign-manifests         # cosign (see section 17.3)
    - gitops-push            # atomic commit to Gitea

  finalize (after release, always runs):
    - save-cache
    - emit-summary
```

**`gitops-push` atomicity:**

The gitops-push step commits all manifests for a release in a single Git commit using the mandatory commit message format defined in section 17.5. The commit message includes `Release-ID`, `Correlation-ID`, and `Build-ID` as Git trailers. If the commit fails, no partial state exists in Git.

**`gitops-push` conflict resolution (non-fast-forward):**

The build gitops-push runs outside the advisory lock (the lock was released at step 11). Between lock release and gitops-push, another writer (e.g., `/internal/secret-hash-changed`) may have committed to the same Git path. When this happens, the Gitea API rejects the push with a non-fast-forward error.

Resolution strategy (fetch-merge-push):

```
On non-fast-forward rejection:
  1. Fetch latest Git HEAD for the environment path
  2. Read runtime-mutable annotations from HEAD manifests:
     - loopzero.io/secret-hash (may have been updated by secret rotation)
  3. Merge: overwrite generated manifests with HEAD values for runtime-mutable fields only.
     All other fields use the build-generated values (they are the new desired state).
  4. Create new commit on top of HEAD (not force-push)
  5. Retry push (up to 3 attempts with this strategy)
  6. If still failing after 3 merge attempts → treat as gitops-push failure
     (Release.gitMirrorStatus = FAILED, async retry takes over)
```

**Why merge instead of force-push:** Force-push would overwrite the secret hash update, causing silent inconsistency between the annotation and the actual secret value. The merge preserves both the new deployment manifests and any concurrent runtime-mutable field updates.

**Runtime-mutable fields:** Only `loopzero.io/secret-hash` is currently runtime-mutable (updated outside the build flow). This list is intentionally small and explicitly enumerated — any new runtime-mutable field must be added here and to the merge logic.

**If frontend assets were already uploaded to MinIO but gitops-push fails:** The assets are orphaned. The compensation mechanism (section 11) handles this — orphaned assets are cleaned up by a periodic sweep that checks MinIO for assets with release IDs that have no corresponding Git commit.

---

### 7.5 Gitea (Source Store + GitOps Store)

Gitea is an internal component. Users never interact with Gitea directly — code arrives via GitHub mirror (Mode A) or CLI upload (Mode B). See section 7.1 for ingestion details.

**1. Source repositories (user code)**

Each project has a source repository in Gitea that mirrors the user's code:

```
loopzero-source-{tenant}-{project}/       ← mirror of user's repo
├── packages/
├── loopzero.yaml
└── ...
```

Source repos are the input to the build pipeline. They are NOT the GitOps repos.

**2. GitOps repositories (generated manifests)**

```
loopzero-tenant-acme/
├── apps/
│   ├── my-saas/
│   │   ├── production/
│   │   │   ├── namespace.yaml
│   │   │   ├── deployment-api.yaml
│   │   │   ├── service-api.yaml
│   │   │   ├── hpa-api.yaml
│   │   │   ├── migration-job.yaml
│   │   │   └── argocd-app.yaml
│   │   ├── staging/
│   │   └── preview-pr-142/
```

**3. Helm Chart Registry (OCI)**

**Git commit idempotency:**

The gitops-push step includes the correlation ID in the commit message. Before committing, it checks if a commit with the same correlation ID already exists (searching the last 10 commits). If found, the step is a no-op. This prevents duplicate commits on retry.

**Gitea throughput analysis:**

Gitea (2 CPU, 4Gi, single instance) has a finite throughput budget shared across all tenants. The following operations compete for Gitea resources:

| Operation | Source | Frequency | Weight |
|-----------|--------|-----------|--------|
| `git clone` (source repo) | Build pipeline (per build) | Per deploy | Heavy (full repo clone) |
| `gitops-push` | Build pipeline (per build) | Per deploy | Medium (small commit) |
| `git fetch` | ArgoCD (per Application) | Every 3 min per App (or on-demand via API) | Light |
| `git bundle` upload | CLI Mode B | Per deploy | Heavy (full repo upload) |
| GitHub mirror sync | Webhook Mode A | Per push to GitHub | Medium |
| Backup (`git bundle` to MinIO) | Cron job | Every 6 hours | Heavy (brief) |

**Estimated capacity (2 CPU, 4Gi):**

- ~200 concurrent git operations before latency degrades
- With 100 tenants, 2 builds each = 200 concurrent clones (worst case, but advisory locks serialize per env)
- Realistic steady-state: ~50 concurrent operations (builds are short-lived and serialized per env)
- Burst capacity tested up to: 100 concurrent clones (v1 target, validated in load test Sprint 9)

**v1 conclusion:** Gitea single instance is sufficient for v1 (target: 100 tenants). The rate limiting per tenant (50 req/s) prevents any single tenant from monopolizing. Global throughput is monitored via `loopzero_gitea_concurrent_operations` and `loopzero_gitea_request_duration_seconds`.

**Gitea failure scenarios**

Gitea is the single most critical dependency in the platform. Every deploy, rollback, promote, and ArgoCD sync depends on Gitea availability. The following scenarios are explicitly handled:

**Scenario A — Gitea down during gitops-push:**

The build completed (image built, signed, SBOM generated, assets uploaded to MinIO), but the gitops-push step fails because Gitea is unreachable.

- Retry policy: 5 retries with exponential backoff (5s, 10s, 20s, 40s, 80s). Total: ~2.5 minutes.
- If retries exhausted: Build transitions to `FAILED`. Assets become orphans for the compensation sweeper (section 11.2).
- Artifact reuse: A retry of `loopzero deploy` with a new idempotency key creates a new build. Artifacts from the failed build are NOT reused — the sweeper cleans them. This avoids complexity of partial-state reuse.

**Mirror retry lifecycle:**

When `gitMirrorStatus = FAILED`, an async background job retries gitops-push with exponential backoff (5 min, 10 min, 20 min, 40 min). Maximum 6 retries over ~2.5 hours.

- If any retry succeeds: `gitMirrorStatus → SYNCED`, `EXPECTED_PENDING_MIRROR` cleared, normal operation resumes.
- If all retries exhausted (Gitea still down after ~2.5h): Release stays `GIT_MIRROR_FAILED`. Alert `LoopZeroMirrorSyncStale` fires. Operator must resolve Gitea and trigger manual mirror sync.
- If new deploy arrives while previous release is `GIT_MIRROR_FAILED`: New deploy proceeds normally. The old release's mirror retry is cancelled — the new release's gitops-push will contain the latest state. Only the most recent release per environment needs a mirror.

**Scenario B — Gitea down during ArgoCD sync wait:**

The gitops-push succeeded. The Release is `PENDING`. ArgoCD cannot pull manifests from Gitea.

- ArgoCD retries automatically with its own backoff (default: 5s interval, configurable via `argocd-cm`). This is confirmed behavior, not assumed.
- The Release stays `PENDING` until ArgoCD sync succeeds or the 2-minute sync timeout expires (→ `SYNC_FAILED`).
- When Gitea recovers, ArgoCD picks up the commit on its next poll. No manual intervention required unless the 2-minute timeout already fired.

**Scenario C — Gitea down during StateReconciler check:**

The StateReconciler cannot read the Gitea repo to get the latest commit SHA.

- The reconciler sets `clusterState = Error` (not `Drifted`). This is a distinct state meaning "unable to determine sync status".
- The alert `LoopZeroDriftDetected` is NOT fired for `clusterState = Error`. Instead, `LoopZeroGiteaDown` fires (from Gitea health check).
- The reconciler enters backoff (section 13.2 step 6) and retries when Gitea recovers.

**Scenario D — Gitea data loss (PVC loss):**

Gitea runs as a single replica with PVC. Loss of the PVC means loss of all Git history — the source of truth for desired state.

- **Backup strategy:** Gitea `dump` to MinIO daily. Gitea repos mirrored to MinIO via `git bundle` every 6 hours.
- **RTO:** < 1 hour (restore from MinIO backup, re-register repos in ArgoCD).
- **RPO:** 6 hours worst case (time between bundle mirrors).
- **HA in v1:** Gitea HA is NOT in scope for v1. Single replica is a known, documented risk. The mitigation is frequent backups and fast restore.
- Alert: `LoopZeroGiteaBackupStale` fires if last backup is older than 7 hours.

---

### 7.6 MinIO (Artifact Store)

**`loopzero-builds`** — Build artifacts, cache. TTL: 7 days.
**`loopzero-assets`** — Frontend assets in production. No TTL — cleaned by compensation sweeper.

**Bucket policy per tenant:**

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {"AWS": ["arn:aws:iam::build-acme"]},
    "Action": ["s3:GetObject", "s3:PutObject"],
    "Resource": [
      "arn:aws:s3:::loopzero-builds/acme/*",
      "arn:aws:s3:::loopzero-assets/acme/*"
    ]
  }]
}
```

This prevents tenant A's build from reading/writing tenant B's artifacts or cache.

---

### 7.7 Traefik (Ingress and TLS)

**URL scheme:** `{app}-{env}.{project}.{tenant}.loopzero.example`

The URL includes tenant, project, and environment as explicit levels. This prevents collisions when a tenant has multiple projects.

| Tenant | Project | App | Environment | URL |
|--------|---------|-----|-------------|-----|
| acme | my-saas | api | production | `https://api-production.my-saas.acme.loopzero.example` |
| acme | my-saas | dashboard | production | `https://production.my-saas.acme.loopzero.example` |
| acme | my-saas | api | staging | `https://api-staging.my-saas.acme.loopzero.example` |
| acme | my-saas | api | preview PR#142 | `https://api-pr-142.my-saas.acme.loopzero.example` |
| acme | internal-tools | admin | production | `https://admin-production.internal-tools.acme.loopzero.example` |

**Frontends** (static, served from MinIO via Traefik) use the bare environment URL without app prefix:

```
https://{env}.{project}.{tenant}.loopzero.example       → MinIO assets (frontend)
https://{app}-{env}.{project}.{tenant}.loopzero.example  → Service/Pod (server)
```

**TLS certificates:**

Multi-level wildcard certificates (`*.*.*.loopzero.example`) are not supported by any public CA, including Let's Encrypt (RFC 6125, Section 6.4.3). The platform uses **per-project wildcard certificates**, generated during TenantController reconcile via cert-manager:

- Certificate: `*.{project}.{tenant}.loopzero.example` (covers all apps and environments for a project)
- Issued via: cert-manager with Let's Encrypt (ACME DNS-01 challenge)
- One certificate per (tenant, project) pair
- Renewal: automatic via cert-manager (30 days before expiry)

Example: tenant `acme` with project `my-saas` gets certificate `*.my-saas.acme.loopzero.example`, which covers `api-production.my-saas.acme.loopzero.example`, `staging.my-saas.acme.loopzero.example`, etc.

---

### 7.8 PostgreSQL (Operations Database)

See section 3.3 for full analysis of PostgreSQL's role and failure impact.

**Backup strategy:**

- WAL archiving to MinIO (continuous)
- Base backup daily
- Point-in-time recovery (PITR) within 7-day window
- Backup verification: weekly restore test to temporary instance
- `LoopZeroPostgresBackupFailed` alert if backup is older than 25 hours

**Connection pool configuration:**

```typescript
// Required pool settings for advisory lock safety:
const pool = new Pool({
  max: 20,                        // per API replica
  idleTimeoutMillis: 30000,       // close idle connections after 30s
  connectionTimeoutMillis: 5000,  // fail fast if pool exhausted
  statement_timeout: 30000,       // 30s — prevents any single statement from running forever
});
```

- **`lock_timeout` (5s):** Set per-transaction before `pg_advisory_xact_lock`. This is what enforces the "5 second lock wait" documented in section 4.2. If the advisory lock cannot be acquired within 5s, the statement is cancelled and the API returns `409 OPERATION_IN_PROGRESS`. Set via `SET LOCAL lock_timeout = '5s'` inside the transaction (see Drizzle snippet in section 4.2).
- **`statement_timeout` (30s):** Set at pool level. Prevents any single statement (including the entire critical section after lock acquisition) from running longer than 30s. This is a safety net — not the lock timeout mechanism.
- These are distinct PostgreSQL mechanisms: `lock_timeout` applies only to lock acquisition waits; `statement_timeout` applies to total statement execution time. Both are needed.
- The pool validates connections before reuse. Dead connections (killed by PostgreSQL idle timeout or restart) are detected and replaced.
- Advisory locks from dead connections are automatically released by PostgreSQL — this is the crash recovery mechanism for serialization.

---

## 8. Responsibility Boundaries (ADR)

Every component has a single, clearly defined responsibility. This section defines what each component owns, what it delegates, and what it explicitly does NOT handle.

### 8.1 Responsibility matrix

| Component | Owns | Delegates to | Does NOT handle |
|-----------|------|-------------|----------------|
| **loopzero CLI** | User intent → HTTP call, idempotency key generation, SSE reconnection | Everything else to API | Business logic, kubectl, cluster access |
| **loopzero API** | Request validation, serialization (env lock), idempotency dedup, build orchestration, SSE event journal, release state transitions | Build execution to Argo, Git commits to Gitea, cluster sync to ArgoCD | Manifest application, cluster state management, secret storage |
| **TenantController** | Tenant lifecycle: namespace, RBAC, quota, NetworkPolicy, ArgoCD Application, build namespace | Git repo creation to Gitea API | Build execution, release management, secret syncing |
| **AppSecretOperator** | Secret sync from mgmt to workload, rotation tracking, hash change detection, audit | Encryption at rest to K8s (EncryptionConfiguration), Git manifest update to API (`/internal/secret-hash-changed`) | Secret creation (API does that), secret usage (app does that), direct Git writes (API serializes all gitops-push) |
| **StateReconciler** | Drift detection: compare Git vs ArgoCD vs cluster, emit metrics/alerts | Drift repair to ArgoCD (auto-sync) or platform operator (manual) | Modifying Git, modifying cluster state, making remediation decisions |
| **Argo Workflows** | Build execution: clone, build, test, publish, commit | Artifact storage to MinIO, Git to Gitea | Release tracking, deployment, rollback |
| **ArgoCD** | Cluster convergence executor: notified via API when environment_target changes, syncs desired state to cluster. Drift auto-repair within sync scope. | Health status reporting | Build execution, release management, tenant lifecycle. ArgoCD does NOT discover changes via Git polling — it is explicitly notified. |
| **Gitea** | Git repository hosting, OCI registry | Nothing | Build execution, deployment, any business logic |
| **PostgreSQL** | Durable storage for operational data | Backup to MinIO via WAL archiving | Cluster state, desired state, artifact storage |
| **MinIO** | Artifact and asset storage | Nothing | State management, any business logic |

### 8.2 What the platform does NOT solve

These are explicit non-goals documented to prevent scope creep and set correct expectations:

- **Database migration rollback:** The platform rolls back deployments (containers, services, ingress). It does NOT roll back database schema changes. If a migration is destructive, the developer must handle data recovery.
- **Application data repair:** A bad deploy that corrupts user data is the application's problem, not the platform's.
- **Cross-tenant communication:** Tenants are isolated. If two tenants need to talk, they use external APIs, not the platform.
- **Custom DNS:** All apps are served under `*.{project}.{tenant}.loopzero.example`. Custom domains are v3 scope.
- **Multi-region:** Single-region only. Multi-region replication is v3 scope.
- **Cost allocation:** No per-tenant cost tracking. v3 scope.
- **Application-level health checks beyond HTTP 200:** The platform checks if the app responds to /health. Application-specific health semantics are the app's concern.

---

## 9. Data Model

### 9.1 Core entities

```typescript
interface Release {
  id: string                    // rel-{8 chars hex}
  buildId: string               // build-{8 chars hex}
  tenantId: string
  projectId: string
  environment: Environment
  createdAt: Date
  createdBy: string
  correlationId: string

  apps: AppRelease[]

  // Artifacts — exist independently of Git
  artifacts: ReleaseArtifacts

  // Git mirror — nullable. null if gitops-push hasn't happened yet
  gitCommit: string | null
  gitMirrorStatus: GitMirrorStatus  // PENDING | SYNCED | FAILED

  status: ReleaseStatus         // see state machine 5.2

  sideEffects: SideEffect[]
}

interface ReleaseArtifacts {
  images: Array<{ app: string; digest: string; registry: string }>
  assets: Array<{ app: string; minioPrefix: string }>
  deploymentSpec: DeploymentSpec   // full spec persisted in PostgreSQL
  signedAt: Date
  sbomRef: string
}

enum GitMirrorStatus {
  PENDING = "pending",     // gitops-push hasn't happened yet
  SYNCED  = "synced",      // commit exists in Gitea GitOps repo
  FAILED  = "failed",      // gitops-push failed after retries
}

```

```sql
-- Operational pointer per environment (section 10 flows use this for activation)
CREATE TABLE environment_targets (
    tenant_id          TEXT NOT NULL,
    project_id         TEXT NOT NULL,
    environment        TEXT NOT NULL,
    desired_release_id TEXT NOT NULL REFERENCES releases(id),
    active_release_id  TEXT REFERENCES releases(id),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by         TEXT NOT NULL,
    correlation_id     TEXT NOT NULL,
    PRIMARY KEY (tenant_id, project_id, environment)
);
```

This table is the operational mechanism for deploy activation, promote, and rollback. Changing `desired_release_id` is an atomic PostgreSQL operation that does not depend on Gitea.

```typescript
interface SideEffect {
  type: SideEffectType
  description: string
  classification: SideEffectClassification
  acknowledgedAt?: Date
  acknowledgedBy?: string
}

// Side effect classifications determine what the platform does:
enum SideEffectClassification {
  // Platform can reverse this (e.g., config change, env var)
  REVERSIBLE = "reversible",

  // Platform cannot reverse, but impact is known and bounded
  // (e.g., DB migration that adds a column — column persists after rollback)
  IRREVERSIBLE_KNOWN = "irreversible-known",

  // Platform cannot reverse, impact is unknown
  // (e.g., arbitrary Job that calls external APIs)
  IRREVERSIBLE_UNKNOWN = "irreversible-unknown",

  // Side effect is external to the platform
  // (e.g., webhook notification, email sent)
  EXTERNALIZED = "externalized",
}
```

**Side effect classification rules:**

| Side effect type | Default classification | Can be overridden in loopzero.yaml? |
|-----------------|----------------------|-------------------------------|
| Migration job (Kubernetes Job) | `irreversible-known` | Yes — developer can mark as `reversible` if migration is idempotent |
| Schema change (detected from migration) | `irreversible-known` | No |
| Secret rotation | `irreversible-known` | No |
| External webhook | `externalized` | No |
| Config/env var change | `reversible` | No |
| Container image change | `reversible` | No |

**What each classification means for rollback:**

| Classification | Rollback behavior |
|---------------|------------------|
| `reversible` | Rollback proceeds automatically |
| `irreversible-known` | Rollback requires `--force` flag. CLI displays: "WARNING: This release executed [migration X]. Rollback will revert deployments but will NOT reverse [migration X]. Proceed?" |
| `irreversible-unknown` | Rollback requires `--force` flag. CLI displays: "WARNING: This release executed [Job Y] with unknown side effects. Manual verification may be required after rollback." |
| `externalized` | Informational only. "NOTE: This release triggered [webhook Z]. External systems may need manual cleanup." |

**v1 UX simplification:** The classification model above is implemented internally from day 1, but the CLI presents a simplified rollback experience:

```
loopzero rollback --env production
  → "Rolling back to release rel-abc123 (deployed 2h ago).
     ⚠ This release ran a database migration (AddUserIndex).
        Rollback will revert the deployment but NOT the migration.
     Confirm rollback? [Y/n]"
```

The CLI auto-detects side effects and presents human-readable warnings inline. The `--force` and `--acknowledge-side-effects` flags exist for automation/CI but are never required for interactive use — the `[Y/n]` confirmation replaces them. This prevents rollback friction during incidents while preserving the safety model for scripted workflows.

### 9.1.1 `loopzero.yaml` schema

**`loopzero.yaml` is the only infrastructure file a developer ever writes.** One file in the repository root. Everything else — URLs, networking, build pipelines, environments, previews — is derived automatically. This is the primary product interface of LoopZero.

```yaml
# loopzero.yaml — project root
project: my-saas
# Note: account (tenant) is NOT declared in loopzero.yaml.
# It is inferred from the authenticated user's account at deploy time.
# This prevents configuration drift and removes infrastructure concerns from the repo.

apps:
  api:
    type: server              # server | worker | frontend
    path: packages/api
    port: 3000
    env:
      NODE_ENV: "${env}"
      LOG_LEVEL: "info"
    secrets:
      - DATABASE_URL
      - JWT_SECRET

  worker:
    type: worker              # no ingress — background job
    path: packages/worker
    env:
      NODE_ENV: "${env}"
    secrets:
      - DATABASE_URL

  dashboard:
    type: frontend            # static build, served from MinIO
    path: packages/dashboard
    build:
      command: pnpm build
      output: dist/
    env:
      VITE_API_URL: "https://api-${env}.${project}.${tenant}.loopzero.example"
      VITE_ENV: "${env}"
```

**App types:**

| Type | Ingress | Runtime | Artifacts |
|------|---------|---------|-----------|
| `server` | `{app}-{env}.{project}.{team}.loopzero.example` | Kubernetes Deployment + Service | Container image |
| `worker` | None — no external access | Kubernetes Deployment (no Service) | Container image |
| `frontend` | `{env}.{project}.{team}.loopzero.example` | MinIO static hosting via Traefik | Static files in MinIO |

> **Terminology mapping:** `{team}` in URLs corresponds to `tenant_id` internally. Users see "team" or "account" — never "tenant". The internal CRDs and database continue to use `tenant_id` for technical precision.

**Template resolution (`resolve-config` step):**

The `generate-manifests` step in the Argo Workflow resolves all `${...}` templates before building:

```
${team}    → acme           (resolved from authenticated user's account)
${project} → my-saas        (from loopzero.yaml)
${env}     → production     (or staging, pr-142, etc.)
```

For frontend apps, resolved variables are injected as build args (e.g., `VITE_API_URL` is compiled into the bundle at build time). For server/worker apps, resolved variables become `env:` entries in the Deployment manifest.

**Invariant:** Backend URLs are deterministic and known before the build starts. No runtime service discovery. No manual URL configuration. The `loopzero.yaml` is the single source of truth for the relationship between apps.

**Fields:**

| Field | Required | Description |
|-------|----------|-------------|
| `tenant` | Yes | Tenant ID. Must match the Tenant CR. |
| `project` | Yes | Project ID. Unique within tenant. |
| `apps` | Yes | Map of app name → app configuration. |
| `apps.{name}.type` | Yes | `server`, `worker`, or `frontend`. |
| `apps.{name}.path` | Yes | Path to the app source relative to repo root. |
| `apps.{name}.port` | For `server` | Port the server listens on. |
| `apps.{name}.build.command` | For `frontend` | Build command. |
| `apps.{name}.build.output` | For `frontend` | Output directory for static files. |
| `apps.{name}.env` | No | Non-sensitive environment variables. Supports `${tenant}`, `${project}`, `${env}` templates. |
| `apps.{name}.secrets` | No | List of secret names (values set via `loopzero secret set`). See section 16.2. |

### 9.2 CRDs

```go
type TenantSpec struct {
    DisplayName  string             `json:"displayName"`
    Plan         PlanTier           `json:"plan"`
    Environments []EnvironmentSpec  `json:"environments"`
    GitOps       GitOpsSpec         `json:"gitops"`
    Preview      PreviewSpec        `json:"preview"`
    Limits       ResourceLimits     `json:"limits"`
}

type ResourceLimits struct {
    MaxCPU              resource.Quantity `json:"maxCPU"`
    MaxMemory           resource.Quantity `json:"maxMemory"`
    MaxStorage          resource.Quantity `json:"maxStorage"`
    MaxBuildConcurrency int              `json:"maxBuildConcurrency"`
}

type TenantStatus struct {
    Phase              TenantPhase        `json:"phase"`
    GitOpsRepo         string             `json:"gitopsRepo,omitempty"`
    Conditions         []metav1.Condition `json:"conditions,omitempty"`
    ObservedGeneration int64              `json:"observedGeneration,omitempty"`
    ActiveBuilds       int                `json:"activeBuilds"`
    TotalReleases      int                `json:"totalReleases"`
}

type PreviewEnvironmentSpec struct {
    TenantID    string        `json:"tenantId"`
    ProjectID   string        `json:"projectId"`
    PRNumber    int           `json:"prNumber,omitempty"`
    Branch      string        `json:"branch"`
    ExpiresAt   *metav1.Time  `json:"expiresAt"`
    ReleaseID   string        `json:"releaseId"`
}

type PreviewEnvironmentStatus struct {
    Phase      PreviewPhase       `json:"phase"`  // see state machine 5.3
    URL        string             `json:"url,omitempty"`
    CreatedAt  *metav1.Time       `json:"createdAt"`
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

---

## 10. Core Flows

### 10.1 `loopzero deploy` — Complete flow with serialization

```
0a. PRE-DEPLOY VALIDATION:
   a. CLI reads loopzero.yaml, extracts secrets list per app
   b. CLI calls GET /api/v1/secrets?env={env}&project={project}
   c. Compare declared secrets vs existing secrets
   d. If any secret missing → abort with message listing missing secrets
   e. If all present → proceed

0b. CODE INGESTION (Mode B — CLI push):
   a. CLI reads loopzero.yaml, validates structure
   b. CLI detects HEAD commit in local repo
   c. CLI packages code via git bundle
   d. CLI uploads bundle: POST /api/v1/source/upload
   e. API pushes bundle to Gitea source repo
   f. Gitea pre-receive hooks validate (secret scan, loopzero.yaml, file size)
   g. If hooks reject → CLI displays error, deploy aborted
   (Mode A — GitHub Mirror: steps 0a-0g happen automatically via webhook)

1. CLI generates Idempotency-Key (UUID v4)
2. CLI sends POST /api/v1/builds with Idempotency-Key header
3. API checks idempotency key:
   - Key exists with same request hash → return cached response
   - Key exists with different hash → 422
   - Key not found → proceed
4. API acquires environment lock: pg_advisory_xact_lock(hash(tenant:project:env))
   - Lock acquired → proceed
   - Lock timeout (5s) → 409 OPERATION_IN_PROGRESS
5. API validates request INSIDE advisory lock transaction:
   a. Tenant exists and is active
   b. Environment is valid
   c. No concurrent build: SELECT ... WHERE status IN ('QUEUED','RUNNING') FOR UPDATE NOWAIT
      If found → 409 DEPLOY_IN_PROGRESS (this query is inside the lock transaction, not a pre-check)
6. API creates Build record in PostgreSQL (state: QUEUED)
7. API generates ephemeral credentials for build (scoped to tenant, 1h TTL)
8. API submits Argo Workflow to build cluster with ephemeral credentials
9. API transitions Build to RUNNING when Argo confirms start
10. API stores idempotency key + response
11. API releases environment lock (transaction commits)
12. CLI connects to SSE stream
13. Build executes:
    a. clone-repo (from Gitea source repo)
    b. restore-cache
    c. resolve-config: read loopzero.yaml, resolve ${env}/${project}/${tenant} templates
    d. security-scan + install-deps (parallel)
    e. build-apps (inject resolved env vars as build args for frontends)
    f. upload-frontend (MinIO) + build-image (Kaniko, signed) (parallel)
    --- RELEASE CREATION POINT ---
    f+. API creates Release record (state: READY)
        - artifacts: image digests, asset prefixes, deploymentSpec
        - gitCommit: null, gitMirrorStatus: PENDING
        - Release exists from this point regardless of Git
    g. generate-manifests: env vars → inline env:, secrets → envFrom: secretRef,
       loopzero.io/secret-hash annotation → current hash of referenced secrets
    h. validate-manifests + sign-manifests
    i. gitops-push (atomic commit to Gitea GitOps repo)
       - Success: Release.gitCommit = SHA, Release.gitMirrorStatus = SYNCED
       - Non-fast-forward conflict: fetch-merge-push (re-read runtime-mutable
         fields from HEAD, merge into generated manifests, retry — see 7.4)
       - Failure (after 5 retries + 3 merge attempts): Release.gitMirrorStatus = FAILED
         → alert LoopZeroGitMirrorFailed
         → async retry in background
14. DEPLOYMENT ACTIVATION:
    a. API acquires env lock
    b. API updates environment_targets.desired_release_id = new release ID
    c. API notifies ArgoCD via API: POST /api/v1/applications/{app}/sync
       - If gitMirrorStatus = SYNCED: ArgoCD syncs from Git commit
       - If gitMirrorStatus = FAILED: fallback path — disable auto-sync, apply directly,
         mark fallback-active (see DeploymentActivator emergency fallback path in 7.2)
    d. API polls ArgoCD sync status (10s interval, 2 min timeout)
    e. On sync success: Release → ACTIVE, previous → SUPERSEDED
       Update environment_targets.active_release_id
    f. On sync timeout: Release → SYNC_FAILED
    g. API releases env lock
15. API emits final SSE event (deployed or failed)
16. CLI displays result
```

**Key properties:**

- Steps 4-11 hold the environment lock. No other mutation can start during this window.
- Step 7 generates ephemeral credentials that expire in 1 hour (see section 17.2).
- Step 14a-14g hold the environment lock for the deployment activation window. Release already exists from step 13f+.
- The Argo Workflow runs WITHOUT holding the lock. Only critical sections hold it.
- Step 0 (code ingestion) happens BEFORE the idempotency key check. If ingestion fails, no build is created.
- Step 13: the Argo Workflow clones from the Gitea source repo, NOT from GitHub. The source repo already has the validated code.
- Step 0a validates secrets BEFORE any code is uploaded or build is created. No resources are wasted on builds that will fail due to missing secrets.
- Step 13g: `generate-manifests` produces `env:` inline entries for non-sensitive variables from loopzero.yaml and `envFrom: secretRef` for secrets. The two types never mix — secrets never appear as values in Git.

**ArgoCD health gate (circuit breaker):**

Before step 14c (ArgoCD sync notification), the API checks ArgoCD health:

```
ArgoCD health check (before sync call):
  1. API calls GET /api/v1/applications — measures response time
  2. If response time > 5s OR connection error for 3 consecutive checks:
     → Set argocd_circuit = OPEN
     → Reject new deploy activations with 503 PLATFORM_DEGRADED:
       "ArgoCD is experiencing degraded performance. Deploy accepted but
        activation deferred. The system will activate automatically when
        ArgoCD recovers."
     → Release stays READY (not SYNC_FAILED — it hasn't been tried yet)
  3. Every 30s while circuit is OPEN: retry health check
     → If response < 2s for 2 consecutive checks: circuit = CLOSED
     → Resume pending activations in FIFO order
  4. Alert LoopZeroArgoCDCircuitOpen fires immediately when circuit opens
```

This prevents the cascading failure where ArgoCD degradation causes all environment locks to be held for the full 2-minute timeout, blocking all deploys across all tenants. The circuit breaker fails fast (< 1s) instead of slow (2 min timeout per deploy).

Metric: `loopzero_argocd_circuit_state{}` gauge (0 = closed, 1 = open).

### 10.2 `loopzero promote` (advanced workflow)

> **Product context:** For most users, production deploy is automatic — merge to the production branch triggers a deploy via GitHub webhook (Mode A). The `promote` command is an **advanced workflow** for teams that require explicit staging-to-production gates, approval checks, or controlled rollouts. It is not required for the default deploy experience.

```
1. CLI generates Idempotency-Key
2. API checks idempotency key
3. API acquires lock on TARGET environment
4. API validates:
   a. Source release exists and is ACTIVE
   b. No concurrent operation on target env
   c. Policy checks (approval, tests)
   d. Side effects classification:
      - All REVERSIBLE → proceed
      - Any IRREVERSIBLE_KNOWN or IRREVERSIBLE_UNKNOWN →
        require --acknowledge-side-effects flag
5. API creates Promotion record (state: VALIDATING → COMMITTING)
6. API creates Release for target env (state: READY, artifacts copied from source release)
7. API updates environment_targets.desired_release_id = new release ID (atomic PostgreSQL op)
8. API notifies ArgoCD via API: POST /api/v1/applications/{app}/sync
9. API transitions to SYNCING, polls ArgoCD sync status (10s, 2 min timeout)
10. On sync success: Release → ACTIVE, previous → SUPERSEDED
    Update environment_targets.active_release_id
11. Release environment lock
12. Async: gitops-push manifests to target env path in Gitea (audit mirror, not activation)
```

### 10.3 `loopzero rollback`

> **Product context:** Rollback is available as `loopzero rollback` in the CLI (primary) and as a "Revert to previous deploy" button in the dashboard (v2). The CLI presents inline warnings about irreversible side effects (migrations, external calls) and asks for `[Y/n]` confirmation — no flags or technical knowledge required for interactive use. The flow below is the underlying mechanism; both CLI and dashboard call the same API.

```
1. CLI generates Idempotency-Key
2. API checks idempotency key
3. API acquires lock on environment
4. API validates:
   a. Current release is ACTIVE
   b. Previous release exists (is SUPERSEDED or ROLLED_BACK)
   c. Side effects of CURRENT release:
      - All REVERSIBLE → proceed
      - Any IRREVERSIBLE → require --force flag
      - Display specific warnings per side effect
5. API creates Rollback record (state: VALIDATING → REVERTING)
6. API updates environment_targets.desired_release_id = target_release.id
   (atomic PostgreSQL operation — does not depend on Gitea)
7. API notifies ArgoCD via API: POST /api/v1/applications/{app}/sync
   - If target_release.gitMirrorStatus = SYNCED: ArgoCD syncs from existing commit
   - If target_release.gitMirrorStatus = FAILED: fallback path — disable auto-sync,
     apply directly, mark fallback-active (see 7.2)
8. API transitions to SYNCING, polls ArgoCD sync status (10s, 2 min timeout)
9. On sync success: current release → ROLLED_BACK, target release → ACTIVE
   Update environment_targets.active_release_id
10. Release environment lock
11. Async: gitops-push rollback manifests to Gitea (audit mirror)

REMOVED: git revert. Rollback no longer depends on Gitea.
```

### 10.4 Preview environment

**Primary flow — automatic via GitHub PR (zero user action):**

```
1. Developer opens PR on GitHub
2. GitHub webhook notifies LoopZero API (POST /internal/hooks/github)
3. API detects new PR branch, creates PreviewEnvironment
4. API acquires lock on (tenant, project, preview-{branch})
5. API creates PreviewEnvironment CR (state: CREATING)
6. TenantController reconciles: namespace, RBAC, NetworkPolicy, quota
7. Argo Workflow builds with environment=preview
8. On success: gitops-push to preview path, ArgoCD syncs
9. PreviewEnvironment transitions to ACTIVE
10. API posts preview URL as GitHub PR comment via GitHub API
11. Every subsequent push to the PR branch triggers a new build + deploy to the same preview
12. PR merge or close → PreviewEnvironment transitions to DESTROYING → cleanup

Preview URL format: https://{app}-pr-{number}.{project}.{team}.loopzero.example
```

**Secondary flow — manual via CLI (for branches without PR):**

```
1. Developer runs: loopzero deploy --preview --branch feature-x
2. Same steps 4-9 as above
3. No GitHub PR comment (no PR exists)
```

**TTL enforcement (three mechanisms):**

```
Primary:   Controller requeue at ExpiresAt
Secondary: Reaper scan every 5 minutes (catches controller restart)
Tertiary:  Garbage collector every 1 hour (catches orphaned namespaces)

Cleanup (section 11): ordered, each step tolerates failure of subsequent steps
```

---

## 11. Cancellation & Compensation

### 11.1 Build cancellation

```
loopzero cancel --build build-abc123

Cancellation points (what gets cancelled at each stage):

QUEUED:
  → Mark build CANCELLED in DB
  → Remove workflow from Argo queue
  → Nothing to clean up
  → Cost: zero

RUNNING (before gitops-push):
  → API sends cancel to Argo Workflows
  → Argo terminates running pods (SIGTERM, 30s grace, SIGKILL)
  → Build transitions RUNNING → CANCELLING → CANCELLED
  → Orphaned assets: none (publish steps haven't run or are atomic)
  → Cache: save-cache still runs in finalize (always-run step)
  → Cost: wasted build time only

RUNNING (during gitops-push):
  → Cancellation is BLOCKED until gitops-push completes or fails
  → Rationale: partial Git commit is worse than completing the commit and
    then rolling back if needed
  → If gitops-push succeeds: release already exists (READY from step 13f+),
    transitions to CANCELLED. environment_targets is NOT updated.
  → If gitops-push fails: release is READY with GIT_MIRROR_FAILED,
    transitions to CANCELLED. No deployment activation occurs.

RUNNING (after gitops-push, during ArgoCD sync wait):
  → Release transitions PENDING → CANCELLED
  → environment_targets.desired_release_id reverted to previous release
  → ArgoCD notified to sync previous release
  → No git revert needed — Git mirror is updated asynchronously
```

### 11.2 Compensation mechanisms

Compensation handles resources orphaned by failures, cancellations, or partial completions.

| Orphan type | How it happens | Compensation | Frequency |
|-------------|---------------|-------------|-----------|
| MinIO assets for GIT_MIRROR_FAILED releases | gitops-push failed after artifacts uploaded | Sweeper: check releases in GIT_MIRROR_FAILED state for > 24h. If async retry exhausted → delete assets + mark release FAILED | Every 1 hour |
| Container images for GIT_MIRROR_FAILED releases | gitops-push failed after image push | Sweeper: check releases in GIT_MIRROR_FAILED state for > 24h. If async retry exhausted → delete images + mark release FAILED | Every 1 hour |
| Namespace without PreviewEnvironment CR | CR deleted but namespace deletion failed | GC: list namespaces with label `loopzero.io/preview=true`, check if CR exists. If not → delete | Every 1 hour |
| ArgoCD Application without Tenant CR | Tenant deleted but ArgoCD cleanup failed | GC: list ArgoCD Applications with `loopzero.io/tenant` label, check if Tenant CR exists. If not → delete | Every 1 hour |
| Git manifests without active release | Release rolled back but manifests not cleaned | Not cleaned — Git history is immutable. Old manifests exist in old commits, not in HEAD. ArgoCD only syncs HEAD. | N/A |
| Build workflow stuck in Running | Argo controller crash or node failure | Argo's own garbage collector handles stuck workflows (configured TTL: 1 hour) | Continuous |
| Stale idempotency keys | Normal accumulation | PostgreSQL cron: delete keys older than 24 hours | Every 1 hour |
| Gitea token without active workflow | Build pod killed before finalize step | Sweeper: list tokens with prefix `build-`, check if corresponding workflowId exists in Argo. If not → revoke via `DELETE /api/v1/users/{admin}/tokens/{token_name}` | Every 1 hour |
| Stale SSE events | Normal accumulation | PostgreSQL cron: delete events for completed builds older than 1 hour | Every 15 minutes |

### 11.3 What the platform does NOT compensate

- **Database changes from migrations:** Compensation cannot reverse DDL/DML. This is the developer's responsibility.
- **External API calls from Jobs:** If a Job called an external API, compensation cannot undo that call.
- **Emails/notifications sent:** Cannot be unsent.

These are classified as `externalized` or `irreversible-unknown` side effects (section 9.1).

---

## 12. Failure Modes & Recovery

### 12.1 Failure matrix

| Component | Failure Mode | Detection | Impact | Recovery | Auto? |
|-----------|-------------|-----------|--------|----------|-------|
| **loopzero API** | Crash/restart | K8s liveness probe | CLI loses SSE stream, no new mutations accepted during restart | Startup reconciler rebuilds state from Argo + Git + DB. CLI reconnects via SSE with Last-Event-ID. | Yes |
| **loopzero API** | Overloaded | Prometheus `loopzero_api_request_duration_p99 > 5s` | Slow responses | HPA scales API pods. If downstream is the bottleneck, scaling won't help → alert for investigation. | Partial |
| **PostgreSQL** | Down | Connection error, health check | **No new mutations accepted.** Reads served from cached state where possible. | API returns 503 for mutations. PostgreSQL restarts. On recovery, normal operations resume. | Yes (K8s restart) |
| **PostgreSQL** | Data loss | Backup verification failure, checksum mismatch | Historical data lost. Operational data partially reconstructible. Audit trail lost. | PITR from WAL archive. Rebuild release index from Git. Idempotency keys regenerated naturally. | Manual |
| **Argo Workflows** | Workflow fails | Argo marks Failed, API receives event | Build doesn't complete. No Git commit → no ArgoCD sync → cluster unchanged. | User retries `loopzero deploy`. New idempotency key, new build. | Manual |
| **Argo Workflows** | Controller crash | K8s restarts, running workflows resume from last checkpoint | Brief delay in new builds | Argo controller is stateless, CRDs preserve workflow state. | Yes |
| **Build cluster** | Unreachable | API connection error on workflow submit | No new builds. Existing builds in-flight may be affected. | API returns 503 with `BUILD_CLUSTER_UNAVAILABLE`. Retries 3x with backoff, then fails. | Manual |
| **Gitea** | Down (clone phase) | API health check, Prometheus up metric | Builds fail at clone-repo step | Gitea restarts. Build retry with new idempotency key. | Yes (K8s) |
| **Gitea** | Down (gitops-push phase) | Workflow step failure after 5 retries | Release created as READY, transitions to GIT_MIRROR_FAILED. Async retry in background. Deployment activation can proceed via deploymentSpec fallback. | Automatic retry of gitops-push. If retry succeeds → normal flow. If exhausted → alert, operator intervenes. | Partial |
| **Gitea** | Down (ArgoCD sync) | ArgoCD sync status, API sync timeout | Release stays PENDING → SYNC_FAILED after 2 min timeout. | ArgoCD retries on Gitea recovery. If timeout fired, user retries deploy. | Partial |
| **Gitea** | Down (StateReconciler) | Reconciler sets clusterState=Error | Drift detection paused. No false LoopZeroDriftDetected alerts. | Reconciler resumes on Gitea recovery. Backoff per section 13.2. | Yes |
| **Gitea** | Data loss (PVC) | Backup verification, manual detection | All desired state lost. Deploys, promotes, rollbacks impossible. | Restore from MinIO backup (git bundles). RPO: 6h. RTO: < 1h. | Manual |
| **ArgoCD** | Sync failure | ArgoCD Application status, StateReconciler | Deploy committed to Git but not applied to cluster. Release stays PENDING → SYNC_FAILED. | ArgoCD retries automatically (up to 3 times). StateReconciler alerts if drift persists > 5 min. | Partial |
| **ArgoCD** | Drift (external modification) | StateReconciler | Cluster state doesn't match Git. | ArgoCD auto-sync repairs drift (if enabled). StateReconciler alerts. | Partial |
| **Rollback** | Gitea down during rollback | Not applicable — rollback does not depend on Gitea | Rollback proceeds normally via environment_targets + ArgoCD API. Git mirror updated asynchronously when Gitea recovers. | No recovery needed — rollback is independent of Gitea. | Yes |
| **Workload cluster** | Unreachable | ArgoCD health check, StateReconciler | Deploys committed to Git but can't be applied. Apps unavailable. | Alert. Wait for cluster recovery. ArgoCD will sync when cluster is reachable. | Manual |
| **MinIO** | Down | Health check, build cache miss | Builds run without cache (slower). Asset serving fails. | MinIO restarts. Cache miss is tolerated (build continues). Asset serving recovers on restart. | Yes (degraded) |
| **Build queue** | Tenant queue full | API pre-check on build submission | New builds for that tenant rejected with 429 | User waits for current builds to complete, or cancels pending builds | No |
| **Network: mgmt↔build** | Partition | API connection timeout | New builds fail. In-flight builds may continue (Argo is independent). | Alert. Wait for resolution. | Manual |
| **Network: mgmt↔workload** | Partition | ArgoCD sync timeout | Deploys committed but not applied. | StateReconciler detects. Alert. Wait for resolution. | Manual |

### 12.2 Recovery invariants

1. **No build ever produces a partial Git commit.** gitops-push is atomic per commit. It either commits all manifests or none.
2. **No release is marked ACTIVE without ArgoCD sync confirmation.** Until sync succeeds, release stays READY, PENDING, or SYNC_FAILED. A release exists from the moment artifacts are ready (READY state), independent of Git.
3. **The API can crash and restart without losing any in-flight build.** Build state lives in Argo Workflow CRDs. API startup reconciler recovers.
4. **Every operation is idempotent.** Retries with the same idempotency key return the same result.
5. **Every controller reconcile step is idempotent.** Running twice produces same result.
6. **Cache failure is not build failure.** Every MinIO cache operation has fallback to proceeding without cache.
7. **PostgreSQL loss blocks mutations but does not lose in-flight builds.** Builds continue in Argo. Recovery reconstructs from Git + Argo CRDs.
8. **Orphaned resources are eventually cleaned.** Compensation sweepers run hourly. Nothing is orphaned permanently.
9. **Concurrent mutations on the same environment are impossible.** Advisory locks in PostgreSQL prevent this.

---

## 13. State Reconciliation (Detailed)

### 13.1 StateReconciler design

The StateReconciler is an operator that continuously compares desired state (Git) with actual state (cluster) and reports drift. It is **observational only** — it never modifies Git or cluster state.

**CRD: ReconciliationReport**

```yaml
apiVersion: loopzero.io/v1
kind: ReconciliationReport
metadata:
  name: acme-production-report
  namespace: loopzero-system
spec:
  tenantId: acme
  projectId: my-saas
  environment: production
  checkInterval: 60s
  driftThreshold: 300s
  excludeFields:              # fields mutated by runtime, not drift
    - "metadata.resourceVersion"
    - "metadata.generation"
    - "metadata.uid"
    - "metadata.creationTimestamp"
    - "metadata.managedFields"
    - "status"                # status is runtime state, not desired state
  excludeAnnotations:
    - "kubectl.kubernetes.io/last-applied-configuration"
    - "deployment.kubernetes.io/revision"
  excludeLabels: []
  authorizedControllers:       # extensible authority registry
    - name: "HPA"
      fields: ["spec.replicas"]
    - name: "cert-manager"
      fields: ["metadata.annotations/cert-manager.io/*"]
status:
  lastCheckedAt: "2025-01-01T12:05:00Z"
  gitHeadCommit: "abc123"
  argoSyncCommit: "abc123"
  clusterState: Synced         # Synced | Drifted | Checking | Error
  driftDetectedAt: null
  driftResolution: null        # AutoRepaired | ManualRequired | null
  driftDetails:
    - resource: "apps/v1/Deployment/api"
      field: "spec.replicas"
      gitValue: "3"
      clusterValue: "5"
      cause: "HPA scaled up — this is expected drift"
  conditions:
    - type: InSync
      status: "True"
```

### 13.2 Reconciliation algorithm

```
Every checkInterval:

1. INPUT COLLECTION
   a. Read Gitea: latest commit SHA for tenant/env path
   b. Read ArgoCD: Application sync status + last synced commit
   c. Read cluster: list Deployments, Services, ConfigMaps in namespace
      with label selector loopzero.io/tenant={tenant},loopzero.io/env={env}

2. LAYER 1: Git↔ArgoCD comparison
   If gitHeadCommit != argoSyncCommit:
     If ArgoCD status == "Syncing": → no drift, sync in progress
     If ArgoCD status == "OutOfSync" for < driftThreshold: → no drift, wait
     If ArgoCD status == "OutOfSync" for > driftThreshold: → DRIFT detected
     If ArgoCD status == "SyncFailed": → DRIFT detected (with error detail)

3. LAYER 2: ArgoCD↔Cluster comparison (only if Layer 1 is in sync)
   For each resource in ArgoCD Application:
     Compare ArgoCD desired state with actual cluster state
     EXCLUDE:
       - Fields in spec.excludeFields (runtime-mutated fields)
       - Annotations in spec.excludeAnnotations
       - Labels in spec.excludeLabels
       - HPA-managed replica counts (detected via HPA presence)
       - Status subresources (these are runtime state)
     If difference found after exclusions → potential drift

4. DRIFT CLASSIFICATION (authority-based model)

   For each detected difference, classify by field authority — not by resource type:

   a. Determine field authority:
      - Is the mutated field owned by a REGISTERED CONTROLLER?
        (HPA → spec.replicas, PDB → pod disruption fields, VPA → resource requests)
        → Check authority registry (see below)
      - Is it a known runtime-mutated annotation/label?
        → Check authority registry
      - Is it an active rollout? (Deployment rollout status = Progressing)
        → EXPECTED_TRANSIENT (clears automatically)
      - Is it EXPECTED_PENDING_MIRROR? (environment gitMirrorStatus = FAILED
        and deployment activated via direct apply fallback)
        → EXPECTED_PENDING_MIRROR (clears when gitMirrorStatus = SYNCED)
        TIMEOUT: If persists > 1 hour → escalate to MIRROR_STALE (requires operator)
      - Otherwise → REAL DRIFT

   b. Authority registry (extensible without modifying the algorithm):
      ┌────────────────────────┬──────────────────────────┬──────────────┐
      │ Controller             │ Fields it may mutate     │ Status       │
      ├────────────────────────┼──────────────────────────┼──────────────┤
      │ HPA                    │ spec.replicas            │ Built-in     │
      │ VPA (if enabled)       │ resources.requests/limits│ Built-in     │
      │ PDB                    │ disruption-related fields│ Built-in     │
      │ cert-manager           │ tls secret annotations   │ Built-in     │
      │ ArgoCD (self-managed)  │ sync annotations         │ Built-in     │
      └────────────────────────┴──────────────────────────┴──────────────┘

      New controllers are added to this registry via ReconciliationReport
      spec.authorizedControllers field — no code change required.

   c. Classification output:
      EXPECTED           → No drift. Metric: 0. No alert.
      EXPECTED_TRANSIENT → No drift. Clears within checkInterval. If persists > 5 min → REAL DRIFT.
      EXPECTED_PENDING_MIRROR → Not drift, but tracked. Metric: loopzero_git_mirror_status. Timeout: 1h.
      MIRROR_STALE       → Soft drift. Alert LoopZeroMirrorSyncStale. Requires operator.
      REAL DRIFT         → Hard drift. Alert LoopZeroDriftDetected. ArgoCD auto-sync or manual.

5. OUTPUT
   If no real drift:
     Set clusterState = Synced
     Clear driftDetectedAt
     Metric: loopzero_drift_detected{tenant, env} = 0

   If real drift detected:
     If first detection:
       Set driftDetectedAt = now
       Set clusterState = Drifted
       Metric: loopzero_drift_detected{tenant, env} = 1
       Log: structured drift details

     If drift persists beyond driftThreshold:
       Emit Kubernetes Event on Tenant CR
       Metric: loopzero_drift_duration_seconds{tenant, env} = duration
       Alert: LoopZeroDriftDetected fires (Prometheus alerting)

     If ArgoCD auto-sync repairs the drift:
       Set clusterState = Synced
       Set driftResolution = AutoRepaired
       Clear driftDetectedAt
       Log: "Drift auto-repaired by ArgoCD"

6. BACKOFF
   If checking produces errors (API server unavailable, timeout):
     Exponential backoff: 60s → 120s → 240s → max 600s
     Metric: loopzero_reconciler_errors_total{tenant, env}
     After 5 consecutive errors: alert TheReconcilerUnhealthy

7. COST CONTROL
   Max concurrent checks: 10 (to avoid overloading API servers)
   Check interval minimum: 30s (cannot be set lower)
   Per-check timeout: 15s (if cluster query takes longer, count as error)
```

### 13.3 What StateReconciler does NOT do

- **Auto-remediate:** It reports drift. ArgoCD's auto-sync handles remediation. If auto-sync can't fix it, a human must intervene.
- **Compare application-level state:** It compares Kubernetes resources, not database contents or application behavior.
- **Monitor resources not managed by LoopZero:** It only checks resources with `loopzero.io/tenant` labels.
- **Guarantee real-time detection:** It checks every `checkInterval` (default 60s). Sub-second drift detection is not a goal.
- **Manage the authority registry:** The registry is declarative (in ReconciliationReport spec). The reconciler reads it, never writes it. Platform operators maintain it.
- **Classify unknown controllers as EXPECTED:** Unknown field mutations are conservatively classified as REAL DRIFT. Only explicitly registered controllers are trusted.

---

## 14. Multi-Tenancy & Isolation

### 14.1 Isolation across five dimensions

| Dimension | Mechanism | Enforcement |
|-----------|-----------|-------------|
| **Compute (workload)** | Per-namespace ResourceQuota + LimitRange | TenantController |
| **Compute (build)** | Per-tenant namespace in build cluster with ResourceQuota | TenantController + API build submission check |
| **Network (workload)** | NetworkPolicy: deny-all ingress+egress + explicit allow-list | TenantController |
| **Network (build)** | NetworkPolicy: egress only to Gitea, MinIO, registry | TenantController |
| **Storage** | Per-tenant MinIO bucket prefix + bucket policies | TenantController + MinIO policy |
| **Scheduling** | PriorityClass per plan tier | TenantController |

### 14.2 Noisy neighbor protection

**Build cluster:**

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: build-quota-acme
  namespace: build-acme
spec:
  hard:
    requests.cpu: "4"
    requests.memory: "8Gi"
    limits.cpu: "8"
    limits.memory: "16Gi"
    count/workflows.argoproj.io: "2"  # max 2 concurrent builds
```

```yaml
# Tenant spec includes queue depth limit
apiVersion: loopzero.io/v1
kind: Tenant
metadata:
  name: acme
spec:
  plan: enterprise
  limits:
    maxBuildConcurrency: 2
    maxBuildQueueDepth: 10    # max pending builds per tenant
```

**Workload cluster:**

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: workload-quota
  namespace: acme-my-saas-production
spec:
  hard:
    requests.cpu: "4"
    requests.memory: "8Gi"
    limits.cpu: "8"
    limits.memory: "16Gi"
    persistentvolumeclaims: "10"
    services.loadbalancers: "0"
```

### 14.3 PriorityClass: scheduling priority, not fairness

PriorityClass controls **scheduling priority** (which pods get placed first when resources are scarce) and **preemption** (which pods get evicted).

```yaml
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: loopzero-platform
value: 10000
preemptionPolicy: PreemptLowerPriority
description: "Platform components — preempt tenant workloads if necessary"

---
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: loopzero-tenant-enterprise
value: 1000
preemptionPolicy: PreemptLowerPriority
description: "Enterprise tenants — can preempt starter and growth"

---
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: loopzero-tenant-growth
value: 500
preemptionPolicy: PreemptLowerPriority
description: "Growth tenants — can preempt starter"

---
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: loopzero-tenant-starter
value: 100
preemptionPolicy: Never
description: "Starter tenants — never preempt others"
```

**Preemption rules:**

- Platform (`10000`) can preempt any tenant workload. Platform stability > tenant availability.
- Enterprise (`1000`) can preempt Growth and Starter. This is the paid-tier advantage.
- Growth (`500`) can preempt Starter only.
- Starter (`100`) cannot preempt anyone. Starter workloads are best-effort when cluster is full.

**Fairness (sustained consumption):**

PriorityClass solves scheduling priority, NOT sustained consumption fairness. Sustained fairness comes from:

1. **ResourceQuota** — hard caps per namespace. Tenant cannot exceed limits regardless of priority.
2. **LimitRange** — per-pod limits. Single pod cannot monopolize namespace quota.
3. **Build concurrency limit** — `maxBuildConcurrency` in Tenant spec. API enforces this before submitting workflows.
4. **Build queue depth limit** — `maxBuildQueueDepth` in Tenant spec. API rejects new builds when queue is full (see below).
5. **Monitoring** — `loopzero_tenant_quota_usage_ratio` alerts when tenant is consistently near limits.

**Build queue fairness (GAP-01 — Option B: Weighted fair queue):**

Build queue fairness is achieved through a combination of three mechanisms:

1. **Queue depth limit** (`maxBuildQueueDepth`): API rejects new builds with `429 BUILD_QUEUE_FULL` when the count of builds in `QUEUED` or `RUNNING` status for the tenant reaches this limit. This prevents any single tenant from saturating the global build queue.

2. **Weighted scheduling via PriorityClass**: Build workflows receive `priorityClassName` based on tenant plan. Enterprise builds are scheduled before Growth builds, which are scheduled before Starter builds. This is applied in the WorkflowSpec submitted by the API.

3. **ResourceQuota**: Hard cap on concurrent workflows per tenant namespace. This is the floor — even with high priority, a tenant cannot exceed its quota.

```sql
-- Enforcement in API (before submitting to Argo):
SELECT COUNT(*) FROM builds
  WHERE tenant_id = $1
    AND status IN ('QUEUED', 'RUNNING');
-- If count >= maxBuildQueueDepth → return 429 BUILD_QUEUE_FULL
```

```yaml
# WorkflowSpec submitted by API includes tenant plan priority:
spec:
  priorityClassName: loopzero-tenant-enterprise   # derived from tenant.spec.plan
  podPriorityClassName: loopzero-tenant-enterprise
```

**SLA implication:** Starter plan build queue wait time is best-effort under high load. Enterprise and Growth builds are scheduled with higher priority. Starter builds are not starved (queue depth limits prevent monopolization) but may experience longer wait times during sustained Enterprise load.

**What happens when a Starter tenant is starved:**

If a Starter tenant's pods are pending due to higher-priority tenants consuming resources:

1. Alert fires: `LoopZeroTenantPodsStarved{tenant, plan=starter}`
2. Platform operator decides: scale cluster or suggest plan upgrade
3. The platform does NOT auto-resolve this — it's a capacity planning decision

### 14.4 Network isolation

**Workload namespace:**

```yaml
# Default deny all
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]

---
# Allow intra-namespace only
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-same-namespace
spec:
  podSelector: {}
  ingress:
    - from:
        - podSelector: {}
  egress:
    - to:
        - podSelector: {}

---
# Allow Traefik ingress to exposed services
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-traefik-ingress
spec:
  podSelector:
    matchLabels:
      loopzero.io/expose: "true"
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              app.kubernetes.io/name: traefik

---
# Allow DNS resolution
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-dns
spec:
  podSelector: {}
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53

---
# Block metadata endpoint (cloud provider instance metadata)
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: deny-metadata
spec:
  podSelector: {}
  egress:
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0
            except:
              - 169.254.169.254/32  # AWS/GCP metadata
              - 100.100.100.200/32  # Alibaba metadata
```

**Build namespace (egress restricted):**

```yaml
# Default deny all
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny
  namespace: build-acme
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]

---
# Allow egress to Gitea, MinIO, registry only (by pod selector, not IP)
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-build-egress
  namespace: build-acme
spec:
  podSelector: {}
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: loopzero-system
          podSelector:
            matchLabels:
              app.kubernetes.io/name: gitea
      ports:
        - port: 3000
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: loopzero-system
          podSelector:
            matchLabels:
              app.kubernetes.io/name: minio
      ports:
        - port: 9000
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: loopzero-system
          podSelector:
            matchLabels:
              app.kubernetes.io/name: registry
      ports:
        - port: 5000
    - to:  # DNS
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
      ports:
        - protocol: UDP
          port: 53
```

**Build egress NetworkPolicy design (same-cluster):**

The build namespace egress rules use `namespaceSelector` + `podSelector` to target service pods directly, not `ipBlock` with ClusterIPs. This avoids a dangerous failure mode: ClusterIPs change on service recreation (upgrades, node failures, maintenance), which would silently break all tenant builds until the TenantController reconciles.

With pod/namespace selectors:
- NetworkPolicy resolves targets dynamically via the Kubernetes API — no hardcoded IPs
- Service recreation, pod rescheduling, and IP changes are transparent
- TenantController renders the NetworkPolicy once; no drift detection needed for IP changes

**Alternative for cross-cluster builds:** When the build cluster is separate from the management cluster (production topology), pod selectors cannot cross cluster boundaries. In this case:
- **ipBlock with LoadBalancer/NodePort IPs:** TenantController reads from Service `.status.loadBalancer.ingress` and renders `ipBlock` rules
- TenantController watches the target Services and re-renders NetworkPolicy immediately on IP change (not on periodic reconcile)
- Alert `LoopZeroBuildEgressStale` fires if build pods fail DNS/connection to Gitea/MinIO/registry
- The exact mechanism is configured via `ClusterCredential` CRD (section 7.3.3)

### 14.5 Capacity planning thresholds

| Metric | Threshold | Action |
|--------|-----------|--------|
| Build queue depth > 10 | Warning | Consider scaling build cluster |
| Build cluster CPU > 80% sustained 15min | Warning | Scale build nodes |
| Workload cluster CPU > 70% sustained 15min | Warning | Scale workload nodes |
| Tenant hitting ResourceQuota > 5x/hour | Warning | Suggest plan upgrade |
| Total tenants > 100 per workload cluster | Warning | Add workload cluster |
| Starter tenant pods pending > 5 min | Warning | Capacity decision needed |
| Platform namespace CPU > 80% | Critical | Scale mgmt cluster |
| ArgoCD Applications > 200 | Warning | Monitor reconciliation latency — approaching degradation zone |
| ArgoCD Applications > 400 | Critical | Increase controller resources or enable sharding before hitting 500 limit |
| ArgoCD reconciliation latency p99 > 30s | Warning | Controller overloaded — scale or shard |
| Gitea request latency p99 > 2s | Warning | Gitea approaching capacity — see AI-07 analysis |
| Gitea concurrent git operations > 80% of capacity | Warning | Scale Gitea or offload to external Git |

---

## 15. Observability

### 15.1 Three pillars: metrics, traces, logs — correlated

```
Every component exports:
  - Metrics: Prometheus (scrape every 15s)
  - Traces: OTLP to OTel Collector
  - Logs: structured JSON to stdout → OTel Collector → Loki (optional)

Correlation: every operation carries correlationId across all three pillars
```

### 15.2 Key metrics

```
# Build metrics
loopzero_build_duration_seconds{tenant, project, env, status}        histogram
loopzero_build_step_duration_seconds{tenant, step, status}           histogram
loopzero_builds_total{tenant, env, status}                           counter
loopzero_builds_active{tenant}                                       gauge
loopzero_build_queue_depth{}                                         gauge
loopzero_build_cache_hit_ratio{tenant, cache_type}                   gauge

# Deploy metrics
loopzero_deploy_duration_seconds{tenant, env}                        histogram
loopzero_deploy_total{tenant, env, status}                           counter
loopzero_argocd_sync_duration_seconds{tenant, env}                   histogram
loopzero_argocd_notification_latency_seconds{tenant, env}            histogram  # time from env_target update to ArgoCD sync start
loopzero_rollback_duration_seconds{tenant, env}                      histogram  # rollback is now fast (no git revert)
loopzero_git_mirror_status{tenant, env, status}                      gauge      # PENDING/SYNCED/FAILED per environment

# Drift metrics
loopzero_drift_detected{tenant, env}                                 gauge (0 or 1)
loopzero_drift_duration_seconds{tenant, env}                         histogram
loopzero_reconciliation_last_check_timestamp{tenant, env}            gauge
loopzero_reconciler_errors_total{tenant, env}                        counter

# Serialization metrics
loopzero_lock_wait_duration_seconds{tenant, env}                     histogram
loopzero_lock_conflicts_total{tenant, env}                           counter
loopzero_idempotency_dedup_total{tenant, endpoint}                   counter

# Tenant metrics
loopzero_tenant_quota_usage_ratio{tenant, resource}                  gauge
loopzero_tenant_active_previews{tenant}                              gauge
loopzero_tenant_pods_pending_duration_seconds{tenant, plan}          histogram

# Platform health
loopzero_api_request_duration_seconds{method, path, status}          histogram
loopzero_api_requests_total{method, path, status}                    counter
loopzero_operator_reconcile_duration_seconds{controller}             histogram
loopzero_operator_reconcile_errors_total{controller}                 counter
loopzero_postgres_connections_active{}                               gauge
loopzero_postgres_query_duration_seconds{query_type}                 histogram
loopzero_argocd_app_count{app_type}                                  gauge      # total ArgoCD Applications (app_type: production|staging|preview)
loopzero_argocd_circuit_state{}                                      gauge      # 0=closed, 1=open. Circuit breaker for ArgoCD API health.
loopzero_argocd_reconcile_duration_seconds{app_type}                 histogram  # app_type: production|staging|preview
loopzero_gitea_request_duration_seconds{operation}                   histogram  # operation: clone|push|api
loopzero_gitea_concurrent_operations{}                               gauge
loopzero_secret_sync_hash_mismatch_total{tenant, env}                counter    # AppSecretOperator divergence events
loopzero_git_desired_state_divergence{tenant, env}                   gauge      # 1 if Git HEAD Release-ID != environment_targets.desired_release_id. Emitted by periodic API verification loop (60s). NOTE: value 1 is expected during gitops-push pending/retry — alerts filter on mirror status to avoid false positives.
```

**Cardinality control:**

- `tenant` label: bounded by number of tenants (target: < 500)
- `project` label: bounded by tenants × projects (target: < 2000)
- `env` label: bounded by {dev, staging, production, preview-*}. Preview labels are aggregated as `preview` to avoid cardinality explosion.
- `step` label: bounded by workflow template steps (< 15)
- `path` label: normalized (e.g., `/api/v1/builds/:id` not `/api/v1/builds/build-abc123`)

**Retention:**

- Metrics: 15 days raw, 90 days downsampled (1-minute resolution)
- Traces: 7 days
- Logs: 30 days

### 15.3 Correlation ID

```
CLI request (generates UUID) →
  API (X-Correlation-ID header) →
    PostgreSQL (build.correlation_id column) →
    Argo Workflow (label: loopzero.io/correlation-id) →
    Git commit (trailer: Correlation-ID: xxx) →
    ArgoCD Application (annotation: loopzero.io/correlation-id) →
    OTel trace (attribute: loopzero.correlation_id)
```

### 15.4 SLOs

| SLO | Target | Measurement | Window |
|-----|--------|-------------|--------|
| Build success rate | > 95% (excl. user code errors) | `loopzero_builds_total{status=succeeded} / loopzero_builds_total` | 7 days |
| Deploy latency (warm cache) | p99 < 3 min | `loopzero_deploy_duration_seconds` | 7 days |
| Rollback latency | p99 < 60s | `loopzero_rollback_duration_seconds` | 7 days |
| API availability | > 99.5% | `1 - (5xx responses / total responses)` | 30 days |
| Drift detection latency | < 5 min | `loopzero_drift_duration_seconds` | 30 days |
| Preview cleanup accuracy | 100% within 1 hour of TTL | E2E test verification | Per release |
| Lock contention rate | < 1% of requests | `loopzero_lock_conflicts_total / loopzero_api_requests_total` | 7 days |

### 15.5 Alerting rules

```yaml
# Critical
- alert: LoopZeroDriftDetected
  expr: loopzero_drift_detected == 1
  for: 5m
  severity: critical

- alert: LoopZeroBuildClusterUnreachable
  expr: up{job="build-cluster"} == 0
  for: 2m
  severity: critical

- alert: LoopZeroPostgresDown
  expr: pg_up{job="loopzero-postgres"} == 0
  for: 1m
  severity: critical

- alert: LoopZeroPostgresBackupStale
  expr: time() - pg_last_backup_timestamp > 90000  # 25 hours
  severity: critical

- alert: LoopZeroGiteaDown
  expr: up{job="loopzero-gitea"} == 0
  for: 2m
  severity: critical

- alert: LoopZeroGiteaBackupStale
  expr: time() - loopzero_gitea_last_backup_timestamp > 25200  # 7 hours
  severity: critical

# Warning
- alert: LoopZeroAPIHighLatency
  expr: histogram_quantile(0.99, loopzero_api_request_duration_seconds) > 5
  for: 5m
  severity: warning
  annotations:
    runbook: "Check downstream dependencies (Gitea, Argo, PostgreSQL). Scaling API pods only helps if API itself is the bottleneck."

- alert: LoopZeroTenantQuotaNearLimit
  expr: loopzero_tenant_quota_usage_ratio > 0.9
  for: 10m
  severity: warning

- alert: LoopZeroBuildQueueBacklog
  expr: loopzero_build_queue_depth > 10
  for: 5m
  severity: warning

- alert: LoopZeroReconcilerUnhealthy
  expr: increase(loopzero_reconciler_errors_total[5m]) > 5
  severity: warning

- alert: LoopZeroSecretSyncFailed
  expr: increase(loopzero_secret_sync_hash_mismatch_total[15m]) > 3
  severity: warning
  annotations:
    runbook: "AppSecretOperator failed to sync secret to workload namespace 3+ times in 15 min. Check operator logs, workload cluster connectivity, and namespace RBAC."

- alert: LoopZeroTenantPodsStarved
  expr: loopzero_tenant_pods_pending_duration_seconds{quantile="0.99"} > 300
  severity: warning

- alert: LoopZeroMirrorSyncStale
  expr: loopzero_git_mirror_status{status="FAILED"} == 1
  for: 1h
  severity: warning
  annotations:
    runbook: "Gitea mirror failed for > 1h. Check Gitea health. If Gitea is up, trigger manual mirror sync via API. If Gitea is down, resolve Gitea first."

- alert: LoopZeroGitDesiredStateDivergence
  expr: loopzero_git_desired_state_divergence == 1
        and on(tenant, env) loopzero_git_mirror_status{status="FAILED"} == 0
        and on(tenant, env) loopzero_git_mirror_status{status="PENDING"} == 0
  for: 5m
  severity: critical
  annotations:
    runbook: "Git SHA does not match environment_targets desired release AND no mirror sync is in progress or retrying. This indicates unauthorized Git push or data corruption. Check Git audit log and environment_targets table."

- alert: LoopZeroGitMirrorExhausted
  expr: loopzero_git_desired_state_divergence == 1
        and on(tenant, env) loopzero_git_mirror_status{status="FAILED"} == 1
  for: 3h
  severity: critical
  annotations:
    runbook: "Git mirror has been FAILED for > 3h and Git still diverges from desired state. Mirror retry likely exhausted. Check Gitea health, resolve, then trigger manual mirror sync."
```

### 15.6 Dashboards

| Dashboard | Purpose | Key panels |
|-----------|---------|-----------|
| Platform Overview | Overall health at a glance | Build success rate, deploy latency, active builds, drift count, API error rate |
| Per-Tenant Health | Drill-down per tenant | Quota usage, build history, release history, drift history, active previews |
| Build Pipeline | Build performance analysis | Step duration breakdown, cache hit ratio, queue depth over time |
| Drift Detection | StateReconciler visibility | Drift events timeline, auto-repair rate, resolution time |
| Failure Investigation | Incident response | Correlation ID search, end-to-end trace view, error timeline |

---

## 16. Security Model

### 16.1 Management plane security

The management cluster is the **crown jewel**. Compromise of the management cluster is full platform compromise. This section defines defenses in depth.

**Envelope encryption with KMS:**

```yaml
# EncryptionConfiguration for etcd
apiVersion: apiserver.config.k8s.io/v1
kind: EncryptionConfiguration
resources:
  - resources:
      - secrets
    providers:
      - kms:
          apiVersion: v2
          name: loopzero-kms
          endpoint: unix:///var/run/kms/kms.sock
          # KMS provider wraps data encryption keys (DEKs)
          # with a key encryption key (KEK) stored in external KMS
          # (AWS KMS / GCP Cloud KMS / HashiCorp Vault)
      - identity: {}  # fallback for reading old unencrypted secrets
```

**Key rotation:**

- KEK (Key Encryption Key) in external KMS: rotated every 90 days
- DEK (Data Encryption Key) per secret: rotated on KEK rotation
- Rotation process: `kubectl get secrets --all-namespaces -o json | kubectl replace -f -` (re-encrypts with new DEK)
- `LoopZeroKEKRotationOverdue` alert if rotation is older than 95 days

**Break-glass procedure:**

```
Break-glass access to management cluster is:
  1. Authenticated via MFA + emergency credentials (sealed in HashiCorp Vault)
  2. Time-limited (4-hour session max)
  3. Audited via Kubernetes audit log (all API server requests logged)
  4. Triggering a LoopZeroBreakGlassAccess alert immediately
  5. Requiring post-incident review within 24 hours

Break-glass credentials are NOT the same as normal operator credentials.
They are separately stored, separately rotated, and their use is always an incident.
```

**Audit trail immutability:**

- Kubernetes API audit log → shipped to external log aggregator (append-only)
- PostgreSQL audit table → triggers prevent UPDATE and DELETE (append-only)
- Both streams include: actor, timestamp, resource, action, correlation ID
- Retention: 1 year minimum (compliance requirement)

**Tenant segregation in etcd:**

Secrets are namespaced in etcd by `namespace/name`. Each tenant's secrets are in their own namespace (`loopzero-secrets-{tenantId}`). A compromise of one namespace's RBAC does not expose other namespaces' secrets.

### 16.2 Secret lifecycle

> **Navigation:** Section 10.1 (step 0a) describes when secret validation occurs in the deploy flow. Section 9.1.1 describes how secrets are declared in `loopzero.yaml`. This section describes the complete secret lifecycle independent of deploys.

**Relationship between secrets and deploys:**

Secrets are managed independently from deploys. The `loopzero secret set` command creates/updates a secret at any time — it does not require or trigger a deploy. The AppSecretOperator synchronizes the secret to the workload namespace immediately. When a deploy happens, the generated Deployment manifest references the secret via `envFrom: secretRef`. The Secret already exists in the namespace when the pod starts.

```
loopzero secret set                         ← user does this once
  → AppSecret CR created in mgmt cluster
    → AppSecretOperator syncs K8s Secret to workload namespace
      → Secret exists in namespace

loopzero deploy                             ← days/weeks later
  → Deployment generated with envFrom: secretRef
    → Pod starts and finds Secret already present
      → No ordering dependency between secret set and deploy
```

**Secret value changes trigger rolling updates:** When the AppSecretOperator detects a hash change in the secret value, it follows a serialized update path to avoid race conditions with the deploy flow:

1. **AppSecretOperator detects hash change** in the synced Secret.
2. **AppSecretOperator calls internal API:** `POST /internal/secret-hash-changed` with `{tenantId, projectId, env, app, newHash}`.
3. **API acquires environment lock** via `withEnvironmentLock(tenant, project, env)`.
4. **API performs gitops-push** that updates the `loopzero.io/secret-hash` annotation in the Deployment manifest stored in Git.
5. **API notifies ArgoCD** via sync API. ArgoCD applies the updated manifest, triggering a rolling update.
6. **API releases environment lock** (transaction commits).

**Idempotency:** The API checks the current Git HEAD manifest for the target app before committing. If `loopzero.io/secret-hash` in Git already equals `newHash`, the endpoint returns `200 OK` (no-op) without acquiring the lock or making a commit. This handles Kubernetes controller retry loops (level-triggered reconciliation) without producing duplicate commits. No `Correlation-ID` is needed — the hash value itself is the idempotency key.

This ensures the `loopzero.io/secret-hash` annotation is always present in Git manifests, preventing ArgoCD from removing it during sync. The annotation is first set during `generate-manifests` (step 13g of deploy flow) with the secret hash at build time, and subsequently updated via the serialized path above when secrets change between deploys.

**Why Option C (API-serialized gitops-push):** The AppSecretOperator is a Go controller that cannot acquire PostgreSQL advisory locks. If it pushed to Git directly, its writes would race with deploy/promote/rollback flows that also write to the same Git path. By routing through the API, all Git writes for a given `(tenant, project, environment)` are serialized through the same lock, eliminating the race window.

**Invariant:** `loopzero.io/secret-hash` is NEVER applied out-of-band to the cluster. It is always committed to Git first (via API-serialized gitops-push), and ArgoCD applies it during sync. This prevents ArgoCD sync from reverting the annotation.

```
Creation:
  1. Developer: loopzero secret set API_KEY=xxx --env production
  2. CLI sends to API over TLS
  3. API validates RBAC (tenant-admin or tenant-deploy)
  4. API creates AppSecret CR in management cluster
  5. Secret data stored in etcd (encrypted via KMS envelope encryption)
  6. AppSecretOperator syncs to workload cluster namespace
  7. Audit: logged to PostgreSQL (who, when, which secret name — never the value)

Rotation:
  1. Rotation tracking via AppSecret CR status.rotationDueAt
  2. 7 days before expiry: loopzero_secret_rotation_due metric + K8s Event
  3. Developer rotates: loopzero secret set API_KEY=new-value
  4. Operator detects hash change, syncs new value to workload
  5. Deployment restarts (annotation hash change triggers rolling update)
  6. Audit: logged

Deletion:
  1. Developer: loopzero secret delete API_KEY --env production
  2. API deletes AppSecret CR
  3. Operator deletes Secret from workload namespace
  4. Audit: logged
```

### 16.3 RBAC model

```
API roles (per tenant):
  tenant-admin:  deploy, promote, rollback, secrets, preview (all envs)
  tenant-deploy: deploy, preview (non-production only)
  tenant-viewer: status, logs, releases (read-only)
  platform-admin: all operations across all tenants

Kubernetes RBAC (per namespace):
  loopzero-argocd-deployer: create/update Deployment, Service, ConfigMap
    (used by ArgoCD ServiceAccount)
  loopzero-secret-syncer: create/update Secret
    (used by AppSecretOperator ServiceAccount)
```

---

## 17. Supply Chain Security

### 17.1 Threat model

A compromised build can:

- Exfiltrate source code (via network egress)
- Tamper with artifacts (image poisoning, manifest injection)
- Poison cache (affect future builds for the same or other tenants)
- Commit malicious manifests to GitOps repo
- Exploit the container registry

### 17.2 Ephemeral credentials per build

Every build workflow receives short-lived, scoped credentials. No long-lived tokens.

```
Credential generation (by API, before submitting workflow):

1. Gitea token:
   - Scope: read on source repo, write on tenant GitOps repo ONLY
   - TTL: 1 hour (requires Gitea >= 1.20 for native expiration support)
   - Created via Gitea API: POST /api/v1/users/{admin}/tokens
   - Injected as Kubernetes Secret in build namespace
   - Primary cleanup: Explicit revocation via DELETE /api/v1/users/{admin}/tokens/{token_name}
     in the `finalize` step of the WorkflowTemplate (runs on success AND failure)
   - Secondary cleanup: Native TTL expiration (1 hour) as fallback if finalize step doesn't run
   - Tertiary cleanup: Orphan token sweeper (see section 11.2)
   - Minimum Gitea version: 1.20 (sha256 token format with native expiration)

2. MinIO credentials:
   - Scope: read/write on tenant prefix ONLY (loopzero-builds/{tenant}/, loopzero-assets/{tenant}/)
   - TTL: 1 hour
   - Created via MinIO STS (Security Token Service)
   - Injected as Kubernetes Secret in build namespace

3. Registry credentials:
   - Scope: push to tenant repository ONLY ({registry}/{tenant}/*)
   - TTL: 1 hour
   - Created via registry token API
   - Injected as Kubernetes Secret in build namespace

No credential is shared between tenants.
No credential outlives the build by more than minutes.
All credentials are deleted from build namespace on workflow completion.
```

### 17.3 Artifact signing and verification

```
Build time (in build cluster):
  1. Kaniko builds container image
  2. Image is pushed to registry
  3. cosign signs the image digest with a key scoped to the tenant
     cosign sign --key k8s://loopzero-system/cosign-{tenant} {image}@{digest}
  4. SBOM (Software Bill of Materials) generated via syft
     syft {image}@{digest} -o spdx-json > sbom.json
  5. SBOM attached to image via cosign
     cosign attach sbom --sbom sbom.json {image}@{digest}
  6. Build provenance (SLSA) attestation generated
     cosign attest --predicate provenance.json --key k8s://... {image}@{digest}

Deploy time (promotion verification):
  1. When promoting staging → production, API verifies:
     a. Image signature is valid (cosign verify)
     b. SBOM exists
     c. Provenance attestation exists and matches expected builder
  2. If verification fails: promotion is REJECTED
  3. Verification is logged to audit trail

Runtime (admission control, future enhancement):
  - Workload cluster admission webhook (Kyverno/Gatekeeper) verifies
    cosign signatures before allowing image pull
  - Only images signed by LoopZero's build pipeline are allowed
```

### 17.4 Cache isolation

```
Per-tenant cache isolation:

1. MinIO bucket policy: tenant A's credentials cannot access tenant B's prefix
2. Cache keys include tenant ID: collision between tenants is impossible
3. Cache integrity: each cache entry stores SHA256 of content. On restore,
   hash is verified. Mismatch → cache miss (not error)
4. Cache TTL: 7 days since last access. Stale cache is automatically pruned
5. No shared/global cache between tenants
```

### 17.5 Git commit integrity

**Commit message format (mandatory for all gitops-push commits):**

```
deploy: {tenant}/{project}/{env} release {release-id}

Apps: {app1}@{digest-short}, {app2}@{digest-short}

Release-ID: {release-id}
Correlation-ID: {correlation-id}
Build-ID: {build-id}
```

The last three lines are [Git trailers](https://git-scm.com/docs/git-interpret-trailers) — structured key-value pairs extractable via `git log --format='%(trailers:key=Release-ID)'`. The PITR reconciler (section 7.2 step 1b) uses `Release-ID` to identify which release a Git commit belongs to without querying PostgreSQL.

```
Every gitops-push:
  1. Commit is made via Gitea API (not SSH)
  2. Commit message follows the format above (trailers are mandatory)
  3. Commit is made by a dedicated bot user (loopzero-build-bot)
  4. Bot user's credentials are ephemeral (per-build, 1h TTL)
  5. Gitea repo has branch protection: only loopzero-build-bot can push to main
  6. Other users cannot push directly to the GitOps repo

Git commit deduplication:
  Before committing, the build step searches the last 10 commits for one
  with the same Correlation-ID trailer. If found → no-op (idempotent)
```

---

## 18. TDD/BDD Strategy

### 18.1 Test hierarchy

```
┌─────────────────────────────────────────────────────────────┐
│  E2E Tests (Ginkgo + real clusters)                         │
│  "Given a tenant, when deploy, then live"                   │
│  "When API crashes mid-deploy, build completes"             │
├─────────────────────────────────────────────────────────────┤
│  Chaos Tests (Ginkgo + real clusters + fault injection)     │
│  "When build cluster loses network, deploy fails cleanly"   │
├─────────────────────────────────────────────────────────────┤
│  Integration Tests (Ginkgo + envtest)                       │
│  "Given Tenant CR, when reconcile, then resources exist"    │
├─────────────────────────────────────────────────────────────┤
│  Component Tests (Jest + Supertest)                         │
│  "POST /builds returns 202 with build ID"                   │
│  "Concurrent deploy returns 409"                            │
├─────────────────────────────────────────────────────────────┤
│  Unit Tests (Go testing / Jest)                             │
│  "Release ID generator produces right format"               │
│  "State machine rejects invalid transition"                 │
└─────────────────────────────────────────────────────────────┘
```

### 18.2 State machine tests

Every state machine (section 5) has dedicated tests:

```go
func TestBuildStateMachine(t *testing.T) {
    tests := []struct {
        name     string
        from     BuildState
        to       BuildState
        valid    bool
    }{
        {"queued to running", QUEUED, RUNNING, true},
        {"queued to cancelled", QUEUED, CANCELLED, true},
        {"running to succeeded", RUNNING, SUCCEEDED, true},
        {"running to failed", RUNNING, FAILED, true},
        {"running to cancelling", RUNNING, CANCELLING, true},
        {"succeeded to anything", SUCCEEDED, RUNNING, false},    // terminal
        {"failed to anything", FAILED, RUNNING, false},          // terminal
        {"cancelled to anything", CANCELLED, RUNNING, false},    // terminal
        {"running to queued", RUNNING, QUEUED, false},           // invalid
        {"cancelling to running", CANCELLING, RUNNING, false},   // invalid
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateTransition(tt.from, tt.to)
            if tt.valid {
                assert.NoError(t, err)
            } else {
                assert.Error(t, err)
            }
        })
    }
}
```

### 18.3 Serialization tests

```typescript
describe("Environment serialization", () => {

    it("rejects concurrent deploys to same environment", async () => {
        const key1 = randomUUID()
        const key2 = randomUUID()

        // First deploy starts
        const p1 = request(app)
            .post("/api/v1/builds")
            .set("Idempotency-Key", key1)
            .send(deployRequest("acme", "my-saas", "production"))

        // Second deploy immediately after
        const p2 = request(app)
            .post("/api/v1/builds")
            .set("Idempotency-Key", key2)
            .send(deployRequest("acme", "my-saas", "production"))

        const [r1, r2] = await Promise.all([p1, p2])

        // One succeeds, one gets 409
        const statuses = [r1.status, r2.status].sort()
        expect(statuses).toEqual([202, 409])
    })

    it("deduplicates retried deploy with same idempotency key", async () => {
        const key = randomUUID()

        const r1 = await request(app)
            .post("/api/v1/builds")
            .set("Idempotency-Key", key)
            .send(deployRequest("acme", "my-saas", "dev"))

        const r2 = await request(app)
            .post("/api/v1/builds")
            .set("Idempotency-Key", key)
            .send(deployRequest("acme", "my-saas", "dev"))

        expect(r1.status).toBe(202)
        expect(r2.status).toBe(202)
        expect(r1.body.buildId).toBe(r2.body.buildId)  // same build returned
    })

    it("rejects idempotency key reuse with different payload", async () => {
        const key = randomUUID()

        await request(app)
            .post("/api/v1/builds")
            .set("Idempotency-Key", key)
            .send(deployRequest("acme", "my-saas", "dev"))

        const r2 = await request(app)
            .post("/api/v1/builds")
            .set("Idempotency-Key", key)
            .send(deployRequest("acme", "my-saas", "staging"))  // different env!

        expect(r2.status).toBe(422)
    })
})
```

### 18.4 E2E chaos tests

```go
var _ = Describe("Chaos: API crash recovery", Label("chaos"), func() {

    It("build completes when API dies mid-deploy", func() {
        resp, _ := loopzeroClient.Deploy(deployRequest)

        Eventually(buildIsRunning(resp.BuildID), 30*time.Second).Should(BeTrue())

        killLoopZeroAPIPod()

        Eventually(loopzeroAPIIsHealthy(), 60*time.Second).Should(BeTrue())

        Eventually(buildStatus(resp.BuildID), 5*time.Minute).Should(Equal("succeeded"))
    })
})

var _ = Describe("Chaos: concurrent operations", Label("chaos"), func() {

    It("concurrent promote and rollback on same env: one wins, one gets 409", func() {
        // Setup: active release in production
        // Launch promote and rollback concurrently
        // Verify: exactly one succeeds, one gets 409
        // Verify: final state is consistent
    })
})
```

---

## 19. Local Infrastructure (Inner Loop)

### 19.1 Topology

```
Developer machine
├── k3d cluster: loopzero-mgmt
│   ├── loopzero-system namespace (all platform components)
│   ├── argocd namespace
│   └── monitoring namespace
├── k3d cluster: loopzero-build
│   └── argo namespace + build-{tenant} namespaces
└── k3d cluster: loopzero-workload
    └── tenant namespaces
```

### 19.2 Taskfile

```yaml
version: '3'

tasks:
  setup:
    desc: "Full local environment bootstrap"
    cmds:
      - task: clusters:create
      - task: infra:install
      - task: tilt:up

  clusters:create:
    cmds:
      - k3d cluster create loopzero-mgmt --config infra/k3d/mgmt-cluster.yaml
      - k3d cluster create loopzero-build --config infra/k3d/build-cluster.yaml
      - k3d cluster create loopzero-workload --config infra/k3d/workload-cluster.yaml
      - task: clusters:connect

  clusters:delete:
    cmds:
      - k3d cluster delete loopzero-mgmt
      - k3d cluster delete loopzero-build
      - k3d cluster delete loopzero-workload

  test:unit:
    cmds:
      - go test ./operators/... -short
      - pnpm --filter @loopzero/api test:unit
      - pnpm --filter @loopzero/cli test:unit

  test:integration:
    cmds:
      - go test ./operators/... -tags=integration -v

  test:e2e:
    cmds:
      - go test ./infra/tests/e2e/... -v -timeout 20m

  test:e2e:chaos:
    cmds:
      - go test ./infra/tests/e2e/... -v -run "Chaos" -timeout 15m

  test:all:
    cmds:
      - task: test:unit
      - task: test:integration
      - task: test:e2e
```

### 19.3 k3d Configuration

```yaml
# infra/k3d/mgmt-cluster.yaml
apiVersion: k3d.io/v1alpha5
kind: Simple
metadata:
  name: loopzero-mgmt
servers: 1
agents: 2
kubeAPI:
  hostPort: "6443"
ports:
  - port: 3000:30000   # Gitea
  - port: 3001:30001   # loopzero-api
  - port: 3002:30002   # Grafana
  - port: 9001:30003   # MinIO console
  - port: 9090:30004   # Prometheus
  - port: 30082:30082  # ArgoCD UI
options:
  k3s:
    extraArgs:
      - arg: --disable=traefik
        nodeFilters: ["server:*"]
registries:
  use:
    - loopzero-registry:5000

---
# infra/k3d/build-cluster.yaml
apiVersion: k3d.io/v1alpha5
kind: Simple
metadata:
  name: loopzero-build
servers: 1
agents: 2
kubeAPI:
  hostPort: "6445"
ports:
  - port: 30081:30081  # Argo Workflows UI
options:
  k3s:
    extraArgs:
      - arg: --disable=traefik
        nodeFilters: ["server:*"]
registries:
  use:
    - loopzero-registry:5000

---
# infra/k3d/workload-cluster.yaml
apiVersion: k3d.io/v1alpha5
kind: Simple
metadata:
  name: loopzero-workload
servers: 1
agents: 2
kubeAPI:
  hostPort: "6444"
options:
  k3s:
    extraArgs:
      - arg: --disable=traefik
        nodeFilters: ["server:*"]
```

---

## 20. Test Contracts by Layer

| Layer | Tests | Does not test | Tools |
|-------|-------|---------------|-------|
| **Unit** | Pure logic, state machine transitions, naming, validation, ID generation | Any I/O | Go testing, Jest |
| **Integration** | Operators against envtest, serialization locks | Real cluster | Ginkgo, envtest |
| **Component** | HTTP endpoints, idempotency, serialization, error codes | Argo, K8s real | Jest, Supertest |
| **E2E** | Complete flows from CLI to cluster | Performance, load | Ginkgo, real clusters |
| **Chaos** | Failure recovery, crash resilience, concurrent operations, drift detection | Happy path | Ginkgo, fault injection |

---

## 21. Quality Pipeline

```yaml
on: [pull_request]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - run: task test:unit

  lint:
    runs-on: ubuntu-latest
    steps:
      - run: golangci-lint run ./operators/...
      - run: pnpm --filter @loopzero/api lint
      - run: pnpm --filter @loopzero/cli lint

  validate-manifests:
    runs-on: ubuntu-latest
    steps:
      - run: task validate:manifests

  integration-tests:
    runs-on: ubuntu-latest
    steps:
      - run: task clusters:create && task infra:install && task test:integration

  e2e-tests:
    runs-on: ubuntu-latest
    if: github.base_ref == 'main'
    steps:
      - run: task setup && task test:e2e

  chaos-tests:
    runs-on: ubuntu-latest
    if: github.base_ref == 'main'
    needs: [e2e-tests]
    steps:
      - run: task setup && task test:e2e:chaos
```

---

## 22. Toolchain Decision Log

| Decision | Choice | Alternatives | Reason |
|----------|--------|-------------|--------|
| Operator language | Go | Rust, Python | kubebuilder ecosystem, industry standard |
| API/CLI language | TypeScript | Go, Python | Fullstack team, JS framework detection |
| Test framework (Go) | Ginkgo + Gomega | go testing, Testify | BDD, label filtering, suites |
| Test framework (TS) | Jest + Supertest | Vitest, Mocha | Maturity, TypeScript integration |
| Build engine | Argo Workflows | Tekton, GH Actions | Native DAG, K8s state, artifacts |
| GitOps store | Gitea | GitLab CE, Gogs | GitHub API compat, OCI registry, lightweight |
| Ingress | Traefik | ingress-nginx | IngressRoute CRD, native LE, dashboard |
| Artifact store | MinIO | S3 | Self-hosted, S3-compatible |
| Manifest validation | kubeconform | kubeval | Updated schemas, CRD support |
| Local clusters | k3d | minikube, kind | Fast, multi-cluster, local registry |
| Local dev loop | Tilt | Skaffold | Live update, UI, Tiltfile as code |
| Task runner | Taskfile | Makefile | YAML, variables, no tab-hell |
| Database | PostgreSQL + Drizzle | SQLite, Prisma | Production-grade, type-safe |
| Schema validation | Zod | Joi, yup | Type inference, runtime + compile |
| Metrics | Prometheus | Datadog | Self-hosted, K8s native |
| Tracing | OpenTelemetry | Jaeger direct | Vendor-neutral, standard |
| Image signing | cosign | Notary v2 | Sigstore ecosystem, keyless option |
| SBOM | syft | trivy | Comprehensive, multiple formats |
| KMS | External (AWS/GCP/Vault) | Kubernetes native encryption | Envelope encryption, key rotation, audit |
| Serialization | PostgreSQL advisory locks | Redis, etcd | Already have PostgreSQL, advisory locks are transactional |

---

## 23. Implementation Order

> **Structural principle:** Every Sprint delivers a product increment that a user (or founder) can demo. The CLI is built alongside the platform, not after it. Infrastructure exists to serve the CLI — if a Sprint doesn't end with a working command, it's a platform Sprint disguised as product work.

### Sprint 0 — Foundation + Minimal Observability (Week 1-2)

**Objective:** Local environment boots with `task setup`. Three clusters running. CRDs installed. Structured logging and basic Prometheus operational from day one. Unit tests pass.

**Product exit:** `task setup` succeeds. `task test:unit` passes. Grafana accessible at localhost with basic cluster health dashboard.

```
[ ] Taskfile: clusters:create, infra:install, test:unit
[ ] k3d configs: mgmt, build, workload
[ ] CRDs: Tenant, PreviewEnvironment, AppSecret, ClusterCredential, ReconciliationReport
[ ] PostgreSQL schema: builds, releases, audit_log, idempotency_keys, sse_events, environment_targets
[ ] State machine types + transition validation (Go + TypeScript)
[ ] Unit tests: namespace naming, release ID, error types, state machine transitions
[ ] CI: unit tests + lint
[ ] Prometheus + Grafana installed in mgmt cluster (basic cluster health)
[ ] Structured logging (JSON) configured for all platform components from start
[ ] OpenTelemetry Collector installed (receives traces, even if no dashboards yet)
```

### Sprint 1 — `loopzero login` + `loopzero init` + TenantController (Week 3-5)

**Objective:** The CLI exists. A user can authenticate and initialize a project. `loopzero init` detects a monorepo, generates `loopzero.yaml`, and triggers TenantController to provision infrastructure.

**Product exit:** `loopzero login` → `loopzero init` works end-to-end. User sees generated `loopzero.yaml`. Gitea repo, namespaces, and RBAC exist in cluster.

```
# CLI scaffold
[ ] CLI project structure (Commander/oclif)
[ ] loopzero login: GitHub OAuth flow (browser open + token store)
[ ] loopzero login --token <key>: CI/CD auth (no browser)
[ ] loopzero logout, loopzero whoami
[ ] loopzero init: monorepo detection (Turborepo, Nx, Lerna, pnpm/npm/yarn workspaces, flat)
[ ] loopzero init: framework detection (10 frameworks from section 26.5)
[ ] loopzero init: interactive wizard (app discovery → confirmation → loopzero.yaml generation)
[ ] loopzero init --yes: non-interactive mode
[ ] loopzero init: adds .env.local to .gitignore
[ ] Unit tests: monorepo detection, framework detection, loopzero.yaml generation

# API — auth + project creation
[ ] Fastify app with plugins
[ ] JWT authentication middleware (GitHub OAuth token validation)
[ ] POST /projects (triggered by loopzero init)
[ ] API health check endpoint

# TenantController
[ ] Ginkgo specs written BEFORE implementation
[ ] Reconcile: Gitea repo, namespaces, ResourceQuota, LimitRange, RBAC
[ ] Reconcile: NetworkPolicy (deny-all + allow-list + DNS + metadata block)
[ ] Reconcile: PriorityClass, build namespace in build cluster
[ ] Reconcile: ArgoCD Application
[ ] Crash recovery: controller restart mid-reconcile resumes
[ ] Integration tests: TenantController against envtest
```

### Sprint 2 — `loopzero deploy` happy path end-to-end (Week 6-9)

**Objective:** The core promise works. A user runs `loopzero deploy` and gets a live URL. This is the first demo-able product increment.

**Product exit:** `loopzero login` → `loopzero init` → `loopzero deploy` produces a running app with a real URL. The "5-minute promise" is testable.

```
# CLI — deploy command
[ ] loopzero deploy: packages code, calls API, streams build progress
[ ] loopzero deploy --env staging
[ ] loopzero deploy --app api (single service)
[ ] loopzero deploy --no-wait
[ ] CLI output: progress indicators (✓ Building api... [45s])
[ ] CLI output: final URLs on success
[ ] CLI exit codes: 0 (success), 2 (build failure), 3 (deploy failure), 5 (partial failure)
[ ] CI mode: CI=true disables spinners, colors, confirmations

# API — build orchestration + serialization
[ ] PostgreSQL advisory locks (EnvironmentLock with withEnvironmentLock wrapper)
[ ] SET LOCAL lock_timeout = '5s' in advisory lock transaction
[ ] pg_advisory_xact_lock (not session-scoped) with code comment
[ ] Connection pool: statement_timeout=30s, connection validation
[ ] Idempotency key middleware
[ ] POST /builds with serialization + idempotency
[ ] BuildOrchestrator + ReleaseStore
[ ] SSE event journal for build progress
[ ] GET /builds/:id/stream with Last-Event-ID replay
[ ] Partial unique index: idx_one_active_build_per_env
[ ] Step 5 validation inside advisory lock transaction (not pre-check)

# Build pipeline
[ ] WorkflowTemplate: loopzero-build-nodejs-v1
[ ] All build steps including cache restore/save
[ ] gitops-push with correlation ID dedup
[ ] Git commit format with trailers (Release-ID, Correlation-ID, Build-ID)

# ArgoCD sync
[ ] ArgoCD installation and configuration
[ ] ClusterCredentialsOperator
[ ] API sync wait with timeout semantics (2 min)
[ ] Release state machine: READY → PENDING → ACTIVE or SYNC_FAILED
[ ] ArgoCD health gate: 3 consecutive failures → circuit OPEN → 503 PLATFORM_DEGRADED

# Tests
[ ] Component tests: serialization, idempotency, concurrent rejection
[ ] E2E: deploy boilerplate → app running at URL
[ ] E2E: second build uses cache
[ ] Test: two concurrent deploys → exactly one 202, one 409
[ ] Test: API pod killed mid-lock → next operation succeeds immediately
```

### Sprint 3 — `loopzero logs` + `loopzero status` + `loopzero secret` (Week 10-12)

**Objective:** All daily-use commands work. Lucas can deploy, check status, view logs, and manage secrets without leaving the terminal.

**Product exit:** All 5 workflows from section 0.1 are functional (except rollback and preview).

```
# CLI — daily commands
[ ] loopzero status: status of all services in environment
[ ] loopzero status --env staging, loopzero status --app api
[ ] loopzero inspect <deploy-id>: details of specific deploy
[ ] loopzero logs: logs for all services (default env)
[ ] loopzero logs --app api, loopzero logs --env staging
[ ] loopzero logs --follow: continuous streaming (SSE)
[ ] loopzero logs --since 2h
[ ] loopzero logs --build <build-id>: build logs
[ ] loopzero secret set KEY=value (default: production)
[ ] loopzero secret set KEY=value --env staging --app api
[ ] loopzero secret list (names only, never values)
[ ] loopzero secret delete KEY
[ ] loopzero secret pull (download to .env.local)
[ ] loopzero secret pull --env staging --app api
[ ] --json flag for all commands (machine-readable output per section 0.3)
[ ] Unit tests: JSON output schema stability

# API — secrets + logs
[ ] AppSecretOperator: Kubernetes Secret creation from API calls
[ ] POST /internal/secret-hash-changed (serialized via environment lock)
[ ] loopzero.io/secret-hash always in Git manifests (set during generate-manifests)
[ ] Idempotency: if secret-hash already matches, return 200 (no-op)
[ ] Log streaming endpoint (SSE from pod logs)

# Resilience
[ ] gitops-push retry policy (5 retries, exponential backoff)
[ ] Fetch-merge-push on non-fast-forward Git rejection (runtime-mutable field registry)
[ ] Gitea health check integration in API deep health
[ ] Gitea backup: git bundle to MinIO every 6 hours
[ ] LoopZeroGiteaDown + LoopZeroGiteaBackupStale alerts

# Tests
[ ] Component tests: secret CRUD, log streaming, JSON output contracts
[ ] Test: Gitea down during gitops-push → release GIT_MIRROR_FAILED, assets orphaned, sweeper cleans
[ ] Test: secret-hash-changed idempotency (same hash → no-op)
```

### Sprint 4 — `loopzero rollback` + `loopzero promote` + Cancel (Week 13-15)

**Objective:** Operational safety commands work. Lucas can recover from a bad deploy in seconds.

**Product exit:** `loopzero rollback` reverts in < 30s. `loopzero promote` moves staging to production. `loopzero build cancel` stops a running build.

```
# CLI — operational commands
[ ] loopzero rollback: revert to previous deploy (production)
[ ] loopzero rollback --env staging
[ ] loopzero rollback <deploy-id>: revert to specific deploy
[ ] loopzero rollback --list: list available deploys
[ ] loopzero rollback: inline warnings for irreversible side effects + [Y/n] confirmation
[ ] loopzero rollback --force: skip confirmation (CI use)
[ ] loopzero promote --from staging --to production
[ ] loopzero promote --from staging --to production --app api
[ ] loopzero build list, loopzero build cancel <build-id>, loopzero build watch <build-id>
[ ] loopzero env list, loopzero team list, loopzero team switch, loopzero project list/link/unlink

# API — rollback/promote via environment_targets
[ ] Rollback: atomic pointer update in environment_targets
[ ] Promote: pointer update + ArgoCD notification
[ ] Cancel with cancellation points per build stage
[ ] Compensation sweepers (orphaned assets, images, namespaces)
[ ] State machines: Promotion, Rollback (section 5.4, 5.5)
[ ] Side effect classification (reversible, irreversible-known, irreversible-unknown)
[ ] Emergency fallback path: if gitMirrorStatus=FAILED, disable auto-sync, apply directly
[ ] loopzero.io/fallback-active annotation, loopzero-fallback field manager
[ ] Auto-sync restored when gitMirrorStatus → SYNCED

# StateReconciler
[ ] StateReconciler operator (full algorithm from section 13)
[ ] ReconciliationReport CRD
[ ] Authority-based drift classification
[ ] EXPECTED_PENDING_MIRROR classification for fallback deploys

# Tests
[ ] E2E: promote staging→production, rollback, cancel mid-build
[ ] E2E: drift detection + ArgoCD auto-repair
[ ] E2E: full deploy → rollback → verify previous version running
[ ] Chaos: API crash mid-deploy recovery
[ ] Chaos: concurrent promote and rollback → one wins, one gets 409
[ ] Test: fallback path activates when gitMirrorStatus=FAILED
```

### Sprint 5 — Preview Environments + GitHub Webhook (Week 16-18)

**Objective:** Preview environments work automatically on PR open. This completes the "Vercel parity" for the core workflow.

**Product exit:** Opening a GitHub PR creates a preview environment. URL appears as PR comment. Merging/closing the PR destroys the preview.

```
# Webhook + ingestion
[ ] GitHub webhook receiver: PR opened → trigger preview build
[ ] GitHub webhook receiver: push to main → trigger production deploy
[ ] Code ingestion Mode A: GitHub mirror via webhook
[ ] Code ingestion Mode B: CLI push via git bundle (loopzero deploy)
[ ] PR comment bot: posts preview URLs on PR

# CLI — preview commands
[ ] loopzero preview list: active preview environments
[ ] loopzero preview delete <preview-id>
[ ] loopzero preview open <preview-id>: open in browser
[ ] loopzero deploy --preview: manual preview creation
[ ] loopzero deploy --preview --branch feat/payments

# Preview lifecycle
[ ] PreviewEnvironment CRD + controller + state machine
[ ] TTL: deadline + reaper + garbage collector
[ ] Cleanup order with failure tolerance
[ ] Concurrent preview delete + build handling
[ ] Per-project wildcard TLS cert (*.{project}.{tenant}.loopzero.example) via cert-manager DNS-01

# Multi-tenancy enforcement
[ ] Build queue depth enforcement (maxBuildQueueDepth in Tenant spec)
[ ] PriorityClassName injection in WorkflowSpec based on tenant plan
[ ] Ephemeral credential generation per build (Gitea, MinIO, registry)
[ ] Per-tenant cache isolation in MinIO
[ ] Build egress NetworkPolicy (namespaceSelector + podSelector, no hardcoded IPs)

# Tests
[ ] E2E: preview lifecycle end-to-end (PR open → URL → PR close → cleanup)
[ ] E2E: TTL expiry triggers cleanup
[ ] E2E: controller restart during TTL → preview still cleaned up
[ ] Test: tenant at maxBuildQueueDepth → new build returns 429 BUILD_QUEUE_FULL
[ ] Chaos: 3 Enterprise tenants saturating queue → Starter build eventually completes
```

### Sprint 6 — Security + Supply Chain (Week 19-21)

**Objective:** The platform is secure enough for external users. Supply chain integrity verified. Secrets encrypted at rest.

**Product exit:** Every build produces a signed image + SBOM. Secrets are encrypted via KMS. Ephemeral credentials expire correctly.

```
# Supply chain
[ ] cosign image signing in build pipeline
[ ] syft SBOM generation
[ ] Provenance attestation
[ ] Image signature verification on promote
[ ] Git commit integrity (branch protection, bot user)

# Secrets security
[ ] AppSecretOperator with rotation tracking (workloadHash + hashMatch verification)
[ ] KMS envelope encryption for etcd
[ ] KEK rotation procedure
[ ] 3 consecutive hash verification failures → LoopZeroSecretSyncFailed alert

# Credential lifecycle
[ ] Ephemeral credential TTL verification
[ ] Gitea minimum version 1.20 documented as infrastructure requirement
[ ] WorkflowTemplate finalize step: explicit Gitea token revocation
[ ] Gitea orphan token sweeper (tokens without active workflow)

# API hardening
[ ] API rate limiting per tenant
[ ] RBAC: role-based access control (owner, member, deploy-only)
[ ] API key management (for CI/CD, separate from OAuth tokens)
[ ] Platform ResourceQuotas (loopzero-system namespace)
[ ] Gitea rate limiting per tenant
[ ] PostgreSQL connection pool limits

# Audit
[ ] Audit trail: PostgreSQL append-only + K8s audit log
[ ] Per-tenant cache isolation verification

# Tests
[ ] Test: build pod killed before finalize → sweeper revokes token within 1 hour
[ ] Test: build token not accessible to other tenants
[ ] Test: unsigned image rejected on promote
[ ] Test: rate limit triggers on burst traffic
```

### Sprint 7 — Observability Complete (Week 22-23)

**Objective:** Full observability stack operational. Every failure mode has a dashboard and alert. On-call can diagnose incidents without reading code.

**Product exit:** Simulated incident (Gitea down) resolved using only dashboards + runbooks in < 15 minutes.

```
# Metrics
[ ] All metrics from section 15.2 emitting correctly
[ ] loopzero_argocd_app_count with app_type label (production|staging|preview)
[ ] loopzero_argocd_circuit_state metric
[ ] loopzero_git_desired_state_divergence gauge (60s verification loop)
[ ] loopzero_gitea_concurrent_operations gauge
[ ] Correlation ID end-to-end (CLI → API → Argo Workflow → ArgoCD)

# Dashboards
[ ] Platform Overview dashboard
[ ] Per-Tenant dashboard
[ ] Build Pipeline dashboard
[ ] Drift Investigation dashboard
[ ] Failure Investigation dashboard
[ ] SLO tracking dashboard (5-min deploy target)

# Alerting
[ ] All alerting rules from section 15.5
[ ] LoopZeroPostgresDown, LoopZeroGiteaDown, LoopZeroGiteaBackupStale
[ ] LoopZeroMirrorSyncStale, LoopZeroGitMirrorExhausted (3h threshold)
[ ] LoopZeroFallbackStuck, LoopZeroSecretSyncFailed
[ ] LoopZeroBuildEgressStale
[ ] ArgoCD sync duration > 30s
[ ] ArgoCD app count thresholds: Warning at 200, Critical at 400
[ ] Alert filters: loopzero_git_desired_state_divergence excludes PENDING and FAILED mirrors

# Startup reconciler
[ ] API startup reconciler: 5-step ordered startup (section 7.2)
[ ] PITR detection as step 1 (Release-ID from Git trailers vs environment_targets)
[ ] Fallback reconciliation step

# Tests
[ ] Test: every alert fires correctly in simulated failure
[ ] Test: correlation ID traceable from CLI request to pod log
[ ] Incident simulation: Gitea down → alert fires → runbook followed → resolved
```

### Sprint 8 — Hardening + Chaos (Week 24-25)

**Objective:** The platform survives everything we can throw at it. Every failure mode from section 12.1 has a passing test.

**Product exit:** Load test with 50 concurrent deploys passes. Every chaos scenario recovers automatically.

```
# Load testing
[ ] Load test: 50 concurrent deploys across 10 tenants
[ ] Load test: verify advisory locks under contention (no deadlocks)
[ ] Load test: verify ArgoCD under 200+ Applications
[ ] Load test: verify Gitea throughput under concurrent git operations

# Chaos testing
[ ] Chaos: every failure mode in section 12.1 has a passing test
[ ] Chaos: concurrent operations on same env (deploy + rollback + promote)
[ ] Chaos: compensation sweeper handles all orphan types
[ ] Chaos: API crash mid-deploy → build completes → release recoverable
[ ] Chaos: Gitea down during deploy → fallback path activates → mirror catches up
[ ] Chaos: ArgoCD degraded → circuit breaker opens → releases queue as READY
[ ] Chaos: PostgreSQL connection pool exhaustion → graceful degradation
[ ] Chaos: build node OOM during Kaniko → build fails cleanly, no orphans

# Error message compliance
[ ] Audit: all CLI error messages follow section 25.12 format
[ ] Audit: no internal terms (ArgoCD, advisory lock, gitops-push, CRD, tenant_id) in CLI output
[ ] Audit: all errors suggest next action
[ ] Audit: all CLI output uses product terminology (deploy, team, revert)
[ ] Audit: --json output matches schema defined in section 0.3

# Operational readiness
[ ] Break-glass procedure documented and tested
[ ] Runbooks per failure mode (PostgreSQL, Gitea, ArgoCD, MinIO, etcd)
[ ] On-call rotation and alert routing configured
```

### Sprint 9 — Polish + Launch (Week 26-27)

**Objective:** The product is ready for the first external user. Every item in the launch checklist passes.

**Product exit:** A non-team-member completes full onboarding (install → login → init → deploy → logs → secret pull → rollback) in < 10 minutes without help.

```
# External validation
[ ] Non-team-member completes onboarding unassisted in < 10 minutes
[ ] loopzero.yaml generation tested against at least 3 real-world monorepos
[ ] CLI tested on macOS, Linux, and Windows (WSL2)
[ ] npm package published and installable via npm install -g loopzero-cli

# Documentation
[ ] docs.loopzero.example: quickstart guide
[ ] docs.loopzero.example: loopzero.yaml reference (mirrors section 26)
[ ] docs.loopzero.example: CLI reference (mirrors section 25)
[ ] docs.loopzero.example: FAQ (common errors and solutions)

# Infrastructure
[ ] *.loopzero.example DNS operational
[ ] TLS certificates (cert-manager + DNS-01) operational
[ ] Production Kubernetes cluster provisioned (managed, HA etcd)
[ ] PostgreSQL WAL archiving to object storage (PITR within 7 days)
[ ] Weekly backup restore test automated

# Business
[ ] Billing functional (user can sign up and pay)
[ ] Data retention and deletion policy documented (LGPD/GDPR minimum)
[ ] Terms of service and privacy policy published

# Final gate
[ ] All E2E + chaos tests pass
[ ] Load test passes (50 concurrent deploys)
[ ] Every row in section 12.1 failure matrix has a passing test
[ ] Supply chain signing verified end-to-end
[ ] All items in "Ready for first paying user" checklist below pass
```

### Ready for first paying user — product exit criteria

This is the definitive pre-launch checklist. Every item must be verified before the first external user deploys on LoopZero. Items may be added but never removed without CTO approval.

```
[ ] Onboarding works end-to-end: npm install → loopzero login → loopzero init → loopzero deploy → live URLs
[ ] A non-team-member completed onboarding in < 10 minutes without help
[ ] loopzero.yaml generation produces correct output for at least 3 real-world monorepos
[ ] Error messages follow the format in section 25.12 — no internal terms leak to users
[ ] CLI outputs use product terminology (deploy, team, revert) — never internal (release, tenant, rollback)
[ ] --json output matches section 0.3 contract for all commands
[ ] Exit codes match section 0.3 contract (0-5)
[ ] CI mode (CI=true) works correctly (no spinners, no color, no interactive prompts)
[ ] Partial failure behavior matches section 0.3 (per-app status, exit code 5)
[ ] *.loopzero.example DNS and TLS are operational for production traffic
[ ] Usage-based billing or fixed plan billing is functional (user can sign up and pay)
[ ] docs.loopzero.example exists with: quickstart, loopzero.yaml reference, CLI reference, and FAQ
[ ] Monitoring alerts (section 15.5) are routed to an on-call channel
[ ] Runbooks exist for every P1 failure mode (PostgreSQL down, Gitea down, ArgoCD degraded)
[ ] Data retention and deletion policy is documented (LGPD/GDPR minimum)
```

### Note on Dashboard

The web dashboard is not in scope for Sprints 0-9. The priority is building a complete and mature CLI that covers all capabilities documented in Section 25 (CLI Reference).

The dashboard will be specified and implemented after:

```
[ ] All commands in Section 25 are implemented and tested
[ ] At least 10 active users using the product via CLI
[ ] Real usage feedback informs which dashboard flows have highest value
```

This decision is not about delaying the dashboard — it is about not building UI for a product whose interaction model is still being validated by real users.

### Sprint summary

| Sprint | Weeks | Product exit gate | CLI commands delivered |
|--------|-------|-------------------|-----------------------|
| 0 | 1-2 | `task setup` boots, Grafana running | — |
| 1 | 3-5 | `loopzero login` → `loopzero init` → `loopzero.yaml` generated | `login`, `logout`, `whoami`, `init` |
| 2 | 6-9 | `loopzero deploy` → live URL (demo-able) | `deploy` |
| 3 | 10-12 | All daily workflows functional | `logs`, `status`, `inspect`, `secret` |
| 4 | 13-15 | Recovery works in < 30s | `rollback`, `promote`, `build`, `env`, `team`, `project` |
| 5 | 16-18 | PR open → preview URL in comment | `preview` |
| 6 | 19-21 | Signed images, encrypted secrets, RBAC | — (security layer) |
| 7 | 22-23 | Incident resolved via dashboards | — (observability layer) |
| 8 | 24-25 | 50 concurrent deploys, all chaos passes | — (hardening) |
| 9 | 26-27 | Non-team-member onboards in < 10 min | — (polish + launch) |

---

## 24. User Experience & Onboarding

### 24.1 Product positioning

LoopZero is the Vercel for developers who have a backend. The calibration question for every feature and interface:

> **If a developer who uses Vercel saw this command/output, would they understand what to do without reading documentation?**

If the answer is "no" or "maybe", the interface needs revision. The strategy is **CLI-first** — the terminal is the primary interface. Dashboard comes after the CLI is mature and validated by real users.

### 24.2 User onboarding flow

**Primary flow — CLI-first (v1):**

```
# 1. Install
npm install -g loopzero-cli            # or: brew install loopzero

# 2. Authenticate
loopzero login                         # Opens browser for GitHub OAuth, stores token

# 3. Initialize
cd my-monorepo/
loopzero init                          # Detects structure, generates loopzero.yaml in ~30s

# 4. Deploy
loopzero deploy                        # Entire monorepo live in < 5 minutes

# Expected output:
# ✓ Building api...    [45s]
# ✓ Building web...    [38s]
# ✓ Deploying...       [12s]
#
# 🚀 Live:
#   api  → https://api-production.my-saas.acme.loopzero.example
#   web  → https://production.my-saas.acme.loopzero.example
#
# Preview URLs are automatic on every PR.
```

**What happens under the hood (invisible to user):**

- `loopzero login`: Stores OAuth token locally
- `loopzero init`: Creates Tenant CR, provisions Gitea repos, namespaces, RBAC (TenantController), generates loopzero.yaml
- `loopzero deploy`: Triggers the full deploy flow (section 10.1) — mirrors code to Gitea, builds, creates release, activates via ArgoCD
- After first deploy: Registers GitHub webhook for auto-deploy on push and preview on PR

**The user never sees:** Gitea, ArgoCD, Kubernetes namespaces, advisory locks, CRDs, Helm charts, or any internal component.

**Secondary flow — Dashboard (v2, after CLI is mature):**

```
1. User visits loopzero.example
2. Sign up with GitHub OAuth
3. Select repository
4. LoopZero detects structure and suggests loopzero.yaml
5. User clicks Deploy
6. Dashboard shows real-time build logs
7. URLs appear on completion
```

The dashboard is specified and implemented after the CLI is validated with real users (see section 23, "Note on Dashboard").

### 24.3 Interface parity: CLI and dashboard

The CLI and dashboard are **equivalent interfaces** to the same API. Everything possible in the CLI is possible in the dashboard, and vice-versa:

| Concern | CLI (primary, v1) | Dashboard (v2) |
|---------|-------------------|----------------|
| Onboarding | `loopzero login` → `loopzero init` → `loopzero deploy` | Sign up → connect → deploy |
| Deploy | `loopzero deploy` | Push to GitHub (automatic) |
| Preview | Open PR (automatic) / `loopzero deploy --preview` | Open PR (automatic) |
| Rollback | `loopzero rollback` | Click "Revert" button |
| Promote | `loopzero promote` | Click "Deploy to production" |
| Secrets | `loopzero secret set` | Settings panel |
| Local env setup | `loopzero secret pull` | Download button (settings) |
| Logs | `loopzero logs` | Real-time panel |
| Status | `loopzero status` | Overview page |

### 24.4 Terminology mapping (internal → user-facing)

The internal architecture uses precise technical terms. The user-facing interfaces (dashboard, CLI output, documentation, error messages) use product-friendly equivalents:

| Internal term | User-facing term | Where internal term is used |
|--------------|-----------------|---------------------------|
| `tenant` / `tenant_id` | **team** or **account** | CRDs, database, API internals |
| `environment_targets` | (not exposed) | Database table |
| `gitMirrorStatus` | (not exposed) | Release state machine |
| `advisory lock` | (not exposed) | Serialization mechanism |
| `ArgoCD Application` | (not exposed) | Cluster convergence |
| `Release` (state machine) | **deploy** | API responses, CLI output |
| `SUPERSEDED` | **replaced by newer deploy** | Dashboard history |
| `SYNC_FAILED` | **deploy failed** | Dashboard, CLI output |
| `GIT_MIRROR_FAILED` | (not exposed — system handles internally) | Internal state |
| `promote` | **deploy to {env}** | Dashboard button label |
| `Rollback` | **revert to previous deploy** | Dashboard button label |

---

## 25. CLI Reference

> Every command below is a **product contract**. Each must be implemented, tested, and compared against the Vercel CLI equivalent before release. If a LoopZero command requires more arguments or produces more confusing output than its Vercel equivalent, it must be revised.

### 25.1 Authentication

```
loopzero login                        # Opens browser for GitHub OAuth
loopzero login --token <api-key>      # For CI/CD, no browser
loopzero logout
loopzero whoami                       # Shows active user and team
```

Vercel equivalent: `vercel login`, `vercel logout`, `vercel whoami`

### 25.2 Project initialization

```
loopzero init                         # Interactive wizard, auto-detects monorepo
loopzero init --yes                   # Accept all defaults without prompts
loopzero init --force                 # Overwrite existing loopzero.yaml
```

`loopzero init` detects the repository structure, identifies apps by framework markers (see section 26), and generates a `loopzero.yaml`. The user reviews and confirms. Under the hood: creates Tenant CR, provisions Gitea repos, namespaces.

**Monorepo tool detection:** `loopzero init` recognizes the following monorepo structures:

| Tool | Detection marker |
|------|-----------------|
| Turborepo | `turbo.json` in root |
| Nx | `nx.json` in root |
| Lerna | `lerna.json` in root |
| pnpm workspaces | `pnpm-workspace.yaml` in root |
| npm workspaces | `workspaces` field in root `package.json` |
| yarn workspaces | `workspaces` field in root `package.json` + `yarn.lock` |
| No tool (flat) | Multiple directories with `package.json` / `go.mod` / `requirements.txt` |

After identifying the monorepo tool, `loopzero init` scans workspace directories for framework markers (section 26.5) to discover individual apps.

Vercel equivalent: `vercel` (on first use, detects project and configures)

### 25.3 Deploy

```
loopzero deploy                                    # Deploy to default environment (production)
loopzero deploy --env staging                      # Deploy to specific environment
loopzero deploy --env production --app api         # Deploy a single service
loopzero deploy --preview                          # Create preview environment manually
loopzero deploy --preview --branch feat/payments   # Preview for specific branch
loopzero deploy --no-wait                          # Submit and return (don't wait for completion)
```

Expected output:
```
✓ Building api...    [45s]
✓ Building web...    [38s]
✓ Deploying...       [12s]

🚀 Live:
  api  → https://api-production.my-saas.acme.loopzero.example
  web  → https://production.my-saas.acme.loopzero.example
```

Vercel equivalent: `vercel`, `vercel --prod`, `vercel deploy --prebuilt`

### 25.4 Status and inspection

```
loopzero status                        # Status of all services in default environment
loopzero status --env staging          # Status in specific environment
loopzero status --app api              # Status of a single service
loopzero inspect <deploy-id>           # Details of a specific deploy
```

Vercel equivalent: `vercel inspect`, `vercel ls`

### 25.5 Logs

```
loopzero logs                          # Logs for default environment (all services)
loopzero logs --app api                # Logs for a specific service
loopzero logs --env staging            # Logs for specific environment
loopzero logs --follow                 # Continuous streaming (tail -f)
loopzero logs --since 2h               # Logs from the last 2 hours
loopzero logs --build <build-id>       # Logs for a specific build
```

Vercel equivalent: `vercel logs`

### 25.6 Rollback

```
loopzero rollback                      # Revert to previous deploy (production)
loopzero rollback --env staging        # Revert specific environment
loopzero rollback <deploy-id>          # Revert to specific deploy
loopzero rollback --list               # List deploys available for rollback
```

Interactive confirmation with inline warnings (see section 9.1 v1 UX simplification). `--force` flag available for CI but never required interactively.

Vercel equivalent: `vercel rollback`

### 25.7 Promote (advanced workflow)

```
loopzero promote --from staging --to production                    # Promote all services
loopzero promote --from staging --to production --app api          # Promote specific service
```

No direct Vercel equivalent — this is a LoopZero differentiator for teams with staging gates.

### 25.8 Secrets / Environment variables

```
loopzero secret set KEY=value                                      # Create/update in production
loopzero secret set KEY=value --env staging                        # In specific environment
loopzero secret set KEY=value --env production --app api           # For specific service
loopzero secret list                                               # List names (never values)
loopzero secret list --env staging
loopzero secret delete KEY                                         # Remove secret
loopzero secret delete KEY --env staging
loopzero secret pull                                               # Download secrets to .env.local
loopzero secret pull --env staging                                 # From specific environment
loopzero secret pull --env staging --app api                       # For specific service
```

`loopzero secret pull` writes a `.env.local` file in the current directory (or in the app's `path` when `--app` is specified). The file is overwritten on each pull — it is never merged. `.env.local` must be in `.gitignore` (`loopzero init` adds it automatically).

**Security:** `loopzero secret pull` requires explicit authentication and is rate-limited. Values are transmitted over TLS and never cached by the CLI beyond the written file. The CLI prints the count of secrets written, never the values.

```
$ loopzero secret pull --env staging

✓ Pulled 4 secrets to .env.local (staging)
```

Vercel equivalent: `vercel env add`, `vercel env ls`, `vercel env rm`, `vercel env pull`

### 25.9 Environments and previews

```
loopzero env list                      # List project environments (production, staging, previews)
loopzero preview list                  # List active preview environments
loopzero preview delete <preview-id>   # Remove preview environment manually
loopzero preview open <preview-id>     # Open preview in browser
```

Vercel equivalent: `vercel ls` (lists deployments including previews)

### 25.10 Teams and projects

```
loopzero team list                     # List user's teams
loopzero team switch <team-slug>       # Switch active team
loopzero project list                  # List team's projects
loopzero project link                  # Link local directory to existing project
loopzero project unlink                # Remove link
```

Vercel equivalent: `vercel teams list`, `vercel teams switch`, `vercel project ls`, `vercel link`

### 25.11 Build management

```
loopzero build list                    # List recent builds
loopzero build cancel <build-id>       # Cancel build in progress
loopzero build watch <build-id>        # Watch build in progress (SSE stream)
```

No direct Vercel equivalent.

### 25.12 Error output examples

Every error the CLI shows must follow this format: **what happened**, **why it happened**, and **what to do next**. No stack traces. No internal IDs unless `--verbose` is passed.

```
$ loopzero deploy

✗ Missing required secrets for app "api" in environment "production":
  - DATABASE_URL
  - JWT_SECRET

Run: loopzero secret set DATABASE_URL=<value>
     loopzero secret set JWT_SECRET=<value>

Then retry: loopzero deploy
```

```
$ loopzero init

⚠ No apps detected in this repository.
  Looked for: package.json, go.mod, requirements.txt, manage.py in subdirectories.

If your project uses a non-standard structure, create loopzero.yaml manually:
  https://docs.loopzero.example/reference/loopzero-yaml
```

```
$ loopzero deploy

✗ Build failed for app "api" [build-id: b-3f8a]

Last 5 lines of build log:
  > npm run build
  > tsc --noEmit && vite build
  error TS2307: Cannot find module '@shared/types'

Full log: loopzero logs --build b-3f8a
```

```
$ loopzero rollback

✗ No previous deploy found for environment "production".
  This is the first deploy — there is nothing to roll back to.
```

```
$ loopzero deploy --env staging

✗ Deploy already in progress for "staging" [deploy-id: d-7c2e]
  Started 45s ago by lucas@acme.dev

Wait for it to finish, or cancel: loopzero build cancel d-7c2e
```

The error format rules:
- **Never** show internal terms (ArgoCD, advisory lock, gitops-push, CRD, tenant_id).
- **Always** suggest the next action the user can take.
- Use `✗` for errors, `⚠` for warnings, `✓` for success.
- Keep to 5 lines or fewer unless showing build log excerpts.

---

## 26. loopzero.yaml — Complete Specification

> **Design principle:** `loopzero.yaml` describes *what* you have, not *how* to deploy it. LoopZero infers the rest. A valid file for a typical monorepo has less than 20 lines. This is the only infrastructure file a developer ever writes.

### 26.1 Minimal example — NestJS + Vite monorepo (generated by `loopzero init`)

```yaml
project: my-saas

apps:
  api:
    path: apps/api
    framework: nestjs
    port: 3000
    environments:
      - production
      - staging

  web:
    path: apps/web
    framework: vite
    environments:
      - production
      - staging
```

### 26.2 Example with secrets declared

```yaml
project: my-saas

apps:
  api:
    path: apps/api
    framework: nestjs
    port: 3000
    secrets:
      - DATABASE_URL
      - JWT_SECRET
    environments:
      - production
      - staging

  web:
    path: apps/web
    framework: vite
    environments:
      - production
      - staging
```

### 26.3 Example with worker

```yaml
project: my-saas

apps:
  api:
    path: apps/api
    framework: nestjs
    port: 3000
    secrets:
      - DATABASE_URL

  worker:
    path: apps/worker
    framework: node
    type: worker              # no exposed port, no ingress
    secrets:
      - DATABASE_URL
      - QUEUE_URL

  web:
    path: apps/web
    framework: vite
```

### 26.4 Field reference

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `project` | Yes | — | Unique project identifier within the team |
| `apps.<name>` | Yes | — | Service name (becomes part of the URL) |
| `apps.<name>.path` | Yes | — | Relative path of the service in the monorepo |
| `apps.<name>.framework` | No | auto-detected | Framework (see 26.5 for supported list) |
| `apps.<name>.port` | No | auto-detected | Port the service listens on |
| `apps.<name>.type` | No | `service` | `service` (with URL) or `worker` (no URL) |
| `apps.<name>.secrets` | No | `[]` | List of secrets the service requires |
| `apps.<name>.environments` | No | `[production]` | Environments where the service is deployed |
| `apps.<name>.build` | No | auto-detected | Build command (overrides detection) |
| `apps.<name>.start` | No | auto-detected | Start command (overrides detection) |
| `apps.<name>.env` | No | `{}` | Non-sensitive environment variables. Supports `${env}`, `${project}`, `${team}` templates. |

**Note:** The `tenant` field does NOT exist in `loopzero.yaml`. The account (tenant) is inferred from the authenticated user's token at deploy time. This prevents configuration drift and removes infrastructure concerns from the repository.

### 26.5 Supported frameworks (v1)

| Framework | Language | Auto-detection | Default port |
|-----------|----------|---------------|-------------:|
| `nextjs` | TypeScript/JavaScript | `next.config.js` or dep `next` | 3000 |
| `vite` | TypeScript/JavaScript | `vite.config.*` or dep `vite` | 5173 |
| `remix` | TypeScript/JavaScript | `remix.config.js` | 3000 |
| `nestjs` | TypeScript | dep `@nestjs/core` | 3000 |
| `express` | TypeScript/JavaScript | dep `express` (no specific framework) | 3000 |
| `fastify` | TypeScript/JavaScript | dep `fastify` | 3000 |
| `fastapi` | Python | `main.py` with `FastAPI()` | 8000 |
| `django` | Python | `manage.py` | 8000 |
| `gin` | Go | `go.mod` with dep `github.com/gin-gonic/gin` | 8080 |
| `node` | JavaScript/TypeScript | generic Node.js fallback | 3000 |

Frameworks not in this list: `loopzero init` interactively asks for port and build command, and generates a `loopzero.yaml` with `framework: custom`.

### 26.6 Relationship to section 9.1.1

Section 9.1.1 documents the internal data model and template resolution mechanism (`${env}`, `${project}`, `${team}`). This section (26) is the **user-facing specification** — what the developer sees and writes. The internal representation in section 9.1.1 may contain additional fields used by the build pipeline that are not exposed to the user.

---

## Appendix A — Directory Structure

```
loopzero/
├── Taskfile.yaml
├── Tiltfile
├── packages/
│   ├── cli/
│   │   ├── src/
│   │   │   ├── commands/
│   │   │   ├── client/              # LoopZeroClient + SSE reconnection + idempotency
│   │   │   └── ui/
│   │   └── tests/
│   ├── api/
│   │   ├── src/
│   │   │   ├── builds/
│   │   │   ├── previews/
│   │   │   ├── releases/
│   │   │   ├── workflow/
│   │   │   ├── gitops/
│   │   │   ├── health/
│   │   │   ├── serialization/       # EnvironmentLock, IdempotencyMiddleware
│   │   │   ├── compensation/        # OrphanSweeper, GarbageCollector
│   │   │   ├── sse/                 # EventJournal, ReplayStream
│   │   │   └── plugins/
│   │   └── tests/
│   ├── core/
│   │   ├── src/
│   │   │   ├── state-machines/      # Build, Release, Preview, Promotion, Rollback
│   │   │   ├── side-effects/        # Classification, acknowledgement
│   │   │   ├── changeset/
│   │   │   ├── discovery/
│   │   │   ├── manifest/
│   │   │   ├── config/            # loopzero.yaml parser, template resolver, schema validation
│   │   │   └── release/
│   │   └── tests/
│   └── shared/
│       └── src/
│           ├── schemas/
│           └── errors/
├── operators/
│   ├── loopzero-tenant-controller/
│   ├── loopzero-state-reconciler/
│   │   ├── internal/
│   │   │   ├── controller/          # ReconciliationReportReconciler
│   │   │   ├── comparator/          # Git↔ArgoCD↔Cluster comparison
│   │   │   ├── normalizer/          # Field exclusion, HPA detection
│   │   │   └── classifier/          # Expected vs real drift
│   │   └── api/v1/
│   ├── loopzero-appsecret-operator/
│   ├── loopzero-clustercredentials-operator/
│   └── shared/
├── infra/
│   ├── k3d/
│   │   ├── mgmt-cluster.yaml
│   │   ├── build-cluster.yaml
│   │   └── workload-cluster.yaml
│   ├── helm/
│   │   └── loopzero/templates/
│   ├── monitoring/
│   │   ├── dashboards/
│   │   └── alerts/
│   ├── security/
│   │   ├── encryption-config.yaml
│   │   └── network-policies/
│   ├── tests/e2e/
│   └── workflow-images/
├── charts/
│   └── loopzero-app/templates/
│       ├── deployment.yaml
│       ├── service.yaml
│       ├── hpa.yaml
│       ├── networkpolicy.yaml
│       └── migration-job.yaml
└── docs/
    ├── architecture/decisions/
    ├── runbooks/
    └── guides/
```

---

## Appendix B — Review Response Matrix

| # | v2 Review Issue | Resolution in v3 | Section |
|---|----------------|-------------------|---------|
| 1 | Concurrency and serialization undefined | PostgreSQL advisory locks per (tenant, project, env), conflict matrix, retry semantics | 4 |
| 2 | No formal state machines | State machines for Build, Release, Preview, Promotion, Rollback with valid/invalid transitions and timeouts | 5 |
| 3 | PostgreSQL dismissed as "just cache" | PostgreSQL acknowledged as critical operational dependency. Loss = incident, not degradation. Mutations blocked when PG is down. | 3.3 |
| 4 | Side effects as warning, not model | Side effect classification (reversible, irreversible-known, irreversible-unknown, externalized) with per-classification rollback behavior | 9.1 |
| 5 | StateReconciler underspecified | Full algorithm: inputs, comparison, normalization, exclusions, drift classification (expected vs real), backoff, cost control | 13 |
| 6 | Supply chain not hardened | Ephemeral credentials per build, cosign signing, SBOM, provenance attestation, per-tenant cache isolation, Git commit integrity | 17 |
| 7 | Management plane security insufficient | KMS envelope encryption, KEK rotation, break-glass procedure, immutable audit trail, tenant segregation in etcd | 16.1 |
| 8 | No cancellation/compensation model | Cancellation points per build stage, compensation sweepers for each orphan type, explicit non-compensation list | 11 |
| 9 | SSE reconnect underspecified | Event journal in PostgreSQL, numeric IDs, journal retention, expiry handling, duplicate detection, at-least-once delivery | 7.1 |
| 10 | No ADR for responsibility boundaries | Responsibility matrix: what each component owns, delegates, and does NOT handle. Explicit platform non-goals. | 8 |
| 11 | Observability centered on alerting, not explaining | Correlation ID end-to-end, cardinality control, retention policy, per-flow SLOs, investigation dashboard, runbook links in alerts | 15 |
| 12 | "API waits for ArgoCD sync" without timeout semantics | Explicit timeout (2 min), PENDING → SYNC_FAILED on timeout, idempotent retry via key, no duplicate commits | 5.2, 10.1 |
| 13 | "No partial commit" undefined mechanism | Correlation ID dedup in Git (check last 10 commits), MinIO orphan sweeper, atomic gitops-push | 7.4, 7.5, 11.2 |
| 14 | PriorityClass != fairness | Explicit: PriorityClass = scheduling priority. Fairness = ResourceQuota + LimitRange + concurrency limits + monitoring. Preemption rules per plan. | 14.3 |
| 15 | NetworkPolicy incomplete (egress, DNS, metadata) | Full NetworkPolicy for workload (deny-all, intra-ns, Traefik, DNS, metadata block) and build (egress restricted to Gitea/MinIO/registry) | 14.4 |
| 16 | Control plane not self-protected | Platform ResourceQuotas, per-component resource limits, Gitea rate limiting, PostgreSQL connection pool, platform PriorityClass | 6.3 |
| 17 | State precedence undefined | Explicit precedence table per flow (deploy, promote, rollback) | 3.4 |

---

## Appendix C — Gap Resolution Matrix (v3.0 → v3.1)

| GAP | Issue | Severity | Resolution | Sections updated |
|-----|-------|----------|------------|-----------------|
| GAP-01 | Build queue fairness entre tenants | High | Option B: `maxBuildQueueDepth` + weighted PriorityClass in WorkflowSpec + ResourceQuota. API rejects with 429 when queue full. | 14.2, 14.3, 12.1, Sprint 4 |
| GAP-02 | Comportamento durante Gitea outage | High | Four scenarios specified (gitops-push, ArgoCD sync, StateReconciler, data loss). Retry policy, backup strategy (git bundle to MinIO, RPO 6h), LoopZeroGiteaDown alert. HA deferred to post-v1. | 7.5, 12.1, 15.5, Sprint 2 |
| GAP-03 | Race window entre duas aquisições de lock | Refinement | Partial unique index `idx_one_active_build_per_env`. Step 5 validation inside advisory lock transaction with `FOR UPDATE NOWAIT`. | 4.2, 10.1, Sprint 3 |
| GAP-04 | pg_advisory_lock sob perda de conexão | Refinement | Explicit: use `pg_advisory_xact_lock` only. Connection pool with `statement_timeout=30s` and dead connection detection. Crash recovery via automatic lock release. | 4.2, 7.8, Sprint 3 |
| GAP-05 | TTL de token efêmero do Gitea | Refinement | Triple cleanup: explicit revocation in finalize step + native TTL (Gitea >= 1.20) + orphan token sweeper. Minimum Gitea version documented. | 17.2, 11.2, Sprint 8 |

---

## Appendix D — Conceptual Corrections Matrix (v3.1 → v4.0)

| # | Correction | Problem | Resolution | Sections updated |
|---|-----------|---------|------------|-----------------|
| 1 | Code ingestion: user never sees Gitea | Document assumed user interacts with Gitea directly. No description of how source code enters the pipeline. | Two ingestion modes: Mode A (GitHub Mirror via webhook) and Mode B (CLI push via git bundle). Pre-receive hooks for validation. Gitea invisible to user. | 1, 7.1, 7.2, 7.5, 10.1 |
| 2 | URL scheme: tenant has multiple projects | URL pattern `{app}-{env}.{tenant}.loopzero.example` collapsed tenant and project, causing collisions. | New pattern: `{app}-{env}.{project}.{tenant}.loopzero.example`. `loopzero.yaml` schema with app types (server/worker/frontend) and template resolution (`${env}`, `${project}`, `${tenant}`). | 7.7, 8.2, 9.1.1, 10.1, Appendix A |
| 3 | Env vars and secrets flow undocumented | Secret model existed in section 16.2 but was disconnected from CLI, Data Model, and deploy flow. Implementors would not know how to wire secrets to pods. | Two types: non-sensitive vars (inline in manifest, in Git) and secrets (never in Git, `envFrom: secretRef`). Pre-deploy validation checks all declared secrets exist. Secret changes trigger rolling updates via annotation hash. | 7.1, 7.2, 9.1.1, 10.1, 16.2 |

---

## Appendix E — Architectural Decoupling Matrix (v4.0 → v5.0)

| # | Correction | Problem | Resolution | Sections updated |
|---|-----------|---------|------------|-----------------|
| 1 | Release exists when artifacts exist, not when Git accepts commit | Release was coupled to gitops-push success. Build that completed but failed gitops-push produced no Release, discarding all work. | New READY state: Release created after artifacts signed (step 13f+). gitops-push is consequence, not precondition. `GIT_MIRROR_FAILED` state with async retry. `ReleaseArtifacts` persisted in PostgreSQL. | 3.2, 5.2, 9.1, 10.1, 11.2, 12.1, 12.2 |
| 2 | Rollback/Promote via `environment_targets` pointer, not git revert/commit | Rollback depended on Gitea + git revert + ArgoCD polling — 5 serial dependencies for an emergency operation. Promote depended on Git commit as activation mechanism. | `environment_targets` table in PostgreSQL. Rollback/Promote update pointer atomically. ArgoCD notified via API. Git mirror happens async for audit. No git revert. | 5.2, 5.4, 5.5, 8.1, 9.1, 10.2, 10.3, 11.1, 12.1, 15.2, 15.4 |
| 3 | ArgoCD notified via API, not discovery via polling | ArgoCD polling Git every 3 min added latency. With `environment_targets`, ArgoCD needs explicit notification. | All flows call ArgoCD sync API after environment_target update. Emergency fallback: if gitMirrorStatus=FAILED, API applies deploymentSpec directly. StateReconciler classifies as EXPECTED_PENDING_MIRROR. | 7.2, 10.1, 10.2, 10.3, 13.2, 15.2, 15.4 |

**Anti-pattern explicitly rejected:** Replacing ArgoCD with a custom deployment controller. ArgoCD remains the convergence executor — only the notification mechanism changes (API call vs Git polling).

---

## Appendix F — v1 Known Risks (Accepted Trade-offs)

This section documents architectural risks that are **consciously accepted in v1** to prioritize delivery speed. Each risk has a mitigation strategy and a documented trigger for upgrading to a resilient solution.

A new contributor should read this section to distinguish "known risk with plan" from "design gap without plan".

| Component | Risk | Impact if failure occurs | Current mitigation | Trigger for HA migration |
|-----------|------|-------------------------|-------------------|--------------------------|
| **PostgreSQL** | Single instance. SPOF for all mutations, serialization, and audit. | All deploys/promotes/rollbacks blocked. Reads degraded. Audit trail lost if unrecoverable. | WAL archiving to MinIO (continuous). PITR within 7 days. `LoopZeroPostgresDown` alert. Weekly backup restore test. | > 50 tenants OR > $50k MRR OR compliance audit requires HA. Migrate to CloudNativePG with 2 replicas + automated failover. |
| **Gitea** | Single instance with PVC. SPOF for source store and GitOps store. | Builds fail at clone. Deploys with `gitMirrorStatus=FAILED` proceed via fallback but Git audit is delayed. New code ingestion blocked. | git bundle to MinIO every 6h. `LoopZeroGiteaDown` + `LoopZeroGiteaBackupStale` alerts. RPO: 6h, RTO: < 1h. | > 50 tenants OR Git throughput > 70% of Gitea capacity (see section 14.5). Migrate to Gitea HA (2 replicas + shared storage) or external Git (GitHub/GitLab). |
| **MinIO** | Single instance. SPOF for build cache and frontend assets. | Builds run without cache (slower, not blocked). Frontend asset serving fails. | Build cache miss is tolerated. Frontend TTL in CDN (future). `LoopZeroMinIODown` alert. | > 100 tenants OR asset serving SLO violated. Migrate to MinIO distributed mode or S3-compatible service. |
| **ArgoCD** | 2 replicas, but single Application controller. High Application count with previews may degrade reconciliation latency. | Sync latency increases. New deploys take longer to converge. | ArgoCD controller resources: 2 CPU, 4Gi. `ArgoCD sync duration > 30s` alert. | > 500 ArgoCD Applications simultaneously. Implement Application controller sharding (`--application-namespaces`). See AI-05 analysis. |
| **etcd (mgmt cluster)** | Single k3d instance in dev. In production, depends on cloud provider's control plane. | Loss of all CRDs (Tenant, PreviewEnvironment, AppSecret, ReconciliationReport). | CRDs are reconcilable from PostgreSQL + Git. Production should use managed Kubernetes with HA etcd. | Production deployment. Use managed K8s with SLA. |

**What is NOT a known risk (i.e., these are real gaps if they fail):**

- Serialization (advisory locks) — correctly designed with crash recovery
- Idempotency — correctly designed with key expiry
- NetworkPolicy isolation — correctly designed with deny-all default
- Supply chain (cosign, SBOM) — correctly designed with ephemeral credentials

**Review cadence:** This table is reviewed quarterly. Each component's trigger conditions are checked against current metrics. When a trigger fires, the migration plan is executed in the next sprint.

---

## Appendix G — Staff+ Review Action Items Matrix (v5.0 → v5.1)

| AI | Issue | Severity | Resolution | Sections updated |
|----|-------|----------|------------|-----------------|
| AI-01 | GitOps híbrido sem precedência canônica formal | Alta | Canonical precedence rule: `environment_targets` wins during normal ops, Git wins during PITR. Startup reconciler handles transition. Scenario table with 5 conflict cases. | 3.2, 3.4, 5.2 |
| AI-02 | Fallback imperativo sem ciclo de vida documentado | Alta | Mirror retry: 6 retries over ~2.5h with exponential backoff. `EXPECTED_PENDING_MIRROR` timeout: 1h → escalates to `MIRROR_STALE`. Alerts: `LoopZeroMirrorSyncStale`, `LoopZeroGitDesiredStateDivergence`. New deploy cancels old mirror retry. | 7.5, 13.2, 15.5 |
| AI-03 | Riscos v1 aceitos sem seção unificada | Média | Appendix F: 5 SPOFs documentados (PostgreSQL, Gitea, MinIO, ArgoCD, etcd) com mitigação e critério de migração para HA. Quarterly review cadence. | Appendix F (novo) |
| AI-04 | StateReconciler: classificação por heurísticas sem lattice | Média | Authority-based drift classification: extensible controller registry in ReconciliationReport spec. 5 classification outputs. Unknown mutations = REAL DRIFT (conservative). | 13.1, 13.2, 13.3 |
| AI-05 | ArgoCD Application controller sob carga de previews | Média | Capacity analysis: ~500 Apps at 60s interval. Scaling strategy: single → resource bump → sharding. `loopzero_argocd_app_count` metric. Thresholds in 14.5. | 5.3, 6.3, 14.5, 15.2 |
| AI-06 | AppSecretOperator sem invariant de hash | Média | `workloadHash` + `hashMatch` in AppSecret status. Periodic verification on every reconcile (not just on change). 3 consecutive failures → `LoopZeroSecretSyncFailed` alert. | 7.3.2, 15.2, 15.5 |
| AI-07 | Gitea throughput sob carga não analisado | Baixa→Alta | Throughput analysis: ~200 concurrent git ops capacity. Budget allocation documented. `loopzero_gitea_concurrent_operations` metric. v1 sufficient for 100 tenants. Scaling triggers in 14.5. | 6.3, 7.5, 14.5, 15.2 |

---

## Appendix H — Maximum Rigor Review Matrix (v5.1 → v5.2)

| # | Issue | Severity | Resolution | Sections updated |
|---|-------|----------|------------|-----------------|
| P1 | North Star "5 minutes" sem cálculo de fases | Blocker | Phase breakdown table: warm cache < 3 min, cold cache < 5 min p95. Build phase dominates. Exclusions documented (image pull, DNS, migrations). | 1 |
| P2 | Fallback imperativo vs ArgoCD auto-sync (BUG DE DESIGN) | Blocker | Emergency fallback path: disable auto-sync before direct-apply, `loopzero.io/fallback-active` annotation, auto-sync restored when gitMirrorStatus → SYNCED. `loopzero-fallback` field manager. `LoopZeroFallbackStuck` alert. | 7.2, 10.1, 10.3 |
| P3 | Secret annotation vs ArgoCD sync (BUG DE DESIGN) | Blocker | `loopzero.io/secret-hash` always in Git manifests. Set during `generate-manifests` (step 13g). AppSecretOperator updates via gitops-push, not out-of-band. ArgoCD applies during sync. Invariant: never applied out-of-band. | 10.1, 16.2 |
| P4 | Startup reconciler PITR ordering gap | Blocker | PITR detection as step 1 (must run first). Explicit divergence detection: DB release vs Git release vs ArgoCD synced. Fallback reconciliation step added. 5-step ordered startup. | 7.2 |
| S1 | hashtext() collision risk | Significativo | Birthday paradox analysis: ~0.001% at 2000 envs. Impact: false serialization (liveness), not data corruption (safety). Two-int4 upgrade path documented. | 4.2 |
| S2 | "exactly-once semantics" terminology wrong | Significativo | Changed to "at-most-once delivery with idempotency-key deduplication". | 7.1 |
| S3 | NetworkPolicy hardcoded IPs | Significativo | IP resolution documented: TenantController reads ClusterIP at reconcile time. Cross-cluster alternatives: NodePort/LB, ExternalName, ClusterCredential CRD. | 14.4 |
| S4 | Flow 10.1 truncation | Significativo | Verified: flow 10.1 is complete (steps 0a through 16). No truncation — reading artifact from reviewer. | N/A |
| S5 | `loopzero_argocd_app_count` missing `app_type` label | Significativo | Added `app_type` label (production\|staging\|preview). | 15.2 |
| S6 | Drizzle ORM advisory lock integration | Significativo | `withEnvironmentLock` TypeScript snippet showing advisory lock within Drizzle transaction scope. Critical note: all mutations must use same `tx`. | 4.2 |

---

## Appendix I — Review Round 2 Matrix (v5.2 → v5.3)

| # | Issue | Severity | Resolution | Sections updated |
|---|-------|----------|------------|-----------------|
| B1 | PITR reconciler: Git commit → Release-ID lookup undefined | Blocker | Mandatory commit message format with Git trailers (`Release-ID`, `Correlation-ID`, `Build-ID`). PITR step 1b extracts Release-ID via `git log --format='%(trailers:key=Release-ID,valueonly)'`. Dedup uses `Correlation-ID` trailer. | 7.2, 7.4, 17.5 |
| B2 | AppSecretOperator gitops-push races with deploy flow | Blocker | Option C: AppSecretOperator does NOT gitops-push directly. Calls `POST /internal/secret-hash-changed`, API serializes via environment lock. All Git writers go through same lock. Added to serialized operations list. | 4.2, 7.2, 8.1, 16.2 |
| S7 | `*.*.*.loopzero.example` wildcard not supported by any CA | Significativo | Removed. Per-project wildcard cert (`*.{project}.{tenant}.loopzero.example`) is the only mechanism, generated via cert-manager DNS-01 challenge. RFC 6125 reference. | 7.7 |
| S8 | `statement_timeout` 30s conflicts with 5s lock wait | Significativo | Added `SET LOCAL lock_timeout = '5s'` in Drizzle snippet. Documented `lock_timeout` (lock acquisition) vs `statement_timeout` (total execution) as distinct mechanisms. | 4.2, 7.8 |
| S9 | ExternalName + namespaceSelector doesn't work for NetworkPolicy | Significativo | Removed incorrect ExternalName option. NetworkPolicy operates L3/L4 with IPs only. Cross-cluster uses `ipBlock` with resolved LoadBalancer/NodePort IPs. | 14.4 |
| S10 | `loopzero_git_desired_state_divergence` metric undefined | Significativo | Added to metrics list: gauge, labels `{tenant, env}`, emitted by periodic API verification loop (60s interval). | 15.2 |

---

## Appendix J — Review Round 3 Matrix (v5.3 → v5.4)

| # | Issue | Severity | Resolution | Sections updated |
|---|-------|----------|------------|-----------------|
| B3 | Build gitops-push races with `/internal/secret-hash-changed` at Git level | Blocker | Fetch-merge-push strategy: on non-fast-forward rejection, fetch HEAD, re-read runtime-mutable fields (`loopzero.io/secret-hash`), merge into generated manifests, retry. No force-push. Explicit runtime-mutable field registry. | 7.4, 10.1 |
| S11 | `/internal/secret-hash-changed` has no idempotency | Significativo | API checks Git HEAD manifest before committing. If `loopzero.io/secret-hash` already equals `newHash`, returns 200 (no-op). Hash value is the idempotency key — no Correlation-ID needed. | 16.2 |
| S12 | `loopzero_git_desired_state_divergence` false positive during mirror pending/retry | Significativo | Alert filters on `loopzero_git_mirror_status`: excludes PENDING and FAILED mirrors. New `LoopZeroGitMirrorExhausted` alert (3h threshold) for exhausted retries. Metric comment updated. | 15.2, 15.5 |

---

## Appendix K — External Review Matrix (v5.4 → v5.5)

| # | Issue | Severity | Resolution | Sections updated |
|---|-------|----------|------------|-----------------|
| 4.2 | Build egress NetworkPolicy with runtime-resolved IPs breaks on service recreation | Alta | Replaced `ipBlock` with `namespaceSelector` + `podSelector` (same-cluster). No hardcoded IPs. Cross-cluster uses `ipBlock` with Service watch (not periodic reconcile) + `LoopZeroBuildEgressStale` alert. | 14.4 |
| 4.3 | ArgoCD polling threshold too high (500 → degradation already at ~200) | Média-Alta | Split into two thresholds: Warning at 200, Critical at 400. Proactive monitoring before degradation zone. | 14.5 |
| 4.4 | No circuit breaker when ArgoCD is degraded — cascading lock timeouts | Média | ArgoCD health gate: 3 consecutive failures → circuit OPEN → reject activations with 503 PLATFORM_DEGRADED. Releases stay READY (deferred). FIFO resume on recovery. `loopzero_argocd_circuit_state` metric. | 10.1, 15.2 |
| 4.5 | Rollback side effects UX too complex for v1 incidents | Baixa-Média | v1 CLI: inline warnings + `[Y/n]` confirmation. `--force`/`--acknowledge-side-effects` flags available for CI but never required interactively. | 9.1 |
| 4.6 | p95 5-min target has no hardware baseline | Baixa | Reference hardware: 4 vCPU, 8 GB RAM, 3000 IOPS, 100 Mbps. Scaling behavior documented (2 vCPU ~8-10 min, 8 vCPU ~2-3 min). | 1 |

---

## Appendix L — Product Positioning Alignment (v5.5 → v6.0)

| # | Issue | Severity | Resolution | Sections updated |
|---|-------|----------|------------|-----------------|
| 1 | North Star describes CLI command, not product experience | Crítico | Rewritten to describe repository-connect-and-deploy experience. CLI positioned as alternative for power users/CI. Product context paragraph added. | 1 |
| 2 | "What the system never does" uses engineering language | Crítico | Split into "What the user never needs to do" (product language) and "Platform engineering guarantees" (internal language). | 1 |
| 3 | User onboarding flow completely absent | Crítico | New section 24: User Experience & Onboarding. 10-step onboarding flow, dashboard/CLI parity table, terminology mapping (internal → user-facing). | 24 (new) |
| 4 | Preview environments presented as CLI command | Crítico | Primary flow is now automatic via GitHub PR webhook. URL posted as PR comment. CLI flow is secondary for branches without PRs. | 10.4 |
| 5 | `loopzero.yaml` framed as implementation detail | Alto | Reframed as "the only infrastructure file a developer ever writes". `tenant` field removed — inferred from authenticated account. | 9.1.1 |
| 6 | Dashboard/UI not mentioned as product surface | Alto | Section 24.3: dashboard and CLI as equivalent interfaces. Dashboard is primary discovery path. Sprint priority: dashboard is parallel to backend, not phase 2. | 24.3 |
| 7 | `promote` presented as default workflow | Médio | Renamed to "advanced workflow". Product context: default is merge-to-main = auto-deploy. Promote is for staging gates and approvals. | 10.2 |
| 8 | `rollback` presented as CLI-only | Médio | Product context added: one-click dashboard action is primary, CLI is for automation. | 10.3 |
| 9 | "tenant" exposed in user-facing interfaces | Médio | URLs use `{team}` instead of `{tenant}`. Terminology mapping table: tenant→team/account in all user-facing contexts. Internal CRDs/DB unchanged. | 9.1.1, 24.4 |

---

## Appendix M — CTO Directive: CLI-first Alignment (v6.0 → v7.0)

| # | Directive | Resolution | Sections updated |
|---|-----------|------------|-----------------|
| 1.1 | Rewrite North Star for CLI-first positioning | North Star now opens with `loopzero init` + `loopzero deploy`. Vercel comparison table with 13 capabilities. CLI-first strategy explicit. | 1 |
| 1.2 | Add CLI-first DX principle | New section 2.8: "CLI-first, Vercel-quality DX". Vercel-to-LoopZero command mapping table. DX quality bar rule. | 2.8 |
| 1.3 | Create CLI Reference | New section 25: 11 command groups (auth, init, deploy, status, logs, rollback, promote, secrets, envs, teams, builds). Each with syntax, expected output, and Vercel equivalent. | 25 (new) |
| 1.4 | Create loopzero.yaml complete specification | New section 26: 3 annotated examples (minimal, secrets, worker), field reference table, supported frameworks table (10 frameworks with auto-detection). | 26 (new) |
| 1.5 | Rewrite onboarding CLI-first | Section 24.2: primary flow is `npm install` → `loopzero login` → `loopzero init` → `loopzero deploy`. Dashboard flow is secondary (v2). Parity table reordered: CLI first. | 24.1, 24.2, 24.3 |
| 1.6 | Remove dashboard-parallel-to-backend statement | Added "Note on Dashboard" in section 23: dashboard out of scope for Sprints 0-9. Three prerequisites before dashboard work begins. | 23 |
| V1 | Dashboard-as-primary references corrected | Rollback product context (10.3) updated to CLI-primary. Section 24.3 table flipped. Sprint priority paragraph removed. | 10.3, 24.3 |
| V2 | loopzero.yaml framing verified | Already "the only infrastructure file a developer ever writes" (from v6.0). No "configuration file" references found. | — |
| V3 | 5-minute SLO references CLI flow | North Star explicitly states: "SLO measured from `loopzero deploy` to live URL". | 1 |
| V4 | Vercel terminology as DX reference | Section 2.8 establishes Vercel as explicit DX bar. Every CLI command in section 25 has Vercel equivalent noted. | 2.8, 25 |

---

## Appendix N — Alignment Checklist Resolution (v7.0 → v7.1)

| # | Gap | Resolution | Sections updated |
|---|-----|------------|-----------------|
| G1 | No concrete persona — implementors don't know who they're building for | Added "Target persona — Lucas": fullstack tech lead, 15-person startup, NestJS + Vite + BullMQ in Turborepo, maintains 400-line CI pipeline, uses Vercel for marketing site, wants `loopzero deploy` simplicity. | 1 |
| G2 | `loopzero init` says "auto-detects monorepo" without listing what it detects | Added monorepo tool detection table: Turborepo, Nx, Lerna, pnpm workspaces, npm workspaces, yarn workspaces, flat (no tool). Detection markers documented. | 25.2 |
| G3 | CLI Reference has no error output examples — implementors don't know error format | New section 25.12: 5 error output examples (missing secrets, no apps detected, build failure, nothing to rollback, concurrent deploy). Error format rules: no internal terms, always suggest next action, ✗/⚠/✓ symbols, max 5 lines. | 25.12 (new) |
| G4 | Sprint 9 exit gate is purely technical — no product readiness criteria | Added "Ready for first paying user" product exit criteria: 11 items including real-user onboarding test, error message compliance, DNS/TLS operational, billing functional, docs published, on-call routing, runbooks, data retention policy. | 23 |

---

## Appendix O — Product & CLI Contract (v7.1 → v8.0)

| # | Issue | Resolution | Sections updated |
|---|-------|------------|-----------------|
| 1 | Product workflows scattered across sections 1, 24, 25 — no centralized user journey | New section 0.1: 5 core workflows with complete terminal output (first deploy, logs, rollback, secrets, preview). Product experience defined before architecture. | 0.1 (new) |
| 2 | No CLI stability contract — commands could break between releases | New section 0.2: CLI Product Contract. Backward compatibility rules (commands permanent, flags permanent, defaults frozen, exit codes permanent). Semver for CLI. Deprecation protocol with 6-month warning. | 0.2 (new) |
| 3 | No exit codes, no `--json` output, no CI mode defined | New section 0.3: Exit codes (0-5), `--json` output contract with schema examples, CI mode behavior table (`CI=true`). | 0.3 (new) |
| 4 | Partial monorepo deploy behavior undefined (api succeeds + worker fails = ?) | Section 0.3: Partial failure rules — build independence, deploy independence, no automatic rollback of siblings, exit code 5, per-app status in JSON. Rollback is atomic per environment (no per-app rollback in v1). | 0.3 |
| 5 | Timeout behavior and retry hints not defined for CLI | Section 0.3: Timeout table (build 10m, deploy 2m, connection 30s) with exact CLI output. Retry hints on transient failures. CLI never retries automatically. | 0.3 |
| 6 | North Star says "two commands" but CLI has 20+ commands | North Star rewritten: "three commands — loopzero login, loopzero init, loopzero deploy. All other commands are optional operational tooling." Aligns marketing with reality. | 1 |
| 7 | Persona doesn't declare terminal preference — leaves room for dashboard priority drift | Added explicit statement: "The primary user is a backend engineer who prefers terminal workflows over web dashboards." Any feature requiring dashboard but unavailable in CLI is a product bug. | 1 |
| 8 | Section 0 inserted as "section zero" to avoid renumbering existing sections | All existing section numbers (1-26) and cross-references preserved. Product contract precedes architecture without breaking any reference. | 0, Index |

---

## Appendix P — Sprint Plan Restructure (v8.0 → v8.1)

**Problem:** The Sprint plan (section 23) was a platform plan, not a product plan. It built infrastructure bottom-up and assumed the CLI would "fit" at the end. For a CLI-first product, this is a structural error.

**Seven specific failures identified:**

| # | Failure | Impact |
|---|---------|--------|
| 1 | CLI not present in any Sprint | Product untestable until week 21 |
| 2 | Sprint scopes understated (2-week labels for 6-week work) | False schedule confidence |
| 3 | No team size assumption | Estimates meaningless without headcount |
| 4 | No product milestone per Sprint | No demo-able increment, no founder visibility |
| 5 | Observability at Sprint 7 (week 17) | 6 sprints of blind debugging |
| 6 | Auth at Sprint 9 | API open without authentication for 7 sprints |
| 7 | `loopzero init` absent from all Sprints | Most complex CLI command with no delivery plan |

**Resolution — structural changes:**

| Change | Old | New |
|--------|-----|-----|
| Sprint structure | Platform-first, CLI absent | CLI-first, every Sprint delivers commands |
| Observability | Sprint 7 (week 17) | Sprint 0 (basic) + Sprint 7 (complete) |
| Auth | Sprint 9 (week 20) | Sprint 1 (JWT basics) + Sprint 6 (RBAC, rate limiting) |
| `loopzero init` | Not planned | Sprint 1 (full implementation) |
| `loopzero deploy` e2e | Implicit at Sprint 4 | Sprint 2 (explicit product exit gate) |
| Exit gates | Technical only | Product exit per Sprint ("user can do X") |
| Timeline | 21 weeks (unrealistic) | 27 weeks (honest) |
| Sprint summary table | Did not exist | Added — shows CLI commands delivered per Sprint |

**Sections updated:** 23 (complete rewrite)
