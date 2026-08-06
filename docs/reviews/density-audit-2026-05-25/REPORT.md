---
type: review
title: "Dashboard Density Audit — Full Tour"
---

# Dashboard Density Audit — Full Tour

**Data:** 2026-05-25
**Skill:** `/dashboard-density-audit` (sem argumento → 13 rotas)
**Viewport:** 1440×900
**Browser:** Chrome 145.0.7632.159 headless
**Mock layer:** `cloud/dashboard/src/lib/dev-mocks.ts` (VITE_USE_MOCKS=true; VITE_MOCK_LOGGED_OUT=true para auth routes)
**Screenshots:** `docs/reviews/density-audit-2026-05-25/{route}-density.png` (13 PNGs)
**DSS reference:** [`docs/design/2026-05-25-loopzero-dashboard-design-system.md`](../../design/2026-05-25-loopzero-dashboard-design-system.md) § 6.1 (densidade) + § 4.7 (KPI strip) + § 5.2 (3 patterns) + CC-3 (cross-cutting: 1 empty-state max por página)

---

## Resumo Executivo

- Rotas auditadas: **13/13** (full tour)
- Pages over budget: **4/13** (Overview, Billing, Settings, Profile)
- Pages within budget: **9/13** (Projects, Environments, Domains, Team + 5 auth: login, register, recovery, verification, device-success)
- Violações: **0 P0 · 2 P1 · 5 P2 · 4 P3**
- Pattern dominante: **empty states + cards sub-dimensionados em páginas de baixa densidade informacional** (não affeta DataTable pages quando há dados reais)

**Veredicto cross-tour: NEEDS FIX** — 2 violações P1 documentadas com fix concreto + suficiente para alimentar plano de refactor.

---

## Tabela Cross-Route

| Route | Pattern (DSS § 5.2) | Verdict | Worst violation |
|-------|---------------------|---------|-----------------|
| `/` (Overview) | KPI + 2-asymmetric panels | **NEEDS FIX** | 2 empty states (CC-3 viola: max 1) + KPI count 3 vs target 4-5 |
| `/projects` | Table | **PASS*** | ActionBar borderline 70px; sparse com mock data |
| `/environments` | Table | **PASS*** | Sem primary CTA; 61% whitespace mock |
| `/domains` | Table expandable | **PASS*** | ActionBar borderline; 71% whitespace mock |
| `/billing` | KPI + panels | **NEEDS FIX** | Plan card 130px (UNDER 280-360 target); 35% whitespace |
| `/team` | Table | **PASS*** | 47% whitespace mock |
| `/settings` | Form/sparse | **NEEDS FIX** | 40% whitespace; só 2 cards (cli-first + danger zone) |
| `/profile` | Form/sparse | **NEEDS FIX** | "Sign out" wrapped em Card 45px (anti-pattern); 38% whitespace |
| `/login` | Form-only | **PASS** | — density apropriada |
| `/register` | Form-only | **PASS** | — density apropriada |
| `/recovery` | Form-only | **PASS** | — density apropriada |
| `/verification` | PinInput post-T-MIG-1 | **PASS** | ✓ 6 boxes correctly rendered with focus ring |
| `/device-success` | Confirmation | **PASS (N/A)** | Sparseness intencional pós-CLI-auth |

\* PASS condicional: density "ok com dataset real". Tabelas com mock data parecem sparse; mas o design da tabela em si é compacto. Não classifico como violação porque é artefato do mock, não defeito de layout.

---

## Surfaces medidas (cross-route)

| Surface | Observado | Target DSS § 6.1 | Verdict global |
|---------|----------:|------------------|----------------|
| TopBar | **~56px** | 56px fixed | ✓ Universal |
| Sidebar item | **~36-40px** | 36-40 (max 44) | ✓ Universal |
| ActionBar (Projects/Domains) | **~70px** | 56-64 (max 72) | ⚠ Borderline (within max) |
| ActionBar (Overview/Team) | **~55-60px** | 56-64 (max 72) | ✓ |
| Table header row | **~25px** | — (auxiliary) | ✓ |
| Table row (single-line) | **~30-35px** | 40-44 (max 48) | ✓ (denser is OK) |
| Plan card (Billing) | **~130px** | 280-360 (panel) | **✗ UNDER min by 150px** |
| Sign out Card (Profile) | **~45px** | 280-360 (panel) | **✗ Anti-pattern: 1-button Card** |
| Empty state | **270px (Alerts) / 240px+ (Recent Previews)** | ≤200px (CC-3 max 1/page) | **✗ P1 — Overview only** |
| USAGE panel (Billing) | **~240px** | 280-360 | ⚠ UNDER min by 40px (aceitável) |
| Account card (Profile) | **~260px** | 280-360 | ⚠ UNDER min by 20px (aceitável) |
| CLI-First panel (Settings) | **~265px** | 280-360 | ⚠ UNDER min by 15px (aceitável) |
| Auth card (login/register) | **390×645 / 390×625** | N/A (auth pattern) | ✓ |
| Verification PinInput | **6 boxes ~55×55px** | DSS Brief #5 spec | ✓ |

