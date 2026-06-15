# LoopZero — Dashboard Vercel-like Integration Roadmap

> Generated 2026-06-07 from `docs/handoff/2026-06-05-dashboard-vercel-like-integration-roadmap.md` + CLAUDE.md
> "Dashboard Journeys — 20 plans formalizados (locked 2026-06-06)" section. Macro roadmap.
> Per-milestone decomposition lives in `.claude/knowledge-base/plans/2026-06-06-dashboard-journey-jNN-*.md`.

## Vision

Tornar o dashboard LoopZeroCloud uma experiência **Vercel-like 100% integrada** — onde o usuário pode fazer todo o lifecycle de uma app (sign-up → connect repo → deploy → monitor → scale → rollback → manage) sem sair do dashboard, com paridade visual e funcional aos líderes da categoria (Vercel, Railway, Fly).

## Problem

**Root problem:** o dashboard hoje tem ~24 journeys parcialmente implementadas, com gaps de design + UX + backend wiring que impedem o produto de ser usado por um cliente real sem suporte humano.

**Whose pain:** indie devs / Pro tenants que tentam usar LoopZeroCloud via dashboard e batem em telas "stub" ou comportamentos inconsistentes. Dogfood real 2026-06-07 catched bugs latentes (preview cap, env per-project, Stripe key).

## Users

- **Primary:** indie devs solo / pequenos times (Pro tenant; 1-10 projects), self-serve onboarding zero-call.
- **Secondary:** Enterprise sales-led (Team tier; SSO/SAML deferred v1.x).

## Scope

### In scope (V1 — required to call this project alive)

- 20 dashboard journeys formalizadas (`docs/handoff/2026-06-05-dashboard-vercel-like-integration-roadmap.md`)
- Memory Section chain (P0..P4) — separate roadmap em `.claude/knowledge-base/plans/2026-06-06-dashboard-memory-section-roadmap.md` — referenced via M3+ dependencies
- LIVE-validated dev-public end-to-end (não apenas vitest GREEN)
- Sponsor manual UX gate por milestone

### Explicitly out of scope

- **SSO/SAML** (Enterprise tier) — *why excluded:* deferred v1.x per MEET-2026-05-12 D2; "Contact Us" only at GA
- **Multi-cell auto-rebalance** — *why excluded:* ADR-0008 + ADR-0021 sticky placement, manual `loopzero admin tenants pin` only
- **Vercel-style multi-URL preview** — *why excluded:* sponsor verbatim 2026-06-07 "K8S pode sobrecarregar e elevar os custos por usuario"; chose Netlify single-slot model
- **CLI parity with dashboard** — *why excluded:* CLI is primary interface per CLAUDE.md Rule 9; dashboard adds visualization, doesn't replace CLI

> Items in this list are off-limits for V1. To reconsider, write a new roadmap revision — do not silently expand scope.

## Constraints

| Category | Constraint |
|---|---|
| Stack | Go 1.25 + chi (API), TypeScript + React + Vite (dashboard), PostgreSQL 16, K8s + ArgoCD, @loopzero/ui (Violet Forge) |
| Compliance / legal | Pre-release governance per `.claude/rules/pre-release-governance.md`; T2/T3 changes require meeting |
| Deadline | None hard; sprint plan tracks cadence |
| Team | 1 developer (sponsor) + LLM pair (Claude); pre-release phase |
| Runtime target | dev-public env (`*-dev.loopzero.example`) for LIVE validation; production = DOKS prod-parity via `task drill:l2-scale` |
| Budget (infra, tooling) | DO managed PG + 3 DOKS clusters; Vercel-style cost-cap explicitly chosen vs multi-URL preview |

## Success criteria

**V1 ship criterion (measurable):** 20 journeys delivered LIVE no dev-public, dogfood `/dogfood full` health score >= 70, zero CRITICAL issues introduzidos, sponsor manual gate aprovado por journey.

**North-star metric (tracked post-launch):** time-to-first-deploy (sign-up → first ACTIVE release) ≤ 5min sem ajuda humana.

---

## Milestones

