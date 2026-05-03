# LoopZero PaaS — Maturity Assessment

**Date:** 2026-05-03 (last update: fix-all-platform-issues-plan iter 7 — 18/20 tasks GREEN, Stage 1 ADR-compliant 1h LIVE GREEN, T4.3 E2E LIVE deploy on cell-2)
**Previous date:** 2026-05-02 (cell-2-activation-plan iter 3)
**Assessor:** Engineering team (self-assessment)
**Score: 3.9 / 5.0** (was 3.8 post-cell-2-activation; +0.1 from fix-all-platform-issues-plan iter 1-7. **Empirical evidence**: T4.3 LIVE — `loopzero deploy` end-to-end on cell-2: build-b04e0905c112b23d → rel-6b22241d012f71ce on cell-2 → ArgoCD Synced → Pod Running 4m52s on loopzero-runtime-2 → /health 200 → cell-1 namespace NotFound (isolation confirmed). Stage 1 1h ADR-compliant: 120 probes × 30s = 3600s GREEN with 0 5xx. Bug-fix sprint: cell-aware EnvironmentReconciler factory (covers monolithic + split-binary), CC controller reorder eliminating token-propagation gap, application reaper for orphan CRs, migration 000008 ARCHIVED status, mgmt-argocd duplicate env var fix, release reaper observability via AppendSideEffect. CI cron weekly armed for ADR-compliant 1h+24h Sunday runs (cell2-soaks.yml) + hourly platform-health-7day-watch.yml. **D7 reformulated 2026-05-03**: 4.0 not gated on local 7-day GREEN streak (tautological in static env) but on first CI Sunday GREEN + 1 prod customer day-trial.)

---

## Summary

LoopZero has a strong technical foundation -- 4-plane architecture, cell-based scaling, strict state machines, advisory locks, consistent DIP -- all implemented with high code quality. The core deploy flow works end-to-end on real infrastructure (DigitalOcean). However, the product is commercially unvalidated: zero external users, missing table-stakes features (preview environments), and scale validation is loopzeroretical.

**In one line:** Excellent engineering, incomplete product. (Sprint 5 / Preview Environments closed the most-cited "table stakes" gap from prior assessments — wiring is now end-to-end across CLI/API/DB/GitHub; final controller polish + E2E remain.)

---

## Dimension Scores

