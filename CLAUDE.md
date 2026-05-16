# CLAUDE.md

Contract between Claude and the LoopZero codebase. Read before touching any file.

## What This Project Is

LoopZero is a **CLI-first Kubernetes PaaS** for monorepo deployments. Vercel-quality DX for backends — `loopzero login`, `loopzero init`, `loopzero deploy`, and in under 5 minutes every service is live with TLS, preview environments on every PR, and rollback with one command. No Kubernetes. No infrastructure YAML.

The authoritative spec is the code itself + `CHANGELOG.md` + relevant ADRs. If this file and the codebase conflict, the codebase wins.

**Status:** Pre-release. Latest snapshot: `.claude/knowledge-base/audits/2026-05-11/SUMMARY.md` (modular audit, 12 módulos, média 3.49/4.0). H1-H7 hardening themes complete (runtime, TLS, PG HA, observability, pipeline, ops, workload security); Sprint 9 (Polish + Launch) pending gates: (a) first CI Sunday cron GREEN producing real `cell2-soaks.yml` workflow run evidence; (b) 1 prod customer day-trial with no incidents. Note: H12-H18 in the self-healing section below are cleanroom heal entries (not hardening sprints).

**Pricing (per MEET-2026-05-12 D4 = B):** Starter (Free, 3 projects) and Pro ($29/mo, 10 projects) are **public at GA**. Team ($99/mo, unlimited projects + RBAC + progressive delivery config) is **Early Access** (manual provisioning, no public signup until Sprint 10). Enterprise (custom, multi-cell + SSO/SAML + SLA) is **"Contact Us" only** — no public pricing, no signup flow, sales-led with explicit SSO/SAML "post-launch" disclosure (SSO/SAML deferred to v1.x). Go-to-market: bottom-up (validate with indie devs, learn, scale to enterprise). Indie DX is the funnel for enterprise adoption.

**Completed:** deploy happy path (full E2E dogfood validated), **progressive delivery (Argo Rollouts canary)** with TraefikService weighted routing + per-plan steps + AnalysisTemplate hooks + auto-abort on readiness failure (`progressDeadlineSeconds=300`), scaling+HPA targeting Rollout (Starter=2/Pro=5/Team=10), promote staging→prod, rollback, secrets via ESO, custom domains v2 (CF Zone Mgmt, model Vercel), GitHub App auto-deploy (~4min), monorepo validator (30 frameworks), cosign image signing + syft SBOM (CycloneDX) + per-build SBOM download (`GET /builds/{id}/sbom`) + attestation endpoint (`GET /builds/{id}/attestation`), supply-chain admission (policy-controller deny-by-default + warn-mode ClusterImagePolicy + digest enforcement in manifests), plan enforcement (4 enforcement middlewares: project/build/env/seat caps, HTTP 402 on exceed), audit log (filterable by action/resource_type/since, paginated), RSA-only JWT auth, two-host gateway, concurrent deploy rejection (1×202+1×409), JSON contracts (8/8), observability (109 loopzero_* metrics including 8 cell metrics + Grafana + SLO alerts + 7 multi-cell alerts), KEDA scale-to-zero (targets Rollout via `Kind=Rollout` ScaleTargetRef + `transfer-hpa-ownership` annotation), cell architecture (Phase 0+1+1.5 + cells table + admin pin), **cell-2 ACTIVATED + LIVE-validated** (3/3 cells connected; namespace cell-aware reconcile; rollback SLO P50=648ms / P95=705ms <<300s budget; 12 ADR gates persistente), operator multi-binary split (4 binaries), layered bootstrap (Pulumi L1 + Helmfile L2 + ArgoCD L3), `loopzero admin tenants pin/evict` + bulk-delete, tenant soft-delete with restore (`DELETE /tenants/{id}` + `POST /tenants/{id}/restore`), **PG circuit breaker** (rejects mutations at pool saturation >90%, HTTP 503), **application reaper** for orphan CRs (`ApplicationCleaned` terminal state via `ApplicationOrphaned` event), **release reaper observability** (`ReleaseStore.AppendSideEffect` writes reaper context to `release.side_effects`), **release ARCHIVED status** (migration 000008, partial index excludes archived from active queries).

**Deferred to v1.x (per MEET-2026-05-12 D2/D4):** SSO/SAML (Enterprise tier blocker — explicit "post-launch" disclosure required on sales-led conversations); cosign **keyless** verification with Fulcio+Rekor (Keyed v1.0 ships with 5 mandatory mitigations per W1.2 of MEET-2026-05-12; Keyless v1.1 ADR committed with 90-day cutover deadline post-v1.0 GA); Loki cross-cluster streaming (infra gap, not code).

**Not built (no commitment yet):** Sprint 8 Phase 1+3+4+5+6 (require new T3 meeting), production canary stages 4-5 for cell-2 (1 customer 7-day → 10/50/100% — gates G4/G6 in MEET-2026-05-12 phased rollout).

**`loopzero scale` — fully functional E2E** (B40 fixed 2026-05-06, tag `scale-b40-converged-20260506`): chain DB→deploy→K8s closed via `ScalingResolver` (`api/internal/services/scaling_resolver.go`) injected into `BuildOrchestratorDeps.ScalingResolver`. Submit calls `Resolve` BEFORE `enforceScaling` so plan-cap operates on the resolved config (closes MEET-2026-04-23-059 Constraint 6). Scale handler persists with project NAME (not UUID) per CLAUDE.md "Project name vs project ID" convention. Validated LIVE via journey-04 hardening with `kubectl get deploy -o jsonpath='{.spec.replicas}'` assertion + Plan-cap probe (`REPLICAS_EXCEED_PLAN` rejection). Cleanroom dogfood 12/12 GREEN end-to-end (52min) with B40 fix shipped.

**Path to maturity 4.0** (currently 3.49 per modular audit baseline 2026-05-11): (a) first CI Sunday cron GREEN producing real `cell2-soaks.yml` workflow run evidence — **2026-05-16 reformulated:** workflow now provisions an ephemeral drill prod-identical 4-cluster stack instead of assuming a non-existent DEV cluster (per plan `2026-05-16-drill-prod-identical-multicluster-plan.md`, finding `concepts/operations/finding-2026-05-16-gate-a-cell2-soaks-multilayer-blocker.md`). Stage 1 (1h) + Stage 2 (24h with EXPIRES_IN=26h per EC-01) execute via drill stack with mandatory teardown + doctl validator fallback (T4.1.bis EC-05). Gate (a) closure now requires DRILL_DO_TOKEN + DRILL_PULUMI_TOKEN + DRILL_CF_TOKEN secrets configured + MEET-ASYNC-AMENDMENT-2026-05-16-001 ratification + first cron GREEN. AND (b) 1 prod customer day-trial with no incidents (Sprint 9 prod canary). Local 7-day GREEN streak DROPPED as a gate (D7 reformulated 2026-05-03 — tautological in static dev env without diurnal traffic; CI cron + real prod usage are the legitimate signals). **Audit-grind progress (2026-05-12 to 2026-05-13, full tally in `.claude/knowledge-base/audits/2026-05-12-grind-summary.md` + log.md entries):** 30 of the 27 §6 backlog findings closed inline (some closures addressed multiple sub-items), 4 RFCs/design docs landed for the T2/T3 items, 5 latent bugs captured during code-walks under audit context, 3 operator runbooks/templates/dashboards shipped for Wave 2 onboarding. **Sponsor-direct-approval pattern (2026-05-13):** §6.B.2 + §6.B.3 (orphan CRD deletion, 2 CRDs deleted symmetrically) AND §6.B.5 (service-auth Ed25519 dual-control adoption) both shipped via the new pattern documented in `meetings/MEET-2026-05-13-orphan-crd-deletion.md` + `meetings/MEET-2026-05-13-ed25519-sponsor-approval.md` — sponsor explicit override of the Change Governor meeting after RFC review. Audit gaps M03 #2/#3 + M10 G1 closed (engineering side). Score un-bumped because Rule 11 requires 3 evidence sources (T1 commit+test, T2 LIVE log, T3 CI run) and the customer-trial LIVE evidence is still pending. The two original maturity gates remain the bottleneck.