---

## Violações classificadas

### P1 — Bloqueia visual signoff

#### P1-1: Overview tem 2 empty states, ambos oversized (CC-3 violation)
- **Route:** `/`
- **Screenshot:** `overview-density.png`
- **Surfaces afetadas:**
  - ALERTS empty state: ~270px (target ≤200, **excess 70px**)
  - RECENT PREVIEWS empty state: ≥240px (cortado por viewport, target ≤200, **excess ≥40px**)
- **DSS reference:** CC-3 cross-cutting (1 empty state max por página) + § 6.1 (empty state ≤200px)
- **Fix concreto:**
  - Consolidar ALERTS empty state como **inline banner** no header da Usage panel ("Set up alerts →" link em vez de full Card)
  - Mover RECENT PREVIEWS para uma tab dentro do Projects panel OU eliminar até existir backend (preview list é vazio sem feature)
  - OU: substituir AMBOS por uma única `<EmptyState>` no rodapé pequena ("Alerts and previews coming soon →")

#### P1-2: Overview KPI count 3 vs target 4-5
- **Route:** `/`
- **Surface:** USAGE panel (3 metrics: Fast Data Transfer, Fast Origin Transfer, Edge Requests)
- **DSS reference:** § 4.7 + § 5.2 #1 (KPI strip: 4-5 KPIs preferred)
- **Fix concreto:**
  - Adicionar 2 KPIs reais que já existem em backend: **Build Minutes** (visível em /billing como `145/400`) + **Concurrent Builds** (`2/4`). Ambos já fluem de `/v1/billing.usage` — basta adicionar 2 entries no `usageMetrics` map em `overview.tsx`.
  - Alternativa cheaper: render KPI strip horizontal acima do 2-asymmetric grid (4-5 small cards em uma linha), separando "header KPIs" das panels detalhadas

### P2 — Sparseness ou anti-pattern (não bloqueia, mas pollui)

#### P2-1: Billing — Plan + Upgrade cards sub-dimensionadas
- **Route:** `/billing`
- **Surfaces:**
  - "CURRENT PLAN" card: ~130px com 3 info items (plan name + tier badge + team name) em ~520px wide → 60% vertical desperdiçada
  - "Upgrade to Pro" card: similar
- **DSS reference:** § 6.1 (panel card target 280-360px)
- **Fix concreto:**
  - Adicionar **rich content** ao Plan card: lista de quotas inline (3 lines: "10 projects · 3 environments · 1000 builds/month"), data do próximo billing, payment method last4
  - Upgrade card: adicionar comparação visual "What you'll get" (3-line bulleted list) ou screenshot/logo + 2 testimonial points
  - Alternativa cheaper: trocar layout 2-col por 1-col full-width "Plan summary" card empilhando Plan + Upgrade vertically, eliminando o whitespace lateral

#### P2-2: Settings — só 2 cards (CLI-first + DangerZone), 40% whitespace abaixo
- **Route:** `/settings`
- **DSS reference:** § 5.2 pattern 3 (form-only/settings) implica MAIS controles que CLI reference + delete
- **Fix concreto:**
  - Adicionar workspace controls que DSS § 9.1 não obriga estar em CLI: **Timezone**, **Default environment**, **Notification preferences**, **API key management** (rotation, list, create)
  - Adicionar **Webhook URLs** panel (project-level config visualization)
  - Trade-off documented: CLI-first é principal MAS dashboard pode ter convenience config (RBAC labels, audit log link, team avatar upload)

#### P2-3: Profile — "Sign out" wrapped em Card de ~45px
- **Route:** `/profile`
- **Surface:** Sign out Card (45px Card com 1 botão centered)
- **DSS reference:** § Components — anti-pattern de 1-button Card sem grouping value
- **Fix concreto:**
  - Mover "Sign out" button para o header do Account card (ao lado direito, alinhado com o nome — Vercel/Linear pattern)
  - OU: agrupar Sign out + outras account actions ("Manage sessions", "Download my data", "Two-factor auth setup") em um único Card "Security & Sessions" com ≥3 actions

#### P2-4: Profile — 38% whitespace abaixo
- **Route:** `/profile`
- **DSS reference:** § 6.1 + CC-3 sparseness
- **Fix concreto:**
  - Adicionar **Recent activity** panel (audit log slice: last 5 actions by this user)
  - Adicionar **Connected apps** panel (GitHub, Google OAuth links — useful for revoke)
  - Esses dados já existem (audit log endpoint + OAuth provider info from Kratos identity)