| Dimension | Score | Justification |
|-----------|:-----:|---------------|
| **Core Deploy Flow** | 4.5/5 | `loopzero deploy` -> live URL works E2E on DO. Rollback ~3s. Promote cross-env. GitHub App auto-deploy (~4min). Real, validated, competitive. |
| **DX / CLI** | 4.2/5 | 36 commands, `--json` on all, standardized exit codes, 1079 tests. Preview environments now wired (`loopzero preview list/inspect/delete` against real API since Sprint 5 / T4.1). Still missing: onboarding < 10 min (Sprint 9). |
| **Infrastructure / Operations** | **3.8/5** | **4 DO clusters bootstrapped fresh** (mgmt + build + runtime + runtime-2 with Sprint 8 v3.2 sizing — mgmt s-4vcpu-8gb-amd × 3 medium, PG db-s-2vcpu-4gb medium, Valkey managed NEW), CF Tunnel, 43 Prometheus metrics, SLO alerts, Loki, **Chaos Mesh CRDs registered in 4 clusters** (Sprint 8 T0.3), KEDA + KEDA HTTP add-on (with quay.io rbac-proxy fix per Sprint 8 v3.2), **Gitea HA 4/4 with RWX NFS**, **Harbor HA core 2/2 + registry 2/2**, **adopt-or-create policy (ADR-0015) for live stateful primitives** (registry/VPC/CF/buckets), bootstrap reproducibility documented in 4 runbooks (sprint8-bootstrap, pulumi-state-repair, cf-tunnel-cleanup, minio-distributed-migration). MAINTENANCE_MODE middleware for D-7 PG resize procedure. mgmt memory 36-39% (ADR-0010 #6 target met). Still: cross-cluster logs have gaps, cell-2 routing not activated (Sprint 9), L3/L4 not run yet (CHG-002b/c). Infra layer matured significantly under Sprint 8 v3.2. |
| **Security** | 3.5/5 | cosign signing + **syft SBOM (CycloneDX) generated per release & persisted in S3 with presigned URL endpoint** (CHG-2026-04-26-SPRINT6-IMPL), **policy-controller Audit mode active in runtime cluster** (key-based verification — flips to Deny after 2 weeks of clean logs per D4), per-app attestation sidecar table (ADR-0014 fix), PSA restricted, NetworkPolicy, non-root, RSA-only JWT, secret rotation, 5 PrometheusRules + 3 runbooks for the policy webhook. Still: `platform.ts` (495 LOC installer) has zero tests; admission policy is Audit-only, not yet Deny-mode (intentional, D4 — flip pending 2-week observation window). |
| **Scale / Multi-tenancy** | **3.5/5** | Cell architecture code 96% complete + **cell-2 LIVE-validated 2026-05-02** (cell-2-activation-plan iter 3): 12/12 ADR gates GREEN, real tenant pinned via `loopzero admin tenants pin --cell cell-2`, ArgoCD destination flip works, ClusterCredential reconcile works. Stage 1 + Stage 2 canary scripts smoke-validated; full 1h+24h ADR soaks deferred to operator/CI. 3/3 cells discoverable + connected (`loopzero_cell_discovery_count=3`). H14+H15 codify operational recovery. Still: zero chaos tests under load, zero L4+ load tests with multi-cell traffic split, no production canary stages 4+5 (out-of-scope per ADR D6). |
| **Product Completeness** | 3.4/5 | Sprint 5 / Preview Environments — **ALL 14 plan tasks complete** with ACs + DoDs validated. 12 lifecycle E2E specs passing against live k3d cluster (PR opened→preview, synchronize→UPSERT, concurrent PRs, PR closed→soft-delete, plus 8 CLI contract specs). ~110 unit tests + 12 envtest specs + 7 SQL constraint tests + live ciclo CRUD against real Postgres. Still missing: `loopzero scale` persistence, SBOM, admission policy, chaos suite, polish/onboarding. A user paying $29/mo now gets preview envs (table-stakes feature shipped) but still needs 5-min onboarding. |
| **Tests / Confidence** | **3.6/5** | 1079 CLI + 41 Go packages + 186 Ginkgo specs + **42 pulumi vitest specs (NEW Sprint 8 v3.2 — adopt-or-create + sprint8-fixes regression suite)** + **3 CI gate tasks (validate:helm-charts, validate:k8s-versions, validate:bootstrap)** + GitHub workflow `pulumi-validate.yml` PR gate. **bootstrap-smoke.sh 16-item validation matrix automated**. Sprint 8 v3.2 systematization closes the bootstrap fragility class detection (chart name renames, K8s version rotations, image deprecations, env var drift, securityContext PSA). Gaps remain: `platform.ts` zero tests, concurrent reconcile test partial, 10 failing tests in `api/internal/k8s`. |

---

## What Justifies 3.4 (Not Higher)

1. **Zero external users have ever used the product.** No real dogfooding outside the team. The onboarding flow has never been tested by someone who doesn't know the system.

2. **Preview environments are partial.** Sprint 5 landed Phases 0/1.1/2/3 (foundation + DB + store + service + API handler + GitHub PR webhook + comment) — 11 of 14 plan tasks complete. Still missing: T1.2 controller condition-wait flow (BuildSubmitted→BuildCompleted→WorkloadHealthy before Active), T1.3 envtest specs covering retry/CLEANUP_FAILED, T4.2 5 Ginkgo E2E specs against a live cluster. Until T4.2 passes against a fresh PR end-to-end, the feature is "API-shaped, not user-validated".

3. **Scale validation is loopzeroretical.** Cell architecture is in code, but never ran with 2 cells, never had a real chaos test, never processed 1000 concurrent deploys. Confidence is based on design, not evidence.

4. ~~**Supply chain is incomplete.** cosign without syft + without admission policy = half the story. It's like having the lock but not locking the door.~~ **(Closed by CHG-2026-04-26-SPRINT6-IMPL — syft SBOM generated per release, policy-controller in Audit mode verifying every tenant image. Deny-mode flip pending 2-week observation window.)**

## What Justifies 3.4 (Not Lower)

1. **The deploy flow actually works** on real infrastructure (DO, not simulation)
2. **1079 CLI + 41 Go + 186+ Ginkgo + ~110 new preview unit tests** is serious coverage. Migration validated via real Postgres (7 SQL constraint tests).
3. **Cell architecture code ready** is a real competitive differentiator -- no Tier 2-3 competitor has this
4. **Custom domains Vercel model** is production-grade (CF Zone Management, WAF, auto-TLS)
5. **GitHub App auto-deploy** works end-to-end -- this is the experience that sells
6. **Preview environments wiring complete** (CHG-2026-04-26-PREVIEW-IMPL): `loopzero preview list/inspect/delete` work against real API, GitHub PR webhook creates/updates/deletes previews on `opened`/`synchronize`/`closed` actions, PR comment with marker-based upsert keeps developers in GitHub UI

---

## Path to 4.0

| Action | Impact | Sprint |
|--------|:------:|--------|
| Preview Environments — entire 14-task plan complete + E2E live validated | +0.4 (banked) | Sprint 5 (CHG-2026-04-26-PREVIEW-IMPL) |
| Polish / Onboarding < 10 min first deploy | +0.2 | Sprint 9 |
| ~~Cell-2 activation + real chaos test~~ — Cell-2 activation done (2026-05-02, iter 3); chaos test pendente | +0.1 (banked, was +0.2) | cell-2-activation-plan |
| 1 external early adopter doing real deploys | +0.1 | -- |

## Path to 5.0

Everything above, plus:
- ~~syft SBOM generation~~ — DONE (CHG-2026-04-26-SPRINT6-IMPL)
- ~~Admission policy (reject unsigned images)~~ — Audit mode DONE; Deny-mode flip pending 2-week observation window (CHG-2026-04-26-SPRINT6-IMPL)
- Full chaos engineering suite
- 50+ real tenants
- Published SLA
- Validated incident runbooks
- 99.9% uptime measured over 30 days

---

## Test Suite (Current)

| Component | Count | Status |
|-----------|------:|--------|
| CLI (Jest) | 1,079 | Passing (59 suites; 7 skipped) |
| API + Shared libs (Go) | 41 packages | Passing (~110 unit tests added 2026-04-26 across api/db, api/services, api/routes, api/github for preview env wiring) |
| Operators (Ginkgo + envtest) | 200+ specs | Passing (+12 PreviewController envtest specs covering namespace + NP + Quota + conditions wait + Failed transition + delete; +14 unit tests for naming, cleanup-attempt counter, evaluateWorkloadGate) |
| SQL migration validation | 7 constraint tests | Passing (000004_preview_environments validated against real Postgres 16) |
| **E2E preview lifecycle (live k3d)** | **12 specs** | **Passing** (PR opened→preview, synchronize→UPSERT, concurrent PRs, PR closed→cleanup + 8 CLI contract specs) |
| Capacity tests | 10 files | Passing |
| CI pipeline | 14 workflows | Green |

---

## Feature Completion Matrix

| Feature | Status | Notes |
|---------|:------:|-------|
| Deploy happy path | Done | E2E validated on DO |
| Rollback | Done | ~3s, E2E validated |
| Promote | Done | cross-env, HTTP 202 |
| Secrets (ESO) | Done | sync <30s, zero value leak |
| Custom Domains (CF Zone) | Done | WAF/DDoS, auto-TLS, E2E with aquim.ai |
| GitHub App auto-deploy | Done | push -> live in ~4min |
| Monorepo (30 frameworks) | Done | static auto-infer, path traversal protection |
| Image signing (cosign) | Done | every build signed |
| Scale-to-zero (KEDA) | Done (code) | manifests + Helm + Pulumi. Cluster KEDA activation pending |
| Cell architecture (Phase 0+1) | Done (code) | 25/26 items. Phase 2b activation = config only |
| Plan enforcement | Done | Starter/Pro/Team caps |
| Auth hardened | Done | RSA-only, audience validation, rotation |
| Observability | Done | 43 metrics, dashboards, SLO alerts |
| JSON contracts | Done | 8/8 commands, 0 internal term leaks |
| Preview environments | Done | Sprint 5 / CHG-2026-04-26-PREVIEW-IMPL: all 14 plan tasks complete + E2E validated against live k3d (PR opened→preview→synchronize UPSERT→closed soft-delete). DB migration 000004 + store + service + API + GitHub PR webhook (PreviewDeployer) + comment + controller workload (NP/Quota/LR + condition-wait flow with grace period) + cleanup retry/CLEANUP_FAILED + 12 envtest specs + 12 E2E specs. |
| SBOM (syft) | Done | Sprint 6 / CHG-2026-04-26-SPRINT6-IMPL: CycloneDX JSON per release, persisted in S3, queryable via `GET /api/v1/builds/:id/sbom` with presigned URL. loopzero-syft-scanner image baked + DAG step `sbom-{app}` after `sign-image-{app}` (continueOn.failed=true so SBOM never blocks deploys). |
| Admission policy | Done (Audit) | Sprint 6 / CHG-2026-04-26-SPRINT6-IMPL: sigstore policy-controller activated on runtime cluster, key-based verification of every tenant image. Currently in Audit mode (D4) — violations logged, not denied. Manual flip to Deny pending 2-week observation window with `loopzero_release_app_attestation_total{signed="false"}=0`. |
| `loopzero scale` persistence | Not done | Stub (202 acknowledged) |
| Chaos engineering | Not done | Sprint 8 |
| Onboarding polish | Not done | Sprint 9 |

---

*This assessment will be updated quarterly or after major milestones.*