**Known issues:** Local k3d cert-manager letsencrypt-prod cannot issue for `*.loopzero.local` (no public DNS for ACME challenge) — self-signed CA issuer (`loopzero-local-ca-issuer`) fills local gap, production uses CF-managed TLS so no impact, local Stage 1 canary HTTP probes report HTTP 000 in `--smoke` mode (probes informational only). Local chaos-mesh installation in k3d enters CrashLoopBackOff with `informer cache sync timeout` — full chaos suite validation deferred to drill cluster (`pulumi-drill`); local chaos test framework is multi-replica-correct as of commit `99d90dc6` and 3 specs PASS without chaos-mesh dependency. Loki cross-cluster URL was helmfile-hardcoded to k3d local IP (would silently fail cross-VPC in prod); fixed as of `ef7a7c16` — promtail values now `.gotmpl`-templated per environment, prod points at DOKS internal LB explicitly. ADR claim "auto-blocks via W1.14 hook" for cosign keyless cutover was stale (W1.14 only protects ADR deletion); real deadline ratchet shipped as `infra/scripts/check-cosign-cutover-deadline.sh` + weekly cron in `9249e207`.

---

## Development Commands

```bash
task setup               # Bootstrap k3d clusters + Helm + CRDs (idempotent)
task dev:up              # Boot k3d clusters + dependencies
tilt up                  # Dev loop with hot reload
task doctor              # Environment health check

task test                # All tests
task test:unit           # Unit tests
task test:e2e            # E2E (Ginkgo)
task lint                # golangci-lint + ESLint
task validate:manifests  # kubeconform --strict

cd cli && npm test       # CLI (Jest, 1,260 test cases — 88 suites)
cd api && go test ./...  # API (41 packages)
cd operators && go test ./...  # Operators (Ginkgo + envtest)
cd pkg && go test ./...  # Shared libs

task db:migrate          # Forward-only migrations
task db:reset            # Drop + recreate + migrate + seed

cd infra/pulumi && npx pulumi up --stack dev      # Deploy to DO
cd infra/pulumi && npx pulumi preview --stack dev # Preview
```

**Prerequisites:** Go 1.25+, Node.js 20+, Docker, k3d, Tilt, Task, Helm v3.14+, **helm-diff plugin** (`helm plugin install https://github.com/databus23/helm-diff`), **Helmfile v1.0+** (CHG-2026-04-28-LAYERED-BOOTSTRAP T0.3 — orchestrator L2), kubectl, kubeconform (`go install github.com/yannh/kubeconform/cmd/kubeconform@latest`), golangci-lint v2.11+, cosign (optional, image signing)

---

## The Rules (Non-Negotiable)

1. **Tests before code** — No production code without a failing test. Write the test, run it, confirm it fails, then implement.
2. **Code is authoritative** — Read `CHANGELOG.md`, relevant ADRs, and the codebase itself. Don't invent behavior not present in the code.
3. **No internal terms in user-facing output** — Forbidden in CLI output, error messages, `--json`: `ArgoCD`, `argocd`, `GitOps`, `gitops-push`, `CRD`, `tenant_id`, `advisory lock`, `pg_advisory`, `Gitea`, `MinIO`, `kubebuilder`, `envtest`, `Kaniko`. Use product language: deploy, team, account, service, revert, build.
4. **Exit codes are permanent** — `0` success, `1` usage error, `2` build failure, `3` deploy failure, `4` auth error, `5` partial failure. No others.
5. **`--json` is a stability contract** — Field names cannot change without major version bump. New fields can be added.
6. **Every mutation uses the environment lock** — `withEnvironmentLock` for all `(tenant, project, environment)` mutations. Concurrent-build check MUST be inside the lock (race condition otherwise).
7. **`pg_advisory_xact_lock` only** — Never session-scoped (`pg_advisory_lock`) — breaks with connection pools.
8. **State machines are strict** — Use state machine functions, never direct status updates. Invalid transitions panic (programming error).
9. **CLI is the primary interface** — Every feature lives in CLI before dashboard. Dashboard-only features are product bugs.
10. **Local equals production** — Same Helm charts, same operators, same build pipeline. No Docker Compose shortcuts. No "dev mode" with different behavior.
11. **Claims need evidence** — Any claim affecting maturity score requires **3 evidence sources** (T1 commit+test, T2 LIVE log, T3 CI run). Maturity-claim confidence cap **90-95%** (single-model family correlation residual; originally ratified as ADR-0051 in the governance suite removed 2026-05-11, but the cap remains binding). Note: this cap applies to maturity claims only — the 95%+ implementation confidence rule (`.claude/rules/workflow.md`) is a separate, stricter gate for all code changes. TTL applies: 30d operational / 60d arch / 90d strategic. Latest baseline: `.claude/knowledge-base/audits/2026-05-11/SUMMARY.md` (12 module audits + this aggregate). The previous `docs/evidence/{dim}/` per-claim tree was consolidated into the audits folder during the same cleanup.

---

## Stack

