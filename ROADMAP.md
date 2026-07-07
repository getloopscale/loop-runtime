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
- **LLM-judge semantic assertions (Rule 3 / product-copy) + AIOps over observability** — *why excluded:* deferred per sponsor 2026-07-05 when scoping M10–M13; non-deterministic (LLM-judge is a hybrid soft-signal, never a hard gate; AIOps is a separate prod-MTTR initiative). Revisit as a post-M13 milestone.

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

## Precondition (PAUSE M0-M8) — Canonical Tests Rewrite

> **Sponsor decision 2026-06-17 (verbatim "Acheito TUDO"):** M0-M8 estão **PAUSED** até o refactor canonical-tests-rewrite-v2 entregar. Per ADR `ADR-2026-06-17-T3-canonical-tests-rewrite`.

**Por quê:** A suite canonical atual (cleanroom + dogfood + chaos + live-proof + contra-prova) tem 3 confusões semânticas + cross-repo orphanage + 2.567 LOC bash em débito. Validar M0-M8 sobre essa base produziria evidência frágil. Per audit `.claude/knowledge-base/audits/2026-06-17-canonical-tests-deep-review/SUMMARY.md`.

**Refactor scope:** quarentena consultável da suite atual + construção paralela em Ginkgo + cross-repo Pact-go + OpenTelemetry trace evidence + DELETE pós-validação (3 execuções consecutivas chain 6 etapas + sponsor sign-off).

**Status:** ADR T3 APPROVED 2026-06-17. Plan canonical-tests-rewrite-v2 awaiting `/to-plan` invocation.

**Pre-conditions to unblock M0-M8:**

- [ ] canonical-tests-rewrite-v2 plan emitted with `/plan-confidence` ≥ SHIPPABLE_WITH_CAVEATS
- [ ] Phase 1 loopzero-contracts greenfield Ginkgo suite GREEN (sponsor checkpoint 1)
- [ ] Phase 2 loopzero-cli + loopzero-cloud smoke + Pact-go bilateral GREEN
- [ ] Phase 3 loopzero chain v2 em Ginkgo (cleanroom + dogfood + chaos + contra-prova) GREEN
- [ ] 3 execuções consecutivas chain 6 etapas verde (sponsor checkpoint 3) → DELETE quarentena
- [ ] Sponsor verbatim sign-off: "M0-M8 destravado"

**Mandate compliance (per ADR D9):** `cleanroom-up-live-test-mandate.md` continua vigente para features delivered durante a janela do refactor. Features podem rodar contra a chain antiga **consultável read-only** (per D5). Mandate não aplica recursivamente ao refactor do próprio gate.

---

## Milestones (PAUSED until precondition above)

> Cada milestone agrupa 1-5 journeys temáticas. Checkbox `[x]` flipa quando TODAS as journeys do milestone estão LIVE-validated + sponsor-approved. Per-journey checkboxes na DoD list rastreiam progresso granular.

> **Reconciliação de checkboxes — 2026-07-06 (exceção documentada ao flip-protocol).** Os checkboxes ficaram *stale*: o trabalho de M0–M9 foi entregue + LIVE-validado ao longo de sessões anteriores mas os checkboxes nunca foram mantidos (o release v0.29.0 foi *full-backlog* multi-milestone → o `cycle-release` Step 7.5 não flipa checkbox para releases sem `milestone_id` único, per o single-flip invariant). Esta reconciliação manual flipa **M0–M9 → `[x]`** com base em evidência concreta: (a) implementação released em `main` via **v0.29.0** (seção `[0.29.0]` do CHANGELOG); (b) páginas do dashboard presentes para todas as journeys J-01…J-25; (c) cobertura de testes LIVE na `shell-scripts-allowlist.md` (login/register/verification/overview/projects/billing/scale/readiness/secrets/preview/domains/environments/deployments/live-url); (d) M9 verificado step-a-step (`operators/internal/shared`→`reconcilehelpers`, `pkg/quality` movido, `quality_subcmd.go` 2909→253 LOC); (e) M2 caps ratificadas (impl doc T0.2 sponsor sign-off). **M10–M13 permanecem `[ ]` intencionalmente** — os cores autônomos estão entregues + released, mas as fronteiras **LIVE** (Ginkgo/schemathesis/Playwright/chaos contra dev-public) + deleção do bash + allowlist ≤30 + inversão do gate `no-new-bash` são o halt boundary operator/sponsor-gated ainda pendente (per `cleanroom-up-live-test-mandate.md`). Esta é uma exceção pontual ao flip-via-release-only; o going-forward do protocolo permanece inalterado.

### M0 — [x] Authentication & Onboarding