#### P2-5: ActionBar height 70px em DataTable pages (borderline)
- **Routes:** `/projects`, `/domains`
- **Surface:** ActionBar (search + CTA)
- **DSS reference:** § 6.1 (target 56-64px, **max 72px**)
- **Fix concreto:**
  - Reduzir padding interno do ActionBar wrapper de `py-3` para `py-2` (~8px gain)
  - OU: aceitar como design intencional (DataTable pages têm mais ações visíveis) e bumpar target DSS para max 76px

### P3 — Notes (low severity, document or design call)

- **P3-1 — Projects sem h1 body:** Pós-T-MIG-3 (DataTable migration), Projects page perdeu o `<h1>Projects</h1>` no body — só aparece no TopBar center. Hierarchy fraca para screen readers + para usuários que não associam TopBar com page title. Adicionar PageShell ou h1 inline em commit separado (T-MIG-3 trade-off documentado).
- **P3-2 — Environments sem primary CTA:** Não tem "Add Environment" button. Se é CLI-only intencional, adicionar inline note ("Create environments via `loopzero env create` →") no ActionBar.
- **P3-3 — DataTable pages com 60-71% whitespace abaixo:** Artifact do mock dataset (4-6 rows). Não viola DSS. Não-issue com dados reais (10+ rows). Documentar como "dataset-dependent" para próximos auditors não falsos-positive.
- **P3-4 — VITE_MOCK_LOGGED_OUT screenshots não atravessam todos sub-states:** Recovery captou phase 1 (email submission), não phase 2 (code submission). Verification captou phase initial, não success state. Audit cobre "first paint" — sub-states ficam para próximo audit dedicado.

---

## Cross-route findings

### Padrão dominante: "Páginas form/settings ficam sparse"

4/13 pages (Overview, Billing, Settings, Profile) sofrem do mesmo modo: layouts pensados para "dashboard rico" renderizados com pouco data. Comum em SaaS pré-launch (mock data fino, features ainda não shipped).

**Recomendação:** Tratar Settings + Profile + Billing como **work-in-progress dashboards** — adicionar workspace controls reais conforme features chegam, sem rewrite de layout. Overview precisa de fix mais imediato (2 empty states + KPI count).

### O que está OK

- ✓ TopBar + Sidebar item heights universalmente dentro do DSS
- ✓ Tables compactas (denser-than-target é OK)
- ✓ Auth pages todas density-appropriate (compact, focused, no waste)
- ✓ Brief #5 primitives renderizando correctamente: PinInput (6 boxes + focus ring), DataTable (sortable, expandable, sticky), PageShell (title + state machine + ActionBar consolidation)
- ✓ Status badges com DSS variants consistentes (Live/Building/Failed/Active/Protected colorings)

### Recomendação next-step

Este audit é o **insumo medido** para o plano de density refactor (`docs/plans/completed/2026-05-26-dashboard-density-refactor-plan.md` — **ainda não criado**). Próximos passos sugeridos:

1. Invocar `/to-plan` com este REPORT.md + os 13 screenshots como contexto, gerando o plano de density-refactor com tasks T-DR-1..T-DR-N enderecando os 2 P1s + 5 P2s
2. Alternativa: `/edge-case-plan` para stress-test o plano antes de implementar
3. Implementar via T-DR-N commits seguindo o mesmo padrão T-MIG-N de Brief #5 follow-up

---

## Evidência

Todos os 13 screenshots em `docs/reviews/density-audit-2026-05-25/`:

```
billing-density.png         52,582 bytes
device-success-density.png  14,055 bytes
domains-density.png         60,882 bytes
environments-density.png    52,905 bytes
login-density.png           32,158 bytes
overview-density.png        86,272 bytes  ← largest (2 empty states + 3 KPIs + 3 projects)
profile-density.png         51,109 bytes
projects-density.png        60,346 bytes
recovery-density.png        22,189 bytes
register-density.png        33,118 bytes
settings-density.png        56,836 bytes
team-density.png            63,508 bytes
verification-density.png    16,896 bytes  ← smallest (6 PinInput boxes)
```

Total: **603,856 bytes** (13 PNGs at 1440×900 sem scrollbars).

---

## Teardown

- ✓ Vite background process killed (PID 57475)
- ✓ Port 5173 freed
- ✓ Chrome profile dirs removed (`/home/paulo/chrome-density-prof/`)
- ✓ Read-only audit: zero state mutation no codebase ou infra

---

**Status do Ralph loop referente ao plano original:**

O plano `docs/plans/completed/2026-05-26-dashboard-density-refactor-plan.md` referenciado pelo loop **NÃO existe** (verificado por `find docs -name "*density*"` + `git log --all`). Este audit é o pré-requisito para CRIAR esse plano — não é o plano em si. Próxima ação requer `/to-plan` (consciente decisão do operator, não auto-execução).