> Cada milestone agrupa 1-5 journeys temáticas. Checkbox `[x]` flipa quando TODAS as journeys do milestone estão LIVE-validated + sponsor-approved. Per-journey checkboxes na DoD list rastreiam progresso granular.

### M0 — [ ] Authentication & Onboarding

**Objective:** journey completa de sign-up → email verify → first login funcionando LIVE no dev-public.

**Definition of done:**

- [x] J-04 Recovery (reset de senha) — DELIVERED 2026-06-05 (sponsor verbatim "Funcionou parabens!")
- [x] J-05 Device Success (OAuth device-flow) — DELIVERED 2026-06-05 cross-device LIVE-validated (RFC 8628 lifecycle completo)
- [ ] J-01 Login polish (OIDC GitHub + Google) — plan `j01-login-polish-plan.md`, esforço 12h
- [ ] J-02 Register (sign-up novo tenant) — plan `j02-register-plan.md`, esforço 13h
- [ ] J-03 Verification (email code via Resend) — plan `j03-verification-plan.md`, esforço 8h

**Dependencies:** none (foundation).

**Top risks:**

1. Kratos browser-mode quirks (cookie domain, top-level form POST vs XHR CSRF) — pattern conhecido `feedback_security_first_mandate` 2026-06-01
2. Resend courier ConfigMap drift (`env var literal vs interp`) — pattern `reference_kratos_resend_courier_3_layer_bug_2026_06_01`

---

### M1 — [ ] Project Lifecycle

**Objective:** user pode criar projeto + conectar repo GitHub + fazer first deploy sem sair do dashboard.

**Definition of done:**

- [x] J-09 Project Connect (repo integration) — DELIVERED 2026-06-06 (LIVE END-TO-END contra `getloopscale/loopzero-repotest`)
- [ ] J-06 Overview (home autenticada) — plan `j06-overview-plan.md`, esforço 12h
- [ ] J-07 Projects List + CreateProjectSheet wizard — plan `j07-projects-list-plan.md`, esforço 14h
- [ ] J-08 Project Detail Overview (umbrella) — plan `j08-project-detail-overview-plan.md`, esforço 10h

**Dependencies:** M0.

**Top risks:**

1. GitHub App credentials drift (6 artifacts sincronizados) — pattern `reference_github_app_credentials_chain_2026_06_06`
2. Drift 3-layer canonical (env vars Go + values.yaml + helmfile per-env) — pattern `feedback_drift_3layer_canonical_pattern`

---

### M2 — [ ] Environments + Domains (Per-Project Model)

**Objective:** environments runtime são per-project (alinhado com preview B-now); custom domains funcionais.

**Definition of done:**

- [x] J-10 Environments — DELIVERED engineering 2026-06-06 (precursor); per-project refactor M2 plan v1.1 `2026-06-07-runtime-envs-per-project-m2-plan.md` aguarda /implement
- [ ] J-10 M2 refactor LIVE-validated — `validateProject` tenant scoping (EC-1) + empty state 0 projects (EC-2) + 7 tests RED→GREEN
- [ ] J-11 Domains — plan `j11-domains-plan.md`, esforço 10h
- [ ] Plan caps Free=1, Pro=5, Team=20 sponsor sign-off (T0.2 do M2 plan)

**Dependencies:** M1 (precisa de projetos pra criar envs per-project).

**Top risks:**

1. Migration 000029 sync (após renumber memory para 000029/000030/000031)
2. Tenants Pro existentes com >5 tuplas grandfathered (EC-6 do M2 plan)

---

### M3 — [ ] Deployments Runtime Visibility

**Objective:** deploy progress LIVE-streaming + health monitoring + readiness gates visíveis.

**Definition of done:**