| Layer | Technology |
|-------|-----------|
| CLI | TypeScript + Commander (`cli/`) |
| API | Go 1.25 + chi + pgx (`api/`) |
| Operators | Go 1.25 + kubebuilder + controller-runtime (`operators/`) |
| Shared libs | Go (`pkg/`: errors, errors/http, logging, id, naming, conditions, statemachine, jwt, enforcement, circuitbreaker, healthcheck) |
| Database | PostgreSQL 16 (managed DO) |
| Build | Argo Workflows + BuildKit (rootless) |
| Image signing | cosign (in-pipeline, key-based) |
| GitOps | Gitea 1.22.6 + ArgoCD 2.13.3 (manual sync) |
| Progressive delivery | Argo Rollouts 1.7.2 (Helm chart `infra/helm/argo-rollouts/`, native Traefik traffic router for `traefik.io/v1alpha1`, dashboard disabled, PSA restricted) |
| Ingress | Traefik 28.3.0 + cert-manager 1.14.5 |
| Storage | MinIO / DO Spaces (S3) |
| Registry | DigitalOcean Container Registry |
| Observability | Prometheus 25.27.0 + Grafana 8.5.2 + OTel + Tempo + Loki 6.24.0 + Promtail 6.16.6 |
| Secrets | External Secrets Operator 0.14.3 |
| Auth | Ory Kratos + Ory Hydra + Redis |
| Edge | Cloudflare Tunnel + Worker + KV |
| Autoscaling | KEDA 2.16.1 + HTTP Add-on 0.8.0 |
| IaC | **Bipartite SoT formalizado per MEET-2026-05-12 D3.** **L1 (Pulumi):** 5 projetos — `pulumi/` (DO+CF cloud primitives), `pulumi-infra/` (multi-cluster topology), `pulumi-platform/` (bootstrap 32 Helm releases), `pulumi-drill/` (DR drill stack — **PROD-IDENTICAL 4-cluster topology** per plan `2026-05-16-drill-prod-identical-multicluster-plan.md` v1.1: mgmt medium + build small + runtime small + runtime-2 small + managed PG mgmt-tier + shared VPC 10.40.0.0/16, mirroring `pulumi/src/components/infra.ts:139-186`. Separate state bucket `loopzero-drill-pulumi-state` + separate credentials D7. Auto-destroy watchdog cron every 6h via `drill-auto-destroy-watchdog.yml`. Cost ~$0.32/hr active.), `pulumi-shared/` (contracts/interfaces). The previous `pulumi-control-plane/` stub (16 TODOs already reduced to 64-LOC empty-program guard by Wave 1 T1.8) was DELETED 2026-05-15 per close-all-gaps plan T2.1 — control-plane workloads ship via L2 helmfile, not Pulumi. **L2 (Helmfile):** orchestrator de 32 Helm releases (loopzero-api, loopzero-cloud, Kratos, Hydra, ArgoCD, Gitea, MinIO, etc.) — **L2 é o SoT real desses workloads**, não Pulumi. **L3 (ArgoCD):** Day-2 GitOps para tenant workloads from Gitea. **Decision tree para on-call:** is it a cloud resource? → L1 Pulumi. Is it a K8s platform workload? → L2 Helmfile. Is it a tenant app? → L3 ArgoCD. |
| Local clusters | k3d × 4 (mgmt, build, workload, runtime-2) |
| Dev loop | Tilt + Taskfile (2929 lines across 7 files) |
| Helm | 16 custom bootstrap charts + 49 value files across 34 chart dirs (`infra/helm/`) + 13 published charts (`charts/`) |
| CI/CD | GitHub Actions (40 workflows) |
| Migrations | golang-migrate (forward-only `.up.sql`) |

---

## Repository Structure (Brief)

```
loopzero/
├── docs/                  ← audits/, plans/, strategy/. Removed in 2026-05-11: cli-reference.md, STATUS.md, evidence/, architecture/ (empty placeholder), meetings/ (empty placeholder), runbooks/ (empty placeholder). Spec is the code itself.
├── pkg/                   ← shared Go: statemachine, errors, errors/http, jwt, naming, conditions, id, logging, enforcement, circuitbreaker, healthcheck
├── cli/                   ← @loopzero/cli — TS + Commander, 65 commands, 1,260 test cases (88 suites)
├── api/internal/          ← 35 subpacotes: adapters, admission, archive, argo, argocd, cellmgr, cfkv, cfzone, config, correlation, db, domain, gitclient, github, gitops, health, k8s, leader, lock, loki, manifests, metrics, middleware, placement, processing, routes, s3, server, services, shellgen, source, sse, tracing, validation, workqueue
├── operators/             ← 8 controllers (Tenant, Environment, Application, Exposure, PlatformBootstrap, GitRepositoryBinding, ClusterCredential, Preview), 8 CRDs (all wired). The 2 historical orphan CRDs (AppSecret + ReconciliationReport) deleted 2026-05-13 per ADR `decisions/2026-05-13-orphan-crd-deletion.md`.
├── infra/
│   ├── pulumi/            ← IaC (TS), providers: digitalocean/, cloudflare/, aws/(stub)
│   ├── helm/              ← 16 custom charts + 49 value files across 34 chart dirs (custom + upstream overrides)
│   ├── k3d/               ← 4 cluster configs (mgmt, build, workload, runtime-2)
│   ├── cloudflare/        ← CF Worker source (worker.mjs)
│   └── tests/             ← e2e/ (Ginkgo), chaos/, unit/, load/
└── .github/workflows/     ← 40 workflows: ci, build-images, infra-*, release, load-test, load-test-pr, helm-charts, cell2-soaks, chaos-suite, cosign-rotation-drill, decisions-stability-check, operator-split-soak, pkg-soak, multi-cell, cli-commands, cross-validation-weekly, adversarial-spotcheck, platform-health-7day-watch, prod-image-tag-check, pulumi-reconstruction-drill, status-pr-gate, status-refresh, decay-scan, charts-drift-cron, velero-backup-check, pulumi-*, integration-tests, parity-validation, reusable-*
```

Use `Glob` / `git ls-files` for current details.

---

## Knowledge Base (`.claude/knowledge-base/`)

Persistent wiki that stores **WHY** — decisions, trade-offs, rationale, and architectural patterns that cannot be derived by reading code alone. The code answers "what does it do?"; the knowledge-base answers "why was it built this way?".

### Structure

```
knowledge-base/
├── schema.md              ← Contract: page types, frontmatter, operations
├── index.md               ← Catalog of all pages (read first)
├── log.md                 ← Chronological record (append-only)
├── overview.md            ← Current project state summary
├── architecture/          ← C4 diagrams (Four Planes)
├── concepts/              ← SOTA research by domain (15 domains)
│   ├── multi-cell/        ← Placement, routing, DynamicRouter
│   ├── progressive-delivery/ ← Canary, Argo Rollouts, TraefikService
│   ├── state-machines/    ← 11 machines, framework design
│   └── ... (12 more)
├── audits/                ← Modular audits by date
├── decisions/             ← Formal ADRs
├── plans/                 ← Implementation plans
├── strategy/              ← Strategic analyses (CAP, pricing, resilience)
├── reviews/               ← Dogfood and cross-validation results
└── meetings/              ← Technical meeting records
```

### When to Consult

- **Before implementing**: read relevant `concepts/` page to understand prior decisions and rejected alternatives
- **Before changing state machines, multi-cell, or progressive delivery**: these have SOTA research pages with documented trade-offs
- **When asked "why?"**: check `decisions/` for ADRs and `concepts/` for rationale

### When to Update

- **After completing a task** that changes architecture (not cosmetics) — triggered by `[DOC-DRIFT]` hook warnings
- **Max 3 pages per task** (anti-thrashing rule)
- **Never auto-generate** as side effect of coding — doc updates are explicit, conscious decisions
- Full rules: `.claude/rules/auto-docs.md`