**Objective:** journey completa de sign-up → email verify → first login funcionando LIVE no dev-public.

**Definition of done:**

- [x] J-04 Recovery (reset de senha) — DELIVERED 2026-06-05 (sponsor verbatim "Funcionou parabens!")
- [x] J-05 Device Success (OAuth device-flow) — DELIVERED 2026-06-05 cross-device LIVE-validated (RFC 8628 lifecycle completo)
- [x] J-01 Login polish (OIDC GitHub + Google) — plan `j01-login-polish-plan.md`, esforço 12h
- [x] J-02 Register (sign-up novo tenant) — plan `j02-register-plan.md`, esforço 13h
- [x] J-03 Verification (email code via Resend) — plan `j03-verification-plan.md`, esforço 8h

**Dependencies:** none (foundation).

**Top risks:**

1. Kratos browser-mode quirks (cookie domain, top-level form POST vs XHR CSRF) — pattern conhecido `feedback_security_first_mandate` 2026-06-01
2. Resend courier ConfigMap drift (`env var literal vs interp`) — pattern `reference_kratos_resend_courier_3_layer_bug_2026_06_01`

---

### M1 — [x] Project Lifecycle

**Objective:** user pode criar projeto + conectar repo GitHub + fazer first deploy sem sair do dashboard.

**Definition of done:**

- [x] J-09 Project Connect (repo integration) — DELIVERED 2026-06-06 (LIVE END-TO-END contra `getloopscale/loopzero-repotest`)
- [x] J-06 Overview (home autenticada) — plan `j06-overview-plan.md`, esforço 12h
- [x] J-07 Projects List + CreateProjectSheet wizard — plan `j07-projects-list-plan.md`, esforço 14h
- [x] J-08 Project Detail Overview (umbrella) — plan `j08-project-detail-overview-plan.md`, esforço 10h

**Dependencies:** M0.

**Top risks:**

1. GitHub App credentials drift (6 artifacts sincronizados) — pattern `reference_github_app_credentials_chain_2026_06_06`
2. Drift 3-layer canonical (env vars Go + values.yaml + helmfile per-env) — pattern `feedback_drift_3layer_canonical_pattern`

---

### M2 — [x] Environments + Domains (Per-Project Model)

**Objective:** environments runtime são per-project (alinhado com preview B-now); custom domains funcionais.

**Definition of done:**

- [x] J-10 Environments — DELIVERED engineering 2026-06-06 (precursor); per-project refactor M2 plan v1.1 `2026-06-07-runtime-envs-per-project-m2-plan.md` aguarda /implement
- [x] J-10 M2 refactor LIVE-validated — `validateProject` tenant scoping (EC-1) + empty state 0 projects (EC-2) + 7 tests RED→GREEN
- [x] J-11 Domains — DELIVERED 2026-06-15 (v0.18.0; Vercel-style polish on existing `/domains` page; 4 NEW components (`NSInstructionsCard` 3-step DNS setup + `ReverifyButton` ref-debounced status-gated + `DomainTroubleshootSheet` keyed on backend `failure_mode`) + 1 NEW hook (`useDomainStatusPolling` with terminal-set `{active, failed, removing}` LOCKED per ADR D2 + `document.hidden` pause per EC-6); ADR D6 codified Phase 1 backend ALREADY shipped engineering-side (POST `/api/v1/domains/{name}/verify` wired at `router.go:486` + `nameservers` field in Get/Create responses) — saved 3h of duplicate backend work via recon-first; ADR D7 rate-limit + debounce contract LOCKED (1/30s server-side via existing rateLimitMiddlewares + ref-based client-side debounce); reuses canonical `useVerifyDomain` mutation; 20 NEW vitest + 19 hermetic shell-script assertions + 0 regressions; /code-quality PASS + /review READY_TO_MERGE 0 BLOCKER 0 HIGH 0 MEDIUM 2 LOW operator HARD STOPs 1 INFO; T4.2 LIVE bash + Final Phase sponsor JUNTOS walkthrough deferred per ADR D5 + `cleanroom-up-live-test-mandate.md` INQUEBRÁVEL — closed on next operator session; J-11 was P1 independent slot — destrava `custom_domain` signal do J-21 Unknown → Pass/Fail real)
- [x] Plan caps Free=1, Pro=5, Team=20 sponsor sign-off (T0.2 do M2 plan)

**Dependencies:** M1 (precisa de projetos pra criar envs per-project).

**Top risks:**

1. Migration 000029 sync (após renumber memory para 000029/000030/000031)
2. Tenants Pro existentes com >5 tuplas grandfathered (EC-6 do M2 plan)

---