- [x] J-17 Deployments List + Detail — DELIVERED earlier (4-layer log markers + terminal state absorbente)
- [x] J-18 Status / Runtime Health — DELIVERED 2026-06-15 (v0.15.0; foundation-first observability synthesis surface — KPI strip 5 cards + SignalGrid 8 pills + drill-down sheet + Grafana deep-link + Prometheus 7d history hooks + URL-state drill-down; 89 hermetic tests GREEN; ADR D3 v1.1 TERMINAL_ABSORBING vs OSCILLATING dichotomy reducer). **T1.3 follow-up CLOSED v0.15.1 2026-06-15** (SSE pg_notify backend handler + frontend EventSource wire; 38 NEW hermetic tests; plan `2026-06-15-j18-t1.3.b-sse-pg-notify-backend`). Operator-HARD-STOP deferrals remain: T0.1b live Prometheus probe + T5.3 LIVE bash test + T5.4 syntax test + T6.1+T6.2 dogfood wrapper per ADR D5 + `cleanroom-up-live-test-mandate.md` INQUEBRÁVEL — closed on next sponsor JUNTOS walkthrough.
- [x] J-19 Runtime Logs Streaming — DELIVERED 2026-06-13 (v0.14.0; 5 LIVE walkthrough bugs caught + fixed during sponsor canonical chain; SSE Flusher proxy + named-event channel + StreamRuntimeLogs since-param fixes)
- [ ] J-21 Readiness Score — plan `j21-readiness-plan.md`, esforço 14h

**Dependencies:** M1, M2 (precisa de projetos + envs pra ter o que monitorar).

**Top risks:**

1. SSE stream reducer regressions (terminal status absorbente) — pattern `reference_j09_live_e2e_complete_2026_06_06`
2. Loki cross-cluster URL helmfile drift — pattern conhecido `infra/helm/promtail`

---

### M4 — [ ] Scale + Secrets

**Objective:** user pode scalar replicas + gerenciar secrets via dashboard sem CLI.

**Definition of done:**

- [ ] J-20 Scale (replicas + HPA) — plan `j20-scale-plan.md`, esforço 18h
- [x] J-22 Secrets CRUD — DELIVERED 2026-06-11 (LIVE walkthrough end-to-end GREEN; sponsor verbatim "Confirmado" pós SPONSOR_LIVE_TEST_1 created via dashboard + ExternalSecret CR `SecretSynced=True` + K8s Secret materialized com plaintext correto; broker round-trip 7ms p95 via composite property `secretName.projectID.envName`; chain Dashboard → Cloud BFF → Engine → PG vault:v1: → ESO → broker → transit decrypt → K8s Secret validated. Plans involved: original `j22-secrets-crud-plan.md` (Phase 1-8 engineering shipped earlier) + `2026-06-10-j22-openbao-transit-eso-sync-plan.md` v1.2 (Layer 1 transit) + `2026-06-10-j22-layer2-code-review-fixes-plan.md` v1.1 (Layer 2 broker hardening — 11 commits + 2 LIVE-caught fixes: SQL `project_id` + ESO v1beta1 CRD rejects metadata). Bisect anchor: `docs/operations/baselines/2026-06-11-j22-layer2-LIVE-walkthrough-green.md`)

**Dependencies:** M3 (precisa de runtime visibility pra confirmar scale efeitos).

**Top risks:**

1. ESO sync race (`secret_resyncer` post-EnvironmentEnsurer) — known systemic
2. Plan-cap enforcement integration (Scale.replicas > plan cap retorna 402)

---

### M5 — [ ] Previews + Configuration

**Objective:** preview environments per-PR LIVE + loopzero.yaml editor Monaco no dashboard.

**Definition of done:**

- [x] J-23 Previews per-PR — DELIVERED 2026-06-07 (canonical chain webhook → build → CR → operator → ns → SA → ArgoCD → wildcard TLS)
- [x] Preview-as-category + single-slot (Netlify-style) — DELIVERED 2026-06-07 (migration 000027+000028 LIVE; D3 substitute + D6 cleanup LIVE-validated)
- [ ] J-24 Configuration (loopzero.yaml editor + env config) — plan `j24-configuration-plan.md`, esforço 24h

**Dependencies:** M2 (per-project envs), M3 (runtime visibility for previews).

**Top risks:**

1. Per-project slot refactor (B-now) regressions — pattern `reference_preview_as_category_phase_0_7_engineering_complete`
2. Monaco editor bundle size impact

---

### M6 — [ ] Billing + Team

**Objective:** Stripe checkout/portal LIVE + team RBAC (Team plan early-access) operational.

**Definition of done:**