### Page Types

| Type | Purpose | Example |
|------|---------|---------|
| Entity | Named component/service/chart | DynamicRouter, LoopZeroApplication CRD |
| Concept | Pattern or technique | Placement Once Route Many |
| Comparison | A vs B | Deployment vs Rollout |
| Decision | Formal ADR | T3-6 State Machines |
| Synthesis | Cross-source analysis | CAP LoopZerorem Analysis |
| Audit | Modular audit result | 01-cli.md (3.5/4.0) |

Every page requires YAML frontmatter (`type`, `created_at`, `updated_at`, `updated_by`). Evidence must be classified: `[MEASURED]`, `[REPORTED]`, or `[ESTIMATED]`. Full schema: `.claude/knowledge-base/schema.md`.

---

## Agents

Claude operates as a team of specialists. Sequence work across agents for multi-component tasks.

- **orchestrator** — entry for multi-component tasks. Reads spec, breaks into subtasks, delegates, validates.
- **agent:cli** — owns `cli/`. Bar: Vercel CLI quality. Required: `--json` schema, exit codes, CI mode (`CI=true`), no internal terms, error messages suggest next action, unit tests cover all paths.
- **agent:api** — owns `api/`. Required: `withEnvironmentLock` on mutations, `Idempotency-Key` middleware, typed errors (`LoopZeroError`), `Correlation-ID` forwarded, component test for success + 409 + idempotency replay + concurrent rejection.
- **agent:operator** — owns `operators/`. Required: Ginkgo + envtest spec written first, idempotent reconcile, crash-mid-reconcile resumes correctly, state via state machine only, leader election enabled.
- **agent:infra** — owns `infra/`. Standards: idempotent Taskfile, `kubeconform --strict` passes, resource limits + readiness/liveness probes + non-root, no `latest` image tags.
- **agent:test** — writes specs before implementation. Critical tests that MUST exist: concurrent deploys (1×202+1×409), idempotency replay, lock release on crash, invalid transition panic, exit codes, JSON shape, no internal terms.
- **agent:pipeline-reviewer (Helena Duarte)** — Argo Workflows / CI/CD. Reviews: determinism (same commit = same artifact), build/release separation, scale (100 concurrent), observability, recovery (idempotent retries).
- **Meeting Review Board** — 9 Staff/Principal review agents + orchestrator for critical decisions. Never write code. Security/SRE have absolute veto. Min 3 agents per meeting.

Behavioral and governance rules for all agents: see `.claude/rules/` (workflow, TDD, CLI output, design principles, pre-release governance, LoopZero design).

Skills/slash commands (subset): `/sprint`, `/implement`, `/test`, `/review`, `/design-check`, `/failure-check`, `/schema-check`, `/state-machine`, `/dogfood`, `/sre-guardian`, `/pipeline-review`, `/meeting`, `/start-loop`, `/cancel-loop`. Full list injected by harness.

---

## Pre-Completion Checklist

```
[ ] task test:unit passes
[ ] task lint passes
[ ] task validate:manifests passes
[ ] No internal terms in CLI output (Rule 3)
[ ] Mutating API routes have withEnvironmentLock
[ ] Mutating API routes handle Idempotency-Key
[ ] CLI --json matches spec schema
[ ] Exit codes are 0/1/2/3/4/5 only
[ ] State machine changes match spec
```

**Bash blocked:** `kubectl delete` (without dry-run), `k3d cluster delete`, `DROP TABLE/COLUMN`, `git push --force` to main, `helm uninstall` on postgresql/gitea/minio.

---

## Sprint Plan

| Sprint | Focus | Status |
|--------|-------|--------|
| 0 | Foundation | ✅ |
| 1 | Login + Init + TenantController | ✅ |
| 2 | Deploy happy path | ✅ (E2E on DO) |
| 3 | Logs + Status + Secrets | ✅ (H6) |
| 4 | Rollback + Promote + Cancel | ✅ (E2E 2026-04-23) |
| 5 | Preview environments | ✅ ALL 14 tasks done. E2E validated live (12 specs pass against k3d). |
| 6 | Security + Supply chain | ✅ cosign + syft SBOM + policy-controller installed (CHG-2026-04-26-SPRINT6-IMPL). policy-controller is **deny-by-default**; warn-mode `loopzero-images-warn` ClusterImagePolicy is required (B16). As of CHG-2026-04-28-LAYERED-BOOTSTRAP T0.1 the policy is shipped as a Helm chart in `infra/helm/admission-policies/` (templates/cluster-image-policy.yaml). Deny-flip + cosign keyless verification pending Sprint 9 with Fulcio. |
| 7 | Observability | ✅ (H4) |
| 8 | Hardening + Chaos | 🟡 Phase -1 + Phase 0 + Phase 2 GREEN (CHG-001 + CHG-002a, MEET-060 + MEET-061, ADR-0015 + ADR-0016). Phase 1+3+4+5+6 → CHG-002b/c new meeting required |
| 9 | Polish + Launch | 🟡 Gates pending: (a) first CI Sunday cron GREEN, (b) 1 prod customer day-trial. Preparatory work (dogfood fixes, progressive delivery, build mode dispatch) complete in prior sprints. |
| 10 | Static Frontend Delivery | ✅ |
| 11 | Auth + Billing | ✅ |
| H1-H7 | Hardening themes (runtime, TLS, PG HA, observability, pipeline, ops, workload security). Note: H12-H18 referenced in Multi-Cell section are cleanroom heal entries, not hardening sprints. | ✅ all 7 themes complete |
| Cleanroom Dogfood Loop | Real PaaS test: destroy infra + setup + 12 journeys end-to-end | ✅ 12/12 GREEN (2026-05-05, tag `dogfood-cleanroom-converged-20260505`). 39 bugs B1-B39 fixed canonically. |
| Argo Rollouts Progressive Delivery | Canary deploys (per-plan steps + Traefik weighted routing + AnalysisTemplate + KEDA targets Rollout) | ✅ 2026-05-07 — 15 plan tasks (T0.1-T5.2) complete. Warm-infra journey **80/80 GREEN in 41m41s**, fresh-cleanroom journey **71/72 GREEN in 42m49s** (1 unrelated ESO timing fail). 6 fixes: F-17 (fixture), F-18 (progressDeadlineSeconds=300), F-19 (S1 sync_client.go accepts Rollout), F-20 (S2 e2e scaffolding), F-21 (Taskfile bootstrap selector includes argo-rollouts), F-22 (S1 ClusterRole + argoproj.io RBAC). |
| Build Mode Dispatch + User Dockerfile Security | Strategy-pattern dispatch for `build: dockerfile` opt-in + dockerfile-lint security barrier | ✅ 2026-05-08 — 4 phases complete (T0-T4). Restored `LanguageStrategy.DockerfileStep` (was removed in MEET-2026-03-29-017); new `UserDockerfileStrategy` distinct from `CustomStrategy`; `ResolveStrategyForApp(AppSpec)` dispatches `build: dockerfile` apps to UserDockerfileStrategy. New `loopzero-dockerfile-lint` binary (Go, `tools/loopzero-dockerfile-lint/`) enforces hard rejection rules: FROM allow-list (docker.io/library/*, ghcr.io/*, DOCR, k3d-loopzero-registry, public.ecr.aws/, gcr.io/, mcr.microsoft.com/, quay.io/, opt-in via LOOPZERO_DOCKERFILE_ALLOWED_REGISTRIES), --privileged/--security=insecure RUN flags, absolute/escaping bind mounts, reserved env keys (LOOPZERO_*/KUBERNETES_*/AWS_*/DO_*), seccomp/apparmor unconfined. Soft warnings: USER root in final stage, ADD remote URL, apt without --no-install-recommends, HEALTHCHECK without CMD. loopzero-packs v0.4.0 canonical lenient contract published. loopzero-api uses `dockerfileStepTemplateName(strategy)` to wire DAG correctly (no hardcoded "generate-dockerfile"). LIVE: 25/25 lint unit tests + 17 strategy/dispatch tests + e2e `journey_user_dockerfile_test.go` 3/3 GREEN (T4.3 happy + T4.4 rejection). Full journey suite 74/75 PASS in 45m45s (1 J03 ESO timing flake unrelated). |
| Dogfood 2026-05-08 Fixes | F-1..F-7 closed: golang-migrate canonical, ESO ExternalSecret instrumentation+fix, build_events dead code, Application phase fast-path Healthy, OTel local off, seed cell_id, test scaffolding cleanup | ✅ 2026-05-08 — 14 plan tasks across 5 phases. Plan v1.1 with edge-case review (12 edges, 5 MUST FIX folded into tasks). Root causes identified LIVE via instrumentation (e.g., F-2 was runtime.kubeconfig context name drift between F-11 and B34). 9 commits. F-2 ExternalSecret CR Synced=True LIVE; F-6 Application phase Progressing→Serving LIVE; F-1 schema_migrations populated version=10 dirty=false. Heal H17 (schema_migrations backfill) + H18 (stale gitea-credentials-template) codified. Final journey 81/82 PASS (98.8%, single fail = pre-existing T4.3 deploy race outside plan scope). LP-009 added to local-prod divergence matrix. |
| ArgoCD Sync Cleanroom Resilience | F-01 (S0) + F-02 (S2) from /dogfood full 2026-05-08: ArgoCD repo-server timeout overrun + duplicate Gitea credentials | ✅ 2026-05-08 — Plan v1.1 with T2.1 NO-OP amendment (CLI deploy.sync_failed exit 3 path already implemented). T1.1: `ARGOCD_APPLICATION_CONTROLLER_REPO_SERVER_TIMEOUT_SECONDS=60` in bootstrap chart (4× default 15s) — production chart UNCHANGED. T1.2: heal H18 extended to delete `argocd-repo-gitea-internal` Helm-stub conflicting with canonical `argocd-creds-gitea`. **Cleanroom journey LIVE 83/83 PASS in 44m27s** (vs pre-fix 25/35 with 10 FAIL — full warm-infra baseline restored). Zero `PLATFORM_DEGRADED` + zero `application refresh deadline exceeded` post-fix. |

