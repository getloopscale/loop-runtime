# Dashboard FAANG-Tier 3 Refactor Plan

> **Version 1.0** — Refactor incremental do `cloud/dashboard/` em 3 phases para atingir UX-tier "Differentiation Features" alinhado a concorrentes top (Vercel/Linear/Stripe) com signature distintivo (LoopZero's own voice: CLI-first amplification + Vercel density + Linear keyboard). Cobre as 2 violações P1 + 5 P2 do density audit, os 8 V-gaps do Vercel benchmark, e os 6 fails do loopzero-ui slop test, mais 8 features novas que concorrentes têm e LoopZero não. Pre-release context. **STATUS: PROPOSED — T3 governance gate pending sponsor approval + ADR ratification.**

## Source-of-truth references (consult ANTES de assumir API)

- `docs/reviews/density-audit-2026-05-25/REPORT.md` — density audit DSS-based (2 P1 + 5 P2 + 4 P3 violations medidas em 13/13 rotas)
- `docs/reviews/density-audit-2026-05-25/vercel-benchmark.md` — comparação 1:1 com Vercel real (8 V-gaps + validação de P1s)
- `docs/design/2026-05-25-loopzero-dashboard-design-system.md` — DSS v1.0 (Violet Forge anchor, § 6.1 density targets, CC-3 empty state limit)
- `cloud/dashboard/.llms.txt` — @loopzero/ui canonical component spec (v0.11.0-next.0 → upgrade-target 0.12.0-next.0)
- `CLAUDE.md` Voice/Tone section + Cross-Project Rule 9 — CLI-first product abstraction
- `.claude/rules/pre-release-governance.md` — T3 classification + Plan Amendment Policy
- `docs/handoff/2026-05-25-loopzero-ui-cloud-dashboard-brief-5.md` + `brief-6.md` — Brief #5 primitives consumption status

## Changelog

- **v1.0 (2026-05-26):** Initial PROPOSED draft consolidando inputs de 3 audits (DSS density, Vercel benchmark, loopzero-ui slop test) + decisões estratégicas (mirror=own-voice, escopo=incremental-3-phase, tier=3-differentiation).

## Context

### O que existe hoje (2026-05-26 baseline)

**Dashboard funcional, design-system shipped:**
- 13 routes ativas (overview, projects, environments, domains, billing, team, settings, profile, login, register, recovery, verification, device-success)
- @loopzero/ui@0.11.0-next.0 pinned (npm `next` agora em 0.12.0-next.0 — additive doc-only, bump independente)
- DSS v1.0 anchored to Violet Forge (`docs/design/2026-05-25-loopzero-dashboard-design-system.md`)
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

**Dashboard atinge Tier 3 ("Differentiation Features") em 3 phases incrementais:**

1. **Phase 1 (Foundational):** Close TODOS os 7 findings documentados (2 P1 + 5 P2 + 6 slop fails). Atinge "tecnicamente correto, density-compliant, slop-test 288/288". 1 week.

2. **Phase 2 (Polish + Microinteractions):** Adiciona motion system + transitions polidas + loading states ricos + empty states contextuais. Atinge "feels alive". 1-2 weeks.

3. **Phase 3 (Differentiation):** Implementa 8 features que concorrentes têm e LoopZero não: Command Palette, Breadcrumb, Notification Center, Sidebar Grouping, TopBar Kebab, Plan-Gating Inline, CLI Signature Integration, Real-time SSE wiring. **Requires Brief #7 to @loopzero/ui team** para primitives novos (CommandPalette, Breadcrumb, NotificationBell). 2-3 weeks.

**Total: 4-6 weeks. Gate entre cada phase: re-run density audit + slop test + Lighthouse + manual UX review por sponsor.**

**Skip: Tier 4 personalization (sidebar reorder, density-per-user, custom themes per workspace) — YAGNI pre-release per CLAUDE.md design-principles.md.**

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

### D3 — Skip Tier 4 (personalization) — YAGNI pre-release

- **Context:** Personalization (sidebar reorder per user, density per user, custom workspace themes) é feature avançada de produtos com 1000+ users dando feedback divergente.
- **Decision:** Defer to v1.x pós-GA. Pre-release single-user customization é YAGNI.
- **Consequences:**
  - ✅ Foco em Tier 3 entregável em 4-6 weeks
  - ✅ Personalization ADR fica pra quando temos data real (usage analytics)
  - ❌ Concorrentes (Vercel) têm sidebar customizable — gap consciente, document

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
  - ❌ Phase 3 depends de upstream timing — risk de delay. Mitigated: Brief #7 envia early (end of Phase 2), upstream typical 24-72h response per Brief #4 evidence

## Coverage Matrix

| Gap source | Item | Phase | Task |
|------------|------|-------|------|
| Density REPORT P1-1 | Overview empty states oversized | 1 | T-DR-1.1 |
| Density REPORT P1-2 | Overview KPI count 3 → 5 | 1 | T-DR-1.2 |
| Density REPORT P2-1 | Billing Plan card sub-dimensionada | 1 | T-DR-1.3 |
| Density REPORT P2-2 | Settings sparseness (40% whitespace) | 1 | T-DR-1.4 |
| Density REPORT P2-3 | Profile Sign-out 1-button Card anti-pattern | 1 | T-DR-1.5 |
| Density REPORT P2-4 | Profile sparseness (38% whitespace) | 1 | T-DR-1.5 |
| Density REPORT P2-5 | ActionBar height 70px borderline (Projects/Domains) | 1 | T-DR-1.6 |
| Slop L-01 | projects.tsx:110 raw `<button>` | 1 | T-DR-1.7 |
| Slop C-01 | environments.tsx sem PageShell | 1 | T-DR-1.8 |
| Slop C-05 | environments.tsx Loader2 inside Card | 1 | T-DR-1.8 |
| Slop R-03 × 3 | DataTable >4 cols sem hideBelow (Projects/Domains/Environments) | 1 | T-DR-1.9 |
| Slop T-06 | billing.tsx:212 font-bold redundante | 1 | T-DR-1.10 |
| Vercel V-2 | Breadcrumb no TopBar | 3 | T-DR-3.2 |
| Vercel V-4 | ActionBar filter + layout-switch | 3 | T-DR-3.6 |
| Vercel V-6 | Plan-gating inline (read-only banner pattern) | 3 | T-DR-3.7 |
| Vercel V-8 | Usage panel scroll interno | 2 | T-DR-2.5 |
| Vercel V-1 | Sidebar sub-nav | (deferred — ADR pendente; sem rotas profundas hoje) |
| Vercel V-3 | TopBar kebab "..." menu | 3 | T-DR-3.5 |
| Vercel V-5 | Notification bell | 3 | T-DR-3.4 |
| Vercel V-7 | Sidebar visual grouping | 3 | T-DR-3.3 |
| CLI signature (D4) | CodeBlock pattern em toda page | 3 | T-DR-3.8 |
| Motion system | DSS v1.1 motion tokens | 2 | T-DR-2.1 |
| Loading polish | PageShell skeleton + optimistic UI | 2 | T-DR-2.2 |
| Empty state quality | Contextual CTAs + icon variety | 2 | T-DR-2.4 |
| Real-time SSE | Status badges live update | 3 | T-DR-3.9 |
| Command Palette (Linear signature) | cmd+k everywhere | 3 | T-DR-3.1 (requires Brief #7) |

**Coverage: 25/25 findings + signature features endereçadas.** P3 notes do density audit (DataTable mock-data sparseness) intencionalmente NÃO endereçadas — depende de data real, não defeito de design.

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

**Acceptance:**
- Density audit Profile verdict = PASS
- No 1-button Card anti-pattern

### T-DR-1.6 — ActionBar height tuning (closes P2-5)

**Files to edit:**
- `cloud/dashboard/src/components/layout/action-bar.tsx` — reduzir padding interno de `py-3` para `py-2` (~8px gain)

**TDD:**
- `test_action_bar_height_at_most_64px` — assertion via render

**Acceptance:**
- ActionBar em Projects/Domains ≤ 64px (target 56-64, max 72)

**Trade-off considerado:** Pode mexer em outras consumers (Team, Overview). Mitigated por revisão visual completa post-edit.

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
- Existing tests still PASS

**Acceptance:**
- Slop L-01 closed (grep `<button ` em projects.tsx = 0)
- Slop A-03 closed (navigation via Link, not button onClick)

### T-DR-1.8 — Environments PageShell migration (closes slop C-01 + C-05)

**Files to edit:**
- `cloud/dashboard/src/pages/environments.tsx` — full rewrite seguindo pattern T-MIG-4 (Team page): PageShell + search + loading/empty state machine. Eliminate inline `<Card><Loader2></Card>` + inline `<EmptyState>` branches
- `cloud/dashboard/src/pages/environments.test.tsx` — adjust assertions

**TDD:**
- `test_environments_uses_page_shell` — assert PageShell wrapper
- `test_environments_loading_via_page_shell_loading_prop`
- `test_environments_empty_via_page_shell_empty_prop`
- Existing tests still PASS

**Acceptance:**
- Slop C-01 + C-05 closed
- environments score 32/32

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
- `docs/design/2026-05-25-loopzero-dashboard-design-system.md` — bump to v1.1, add § 10 Motion
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
```

**TDD:**
- `test_motion_tokens_in_tailwind_preset` — assert classes generated
- DSS v1.1 doc updated

**Acceptance:**
- Motion CSS vars available globally
- Tailwind utilities `transition-base`, `transition-fast`, `ease-standard` etc.

### T-DR-2.2 — Loading transitions polish

**Files to edit:**
- `cloud/dashboard/src/pages/projects.tsx`, `domains.tsx`, `environments.tsx`, `team.tsx` — wire `<Skeleton>` rows (count=5) instead of generic spinners; opt-in via DataTable.loading + PageShell.loading
- Optimistic UI: implement in T-DR-2.7 separately

**TDD:**
- `test_projects_loading_renders_skeleton_rows` — already PASS post T-MIG-3 (DataTable.loading handles)
- Same for environments + team

**Acceptance:**
- All loading states use Skeleton primitive (closes C-05 broader pattern)

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

**TDD:**
- `test_team_invite_optimistic_adds_row_before_response`

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

**TDD:**
- `test_command_palette_opens_on_cmd_k`
- `test_command_palette_navigation_works`
- `test_command_palette_fuzzy_search`

**Acceptance:**
- cmd+k opens palette everywhere
- Top-N catalog actions wired

### T-DR-3.2 — Breadcrumb composite + TopBar wire (closes V-2)

**Files to edit:**
- `cloud/dashboard/src/components/layout/top-bar.tsx` — substituir título-só por Breadcrumb (post Brief #7)
- Para rotas shallow (1 level), Breadcrumb degenera para single title (backward compatible)
- Para rotas deep futuras (`/projects/{id}/deployments/{id}`), funciona automaticamente

**TDD:**
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

**TDD:**
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

**TDD:**
- `test_projects_cli_section_renders`
- `test_domains_cli_section_renders`
- Similar para domains/environments/team

**Acceptance:**
- Toda action page tem CLI equivalent visible inline (collapse default)
- Pattern documented em DSS v1.1 Voice section

### T-DR-3.9 — SSE real-time wire for status badges

**Files to edit:**
- `cloud/dashboard/src/hooks/use-sse.tsx` (NEW) — generic SSE hook (já existe em api SSE wiring per CHANGELOG)
- `cloud/dashboard/src/pages/projects.tsx`, `domains.tsx`, `environments.tsx`, `overview.tsx` — wire StatusDot updates via SSE

**TDD:**
- `test_status_updates_live_via_sse`

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

## Global DoD

- [ ] Phase 1, 2, 3 todos DoD met
- [ ] 117+ → ~150+ vitest tests GREEN (estimate: +30 tests across 3 phases)
- [ ] Bundle ≤ 180 KB (gate; aceitar 10-15 KB growth justificado por novas primitives)
- [ ] Density audit re-run: 0 P1 + 0 P2
- [ ] loopzero-ui audit re-run: 100% across all 13 routes
- [ ] Lighthouse scores: Performance ≥ 90, Accessibility ≥ 95, Best Practices ≥ 95
- [ ] WCAG 2.5.8 AA pass em manual a11y audit
- [ ] DSS bumped 1.0 → 1.1 (Motion + CLI signature pattern)
- [ ] Brief #7 emitted + primitives shipped + Brief #8 closure
- [ ] CLAUDE.md updated (se signature pattern muda product language convention)
- [ ] Vercel benchmark re-run: V-2, V-3, V-4, V-5, V-6, V-7, V-8 closed (V-1 deferred per D2)
- [ ] Sponsor manual UX review = "FAANG-tier achieved" verdict

## Pre-release governance

**Classification: T3** (architecture, design system, 13 pages, multi-week, brand-defining).

**Requires (per `.claude/rules/pre-release-governance.md`):**
- [ ] Meeting `MEET-2026-05-26-dashboard-faang-tier-3.md` convened
- [ ] Specialists: principal-architect, dx-engineer, production-design-reviewer, change-validation-planner
- [ ] ADR D1-D5 ratification
- [ ] Plan version bump v1.0 → v1.1 post-ratification
- [ ] Sponsor explicit approval before T-DR-1.1 commit

**Plan Amendment Policy:**
- Per-phase gates allow scope refinement (MEET lightweight) sem full re-meeting
- DoD redefinition per-phase requires MEET + new ADR
- Task deferral allowed within phase if technical blocker; document in MEET-ASYNC
- Big-bang abandon (revert to v1.0 incremental) requires full T3 re-meeting

## Timing estimate

| Phase | Duration | Risk | Output |
|-------|----------|------|--------|
| Phase 1 | 1 week (5 days) | Low — execution of documented findings | 10 commits closing 7 P1/P2 + 5 slop fails |
| Phase 2 | 1-2 weeks | Medium — motion system is new abstraction | DSS v1.1 + 7 commits + Brief #7 draft |
| Phase 3 | 2-3 weeks | Medium-High — depends on @loopzero/ui Brief #7 turnaround | 9 commits + Brief #8 + final review |
| **Total** | **4-6 weeks** | — | 26 commits + 2 briefs sent + 1 brief published + DSS bump + audit re-runs |

## Validation gates between phases

**Phase 1 → Phase 2 gate:**
- Density audit: 0 P1 + 0 P2
- loopzero-ui audit: 288/288
- All Phase 1 commits merged to develop
- Sponsor review: "Foundational tier achieved"

**Phase 2 → Phase 3 gate:**
- Motion system shipped + DSS v1.1 published
- All transitions audited via Lighthouse + manual review
- Brief #7 sent to @loopzero/ui (early signal to upstream)

**Phase 3 final gate:**
- Vercel benchmark closure (V-2..V-8)
- CLI signature pattern adopted across 6 pages
- Sponsor verdict: "FAANG-tier achieved" OR explicit deferred items documented

## Anti-patterns forbidden (per CLAUDE.md design-principles.md)

- ❌ Big-bang full rewrite in single sprint (D2 explicit decision)
- ❌ Copy Vercel descaradamente (D1 explicit decision)
- ❌ Implement Phase N before Phase N-1 gate cleared
- ❌ Skip ADR for any of D1-D5
- ❌ Build features that backend doesn't support (e.g., notification center if audit_log endpoint missing)
- ❌ Bumpar @loopzero/ui version mid-phase (do it antes de Phase 1 começar)
- ❌ Add new dependency without ADR (per Rule 9 "Don't Reinvent")
- ❌ Personalization features (Tier 4 explicitly skipped per D3)

## What this plan does NOT cover (out of scope)

- Backend changes (audit_log filtering, billing data shape, etc.) — separate plan if needed
- Mobile app (not roadmap)
- Email notification template design (separate marketing concern)
- Onboarding flow redesign (separate plan if needed)
- Public website redesign (loopzerokit lives separate)

## Next steps

1. **Sponsor review** este plan (PROPOSED → ACCEPTED status transition)
2. Convene meeting `MEET-2026-05-26-dashboard-faang-tier-3` via `/meeting` skill
3. Ratify ADRs D1-D5
4. Bump plan v1.0 → v1.1 com meeting outcomes
5. Begin Phase 1 T-DR-1.1 (após approval)

**Critical reminder:** per `.claude/rules/pre-release-governance.md`, código de Phase 1 não pode ser commitado antes do meeting + ADRs ratificados. Plano sozinho não é authorization para implementar.