- [ ] J-13 Billing (Stripe parity) — plan `j13-billing-stripe-parity-plan.md`, esforço 12h; STRIPE_API_KEY drift fixed (memory pending #76)
- [ ] J-12 Team RBAC + invites — plan `j12-team-rbac-plan.md`, esforço 17h

**Dependencies:** M0 (auth foundation).

**Top risks:**

1. Stripe `Idempotency-Key` time-bucket (1h vs 60s) — pattern `feedback_anti_lying_workaround_pattern`
2. PlanTier CRD enum drift (pro/team missing, growth legado) — known bug #73

---

### M7 — [ ] Settings + Profile

**Objective:** workspace settings (CLI-First card, danger zone) + user profile management.

**Definition of done:**

- [ ] J-15 Profile — plan `j15-profile-plan.md`, esforço 15h
- [ ] J-16 Settings workspace — plan `j16-settings-workspace-plan.md`, esforço 14h

**Dependencies:** M0, M6.

**Top risks:**

1. Memory cross-cuts integration (`/settings` Memory card per-roadmap-memory) — pattern `reference_dashboard_integration_plan_engineering_complete_2026_05_29`
2. DSS v1.0 compliance per page (LoopZero Dashboard Design System)

---

### M8 — [ ] Audit Log

**Objective:** audit log filterable by action/actor/since LIVE — compliance + debug surface.

**Definition of done:**

- [ ] J-14 Audit Log — plan `j14-audit-log-plan.md`, esforço 8h
- [ ] Memory cross-cut: `/audit` source=memory filter chip — already in Memory Section P4 plan (delivered separately)

**Dependencies:** M0..M7 (audit precisa de operações para auditar).

**Top risks:**

1. Audit log volume (cardinality if every API call logs) — design já filtra para mutations only
2. Client-side filter vs backend `?source=` query param — OOS deferred

---

## State-of-the-art references

| Peer | License | Why it's here | Supports milestone(s) |
|---|---|---|---|
| Vercel Dashboard | proprietary (UX study only) | reference for indie dev UX patterns (sign-up → deploy ≤ 3min) | M0, M1, M5 |
| Netlify Dashboard | proprietary (UX study only) | reference single-slot preview model (chosen vs Vercel multi-URL) | M5 |
| Railway Dashboard | proprietary (UX study only) | reference per-project env scoping + service mesh | M2, M3 |
| Fly.io Dashboard | proprietary (UX study only) | reference scale + region UX | M4 |
| Linear Dashboard | proprietary (UX study only) | reference table density + keyboard-first | M3, M7 |

---

## Revision protocol

- **Marking progress:** flip `[ ]` → `[x]` no header da milestone (e per-journey DoD item) quando LIVE-validated + sponsor-approved
- **Adjusting DoD:** edit em place, anotar data em CHANGELOG.md
- **Adding milestone post-M8:** projeto outgrowing — write `ROADMAP-v2.md`
- **Removing milestone:** `~~M3 — [-] {name} (cancelled YYYY-MM-DD — reason)~~`

## Unresolved at inception

- T0.2 sponsor sign-off pendente para M2 caps Free=1, Pro=5, Team=20 (referenciado no `2026-06-07-runtime-envs-per-project-m2-plan.md`)
- Memory Section roadmap chain (P0..P4) referenciado mas tracked separately em `.claude/knowledge-base/plans/2026-06-06-dashboard-memory-section-roadmap.md`
- Pre-existing bugs em standby (tasks #73, #75, #76) podem afetar M0/M2/M6 timeline

## Cross-references

- **Source roadmap (full per-journey detail):** `docs/handoff/2026-06-05-dashboard-vercel-like-integration-roadmap.md`
- **20 per-journey plans:** `.claude/knowledge-base/plans/2026-06-06-dashboard-journey-jNN-*.md`
- **Memory Section roadmap (parallel):** `.claude/knowledge-base/plans/2026-06-06-dashboard-memory-section-roadmap.md`
- **M2 runtime envs per-project plan v1.1:** `.claude/knowledge-base/plans/2026-06-07-runtime-envs-per-project-m2-plan.md`
- **Active dashboard journeys mandate:** CLAUDE.md § "Dashboard Journeys — 20 plans formalizados (locked 2026-06-06)"