**E2E Validation (2026-04-23):** 25/30 pass (83%). Confidence: deploy 99%, scaling 95%, promote 95%, rollback 99%, concurrent rejection 99%, JSON contracts 99%, internal terms (Rule 3) 99%. Log streaming 60% (infra gap, not code).

**Maturity:** Latest snapshot 3.49/4.0 (modular audit 12 módulos, 2026-05-11). Path to 4.0: (a) first CI Sunday cron GREEN, (b) 1 prod customer day-trial. Snapshot completo: `.claude/knowledge-base/audits/2026-05-11/SUMMARY.md`.

---

## Architecture Overview

### Four Planes

```
DEVELOPER PLANE   — CLI (TypeScript) ↔ API (Go)
CONTROL PLANE     — mgmt cluster: 8 operators · Gitea · PG · MinIO · ArgoCD · Traefik · cert-manager · Ory Kratos · Ory Hydra · Redis
BUILD PLANE       — build cluster: Argo Workflows + BuildKit + cosign signing
WORKLOAD PLANE    — runtime cluster: user apps via ArgoCD · Traefik + cert-manager + KEDA + Argo Rollouts (canary)
```

### Deploy Flow (Happy Path)

```
loopzero deploy → CLI validates → upload bundle → API locks env → submit Argo Workflow
  → build (BuildKit) → cosign sign → push image → generate manifests
    (Rollout + 2 Services + TraefikService + AnalysisTemplate when ProgressiveDelivery=true,
     legacy Deployment when LOOPZERO_PROGRESSIVE_DELIVERY=false)
  → push to Gitea (gitops) → ArgoCD sync → Argo Rollouts orchestrates canary steps
    (per-plan: Starter setWeight 50→100, Pro 20→Analysis→50→100, Team/Enterprise 10→Analysis→30→60→100)
  → SSE rollout.step events stream during canary; rollout.aborted (exit 3) on Degraded/Aborted
  → app Healthy → SSE deploy.completed → CLI shows URL
  (static apps: build → upload S3 → write CF KV → CF Worker serves from DO Spaces)
```

### CRDs (8 total, all wired)

Wired (✅): Tenant, Environment, LoopZeroApplication, ExposedRoute, PlatformBootstrap, GitRepositoryBinding, ClusterCredential.
Wired (✅): PreviewEnvironment — namespace + NetworkPolicy (deny-all/allow-ingress/allow-egress) + ResourceQuota + LimitRange + TTL + condition-wait flow (BuildSubmitted→BuildCompleted→WorkloadHealthy with grace period) + Failed transition + cleanup retry/CLEANUP_FAILED + finalizer + canonical naming via `naming.PreviewNamespace`. 12 envtest specs covering full lifecycle. E2E live validated against k3d.

The 2 historical orphan CRDs (AppSecret + ReconciliationReport) were deleted 2026-05-13 per ADR `decisions/2026-05-13-orphan-crd-deletion.md` — sponsor-approved T2, replacing the 2 PROPOSED RFCs `2026-05-12-appsecret-rfc.md` + `2026-05-12-reconciliationreport-rfc.md`. Both CRDs had been intentionally unregistered in the scheme (no controller, zero production touchpoints); the secret-sync path goes through ESO directly; drift-detection is provided ad-hoc via ArgoCD sync status + manual `kubectl diff` (no production gap surfaced).

### State Machines (11 in `pkg/statemachine/`)

All states shown below — not just happy path. Terminal states in **bold**. Code is authoritative (`pkg/statemachine/*.go`).

