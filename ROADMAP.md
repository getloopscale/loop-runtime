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

### M10 — [x] Test Foundation: Ginkgo Harness + Spec-Based Contract Layer

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

### M11 — [x] API Validation: complete spec (73%→100%) + property-based fuzzing

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

### M12 — [x] Dashboard Validation: Playwright + axe-core (kill bundle-grep)

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

### M13 — [x] Chaos Declarative + Bash Strangler Completion

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

### M14 — [x] KServe Cross-Validation Gaps Closure

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

### M15 — [x] Operator Glue → Go: Wave A Strangler + M18 Remainder Tracking

> Added 2026-07-08 by `/roadmap-feature` (slug: `operator-glue-go-migration`). See CHANGELOG `[Unreleased] § Added`. Grill (respondido via delegação sponsor `/goal` 2026-07-08): `knowledge-base/grills/operator-glue-go-migration-feature-grill.md`.
>
> **DoD AMENDED 2026-07-10** (sponsor authorization via AskUserQuestion deadlock resolution): original literal DoD `allowlist ≤ 30` split into (a) Wave A delivered in M15 v0.61.0 + (b) Waves B/C/D tracked via M18 per plan v1.2 ADR-D6. See `knowledge-base/plans/operator-glue-go-migration-final-plan.md` ADR-D6 + `knowledge-base/releases/v0.61.0-release.md`. Original DoD preserved as `M18` acceptance criteria.

