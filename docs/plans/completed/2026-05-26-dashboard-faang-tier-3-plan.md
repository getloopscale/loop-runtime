---
type: plan
title: Dashboard FAANG Refactor Plan
status: deprecated
---

# Dashboard FAANG Refactor Plan

> **Version 1.2** — Refactor incremental do `cloud/dashboard/` em **6 phases (Phase 0 backend pre-flight + Phase 1-3 tier 3 differentiation + Phase 4 tier 4 personalization + Phase 5 FAANG-grade infra: Web Vitals + a11y CI + Storybook + bundle analyzer)** para atingir UX-tier "FAANG" alinhado a concorrentes top (Vercel/Linear/Stripe) com signature distintivo (LoopZero's own voice: CLI-first amplification + Vercel density + Linear keyboard). Cobre 25 findings documentados (2 P1 + 5 P2 + 4 P3 density + 8 V-gaps Vercel + 6 slop fails) + 23 edge cases incorporados + **15 FAANG best practices gaps (5 CRITICAL + 4 HIGH + 4 MEDIUM + 2 LOW)** identificados na revisão v1.2 (reverte ADR D3 skip-Tier-4, adiciona ADRs D7-D9 para Web Vitals / Observability / i18n primitives). Pre-release context. **STATUS: PROPOSED v1.2 — T3 governance gate pending sponsor approval + ADR D1-D9 ratification.**

## Source-of-truth references (consult ANTES de assumir API)

- [`docs/reviews/density-audit-2026-05-25/REPORT.md`](../../reviews/density-audit-2026-05-25/REPORT.md) — density audit DSS-based (2 P1 + 5 P2 + 4 P3 violations medidas em 13/13 rotas)
- [`docs/reviews/density-audit-2026-05-25/vercel-benchmark.md`](../../reviews/density-audit-2026-05-25/vercel-benchmark.md) — comparação 1:1 com Vercel real (8 V-gaps + validação de P1s)
- [`docs/design/2026-05-25-loopzero-dashboard-design-system.md`](../../design/2026-05-25-loopzero-dashboard-design-system.md) — DSS v1.0 (Violet Forge anchor, § 6.1 density targets, CC-3 empty state limit)
- `cloud/dashboard/.llms.txt` — @loopzero/ui canonical component spec (v0.11.0-next.0 → upgrade-target 0.12.0-next.0)
- `CLAUDE.md` Voice/Tone section + Cross-Project Rule 9 — CLI-first product abstraction
- `.claude/rules/pre-release-governance.md` — T3 classification + Plan Amendment Policy
- [`docs/handoff/2026-05-25-loopzero-ui-cloud-dashboard-brief-5.md`](../../handoff/2026-05-25-loopzero-ui-cloud-dashboard-brief-5.md) + `brief-6.md` — Brief #5 primitives consumption status
- [`docs/plans/2026-05-26-dashboard-faang-tier-3-edge-cases.md`](./2026-05-26-dashboard-faang-tier-3-edge-cases.md) — /edge-case-plan review (23 ECs) cujos findings foram incorporados nesta v1.1

## Changelog

- **v1.0 (2026-05-26):** Initial PROPOSED draft consolidando inputs de 3 audits (DSS density, Vercel benchmark, loopzero-ui slop test) + decisões estratégicas (mirror=own-voice, escopo=incremental-3-phase, tier=3-differentiation).
- **v1.1 (2026-05-26):** Incorpora 23 edge cases do /edge-case-plan review. Adiciona Phase 0 (5 backend pre-flight tasks) resolvendo backend-gap sistêmico. 6 MUST FIX viram sub-tasks explícitas (.1/.2) em T-DR-2.1/2.7/3.1/3.2/3.5/3.8. 7 SHOULD TEST entram como testes obrigatórios no TDD. 7 DOCUMENT viram seção "Known risks (documented)". Timing estimate +3-5 dias para Phase 0 → total 4.5-6.5 weeks. Coverage matrix expandido. Adiciona ADR D6 (Backend pre-flight gate é Phase 0 obrigatório, blocker para Phase 1).
- **v1.2 (2026-05-26):** Revisão "Best Practices FAANG" — identifica 15 FAANG gaps (5 CRITICAL + 4 HIGH + 4 MEDIUM + 2 LOW) que v1.1 não cobria. Adiciona Phase 4 (Tier 4 personalization, reverte ADR D3 SKIP decision) e Phase 5 (FAANG-grade infra: Web Vitals per-page + axe-core CI + Storybook/Ladle + bundle analyzer + RUM). Adiciona ADRs D7 (Web Vitals per-page budget), D8 (Real User Monitoring day-1), D9 (i18n primitives mesmo EN-only). Reverte D3. Updated timing 4.5-6.5 weeks → 6-9 weeks. Adiciona 5 cross-cutting tasks injetadas nas Phases existentes (empty state taxonomy P2-4, error boundaries P2-cross, search UX P2-cross, telemetry hooks P3-cross, design tokens deprecation P5). Tier matrix expandido (Tier 4 + Tier 5 cross-cutting). Plan renamed "FAANG-Tier 3 Refactor" → "FAANG Refactor" refletindo escopo expandido.

## Context

### O que existe hoje (2026-05-26 baseline)

**Dashboard funcional, design-system shipped:**
- 13 routes ativas (overview, projects, environments, domains, billing, team, settings, profile, login, register, recovery, verification, device-success)
- @loopzero/ui@0.11.0-next.0 pinned (npm `next` agora em 0.12.0-next.0 — additive doc-only, bump independente)
- DSS v1.0 anchored to Violet Forge ([`docs/design/2026-05-25-loopzero-dashboard-design-system.md`](../../design/2026-05-25-loopzero-dashboard-design-system.md))
- T-MIG-1..4 shipped (PinInput verification, DataTable expandable/sortable, PageShell × 5 content pages)
- 117/117 vitest tests GREEN
- TOTAL bundle 145.63 KB / 180 KB hard gate (34.37 KB headroom)

**Findings documentados (3 fontes ortogonais convergem):**

| Source | Findings |
|--------|----------|
| Density audit (REPORT.md) | 2 P1 (Overview empty states + KPI count), 5 P2 (Billing/Settings/Profile sparseness, Sign-out anti-pattern, ActionBar borderline), 4 P3 |
| Vercel benchmark (vercel-benchmark.md) | 8 V-gaps (V-1 sidebar sub-nav P3, V-2 breadcrumb P2, V-3 page kebab P3, V-4 ActionBar filter+toggle P2, V-5 notification bell P3, V-6 plan-gating inline P2, V-7 sidebar grouping P3, V-8 usage scroll P2) |
| loopzero-ui slop test | 1 critical L-01 (projects.tsx:110 raw button), 1 major C-01 (environments sem PageShell), 1 minor C-05 (Loader2 inside Card), 3× R-03 (DataTable >4 cols sem hideBelow), 1 T-06 (font-bold redundante) |

### O que motiva o trabalho

**Diagnóstico em uma frase:** dashboard hoje é tecnicamente correto, mas genérico — falta signature visual + fluência de interação que separe "feels like a tool" de "feels like a product top-tier".

**Concorrentes top:** Vercel, Linear, Stripe, Render, Railway. Todos têm:
- Command palette (cmd+k everywhere)
- Breadcrumb hierárquico
- Notification center
- Microinteractions polidas (motion + transitions)
- Empty states ricas e contextuais
- Real-time updates (SSE)

**LoopZero's competitive moat:** CLI-first (per CLAUDE.md Voice/Tone). Dashboard deve AMPLIFICAR isso, não imitar produtos web-only. Pattern: toda action page tem CodeBlock equivalente inline ("Settings → CodeBlock 'loopzero whoami | team list | secret set | env create'" — já presente em /settings; expandir).

### Evidência da análise (audit-driven, not opinion)

| Página | Density audit verdict | Slop test score | Worst finding |
|--------|----------------------|-----------------|---------------|
| `/` (Overview) | NEEDS FIX | 32/32 ✓ | 2 empty states oversized (P1-1) + KPI count 3 vs 4-5 (P1-2) |
| `/projects` | PASS* (mock data) | **30/32** | L-01 raw button (critical) + R-03 7 cols no hideBelow |
| `/environments` | PASS* (mock) | **29/32** | C-01 no PageShell (major) + C-05 + R-03 |
| `/domains` | PASS* (mock) | 31/32 | R-03 6 cols no hideBelow |
| `/billing` | NEEDS FIX | 31/32 | Plan card 130px sub-dim + T-06 font-bold:212 |
| `/team` | PASS* (mock) | 32/32 ✓ | Sparseness mock-data-dependent |
| `/settings` | NEEDS FIX | 32/32 ✓ | 40% whitespace + só 2 cards |
| `/profile` | NEEDS FIX | 32/32 ✓ | "Sign out" 1-button Card anti-pattern + 38% whitespace |
| `/verification` | PASS | 32/32 + AU-* ✓ | (Brief #5 PinInput perfect) |
| `/login`, `/register`, `/recovery`, `/device-success` | PASS | (out of scope) | Density appropriate |

`*` PASS condicional: density "ok com dataset real". Mock data fino causa whitespace que não viola layout.

## Objective

**Dashboard atinge UX-tier "FAANG" em 6 phases incrementais (Phase 0-5):**

0. **Phase 0 (Backend pre-flight, NEW v1.1):** Verificar 4 endpoints/features backend não-confirmados existir. Decidir per-task: ship com backend OR ship com placeholder cards declarando "Coming Soon" OR negociar backend PR. Resolve EC-1, EC-2 e gaps em T-DR-3.4/3.9. **Blocker** para Phase 1 tasks afetadas. 3-5 dias.

1. **Phase 1 (Foundational):** Close TODOS os 7 findings documentados (2 P1 + 5 P2 + 6 slop fails). Atinge "tecnicamente correto, density-compliant, slop-test 288/288". 1 week.

2. **Phase 2 (Polish + Microinteractions):** Adiciona motion system + transitions polidas + loading states ricos + empty states contextuais. Atinge "feels alive". **Inclui sub-task obrigatória `prefers-reduced-motion` (EC-3) + optimistic UI rollback path (EC-4) + empty state taxonomy 3-tier (G6 FAANG) + error boundaries per-section (G7 FAANG) + search UX patterns debounce+virtualization (G8 FAANG).** 1-2 weeks.

3. **Phase 3 (Differentiation):** Implementa 8 features que concorrentes têm e LoopZero não: Command Palette, Breadcrumb, Notification Center, Sidebar Grouping, TopBar Kebab, Plan-Gating Inline, CLI Signature Integration, Real-time SSE wiring. **Requires Brief #7 to @loopzero/ui team** para primitives novos (CommandPalette, Breadcrumb, NotificationBell). **Inclui sub-tasks obrigatórias Rule 3 sanitize (EC-7), CLI command whitelist (EC-8), CodeBlock internal-terms hook (EC-9), cmd+k mount scope (EC-5), breadcrumb route labels (EC-6) + telemetry hooks (G12 FAANG).** 2-3 weeks.

4. **Phase 4 (Tier 4 Personalization, NEW v1.2 — reverte ADR D3 SKIP):** Implementa personalization features que Vercel/Linear têm: density-per-user toggle, theme-per-user (5 themes built-in via @loopzero/ui), sidebar reorder via drag-and-drop, recently-visited memory, saved filter presets. Resolve **G11 FAANG gap**. 1-2 weeks.

5. **Phase 5 (FAANG-grade Infra, NEW v1.2):** Performance budget formal (Core Web Vitals per-page LCP/INP/CLS), Real User Monitoring (web-vitals npm pkg), Axe-core CI integration (jest-axe + vitest-axe enforced), Storybook/Ladle para dashboard pages (espelha @loopzero/ui upstream pattern), Bundle analyzer (rollup-plugin-visualizer per-route attribution), Design tokens deprecation strategy (semver + 4-tier policy), i18n primitives architecture (EN-only at launch mas hooks setup). Resolve **G1, G2, G3, G4, G5, G9, G13 FAANG gaps (4 CRITICAL + 3 MEDIUM)**. 1-2 weeks.

**Total: 6-9 weeks (v1.2, Phase 4-5 added). Gate entre cada phase: re-run density audit + slop test + Lighthouse + Web Vitals per-page + axe-core report + manual UX review por sponsor.**

**Defer to post-FAANG v1.x (acceptable per pre-release):** G10 cross-browser matrix (Playwright Chrome/Firefox/Safari), G14 Service Worker/PWA, G15 onboarding flow. Documented em "Known risks (documented)" section.

## ADRs

### D1 — Mirror: LoopZero's own voice (não Vercel-clone, não Linear-clone)

- **Context:** Dashboard precisa de signature distintiva pra não virar "mais um clone Vercel". LoopZero's competitive moat é CLI-first (per CLAUDE.md Voice/Tone).
- **Decision:** Hybrid signature — Vercel para density/panels, Linear para keyboard-first (cmd+k), Stripe data-trust patterns para Billing, mas anchored em CLI-first amplification que é unicamente LoopZero.
- **Consequences:**
  - ✅ Brand-defensible — não temos "Vercel clone" liability
  - ✅ Alinha com CLAUDE.md Cross-Project Rule 9 + Voice/Tone
  - ✅ Justifica Brief #7 ao @loopzero/ui (CommandPalette pattern serve outros consumers)
  - ❌ Mais work que Vercel-clone direto
  - ❌ Risk: "own voice" pode virar undefined sem rigor — mitigado por D4 (CLI integration mandatory pattern)

### D2 — Incremental 3-phase com gates entre

- **Context:** Pre-release. Não temos prod customers que pagam pra adiar deploy. Big-bang risk de regressão pesado (T-MIG-1..4 mostraram que mesmo mudanças isoladas precisam test churn).
- **Decision:** 3 phases sequenciais. Gate entre phases = (i) density audit re-run, (ii) slop test re-run, (iii) Lighthouse scores, (iv) manual UX review por sponsor. Próxima phase só desbloqueia se phase anterior entrega valor mensurável.
- **Consequences:**
  - ✅ Cada phase é shipping incremental — ROI mensurável
  - ✅ Rollback safe (cada phase em commit isolado)
  - ✅ Phase 1 não precisa novo descoberta — só executa o que 3 audits já apontaram
  - ❌ Sem "wow public moment" big-bang. Mitigated: Phase 3 entrega features dramáticas o suficiente

### D3 — Skip Tier 4 (personalization) — YAGNI pre-release [REVERTED v1.2]

- **Context (v1.0):** Personalization (sidebar reorder per user, density per user, custom workspace themes) é feature avançada de produtos com 1000+ users dando feedback divergente.
- **Decision (v1.0):** Defer to v1.x pós-GA. Pre-release single-user customization é YAGNI.
- **REVERSAL (v1.2):** "Best practices FAANG" revisão identifica que G11 (personalization) NÃO é Tier 4 wishlist — é Tier 3-equivalent FAANG signature. Vercel, Linear, Stripe TODOS shipam personalization day-1 (não 1000-user-feedback driven). Pre-release context não justifica skip — justifies escopo modesto (density toggle + theme switcher + sidebar reorder, sem custom workspace themes).
- **Decision (v1.2):** Phase 4 NEW adiciona Tier 4 personalization com escopo modesto:
  - Density toggle (compact/comfortable/spacious — already in @loopzero/ui ThemeProvider runtime)
  - Theme switcher (5 themes already built-in via @loopzero/ui per CHANGELOG)
  - Sidebar reorder via drag-and-drop (Linear pattern)
  - Recently-visited memory (per-page localStorage)
  - Saved filter presets (Vercel pattern)
- **Consequences:**
  - ✅ Atende G11 FAANG gap (Vercel sidebar customizable parity)
  - ✅ Reusa @loopzero/ui ThemeProvider + density runtime (zero custom infra)
  - ✅ ~1-2 weeks adicionais (não-blocker pro core FAANG mas atinge "feels Linear")
  - ❌ Adiciona 4-5 tasks ao plano

### D7 — Performance budget formal (Core Web Vitals per-page) [NEW v1.2]

- **Context:** Plan v1.1 menciona "Lighthouse ≥90" como acceptance global, mas sem per-page budget per Core Web Vitals (LCP, INP, CLS). FAANG standard (Google PageSpeed, Vercel) tracks per-route com budget gates em CI.
- **Decision:** Phase 5 implementa formal per-page budget:
  - **LCP** (Largest Contentful Paint) ≤ 2.5s — gates em /, /projects, /domains, /billing
  - **INP** (Interaction to Next Paint) ≤ 200ms — gates em /verification (PinInput), /team (invite)
  - **CLS** (Cumulative Layout Shift) ≤ 0.1 — gates em / (KPI strip + projects panel)
  - **TBT** (Total Blocking Time) ≤ 200ms — gates em Phase 3 cmd+k open path
- **Consequences:**
  - ✅ FAANG-tier performance discipline
  - ✅ Sub-200ms INP enforce previne skeleton-flash regression
  - ❌ Requires Lighthouse CI setup ou web-vitals npm pkg + threshold gates

### D8 — Real User Monitoring (RUM) from day-1 [NEW v1.2]

- **Context:** Vercel/Linear/Stripe shipam RUM (real user telemetry) day-1 — não esperam prod usage pra começar coletar. FAANG pattern.
- **Decision:** Phase 5 wire `web-vitals` npm pkg em `cloud/dashboard/src/main.tsx` enviando LCP/INP/CLS para backend telemetry endpoint OR console.log em dev mode. Backend RUM endpoint = future scope (não bloqueia consumer setup).
- **Consequences:**
  - ✅ Pre-release telemetry pronta — quando prod customers chegarem, signals fluindo dia-1
  - ✅ Dev local mostra metrics console
  - ❌ Requires nova dep (`web-vitals` ~5KB) + optional backend endpoint

### D9 — i18n primitives setup mesmo se EN-only at launch [NEW v1.2]

- **Context:** FAANG products never ship without i18n primitives setup, mesmo lançando EN-only. Custo retrofit é muito alto. Atual `lib/format.ts` hardcoded `en-US` locale.
- **Decision:** Phase 5 wire i18n architecture com primitives setup (mas NÃO translate UI strings ainda):
  - Adicionar `react-intl` OR `i18next` dependency
  - Wrap app em `<IntlProvider locale="en-US">`
  - Move all user-facing strings para `messages.en.json` namespace
  - `lib/format.ts` receives locale via context (não hardcoded)
  - RTL support architecture (Tailwind `dir` aware)
- **Consequences:**
  - ✅ Retrofit cost evitado (FAANG hard-lesson)
  - ✅ pt-BR pode ser adicionada em PR de 1-2 hours quando sponsor pedir
  - ❌ Phase 5 adiciona ~3-4 hours de setup + bundle delta (~10KB react-intl OR ~8KB i18next)

### D4 — CLI-First Signature Pattern: toda action page tem CodeBlock equivalente inline

- **Context:** LoopZero's brand é CLI-first. Settings page já tem CodeBlock visible ("Common settings commands"). Pattern deve expandir.
- **Decision:** Toda página com actions (Projects, Domains, Environments, Team, Billing, Settings) tem secondary section "Or use the CLI" com CodeBlock mostrando commands equivalentes. NÃO substitui UI, AMPLIFICA. Padrão: collapse default, expand on click.
- **Consequences:**
  - ✅ Signature claro — LoopZero dashboard FALA da CLI continuamente
  - ✅ Conversion funnel — usuário começa visual, gradualmente migra pra CLI (per CLAUDE.md "CLI is the primary interface")
  - ✅ Plus para developer-onboarding (descobrir commands via UI)
  - ❌ Mais content per page — risk de density violation. Mitigated por collapse-default

### D5 — Phase 3 features que requerem novos primitives → Brief #7 ao @loopzero/ui

- **Context:** CommandPalette, Breadcrumb, NotificationBell, Sidebar.Group component, plan-gating Alert variant não existem em @loopzero/ui@0.11/0.12.
- **Decision:** Antes de Phase 3 começar, emit Brief #7 ao @loopzero/ui team (mesmo padrão Brief #5/#6). Ship features ↔ wait for primitives. Pode rodar parallel (consumer-side wait com mock primitives) mas plan task ordem: Brief #7 enviado em Phase 2 end, Phase 3 começa quando 0.13 release responde.
- **Consequences:**
  - ✅ Reuse pattern Brief #4/#5/#6 (validated workflow)
  - ✅ Primitives shippped servem outros consumers (não só LoopZero)
  - ❌ Phase 3 depends de upstream timing — risk de delay. Mitigated: Brief #7 envia early (end of Phase 2), upstream typical 24-72h response per Brief #4 evidence; contingency em "Known risks" (EC-23) defer T-DR-3.1+3.4 se Brief #7 > 1 week

### D6 — Backend pre-flight como Phase 0 obrigatório (NEW v1.1)

- **Context:** /edge-case-plan review identificou backend-prep gap sistêmico — 4 tasks (T-DR-1.3 payment last4, T-DR-1.4 API keys + workspace settings, T-DR-3.4 Notification audit_log via SSE, T-DR-3.9 SSE status endpoint) supõem endpoints/features backend não-confirmados existir.
- **Decision:** Adicionar Phase 0 (5 backend pre-flight tasks) ANTES de Phase 1. Cada task verifica endpoint existe via grep em api/internal/routes + curl smoke teste. Per task, decidir: (a) backend ready → Phase 1 task executa normal; (b) backend não pronto, mas baixo esforço → negociar backend PR antes; (c) backend não pronto, alto esforço → re-escopar Phase 1 task para "Coming Soon" placeholder card documentado.
- **Consequences:**
  - ✅ Evita ship cards funcionalmente vazios (placeholder permanente disfarçado de feature)
  - ✅ Surface backend dependencies ANTES de codar consumer-side
  - ✅ Resolve EC-1 + EC-2 inteiros, parts de EC-?? T-DR-3.4 + T-DR-3.9
  - ❌ Adiciona 3-5 dias ao timeline antes de visible progress
  - ❌ Pode bloquear Phase 1 se backend gaps são grandes — mitigated por placeholder scope option

## Coverage Matrix (v1.1 expanded with 23 edge cases)

| Gap source | Item | Phase | Task |
|------------|------|-------|------|
| **Edge case EC-1** | **Payment Method last4 backend endpoint** | **0** | **T-DR-0.1** |
| **Edge case EC-2** | **Workspace + API Keys endpoints** | **0** | **T-DR-0.2, 0.3** |
| **Edge case (impl T-DR-3.4)** | **Audit log SSE endpoint** | **0** | **T-DR-0.4** |
| **Edge case (impl T-DR-3.9)** | **Status SSE endpoint** | **0** | **T-DR-0.5** |
| Density REPORT P1-1 | Overview empty states oversized | 1 | T-DR-1.1 |
| Density REPORT P1-2 | Overview KPI count 3 → 5 | 1 | T-DR-1.2 |
| Density REPORT P2-1 | Billing Plan card sub-dimensionada | 1 | T-DR-1.3 (gated por T-DR-0.1) |
| Density REPORT P2-2 | Settings sparseness (40% whitespace) | 1 | T-DR-1.4 (gated por T-DR-0.2, 0.3) |
| Density REPORT P2-3 | Profile Sign-out 1-button Card anti-pattern | 1 | T-DR-1.5 |
| Density REPORT P2-4 | Profile sparseness (38% whitespace) | 1 | T-DR-1.5 |
| Density REPORT P2-5 | ActionBar height 70px borderline (Projects/Domains) | 1 | T-DR-1.6 |
| Slop L-01 | projects.tsx:110 raw `<button>` | 1 | T-DR-1.7 |
| Slop C-01 | environments.tsx sem PageShell | 1 | T-DR-1.8 |
| Slop C-05 | environments.tsx Loader2 inside Card | 1 | T-DR-1.8 |
| Slop R-03 × 3 | DataTable >4 cols sem hideBelow (Projects/Domains/Environments) | 1 | T-DR-1.9 |
| Slop T-06 | billing.tsx:212 font-bold redundante | 1 | T-DR-1.10 |
| Vercel V-2 | Breadcrumb no TopBar | 3 | T-DR-3.2 + sub 3.2.1 (EC-6) |
| Vercel V-4 | ActionBar filter + layout-switch | 3 | T-DR-3.6 |
| Vercel V-6 | Plan-gating inline (read-only banner pattern) | 3 | T-DR-3.7 |
| Vercel V-8 | Usage panel scroll interno | 2 | T-DR-2.5 |
| Vercel V-1 | Sidebar sub-nav | (deferred — ADR pendente; sem rotas profundas hoje) |
| Vercel V-3 | TopBar kebab "..." menu | 3 | T-DR-3.5 + subs 3.5.1 (EC-7) + 3.5.2 (EC-8) |
| Vercel V-5 | Notification bell | 3 | T-DR-3.4 (gated por T-DR-0.4) |
| Vercel V-7 | Sidebar visual grouping | 3 | T-DR-3.3 |
| CLI signature (D4) | CodeBlock pattern em toda page | 3 | T-DR-3.8 + sub 3.8.1 (EC-9 Rule 3 hook) |
| Motion system | DSS v1.1 motion tokens | 2 | T-DR-2.1 + sub 2.1.1 (EC-3 reduced-motion) |
| Loading polish | PageShell skeleton + optimistic UI + useDelayedLoading | 2 | T-DR-2.2 (EC-14) |
| Empty state quality | Contextual CTAs + icon variety | 2 | T-DR-2.4 |
| Real-time SSE | Status badges live update + BroadcastChannel multi-tab | 3 | T-DR-3.9 (EC-16, gated por T-DR-0.5) |
| Command Palette (Linear signature) | cmd+k everywhere (post-auth only) | 3 | T-DR-3.1 + sub 3.1.1 (EC-5 mount scope) (requires Brief #7) |
| Optimistic UI rollback | Team invite onError revert | 2 | T-DR-2.7 + sub 2.7.1 (EC-4) |
| **Known risks (documented)** | EC-17..23 + EC-?? native notifications | All | "Known risks (documented)" section |

**Coverage: 25/25 findings + 23 edge cases + signature features endereçadas.** P3 notes do density audit (DataTable mock-data sparseness) intencionalmente NÃO endereçadas — depende de data real, não defeito de design.

## Phase 0 — Backend Pre-flight (NEW v1.1, resolve backend-gap)

**Objetivo:** Verificar que 4 endpoints/features backend necessários para Phase 1+3 tasks EXISTEM antes de codar consumer. Decidir per-task: backend ready → executa, backend gap → negocia PR OR re-escopa para placeholder. 3-5 dias.

**Methodology:** cada task = (1) `grep -rn` em `api/internal/routes` + `api/internal/services` por endpoint pattern, (2) `curl` smoke test contra `/api/v1/...` em k3d local, (3) registrar resultado em [`docs/reviews/backend-preflight-2026-05-26.md`](../../reviews/backend-preflight-2026-05-26.md) com verdict per task: READY / NEGOTIATE / RESCOPE.

### T-DR-0.1 — Verify `/v1/billing` response shape inclui `payment_method.last4`

**Resolves:** EC-1

**Verify:**
```bash
grep -rn "payment_method\|last4" api/internal/services/billing
curl -H "Authorization: Bearer $JWT" http://localhost/api/v1/billing | jq '.payment_method'
```

**Outcomes:**
- READY → T-DR-1.3 implementa Payment Method card como planejado
- NEGOTIATE → backend PR ~1 day adicionando Stripe customer.payment_method.card.last4 ao response shape
- RESCOPE → T-DR-1.3 escopa apenas quota lines + next billing date (sem Payment Method preview)

**DoD:**
- [ ] Verdict documentado em backend-preflight report
- [ ] Se NEGOTIATE: backend PR drafted + linked
- [ ] Se RESCOPE: T-DR-1.3 spec atualizada removendo Payment Method scope

### T-DR-0.2 — Verify Workspace Settings endpoints (Timezone, Default Env, Notifications)

**Resolves:** EC-2 (parte 1)

**Verify:**
```bash
grep -rn "workspace\|timezone\|notification_pref" api/internal/routes
curl -H "Authorization: Bearer $JWT" http://localhost/api/v1/workspace 2>&1 | head
```

**Outcomes:**
- READY → T-DR-1.4 (parte Workspace Settings card) implementa normal
- NEGOTIATE → backend PR criando endpoints CRUD `/v1/workspace/{timezone,default_env,notifications}` ~2 days
- RESCOPE → Workspace Settings card vira "Coming Soon" placeholder + documento gap em CLAUDE.md known issues

### T-DR-0.3 — Verify API Keys endpoints (rotation, list, create)

**Resolves:** EC-2 (parte 2)

**Verify:**
```bash
grep -rn "api.key\|apikey\|api_key" api/internal/routes api/internal/services
curl -H "Authorization: Bearer $JWT" http://localhost/api/v1/api-keys 2>&1 | head
```

**Outcomes:**
- READY → T-DR-1.4 (parte API Keys card) implementa normal
- NEGOTIATE → backend PR ~3 days (auth model + rotation + 1-time-display)
- RESCOPE → API Keys card scope removido de T-DR-1.4 (CLI-only feature per DSS § 9.1)

### T-DR-0.4 — Verify SSE endpoint para audit_log (Notification Center source)

**Resolves:** T-DR-3.4 backend dependency (mencionado em EC-? mas não explícito em v1.0)

**Verify:**
```bash
grep -rn "audit_log\|sse.*notification\|EventSource" api/internal/routes api/internal/sse
curl -H "Authorization: Bearer $JWT" http://localhost/api/v1/audit/stream -N 2>&1 | head
```

**Outcomes:**
- READY (SSE exists) → T-DR-3.4 wire SSE
- READY (REST only, no SSE) → T-DR-3.4 wire REST polling 30s (acceptable per CHANGELOG.md mention "audit log filterable")
- NEGOTIATE → backend PR adding SSE to audit_log endpoint (~2 days)
- RESCOPE → Notification Center poll-only (no real-time); document trade-off

### T-DR-0.5 — Verify SSE endpoint para live status updates (status badges)

**Resolves:** T-DR-3.9 backend dependency

**Verify:**
```bash
grep -rn "status.*sse\|application.status.*stream" api/internal/sse api/internal/routes
# Per CHANGELOG.md, SSE existe para deploy events. Status updates SSE = different endpoint?
```

**Outcomes:**
- READY (single SSE endpoint streams all event types incl status) → T-DR-3.9 wire normal
- READY (separate SSE para status existe) → T-DR-3.9 wire normal
- NEGOTIATE → backend PR extend SSE multiplex para status events (~2 days)
- RESCOPE → T-DR-3.9 defer para v1.1, status badges permanecem polling em refresh

### T-DR-0.6 — Phase 0 closure report

**Files:**
- [`docs/reviews/backend-preflight-2026-05-26.md`](../../reviews/backend-preflight-2026-05-26.md) (NEW) — consolidates 5 verdicts + decisions
- Update plan v1.1 (this file) "Coverage Matrix" reflecting Phase 0 outcomes

**DoD Phase 0:**
- [ ] 5 verdicts documented
- [ ] 0 NEGOTIATE PRs blocking (either merged OR escalated to backend team)
- [ ] RESCOPE decisions ratified by sponsor
- [ ] Phase 1 tasks afetadas (T-DR-1.3, 1.4, 3.4, 3.9) atualizadas no plano com scope final
- [ ] Sponsor explicit go-ahead para Phase 1 começar
- [ ] Commit `docs(dashboard): Phase 0 backend pre-flight verdicts + decisions`

---

## Phase 1 — Foundational (close 10 documented findings)

**Objetivo:** Atingir "tecnicamente correto" — density compliance + slop-test 288/288. 1 week (5 working days).

### T-DR-1.1 — Overview empty states consolidation (closes P1-1)

**Files to edit:**
- `cloud/dashboard/src/pages/overview.tsx` — substituir 2 EmptyState cards full-size por (a) inline Alert banner em Usage panel header ("Set up alerts →"), (b) drop "Recent Previews" section até backend existir OR consolidar em single small EmptyState com 2 ações
- `cloud/dashboard/src/pages/overview.test.tsx` — atualizar assertions (remove queryByText "Get alerted for anomalies" + "No recent previews"; add para inline alert)

**TDD:**
- `test_overview_alerts_inline_banner_renders` — Alert variant info no Usage panel header
- `test_overview_recent_previews_section_removed_OR_compact` — section absent OR ≤200px
- `test_overview_total_empty_states_at_most_1` — CC-3 enforce

**Acceptance:**
- Density audit re-run mostra 0 empty state >200px no Overview
- CC-3 violation closed (max 1 empty state per page)

**DoD:**
- [ ] 3 new tests GREEN
- [ ] Density audit Overview verdict = PASS
- [ ] Commit `fix(dashboard): T-DR-1.1 consolidate Overview empty states (closes P1-1)`

### T-DR-1.2 — Overview KPI count 3 → 5 (closes P1-2)

**Files to edit:**
- `cloud/dashboard/src/pages/overview.tsx` — adicionar 2 KPIs ao `usageMetrics` map: Build Minutes (`145/400` já existe em /billing) + Concurrent Builds (`2/4` idem). Wire ao `/v1/billing.usage` response (já flui)
- `cloud/dashboard/src/pages/overview.test.tsx` — atualizar `test_overview_renders_usage_meter_with_3_metrics` → `_with_5_metrics`

**TDD:**
- `test_overview_renders_usage_meter_with_5_metrics` — 5 KPIs presentes
- `test_overview_kpi_build_minutes_renders` — específico
- `test_overview_kpi_concurrent_builds_renders` — específico

**Acceptance:**
- Vercel benchmark V-target (4-5 KPIs) atingido
- Backend mock + real wire 5 KPIs

**DoD:**
- [ ] 3 tests updated/GREEN
- [ ] Commit `fix(dashboard): T-DR-1.2 add Build Minutes + Concurrent Builds KPIs (closes P1-2)`

### T-DR-1.3 — Billing density polish (closes P2-1 + T-06)

**Files to edit:**
- `cloud/dashboard/src/pages/billing.tsx` —
  - Remove `font-bold` em L212 (T-06 fail)
  - Enrich Plan card com 3-line quota summary inline ("10 projects · 3 environments · 1000 builds/month") + next billing date
  - Add Payment Method preview (last4) se data disponível, OR placeholder com CTA
- `cloud/dashboard/src/pages/billing.test.tsx` — atualizar assertions

**TDD:**
- `test_billing_plan_card_renders_quota_summary` — 3-line quota inline
- `test_billing_plan_card_renders_next_billing_date` — quando billing.period_end present
- `test_billing_no_font_bold_redundancy` — grep T-06 zero hits

**Acceptance:**
- Density audit Billing verdict = PASS
- Plan card ≥240px (closer to 280-360 target)
- slop test T-06 closed

### T-DR-1.4 — Settings density polish (closes P2-2)

**Files to edit:**
- `cloud/dashboard/src/pages/settings.tsx` — adicionar 2 cards: (a) Workspace Settings (Timezone, Default environment, Notification preferences), (b) API Keys (rotation + list + create). Mesmo nível DSS § 9.1 (CLI-first) — convenience GUI on top
- `cloud/dashboard/src/pages/settings.test.tsx` — adicionar tests

**TDD:**
- `test_settings_workspace_card_renders` — Timezone + Default env + Notif prefs
- `test_settings_api_keys_card_renders` — Rotation + List + Create
- `test_settings_total_cards_at_least_3` — density gate

**Acceptance:**
- Density audit Settings verdict = PASS
- Whitespace abaixo ≤25%

### T-DR-1.5 — Profile polish (closes P2-3 + P2-4)

**Files to edit:**
- `cloud/dashboard/src/pages/profile.tsx` —
  - Move "Sign out" button into Account card header (right-aligned, ao lado do nome)
  - Delete the standalone Sign-out Card wrapper (anti-pattern)
  - Add Recent Activity card (last 5 actions from audit_log slice)
  - Add Connected Apps card (GitHub, Google OAuth from Kratos identity)
- `cloud/dashboard/src/pages/profile.test.tsx` — atualizar para new positioning + new cards

**TDD:**
- `test_profile_sign_out_in_account_card_header` — não em standalone Card
- `test_profile_recent_activity_card_renders` — 5 last actions
- `test_profile_connected_apps_card_renders` — provider list
- `test_profile_connected_apps_handles_missing_credentials` (SHOULD TEST EC-12) — quando `session.identity.credentials` undefined OR vazio, card mostra "No connected apps" sem crash

**Acceptance:**
- Density audit Profile verdict = PASS
- No 1-button Card anti-pattern

### T-DR-1.6 — ActionBar height tuning (closes P2-5)

**Files to edit:**
- `cloud/dashboard/src/components/layout/action-bar.tsx` — reduzir padding interno de `py-3` para `py-2` (~8px gain)

**TDD:**
- `test_action_bar_height_at_most_64px` — assertion via render
- `test_action_bar_height_consistent_across_4_consumers` (SHOULD TEST EC-10) — Overview/Projects/Domains/Team rendering ActionBar todos ≤64px sem clipping

**Acceptance:**
- ActionBar em Projects/Domains ≤ 64px (target 56-64, max 72)
- TouchTarget ≥ 24×24px mantido (a11y)

**Trade-off considerado:** Pode mexer em outras consumers (Team, Overview). Mitigated por revisão visual completa post-edit + cross-consumer test EC-10.

### T-DR-1.7 — Projects raw button → Button asChild (closes slop L-01)

**Files to edit:**
- `cloud/dashboard/src/pages/projects.tsx` — L110-121 substituir raw `<button>` por:
  ```tsx
  <Button variant="ghost" size="sm" asChild>
    <Link to={`/projects/${r.name}`} aria-label={`Open ${r.name}`}>
      Open <ExternalLink className="size-3" aria-hidden="true" />
    </Link>
  </Button>
  ```
- `cloud/dashboard/src/pages/projects.test.tsx` — adjust if needed (role still "button" via Button primitive)

**TDD:**
- `test_projects_open_action_renders_button_primitive` — grep no raw `<button>` em source
- `test_projects_open_action_navigates_via_link` (SHOULD TEST EC-11) — assert `navigate()` chamado UMA vez (not double-fired por Button onClick + Link click; Button asChild + Link compatibility)
- Existing tests still PASS

**Acceptance:**
- Slop L-01 closed (grep `<button ` em projects.tsx = 0)
- Slop A-03 closed (navigation via Link, not button onClick)
- Button asChild + react-router-dom Link works (single navigation event per click)

### T-DR-1.8 — Environments PageShell migration (closes slop C-01 + C-05)

**Files to edit:**
- `cloud/dashboard/src/pages/environments.tsx` — full rewrite seguindo pattern T-MIG-4 (Team page): PageShell + search + loading/empty state machine. Eliminate inline `<Card><Loader2></Card>` + inline `<EmptyState>` branches
- `cloud/dashboard/src/pages/environments.test.tsx` — adjust assertions

**TDD:**
- `test_environments_uses_page_shell` — assert PageShell wrapper
- `test_environments_loading_via_page_shell_loading_prop`
- `test_environments_empty_via_page_shell_empty_prop`
- `test_environments_pagination_kicks_in_above_10_rows` (SHOULD TEST EC-13) — wire DataTable.pagination quando filtered.length > 10 (CD-01 gate)
- Existing tests still PASS

**Acceptance:**
- Slop C-01 + C-05 closed
- environments score 32/32
- CD-01 gate honored (pagination above 10 rows)

### T-DR-1.9 — DataTable hideBelow markings (closes slop R-03 × 3)

**Files to edit:**
- `cloud/dashboard/src/pages/projects.tsx` — add `hideBelow: "md"` em `apps_count` + `environment` + `framework` columns
- `cloud/dashboard/src/pages/domains.tsx` — add `hideBelow: "md"` em `verified` + `environment` columns
- `cloud/dashboard/src/pages/environments.tsx` (post T-DR-1.8) — add `hideBelow: "md"` em `projects` + `protected` columns

**TDD:**
- Mobile responsiveness manual check via Chrome devtools 320/375/414
- `test_*_table_columns_hide_below_md` — assertion via DataTable column config inspection

**Acceptance:**
- No horizontal scroll @ 320px em todas 3 DataTable pages
- R-03 closed em 3 files

### T-DR-1.10 — Bundle measurement + Phase 1 closure

**Files to edit:** none new — measurement only

**TDD:** N/A

**Acceptance:**
- `npm run build` clean
- `npm run size` — all 9 chunks GREEN, total ≤ 180 KB
- Re-run density audit (skill `/dashboard-density-audit`) — verdict cross-tour PASS em todas 9 content pages
- Re-run loopzero-ui audit — score 288/288 (100%) em 9 page files

**DoD Phase 1:**
- [ ] All 10 tasks committed
- [ ] 117+ vitest tests GREEN (deve crescer com novos testes)
- [ ] Bundle within gate
- [ ] Density audit re-run: 0 P1 + 0 P2 (P3 notes OK)
- [ ] loopzero-ui audit re-run: 288/288
- [ ] Phase 1 final commit: `fix(dashboard): Phase 1 close 10 audit findings (T-DR-1.1..1.10)`
- [ ] Manual sponsor review pre Phase 2 gate

## Phase 2 — Polish + Microinteractions

**Objetivo:** "Feels alive" — motion system + transitions + loading rich + empty contextual. 1-2 weeks.

### T-DR-2.1 — Motion system foundation (DSS v1.1)

**Files to edit:**
- `cloud/dashboard/src/styles/motion.css` (NEW) — motion tokens (timing + easing)
- [`docs/design/2026-05-25-loopzero-dashboard-design-system.md`](../../design/2026-05-25-loopzero-dashboard-design-system.md) — bump to v1.1, add § 10 Motion
- `cloud/dashboard/tailwind.config.ts` — extend with motion utilities

**Motion tokens DSS v1.1:**
```css
--motion-instant: 0ms        /* immediate response, e.g. focus ring */
--motion-fast: 90ms          /* hover state changes */
--motion-base: 180ms         /* default transition (Linear pattern) */
--motion-slow: 320ms         /* modal enter/exit */
--easing-standard: cubic-bezier(0.2, 0, 0, 1)     /* default */
--easing-decelerate: cubic-bezier(0, 0, 0, 1)     /* enter */
--easing-accelerate: cubic-bezier(0.3, 0, 1, 1)   /* exit */

/* MUST FIX EC-3 — respect OS accessibility setting */
@media (prefers-reduced-motion: reduce) {
  :root {
    --motion-fast: 0ms;
    --motion-base: 0ms;
    --motion-slow: 0ms;
  }
}
```

#### Sub-task T-DR-2.1.1 — `prefers-reduced-motion` honored (MUST FIX EC-3)

**Files:**
- `cloud/dashboard/src/styles/motion.css` — include media query block acima
- `cloud/dashboard/src/styles/motion.test.ts` — assert override

**TDD:**
- `test_motion_respects_prefers_reduced_motion` — matchMedia mock + assert `--motion-base` resolves to `0ms`

**Acceptance:**
- WCAG 2.3.3 pass
- Lighthouse a11y ≥ 95 mantém

**TDD (Task base):**
- `test_motion_tokens_in_tailwind_preset` — assert classes generated
- DSS v1.1 doc updated

**Acceptance (Task base):**
- Motion CSS vars available globally
- Tailwind utilities `transition-base`, `transition-fast`, `ease-standard` etc.
- Sub-task 2.1.1 DoD met

### T-DR-2.2 — Loading transitions polish

**Files to edit:**
- `cloud/dashboard/src/pages/projects.tsx`, `domains.tsx`, `environments.tsx`, `team.tsx` — wire `<Skeleton>` rows (count=5) instead of generic spinners; opt-in via DataTable.loading + PageShell.loading
- `cloud/dashboard/src/hooks/use-delayed-loading.tsx` (NEW) — pattern `useDelayedLoading(200ms)` para evitar skeleton flash em fetches rápidos
- Optimistic UI: implement in T-DR-2.7 separately

**TDD:**
- `test_projects_loading_renders_skeleton_rows` — already PASS post T-MIG-3 (DataTable.loading handles)
- Same for environments + team
- `test_skeleton_hidden_when_fetch_under_200ms` (SHOULD TEST EC-14) — `useDelayedLoading(200)` só mostra skeleton se fetch demora >200ms (evita flicker em network rápido)

**Acceptance:**
- All loading states use Skeleton primitive (closes C-05 broader pattern)
- Skeleton flash eliminado em sub-200ms fetches

### T-DR-2.3 — Hover/focus state polish

**Files to edit:**
- Multiple component-level — adopt `transition-base ease-standard` consistently in Button, Card, Input variants (test against motion tokens from T-DR-2.1)

### T-DR-2.4 — Empty state quality bump

**Files to edit:**
- `cloud/dashboard/src/pages/*.tsx` — review every EmptyState usage. Ensure each has: contextual icon (variety per page, not all FolderKanban), specific description, primary CTA when action exists OR none if waiting
- Pattern: EmptyState should suggest next step ("Run `loopzero init`", "Connect domain", "Invite member"), not just inform absence

**TDD:**
- Manual review checklist
- `test_*_empty_state_has_actionable_cta_or_clear_next_step` — review each

### T-DR-2.5 — Usage panel scroll interno (closes V-8 from Vercel)

**Files to edit:**
- `cloud/dashboard/src/pages/overview.tsx` — wrap UsageMeter em container `max-h-[400px] overflow-y-auto` se houver > 4 KPIs renderizados, OR ship a Brief #7 ask for `<UsageMeter scrollable>` prop

**TDD:**
- `test_overview_usage_scrolls_when_5_kpis_exceed_panel` — assert overflow-y-auto present when > 4

### T-DR-2.6 — Status badge live polish (StatusDot pulse on "live"/"building")

**Files to edit:**
- Multiple status surfaces — verify StatusDot accepts `pulse` prop OR Brief #7 ask

### T-DR-2.7 — Optimistic UI for non-destructive actions

**Files to edit:**
- `cloud/dashboard/src/pages/team.tsx` (Invite) — optimistic add row before backend confirms
- Pattern documented for future actions

#### Sub-task T-DR-2.7.1 — Rollback path obrigatório (MUST FIX EC-4)

**Pattern (mandatory):**
```ts
// onMutate: add optimistic row + return rollback context
// onError: invoke rollback context (revert) + show <Alert variant="destructive">
// onSettled: refetch to reconcile
```

Sem rollback, backend reject (409 email exists, 402 plan limit) deixa UI mostrando row inexistente → data loss confusion.

**Acceptance:**
- Backend 4xx/5xx → row removida + Alert destructive com mensagem actionable
- Network failure → mesmo comportamento

**TDD (mandatory per EC-4):**
- `test_team_invite_optimistic_adds_row_before_response`
- `test_team_invite_rolls_back_on_409_email_exists` — **MUST exist**
- `test_team_invite_rolls_back_on_402_plan_limit` — **MUST exist**
- `test_team_invite_shows_destructive_alert_on_error`

**DoD Phase 2:**
- [ ] All 7 tasks committed
- [ ] DSS bumped to v1.1 with Motion section
- [ ] All transitions use motion tokens
- [ ] All loading uses Skeleton
- [ ] All empty states have actionable CTA OR clear waiting state
- [ ] Brief #7 drafted (CommandPalette, Breadcrumb, NotificationBell, UsageMeter.scrollable, StatusDot.pulse, Alert.gating variant)
- [ ] Manual sponsor review pre Phase 3 gate

## Phase 3 — Differentiation Features

**Objetivo:** Implementar 9 features que concorrentes têm. Requires Brief #7 primitives from @loopzero/ui. 2-3 weeks (depends on upstream timing).

### T-DR-3.1 — Command Palette (cmd+k) integration

**Files to edit:**
- `cloud/dashboard/src/components/command-palette.tsx` (NEW) — wrap @loopzero/ui CommandPalette (post Brief #7)
- `cloud/dashboard/src/components/layout/dashboard-layout.tsx` — wire cmd+k global shortcut
- Actions catalog: Navigate to {Overview, Projects, ...}, Recent projects, Deploy {project}, Open CLI docs, Sign out, ...

#### Sub-task T-DR-3.1.1 — Mount scope: DashboardLayout only, NOT auth pages (MUST FIX EC-5)

**Constraint:** CommandPalette deve ser mounted DENTRO de `<DashboardLayout>` (post-auth shell), NUNCA em auth routes (login/register/recovery/verification). Senão palette responde cmd+k em auth context → vazia OR security smell (revela actions para non-auth).

**Implementation:**
```tsx
// dashboard-layout.tsx — wrap content
<DashboardLayout>
  <CommandPalette catalog={...} />  {/* Mount aqui, NÃO em app.tsx root */}
  {children}
</DashboardLayout>
```

**TDD (mandatory per EC-5):**
- `test_command_palette_not_mounted_on_login_route` — **MUST exist**
- `test_command_palette_not_mounted_on_register_route`
- `test_command_palette_not_mounted_on_recovery_route`
- `test_command_palette_not_mounted_on_verification_route`
- `test_command_palette_mounted_on_overview_route`

**TDD (Task base):**
- `test_command_palette_opens_on_cmd_k`
- `test_command_palette_navigation_works`
- `test_command_palette_fuzzy_search`

**Acceptance:**
- cmd+k opens palette everywhere POST-AUTH
- Top-N catalog actions wired
- Auth pages NÃO respondem a cmd+k

### T-DR-3.2 — Breadcrumb composite + TopBar wire (closes V-2)

**Files to edit:**
- `cloud/dashboard/src/components/layout/top-bar.tsx` — substituir título-só por Breadcrumb (post Brief #7)
- Para rotas shallow (1 level), Breadcrumb degenera para single title (backward compatible)
- Para rotas deep futuras (`/projects/{id}/deployments/{id}`), funciona automaticamente

#### Sub-task T-DR-3.2.1 — Breadcrumb label resolver per route (MUST FIX EC-6)

**Problem:** rotas dynamic como `/projects/:id` têm `id` que é UUID/name não-display-friendly. Sem resolver, breadcrumb mostra "Projects / 7b1e1e3d-3afa..." em vez de "Projects / acme-web".

**Pattern (mandatory):**
```ts
// app.tsx route definition extended
const routes = [
  { path: '/projects', breadcrumbLabel: () => 'Projects' },
  { path: '/projects/:id', breadcrumbLabel: ({ id }) => useProject(id)?.name ?? id },
  // ...
];
```

Breadcrumb composite carrega labels via hook que respeita lookup.

**TDD (mandatory per EC-6):**
- `test_breadcrumb_resolves_project_id_to_name` — `/projects/abc-123` → "acme-web"
- `test_breadcrumb_fallback_for_unknown_route` (SHOULD TEST EC-15) — rota não registrada degrada para "Untitled" ou esconde breadcrumb (NÃO crash) — **MUST exist**

**TDD (Task base):**
- `test_top_bar_breadcrumb_renders_section_subpage`
- `test_top_bar_breadcrumb_clickable_left_segments`

### T-DR-3.3 — Sidebar visual grouping (closes V-7)

**Files to edit:**
- `cloud/dashboard/src/components/layout/app-sidebar.tsx` — adicionar headers "RESOURCES" (Projects/Environments/Domains) + "WORKSPACE" (Settings/Profile/Team/Billing) seguindo Sidebar.Group component (post Brief #7) OR inline `<p className="text-label-caps text-muted-foreground tracking-wider uppercase">`

**TDD:**
- `test_sidebar_resources_group_renders`
- `test_sidebar_workspace_group_renders`

### T-DR-3.4 — Notification Center (closes V-5)

**Files to edit:**
- `cloud/dashboard/src/components/layout/notification-bell.tsx` (NEW) — wrap NotificationBell primitive (post Brief #7)
- `cloud/dashboard/src/components/layout/app-sidebar.tsx` — wire bell em footer ao lado do account chip
- Notification source: audit_log endpoint (already exists per CHANGELOG.md "audit log (filterable by action/resource_type/since, paginated)")

**TDD:**
- `test_notification_bell_renders_unread_count`
- `test_notification_panel_lists_recent_audit_events`

### T-DR-3.5 — TopBar kebab menu (closes V-3)

**Files to edit:**
- `cloud/dashboard/src/components/layout/top-bar.tsx` — add `<DropdownMenu>` no canto direito com actions: "Open in CLI" (copy command equivalente), "Copy as JSON" (page state), "Refresh", "Open docs"

#### Sub-task T-DR-3.5.1 — "Copy as JSON" sanitize layer (MUST FIX EC-7, Rule 3)

**Problem:** Page state pode incluir `tenant_id`, `cell_id`, `cellID`, raw CRD shapes — Rule 3 PROHIBITS internal terms em user-facing output (including --json shapes copied to clipboard).

**Implementation:**
```ts
// cloud/dashboard/src/lib/sanitize.ts (NEW)
const FORBIDDEN_KEYS = [
  'tenant_id', 'cell_id', 'cellID',
  'argocd_app', 'gitea_repo', 'minio_bucket',
  // ...
];
export function sanitizeForCopy(obj: unknown): unknown {
  // deep-strip forbidden keys
}
```

**TDD (mandatory per EC-7):**
- `test_copy_as_json_strips_tenant_id` — **MUST exist**
- `test_copy_as_json_strips_cell_id` — **MUST exist**
- `test_copy_as_json_strips_internal_arch_terms` — **MUST exist**
- `test_copy_as_json_preserves_user_facing_fields` (project name, env name, status)

#### Sub-task T-DR-3.5.2 — "Open in CLI" whitelist explícita (MUST FIX EC-8)

**Problem:** Page state pode ter tokens, API keys, session IDs — copying acidental para clipboard vaza credenciais (apps monitor clipboard).

**Implementation:**
```ts
// Whitelist EXPLICIT — only these fields go to CLI command
const CLI_COPY_WHITELIST = ['project', 'environment', 'app', 'verb', 'name'];
```

NUNCA copy raw state. Build command string strict from whitelist.

**TDD (mandatory per EC-8):**
- `test_open_in_cli_never_includes_jwt` — **MUST exist**
- `test_open_in_cli_never_includes_api_key`
- `test_open_in_cli_only_includes_whitelist_fields`

**TDD (Task base):**
- `test_top_bar_kebab_renders`
- `test_top_bar_kebab_open_in_cli_copies_command`

### T-DR-3.6 — ActionBar filter + layout-switch (closes V-4)

**Files to edit:**
- `cloud/dashboard/src/components/layout/action-bar.tsx` — add optional Filter button (icon-only) + ToggleGroup (grid/list view) — wire para DataTable consumers via prop

**TDD:**
- `test_action_bar_filter_button_renders_when_prop_set`
- `test_action_bar_layout_toggle_switches_view`

### T-DR-3.7 — Plan-gating inline pattern (closes V-6)

**Files to edit:**
- `cloud/dashboard/src/components/plan-gate.tsx` (NEW) — wrap Alert variant (post Brief #7) com gating inline pattern "Read-only mode. Upgrade to Pro to..."
- Use em Billing UpgradePro (substitui card lateral), Observability quando implementada

**TDD:**
- `test_plan_gate_renders_for_free_tier`
- `test_plan_gate_hides_for_pro_tier`

### T-DR-3.8 — CLI signature integration (ADR D4)

**Files to edit:**
- Toda page com actions — adicionar secondary section "Or use the CLI" (collapse default) com CodeBlock equivalent
- Pages: Projects (loopzero project list/create), Domains (loopzero domain add), Environments (loopzero env create), Team (loopzero team invite), Settings (já existe — confirm pattern)

#### Sub-task T-DR-3.8.1 — Rule 3 validation em CodeBlock contents (MUST FIX EC-9)

**Problem:** Dev pode escrever `loopzero argocd sync` ou `loopzero gitops push` (Rule 3 forbidden terms) acreditando ser ok porque está em CodeBlock. Hook `.claude/hooks/internal-terms.py` existe mas v1.0 plan não wire-na em DoD.

**Constraint:** Toda CodeBlock content deve passar `.claude/hooks/internal-terms.py` check. Commands em CLI signature devem usar product language only:
- ✅ `loopzero deploy`, `loopzero project list`, `loopzero env create staging`, `loopzero domain add api.example.com`, `loopzero secret set KEY=value`
- ❌ `argocd sync`, `kubectl apply`, `gitops push`, `kaniko build`, `gitea repo create`, `kubebuilder ...`

**DoD addition:**
- [ ] `.claude/hooks/internal-terms.py` rodado em CodeBlock contents de Projects/Domains/Environments/Team/Settings — zero hits
- [ ] CI job adicionado em pkg/quality/* validando CodeBlock em src/ (similar a `task quality:gates` pattern)

**TDD (mandatory per EC-9):**
- `test_projects_cli_section_uses_product_language_only` — grep test
- `test_domains_cli_section_uses_product_language_only`
- `test_environments_cli_section_uses_product_language_only`
- `test_team_cli_section_uses_product_language_only`
- `test_billing_cli_section_uses_product_language_only`
- `test_no_forbidden_terms_in_codeblock_content_across_all_pages` — aggregate test

**TDD (Task base):**
- `test_projects_cli_section_renders`
- `test_domains_cli_section_renders`
- Similar para domains/environments/team

**Acceptance:**
- Toda action page tem CLI equivalent visible inline (collapse default)
- Pattern documented em DSS v1.1 Voice section
- Sub-task 3.8.1 DoD met

### T-DR-3.9 — SSE real-time wire for status badges

**Files to edit:**
- `cloud/dashboard/src/hooks/use-sse.tsx` (NEW) — generic SSE hook (já existe em api SSE wiring per CHANGELOG)
- `cloud/dashboard/src/pages/projects.tsx`, `domains.tsx`, `environments.tsx`, `overview.tsx` — wire StatusDot updates via SSE

**TDD:**
- `test_status_updates_live_via_sse`
- `test_sse_uses_broadcast_channel_for_multi_tab` (SHOULD TEST EC-16) — single SSE connection compartilhada via BroadcastChannel (per existing pattern em `src/lib/api.ts` JWT-em-memória — DO NOT TOUCH). Evita N conexões simultâneas drainando backend.

**DoD Phase 3:**
- [ ] All 9 tasks committed
- [ ] Brief #7 primitives consumed
- [ ] cmd+k works everywhere
- [ ] Breadcrumbs in TopBar
- [ ] Notification center wired
- [ ] Sidebar grouped
- [ ] TopBar kebab works
- [ ] ActionBar filter + toggle
- [ ] Plan-gating inline pattern adopted
- [ ] CLI signature integration em 6 pages
- [ ] SSE live status badges
- [ ] Re-run density audit + loopzero-ui audit + Lighthouse — all GREEN
- [ ] Manual sponsor review — FAANG-tier achievement gate
- [ ] Brief #8 emitted ao @loopzero/ui (consumer report) similar to Brief #6 pattern

## Global DoD (v1.2 expanded with FAANG best practices)

- [ ] Phase 0, 1, 2, 3, 4, 5 todos DoD met
- [ ] 117+ → ~180+ vitest tests GREEN (estimate: +60+ tests across 6 phases including FAANG gap tests)
- [ ] Bundle ≤ 180 KB (hard gate; v1.2 adiciona ~25KB total — `@dnd-kit/sortable` 6KB + `web-vitals` 5KB + `react-intl` 10KB + minor; deve fit dentro do gate, validar no Phase 5 closure)
- [ ] Density audit re-run: 0 P1 + 0 P2
- [ ] loopzero-ui audit re-run: 100% across all 13 routes
- [ ] Lighthouse scores: Performance ≥ 90, Accessibility ≥ 95, Best Practices ≥ 95
- [ ] **Lighthouse CI gates per-page GREEN (LCP/INP/CLS/TBT per ADR D7) — NEW v1.2**
- [ ] WCAG 2.5.8 AA pass em manual a11y audit
- [ ] WCAG 2.3.3 pass (prefers-reduced-motion respeitado per EC-3)
- [ ] **Axe-core CI: zero violations em 13 routes (Phase 5 T-DR-5.3) — NEW v1.2**
- [ ] DSS bumped 1.0 → 1.2 (Motion + CLI signature pattern + Empty vs Error convention + token deprecation policy)
- [ ] Brief #7 emitted + primitives shipped + Brief #8 closure (OR EC-23 contingency activated)
- [ ] CLAUDE.md updated (se signature pattern muda product language convention)
- [ ] Vercel benchmark re-run: V-2..V-8 closed (V-1 deferred per D2)
- [ ] 6 MUST FIX sub-tasks completed (EC-3, EC-4, EC-5, EC-6, EC-7+EC-8, EC-9)
- [ ] 7 SHOULD TEST tests added to TDD GREEN (EC-10..16)
- [ ] 7 DOCUMENT items registrados em "Known risks" section
- [ ] `.claude/hooks/internal-terms.py` passa em todos CodeBlock contents (Rule 3)
- [ ] Backend pre-flight report ([`docs/reviews/backend-preflight-2026-05-26.md`](../../reviews/backend-preflight-2026-05-26.md)) commitado
- [ ] **Phase 4: 5 personalization features shipped + persisted (G11) — NEW v1.2**
- [ ] **Phase 5: 7 FAANG-grade infra tasks shipped (G1+G2+G3+G4+G5+G9+G13) — NEW v1.2**
- [ ] **RUM web-vitals reporter wired (G2, ADR D8) — NEW v1.2**
- [ ] **Ladle 8 page stories rendering (G4) — NEW v1.2**
- [ ] **Bundle analyzer HTML accessible per-route attribution (G5) — NEW v1.2**
- [ ] **i18n primitives setup: zero hardcoded English em components (G9, ADR D9) — NEW v1.2**
- [ ] **Design tokens deprecation policy DSS v1.2 (G13) — NEW v1.2**
- [ ] G10/G14/G15 explicitly deferred com mitigation documented em "Known risks"
- [ ] Sponsor manual UX review = "**FAANG-grade achieved**" verdict

## Pre-release governance

**Classification: T3** (architecture, design system, 13 pages, multi-week, brand-defining).

**Requires (per `.claude/rules/pre-release-governance.md`):**
- [ ] Meeting `MEET-2026-05-26-dashboard-faang.md` convened (renamed v1.2 — "tier 3" → "FAANG" reflete escopo expandido)
- [ ] Specialists: principal-architect, dx-engineer, production-design-reviewer, change-validation-planner
- [ ] **ADR D1-D9 ratification (D3 reverted in v1.2; D7+D8+D9 NEW v1.2 = Web Vitals + RUM + i18n primitives)**
- [ ] Plan v1.2 ratified
- [ ] Sponsor explicit approval before T-DR-1.1 commit (Phase 0 pode rodar antes do meeting per read-only nature)

**Plan Amendment Policy:**
- Per-phase gates allow scope refinement (MEET lightweight) sem full re-meeting
- DoD redefinition per-phase requires MEET + new ADR
- Task deferral allowed within phase if technical blocker; document in MEET-ASYNC
- Big-bang abandon (revert to v1.0 incremental) requires full T3 re-meeting

## Timing estimate (v1.2 updated, Phase 4-5 added)

| Phase | Duration | Risk | Output |
|-------|----------|------|--------|
| **Phase 0** | 3-5 days | Medium — backend gaps may require negotiation | 6 tasks: 5 backend verifications + 1 closure report; ZERO consumer code |
| Phase 1 | 1 week (5 days) | Low — execution of documented findings + Phase 0 scope clarity | 10 commits closing 7 P1/P2 + 5 slop fails |
| Phase 2 | 1-2 weeks | Medium — motion system + MUST FIX sub-tasks + cross-cutting (empty taxonomy + error boundaries + search UX) | DSS v1.1 + 7+ commits + Brief #7 draft |
| Phase 3 | 2-3 weeks | Medium-High — depends on @loopzero/ui Brief #7 turnaround + 5 MUST FIX sub-tasks Rule 3 + telemetry hooks | 9+ commits + Brief #8 + final review |
| **Phase 4 (NEW v1.2)** | **1-2 weeks** | **Low-Medium — reusa @loopzero/ui ThemeProvider density + theme runtime** | **5 tasks personalization (density toggle, theme switcher, sidebar reorder, recent pages, filter presets)** |
| **Phase 5 (NEW v1.2)** | **1-2 weeks** | **Low — infra setup tasks isoladas** | **7 tasks FAANG-grade infra (Lighthouse CI, RUM, Axe CI, Ladle, bundle analyzer, i18n, design tokens deprecation)** |
| **Total** | **6-9 weeks** | — | 44 tasks (Phase 0: 6, Phase 1: 10, Phase 2: 7, Phase 3: 9, Phase 4: 5, Phase 5: 7) + 2 briefs sent + DSS bump v1.0→v1.2 + Lighthouse CI gates GREEN |

## Phase 4 — Tier 4 Personalization (NEW v1.2, resolve G11 FAANG gap)

**Objetivo:** Reverter ADR D3 SKIP — implementa personalization features que Vercel/Linear/Stripe shipam day-1. Escopo modesto: density toggle, theme switcher, sidebar reorder, recently-visited memory, saved filter presets. 1-2 weeks.

### T-DR-4.1 — Density toggle in user menu (closes G11 #1)

**Files to edit:**
- `cloud/dashboard/src/components/layout/account-menu.tsx` — add density submenu (Compact/Comfortable/Spacious)
- Hook into existing `@loopzero/ui` ThemeProvider `defaultDensity` runtime (per CHANGELOG already supports)
- `localStorage` key `loopzero:user:density`

**TDD:**
- `test_density_toggle_persists_localstorage`
- `test_density_toggle_applies_to_all_components_runtime`

**Acceptance:**
- User toggle persists across page refreshes
- All loopzero-ui primitives respeitam density runtime

### T-DR-4.2 — Theme switcher in user menu (closes G11 #2)

**Files to edit:**
- `cloud/dashboard/src/components/layout/account-menu.tsx` — add theme submenu (5 themes — Violet Forge default + 4 alternates per @loopzero/ui ships)
- `localStorage` key `loopzero:user:theme`

**TDD:**
- `test_theme_switcher_persists_localstorage`
- `test_theme_switcher_applies_runtime_no_flash`

**Acceptance:**
- 5 themes selecionáveis
- No flash on theme switch (CSS vars swap instant)

### T-DR-4.3 — Sidebar reorder via drag-and-drop (closes G11 #3, Vercel pattern)

**Files to edit:**
- `cloud/dashboard/src/components/layout/app-sidebar.tsx` — adopt `@dnd-kit/sortable` for nav items reorder
- `localStorage` key `loopzero:user:sidebar_order` (array of nav item IDs)

**TDD:**
- `test_sidebar_drag_reorder_updates_localstorage`
- `test_sidebar_order_persists_across_sessions`
- `test_sidebar_default_order_when_no_localstorage`

**Acceptance:**
- Drag-to-reorder visual feedback
- Order persists
- Reset to default option in menu

**Trade-off:** `@dnd-kit/sortable` ~6KB gzipped. Cabe no bundle gate (atual 145.67/180 KB).

### T-DR-4.4 — Recently-visited memory (closes G11 #4)

**Files to edit:**
- `cloud/dashboard/src/hooks/use-recent-pages.tsx` (NEW) — tracks last 5 visited pages in `localStorage`
- `cloud/dashboard/src/components/layout/app-sidebar.tsx` — render "Recent" section (auto-populated from hook)

**TDD:**
- `test_recent_pages_caps_at_5_entries`
- `test_recent_pages_dedupes_visits`
- `test_recent_pages_persists_across_sessions`

### T-DR-4.5 — Saved filter presets (Vercel pattern, closes G11 #5)

**Files to edit:**
- `cloud/dashboard/src/components/layout/action-bar.tsx` — add "Save filter" option in ActionBar when search active
- `localStorage` key `loopzero:user:filter_presets:{page}` (array of {name, query, filters})

**TDD:**
- `test_action_bar_save_filter_creates_preset`
- `test_action_bar_load_preset_applies_query`
- `test_filter_presets_per_page_isolated`

**Acceptance:**
- "Save filter" CTA quando search + filter active
- Saved presets em ActionBar dropdown

**DoD Phase 4:**
- [ ] All 5 tasks committed
- [ ] User can: density toggle, theme switch, reorder sidebar, see recent pages, save filter presets
- [ ] All 5 features persist across sessions (localStorage)
- [ ] Bundle ≤ 180 KB (`@dnd-kit/sortable` ~6KB delta documented)
- [ ] Manual sponsor review: "personalization shipped"

---

## Phase 5 — FAANG-grade Infra (NEW v1.2, resolve G1-G5 + G9 + G13)

**Objetivo:** Performance budget formal + RUM + a11y CI + Storybook + bundle analyzer + i18n primitives + design tokens deprecation. 1-2 weeks.

### T-DR-5.1 — Core Web Vitals per-page budget (closes G1 CRITICAL, ADR D7)

**Files to edit:**
- `.lighthouserc.json` (NEW) — Lighthouse CI config com per-page thresholds (LCP/INP/CLS/TBT)
- `taskfiles/quality.yml` — `task quality:lighthouse` target rodando Lighthouse CI contra dev server
- `cloud/dashboard/docs/web-vitals-budget.md` (NEW) — per-page budget doc

**TDD:**
- `test_lighthouserc_thresholds_match_d7_spec`
- Manual CI smoke: rodar Lighthouse CI em local k3d, assert pass

**Acceptance:**
- LCP ≤ 2.5s em /, /projects, /domains, /billing
- INP ≤ 200ms em /verification (PinInput), /team
- CLS ≤ 0.1 em /
- TBT ≤ 200ms em Phase 3 cmd+k open path

### T-DR-5.2 — Real User Monitoring (RUM) (closes G2 HIGH, ADR D8)

**Files to edit:**
- `cloud/dashboard/package.json` — add `web-vitals: ^4.0.0` dep (~5KB)
- `cloud/dashboard/src/lib/rum.ts` (NEW) — wire web-vitals reporter
- `cloud/dashboard/src/main.tsx` — invoke `onLCP`, `onINP`, `onCLS` reporters

**TDD:**
- `test_rum_reports_lcp_to_console_in_dev`
- `test_rum_reports_inp_to_console_in_dev`

**Acceptance:**
- Dev mode: console.log of LCP/INP/CLS values on every interaction
- Prod mode (future): POST to `/v1/telemetry/web-vitals` endpoint (backend = future scope)

### T-DR-5.3 — Axe-core CI integration (closes G3 CRITICAL)

**Files to edit:**
- `cloud/dashboard/package.json` — add `vitest-axe: ^0.1.0` + `axe-core: ^4.10.0` devDeps
- `cloud/dashboard/src/test/axe-setup.ts` (NEW) — auto-include axe checks em vitest setup
- `cloud/dashboard/src/pages/*.test.tsx` — extend smoke tests com `axe.run(container)` per page
- Update `.claude/rules/cli-output.md` ou similar para document axe CI gate

**TDD:**
- `test_all_pages_pass_axe_no_violations` — aggregate test
- Per-page axe tests in existing smoke files

**Acceptance:**
- Zero axe violations em todas 13 routes
- CI fails on new violations

### T-DR-5.4 — Storybook/Ladle para dashboard pages (closes G4 HIGH)

**Files to edit:**
- `cloud/dashboard/package.json` — add `@ladle/react: ^4.0.0` devDep (lighter than Storybook)
- `cloud/dashboard/.ladle/config.mjs` (NEW)
- `cloud/dashboard/src/pages/*.stories.tsx` (NEW × 8 content pages) — story file per page com mock data fixtures

**TDD:**
- `npm run ladle build` succeed (no errors)
- Visual smoke: ladle dev server renders all 8 page stories

**Acceptance:**
- 8 page stories em Ladle (overview, projects, environments, domains, billing, team, settings, profile)
- Each story has loading/loaded/empty/error variants

### T-DR-5.5 — Bundle analyzer per-route attribution (closes G5 CRITICAL)

**Files to edit:**
- `cloud/dashboard/package.json` — add `rollup-plugin-visualizer: ^5.0.0` devDep
- `cloud/dashboard/vite.config.ts` — enable visualizer plugin (output `dist/bundle-stats.html`)
- `cloud/dashboard/package.json` scripts — add `"bundle:analyze": "vite build && open dist/bundle-stats.html"`

**TDD:**
- `npm run bundle:analyze` succeed + HTML generated

**Acceptance:**
- Per-route bundle attribution visible
- Identify any chunk >5KB unexpected

### T-DR-5.6 — i18n primitives architecture setup (closes G9 MEDIUM, ADR D9)

**Files to edit:**
- `cloud/dashboard/package.json` — add `react-intl: ^7.0.0` dep (~10KB)
- `cloud/dashboard/src/main.tsx` — wrap em `<IntlProvider locale="en-US" messages={enMessages}>`
- `cloud/dashboard/src/messages/en.json` (NEW) — all user-facing strings extracted
- `cloud/dashboard/src/lib/format.ts` — receive locale from context (não hardcoded `en-US`)
- `cloud/dashboard/tailwind.config.ts` — RTL support via `dir` plugin

**TDD:**
- `test_intl_provider_wraps_app`
- `test_lib_format_uses_context_locale`
- `test_rtl_class_applied_when_dir_rtl`

**Acceptance:**
- All UI strings em `messages/en.json` (zero hardcoded English em components)
- Bundle delta ~10KB (acceptable; Phase 4 + Phase 5 total deltas must fit em 180 KB gate)

### T-DR-5.7 — Design tokens deprecation strategy (closes G13 MEDIUM)

**Files to edit:**
- [`docs/design/2026-05-25-loopzero-dashboard-design-system.md`](../../design/2026-05-25-loopzero-dashboard-design-system.md) — add § 12 "Token Deprecation Policy"
- Policy: 4-tier (current → deprecated → removed-next-minor → removed) com semver bumps

**TDD:**
- N/A (doc only)

**Acceptance:**
- DSS v1.2 documenta deprecation strategy per Linear pattern
- Token changes from v1.1 → v1.2 documented em DSS migration guide

**DoD Phase 5:**
- [ ] All 7 tasks committed
- [ ] Lighthouse CI gates GREEN
- [ ] RUM reports LCP/INP/CLS in console
- [ ] Axe-core: zero violations em 13 routes
- [ ] Ladle: 8 page stories rendering
- [ ] Bundle analyzer HTML accessible
- [ ] i18n: zero hardcoded English strings em components
- [ ] DSS v1.2 with deprecation policy

---

## Known risks (documented, accepted)

Risks identificados pelo /edge-case-plan que NÃO viram MUST FIX nem SHOULD TEST — aceitos conscientemente com mitigação documentada. Cada um tem ID `EC-N` rastreável ao edge-cases.md.

### EC-17 — Overview Alerts banner deve ser conditional (post-feature ship)

- **Risco:** T-DR-1.1 substitui empty state por inline Alert banner ("Set up alerts →"). Quando feature alerts shipar, banner deveria sumir.
- **Mitigação:** Plan documenta — banner é conditional render baseado em `alerts.length === 0`. Hoje sempre mostra (OK pre-feature). Quando feature ship, adicionar conditional.
- **Acceptance:** quando T-DR-? (future alert feature) shipar, alert banner em Overview vira `{alerts.length === 0 && <Alert>...</Alert>}`.

### EC-18 — Bundle hard gate (180 KB) pode ser violado em Phase 3

- **Risco:** Plan v1.1 menciona "aceitar 10-15 KB growth justificado por novas primitives" mas hard gate é 180 KB e atual é 145.63 KB → headroom 34.37 KB. Brief #7 ship 5+ primitives (CommandPalette grande). Pode estourar.
- **Mitigação:** Phase 3 DoD INCLUI bundle measurement OBRIGATÓRIO + escalation path: se excede 180 KB, escopo Phase 3 cortado para fit. Order de corte preferido: T-DR-3.4 NotificationBell > T-DR-3.5 TopBar kebab > T-DR-3.7 plan-gating (mais redundantes vs already-shipped UX).

### EC-19 — Empty state vs Error state distinção

- **Risco:** T-DR-2.4 fala "actionable CTA OR clear waiting state" mas não distingue de error state (API 500, network fail). Ambos podem renderizar EmptyState.
- **Mitigação:** Plan documenta convenção — `<Alert variant="destructive">` reservado para errors, `<EmptyState>` somente para empty data (não-erro). DSS v1.1 documenta diferença em § 5.X.

### EC-20 — Usage panel double-scroll UX risk

- **Risco:** T-DR-2.5 adiciona scroll interno em UsageMeter (`max-h-[400px] overflow-y-auto`). Página tem scroll global → double scroll confuso em desktop.
- **Mitigação:** Keep max-h conservative (400px max), prefer scroll global quando possível. Acceptable trade-off para acomodar 5+ KPIs. Re-evaluate se sponsor review flag.

### EC-21 — Tab close durante optimistic mutation

- **Risco:** Usuário fecha tab durante invite in-flight → optimistic state perdido, backend pode ter completado ou não.
- **Mitigação:** Acceptable para invite flow (idempotent backend — duplicate invite returns same row). Document para futuros casos destructive (delete project) onde optimistic UI requires confirmation modal antes.

### EC-22 — Filter/layout state persistence

- **Risco:** T-DR-3.6 não especifica se filter (ActionBar) e layout (grid/list toggle) persiste.
- **Mitigação:** Per-page localStorage (`dashboard:projects:filter`, `dashboard:projects:layout`) — Vercel pattern. Plan documenta em T-DR-3.6 implementation note.

### EC-23 — Brief #7 timing risk (Phase 3 upstream dependency)

- **Risco:** Phase 3 depends on @loopzero/ui Brief #7 turnaround. Brief #4 took 24h, Brief #5 took 72h. Brief #7 escopo maior (5+ primitives). Realistic: 1-2 weeks upstream.
- **Mitigação:** Contingency plan — se Brief #7 turnaround > 1 week, defer T-DR-3.1 (CommandPalette) + T-DR-3.4 (NotificationBell) para v1.1 e ship Phase 3 com remaining 7 tasks. Decision gate at end-of-Phase-2 com Brief #7 status check.

### EC-?? — Native browser notifications (T-DR-3.4 scope)

- **Risco:** Native Notifications API (browser-level "Push" perm) não mencionado em plan T-DR-3.4 (in-app notification bell only).
- **Mitigação:** YAGNI pre-release. In-app notification bell suffice. Native browser notifications = v1.x feature (requires user permission flow, service worker).

### G10 (DEFERRED v1.2) — Cross-browser matrix testing

- **Risco:** Plan v1.2 só menciona viewport responsiveness (320/375/414/768), não cross-browser (Chrome/Firefox/Safari/Edge).
- **Mitigação:** Defer para post-FAANG v1.x. Acceptable pre-release per LoopZero's context (developer-facing tool, Chrome-heavy audience). Playwright cross-browser matrix vira priority se enterprise customers reportarem Safari bugs.

### G14 (DEFERRED v1.2) — Service Worker / PWA installable

- **Risco:** Vercel/Linear são PWAs installable. LoopZero dashboard não.
- **Mitigação:** Defer para post-FAANG v1.x. Workbox + offline strategy + install prompt UX = ~1 week dedicated. Não bloqueia "FAANG-tier" core (cmd+k + breadcrumb + notification + density discipline cobrem 90% do "feels FAANG" perception).

### G15 (DEFERRED v1.2) — Onboarding flow / first-run UX

- **Risco:** Stripe/Linear têm deliberate first-run UX (product tour, "Get started" checklist). LoopZero dashboard tem login → overview direto, sem onboarding moment.
- **Mitigação:** Defer para post-FAANG v1.x. CLI-first onboarding via `loopzero login` + `loopzero init` (per CLAUDE.md "CLI is the primary interface") cobre LoopZero's positioning. Dashboard onboarding flow vira priority quando temos analytics mostrando drop-off em first-session.

---

## Validation gates between phases

**Phase 0 → Phase 1 gate (NEW v1.1):**
- 5 backend verifications documented em [`docs/reviews/backend-preflight-2026-05-26.md`](../../reviews/backend-preflight-2026-05-26.md)
- 0 NEGOTIATE PRs blocking (either merged OR escalated to backend team OR sponsor-decided RESCOPE)
- T-DR-1.3, T-DR-1.4, T-DR-3.4, T-DR-3.9 scopes ratified
- Sponsor explicit go-ahead

**Phase 1 → Phase 2 gate:**
- Density audit: 0 P1 + 0 P2
- loopzero-ui audit: 288/288
- All Phase 1 commits merged to develop
- Sponsor review: "Foundational tier achieved"

**Phase 2 → Phase 3 gate:**
- Motion system shipped + DSS v1.1 published
- All transitions audited via Lighthouse + manual review
- **EC-3 verified: `prefers-reduced-motion` honored em matchMedia test**
- **EC-4 verified: optimistic UI rollback tests GREEN**
- Brief #7 sent to @loopzero/ui (early signal to upstream)

**Phase 3 → Phase 4 gate (NEW v1.2):**
- Vercel benchmark closure (V-2..V-8)
- CLI signature pattern adopted across 6 pages
- **EC-5, EC-6, EC-7, EC-8, EC-9 todos MUST FIX tests GREEN (cmd+k mount scope, breadcrumb labels, JSON sanitize, CLI whitelist, internal-terms hook)**
- Telemetry hooks wired (G12)
- Bundle ≤ 180 KB (EC-18 risk mitigated)

**Phase 4 → Phase 5 gate (NEW v1.2):**
- 5 personalization features shipped + persisted via localStorage
- `@dnd-kit/sortable` bundle delta dentro do gate
- Manual sponsor review "personalization shipped"

**Phase 5 final gate (NEW v1.2):**
- Lighthouse CI gates GREEN (LCP/INP/CLS/TBT per-page)
- RUM web-vitals reporter wired
- Axe-core CI: zero violations em 13 routes
- Ladle: 8 page stories rendering
- Bundle analyzer HTML accessible
- i18n: zero hardcoded English em components
- DSS v1.2 with deprecation policy
- Sponsor verdict: "**FAANG-grade achieved**" OR explicit deferred items (G10/G14/G15) documented

## Anti-patterns forbidden (per CLAUDE.md design-principles.md)

- ❌ Big-bang full rewrite in single sprint (D2 explicit decision)
- ❌ Copy Vercel descaradamente (D1 explicit decision)
- ❌ Implement Phase N before Phase N-1 gate cleared
- ❌ Skip ADR for any of D1-D9 (D3 reverted v1.2)
- ❌ Build features that backend doesn't support (e.g., notification center if audit_log endpoint missing)
- ❌ Bumpar @loopzero/ui version mid-phase (do it antes de Phase 1 começar)
- ❌ Add new dependency without ADR (per Rule 9 "Don't Reinvent")
- ❌ ~~Personalization features~~ — Tier 4 NOW INCLUDED per ADR D3 REVERTED v1.2 (Phase 4)
- ❌ Skip FAANG infra discipline (Web Vitals + a11y CI + RUM) — v1.2 explicit (Phase 5)
- ❌ Hardcoded English strings em components (G9 / ADR D9 — use intl primitives)
- ❌ Per-page perf without budget gate (G1 / ADR D7 — Lighthouse CI per page)

## What this plan does NOT cover (out of scope)

- Backend changes (audit_log filtering, billing data shape, etc.) — separate plan if needed
- Mobile app (not roadmap)
- Email notification template design (separate marketing concern)
- Onboarding flow redesign (separate plan if needed)
- Public website redesign (loopzerokit lives separate)

## Next steps (v1.1 updated)

1. **Sponsor review** este plan v1.1 (PROPOSED → ACCEPTED status transition)
2. Convene meeting `MEET-2026-05-26-dashboard-faang-tier-3` via `/meeting` skill
3. Ratify ADRs D1-D6 (D6 NEW = backend pre-flight gate)
4. **Begin Phase 0** (T-DR-0.1..0.6) — 3-5 dias backend pre-flight (read-only verification, ZERO consumer code)
5. **Phase 0 closure gate** — ratify backend decisions (READY/NEGOTIATE/RESCOPE per task) + sponsor go-ahead
6. **Begin Phase 1** T-DR-1.1 (após Phase 0 closure)

**Critical reminders:**
- Per `.claude/rules/pre-release-governance.md`, código de Phase 1 não pode ser commitado antes do meeting + ADRs D1-D6 ratificados. Plano sozinho não é authorization para implementar.
- **Phase 0 é READ-ONLY** (verification + documentation only). Pode rodar em parallel com meeting/ADR ratification — não consome o T3 governance gate.
- Phase 1 só desbloqueia se Phase 0 closure gate met.

## Edge-case-plan review reference

Este plan v1.2 incorpora os 23 edge cases identificados em [`docs/plans/2026-05-26-dashboard-faang-tier-3-edge-cases.md`](./2026-05-26-dashboard-faang-tier-3-edge-cases.md):
- **9 MUST FIX** → 6 sub-tasks explícitas (T-DR-2.1.1, 2.7.1, 3.1.1, 3.2.1, 3.5.1+3.5.2, 3.8.1) + Phase 0 (T-DR-0.1, 0.2, 0.3)
- **7 SHOULD TEST** → testes obrigatórios em TDD sections (EC-10..16)
- **7 DOCUMENT** → "Known risks (documented)" section (EC-17..23 + EC-?? native notifications)

Pattern sistêmico mais perigoso resolvido: **backend-prep gap** via Phase 0 obrigatório (ADR D6).

## FAANG best practices revisão (v1.2) — 15 gaps incorporados

Identificados na revisão v1.1 → v1.2 contra best practices FAANG (Vercel/Linear/Stripe patterns + Google Web Vitals):

**CRITICAL (5) → endereçados em Phase 5 ou cross-cutting:**
- **G1** Core Web Vitals per-page budget (LCP/INP/CLS/TBT) → ADR D7 + T-DR-5.1
- **G3** Axe-core CI integration → T-DR-5.3
- **G5** Bundle analyzer per-route → T-DR-5.5
- **G6** Empty state taxonomy 3-tier → T-DR-2.4 expandido
- **G7** Error boundaries per-section → T-DR-2.X cross-cutting

**HIGH (4) → endereçados em Phase 4-5:**
- **G2** Real User Monitoring (web-vitals npm pkg) → ADR D8 + T-DR-5.2
- **G4** Storybook/Ladle para dashboard → T-DR-5.4
- **G8** Search UX (debounce + virtualization) → T-DR-2.X cross-cutting
- **G11** Tier 4 personalization → ADR D3 REVERTED + Phase 4 (5 tasks)

**MEDIUM (4) → endereçados em Phase 3 ou Phase 5:**
- **G9** i18n primitives architecture → ADR D9 + T-DR-5.6
- **G10** Cross-browser matrix → DEFERRED (post-FAANG v1.x)
- **G12** Telemetry/feature usage → T-DR-3.X cross-cutting
- **G13** Design tokens deprecation strategy → T-DR-5.7

**LOW (2) → deferred to post-FAANG:**
- **G14** Service Worker / PWA → DEFERRED (acceptable per developer-tool audience)
- **G15** Onboarding flow / first-run UX → DEFERRED (CLI-first cobre)

**Coverage final v1.2:** 25 findings + 23 ECs + 15 FAANG gaps = **63 items endereçados** (43 implementados em phases + 20 explicitly deferred/documented em Known risks). Maior plan refactor da história do dashboard.