| Machine | All States | Happy Path | Failure/Recovery States | Timeouts |
|---------|-----------|------------|------------------------|----------|
| Build | QUEUED, RUNNING, RELEASING, **SUCCEEDED**, **FAILED**, CANCELLING, **CANCELLED** | QUEUED→RUNNING→RELEASING→SUCCEEDED | RUNNING→FAILED, CANCELLING→CANCELLED | QUEUED>5m, RUNNING>30m, RELEASING>10m, CANCELLING>60s |
| Release | READY, PENDING, ACTIVE, GIT_MIRROR_FAILED, SYNC_FAILED, DRIFT_ALERT, SUPERSEDED, ROLLING_BACK, ROLLED_BACK, ROLLBACK_FAILED, **CANCELLED** | READY→PENDING→ACTIVE→SUPERSEDED | GIT_MIRROR_FAILED→PENDING (retry), SYNC_FAILED→DRIFT_ALERT→ACTIVE (recovery), ACTIVE→ROLLING_BACK→ROLLED_BACK, ROLLBACK_FAILED→ACTIVE (re-attempt) | READY>5m, PENDING>10m, ROLLING_BACK>2m |
| Tenant | (empty), PROVISIONING, ACTIVE, SUSPENDED, PENDING_DELETION, **DELETING** | PROVISIONING→ACTIVE | ACTIVE→SUSPENDED→ACTIVE (reactivate), ACTIVE→PENDING_DELETION→DELETING (soft delete grace) | — |
| PlatformBootstrap | (empty), PENDING, CONFIGURING_GITEA, CONFIGURING_ARGOCD, WAITING_FOR_API, **BOOTSTRAP_COMPLETE**, **FAILED** | PENDING→CONFIGURING_GITEA→CONFIGURING_ARGOCD→WAITING_FOR_API→BOOTSTRAP_COMPLETE | Any step→FAILED (retry exhausted) | — |
| Preview | CREATING, ACTIVE, EXPIRING, **DELETED**, FAILED, DELETING, CLEANUP_FAILED | CREATING→ACTIVE→EXPIRING→DELETED | CREATING→FAILED→DELETING→DELETED, EXPIRING→CLEANUP_FAILED→DELETED | TTL-based |
| Promotion | VALIDATING, COMMITTING, SYNCING, **COMPLETED**, **REJECTED**, **COMMIT_FAILED**, **SYNC_FAILED** | VALIDATING→COMMITTING→SYNCING→COMPLETED | VALIDATING→REJECTED, COMMITTING→COMMIT_FAILED, SYNCING→SYNC_FAILED | — |
| Rollback | VALIDATING, REVERTING, SYNCING, **COMPLETED**, **REJECTED**, **BLOCKED**, **REVERT_FAILED**, **SYNC_FAILED** | VALIDATING→REVERTING→SYNCING→COMPLETED | VALIDATING→REJECTED/BLOCKED (NoPreviousRelease), REVERTING→REVERT_FAILED, SYNCING→SYNC_FAILED | — |
| Domain | PENDING_VERIFICATION, VERIFIED, PENDING_NS, NS_ACTIVE, PROVISIONING, ACTIVE, DEGRADED, FAILED, **REMOVING** | v1: PENDING_VERIFICATION→VERIFIED→PROVISIONING→ACTIVE. v2: PENDING_NS→NS_ACTIVE→PROVISIONING→ACTIVE | PROVISIONING→FAILED→VERIFIED/NS_ACTIVE (retry), ACTIVE→DEGRADED→ACTIVE (recovery), DEGRADED→FAILED | — |
| Environment | (empty), PENDING, ACTIVE, FAILED, **DELETING** | PENDING→ACTIVE→DELETING | PENDING→FAILED→PENDING (retry) | — |
| Application | (empty), PROGRESSING, SERVING, REACHABLE, DEGRADED, DOWN, **CLEANED** | PROGRESSING→SERVING→REACHABLE | REACHABLE↔DEGRADED (cyclic), DOWN→PROGRESSING (recovery), any→CLEANED (Orphaned event) | — |
| ExposedRoute | (empty), PENDING, CERT_ISSUED, REACHABLE, FAILED | PENDING→CERT_ISSUED→REACHABLE | CERT_ISSUED/REACHABLE→FAILED→PENDING (retry). No terminal state (GC via ownerRef). | — |

### Helm Chart Dual System

Two chart systems coexist with distinct purposes (ADR D1, bootstrap-integrity-fixes-plan):

| System | Path | Purpose | Used by |
|--------|------|---------|---------|
| **Bootstrap charts** | `infra/helm/` | Lightweight, inline values, direct install | Helmfile L2 (`infra/helmfile/`) |
| **Published charts** | `charts/` | OCI registry, loopzero-common library, full RBAC/monitoring | ArgoCD L3 (Day-2 GitOps), CI (`helm-charts.yml`) |

- `infra/helm/` is canonical for `helmfile sync` (bootstrap). Charts are simple, standalone, with hardcoded defaults.
- `charts/` is canonical for OCI publishing and ArgoCD. Charts use `loopzero-common` library for consistent labels and security contexts.
- **3 charts overlap functionally** (loopzero-api, loopzero-cloud, loopzero-admission-policies) — when patching one, apply the same fix to the other. `monitoring` / `loopzero-monitoring` are complementary layers (infra vs config), NOT overlapping.
- **admission-policies**: Both systems use `.Values.vaps.*` for VAP toggles and `.Values.registries.*` for registry prefixes (unified 2026-05-06, helm-chart-drift-alignment-plan).
- **loopzero-api / loopzero-cloud**: Both systems must share `runAsUser: 1000` (matching Dockerfile) and identical resource limits.
- The operators are deployed as **4 split binaries** (ADR-0023, plan `operators-sota-faang-grade-plan.md` Phase 1, 2026-05-04 LOCAL): `loopzero-tenant-operator`, `loopzero-app-operator`, `loopzero-preview-operator`, `loopzero-platform-operator`. Each has its own `operators/cmd/<name>/main.go`, Dockerfile, Helm chart in `charts/`, ServiceAccount + ClusterRole + leader-election Lease, and only mounts the env vars its controllers need. The legacy monolith chart `infra/helm/tenant-controller` and the redundant `infra/helm/loopzero-controller` were deleted in T1.5; production rollout is governed by separate `CHG-NNNN-OPERATOR-SPLIT-PROD` per ADR-0023's 4-stage cutover.

### Production Infrastructure (DigitalOcean)

3 DOKS clusters (v1.32.13, 2 nodes, SFO3) · PostgreSQL 16 managed · DO Container Registry (`registry.digitalocean.com/loopzero-infra-registry`) · CF Tunnel (zero public IPs, all runtime via CF edge) · CF Worker `loopzero-static-delivery` · CF KV `loopzero-routing` · CF Zone Mgmt for custom domains · GitHub App `LoopZero Platform` (ID 3319955) · DO Spaces `loopzero-static-assets` + CDN · State backend `s3://loopzero-infra-pulumi-state`.

### Multi-Cell Routing

Tenant workloads run on independent K8s clusters called **cells** (`cell-default`, `cell-1`, `cell-2`, ...). Routing is **placement once, route many** — not load-balanced. Local k3d state validated 2026-05-01: 3/3 ClusterCredentials connected; DynamicRouter `count=3`.