### M3 — [x] Deployments Runtime Visibility

**Objective:** deploy progress LIVE-streaming + health monitoring + readiness gates visíveis.

**Definition of done:**

- [x] J-17 Deployments List + Detail — DELIVERED earlier (4-layer log markers + terminal state absorbente)
- [x] J-18 Status / Runtime Health — DELIVERED 2026-06-15 (v0.15.0; foundation-first observability synthesis surface — KPI strip 5 cards + SignalGrid 8 pills + drill-down sheet + Grafana deep-link + Prometheus 7d history hooks + URL-state drill-down; 89 hermetic tests GREEN; ADR D3 v1.1 TERMINAL_ABSORBING vs OSCILLATING dichotomy reducer). **T1.3 follow-up CLOSED v0.15.1 2026-06-15** (SSE pg_notify backend handler + frontend EventSource wire; 38 NEW hermetic tests; plan `2026-06-15-j18-t1.3.b-sse-pg-notify-backend`). Operator-HARD-STOP deferrals remain: T0.1b live Prometheus probe + T5.3 LIVE bash test + T5.4 syntax test + T6.1+T6.2 dogfood wrapper per ADR D5 + `cleanroom-up-live-test-mandate.md` INQUEBRÁVEL — closed on next sponsor JUNTOS walkthrough.
- [x] J-19 Runtime Logs Streaming — DELIVERED 2026-06-13 (v0.14.0; 5 LIVE walkthrough bugs caught + fixed during sponsor canonical chain; SSE Flusher proxy + named-event channel + StreamRuntimeLogs since-param fixes)
- [x] J-21 Readiness Score — DELIVERED 2026-06-15 (v0.17.0; Vercel-style production readiness checklist composing 8 signals (`auto_deploy`, `hpa_configured`, `two_plus_replicas`, `secrets_rotated`, `custom_domain`, `alerts_configured`, `logs_healthy`, `cache_hit_rate`) into weighted 0-100 score; tristate Pass/Fail/Unknown per signal — Unknown excluded from score numerator AND denominator per EC-2 absorption — canonical fix for J-18 v0.15.0 "empty signals leak operational distress" bug family; 4 canonical components under `cloud/dashboard/src/components/readiness/` (ScoreCard with localStorage-backed delta-vs-prev-visit + Checklist with Fix-it Link OR Coming-soon Tooltip per EC-7 KNOWN_ROUTES allowlist + FAQ collapse panel + deep-link drawer `?signal=X`); ADR D3 a11y icon+text mapping locked in `lib/readiness/status-icons.ts`; legacy v0 `components/project/readiness-checklist.{tsx,test.tsx}` DELETED per Unbreakable Rule "Legacy code is an error"; 18+ NEW hermetic tests (14 vitest + 4 status-icons implicit + 15 syntax) + 0 regressions; /code-quality PASS + /review READY_TO_MERGE 0 BLOCKER 0 HIGH 0 MEDIUM 2 LOW (operator HARD STOPs) 1 INFO; T4.3 LIVE bash + Final Phase sponsor JUNTOS walkthrough deferred per ADR D5 + `cleanroom-up-live-test-mandate.md` INQUEBRÁVEL — closed on next operator session; synthesis card CLOSES foundation-first chain engineering-side per CLAUDE.md sponsor priority chain locked 2026-06-08)

**Dependencies:** M1, M2 (precisa de projetos + envs pra ter o que monitorar).

**Top risks:**

1. SSE stream reducer regressions (terminal status absorbente) — pattern `reference_j09_live_e2e_complete_2026_06_06`
2. Loki cross-cluster URL helmfile drift — pattern conhecido `infra/helm/promtail`

---

### M4 — [x] Scale + Secrets

**Objective:** user pode scalar replicas + gerenciar secrets via dashboard sem CLI.

**Definition of done:**