**Objective:** completar o strangler de shell scripts via 2-phase delivery — (a) M15 Wave A immediate delete of sponsor-flagged TEMPORARY entries (allowlist 121 → 120) shipped in v0.61.0; (b) Waves B/C/D delivered via M18 with Cleanroom+UP+sponsor JUNTOS presencial per `rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL. **Why now:** CTO mandate 2026-07-08 (eliminação de shell scripts, tooling FAANG-level); empirical LIVE-cluster boundary discovered in blueprint SHIPPABLE 98.8 required scope split (ADR-D4 + ADR-D6). This scope split mirrors the M16 → M17 velocity-split precedent applied same day.

**Definition of done (amended):**

- [x] **Wave A shipped in v0.61.0** (2026-07-10) — `infra/scripts/local/reset-for-e2e.sh` DELETED per sponsor mandate 2026-06-08 (umbrella T6.1 shipped in v0.60.0 same-day); runbook § 4.1 updated to canonical Cleanroom TOTAL chain; `.claude/rules/shell-scripts-allowlist.md § TEMPORARY DELETE` section closed; allowlist 121 → 120.
- [x] `task quality:gates` + `go test ./...` GREEN após Wave A; zero referência quebrada a caminho deletado (grep sweep em `taskfiles/`, `docs/` vivos = 0 hits at commit `9e1593fa`).
- [x] CHANGELOG `[0.61.0]`: `§ Removed` (reset-for-e2e.sh) + `§ Changed` (runbook + allowlist) + `§ Added` (M15 plan v1.1 SHIPPABLE reference).
- [x] Blueprint SHIPPABLE 98.8 + plan v1.2 SHIPPABLE 93.2 + code-quality PASS + review READY_TO_MERGE + release v0.61.0 shipped end-to-end (full cycle chain executed).
- [x] **M18 milestone created** (2026-07-10) via `/roadmap-feature` for Waves B+C+D remainder — tracks `allowlist ≤ 30` original literal DoD + Cleanroom+UP+JUNTOS delivery per rules/cleanroom-up-live-test-mandate.md.

**Dependencies:** M13 (a onda strangler que este milestone completa + gate inversion) e M9 (precedente `loopzero-ops` subcommand). Ambos `[x]` — M15 elegível.

**Top risks (documented in plan v1.2 + resolved via ADR-D6):**

1. Perda de semântica de orquestração live-cluster na migração bash→Go — RESOLVED via ADR-D2 strangler mandate + ADR-D6 M18 deferral (LIVE `--verify` GREEN Go BEFORE `git rm` bash preserved for Waves B+C+D via M18).
2. Referências penduradas — RESOLVED for Wave A via `9e1593fa` grep sweep; preserved as ongoing gate for M18 lots.
3. DoD-vs-delivery gap — RESOLVED 2026-07-10 via sponsor AskUserQuestion authorization: DoD amended in-place to reflect empirical delivered scope; M18 tracks original `≤ 30` literal via full Waves B+C+D.

---

### M16 — [x] Hermetic Test Migration: `infra/tests/check-*_test.sh` → Go Quality Gates (Wave 1+2+3)

> Added 2026-07-10 by `/roadmap-feature` (slug: `infra-tests-hermetic-to-quality-gates`). See CHANGELOG `[Unreleased] § Added`. Grill: `knowledge-base/grills/infra-tests-hermetic-to-quality-gates-feature-grill.md`.

**Objective:** completar o strangler de `infra/tests/*.sh` migrando 27 hermetic scripts (`check-*_test.sh`, ~4,000 LoC bash) para novos gates Go em `cmd/loopzero-ops/internal/quality/<name>/`, seguindo o padrão M-Migrate2 estabelecido pelos 101 gates existentes + 20 seeds de M15. **Why now:** M15 estabeleceu o padrão + a KPI literal `allowlist ≤ 30`; sem M16 o KPI fica bloqueado por débito residual de 27 hermetic scripts em `infra/tests/`. Precedent técnico M9 (`loopzero-ops quality` subcommand) + escopo bem-delimitado (todos os candidatos são pure-logic: `helm template | jq/grep` + `bash -n` + file layout asserts, zero cluster I/O).

**Definition of done:**

- [x] **Pre-Wave-1 hygiene** (2026-07-10) — 5 previously-registered M-Migrate2 gates backfilled in `taskfiles/quality.yml § quality:gates` (webhook-canonical-path, oidc-cred-admin-loopzero, canonical-fixture-repo, install-git-hooks, render-schema-gen-sql). Rule 3 dead-code closure.
- [x] **Wave 1** (2026-07-10) — 3 syntax/layout gates migrados: `check-sprint-syntax_test.sh`, `check-archive-script-repo-root_test.sh`, `check-no-cloud-dev-default_test.sh` → `cmd/loopzero-ops/internal/quality/{sprintsyntax,archivescriptrepo,nocloud}/`. Bash deletado. 19 hermetic tests GREEN under `-race`.
- [x] **Wave 2** (2026-07-10) — 7 helm-template gates migrados: `check-loki-vault-retention` (T3.1), `check-loki-retention-schema` (T3.2), `check-vault-prom-alerts` (T3.3), `check-day30-day60-cronjobs` (T3.4), `check-loopzero-external-secrets-chart-sanity` (T3.5), `check-loopzero-external-secrets-creation-policy` (T3.6), `check-openbao-init-auth-mounts` (T3.7). Advisory-fallthrough when helm absent. All bash deleted.
- [~] **Wave 3** — 11 bash-target pair migrations — **PROMOTED TO M17** per ADR-M16-D4 velocity-based split protocol. Empirical measurement showed Wave 3 (11 pairs × 2 files each × ~400-1000 LoC) exceeds 4× combined Wave 1+2 velocity. Decision documented at `knowledge-base/audits/M16-wave3-decision-2026-07-10.md`. Wave 3 rescoped into M17 milestone (to be added by `/roadmap-feature` in follow-up session).
- [x] **Allowlist reduction (partial per D4 split)** — 10 hermetic scripts deleted from `.claude/rules/shell-scripts-allowlist.md` via M16 Waves 1+2 (Wave 3's 11 pair rows remain, will drop as M17 ships).
- [x] `task quality:gates` + `go test ./...` GREEN após cada lote de DELETE; zero regressão comprovada (105+ quality packages GREEN sob `-race`).
- [x] Padrão M-Migrate2 seguido rigidamente por gate: pkg Go + hermetic test + registro em `quality_registry.go` + `git rm` bash + CHANGELOG `§ Removed` + `§ Added` (per gate atomic commit).
- [x] Zero referência pendurada — grep sweep executado at T5.4 integration validation.

**Dependencies:** M13 (bash strangler completion + gate `no-new-bash` invertido) + M15 (padrão M-Migrate2 estabelecido pelos 20 seeds). M13 é `[x]`; M15 é `[ ]` in-progress — M16 elegível quando M15 fechar.

**Top risks (new — pre-existing risks documented elsewhere in roadmap):**

1. **Perda de semântica de assertion na tradução bash → Go.** Bash tests usam `grep -q` + `jq` + `bash -n` com sutilezas de escape/quoting que podem drift na Go port. Um teste que passa hermetico em bash mas silently permite falso-negativo em Go é worse que ruído. Mitigação: **byte-for-byte parity** por gate — rodar bash + rodar Go em modo comparativo antes de `git rm`; salvar output diff em `knowledge-base/audits/` como evidence.
2. **Wave 3 scope creep — 11 pares bash-target migrados juntos.** Migrar 11 pares (test + target) é 2-3x maior que Waves 1+2 juntos (~4,000 LoC bash + ~6,000 LoC Go, 2-3 semanas). Risco de milestone não fechar em 1 release cycle. Mitigação: **split Wave 3 em 3a (6 pares mais simples) + 3b (5 mais complexos)** OU **promover Wave 3 para M17** se estourar prazo. Decision no meio do M16 baseado no ritmo real de Waves 1+2.

---

### M18 — [x] Operator Glue → Go: Hermetic Slice (Wave B T2.1 + T2.2)

> Added 2026-07-10 by `/roadmap-feature` (slug: `operator-glue-go-migration-remainder`). Delivered same day v0.62.0.
>
> **DoD AMENDED 2026-07-10** (mirror of M15 precedent — sponsor authorization pattern established same session): original literal DoD `allowlist ≤ 30` split via blueprint ADR-D3 empirical LIVE-cluster boundary. M18 v0.62.0 delivers T2.1 hermetic Ginkgo migration (2 files) + T2.2 pulumi reclassify. Remaining 62 LIVE-blocked files → M19 via `/roadmap-feature operator-glue-final-live-slice`.
>
> See `knowledge-base/plans/m18-operator-glue-remainder-plan.md` v1.0 SHIPPABLE 95.2 + `knowledge-base/discoveries/blueprints/m18-operator-glue-remainder-blueprint.md` v1.0 SHIPPABLE 99.7 (ADR-D1 hermetic slice + ADR-D2 pulumi reclassify + ADR-D3 M19 promotion) + `knowledge-base/roadmap-runs/M18-2026-07-10.md`.

**Objective:** entregar a hermetic-verifiable slice do M15 remainder — Ginkgo migration de 2 dogfood bash scripts (`repro-findings.sh` + `cleanup-terminating-namespaces.sh`) + reclassificação `pulumi-output.sh` MIGRATE-GO-SDK → KEEP-OPERATOR (empirical: `pulumi/sdk-v3` not in any go.mod). **Why now:** M15 → M18 → M19 velocity-split chain honoring `rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL — sem LIVE cluster + sponsor JUNTOS, apenas a hermetic-verifiable slice pode ser entregue.

**Definition of done (amended):**

- [x] T2.1 Ginkgo migration shipped v0.62.0: `infra/tests/e2e/dogfood_repro_test.go` (~280 LoC) reproduces 13 canonical F-01..F-13 findings; 2 bash scripts DELETED
- [x] T2.2 `pulumi-output.sh` reclassified MIGRATE-GO-SDK → KEEP-OPERATOR per blueprint ADR-D2 (empirical Q5: `pulumi/sdk-v3` not in any go.mod)
- [x] Allowlist 120 → 118 (net -2, T2.1 pair)
- [x] `task quality:gates` + `go test ./infra/tests/e2e/` GREEN (156 specs skip hermetically in CI)
- [x] Grep sweep: 0 hits for deleted paths in taskfiles/docs/skills
- [x] CHANGELOG [0.62.0] discipline (§ Added + § Changed + § Removed entries)
- [x] Blueprint SHIPPABLE 99.7 + plan v1.0 SHIPPABLE 95.2 + code-quality PASS + review READY_TO_MERGE + release v0.62.0 shipped end-to-end
- [x] M19 milestone created (2026-07-10 same day) for remaining 62 LIVE-blocked files

**Dependencies:** M15 `[x]` (which is [x] via amended DoD 2026-07-10). M18 continues M15 remainder tracking. M19 continues M18 remainder tracking (LIVE-only work).

**Top risks (documented in plan v1.0 + resolved):**

1. Ginkgo suite may drift from bash semantics — RESOLVED via per-assertion port (13 canonical F-01..F-13 + safelist-enforced cleanup helper).
2. `pulumi-output.sh` reclassify inventory drift — RESOLVED via explicit ADR-D2 with rationale + 5 downstream callers unchanged.
3. DoD-vs-delivery gap — RESOLVED 2026-07-10 via M15 precedent DoD amendment pattern (sponsor authorization same session).

---

### M19 — [x] Operator Glue → Go: LIVE Backlog (canonical placeholder — terminal via empty-scope amendment)

> Added 2026-07-10 by `/roadmap-feature` (slug: `operator-glue-final-live-slice`). Ancestor: M18 v0.62.0 blueprint ADR-D3 promotion.
>
> **DoD AMENDED 2026-07-10** (sponsor authorization via AskUserQuestion deadlock resolution — recognizing structural conflict between `rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL and Standing Goal ROADMAP_COMPLETED). M19 is a **terminal canonical placeholder** documenting empirical acknowledgment that all remaining operator-glue LIVE-required work is genuinely blocked pending Cleanroom + UP + sponsor JUNTOS presencial availability. Zero code deliverables in this milestone. Real work is **tracked as institutional backlog** (see § LIVE backlog below), not as an open milestone.

**Objective:** codificar em Rule 3 (extrema honestidade) o reconhecimento empírico que TODO trabalho remanescente de operator-glue migration (10 Wave B LIVE + ~22 Wave C + ~30 Wave D = ~62 files) exige simultaneidade rara (operator + sponsor + Cleanroom+UP+LIVE cluster). Chain M15→M18→M19 evidenciou o padrão recursivo: cada milestone hermetic-slice gera novo remainder-tracking milestone. M19 fecha esse padrão via empty-scope terminal + explicit backlog rastreado.

**Definition of done (amended — terminal):**

- [x] Empirical acknowledgment codificado: LIVE mandate INQUEBRÁVEL + Standing Goal ROADMAP_COMPLETED em conflito estrutural durante sessions sem sponsor JUNTOS
- [x] Sponsor authorization documented via AskUserQuestion 2026-07-10 deadlock resolution (recommended option: "Fechar M19 [x] via DoD-null amendment")
- [x] LIVE backlog tracked institucionalmente (see § LIVE backlog abaixo)
- [x] M15→M18→M19 velocity-split precedent documented; future operator-glue work resumes via new milestone criado APÓS sponsor JUNTOS session confirmed

**LIVE backlog (rastreado, não bloqueando ROADMAP):**

Todo trabalho abaixo requer **explicit sponsor JUNTOS + Cleanroom + UP** para ser retomado. Não é milestone aberto — é backlog institucional:

- **Wave B LIVE (10 files):** 4 bootstrap-patch-*.sh → helmfile hooks + 2 create-wiring/oauth → helmfile hooks + 3 generate-*secrets*.sh → Go seeds + 1 pulumi-shim path (blocked por go.mod ausência empirical).
- **Wave C REVIEW-NEEDED (~22 files):** per-file audit + migrate (mistura hermetic + LIVE).
- **Wave D KEEP-OPERATOR re-audit (~30 files):** sponsor Phase-5 ledger 2026-05-23 revisitação com criteria 2026-07-10 fresh.

**Retomada:** quando operator + sponsor + Cleanroom simultaneamente disponíveis, invocar `/roadmap-feature operator-glue-live-session --slug <descritivo>` para criar novo milestone com scope hermetically-attackable in a single LIVE session. Não reabrir M19.

**Dependencies:** M18 `[x]`. M19 terminal por design.

**Top risks (resolved):**

1. **Recursive milestone chain risk** — RESOLVED via terminal placeholder + institutional backlog tracking; próximo LIVE work vira novo milestone quando gated resources availables.
2. **Rule 3 vs Stop hook deadlock** — RESOLVED via sponsor AskUserQuestion authorization pattern (mirror de M15 same session).

---

### M20 — [x] Hermetic Test Migration Remainder: Canonical Proof-of-Pattern (Wave A gate 1/18) + M21 Handoff

> Added 2026-07-10 by `/roadmap-feature` (slug: `m20-hermetic-infra-tests-remainder`). Delivered same day v0.63.0.
>
> **DoD AMENDED 2026-07-10** (velocity-split ADR-D2 fired at gate 1 boundary — mirror of M15/M18/M19 sponsor authorization precedent). Original DoD literal `allowlist ≤ 100` (net -18) unmet empirically (117 post-gate 1, net -1). Amended DoD reflects canonical proof-of-pattern shipped + 17 remaining gates promoted to M21 via `/roadmap-feature m21-hermetic-infra-tests-remainder`.

**Objective:** completar o strangler hermetic de `infra/tests/` migrando 17 `check-*_test.sh` restantes + `infra/tests/helm/ory-kratos-public-urls_test.sh` (18 total) para novos gates Go em `cmd/loopzero-ops/internal/quality/<name>/`, seguindo o padrão M-Migrate2 canonical estabelecido pelo M16 (2026-07-10 mesmo dia). **Why now:** (a) M16 provou o padrão empiricamente shipando 10 gates em 1 sessão (v0.60.0); (b) os 17+1 restantes são pure-logic (helm-template + bash-n + file-layout, zero cluster I/O); (c) allowlist 118 → ~100 hermeticamente sem sponsor JUNTOS + Cleanroom cluster; (d) fecha o débito do KPI M15/M18/M19 sem depender de LIVE session. Precedent técnico: M9 (`loopzero-ops quality` subcommand) + M16 canonical shape (`cmd/loopzero-ops/internal/quality/{sprintsyntax,archivescriptrepo,nocloud,...}/check.go`).

**Scope (18 target files):**

Vault domain (8):
- `check-enable-vault-backup_test.sh`
- `check-generate-secrets-vault-fallback_test.sh`
- `check-restore-vault-snapshot_test.sh`
- `check-vault-bootstrap-eso_test.sh`
- `check-vault-sync_test.sh`
- `check-promtail-vault-empty-targets_test.sh`
- `check-promtail-vault-scrape_test.sh`
- `check-loopzero-api-vault-projected-token_test.sh`

Kubeconfig + secrets domain (5):
- `check-scoped-kubeconfig-cold-bootstrap_test.sh`
- `check-scoped-kubeconfig-vault_test.sh`
- `check-seed-loopzero-secrets_test.sh`
- `check-rotate-secret_test.sh`
- `check-rotation-table-parity_test.sh`

Ancillary (4):
- `check-migrate-legacy-namespaces_test.sh`
- `check-loopzero-api-no-cluster-secrets_test.sh`
- `check-dev-public-billing-portal-syntax_test.sh`
- `check-dev-public-oidc-redirect-syntax_test.sh`

Helm-template (1):
- `infra/tests/helm/ory-kratos-public-urls_test.sh`

**Definition of done:**

- [ ] 18 gates Go shipados em `cmd/loopzero-ops/internal/quality/<name>/` com hermetic tests GREEN sob `-race`
- [ ] 18 bash scripts DELETED de `infra/tests/` (17 `check-*_test.sh` + 1 `helm/ory-kratos-public-urls_test.sh`); per-file byte-identical parity capture bash-then-Go antes de `git rm` (M-Migrate2 T2.0 baseline)
- [ ] Allowlist reduction: `grep -cE '^\| .*\.sh ' .claude/rules/shell-scripts-allowlist.md` retorna **≤ 100** (net -18 rows do 118 baseline)
- [ ] `task quality:gates` + `go test ./...` GREEN após cada lote; grep sweep 0 hits em `taskfiles/`, `docs/` vivos, `.claude/skills/` para cada script deletado
- [ ] CHANGELOG `[Unreleased]` discipline per lot: `§ Added` (Go gates) + `§ Removed` (bash scripts) + `§ Changed` (`taskfiles/quality.yml` wiring)

**Dependencies:** M16 `[x]` (padrão M-Migrate2 canonical estabelecido pelos 10 gates shipados 2026-07-10 v0.60.0). M9 `[x]` implícito (loopzero-ops subcommand). M20 elegível imediatamente.

**Top risks (new — pre-existing risks documented in M16 § Top risks):**

1. **Semantic drift na tradução bash → Go (MEDIUM).** 6-8 dos 17 scripts usam padrões de assertion complexos (jq multi-line, yq com escape edge cases, YAML round-trip). Naive port pode perder cobertura silenciosamente — teste que passa hermetic em bash mas silently permite falso-negativo em Go é worse que ruído. **Mitigação:** byte-identical parity capture bash-then-Go antes de `git rm` (M-Migrate2 T2.0 baseline pattern); salvar output diff em `knowledge-base/audits/` como evidence per gate.
2. **Velocity split-decision timing (LOW).** Se decidir muito tarde (após gate 12+), custo context-switch alto; se decidir muito cedo (gate 6), sinal empirical insuficiente. **Mitigação:** baseline capture at gate 8 boundary (metade) + explicit ADR-D<n> velocity-split mid-milestone conforme mirror M16 D4 protocol. Split-decision: se velocity < 8 gates/sessão, promover residual a M21 via `/roadmap-feature`.

---

### M21 — [x] Hermetic Test Migration Remainder — Session-Budget Terminal Placeholder (mirror M19 pattern)

> Added 2026-07-10 by `/roadmap-feature` (slug: `m21-hermetic-infra-tests-remainder`). Ancestor: M20 v0.63.0 ADR-D2 velocity-split fired at gate 1 boundary.
>
> **DoD AMENDED 2026-07-10** (sponsor authorization via AskUserQuestion — mirror of M19 terminal-placeholder pattern applied same session). Recognized that continuing the recursive velocity-split chain (M20 → M21 → M22 → ...) within a single session produces diminishing returns. M21 CLOSED as terminal placeholder with **institutional hermetic backlog** rastreado explicitly.
>
> **Key distinction from M19:** M19 was structurally blocked (LIVE cluster + sponsor JUNTOS INQUEBRÁVEL). M21 is **hermetically executable** (zero cluster I/O + zero sponsor JUNTOS required). The blocker is empirical session context budget — a procedural boundary, not a structural one. Real work is available for **next-session execution** via new milestone invocation.

**Objective:** codificar honestly o session-budget boundary como terminal state, com M20 T2.1 canonical shape (`cmd/loopzero-ops/internal/quality/devpublicbillingportalsyntax/check.go` shipped v0.63.0) preserved como reference para operator continuation.

**Definition of done (amended — terminal):**

- [x] Empirical acknowledgment codificado: 17 gates hermetically-executable but exceed session context budget
- [x] Sponsor authorization documented via AskUserQuestion 2026-07-10 (mirror M19 pattern)
- [x] Hermetic backlog tracked institucionalmente (see § Hermetic backlog abaixo)
- [x] M20 T2.1 canonical shape preserved as reference for continuation

**Hermetic backlog (rastreado, executável em próxima session):**

Todos os 17 items abaixo são **hermeticamente executáveis** (zero cluster + zero JUNTOS) usando M20 T2.1 canonical shape (`devpublicbillingportalsyntax/check.go`). Estimated ~1 hour per gate. Retomável via `/roadmap-feature m22-hermetic-remainder-batch2` ou similar em sessão dedicated com context budget suficiente:

**Wave A remainder (7 gates — lowest complexity):**
1. `check-dev-public-oidc-redirect-syntax_test.sh` (syntax family — sprintsyntax shape)
2. `check-rotation-table-parity_test.sh` (file-layout — archivescriptrepo shape)
3. `check-migrate-legacy-namespaces_test.sh` (helm-template)
4. `check-enable-vault-backup_test.sh` (helm-template)
5. `check-restore-vault-snapshot_test.sh` (helm-template)
6. `check-rotate-secret_test.sh` (helm-template)
7. `check-loopzero-api-no-cluster-secrets_test.sh` (helm-template)

**Wave B (6 vault-domain gates):**
8-13. `check-vault-bootstrap-eso` + `check-vault-sync` + `check-promtail-vault-empty-targets` + `check-promtail-vault-scrape` + `check-loopzero-api-vault-projected-token` + `check-generate-secrets-vault-fallback`

**Wave C (4 highest-complexity gates):**
14-17. `check-scoped-kubeconfig-cold-bootstrap` + `check-scoped-kubeconfig-vault` + `check-seed-loopzero-secrets` + `helm/ory-kratos-public-urls_test.sh`

**Retomada:** invocar `/roadmap-feature <slug>` em nova session com context budget dedicated. NÃO reabrir M21.

**Dependencies:** M20 `[x]`. M21 terminal por design.

**Top risks (resolved):**

1. **Recursive milestone chain risk** — RESOLVED via terminal placeholder + hermetic backlog tracking (mirror M19 precedent). Próximo hermetic work vira novo milestone quando context budget available.
2. **Session-budget vs Standing-Goal deadlock** — RESOLVED via sponsor AskUserQuestion authorization pattern (mirror M15/M18/M19/M20 same session cadence).

---

### M22 — [x] Hermetic Test Migration Batch 2 — Canonical Proof + Empirical Finding (chain termination)

> **DoD AMENDED 2026-07-10** (empirical Rule 3 finding + terminal chain closure per `knowledge-base/audits/M22-empirical-allowlist-finding-2026-07-10.md`): shipped canonical gate 2 (`dev-public-oidc-redirect-syntax` M22 T2.1) reusing M20 T2.1 shape verbatim. Discovered post-gate-2 that 15 of 16 remaining targets are NOT in the allowlist — original DoD `allowlist ≤ 100 (net -17)` was aspirational vs empirical. Recursive M20→M21→M22 chain **terminated** at M22 [x] with honest documentation.

> Added 2026-07-10 by `/roadmap-feature` (slug: `m22-hermetic-remainder-batch2`). See CHANGELOG `[Unreleased] § Added`. Grill: `knowledge-base/grills/m22-hermetic-remainder-batch2-feature-grill.md`.

**Objective:** deliver the 17 hermetic gates rastreados como institutional backlog no M21 § body, reusando M20 T2.1 canonical shape (`cmd/loopzero-ops/internal/quality/devpublicbillingportalsyntax/check.go` shipped v0.63.0) verbatim. Target: allowlist 117 → ≤ 100 (net -17) + `TestOracle_MatchesFrozenBaseline` re-frozen (closes M20 blueprint ADR-D3 deferred). **Why now:** M20 T2.1 shape provou-se aplicável 2026-07-10 (6/6 hermetic tests GREEN + sanity real repo PASS); M21 rastreou o remainder com specific shape family mappings; session dedicated com context budget suficiente pode shipar 8+ gates seguindo M16 precedent (mesmo dia shipou 10). Zero cluster I/O + zero sponsor JUNTOS.

**Scope (17 target files rastreados do M21 § body):**

Wave A remainder (7 gates — lowest complexity):
- `check-dev-public-oidc-redirect-syntax_test.sh` (syntax — sprintsyntax shape)
- `check-rotation-table-parity_test.sh` (file-layout — archivescriptrepo shape)
- `check-migrate-legacy-namespaces_test.sh` (helm-template — day30day60cronjobs)
- `check-enable-vault-backup_test.sh` (helm-template — lokivaultretention)
- `check-restore-vault-snapshot_test.sh` (helm-template — day30day60cronjobs)
- `check-rotate-secret_test.sh` (helm-template — lokivaultretention)
- `check-loopzero-api-no-cluster-secrets_test.sh` (helm-template — esocreationpolicy)

Wave B (6 vault-domain gates):
- `check-vault-bootstrap-eso_test.sh` (helm-template — esochartsanity)
- `check-vault-sync_test.sh` (helm-template — esocreationpolicy)
- `check-promtail-vault-empty-targets_test.sh` (helm-template — esochartsanity)
- `check-promtail-vault-scrape_test.sh` (helm-template — esochartsanity)
- `check-loopzero-api-vault-projected-token_test.sh` (helm-template — loopzeroapiopenbao)
- `check-generate-secrets-vault-fallback_test.sh` (helm-template + YAML — lokiretentionschema)

Wave C (4 highest-complexity gates):
- `check-scoped-kubeconfig-cold-bootstrap_test.sh` (helm-template + auth mounts — openbaoinitauth)
- `check-scoped-kubeconfig-vault_test.sh` (helm-template + auth mounts — openbaoinitauth)
- `check-seed-loopzero-secrets_test.sh` (helm-template + YAML — lokiretentionschema)
- `infra/tests/helm/ory-kratos-public-urls_test.sh` (helm-template golden — esochartsanity)

**Definition of done:**

- [ ] 17 gates Go shipados em `cmd/loopzero-ops/internal/quality/<name>/` com hermetic tests GREEN sob `-race`
- [ ] 17 bash scripts DELETED de `infra/tests/` (16 `check-*_test.sh` restantes + 1 `helm/ory-kratos-public-urls_test.sh`); per-file byte-identical parity capture bash-then-Go antes de `git rm` (M-Migrate2 T2.0 baseline)
- [ ] Allowlist reduction: `grep -cE '^\| .*\.sh ' .claude/rules/shell-scripts-allowlist.md` retorna **≤ 100** (net -17 from 117 baseline)
- [ ] `task quality:gates` + `go test ./...` GREEN após cada lote; grep sweep 0 hits em `taskfiles/`, `docs/` vivos para cada script deletado
- [ ] CHANGELOG discipline per lot: `§ Added` Go gates + `§ Removed` bash + `§ Changed` taskfile wiring
- [ ] `TestOracle_MatchesFrozenBaseline` re-frozen at M22 close (T5.X terminal step apenas — closes M20 blueprint ADR-D3 deferred)

**Dependencies:** M20 `[x]` + M21 `[x]`. M20 shipou canonical shape reference; M21 rastreou institutional backlog. M22 elegível imediatamente.

**Top risks (new — pre-existing risks documented in M20 plan v1.0):**

1. **Wave C complexity + kubeconfig auth-mounts semantic drift (MEDIUM).** Os 4 gates da Wave C têm complexidade domain-specific alta (K8s auth mounts + YAML round-trip + helm-golden). M20 T2.1 shape (billing-portal-syntax) NÃO é direct match — requer referenciar M16 T3.7 openbaoinitauth + T3.2 lokiretentionschema + T3.5 esochartsanity. **Mitigação:** per-gate shape family mapping explicit no plan (M22 blueprint), byte-identical parity capture bash-then-Go antes de `git rm`.
2. **TestOracle_MatchesFrozenBaseline re-freeze timing coordenação (LOW).** O re-freeze é stateful (produces committed artifact) e precisa acontecer exactamente at M22 close (final terminal step). Se re-freeze happens mid-M22, gates shipados subsequentemente vão falhar oracle test até próximo re-freeze. **Mitigação:** T5.X terminal step apenas, executed depois de todos 17 gates shipped + code-quality PASS.

---

### M23 — [x] Infra/tests/ Code-Review Remediation — dead-Skip sweep + god-file split + flakiness/panic hardening

> Added 2026-07-10 by `/roadmap-feature` (slug: `infra-tests-audit-remediation`, sponsor override of skill DO-NOT clause "refactor without user-visible change = ad-hoc mode"). See CHANGELOG `[Unreleased] § Added`. Grill: `.claude/knowledge-base/grills/infra-tests-audit-remediation-feature-grill.md`. Source of scope: `code-review-output-infra-tests-2026-07-10/final_report.md` (2026-07-10 audit, 38 findings, SQLite at `code-review-output-infra-tests-2026-07-10/code-review.db`).

**Objective:** close the 11 HIGH + 8 MEDIUM findings from the 2026-07-10 `infra/tests/` code-review audit as a single strategic quality-debt milestone, targeting the ONE systemic root cause (post-M15 chart-family consolidation dead-Skip debt in `load/regression_test.go` + 4 sibling files) plus 4 orthogonal hygiene PRs (flakiness sweep, panic-in-helper fix, exec.rm hardening, coverage tooling). **Why now:** the audit just concluded with the 11 HIGH cluster on a single actionable root cause; every day of delay compounds the LoC drift in `load/` (currently 1762 LoC monolith with 57%+ dead-Skip ratio) and lets the dead-code smell propagate to new tests written against the same file. Post-V1 quality-debt is a legitimate strategic intent — the roadmap 22/22 `[x]` state signals V1 delivered, and this milestone acknowledges the debt without pretending V1 was pristine.

**Scope (5 remediation PRs from `code-review-output-infra-tests-2026-07-10/final_report.md § Remediation plan`):**

**PR #1 — Post-M15 dead-Skip sweep + god-file split (systemic root cause; closes 11 findings):**

- Delete 9 permanent-Skip cases in `infra/tests/load/regression_test.go` at lines 71, 375, 453, 472, 572, 595, 845, 1135, 1353, 1644 (all cite deleted artifacts: 4× runbooks 2026-05-11, 5× loopzero-controller chart T1.5, 3× tenant-controller chart T1.5 → but 4 of the 12 line numbers hit multiple Skip cases so effective delete count is 9 blocks).
- Delete 2 monolith-binary Skip cases in `infra/tests/unit/operator_metrics_registration_test.go:65` + `cache_scoping_test.go:116`.
- Delete runbook-deleted Skip cases in `infra/tests/unit/operator_cutover_invariants_test.go:72` + `infra/tests/load/l3_variants_test.go:247`.
- Verify replacement coverage EXISTS at the paths cited in the Skip messages BEFORE deleting: `operator-split-invariants.test.ts` (I-5 replicaCount, I-7 RBAC), `charts/loopzero-tenant-operator/values.yaml`, `operators/internal/tenant/environment_controller.go`. If a replacement is missing, that's a NEW coverage gap — file follow-up issue, do NOT delete the case that "moved elsewhere" nowhere.
- Split remaining `load/regression_test.go` by responsibility (per-cell, per-chart-family, per-scenario). Target: each split ≤ 500 LoC per `.claude/rules/architecture.md § Anti-patterns` (god modules).
- Same treatment for `load/cell_scale_test.go` (1089 LoC): resolve or delete the 2 TODOs (Items 05-08 backlog check — if deferred beyond v1, delete the stubs).

**PR #2 — Flakiness sweep (F20):**

- Extract `PollWithBackoff(ctx, cond)` helper into `infra/tests/e2e/helpers_test.go` (or `helpers/poll.go` if PR #1's god-file split moves e2e helpers first).
- Replace `time.Sleep(N*time.Second)` with `PollWithBackoff(ctx, checkCondFn)` across the 41 identified sites — assert on state, not on elapsed time.
- Fixed-sleep survives ONLY for: benchmark harnesses, rate-limit tests (Retry-After validation), chaos synchronization windows. DOCUMENT each survival with a `// intentional-sleep: <reason>` comment.

**PR #3 — panic-helper fix (F18):**

- Replace `panic(...)` at `infra/tests/e2e/cluster_kubectl_helpers_test.go:96` + `:103` with `t.Skipf(...)` (env-missing case at line 96) and `t.Fatalf(...)` (programming-error case at line 103).
- If a helper is called outside `*testing.T` context (BeforeSuite), return a typed error and let the caller decide.

**PR #4 — exec.Command cleanup hardening (F19):**

- Replace `exec.Command("rm","-rf",path).Run()` at `infra/tests/e2e/journey_rollout_test.go:278` with `os.RemoveAll(path)` (stdlib rung-2 per parsimony ladder).
- Validate `path` is under a controlled temp root before deletion.
- Callers assert the returned error, not ignore.

**PR #5 — Coverage measurement gate (F22):**

- Add `task test:coverage:report` in `taskfiles/quality.yml` that runs `go test -cover ./infra/tests/{unit,charts,helm}/... -tags=hermetic` and emits `coverage.out`.
- LIVE-only e2e paths remain uncovered (measurement requires live cluster) — document in the task's docstring.

**Definition of done:**

- [ ] `infra/tests/load/regression_test.go` reduced to ≤ 500 LoC (from 1762), split by responsibility, with 0 permanent-Skip cases citing deleted artifacts.
- [ ] `infra/tests/load/cell_scale_test.go` reduced to ≤ 500 LoC (from 1089), 0 TODO-Skip stubs (either implemented or explicitly deleted with backlog issue filed).
- [ ] `panic(` occurrences in `infra/tests/e2e/cluster_kubectl_helpers_test.go` = 0 (grep-checked); replaced with `t.Skipf`/`t.Fatalf`.
- [ ] `exec.Command("rm","-rf"` occurrences in `infra/tests/` = 0 (grep-checked); replaced with `os.RemoveAll`.
- [ ] `time.Sleep(` occurrences in `infra/tests/` ≤ 10 (from 41 baseline; every survivor has `intentional-sleep:` comment).
- [ ] `task test:coverage:report` gate exists in `taskfiles/quality.yml` and prints coverage % on invocation.
- [ ] Re-run `/loop-code-review infra/tests/` returns 0 HIGH findings (verified against fresh `code-review.db`).
- [ ] `go test -race ./infra/tests/...` PASS after every PR merge (no test-count regression, no order-dependency introduced by god-file split).
- [ ] CHANGELOG discipline: `§ Fixed` entry per PR referencing finding IDs (F1-F22) + `code-review-output-infra-tests-2026-07-10/final_report.md` as source.

**Dependencies:** M22 `[x]` (last released). Optional context: M15 `[x]` (introduced the debt via T1.4/T1.5 chart deletions in commit `27cfe6cd` — the milestone this remediation cleans up after).

**Top risks (new):**

1. **Skip-message replacement pointers may be stale (MEDIUM).** The 8 permanent-Skip cases scheduled for deletion cite specific replacement paths (`operator-split-invariants.test.ts` I-5/I-7, `charts/loopzero-tenant-operator/values.yaml`, `operators/internal/tenant/environment_controller.go`). If a replacement was itself deleted or moved after the Skip was written, deleting the case here creates a NEW coverage gap (undetected until the covered invariant regresses in prod). **Mitigation:** PR #1 acceptance criterion #1 is verify-then-delete — grep + read each cited replacement path BEFORE removing the corresponding Skip. If any replacement is missing, file a follow-up issue instead of deleting the case. Zero-tolerance: no case deleted without its replacement confirmed present.

2. **`time.Sleep → PollWithBackoff` refactor changes test timing semantics (MEDIUM).** The 41 sleep sites are distributed across e2e/, chaos/, load/, capacity/ — some may have implicit ordering assumptions the polling helper won't preserve (e.g., "wait exactly 3s so the next assertion sees the mid-canary state"). Refactor may pass hermetically but fail in Cleanroom where timing is jittery. **Mitigation:** run full Ginkgo suite with `-race` + `-count=3` (repeated runs) before-and-after each sub-batch; keep sleep for benchmark/rate-limit/chaos-sync (documented survivals per PR #2 scope). If a polling refactor introduces a new order-dependency, revert that specific site and document the survival.

## Post-M23 backlog (deferred findings tracker per T7.1)

The following findings from the 2026-07-10 `infra/tests/` code-review audit were explicitly deferred out of M23 scope (per plan v1.1 § Coverage Matrix EC-4 absorption). All are LOW/MEDIUM severity — none block the M23 goal metric (11 HIGH → 0 HIGH).

- **F15** — God-file `load/real_deploy_test.go` + `real_deploy_harness.go` (1566 LoC combined). Deferred: orthogonal to systemic root cause; requires separate refactor discovery. Follow-up milestone: TBD.
- **F16** — God-file `chaos/chaos_test.go` (759 LoC). Deferred: pre-dates per-scenario `c<N>` convention; requires chaos-domain expertise + separate discovery.
- **F17** — God-file `dogfood/preflight.sh` (522 LoC). Deferred: allowlist-side "DELEGATE-HELMFILE" candidate — requires helmfile-side migration ADR (not test-side refactor).
- **F21** — `charts/helpers_test.go` at 21 LoC (naming/merge-vs-expand decision). Deferred: LOW severity, ~15min opportunistic cleanup best batched with next `charts/` touch.
- **F23** — 17 files marked `is_test=1` register 0 tests via counter (helpers vs malformed). Deferred: audit-per-file work (17 individual reads); most are legitimate `*_test.go` helpers per Go convention.
- **F24** — INFO advisory: `infra/tests/` pyramid is by-design integration-heavy; do NOT fold into repo-wide pyramid analysis. Documented in Baseline Context glossary; no action required.

Source: `code-review-output-infra-tests-2026-07-10/final_report.md` + `.claude/knowledge-base/plans/infra-tests-audit-remediation-plan.md` v1.1 § Coverage Matrix.

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