**Lifecycle of a deploy:**

1. **`loopzero bootstrap` (NEW tenant) — placement decision (one-time).** `RoundRobinEngine.AssignAndExecute` (`api/internal/placement/engine.go:179`) runs inside a SERIALIZABLE transaction with `pg_advisory_xact_lock`. Algorithm: bin-packing — pick the cell with FEWEST tenants AMONG those not at `maxPerCell` capacity (default 500). Writes `tenants.cell_id` (canonical SoT) + Tenant CR `spec.cellID`. Sticky thereafter.
2. **`loopzero deploy` / `promote` / `rollback` / `logs` / `status` — routing (every call).** Reads `tenants.cell_id` from PG. `DynamicRouter.Resolve(cellID)` (`api/internal/placement/dynamic_router.go:133`) is pure hash lookup — zero decision. Returns `CellConfig {kubeconfig, ArgoCD URL/token, base domain}`. Used by `cellmgr/client_manager.go:85` (kubectl) and `argocd/sync_client_factory.go:72` (sync).
3. **`loopzero admin tenants pin --tenant X --cell Y` — operator override (only path to move tenant).** `CellPinner.Pin` (`api/internal/services/cell_pinner.go`): DB UPDATE with capacity gate + Tenant CR MergePatch atomically. Operator reconcile re-emits ArgoCD App+AppProject with `destination.name=Y`. ArgoCD resolves via cluster Secret label.

**Key SoT separation:**
- `tenants.cell_id` PG column — canonical placement decision
- `cells` SQL table — operational metadata (capacity, status active/draining/maintenance/retired, region, audit)
- `ClusterCredential` CRD on mgmt cluster — connectivity (kubeconfig Secret + spec.cellID) — `K8sCellStore.LoadCells()` polls every 60s and **looks up by `spec.cellID`, NOT CR name** (per MEET-053 fix B9)