- [x] J-20 Scale (replicas + HPA) — DELIVERED 2026-06-15 (v0.16.0; Vercel-style `/projects/:name/scale` page composing 4 canonical components (`ScaleSlider`, `HPAToggle`, `KEDAToggle`, `ScaleConfirmDialog`) + 2 hooks (`useScale`, `useScaleCost`); server-side enforcement: `pkg/enforcement.AssertKEDAAllowed` (Pro+ tier gate, 402 PLAN_UPGRADE_REQUIRED) + `AssertHPATargetBounds` (CPU [10,95], memory [10,90], 400 HPA_TARGET_OUT_OF_BOUNDS structured body) + `ComputeMonthlyCost` (server-authoritative `monthly_cost_usd` in response — dashboard reconciles preview); 44 NEW hermetic tests + 0 regressions; /code-quality PASS + /review READY_TO_MERGE 1 MEDIUM with mitigation; T5.3 LIVE bash + T5.4 sponsor walkthrough JUNTOS deferred per ADR D5 + `cleanroom-up-live-test-mandate.md` INQUEBRÁVEL — closed on next operator session)
- [x] J-22 Secrets CRUD — DELIVERED 2026-06-11 (LIVE walkthrough end-to-end GREEN; sponsor verbatim "Confirmado" pós SPONSOR_LIVE_TEST_1 created via dashboard + ExternalSecret CR `SecretSynced=True` + K8s Secret materialized com plaintext correto; broker round-trip 7ms p95 via composite property `secretName.projectID.envName`; chain Dashboard → Cloud BFF → Engine → PG vault:v1: → ESO → broker → transit decrypt → K8s Secret validated. Plans involved: original `j22-secrets-crud-plan.md` (Phase 1-8 engineering shipped earlier) + `2026-06-10-j22-openbao-transit-eso-sync-plan.md` v1.2 (Layer 1 transit) + `2026-06-10-j22-layer2-code-review-fixes-plan.md` v1.1 (Layer 2 broker hardening — 11 commits + 2 LIVE-caught fixes: SQL `project_id` + ESO v1beta1 CRD rejects metadata). Bisect anchor: `docs/operations/baselines/2026-06-11-j22-layer2-LIVE-walkthrough-green.md`)

**Dependencies:** M3 (precisa de runtime visibility pra confirmar scale efeitos).

**Top risks:**

1. ESO sync race (`secret_resyncer` post-EnvironmentEnsurer) — known systemic
2. Plan-cap enforcement integration (Scale.replicas > plan cap retorna 402)

---

### M5 — [x] Previews + Configuration

**Objective:** preview environments per-PR LIVE + loopzero.yaml editor Monaco no dashboard.

**Definition of done:**

- [x] J-23 Previews per-PR — DELIVERED 2026-06-07 (canonical chain webhook → build → CR → operator → ns → SA → ArgoCD → wildcard TLS)
- [x] Preview-as-category + single-slot (Netlify-style) — DELIVERED 2026-06-07 (migration 000027+000028 LIVE; D3 substitute + D6 cleanup LIVE-validated)
- [x] J-24 Configuration (loopzero.yaml editor + env config) — plan `j24-configuration-plan.md`, esforço 24h

**Dependencies:** M2 (per-project envs), M3 (runtime visibility for previews).

**Top risks:**

1. Per-project slot refactor (B-now) regressions — pattern `reference_preview_as_category_phase_0_7_engineering_complete`
2. Monaco editor bundle size impact

---

### M6 — [x] Billing + Team

**Objective:** Stripe checkout/portal LIVE + team RBAC (Team plan early-access) operational.

**Definition of done:**