**Self-healing (`task k3d:heal` codifies):**
- **H12** (matrix entry 153) — cell-1 ClusterCredential missing despite cells SQL active: bootstraps via `clustercredential-create.sh cell-1 workload <scoped-kc>`. Cluster-name=`workload` matches `k3d-loopzero-workload-server-0` docker container (NOT `runtime` — that container doesn't exist locally; B82 fallback would render unreachable).
- **H13** (matrix entry 154) — cell-default SA token rotated/invalidated (401 even with exp claim valid): `kubectl create token loopzero-system/default --duration=24h` + patches Secret `data.kubeconfig` preserving CA + in-cluster server URL.
- Both pinned via vitest in `infra/pulumi/src/__tests__/heal-script-H12.test.ts` (19 invariants).

**Scoped-kubeconfig regen (`task k3d:regen-scoped` codifies — CORRECTIONS.txt item N, plan T2.1):** After ANY topology change that may invalidate scoped SA tokens (node-pool resize via doctl, manual SA recreate, post-rotation drift) — `task k3d:regen-scoped` regenerates the scoped SA + token for every ClusterCredential EXCEPT `cell-default` (which is bootstrap-managed by `bootstrap-cell-default-cc.sh` per H13). Implementation: `infra/scripts/regen-all-scoped-kubeconfigs.sh` (495 LOC), flock-protected (`/tmp/loopzero-regen-scoped-kubeconfig.lock` per EC-3 concurrent-regen race protection), Python filter excludes `spec.cellID == "cell-default"` per EC-1, optimistic-concurrency Secret patch with `--resource-version`, creates Secret if absent per EC-6. Pass `cluster_name` BARE (e.g., "workload"), NOT cell_id and NOT loopzero-prefixed — lib `scoped-kubeconfig.sh` internally prepends `k3d-loopzero-` per B81/B82. `DRY_RUN=1 task k3d:regen-scoped` previews without modification. Idempotent. 9/9 hermetic tests in `infra/tests/check-regen-scoped-kubeconfigs_test.sh`.

**Why no automatic rebalancing** (ADR-0008 + ADR-0021 D3):
- DNS continuity (endpoints `*.{tenant}.loopzero.local` cravado em cluster origem)
- State affinity (Secrets/ESO + S3 bundles + ArgoCD App+AppProj)
- Hot-spot signal = upgrade tier signal, NOT migration signal
- Auditability (`loopzero admin tenants pin` writes audit_log; auto-rebalance would be opaque)

Cell drain (`cells.status='draining'`) is operator-driven — `loopzero admin tenants pin --to cell-novo` per tenant.

---

## Known Systemic Patterns

| Pattern | Mitigation |
|---------|-----------|
| "Implemented but not wired" | Verify call site after implementation; integration test required |
| "Correct code in wrong place" | Single source of truth per behavior; grep duplicates before merge |
| "Project name vs project ID" | CLI sends project name as `project_id` in builds. Use name (not UUID) for build/release queries. Only `projects` table uses UUID. |
| "ArgoCD `notifiers` not `services`" | argo-cd Helm v7.x: use `notifications.notifiers`, `services` silently ignored |
| "Single ArgoCD App per tenant" | One App per environment. Any Degraded resource blocks ALL notifications. `gitops.Remove` failure is FATAL. |
| "Implemented but not documented" | Code is source of truth. Verify before claiming missing. |
| "Custom domains = CF Zone per domain" | v2: CF Zone Mgmt, NS change by user, no cert-manager Certificate (CF handles TLS). Full state machine: see § State Machines (Domain row) — includes v1 (TXT verification) backward-compat flow + degraded/failed/removing states. |
| "CF scan imports conflicting apex" | v2 create flow deletes conflicting A/AAAA/CNAME at apex BEFORE creating routing CNAME |
| "GitHub tarball ≠ git bundle" | autodeploy.go: tarball → extract → git init/add/commit → bundle → S3 |
| "Auto-deploy source = commit SHA" | Key `{tenant}/source/{commitSHA}.bundle`. CommitSHA from webhook must match exactly. |

---

## Common Mistakes — Read Before Coding

| Mistake | Fix |
|---------|-----|
| `pg_advisory_lock` (session-scoped) | Use `pg_advisory_xact_lock` |
| Concurrent build check OUTSIDE lock | Move inside `withEnvironmentLock` |
| `errors.New("...")` | Use `LoopZeroError` with typed code |
| `release.Status.State = "active"` | Use `release.Transition(event)` |
| `kubectl` in user-facing flow | Proxy through API |
| Mark release ACTIVE before ArgoCD confirms | Wait for sync confirmation |
| Internal terms in CLI output | Product language (Rule 3) |
| `notifications.services` in ArgoCD Helm | Use `notifications.notifiers` |
| `project.ID` (UUID) for build/release queries | Use project name from `loopzero.yaml` |
| `gitops.Remove` error treated non-fatal | FATAL — delete must fail |
| cert-manager Certificate for CF-managed domains | Skip Certificate CR (use `CFManaged`) |
| Forward-only migration violations (`DROP`) | Explicit sign-off required |
| Writing prod code before failing test | Test first, always |
| Multi-cell ClusterCredential CR name = cellID assumption | Wrong — `K8sCellStore` lookup is by `spec.cellID`, not metadata.name. CR named `workload` with `spec.cellID=cell-1` works fine (per H12). |
| `wire-clusters.sh` with `cluster-name=runtime` for k3d | Wrong — local cluster is `loopzero-workload`. Use `cluster-name=workload` so `scoped-kubeconfig.sh` resolves `k3d-loopzero-workload-server-0` docker container (B82 + H12). |
| Cached SA token in ClusterCredential Secret stops working before exp claim | SA UID changed (recreated during rollout). Mint fresh: `kubectl create token loopzero-system/default --duration=24h` + patch Secret data.kubeconfig (per H13). |
| ArgoCD `InvalidSpecError: project X does not exist` (but project EXISTS) | Repo-server cached `app path does not exist` for ~5min. `SyncClient.TriggerSync` calls `?refresh=hard` BEFORE POST /sync (B32). |
| VAP `failurePolicy: Fail` deadlocks namespace deletion | Add `matchConditions` excluding `system:serviceaccount:kube-system:namespace-controller`, `system:kube-controller-manager`, `system:masters` group (B33). |
| `secret K8s manager not available — ESO sync disabled` in api logs | `RUNTIME_KUBECONFIG` not mounted. Mount `loopzero-cluster-kubeconfigs` Secret (key=`runtime.kubeconfig`) + export `RUNTIME_KUBE_CONTEXT=in-cluster` (B34). |
| `seed-local-user.sh failed during regen` mid-journey | `HYDRA_NAMESPACE=loopzero-auth` (prod default) ≠ `loopzero-system` (k3d standalone). Set `SEED_HYDRA_NS=loopzero-system` in `_common.sh` (B35). |
| `releases_status_check` constraint violation on rollback | DB CHECK missing `ROLLING_BACK`/`ROLLED_BACK`. Migration `000010_release_rollback_states.up.sql` adds them. State machine declares it but DB CHECK was never updated (B37). |
| Admin endpoints return 404 `Endpoint not available` despite valid X-Service-Auth | `LOOPZERO_ADMIN_API=true` env var missing (defense-in-depth flag). Production sets via Pulumi; standalone Tilt YAML must include it (B38). |
| `kubectl rollout restart` is non-blocking — race with downstream operations | Always pair with `kubectl rollout status` if subsequent commands trigger reconciles in old/new pod. Old pod (env stale) reconciles BEFORE termination, sobrescreve writes (B39). |
| Convergence loop / health checker filters `res.Kind != "Deployment"` only | Wrong — workload primitive is now Deployment OR Rollout (default-on progressive delivery). Accept BOTH (`res.Kind != "Deployment" && res.Kind != "Rollout"`) or every Rollout-backed release stays PENDING forever (F-19). |
| `kubectl get deploy` in test scaffolding to verify workload exists | Wrong post-canary plan — query `deployments.apps,rollouts.argoproj.io` together. ProveDead helpers must include both kinds + TraefikService (F-20). |
| Adding a workload-cluster Helm release without updating `task k3d:bootstrap-workload` selector | The standalone `task setup` only syncs charts named in the explicit `--selector name=X` list. New runtime-cluster releases (e.g., argo-rollouts) MUST be added to that list, otherwise fresh cleanroom never deploys them (F-21). |
| New tenant-side CRD requires applying it via ArgoCD without granting `loopzero-ctrl-runtime` RBAC | The scoped kubeconfig's ClusterRole `loopzero-ctrl-runtime-rbac-admin` (`infra/helmfile/scripts/lib/scoped-kubeconfig.sh`) lists framework CRD apiGroups; missing entries cause silent `forbidden` failures during ArgoCD sync. Add the new apiGroup explicitly (F-22 added `argoproj.io`). |
| Argo Rollouts 1.7.x defaults Traefik traffic-router queries to legacy `traefik.containo.us` | LoopZero runs Traefik v3 on `traefik.io/v1alpha1`. Set `controller.extraArgs: [--traefik-api-group=traefik.io, --traefik-api-version=v1alpha1]` in `infra/helm/argo-rollouts/values.yaml` (F-16). |
| `Rollout` annotation `argocd.argoproj.io/sync-wave: "-1"` on first deploy | Wave -1 makes ArgoCD apply Rollout BEFORE the stable+canary Services. Rollout immediately Degraded (`InvalidSpec — stableService not found`) and ArgoCD halts. Use default wave 0 for ALL canary resources (F-15). |
| Test fixture `loopzero.yaml project: <name>` must match journey's `project` constant | Fixture gets named via loopzero.yaml; deployed Rollout = `<project>-<app>`. If they diverge, `kubectl get rollout <project>-<app>` returns NotFound and the assertion fails. Create dedicated fixture or align constant (F-17). |
| Renderer omits `Rollout.spec.progressDeadlineSeconds` | Argo Rollouts default 600s exceeds the 8min E2E budget for readiness-driven aborts. Renderer sets `progressDeadlineSeconds: 300` for ALL Rollouts so canary failures (e.g., bad `/health` probe) transition to Degraded within budget (F-18). |
| `if app.Build == "dockerfile"` in workflow_builder orchestration layer | Wrong layer — strategy decisions belong in `LanguageStrategy.DockerfileStep`, not in the orchestrator. Use `ResolveStrategyForApp(AppSpec)` + `g.Strategy.DockerfileStep(...)`. UserDockerfileStrategy emits `dockerfile-lint`; language strategies emit `generate-dockerfile`. Build mode dispatch plan T1.6. |
| DAG hardcodes `"generate-dockerfile"` as build-image dependency | Wrong post-build-mode-dispatch plan — UserDockerfileStrategy emits `dockerfile-lint` instead. Use `dockerfileStepTemplateName(strategy)` helper to capture the actual emitted name. Hardcoded literal causes `invalid spec: template name 'generate-dockerfile' undefined` for build:dockerfile workflows. |
| loopzero-packs v0.3.x stale image rejects user Dockerfile | Pin `imageLoopZeroPacks = ":v0.4.0"` (canonical lenient contract). v0.4.0 copies user Dockerfile verbatim to `--output`, exits 0. Cleanroom heal H16 ensures registry has v0.4.0 via `infra/scripts/build-and-push-loopzero-packs.sh`. |
| New tenant CRD apiGroup added without RBAC update | `loopzero-ctrl-runtime-rbac-admin` ClusterRole's framework-CRD apiGroups list MUST include the new group. F-22 added `argoproj.io`; build-mode-dispatch added pattern (no new apiGroup, but Strategy refactor reminded us). Defense-in-depth + governance-gate registers scope. |
| Dockerfile linter rejection in user-facing message uses internal terms | Use product language: "Dockerfile validation" not "BuildKit lint", "LoopZero's allow-list" not "ClusterImagePolicy registries". Hard rejection rule names (FROM_REGISTRY_NOT_ALLOWED etc.) are stable contracts but messages must be plain English. |