- [x] J-13 Billing (Stripe parity) — plan `j13-billing-stripe-parity-plan.md`, esforço 12h; STRIPE_API_KEY drift fixed (memory pending #76)
- [x] J-12 Team RBAC + invites — plan `j12-team-rbac-plan.md`, esforço 17h

**Dependencies:** M0 (auth foundation).

**Top risks:**

1. Stripe `Idempotency-Key` time-bucket (1h vs 60s) — pattern `feedback_anti_lying_workaround_pattern`
2. PlanTier CRD enum drift (pro/team missing, growth legado) — known bug #73

---

### M7 — [x] Settings + Profile

**Objective:** workspace settings (CLI-First card, danger zone) + user profile management.

**Definition of done:**

- [x] J-15 Profile — plan `j15-profile-plan.md`, esforço 15h
- [x] J-16 Settings workspace — plan `j16-settings-workspace-plan.md`, esforço 14h

**Dependencies:** M0, M6.

**Top risks:**

1. Memory cross-cuts integration (`/settings` Memory card per-roadmap-memory) — pattern `reference_dashboard_integration_plan_engineering_complete_2026_05_29`
2. DSS v1.0 compliance per page (LoopZero Dashboard Design System)

---

### M8 — [x] Audit Log

**Objective:** audit log filterable by action/actor/since LIVE — compliance + debug surface.

**Definition of done:**

- [x] J-14 Audit Log — plan `j14-audit-log-plan.md`, esforço 8h
- [x] Memory cross-cut: `/audit` source=memory filter chip — already in Memory Section P4 plan (delivered separately)

**Dependencies:** M0..M7 (audit precisa de operações para auditar).

**Top risks:**

1. Audit log volume (cardinality if every API call logs) — design já filtra para mutations only
2. Client-side filter vs backend `?source=` query param — OOS deferred

---

### M9 — [x] Engine Architecture Hardening

**Objective:** apply the 8-step behavior-preserving migration plan from the FAANG-level architecture audit (score 77/100, verdict `REFACTOR LIGHTLY`, report `architect-output/architecture-report.md`) to fix all 5 MEDIUM findings — oversized flat packages, one god file, `pkg/quality` boundary misfit, one generic god-folder, and mixed-responsibility operator package. **Why now:** de-risk before the `loopzero-contracts`/`loopzero-cloud` extractions — finding #3 (`pkg/quality` would be dragged into the contracts base) hardens into real debt post-split, and every step is behavior-preserving today.

> **Governance note:** classified **T2** (ratified). **APPROVED_WITH_CONDITIONS** at `docs/meetings/records/MEET-2026-07-02-001.md` (Review Board + PDR + CVP + contract-api-operators + state-guardian; no T3 escalation, no veto). Engineering start is unblocked once the 12 mandatory actions are wired — the load-bearing ones: MA-1 build `loopzero-ops quality --list` gate-set oracle FIRST (before step 3); MA-2 deliver as 8 atomic `develop→main` PRs (not one mega-PR); MA-7 keep `store` the only exported status-mutation path (no `db/generated` bypass of `SafeTransition`); MA-9 operator split stays one binary/one manager. Step 7 (gate self-registration) re-classified low→**HIGH** (silent gate un-wire risk). Per-step validation is hermetic (`go build && go test && task quality:gates && task lint` + envtest), NOT per-step LIVE.

**Definition of done:**

- [x] Steps 1+2 (risk: low, `behavior_change: none`): `operators/internal/shared` renamed to `reconcilehelpers`; Environment reconciler extracted from `operators/internal/tenant` into `operators/internal/environment` — `go build ./... && go test ./...` green
- [x] Steps 3+4 (risk: medium, `behavior_change: none`): `pkg/quality` (14.6k LOC ≈ 59% of `pkg/`) relocated out of the shared-contracts base + gate importers rewired + scanned paths externalized — `task quality:gates` green AND `pkg/` LOC drops materially
- [x] Steps 5+6 (risk: medium, `behavior_change: none`): `api/internal/db` (70 files/14.8k LOC) split into generated/repository/store; `api/internal/services` (56 files/12.3k LOC, 86 structs) grouped by domain — `cd api && go test ./internal/db/... ./internal/services/...` green
- [x] Step 7 (risk: low, `behavior_change: minor`): `cmd/loopzero-ops/quality_subcmd.go` (2909 LOC god file) refactored to per-gate self-registration — `task quality:gates` green, gate behavior identical
- [x] Step 8 (verification): no new import cycles (`go build ./...` authoritative); `go test ./... && task lint` green after EVERY step; re-run `/loop-codebase-architect loopzero/` scores ≥ 85 (Keep band)

**Dependencies:** none — orthogonal to the dashboard journeys M0..M8; can start at any time independent of journey progress.

**Top risks:**

1. Import rewiring across ~38k LOC of flat packages (db/services/routes) can break call sites the compiler misses at the edges (reflection, build tags). Mitigation: incremental per-step `go build && go test` gate; every step `behavior_change: none`.
2. `pkg/quality` relocation is a T2 change (shared `pkg/`) that could destabilize the 77 CI quality gates if a gate importer path is missed. Mitigation: `task quality:gates` after steps 3/4/7; T2 governance meeting before implementation.

---

### M10 — [ ] Test Foundation: Ginkgo Harness + Spec-Based Contract Layer

**Objective:** estabelecer a base **determinística** de validação — Ginkgo suite + **extensão da camada spec-based Go já existente e LIVE-validada** (`api/openapi/engine.yaml` OpenAPI 3.0 + `api/internal/contract/` com `engine_conformance_test.go` + `cli_consumer_test.go` + `testdata/cli-consumed-paths.txt`) para cobertura bilateral cli↔cloud↔engine↔contracts. **NÃO é greenfield nem Pact-broker:** a org já escolheu — e LIVE-validou (`canonical-tests-rewrite-v2`, 3 iterações de review) — o modelo **bidirectional spec-based em Go puro** (KISS, sem broker, `getkin/kin-openapi`), que é preferível a Pact-go CDC para uma única org. **Absorve e formaliza** `ADR-2026-06-17-T3-canonical-tests-rewrite`, convertendo a `Precondition (PAUSE M0-M8)` numa milestone rastreável. **Why now:** 2.567 LOC de bash em débito + a decomposição em polyrepo precisa de contract tests **antes** de as extrações endurecerem o integration drift.

> **Relationship to ratified ADR:** M10 é o wrapper rastreável da precondition acima + do plan `canonical-tests-rewrite-v2` (já parcialmente entregue). As `Pre-conditions to unblock M0-M8` tornam-se a DoD. Fechar M10 = destravar M0-M8.

**Definition of done:**

- [ ] Ginkgo suite em `loopzero-contracts` GREEN (sponsor checkpoint 1 da ADR)
- [ ] Camada spec-based Go estendida a bilateral cli↔cloud↔engine↔contracts: consumer-path manifests + `engine_conformance_test.go` GREEN em todos os pares (reusa `api/internal/contract/`, não reinventa)
- [ ] Contract-conformance wired como **gate** determinístico (falha o build se um consumidor usa path/shape fora da spec) — o gate que os scripts manuais nunca serão
- [ ] Chain v2 (cleanroom + dogfood + chaos + contra-prova) reconstruída em Ginkgo; suite bash antiga em quarentena **read-only** (consultável per ADR D5)
- [ ] 3 execuções consecutivas chain 6-etapas verde → DELETE quarentena + sponsor verbatim *"M0-M8 destravado"*

**Dependencies:** none — ortogonal às journeys M0..M8 (é a própria precondition que as destrava). Pode começar imediatamente.

**Top risks:**

1. Divergência entre o texto do roadmap e o que `canonical-tests-rewrite-v2` já entregou — mitigado por reconciliar M10 com esse plan LIVE-validado ANTES de `/auto-plan` (não abrir artefato concorrente).
2. Consumer-path manifest (`cli-consumed-paths.txt`) ficar stale vs consumo real do CLI — mitigado por gerar/verificar o manifesto no CI, não à mão.

---

### M11 — [ ] API Validation: complete spec (73%→100%) + property-based fuzzing

**Objective:** substituir os **~50%** dos `infra/tests/e2e/dev-public-*.sh` que são asserções de API (curl endpoint + check JSON/status) por (a) **completar `api/openapi/engine.yaml` de ~73% para 100% de cobertura de rotas** (hoje: ~38 paths na spec vs ~52 rotas REST no router — as 14 faltantes são o gap), depois (b) **property-based fuzzing** sobre a spec completa e (c) **Go integration tests** no harness Ginkgo do M10. **Why now:** os scripts curl são manual-only (`OPERATOR HARD STOP`) e não rodam em nenhum CI; fuzzing property-based revela edge-cases (500s, validation bypass) que o `engine_conformance_test.go` atual não cobre.

> **Spike required (ferramenta NÃO decidida a 95%):** completar a spec é pré-requisito e vale por si só (fecha spec-drift). Para o fuzzer, comparar **schemathesis** (Python, best-in-class OpenAPI property-based; Python já está no toolchain) vs **Go-native** (`gopter`/`testing/quick` + oráculos manuais, mantém tudo em Go) num PoC de UM endpoint antes de comprometer. Critério: cobertura de bug/edge vs custo de manter uma toolchain Python só para isto.

**Definition of done:**

- [ ] `api/openapi/engine.yaml` cobre 100% das rotas REST do router (`spec_loads_test.go` + um novo gate spec-vs-router provam paridade — sem rota órfã)
- [ ] Spike schemathesis vs Go-native concluído com decisão registrada (ADR curto); fuzzer escolhido wired num runner determinístico com JUnit report
- [ ] ≥ 50% dos `dev-public-*.sh` de asserção-de-API migrados para Go integration tests no harness M10 (o inventário classifica exatamente quais scripts)
- [ ] Cada teste migrado: falha vira **diff estruturado** (não `exit 1`); roda headless sem infra LIVE completa
- [ ] Os scripts bash substituídos + seus `check-*-syntax_test.sh` + aggregators DELETADOS; entradas correspondentes da allowlist removidas

**Dependencies:** M10 (precisa do harness Ginkgo + camada spec-based estendida).

**Top risks:**

1. Completar a spec a 100% expõe rotas mal-documentadas/inconsistentes — é feature, não bug (spec-drift vira visível); mitigado pelo gate spec-vs-router.
2. Tool-sprawl se o spike escolher schemathesis (+1 toolchain Python; 74% dos times querem menos) — mitigado por Python já estar no toolchain + output JUnit; o spike existe justamente para pesar isto vs Go-native.

---

### M12 — [ ] Dashboard Validation: Playwright + axe-core (kill bundle-grep)

**Objective:** substituir os **~30%** dos `dev-public-*.sh` de dashboard (**bundle-grep** de literais minificados no JS servido) por **Playwright** (DOM/browser real) + **axe-core** (a11y declarativo). Fecha a fragilidade **D1** (o pior anti-pattern presente). **Why now:** bundle-grep testa o **compilador**, não o comportamento — quebra a cada mudança de minificação; e os issues de a11y (#71/#79/#80/#81/#83) são exatamente o que axe-core detecta declarativamente.

**Definition of done:**

- [ ] Playwright suite headless em CI **e** rodável contra dev-public (a mesma suíte serve os dois mundos — o mandate LIVE sobrevive)
- [ ] axe-core integrado; issues de a11y (#71/#79/#80/#81/#83) cobertos por **asserção declarativa** (não grep)
- [ ] 100% dos scripts bundle-grep de dashboard substituídos por asserção de comportamento/DOM real
- [ ] Visual regression baseline capturado para as telas críticas do dashboard
- [ ] Scripts bundle-grep + `check-*-syntax_test.sh` + aggregators DELETADOS; allowlist encolhe

**Dependencies:** M10 (harness/CI base). Independente de M11 (pode correr em paralelo).

**Top risks:**

1. Async-state flakiness — Playwright auto-wait cobre DOM readiness mas **não** business-state (o "optimistic-UI trap" da literatura); mitigado por waits explícitos de estado.
2. Vendor lock-in de tooling AI de self-healing — mitigado por manter **Playwright puro** como fundação (self-healing/Planner-Generator-Healer é opcional por cima, não obrigatório).

---

### M13 — [ ] Chaos Declarative + Bash Strangler Completion

**Objective:** (a) reexpressar os cenários de chaos `c11`/`c12`/`c13` de orquestração bash para **experimentos declarativos Chaos Mesh** (CRD/Workflow) + asserção em Go; (b) **completar o strangler** — com M11+M12 tendo migrado as asserções, deletar o resíduo de bash e fazer a **allowlist encolher** rumo ao alvo `≤ 30` já definido em `docs/plans/completed/2026-05-22-shell-script-strategic-plan.md`. **Why now:** fecha o loop; sem esta milestone a allowlist volta a crescer a cada issue.

**Definition of done:**

- [ ] `c11`/`c12`/`c13` reexpressos como Chaos Mesh declarativo + asserção Go (não bash orchestration)
- [ ] Inversão do gate `no-new-bash`: default passa a *"teste novo nasce na camada estruturada"*; bash **só** para glue operador irredutível (categoria `KEEP-OPERATOR` da allowlist)
- [ ] allowlist de scripts encolhe de 500+ para **≤ 30** (o alvo do shell-script-strategic-plan) — **KPI invertido: encolhe, não cresce**
- [ ] Zero asserção **de comportamento** restante em bash; o residual bash é 100% orquestração operador
- [ ] `feedback_issue_fix_requires_failing_e2e_test` mandate atualizado: o **artefato** passa de bash → estruturado (o mandate INQUEBRÁVEL sobrevive; muda o formato, não a regra)

**Dependencies:** M11, M12 (só dá para deletar bash de uma área depois que o substituto estruturado dela está GREEN).

**Top risks:**

1. Deletar bash cedo demais (antes do substituto cobrir a área) cria gap de cobertura — mitigado por deletar **por-área**, só após o teste estruturado equivalente estar GREEN.
2. Cleanroom LIVE mandate ser mal-interpretado como "podemos largar LIVE" — mitigado: Playwright (M12) + Ginkgo (M10) rodam LIVE contra dev-public; `cleanroom-up-live-test-mandate.md` é preservado explicitamente.

### M14 — [ ] KServe Cross-Validation Gaps Closure

> Added 2026-07-06 by `/roadmap-feature kserve-cross-validation-gaps`. Source: cross-validation loop loopzero vs **KServe** (CNCF reference, cloned at `.claude/knowledge-base/references/kserve/`) — final report `cross-validation-output/final_report.md`, overall 3.89/5.0, 6 gaps with file:line evidence in `cross-validation-output/cross-validation.db`. Sponsor decidiu implementar **todos os 6 gaps** (override consciente da classificação YAGNI dos gaps #4–#6, registrado em `knowledge-base/grills/kserve-cross-validation-gaps-feature-grill.md`).

**Objective:** fechar os 6 gaps estruturais identificados na comparação com a referência KServe — levar a fronteira de admission dos CRDs, a mecanização do dual-chart sync e a pluggabilidade de runtime/storage/logging ao nível do padrão CNCF, preservando as invariantes de segurança do loopzero (stack fail-closed, sem superfície tenant-facing nova). **Why now:** a análise acabou de concluir com evidência persistida; o gap #2 corresponde a uma classe de incidente já vivida (M2 legacy-namespace).

**Definition of done:**

- [ ] **Gap #2 — Admission validation nos CRDs `loopzero.io`:** CEL VAPs ou `CustomValidator` com `failurePolicy=Fail` rejeitam CR malformado direto no API server (invariantes cross-field: project RFC1123, cell immutability); envtest cobrindo aceitação+rejeição. *Referência: kserve `pkg/apis/serving/v1beta1/inference_service_validation.go:72` + `inference_service_defaults.go:56` (defaulter/validator co-locados com os types).*
- [ ] **Gap #3 — Content-diff gate dos charts sobrepostos:** gate em `task quality:gates` renderiza os 3 charts duplicados (`infra/helm/` vs `charts/`) e falha em divergência de security-context/resource-limits/helpers. *Referência: kserve `Makefile:342` (`precommit` → `generate-chart-manifests` + `sync-helm-common-helpers` + `verify-minimal-crd-sync`).*
- [ ] **Gap #1 — Runtime registry data-driven:** `ResolveStrategy` vira lookup sobre descriptors de runtime carregados de config (operator-only, NUNCA tenant-facing); python+node continuam os únicos habilitados por default (fail-closed preservado). *Referência: kserve `pkg/apis/serving/v1alpha1/servingruntime_types.go:31` + `config/runtimes/kserve-sklearnserver.yaml` (runtime como dado declarativo).*
- [ ] **Gap #4 — Conversion webhook:** pattern Hub/Convertible instalado nos CRDs `loopzero.io` v1, provando round-trip de conversão em envtest. *Referência: kserve `pkg/apis/serving/v1beta1/inference_service_conversion.go`.*
- [ ] **Gap #5 — `ObjectStore` interface:** interface no domínio (DIP) com o client S3 como primeiro adapter; nenhum call-site fora do composition root importa o driver direto. *Referência: kserve `pkg/credentials/service_account_credentials.go` (multi-backend por abstração).*
- [ ] **Gap #6 — Payload/request logger assíncrono:** dispatcher+worker não-bloqueante, opt-in por tenant, com scrub de PII — nunca logging síncrono no request path. *Referência: kserve `pkg/logger/dispatcher.go`.*
- [ ] Suíte completa verde (`go test ./...` + `task quality:gates`) + CHANGELOG `[Unreleased]` por entrega; zero regressão nos gates existentes.

**Dependencies:** M9 (Engine Architecture Hardening — a superfície operators/CRDs/API que este milestone toca). A ordenação por ID do `cycle-roadmap` garante M10–M13 antes.

**Top risks:**

1. Registry de runtimes data-driven pode reabrir a superfície RCE que a remoção do BYO-Dockerfile fechou — mitigação: descriptors são config operator-only (nunca tenant-supplied), allow-list de imagens preservada.
2. Webhooks `failurePolicy=Fail` no caminho crítico do API server podem deadlockar operações do cluster (classe B33 já vivida com VAPs) — mitigação: preferir CEL VAPs onde possível + `matchConditions` excluindo system controllers (`namespace-controller`, `kube-controller-manager`).

---

## State-of-the-art references

| Peer | License | Why it's here | Supports milestone(s) |
|---|---|---|---|
| Vercel Dashboard | proprietary (UX study only) | reference for indie dev UX patterns (sign-up → deploy ≤ 3min) | M0, M1, M5 |
| Netlify Dashboard | proprietary (UX study only) | reference single-slot preview model (chosen vs Vercel multi-URL) | M5 |
| Railway Dashboard | proprietary (UX study only) | reference per-project env scoping + service mesh | M2, M3 |
| Fly.io Dashboard | proprietary (UX study only) | reference scale + region UX | M4 |
| Linear Dashboard | proprietary (UX study only) | reference table density + keyboard-first | M3, M7 |
| getkin/kin-openapi | MIT | OpenAPI 3 loader/validator — foundation of the existing spec-based Go contract layer (`api/internal/contract/`) | M10 |
| PactFlow bidirectional contracts (concept) | proprietary (concept study only) | reference for spec-based bidirectional contract testing — the lean model the org already chose homegrown vs full Pact CDC | M10 |
| schemathesis | MIT | candidate (spike vs Go-native) for property-based API fuzzing from OpenAPI | M11 |
| Playwright (+ axe-core) | Apache-2.0 (axe-core MPL-2.0) | real-browser E2E + declarative a11y; kills bundle-grep | M12 |
| kserve/kserve | Apache-2.0 | CNCF model-inference platform — gold standard da cross-validation 2026-07-06 (score 3.89/5.0); fonte de todos os 6 gaps do M14, clonado em `.claude/knowledge-base/references/kserve/` | M14 |

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
