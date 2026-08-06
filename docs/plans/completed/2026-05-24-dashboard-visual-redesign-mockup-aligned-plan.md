---
type: plan
title: "Plan: Dashboard Visual Redesign — Mockup-Aligned (Overview literal + design language)"
status: deprecated
---

# Plan: Dashboard Visual Redesign — Mockup-Aligned (Overview literal + design language)

> **Version 1.2** — Redesign visual completo do `cloud/dashboard/` alinhado ao mockup canônico em `cloud/dashboard/design/loopzero_overview_dashboard_mockup.html` E coerente com a documentação canônica de `@loopzero/ui` em `cloud/dashboard/.llms.txt` (espelho local de `https://docs.loopzero.example/llms.txt`, v0.9.0-next.0). Implementa OverviewPage com **alta fidelidade ao mockup** (pixel-close), extrai design language em doc curado, e aplica o pattern nas outras 11 pages. Consome infraestrutura preparada pelo plano `2026-05-23-dashboard-loopzero-ui-migration v1.3` (Phase 0 ✅ DONE + Phase 1 mecânica próxima). **SUBSUME v1.3 Phases 2, 3, 4, 5**. Resultado: UX coerente com identidade visual Vercel-style + uso canônico das primitivas conforme `.llms.txt`.

## Source-of-truth references (consult ANTES de assumir API)

Per `.llms.txt` line 5: "Treat the bullets below as **non-negotiable contracts** unless the source files contradict them — in which case the source files win and this file is stale."

Ordem de precedência:

1. **`cloud/dashboard/.llms.txt`** (local mirror, sync 2026-05-24) — primary contract
2. **`node_modules/@loopzero/ui/dist/index.d.ts`** — types as truth when llms.txt ambíguo
3. **`https://docs.loopzero.example/loopzeroui/{primitives,infra,code,agent}/{slug}`** — MDX pages with examples per component
4. **`node_modules/@loopzero/ui/dist/index.js`** source — last resort

Anti-pattern desta sessão (corrigido v1.2): grep + node REPL probes ad-hoc sem consultar `.llms.txt` primeiro.

## Changelog

- **v1.2 (2026-05-24, revisão coesa com .llms.txt)** — 10 divergências identificadas e corrigidas após download local do `.llms.txt` como referência canônica:
  - **D-1 CSS imports**: T-A.0 (NEW) verifica se `@loopzero/ui/preset` único import substitui 3 imports atuais (`fonts-cdn.css` + `tokens.css` + `styles.css`); se preset.css inclui equivalente, simplificar src/index.css. Se NÃO (preset.css é só Tailwind v4 layer), manter 3 imports com justificativa documentada.
  - **D-2 Card sub-component naming**: v1.3 T1.3 (Card swap) DEVE renomear `CardHeader → Card.Header`, `CardContent → Card.Body`, `CardTitle → Card.Title`, `CardDescription → Card.Description` em todos os ~30 sites. Plano v1.2 referencia este pattern explicitamente em T-D.1, T-E.x onde Card aparece.
  - **D-3 Bundled deps anti-pattern**: revisão de D4 v1.3 — `.llms.txt` line 72 explicita "Already bundled (do NOT install separately): @radix-ui/*, class-variance-authority, clsx, cmdk, lucide-react, tailwind-merge". v1.3 T6.1 (cleanup) deve remover TODOS esses do package.json direto, confiando em transitive de @loopzero/ui. Excepção: `framer-motion` permanece (NÃO bundled em @loopzero/ui).
  - **D-4 LoopZeroUIProvider acknowledged superset**: `.llms.txt` line 84 documenta apenas `<ThemeProvider initial="violet-forge">`. Nosso T0.2 commit (eeb030d3) usa `<LoopZeroUIProvider>` (verificado via .d.ts — composes ThemeProvider+Toaster). Per `.llms.txt` line 268 "source files win" — LoopZeroUIProvider existe em source; .llms.txt é stale neste ponto. Plan v1.2 documenta isso explicitamente em ADR (NEW D9).
  - **D-5 Component count terminology**: "14 primitives" → "14 components (10 primitives + 4 composites)". Correção em changelog + cross-reference + texto de motivação.
  - **D-6 docs.loopzero.example MDX pages como spec autoritativa**: T-D.1 (Overview), T-C.1 (Sidebar/AccountMenu), T-E.* (composites) referenciam URLs específicas (`/loopzeroui/infra/account-menu`, `/loopzeroui/infra/usage-meter`, etc.) como segunda fonte ao lado de .llms.txt.
  - **D-7 SVG inline removal explicit**: T-G.1 (Login) adiciona task step explícito "remover SVG inline do Google logo (login.tsx:200) — substitui por `<SocialAuthRow>` primitive ou Lucide icon". Anti-pattern .llms.txt line 118 (Never inline SVG).
  - **D-8 .llms.txt como source-of-truth header**: este plano adicionou seção "Source-of-truth references" topo deste plano.
  - **D-9 Anti-pattern validation gate**: T-H.2 estendida com grep adicional para anti-patterns .llms.txt (raw hex, inline SVG, manual portals, eslint-disable sem WAI-ARIA justificativa).
  - **D-10 Quality gates note**: plan adiciona nota lateral em T-H sobre `pnpm quality:gates` (library-side, não roda no consumer, mas registry validation pode acontecer se publicarmos custom components futuro).

- **v1.1 (2026-05-24)** — Incorpora 7 MUST FIX do `/edge-case-plan` review ([`docs/plans/2026-05-24-dashboard-visual-redesign-mockup-aligned-edge-cases.md`](./2026-05-24-dashboard-visual-redesign-mockup-aligned-edge-cases.md)):
  - **EC-1** (T-C.1): Sidebar.Item polymorphism fallback documented — se `as={Component}` não funciona, wrap manual com react-router Link para preservar SPA navigation (D3 v1.3 JWT em memória)
  - **EC-2** (T-B.2 + T-C.3): pageTitle prop mechanism via Context provider `usePageMeta()` hook — DashboardLayout provê context, pages chamam `setPageTitle()` em useEffect
  - **EC-3** (T-C.2): novo `src/hooks/use-projects.ts` SWR-style cache (60s TTL) para evitar dupla chamada `/api/v1/projects` (TopBar + Overview)
  - **EC-4** (T-D.1 + T-E.3): pre-step verifica response shape REAL de `/v1/billing` (cloud BillingHandler GetBilling) antes de assumir UsageResponse — placeholders zerados se metrics não existem
  - **EC-5** (T-E.3): pre-step verifica existência de `/v1/billing/invoices` ou similar; se NÃO existe, BillingPage v1 omite invoice Table — deferido para plano sucessor
  - **EC-6** (Phase D-G + T-H.2): migração `text-white` → `text-foreground`, `bg-surface-*` → tokens semantic. T-H.2 grep estendido detecta legacy tokens remanescentes
  - **EC-7** (T-D.1): code example fix — adicionar `searchQuery` useState + filter para que test `test_overview_search_input_filters_projects` tenha implementation correspondente

## Context

### O que existe hoje (2026-05-24)

**Foundation técnica pronta** (commits da Phase 0 de v1.3):

- `@loopzero/ui@0.9.0-next.0` instalado e pinado (commit `6534af96`)
- Tailwind v4.3 migrado via codemod oficial (commit `4ec9f571`)
- `LoopZeroUIProvider` envolvendo `<App />` em `main.tsx` (commit `eeb030d3`) — provê ThemeProvider (themes=builtinThemes, defaultMode=dark) + Toaster + density context
- CSS imports em `src/index.css`: `@loopzero/ui/fonts-cdn.css` (Geist Sans + Mono) + `tokens.css` + `styles.css`
- `class="dark"` removido de html + 2 divs em `app.tsx` (LoopZeroUIProvider gerencia)
- Bundle baseline capturado em `docs/reviews/2026-05-23-dashboard-bundle-baseline.txt`

**Estado visual atual** (verificado via `cloud/dashboard/src/pages/*.tsx`):

- OverviewPage: 113 LOC, 3 stat tiles hand-rolled + recent deploys (lista vazia)
- ProjectsPage: 92 LOC, project rows hand-rolled
- 12 pages routed em `app.tsx` + 5 órfãs (account/error/recovery — recovery será roteada via v1.3 T1.4)
- Sidebar hand-rolled (109 LOC), nav items hardcoded
- PageWrapper hand-rolled (30 LOC), só título + descrição
- Cada page com Tailwind ad-hoc + Framer Motion variants inline
- **Zero coerência cross-page** além do dark surface e accent verde

**Mockup canônico** (`cloud/dashboard/design/loopzero_overview_dashboard_mockup.html`, 90 linhas HTML):

Layout Vercel-style:

```
┌──────────────────┬─────────────────────────────────────────────────┐
│ SIDEBAR (200px)  │ TOP BAR ("All Projects ▾" + Overview + ⋯)       │
│ ┌──────────────┐ ├─────────────────────────────────────────────────┤
│ │ ◯ user [Hobby]│ │ Search... [⚙] [Add New ▾]                       │
│ └──────────────┘ ├──────────────────────┬──────────────────────────┤
│ Find...          │                      │                          │
│ ─ Projects ◀     │ USAGE (Last 30 days) │ PROJECTS                 │
│ ⬚ Deployments    │ ────────────────     │ ┌─ project-u7r43        │
│ ▭ Logs           │ Fast Data 0/100GB    │ │  status: needs git    │
│ 📈 Analytics      │ ████████████        │ │  24h ago               │
│ ⏱ Speed Insights │ Fast Origin 0/10GB   │ └─                       │
│ 👁 Observability │ ███████████          │                          │
│ 🛡 Firewall      │ Edge Requests 0/1M   │ RECENT PREVIEWS          │
│ 🌐 CDN           │ ████████████        │ ┌─ (empty state)         │
│ ─────────        │                      │ │  No previews yet       │
│ Env Variables    │ ALERTS (empty state) │ └─                       │
│ Storage          │ Get alerted for...   │                          │
│ 🤖 AI Gateway    │ [Upgrade to Pro]     │                          │
│ Workflows        │                      │                          │
│ ─────────        │                      │                          │
│ ⏱ Usage          │                      │                          │
│ ❓ Support       │                      │                          │
│ ⚙ Settings       │                      │                          │
└──────────────────┴──────────────────────┴──────────────────────────┘
```

Regions identificadas: Sidebar (account header + search + nav + footer), Top bar (project switcher + title + actions), Action bar (search + filter + CTA), 2-col content grid (1.2fr:1fr), Cards (Usage/Alerts/Projects/Recent Previews).

### O que motiva o trabalho

1. **Sponsor explicit ask** (2026-05-24): "Você criou um plano para REFAZER A TELA CONFORME NO NOVO PADRÃO?" — apontou gap real onde v1.3 só MIGRA primitives sem redesenhar visual.
2. **Mockup é a tese estratégica do projeto**: a decisão de adotar `@loopzero/ui` foi justamente para entregar este nível de polimento UX consistente.
3. **Sprint 9 gate**: "polish visual GA-ready" — sem mockup-aligned redesign, v1.3 entrega só "stack moderna", não "produto GA-ready visualmente".
4. **YAGNI/anti-waste**: fazer v1.3 Phase 4-5 (1:1 migration) e DEPOIS redesenhar seria literalmente reescrever 8 pages duas vezes. Opção A do trade-off resolve isso direto.

### Evidência da análise mockup ↔ inventário @loopzero/ui

Walkthrough confirmou ~95% das regions do mockup têm primitive/composite direto em `@loopzero/ui@0.9.0-next.0`:

| Mockup region | @loopzero/ui component | Disponibilidade |
|---|---|---|
| Sidebar shell (200px) | `Sidebar` (Header/Section/Item/Footer) | ✅ primitive |
| Account header | `AccountMenu` + `PlanBadge` | ✅ composites (Brief #1) |
| Sidebar search | `Input` + Lucide Search icon | ✅ primitive |
| Nav items + active state | `Sidebar.Item` (icon/active/count props) | ✅ sub-component |
| Top bar dropdown "All Projects ▾" | `Select` ou `ProjectSwitcher` | ✅ primitive |
| Page title centralizado | `topnav` primitive | ✅ primitive |
| Dots menu | DropdownMenu (Radix bundled) | ✅ via @loopzero/ui |
| Action bar Search | `Input` | ✅ primitive |
| Filter icon button | `Button` (icon variant) | ✅ primitive |
| "Add New ▾" purple | `Button` (primary) | ✅ primitive |
| Usage card 3 progress bars | `UsageMeter` (multi-metric) | ✅ composite (Brief #1) |
| Single progress bar | `Progress` (4 intents + indeterminate) | ✅ primitive (Brief #1) |
| Alerts empty state | `EmptyState` + `Button` CTA | ✅ primitive |
| Projects card row | `ProjectCard` composite | ✅ composite |
| Status dot orange | `StatusDot` (status="warning") | ✅ primitive (Brief #2) |
| Recent Previews empty | `EmptyState` (eye icon variant) | ✅ primitive |
| Timestamps "24h ago" | `Timestamp` (relative format) | ✅ primitive (Brief #2) |

**Zero gaps fundamentais**. As outras 11 pages reaproveitam mesma paleta de primitivas + pattern language extraído.

## Objective

**Done = OverviewPage renderiza pixel-close ao mockup `loopzero_overview_dashboard_mockup.html` em browser dark mode, e as outras 12 routes (Projects, Environments, Team, Billing, Domains, Settings, Profile, Login, Register, Verification, Recovery, DeviceSuccess) aplicam o mesmo design language (sidebar, top bar, action bar, 2-col grid, card styling, typography Geist) sem regressão funcional em auth/JWT/Kratos/BroadcastChannel, e Rule 3 grep retorna zero termos internos.**

Metas mensuráveis:

1. **Pattern language extraído**: [`docs/design/2026-05-24-dashboard-design-language.md`](../../design/2026-05-24-dashboard-design-language.md) doc curado (≤300 LOC) descrevendo 8 patterns reusable (Layout, Sidebar, TopBar, ActionBar, ContentGrid, Card, StatusDot, Timestamp).
2. **OverviewPage fidelidade**: visual delta ≤5% vs mockup (verificável via screenshot diff + spec compliance checklist).
3. **Cross-page coerência**: 100% das pages consomem `<DashboardLayout>` shell e seguem ActionBar/ContentGrid patterns onde aplicável.
4. **LOC reduction**: dashboard atual 2.914 LOC → target ≤1.400 LOC (-52% revisado vs -49% de v1.3). Mockup aligned reduz mais porque elimina TODO o styling ad-hoc duplicado.
5. **Reusable layout components**: 3 novos componentes locais (`DashboardLayout`, `ActionBar`, `ContentGrid`) com testes vitest cobrindo composição.
6. **Auth pages coerência**: Login/Register/Verification/Recovery/DeviceSuccess seguem login-split ou clean form pattern com Geist + accent violet/teal.
7. **Bundle delta** vs T0.0 baseline: target ≤+30% gzip pós-Phase H (medido em v1.3 T6.3 após cleanup).
8. **Rule 3 clean**: zero termos `Kubernetes|ArgoCD|Gitea|MinIO|Cosign|Pulumi|Helmfile|Tilt|Hydra|Kratos|cell` em DOM renderizado.

## ADRs

### D1 — Mockup como inspiração visual, sidebar items das rotas REAIS do LoopZeroCloud

- **Decisão**: Layout, spacing, paleta, account-header-com-plan-badge, 2-col grid, action-bar pattern vêm do mockup. Sidebar items são as 12 routes reais do LoopZeroCloud (Overview/Projects/Environments/Team/Billing/Domains/Settings/Profile + Login/Register/Verification/Recovery/DeviceSuccess agrupadas em "auth"). NÃO inventar AI Gateway/Firewall/CDN/Speed Insights/Analytics/Logs/Storage/Workflows do template Vercel — features que LoopZeroCloud não tem.
- **Rationale**: Sponsor decidiu 2026-05-24 (segunda pergunta). Coerente com CLAUDE.md Rule 9 ("Não Reinvente a Roda" — corolário: não invente features). Coerente com Cross-Project Rule 9 (Cloud abstraction sem termos internos). Inventar features confunde usuário e viola pre-release honesty (não claim o que não existe).
- **Consequences**:
  - ✅ Sidebar reflete o produto real, sem promessas falsas
  - ✅ Não há rotas vazias "Coming soon" que confundem
  - ❌ Sidebar fica menos densa visualmente que mockup (8 protected routes vs 12+ no mockup)
  - 🔒 Adição de feature nova (ex: Analytics real) requer plano separado + ADR + sidebar item

### D2 — Overview LITERAL (alta fidelidade), outras pages aplicam design language

- **Decisão**: OverviewPage é a única page com pixel-close fidelity ao mockup. Outras 11 pages seguem patterns extraídos no design language doc — mesma visual identity (sidebar, top bar, cards, typography) mas layout interno apropriado ao conteúdo de cada (Projects = grid de ProjectCard, Billing = invoice Table + UsageMeter, etc).
- **Rationale**: Sponsor decidiu 2026-05-24 (primeira pergunta). Temos mockup só do Overview. Inventar mockups para 11 pages seria especulação. Pattern-match é mais honest e fácil de iterar (sponsor pode dar feedback page por page).
- **Consequences**:
  - ✅ Coerência visual cross-page sem mockup-driven invention
  - ✅ Sponsor pode iterar design por page sem refazer plano inteiro
  - ❌ Pages não-Overview podem ter pequenas inconsistências de "como aplicar pattern em contexto X"
  - 🔒 Se sponsor produzir mockup adicional (ex: Billing), abrir mini-plano de "update Billing page to mockup" sem refazer este

### D3 — Reusable Layout components locais (`DashboardLayout`, `ActionBar`, `ContentGrid`)

- **Decisão**: Criar 3 layout components LOCAIS em `src/components/layout/` que compõem primitives de `@loopzero/ui` em patterns dashboard-específicos. NÃO propor upstream em `@loopzero/ui` (são domain-specific demais para um design system framework-agnostic).
- **Rationale**: `@loopzero/ui` ship primitives genéricas (Sidebar, Card, EmptyState). Mas patterns como "DashboardShell com sidebar+topbar+main já pré-cabeado" são opinionated para PaaS dashboards. Encapsular localmente evita repetição em cada page e mantém `@loopzero/ui` agnostic. Equivalente ao pattern shadcn-style "compor primitives na sua app".
- **Consequences**:
  - ✅ Cada page consome `<DashboardLayout>` sem reconstruir
  - ✅ Mudanças no shell propagam automaticamente
  - ✅ @loopzero/ui permanece composable, não opinionated
  - ❌ Outros consumers de @loopzero/ui (futuros) não herdam esses patterns automaticamente

### D4 — Snapshot tests ao invés de full visual regression

- **Decisão**: TDD frontend visual usa testing-library `render` + asserts estruturais (rendering de elementos esperados, props passados corretamente), NÃO snapshot de pixels. Smoke visual é manual em browser.
- **Rationale**: Visual regression real (Chromatic, Playwright, Percy) é T3 separado — adiciona infra. Pixel snapshots são frágeis (mudam com font rendering, sub-pixel anti-alias). Asserts estruturais cobrem regressão funcional sem fragilidade. Sponsor faz smoke manual como gate final (mesmo padrão de v1.3 T0.0.bis task 9).
- **Consequences**:
  - ✅ Testes rápidos, deterministicos, focados em comportamento
  - ✅ Sem dependência de Chromatic/Percy infra
  - ❌ Regressão visual pura (ex: padding mudou) só detectada por humano em smoke
  - 🔒 Visual regression infra pode ser adicionada em plano sucessor sem invalidar testes deste

### D5 — Auth pages seguem "clean form" pattern (não mockup pattern)

- **Decisão**: Auth pages (Login/Register/Verification/Recovery/DeviceSuccess) NÃO usam DashboardLayout (não há sidebar pré-login). Usam `login-split` primitive de `@loopzero/ui` OU clean form layout (centered Card com brand header). Visual coerente com dashboard via typography + palette, mas estrutura diferente.
- **Rationale**: Mockup mostra dashboard logado, não fluxo de auth. Sidebar pre-login não faz sentido (user ainda não tem account). `login-split` (50/50 layout com hero + form) é o pattern PaaS canônico (Vercel, Linear, etc.).
- **Consequences**:
  - ✅ UX correta para fluxo de auth
  - ✅ Branding consistency mantida via tokens compartilhados
  - ❌ Auth pages não validam design language das dashboard pages diretamente (mas usam mesmas primitives)

### D6 — VerificationBanner usa `Alert` primitive (Brief #3) em vez de custom div

- **Decisão**: Substituir `components/verification-banner.tsx` (27 LOC custom) por `<Alert intent="warning" title="..." description="..." action={...} />` de `@loopzero/ui` (Brief #3, shipped 0.9.0-next.0).
- **Rationale**: Alert primitive existe agora upstream. Substituição é trivial (~3 LOC) e ganha a11y proper (`role="status"` no warning, `role="alert"` se destructive).
- **Consequences**:
  - ✅ Semantic coherence — Alert é o primitive correto para banner notice
  - ✅ A11y validation via vitest-axe
  - ❌ VerificationBanner como wrapper local sobrevive só se houver lógica condicional (verified state) — simplificável

### D7 — Bundle target relaxado a +30% gzip vs T0.0 baseline (foi +10%)

- **Decisão**: Relaxar bundle target final de "±10% gzip vs T0.0 baseline" (v1.3 original) para "≤+30% gzip vs T0.0 baseline". Justificativa: mockup-aligned implementation usa significativamente mais primitives de `@loopzero/ui` (Table, AccountMenu, UsageMeter, ProjectCard, Sidebar full composition) que adicionam código real útil.
- **Rationale**: Original ±10% assumia "swap mínimo de primitives". Redesign real consome mais surface — comparable a "lá foi +5KB de Card; agora é +15KB de Sidebar+AccountMenu+UsageMeter+ProjectCard combinado". 30% é o budget honesto para o valor entregue. Em troca: gzip total estimado ~180KB vs baseline 139KB — ainda well-under FAANG benchmarks (Vercel dashboard ~250KB gzip).
- **Consequences**:
  - ✅ Acceptance criteria honesto e atingível
  - ✅ Sem pressão para hand-roll quando primitives entregam mais valor
  - ❌ Bundle maior que v1.3 originalmente projetou
  - 🔒 Sponsor pode pedir Phase I (bundle optimization) em plano sucessor se 180KB virar problema

### D9 — `<LoopZeroUIProvider>` é superset não-documentado de `<ThemeProvider>` (per .llms.txt drift)

- **Decisão**: Continuar usando `<LoopZeroUIProvider>` (commitado em T0.2 `eeb030d3`) como entrypoint, mesmo que `.llms.txt` line 84 só documente `<ThemeProvider initial="violet-forge">`. Verificado via `node_modules/@loopzero/ui/dist/index.d.ts` que LoopZeroUIProvider EXISTE e compõe ThemeProvider + Toaster + density context com defaults sãos.
- **Rationale**: `.llms.txt` line 268 declara explicitamente "If `llms.txt` disagrees with [the code/README/package.json#exports], the code wins — update this file via PR with a one-line rationale." Source files são autoritativos. LoopZeroUIProvider existe → uso válido. .llms.txt drift deve ser reportado upstream via PR (follow-up TODO documentado em comentário em main.tsx).
- **Consequences**:
  - ✅ Boilerplate menor (Toaster + density automático)
  - ✅ Less surface para drift (uma única primitive composes tudo)
  - ❌ Consumer não-padrão segundo .llms.txt — futuros consumers podem ter dúvida
  - 🔒 Se LoopZeroUIProvider for removido em release futuro, fallback é trivial (3-line ThemeProvider + Toaster manual wrap)
- **TODO upstream**: abrir PR em `loopzero-ui` adicionando LoopZeroUIProvider à seção "Theme system" do `.llms.txt` para zerar drift.

### D8 — `<LoopZeroUIProvider>` ou `<ThemeProvider themes={[violetForge]}>` decision deferred to Phase H

- **Decisão**: Phase 0 do v1.3 usou `<LoopZeroUIProvider>` (que carrega `builtinThemes` = 10 themes). Em Phase H deste plano, medir se single-theme `<ThemeProvider themes={[violetForge]} defaultTheme="violet-forge" defaultMode="dark">` reduz bundle suficiente para justificar perda do Toaster automático + outras conveniências.
- **Rationale**: Decisão prematura demais agora. Phase H mede bundle real após todas as pages renderizando, daí decisão informada.
- **Consequences**:
  - ✅ Decisão data-driven
  - ❌ Possível mudança em main.tsx no fim do plano (trivial 5-line edit)

## Dependency Graph

```
v1.3 Phase 0 [DONE]
     │
     ▼
v1.3 Phase 1 [pending] — Button/Badge/Card swap MECÂNICO
     │  T1.3 specificamente DEVE usar Card.Header/Card.Body/Card.Title (D-2 v1.2)
     │
     ▼
Phase A — Foundation Handoff (2 tasks pós v1.2)
     │  T-A.0 (NEW v1.2): verify @loopzero/ui/preset vs 3 imports — D-1
     │  T-A.1: verify v1.3 Phase 1 done + D-2 Card naming check
     │
     ▼
Phase B — Design Language (3 tasks)
     │  T-B.1 doc + T-B.2 DashboardLayout + T-B.3 ActionBar/ContentGrid
     │
     ▼
Phase C — Layout Shell Implementation (3 tasks)
     │  T-C.1 Sidebar + T-C.2 TopBar + T-C.3 DashboardLayout wiring
     │
     ▼
Phase D — Overview Literal (1 task, depende de C)
     │  T-D.1 OverviewPage pixel-close ao mockup
     │
     ├──────────────┬────────────────┐
     ▼              ▼                ▼
Phase E         Phase F          Phase G
Core pages      Secondary        Auth pages
(5 tasks)       (3 tasks)        (5 tasks)
     │              │                │
     └──────────────┼────────────────┘
                    ▼
                Phase H — Validation (4 tasks)
                    │  T-H.1 visual smoke + T-H.2 Rule 3 + T-H.3 bundle + T-H.4 v1.3 reconcile
                    │
                    ▼
                v1.3 Phase 6 (cleanup) — prune deps, tokens, bundle measure
                    │
                    ▼
                Dogfood Gate
```

**Paralelização possível**: Phases E, F, G correm em paralelo após Phase D (Overview valida o design language). Phase H bloqueia conclusão.

---

## Phase A: Foundation Handoff

**Objective:** Verificar que v1.3 Phase 1 (Button/Badge/Card swap mecânico) está completa, validate CSS import strategy contra `.llms.txt` canonical, ready for redesign.

### T-A.0 — Verify CSS import strategy against `.llms.txt` canonical (NEW v1.2 D-1)

#### Objective
Confirmar se `@import "@loopzero/ui/preset"` (single import canônico per `.llms.txt` line 51) substitui adequadamente os 3 imports atuais (`fonts-cdn.css` + `tokens.css` + `styles.css`). Se sim, simplificar `src/index.css`. Se não, documentar o porquê de manter os 3 separados.

#### Evidence
- `.llms.txt` line 51: `import "@loopzero/ui/preset";       // Tailwind v4 preset (zero-config)`
- T0.2 commit (`eeb030d3`) decidiu por 3 imports separados (`fonts-cdn.css`, `tokens.css`, `styles.css`) — predates `.llms.txt` local download
- `package.json#exports` tem AMBOS `./preset` E os 3 separados — sinaliza que preset.css é provavelmente uma compilation curated, mas precisa verificar conteúdo

#### Files to edit
```
cloud/dashboard/src/index.css — possible simplification
```

#### Tasks
1. `cat node_modules/@loopzero/ui/dist/preset.css | head -50` para inspecionar conteúdo do preset
2. Comparar com `cat node_modules/@loopzero/ui/dist/{fonts-cdn,tokens,styles}.css | wc -l`
3. **Decisão**:
   - Se `preset.css` é superset (inclui fonts + tokens + styles + Tailwind v4 layer): simplificar `index.css` para `@import "@loopzero/ui/preset";` único
   - Se `preset.css` é só Tailwind v4 layer (sem fonts/tokens): manter 3 imports atuais, adicionar nota documentando por quê
4. Build verde (`npm run build`)
5. Smoke: dev server abre, Geist fonts renderizando, tokens resolvendo

#### TDD
```
RED: N/A — verification + possible simplification
GREEN: npm run build exits 0, src/index.css alinhado com .llms.txt OR documented divergence
VERIFY: npm run build && grep -c "@import" cloud/dashboard/src/index.css
```

#### Acceptance Criteria
- [ ] preset.css conteúdo verificado
- [ ] Decisão registrada em comentário em index.css OU no header do plano
- [ ] Build verde

#### DoD
- [ ] Commit `chore(dashboard): align CSS imports with @loopzero/ui canonical preset (T-A.0)` se simplificou, ou `docs(dashboard): document CSS import strategy vs .llms.txt canonical (T-A.0)` se manteve

---

### T-A.1 — Verify v1.3 Phase 1 deliverables consumed

#### Objective
Assegurar que toda page e componente do dashboard já importa `Button`, `Badge`, `Card` de `@loopzero/ui` (não de `@/components/ui/*`). Phase A NÃO inicia se v1.3 Phase 1 não estiver done.

#### Evidence
- v1.3 Phase 1 specifica T1.1 (Button) + T1.2 (Badge) + T1.3 (Card) + T1.4 (/recovery route fix)
- Após v1.3 Phase 1: `src/components/ui/` deletado, 25+ imports trocados
- Sem isso, Phase B (que adiciona DashboardLayout consumindo essas primitives) tem ambiguidade de fonte

#### Files to edit
```
(NO FILES EDITED — verification task only)
```

#### Deep file dependency analysis
- Não escreve. Lê: `grep -r "from '@/components/ui/" cloud/dashboard/src/` deve retornar zero
- Verifica que `src/components/ui/` foi deletado
- Verifica que `app.tsx` tem route `/recovery` (v1.3 T1.4)

#### Deep Dives
- **Pre-flight gate**: se v1.3 Phase 1 não estiver done, abort esta task imediatamente — apresentar para sponsor o que falta. NÃO iniciar Phase B antes da verificação verde.

#### Tasks
1. `grep -rn "from '@/components/ui/" cloud/dashboard/src/ --include='*.tsx' --include='*.ts'` deve retornar zero matches
2. `ls cloud/dashboard/src/components/ui/` deve retornar "No such file or directory"
3. `grep -E "Route path=\"/recovery\"" cloud/dashboard/src/app.tsx` deve retornar match
4. `cd cloud/dashboard && npm run build` deve passar verde
5. Se algum check falha: STOP. Sponsor precisa completar v1.3 Phase 1 antes deste plano continuar.

#### TDD
```
RED:     N/A — verification task, gate only
GREEN:   All 4 checks pass; phase advances
REFACTOR: None
VERIFY:  Manual check via the 4 commands above
```

#### Acceptance Criteria
- [ ] Zero matches de `@/components/ui/` em src/
- [ ] `src/components/ui/` não existe
- [ ] Route `/recovery` declarada em app.tsx
- [ ] `npm run build` verde
- [ ] **v1.2 D-2**: v1.3 T1.3 (Card swap) DEVE renomear todos os subcomponentes per .llms.txt sub-component pattern (line 80): `CardHeader → Card.Header`, `CardContent → Card.Body`, `CardTitle → Card.Title`, `CardDescription → Card.Description`. Grep: `grep -rE 'CardHeader|CardContent|CardTitle|CardDescription' cloud/dashboard/src/ --include='*.tsx'` deve retornar zero matches (todos migrados para namespaced form `Card.X`).

#### DoD
- [ ] Tasks 1-4 verificadas verde + D-2 check
- [ ] Confirmação registrada em commit message do próximo task (T-B.1)

---

## Phase B: Design Language Extraction

**Objective:** Extrair design language do mockup em doc curado + construir 3 reusable layout components (DashboardLayout, ActionBar, ContentGrid) que encapsulam patterns dashboard-específicos.

### T-B.1 — Write Design Language doc

#### Objective
Documentar 8 patterns reusable extraídos do mockup em [`docs/design/2026-05-24-dashboard-design-language.md`](../../design/2026-05-24-dashboard-design-language.md). Doc é referência canônica para todas as 13 pages — qualquer divergência futura precisa de RFC.

#### Evidence
- Mockup tem patterns recorrentes (cards, action bar, sidebar shell, 2-col grid) que aparecerão em N pages
- Sem doc canônico, cada page reinventa interpretação (drift visual)

#### Files to edit
```
docs/design/2026-05-24-dashboard-design-language.md (NEW) — canonical pattern doc
```

#### Deep file dependency analysis
- Arquivo NEW
- Referenciado por TODAS as task subsequentes (T-C.* até T-G.*)
- Deve ser commitado ANTES de qualquer implementation começar

#### Deep Dives

8 patterns a documentar:

1. **Layout pattern** — `<DashboardLayout>` shell: sidebar fixed-left 220px (ajuste do mockup 200px para acomodar Geist Sans rendering), main content flex-1, top bar sticky-top 56px
2. **Sidebar pattern** — `<Sidebar>` com `Sidebar.Header` (AccountMenu + PlanBadge), `Sidebar.Section` (search input), `Sidebar.Section` (nav items principais), `Sidebar.Section` (nav items secundárias separadas por border-t), `Sidebar.Footer` (Settings/Support/Usage)
3. **TopBar pattern** — `<TopBar>` com left slot (project selector via Select primitive), center slot (page title — usually omitido se sidebar já indica), right slot (icon menu via DropdownMenu)
4. **ActionBar pattern** — `<ActionBar>` com search Input (flex-1) + filter icon Button (size-9 outline) + primary CTA Button (size-md primary variant) — right-aligned, mt-6 mb-8
5. **ContentGrid pattern** — `<ContentGrid columns="1.2fr-1fr">` ou `<ContentGrid columns="3-equal">` — grid-cols custom para 2-col vs 3-equal vs 1-col
6. **Card pattern** — usar primitive `Card` de @loopzero/ui (já tokenizado), padding p-5 default, header opcional com title + action right-aligned
7. **StatusDot pattern** — usar `StatusDot` primitive (Brief #2): live=success (green), pending=warning (amber/orange), failed=destructive (red), idle=muted
8. **Typography pattern** — Geist Sans para body/headings, Geist Mono para identifiers/timestamps/code, `font-display text-display-md` para stat numbers, `text-label-caps` para section titles uppercase

Plus:
- **Spacing scale**: gap-2 entre sidebar items, gap-3 entre cards horizontais, gap-6 entre seções vertical, p-5 inside cards
- **Color usage**: `bg-surface-0` base, `bg-card` cards, `border-border/40` subtle, `text-foreground` body, `text-muted-foreground` secondary, `text-primary` accent CTA
- **Empty state pattern**: `<EmptyState>` primitive com ícone Lucide + title + description + optional CTA Button — centered, py-8

#### Tasks
1. Criar `docs/design/` directory se não existe
2. Escrever doc seguindo template:
   - Header com data + sponsor decisions + reference ao mockup file
   - Section por pattern (8 patterns) com: descrição + visual mini-ASCII + code example + props/composition exemplo + when to use vs alternatives
   - Section "Cross-page consistency rules": fontes, cores, spacing scale obrigatórios
   - Section "Anti-patterns": ad-hoc Tailwind escapes proibidos, raw `<div>` para layout primário, etc.
   - Section "Evolution": como propor mudanças ao doc (RFC process)
3. Doc deve ter ≤300 LOC (concise reference, not verbose explainer)

#### TDD
```
RED:     N/A — doc artifact, not code
GREEN:   Doc exists + 8 patterns documented + cross-references mockup file
REFACTOR: None expected at creation
VERIFY:  wc -l docs/design/2026-05-24-dashboard-design-language.md (target ≤300)
         grep -c "^## " docs/design/...  (target ≥ 8 sections — 1 per pattern)
```

#### Acceptance Criteria
- [ ] [`docs/design/2026-05-24-dashboard-design-language.md`](../../design/2026-05-24-dashboard-design-language.md) existe
- [ ] 8 patterns documentados (Layout, Sidebar, TopBar, ActionBar, ContentGrid, Card, StatusDot, Typography)
- [ ] LOC ≤300
- [ ] Reference ao mockup file: `cloud/dashboard/design/loopzero_overview_dashboard_mockup.html`
- [ ] Reference ao mapeamento ↔ @loopzero/ui primitives
- [ ] Voice/tone follows CLAUDE.md design-principles.md (KISS, no jargon)

#### DoD
- [ ] Tasks 1-3 completas
- [ ] Commit `docs(dashboard): design language doc — 8 patterns extracted from mockup (T-B.1)`

---

### T-B.2 — Build `DashboardLayout` component

#### Objective
Criar `src/components/layout/dashboard-layout.tsx` — componente que renderiza `<Sidebar>` + `<TopBar>` + main content slot. Replaces inline JSX em `app.tsx` `DashboardLayout` function.

#### Evidence
- `app.tsx:20-42` define DashboardLayout inline com `<div className="dark bg-surface-0 ...">`, `<Sidebar />`, `<main ...>{children}</main>`
- Pattern repetirá em todas as 8 dashboard pages (Overview, Projects, Environments, Team, Billing, Domains, Settings, Profile)
- Centralizar em layout component evita drift

#### Files to edit
```
cloud/dashboard/src/components/layout/dashboard-layout.tsx (NEW) — shell composition
cloud/dashboard/src/components/layout/index.ts (NEW) — barrel export
cloud/dashboard/src/app.tsx — inline DashboardLayout replaced by import
```

#### Deep file dependency analysis
- `dashboard-layout.tsx` (NEW): imports `Sidebar` from `@loopzero/ui` + `TopBar` (T-C.2 NEW local) + uses `useRequireAuth` for session
- `app.tsx`: deletes inline `DashboardLayout` function (lines 20-42), imports from `@/components/layout`
- Downstream: all 8 dashboard pages stay unchanged (still wrapped via `<Routes>` parent in app.tsx)

#### Deep Dives

API proposed:

```tsx
// src/components/layout/dashboard-layout.tsx
import type { ReactNode } from 'react';
import { Sidebar } from '@loopzero/ui';
import { useRequireAuth } from '@/hooks/use-auth';
import { isEmailVerified } from '@/lib/auth';
import { Loader2 } from 'lucide-react';
import { AppSidebar } from './app-sidebar';      // T-C.1
import { TopBar } from './top-bar';              // T-C.2
import { VerificationBanner } from '@/components/verification-banner';

interface DashboardLayoutProps {
  children: ReactNode;
  pageTitle?: string;  // optional override for TopBar center slot
}

export function DashboardLayout({ children, pageTitle }: DashboardLayoutProps) {
  const session = useRequireAuth();
  if (session === null) {
    return (
      <div className="bg-surface-0 min-h-screen text-foreground flex items-center justify-center">
        <Loader2 className="h-6 w-6 text-primary animate-spin" />
      </div>
    );
  }
  const verified = isEmailVerified(session);
  return (
    <div className="bg-surface-0 min-h-screen text-foreground flex">
      <AppSidebar session={session} />
      <div className="flex-1 flex flex-col min-w-0">
        <TopBar pageTitle={pageTitle} />
        <main className="flex-1 p-6">
          {!verified && <VerificationBanner />}
          {children}
        </main>
      </div>
    </div>
  );
}
```

Invariants:
- Preserves `useRequireAuth` redirect-to-login behavior
- Preserves loading state (spinner)
- Preserves VerificationBanner conditional rendering
- TopBar is fixed-height 56px, main fills remaining
- Sidebar is fixed-width 220px

#### Tasks
1. Criar `src/components/layout/` directory
2. **CRITICAL (EC-2 v1.1)**: criar `src/hooks/use-page-meta.ts` com `<PageMetaContext.Provider>` + `usePageMeta()` hook:
   ```tsx
   const PageMetaContext = createContext<{ pageTitle: string; setPageTitle: (s: string) => void }>(...);
   export function PageMetaProvider({ children }) {
     const [pageTitle, setPageTitle] = useState('');
     return <PageMetaContext.Provider value={{ pageTitle, setPageTitle }}>{children}</PageMetaContext.Provider>;
   }
   export function usePageMeta() { return useContext(PageMetaContext); }
   export function useSetPageTitle(title: string) {
     const { setPageTitle } = usePageMeta();
     useEffect(() => { setPageTitle(title); }, [title]);
   }
   ```
3. DashboardLayout envolve com `<PageMetaProvider>` antes de renderizar TopBar. TopBar consome `usePageMeta()` para o center slot. Pages chamam `useSetPageTitle('Overview')` em mount.
4. Implementar `dashboard-layout.tsx` per Deep Dive API (importa `AppSidebar` + `TopBar` stubs que serão implementadas em T-C.1, T-C.2)
5. Stubs temporários para `AppSidebar` e `TopBar` (apenas placeholder `<aside />` + `<header />`) — serão preenchidos em T-C.x
6. Atualizar `app.tsx` para importar `DashboardLayout` em vez de definir inline
7. Build verde

#### TDD
```
RED:     test_dashboard_layout_shows_spinner_when_session_loading() — mock useRequireAuth returns null → asserta Loader2 presente
RED:     test_dashboard_layout_renders_children_when_session_ready() — mock returns session → asserta children + sidebar + topbar
RED:     test_dashboard_layout_shows_verification_banner_when_unverified() — mock session.identity.verifiable_addresses=[{verified:false}] → asserta VerificationBanner presente
RED:     test_dashboard_layout_hides_verification_banner_when_verified() — mock session verified → asserta VerificationBanner ausente
GREEN:   Implement per Deep Dive API
REFACTOR: None expected
VERIFY:  npm test -- dashboard-layout
```

#### Acceptance Criteria
- [ ] `src/components/layout/dashboard-layout.tsx` exists (≤60 LOC)
- [ ] `app.tsx` imports `DashboardLayout` from local layout barrel
- [ ] Inline DashboardLayout function removed from app.tsx
- [ ] 4 RED tests verdes
- [ ] Build verde

#### DoD
- [ ] Tasks 1-5 completas
- [ ] Commit `feat(dashboard): extract DashboardLayout component (T-B.2)`

---

### T-B.3 — Build `ActionBar` + `ContentGrid` components

#### Objective
Criar 2 reusable layout components que pages consomem para implementar action bar pattern (search + filter + CTA) e content grid pattern (2-col, 3-col, 1-col).

#### Evidence
- ActionBar aparece em: Projects, Environments, Domains, Team (≥4 pages)
- ContentGrid aparece em: Overview (2-col 1.2fr:1fr), Billing (2-col), Settings (1-col com seções)
- Sem componentes, cada page reinventa flex/grid + Input + Button spacing

#### Files to edit
```
cloud/dashboard/src/components/layout/action-bar.tsx (NEW)
cloud/dashboard/src/components/layout/content-grid.tsx (NEW)
cloud/dashboard/src/components/layout/index.ts — append exports
```

#### Deep Dives

```tsx
// action-bar.tsx
interface ActionBarProps {
  searchPlaceholder?: string;
  searchValue?: string;
  onSearchChange?: (v: string) => void;
  onFilterClick?: () => void;
  primaryAction?: { label: string; onClick: () => void; icon?: ElementType };
  className?: string;
}

// content-grid.tsx
type GridColumns = '1' | '2-equal' | '2-asymmetric' | '3-equal';
interface ContentGridProps {
  columns: GridColumns;
  children: ReactNode;
  gap?: 4 | 6 | 8;  // Tailwind gap units
  className?: string;
}
```

ContentGrid maps columns to Tailwind classes:
- `1` → `grid grid-cols-1`
- `2-equal` → `grid grid-cols-1 lg:grid-cols-2`
- `2-asymmetric` → `grid grid-cols-1 lg:grid-cols-[1.2fr_1fr]` (mockup default)
- `3-equal` → `grid grid-cols-1 md:grid-cols-3`

#### Tasks
1. Implementar `action-bar.tsx` per API
2. Implementar `content-grid.tsx` per API
3. Append exports ao `layout/index.ts`
4. Build verde

#### TDD
```
RED:     test_action_bar_renders_search_input_with_placeholder()
RED:     test_action_bar_fires_onSearchChange_on_typing()
RED:     test_action_bar_fires_onFilterClick_when_clicked()
RED:     test_action_bar_primary_action_renders_with_icon_and_fires_onClick()
RED:     test_content_grid_one_col_applies_grid_cols_1()
RED:     test_content_grid_two_asymmetric_applies_responsive_grid()
RED:     test_content_grid_renders_children_in_order()
GREEN:   Implement
REFACTOR: None
VERIFY:  npm test -- (action-bar|content-grid)
```

#### Acceptance Criteria
- [ ] Both components ≤40 LOC each
- [ ] 7 RED tests verdes
- [ ] Build verde

#### DoD
- [ ] Tasks 1-4 completas
- [ ] Commit `feat(dashboard): add ActionBar + ContentGrid layout components (T-B.3)`

---

## Phase C: Layout Shell Implementation

**Objective:** Implementar `<AppSidebar>` (com AccountMenu + 12 real routes) + `<TopBar>` (project selector + page title + menu) + completar wiring no `<DashboardLayout>`.

### T-C.1 — Implement `AppSidebar`

#### Objective
Substituir stub criado em T-B.2 por implementação real de `<AppSidebar>` usando `Sidebar` primitive + `AccountMenu` composite. 12 nav items reais do LoopZeroCloud (não os do mockup Vercel).

#### Evidence
- Mockup linhas 3-30 mostram sidebar pattern (200px, account header, search, nav, footer)
- LoopZeroCloud rotas reais ≠ mockup Vercel rotas — D1 ADR explicita
- Existing `src/components/sidebar.tsx` (109 LOC, hand-rolled) será DELETADO

#### Files to edit
```
cloud/dashboard/src/components/layout/app-sidebar.tsx (NEW) — replaces stub from T-B.2
cloud/dashboard/src/components/sidebar.tsx — DELETED (old hand-rolled)
cloud/dashboard/src/components/layout/index.ts — append AppSidebar export
```

#### Deep file dependency analysis
- New `app-sidebar.tsx` ~80 LOC max
- Old `sidebar.tsx` deletion: NO consumers (only used by inline DashboardLayout that became `dashboard-layout.tsx` in T-B.2)
- `DashboardLayout` in T-B.2 already imports from `./app-sidebar` — when this task lands, stub becomes real implementation

#### Deep Dives

Real route mapping (sidebar items):

| Group | Item | Route | Icon (lucide) | LoopZeroCloud feature |
|---|---|---|---|---|
| Main | Overview | `/` | `LayoutGrid` | dashboard home |
| Main | Projects | `/projects` | `FolderKanban` | project list |
| Main | Environments | `/environments` | `Box` | env list per project |
| Main | Domains | `/domains` | `Globe` | custom domains |
| Main | Team | `/team` | `Users` | members + RBAC |
| (separator) | | | | |
| Billing | Billing | `/billing` | `Receipt` | invoices + plan |
| (footer) | | | | |
| Account | Settings | `/settings` | `Settings` | workspace settings |
| Account | Profile | `/profile` | `User` | user profile |

Account header (Sidebar.Header):
- `<AccountMenu name={session.identity.traits.name ?? session.identity.traits.email} avatar={...} plan={planTier} />`
- Plan tier: derivado de `/v1/billing` response — `'free' | 'hobby' | 'pro' | 'team' | 'enterprise'` mapped to PlanBadge
- Avatar fallback: initials da name

Search input below header:
- `<Input placeholder="Find a project..." leadingIcon={<Search />} />` — busca client-side por project name (state local, sem fetch)

```tsx
export function AppSidebar({ session }: { session: Session }) {
  const location = useLocation();
  const isActive = (path: string) =>
    path === '/' ? location.pathname === '/' : location.pathname.startsWith(path);

  return (
    <Sidebar className="w-[220px]">
      <Sidebar.Header className="p-3">
        <AccountMenu
          name={session.identity.traits.email}
          plan="hobby"  // TODO: fetch from /v1/billing in Phase E (T-E.3 Billing page work)
          onClick={() => { /* future: open account switcher */ }}
        />
      </Sidebar.Header>
      <Sidebar.Section className="px-3">
        <Input placeholder="Find a project..." leadingIcon={<Search className="size-3.5" />} />
      </Sidebar.Section>
      <Sidebar.Section>
        <Sidebar.Item as="a" href="/" icon={LayoutGrid} active={isActive('/')}>Overview</Sidebar.Item>
        <Sidebar.Item as="a" href="/projects" icon={FolderKanban} active={isActive('/projects')}>Projects</Sidebar.Item>
        <Sidebar.Item as="a" href="/environments" icon={Box} active={isActive('/environments')}>Environments</Sidebar.Item>
        <Sidebar.Item as="a" href="/domains" icon={Globe} active={isActive('/domains')}>Domains</Sidebar.Item>
        <Sidebar.Item as="a" href="/team" icon={Users} active={isActive('/team')}>Team</Sidebar.Item>
      </Sidebar.Section>
      <Sidebar.Section title="Billing">
        <Sidebar.Item as="a" href="/billing" icon={Receipt} active={isActive('/billing')}>Billing</Sidebar.Item>
      </Sidebar.Section>
      <Sidebar.Footer>
        <Sidebar.Item as="a" href="/settings" icon={Settings} active={isActive('/settings')}>Settings</Sidebar.Item>
        <Sidebar.Item as="a" href="/profile" icon={User} active={isActive('/profile')}>Profile</Sidebar.Item>
      </Sidebar.Footer>
    </Sidebar>
  );
}
```

**NOTE on `as="a" href=...`**: Sidebar.Item supports `as="a" + href` per its source (`primitives/sidebar/sidebar.tsx`). Para SPA navigation via react-router, considerar wrap em `<Link>` — verificar em T-C.1 task 3 se Sidebar.Item aceita `as={Link}` ou se precisamos compose differently.

#### Tasks
1. Inspecionar `node_modules/@loopzero/ui/dist/index.d.ts` para confirmar `Sidebar.Item` polymorphism options (as="a", as="button", as={Component})
2. **CRITICAL (EC-1 v1.1)**: react-router SPA navigation MUST be preserved. Se Sidebar.Item suporta `as={Link}` (polymorphic generic Component), usar:
   ```tsx
   import { Link } from 'react-router-dom';
   <Sidebar.Item as={Link} to="/projects" icon={FolderKanban} active={isActive('/projects')}>Projects</Sidebar.Item>
   ```
   Se NÃO suporta (só `as="a" | "button"`), wrap manual:
   ```tsx
   <Link to="/projects" className="contents"><Sidebar.Item as="button" icon={FolderKanban} active={isActive('/projects')}>Projects</Sidebar.Item></Link>
   ```
   `as="a" href={path}` PURO produz full page reload (perde JWT em memória — viola D3 v1.3). NUNCA usar.
3. Implementar `AppSidebar` per Deep Dive API, adaptando ao polymorphism real
4. Importar Lucide icons necessários (LayoutGrid, FolderKanban, Box, Globe, Users, Receipt, Settings, User, Search)
5. Deletar `src/components/sidebar.tsx` (old hand-rolled)
6. Build verde + smoke test SPA: click em cada nav item NÃO recarrega página (DevTools Network: nenhum request para `/`, `/projects`, etc. — só os fetches de API)

#### TDD
```
RED:     test_app_sidebar_renders_all_8_nav_items() — assert each route present
RED:     test_app_sidebar_active_item_matches_current_route() — mock useLocation, assert active state
RED:     test_app_sidebar_account_menu_renders_session_email()
RED:     test_app_sidebar_account_menu_passes_plan_tier_to_badge()
RED:     test_app_sidebar_search_input_present_with_placeholder()
GREEN:   Implement
REFACTOR: None
VERIFY:  npm test -- app-sidebar
```

#### Acceptance Criteria
- [ ] AppSidebar uses `Sidebar` + `AccountMenu` + `Input` from @loopzero/ui
- [ ] 8 nav items mapped to 12 real routes (Overview, Projects, Environments, Domains, Team, Billing, Settings, Profile)
- [ ] Active state reflects current route
- [ ] Zero Vercel mockup-specific items (AI Gateway, Firewall, CDN, etc.) per D1
- [ ] Old `src/components/sidebar.tsx` deleted
- [ ] 5 RED tests verdes
- [ ] Build verde

#### DoD
- [ ] Tasks 1-5 completas
- [ ] Commit `feat(dashboard): implement AppSidebar with AccountMenu + 12 real routes (T-C.1)`

---

### T-C.2 — Implement `TopBar`

#### Objective
Implementar `<TopBar>` com left slot (project selector), center slot (page title opcional), right slot (menu).

#### Evidence
- Mockup linhas 33-37 mostram top bar pattern
- TopBar precisa em todas as 8 dashboard pages — centralizar evita drift

#### Files to edit
```
cloud/dashboard/src/components/layout/top-bar.tsx (NEW)
cloud/dashboard/src/components/layout/index.ts — append export
```

#### Deep Dives

API:

```tsx
interface TopBarProps {
  pageTitle?: string;
  showProjectSelector?: boolean;  // hide on Settings/Profile/Billing
  menuItems?: Array<{ label: string; onClick: () => void; destructive?: boolean }>;
}

export function TopBar({ pageTitle, showProjectSelector = true, menuItems }: TopBarProps) {
  return (
    <header className="h-14 border-b border-border/40 flex items-center justify-between px-6">
      {showProjectSelector ? (
        <Select placeholder="All Projects" /* TODO: fetch /api/v1/projects in T-D.1 */ />
      ) : <div />}
      {pageTitle && <span className="text-body-sm font-medium text-foreground">{pageTitle}</span>}
      {menuItems?.length ? <DropdownMenu items={menuItems} /> : <div />}
    </header>
  );
}
```

Invariants:
- height fixed 56px (`h-14`)
- 3-column grid via flex justify-between
- Project selector adapts to context (hidden on non-project pages)

#### Tasks
1. **CRITICAL (EC-3 v1.1)**: criar `src/hooks/use-projects.ts` com SWR-style cache (60s TTL) ANTES de implementar TopBar:
   ```tsx
   // Singleton module-level cache evita duplicate fetch entre TopBar + Overview + Projects
   let cache: { data: ProjectsResponse | null; timestamp: number } = { data: null, timestamp: 0 };
   const TTL_MS = 60_000;
   export function useProjects() {
     const [state, setState] = useState(cache.data);
     useEffect(() => {
       if (Date.now() - cache.timestamp < TTL_MS && cache.data) return;
       cloudFetch<ProjectsResponse>('/engine/api/v1/projects').then(data => {
         if (data) { cache = { data, timestamp: Date.now() }; setState(data); }
       });
     }, []);
     return state;
   }
   ```
2. Implementar `top-bar.tsx` per API consumindo `useProjects()` no Select
3. Verificar `Select` API real em @loopzero/ui (placeholder + items + onSelect)
4. Verificar DropdownMenu API (pode ser via Radix bundled)
5. TopBar consome `usePageMeta()` (de T-B.2) para renderizar pageTitle no center slot
6. Build verde

#### TDD
```
RED:     test_top_bar_renders_project_selector_by_default()
RED:     test_top_bar_hides_project_selector_when_prop_false()
RED:     test_top_bar_renders_page_title_when_provided()
RED:     test_top_bar_renders_menu_when_items_provided()
RED:     test_top_bar_menu_destructive_item_styled_correctly()
GREEN:   Implement
VERIFY:  npm test -- top-bar
```

#### Acceptance Criteria
- [ ] TopBar ≤40 LOC
- [ ] 5 RED tests verdes

#### DoD
- [ ] Tasks 1-4 completas
- [ ] Commit `feat(dashboard): implement TopBar with project selector + menu (T-C.2)`

---

### T-C.3 — Wire DashboardLayout consuming AppSidebar + TopBar

#### Objective
Trocar stubs de AppSidebar + TopBar em `dashboard-layout.tsx` (de T-B.2) pelas implementações reais (de T-C.1 + T-C.2). Verificar que app inteiro compila e renderiza.

#### Files to edit
```
cloud/dashboard/src/components/layout/dashboard-layout.tsx — replace stub imports
```

#### Tasks
1. Atualizar imports em `dashboard-layout.tsx`
2. Build verde
3. Smoke test: `npm run dev` → abrir browser → asserta que sidebar + topbar renderizam sem console errors

#### TDD
```
RED:     test_dashboard_layout_integration_renders_full_shell() — render <DashboardLayout> com mock session, assert AppSidebar + TopBar + main content present
GREEN:   Update stubs to real imports
VERIFY:  npm test -- dashboard-layout && npm run build
```

#### Acceptance Criteria
- [ ] Build verde
- [ ] Test integration verde

#### DoD
- [ ] Tasks 1-3 completas
- [ ] Commit `feat(dashboard): wire DashboardLayout with real AppSidebar + TopBar (T-C.3)`

---

## Phase D: Overview Literal

**Objective:** Reescrever `OverviewPage` com **alta fidelidade ao mockup**. Esta é a única page com requisito pixel-close — outras seguem design language extraído.

### T-D.1 — Implement OverviewPage per mockup

#### Objective
Substituir `pages/overview.tsx` (113 LOC) por implementação alinhada ao mockup: ActionBar + 2-col grid (Usage card + Alerts empty | Projects card + Recent Previews empty).

#### Evidence
- Mockup linhas 39-87 mostram conteúdo exato
- Composites disponíveis: UsageMeter (Brief #1), ProjectCard, EmptyState, Card
- Page consome DashboardLayout (T-C.3) — não duplica shell

#### Files to edit
```
cloud/dashboard/src/pages/overview.tsx — full rewrite
```

#### Deep Dives

Estrutura proposta:

```tsx
export function OverviewPage() {
  useRequireAuth();
  const { data: projectsData, loading } = useEngineApi<ProjectsResponse>('/api/v1/projects');
  const { data: usageData } = useCloudApi<UsageResponse>('/v1/billing');
  const projects = projectsData?.projects ?? [];

  return (
    <>
      <ActionBar
        searchPlaceholder="Search Projects..."
        primaryAction={{
          label: 'Add New',
          onClick: () => navigate('/projects/new'),
          icon: Plus
        }}
      />

      <ContentGrid columns="2-asymmetric" gap={6}>
        {/* Left col */}
        <div className="space-y-6">
          <section>
            <p className="text-label-caps text-muted-foreground uppercase mb-3">Usage</p>
            <UsageMeter
              title="Last 30 days"
              action={<Badge variant="outline">Upgrade</Badge>}
              metrics={[
                { label: 'Fast Data Transfer', value: usageData?.dataTransferGB ?? 0, max: 100, unit: 'GB' },
                { label: 'Fast Origin Transfer', value: usageData?.originTransferGB ?? 0, max: 10, unit: 'GB' },
                { label: 'Edge Requests', value: usageData?.requests ?? 0, max: 1_000_000, unit: '', formatter: (v, m) => `${formatNum(v)} / 1M` },
              ]}
            />
          </section>
          <section>
            <p className="text-label-caps text-muted-foreground uppercase mb-3">Alerts</p>
            <EmptyState
              icon={Bell}
              title="Get alerted for anomalies"
              description="Automatically monitor your projects for anomalies and get notified."
              action={<Button>Upgrade to Pro</Button>}
            />
          </section>
        </div>

        {/* Right col */}
        <div className="space-y-6">
          <section>
            <p className="text-label-caps text-muted-foreground uppercase mb-3">Projects</p>
            {loading ? (
              <Card><div className="p-5 text-center"><Loader2 className="mx-auto size-5 text-muted-foreground animate-spin" /></div></Card>
            ) : projects.length === 0 ? (
              <EmptyState
                icon={FolderKanban}
                title="No projects yet"
                description={<>Run <code className="bg-muted px-2 py-0.5 rounded font-mono">loopzero init</code> to create your first.</>}
              />
            ) : (
              projects.slice(0, 3).map(p => (
                <ProjectCard
                  key={p.name}
                  name={p.name}
                  environment="production"
                  lastDeploy={p.last_deploy ?? 'never'}
                  apps={p.apps ?? []}
                />
              ))
            )}
          </section>
          <section>
            <p className="text-label-caps text-muted-foreground uppercase mb-3">Recent Previews</p>
            <EmptyState
              icon={Eye}
              description="Preview deployments that you have recently visited or created will appear here."
            />
          </section>
        </div>
      </ContentGrid>
    </>
  );
}
```

Mockup fidelity compliance checklist:
- [x] ActionBar at top (Search + Add New)
- [x] 2-column grid 1.2fr:1fr (via ContentGrid columns="2-asymmetric")
- [x] Left col: Usage card (3 progress bars) + Alerts empty state
- [x] Right col: Projects card (1+ ProjectCard) + Recent Previews empty
- [x] Section labels uppercase (`text-label-caps`)
- [x] Card spacing (`space-y-6`)
- [x] Empty states with Lucide icons (Bell, Eye, FolderKanban)
- [x] "Upgrade to Pro" CTA in Alerts
- [x] "Upgrade" badge in Usage card header

#### Tasks
1. **CRITICAL (EC-4 v1.1) pre-step**: Ler `cloud/internal/routes/billing.go` GetBilling handler para confirmar response shape REAL. Provavelmente retorna `{ plan, status, period_start, period_end }` — NÃO inclui usage metrics (que estão em `cost_counters` table, escritas pelo cost aggregator). Se confirmado: usar `useCloudApi<BillingResponse>` para plan, e UsageMeter renderiza com placeholder zerados até endpoint `/v1/billing/usage` ou `/v1/usage` existir. Mockup spec aceita placeholders zerados (mockup tem `0/100GB` literal).
2. **CRITICAL (EC-7 v1.1)**: Adicionar `searchQuery` state + filter ao Overview:
   ```tsx
   const [searchQuery, setSearchQuery] = useState('');
   const filteredProjects = projects.filter(p => p.name.toLowerCase().includes(searchQuery.toLowerCase()));
   // ActionBar recebe: searchValue={searchQuery} onSearchChange={setSearchQuery}
   // Right col renderiza filteredProjects (não projects)
   ```
3. Adicionar `useSetPageTitle('Overview')` na mount (EC-2)
4. Definir shape `BillingResponse` em types baseado em billing.go verificado
5. Rewrite `overview.tsx` per Deep Dive structure (com searchQuery state)
6. **EC-6 v1.1**: durante rewrite, substituir QUALQUER `text-white` por `text-foreground` e `bg-surface-*` por equivalente token (`bg-card`, `bg-muted`, etc.)
7. Build verde
8. Smoke local: `npm run dev` → /overview deve renderizar todas as 4 sections + search filtra projects em real-time

#### TDD
```
RED:     test_overview_renders_action_bar_with_add_new()
RED:     test_overview_renders_usage_meter_with_3_metrics()
RED:     test_overview_renders_alerts_empty_state_with_cta()
RED:     test_overview_renders_projects_empty_when_no_data()
RED:     test_overview_renders_project_cards_when_data_present()
RED:     test_overview_renders_recent_previews_empty()
RED:     test_overview_search_input_filters_projects() — type in search → assert only matching projects visible
GREEN:   Implement
REFACTOR: None
VERIFY:  npm test -- overview
```

#### Acceptance Criteria
- [ ] LOC ≤80 (revisado de original 113, target -29%)
- [ ] 7 RED tests verdes
- [ ] Mockup fidelity checklist 8/8 ✓
- [ ] Build verde
- [ ] Smoke visual confirmed (sponsor side)

#### DoD
- [ ] Tasks 1-5 completas
- [ ] Commit `feat(dashboard): rewrite OverviewPage per mockup design (T-D.1)`

---

## Phase E: Core Pages (Projects, Environments, Billing, Domains)

**Objective:** Aplicar design language em 4 core pages. Cada page consome DashboardLayout + ActionBar + ContentGrid + primitives específicos da page.

### T-E.1 — Refactor ProjectsPage

#### Files to edit
```
cloud/dashboard/src/pages/projects.tsx — rewrite (current 92 LOC → target 55)
```

#### Deep Dives
- ActionBar (search + filter + "New Project" CTA)
- ContentGrid columns="3-equal" para project cards grid
- `<ProjectCard>` per project (composite handles status + apps + last deploy)
- EmptyState quando `projects.length === 0`

#### TDD
```
RED:     test_projects_renders_grid_of_cards()
RED:     test_projects_empty_state_when_no_data()
RED:     test_projects_loading_state_renders_skeleton()
RED:     test_projects_search_filters_by_name()
GREEN:   Rewrite per design language
VERIFY:  npm test -- projects
```

#### Acceptance Criteria
- [ ] LOC ≤55
- [ ] 4 RED tests verdes

#### DoD
- [ ] Commit `refactor(dashboard): ProjectsPage with action bar + 3-col grid (T-E.1)`

---

### T-E.2 — Refactor EnvironmentsPage

#### Files to edit
```
cloud/dashboard/src/pages/environments.tsx — rewrite (current 76 LOC → target 50)
```

#### Deep Dives
- ActionBar (search por env name)
- `<Table>` (Brief #2) com cols: Name | Project | Status (StatusDot + label) | Last Deploy (Timestamp relative) | URL (CopyButton)
- Density compact para denser display

#### TDD
```
RED:     test_environments_renders_table_with_headers()
RED:     test_environments_status_dot_reflects_state()
RED:     test_environments_timestamp_relative_format()
RED:     test_environments_copy_button_copies_url()
GREEN:   Rewrite
VERIFY:  npm test -- environments
```

#### Acceptance Criteria
- [ ] LOC ≤50
- [ ] 4 RED tests verdes

#### DoD
- [ ] Commit `refactor(dashboard): EnvironmentsPage with Table + StatusDot + Timestamp (T-E.2)`

---

### T-E.3 — Refactor BillingPage

#### Files to edit
```
cloud/dashboard/src/pages/billing.tsx — rewrite (current 259 LOC → target 130, -50%)
```

#### Deep Dives
- 2-col grid: Plan summary (left) + Usage card (right)
- Plan summary: `<PlanBadge>` + current plan name + price + "Upgrade" or "Manage" Button → /v1/billing/portal
- Usage card: `<UsageMeter>` com metrics — see EC-4 pre-step task abaixo
- Invoice history Table (Brief #2): Date (Timestamp) | Description | Amount (font-mono tabular) | Status (StatusDot) | PDF (CopyButton or download link) — **CONDICIONAL ao EC-5 pre-step**
- Cancel subscription via ConfirmDialog (Brief #2) with confirmationPhrase="cancel"

#### Pre-step CRITICAL (EC-4 + EC-5 v1.1)

ANTES de implementar T-E.3:

1. **EC-4**: Confirmar response shape de `/v1/billing` (handler `cloud/internal/routes/billing.go` GetBilling). Esperado: `{ plan, status, period_start, period_end }`. Se NÃO inclui usage metrics, UsageMeter usa placeholders zerados até endpoint dedicado existir.

2. **EC-5**: Verificar se cloud expõe endpoint para invoice history:
   ```bash
   grep -E "invoice|Invoice" cloud/internal/routes/billing.go
   ```
   - Se EXISTE (`/v1/billing/invoices` ou similar): implementar Table per Deep Dive
   - Se NÃO existe: **OMITIR invoice Table de T-E.3 v1**. BillingPage renderiza só Plan summary + Usage card. Documentar como follow-up: "abrir issue cloud-api para expor GET /v1/billing/invoices via stripe.invoices.list — plano sucessor de Billing." Atualizar acceptance criteria abaixo: LOC target reduzido de ≤130 para ≤90 (sem Table).

#### TDD
```
RED:     test_billing_renders_plan_badge_with_current_plan()
RED:     test_billing_portal_button_calls_endpoint()
RED:     test_billing_renders_invoice_table_with_data()
RED:     test_billing_cancel_subscription_requires_confirmation()
RED:     test_billing_usage_meter_reflects_period_data()
GREEN:   Rewrite
VERIFY:  npm test -- billing
```

#### Acceptance Criteria
- [ ] LOC ≤130
- [ ] 5 RED tests verdes
- [ ] Handlers preserved: `/v1/billing/portal`, `/v1/billing/checkout`

#### DoD
- [ ] Commit `refactor(dashboard): BillingPage with UsageMeter + Table + ConfirmDialog (T-E.3)`

---

### T-E.4 — Refactor DomainsPage

#### Files to edit
```
cloud/dashboard/src/pages/domains.tsx — rewrite (current 182 LOC → target 85, -53%)
```

#### Deep Dives
- ActionBar (search + "Add Domain" CTA)
- `<DomainConfig>` composite per domain (handles verification flow)
- Falls-back para `<Table>` se DomainConfig não cobre our shape
- DNS records use `<CodeBlock copyable>` (Brief #2) for TXT/A/CNAME values
- StatusDot per domain state (verified/pending/failed)

#### TDD
```
RED:     test_domains_renders_list_with_status()
RED:     test_domains_add_domain_opens_form()
RED:     test_domains_dns_record_copy_button_works()
RED:     test_domains_verification_state_displays_correctly()
GREEN:   Rewrite
VERIFY:  npm test -- domains
```

#### Acceptance Criteria
- [ ] LOC ≤85
- [ ] 4 RED tests verdes

#### DoD
- [ ] Commit `refactor(dashboard): DomainsPage with DomainConfig + CodeBlock + StatusDot (T-E.4)`

---

### T-E.5 — Substitute VerificationBanner with Alert primitive

#### Objective
Substituir `components/verification-banner.tsx` (27 LOC custom) por `<Alert intent="warning">` de @loopzero/ui (Brief #3).

#### Files to edit
```
cloud/dashboard/src/components/verification-banner.tsx — simplify to thin wrapper around Alert
```

#### Deep Dives
- Componente continua existindo como wrapper (lógica condicional pode crescer) mas reduz para ~10 LOC
- Alert intent="warning" + title="Verify your email" + description + action=<Button>Resend</Button>

#### TDD
```
RED:     test_verification_banner_renders_alert_when_unverified()
RED:     test_verification_banner_renders_nothing_when_verified()
RED:     test_verification_banner_resend_calls_endpoint()
GREEN:   Substitute custom div for Alert primitive
VERIFY:  npm test -- verification-banner
```

#### Acceptance Criteria
- [ ] LOC ≤15
- [ ] Uses Alert primitive
- [ ] 3 RED tests verdes

#### DoD
- [ ] Commit `refactor(dashboard): VerificationBanner uses Alert primitive (T-E.5)`

---

## Phase F: Secondary Pages (Team, Settings, Profile)

### T-F.1 — Refactor TeamPage

#### Files to edit
```
cloud/dashboard/src/pages/team.tsx — rewrite (current 81 LOC → target 60)
```

#### Deep Dives
- ActionBar (search members + "Invite Member" CTA)
- `<Table>` cols: Avatar+Name | Email | Role (Badge) | Joined (Timestamp) | Actions (DropdownMenu: Change role, Remove → ConfirmDialog)
- Empty state when no members (impossible — at least owner exists, but handle gracefully)

#### TDD
```
RED:     test_team_renders_member_table()
RED:     test_team_invite_member_opens_dialog()
RED:     test_team_remove_member_requires_confirmation()
RED:     test_team_role_selector_disabled_for_owner()
GREEN:   Rewrite
VERIFY:  npm test -- team
```

#### Acceptance Criteria
- [ ] LOC ≤60
- [ ] 4 RED tests verdes

#### DoD
- [ ] Commit `refactor(dashboard): TeamPage with member Table + ConfirmDialog (T-F.1)`

---

### T-F.2 — Refactor SettingsPage

#### Files to edit
```
cloud/dashboard/src/pages/settings.tsx — rewrite (current 57 LOC → target 45)
```

#### Deep Dives
- ContentGrid columns="1" (single col vertical sections)
- Sections: General (workspace name input + save), Notifications (toggles), Integrations (placeholders for future)
- `<DangerZone>` (Brief #2) com `.Action` "Delete workspace" + ConfirmDialog (phrase=workspace name)

#### TDD
```
RED:     test_settings_renders_general_section()
RED:     test_settings_save_general_calls_api()
RED:     test_settings_danger_zone_delete_requires_phrase_match()
GREEN:   Rewrite
VERIFY:  npm test -- settings
```

#### Acceptance Criteria
- [ ] LOC ≤45
- [ ] 3 RED tests verdes

#### DoD
- [ ] Commit `refactor(dashboard): SettingsPage with sections + DangerZone (T-F.2)`

---

### T-F.3 — Refactor ProfilePage

#### Files to edit
```
cloud/dashboard/src/pages/profile.tsx — rewrite (current 74 LOC → target 55)
```

#### Deep Dives
- ContentGrid columns="1"
- Sections: Avatar+name+email (read from session), Account info (form fields), DangerZone (delete account → ConfirmDialog phrase=user email)

#### TDD
```
RED:     test_profile_renders_avatar_with_session_data()
RED:     test_profile_updates_display_name()
RED:     test_profile_delete_account_requires_email_phrase()
GREEN:   Rewrite
VERIFY:  npm test -- profile
```

#### Acceptance Criteria
- [ ] LOC ≤55
- [ ] 3 RED tests verdes

#### DoD
- [ ] Commit `refactor(dashboard): ProfilePage with sections + DangerZone (T-F.3)`

---

## Phase G: Auth Pages

**Objective:** Refactor 5 auth pages (Login, Register, Verification, Recovery, DeviceSuccess) usando login-split primitive ou clean form pattern. Per D5: auth pages NÃO usam DashboardLayout.

### T-G.1 — Refactor LoginPage

#### Files to edit
```
cloud/dashboard/src/pages/login.tsx — rewrite (current 244 LOC → target 100, -59%)
```

#### Deep Dives
- Use `<LoginSplit>` primitive de @loopzero/ui (50/50 layout com brand on left, form on right) OR clean centered Card. Reference: `docs.loopzero.example/loopzeroui/auth/login-split` MDX page.
- `<SocialAuthRow providers={['github', 'google']}>` for OIDC buttons. Reference: `docs.loopzero.example/loopzeroui/auth/social-auth-row`.
- `<FormField>` for email + password. Reference: `docs.loopzero.example/loopzeroui/primitives/form-field`.
- `<Alert intent="warning">` for session-expired notice (Brief #3, shipped 0.9.0)
- `<Alert intent="destructive">` for Kratos errors
- **CRITICAL D-7 (v1.2)**: REMOVER SVG inline do Google logo de `login.tsx:200` (4 paths inline atualmente). Anti-pattern explícito per `.llms.txt` line 118: "Use `lucide-react` for icons. Never inline SVG." Substituir por `<SocialAuthRow>` primitive (que internamente usa Lucide icons) OU se SocialAuthRow não aceita Google direto, usar `<Button><Chrome /> Continue with Google</Button>` (Chrome icon Lucide é proxy aceitável; OR import `<GoogleIcon />` se @loopzero/ui exporta — verificar `.llms.txt` + `.d.ts`).
- CRITICAL: preserve `submitOIDC` vanilla DOM form.submit() per EC-4 v1.3 + EC-13

#### TDD
```
RED:     test_login_loads_kratos_flow()
RED:     test_login_submit_password_calls_kratos()
RED:     test_login_oidc_form_submit_preserves_vanilla_dom() — assert document.createElement('form') still used
RED:     test_login_session_expired_alert_when_searchParams_reason()
RED:     test_login_displays_kratos_errors()
GREEN:   Rewrite preserving handlers
VERIFY:  npm test -- login
```

#### Acceptance Criteria
- [ ] LOC ≤100
- [ ] 5 RED tests verdes
- [ ] submitOIDC handler verbatim preserved

#### DoD
- [ ] Commit `refactor(dashboard): LoginPage with LoginSplit + FormField + Alert (T-G.1)`

---

### T-G.2 — Refactor RegisterPage

Same pattern as T-G.1.

#### TDD/Acceptance: identical structure
- [ ] LOC ≤100

#### DoD
- [ ] Commit `refactor(dashboard): RegisterPage with LoginSplit pattern (T-G.2)`

---

### T-G.3 — Refactor VerificationPage

#### Files to edit
```
cloud/dashboard/src/pages/verification.tsx — rewrite (current 140 LOC → target 70)
```

#### Deep Dives
- Clean centered Card (no LoginSplit — minimal flow)
- States: input email → submit → check email message (EmptyState success variant)
- `<FormField>` + `<Alert>` for errors + `<EmptyState intent="success">` post-submit

#### TDD
```
RED:     test_verification_renders_email_input()
RED:     test_verification_submit_triggers_send()
RED:     test_verification_success_state_after_send()
GREEN:   Rewrite
VERIFY:  npm test -- verification
```

#### Acceptance Criteria
- [ ] LOC ≤70
- [ ] 3 RED tests verdes

#### DoD
- [ ] Commit `refactor(dashboard): VerificationPage with clean form + EmptyState (T-G.3)`

---

### T-G.4 — Refactor RecoveryPage

Same pattern as T-G.3. (Recovery flow is structurally similar to verification — submit email → check inbox.)

#### Acceptance Criteria
- [ ] LOC ≤70
- [ ] 3 RED tests verdes

#### DoD
- [ ] Commit `refactor(dashboard): RecoveryPage with clean form + EmptyState (T-G.4)`

---

### T-G.5 — Refactor DeviceSuccessPage

#### Files to edit
```
cloud/dashboard/src/pages/device-success.tsx — rewrite (current 21 LOC → target 25)
```

#### Deep Dives
- `<EmptyState intent="success" icon={CheckCircle2} title="You're signed in" description="Return to your terminal to continue." />`
- LOC may stay same — already simple

#### TDD
```
RED:     test_device_success_renders_success_state()
GREEN:   Rewrite
VERIFY:  npm test -- device-success
```

#### Acceptance Criteria
- [ ] LOC ≤25
- [ ] 1 RED test verde

#### DoD
- [ ] Commit `refactor(dashboard): DeviceSuccessPage with EmptyState success (T-G.5)`

---

## Phase H: Validation

**Objective:** Validar mockup fidelity + design language coerência + Rule 3 + bundle delta + reconciliação com v1.3.

### T-H.1 — Visual smoke test (13 routes)

#### Tasks
1. `task setup && tilt up`
2. Abrir Chrome dark mode em `https://cloud.loopzero.local`
3. Para cada uma das 13 rotas, capturar screenshot + side-by-side compare com expectativa
4. Para OverviewPage especificamente: comparar com mockup HTML rendered

#### Acceptance Criteria
- [ ] OverviewPage visual delta ≤5% vs mockup (sponsor judgment)
- [ ] Outras 12 pages design language coerente (sidebar/topbar/cards consistent)
- [ ] Zero console errors em qualquer route
- [ ] Login flow (email+password + OIDC GitHub) funcional end-to-end

#### DoD
- [ ] Screenshot log committed to `docs/reviews/2026-05-24-dashboard-visual-smoke.md`

---

### T-H.2 — Rule 3 audit + legacy token migration check (EC-6)

#### Tasks
1. Em browser dev tools console rodar (Rule 3 standard):
   ```js
   document.body.innerText.match(/Kubernetes|ArgoCD|Gitea|MinIO|Cosign|Pulumi|Helmfile|Tilt|Hydra|Kratos|pgvector|cell-\d/gi)
   ```
2. Validar resultado é `null` em TODAS as 13 routes
3. Validar zero termos "agent-shaped" out-of-context per EC-7 v1.3:
   ```js
   document.body.innerText.match(/Mem0|\bMCP\b|agent runtime|tool call|skill manager|sub-agent/gi)
   ```
4. **CRITICAL (EC-6 v1.1)**: grep source para legacy tokens não migrados:
   ```bash
   cd cloud/dashboard && grep -rnE '\b(text-white|bg-surface-[0-4])\b' src/ --include='*.tsx' --include='*.ts' | grep -v node_modules
   ```
   Resultado **DEVE ser zero matches**. Se retornar matches:
   - `text-white` → substituir por `text-foreground`
   - `bg-surface-0` → `bg-background`
   - `bg-surface-1..4` → `bg-card` (ou token semantic apropriado)
   - Re-rodar grep até zero
5. Tokens locais (`@theme {}` block em index.css) serão prunados em v1.3 T6.2 — coordenar com aquele plano
6. **NEW D-9 v1.2 — anti-pattern validation per `.llms.txt`**:
   ```bash
   # Raw hex/rgb (line 127 anti-pattern):
   grep -rnE '#[0-9a-fA-F]{3,6}\b|rgb\(' cloud/dashboard/src/ --include='*.tsx' --include='*.ts'
   # MUST return zero — tokens only
   
   # Inline SVG (line 118 anti-pattern, "Never inline SVG"):
   grep -rnE '<svg ' cloud/dashboard/src/ --include='*.tsx'
   # MUST return zero — use Lucide
   
   # Manual portal wraps (line 131 anti-pattern):
   grep -rnE 'createPortal|<Portal' cloud/dashboard/src/ --include='*.tsx'
   # SHOULD be zero in OUR code (only @loopzero/ui internals)
   
   # Disabled lint rules without WAI-ARIA justification (line 132):
   grep -rnE 'eslint-disable|biome-ignore' cloud/dashboard/src/ --include='*.tsx' --include='*.ts'
   # Each match must have "WAI-ARIA" comment OR equivalent inline justification
   ```
7. **NEW D-3 v1.2 — bundled deps check (coordena com v1.3 T6.1 cleanup)**:
   ```bash
   # Per .llms.txt line 72, these are bundled by @loopzero/ui — should NOT be direct deps:
   for dep in '@radix-ui/react-avatar' '@radix-ui/react-dropdown-menu' '@radix-ui/react-separator' '@radix-ui/react-slot' '@radix-ui/react-tooltip' 'class-variance-authority' 'cmdk'; do
     grep -q "\"$dep\":" cloud/dashboard/package.json && echo "REDUNDANT (per .llms.txt): $dep"
   done
   # Each match indicates dep to remove in v1.3 T6.1 (NOT in this plan — coordinated cleanup)
   # Exception: lucide-react/clsx/tailwind-merge — see D4 v1.3 + D-3 v1.2 — kept direct for now, decision deferred to v1.3 T6.1 final review
   ```

#### Acceptance Criteria
- [ ] Rule 3 grep clean em 13 routes (tasks 1-2)
- [ ] EC-7 agent-shaped grep clean (task 3)
- [ ] **EC-6 v1.1**: zero matches de `text-white|bg-surface-[0-4]` em src/ (task 4)
- [ ] **D-9 v1.2 anti-patterns**: zero raw hex/rgb, zero inline `<svg>`, zero `createPortal` em consumer code, zero `eslint-disable`/`biome-ignore` sem justificativa (task 6)
- [ ] **D-3 v1.2 bundled deps**: lista de deps redundantes catalogada (não removidas neste plano — handoff para v1.3 T6.1) (task 7)

#### DoD
- [ ] Audit log appended to T-H.1 doc com 6 sections (Rule 3, agent-shape, EC-6 legacy tokens, D-9 anti-patterns, D-3 bundled deps catalog, sponsor confirmation)

---

### T-H.3 — Bundle delta measurement

#### Tasks
1. `cd cloud/dashboard && npm run build`
2. Comparar `dist/assets/*.{js,css}` size + gzip vs T0.0 baseline em `docs/reviews/2026-05-23-dashboard-bundle-baseline.txt`
3. Documentar delta em `docs/reviews/2026-05-24-dashboard-bundle-post-redesign.txt`

#### Acceptance Criteria
- [ ] Delta total gzip ≤+30% vs T0.0 baseline (per D7)
- [ ] Se exceder: investigar (tree-shake quebrado? builtinThemes carregando full?)

#### DoD
- [ ] Bundle post-redesign log committed

---

### T-H.4 — Reconcile with v1.3 plan

#### Objective
Atualizar [`docs/plans/2026-05-23-dashboard-loopzero-ui-migration-plan.md`](./2026-05-23-dashboard-loopzero-ui-migration-plan.md) para marcar Phases 2/3/4/5 como SUBSUMED por este plano. Garantir que Phase 6 (cleanup) ainda roda apropriadamente.

#### Tasks
1. Edit v1.3 plan: add note in Phases 2/3/4/5 headers marcando "SUBSUMED by 2026-05-24-dashboard-visual-redesign-mockup-aligned-plan"
2. Bump v1.3 to v1.4 with changelog entry documentando subsume
3. Verify v1.3 Phase 6 (cleanup) tasks ainda fazem sentido após este plano (remove deps redundantes, prune Tailwind tokens não-usados, etc.)

#### Acceptance Criteria
- [ ] v1.3 plan bumped to v1.4 with subsume notes
- [ ] Phase 6 still relevant

#### DoD
- [ ] Commit `docs(dashboard): v1.3 → v1.4 — Phases 2-5 subsumed by visual redesign plan (T-H.4)`

---

## Coverage Matrix

| # | Mockup region / requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Sidebar shell 200px | T-C.1 | AppSidebar com Sidebar primitive |
| 2 | AccountMenu header com plan badge | T-C.1 | AccountMenu + PlanBadge composites |
| 3 | Sidebar search field | T-C.1 | Input com Search icon |
| 4 | Sidebar nav items (12 reais LoopZeroCloud) | T-C.1 | Sidebar.Item × 8 routes |
| 5 | Sidebar footer (Settings/Profile) | T-C.1 | Sidebar.Footer com items |
| 6 | TopBar "All Projects ▾" | T-C.2 | Select primitive |
| 7 | TopBar page title centralizado | T-C.2 | center slot |
| 8 | TopBar dots menu | T-C.2 | DropdownMenu via Radix |
| 9 | ActionBar pattern (search + filter + CTA) | T-B.3 | ActionBar component |
| 10 | 2-col grid 1.2fr:1fr | T-B.3 | ContentGrid columns="2-asymmetric" |
| 11 | Usage card "Last 30 days" com 3 progress bars | T-D.1 | UsageMeter primitive |
| 12 | Alerts empty state com CTA "Upgrade to Pro" | T-D.1 | EmptyState + Button |
| 13 | Projects card com project rows | T-D.1 | Card + ProjectCard composite |
| 14 | Recent Previews empty state | T-D.1 | EmptyState (eye icon) |
| 15 | Status dots (orange/green/red) | T-E.2, T-E.4 | StatusDot primitive |
| 16 | Timestamps relative | T-D.1, T-E.2 | Timestamp primitive |
| 17 | DashboardLayout shell | T-B.2 + T-C.3 | DashboardLayout component |
| 18 | Design language doc | T-B.1 | docs/design/.md artifact |
| 19 | ProjectsPage redesign | T-E.1 | ProjectCard grid + ActionBar |
| 20 | EnvironmentsPage redesign | T-E.2 | Table + StatusDot + Timestamp |
| 21 | BillingPage redesign | T-E.3 | UsageMeter + Table + CostMeter |
| 22 | DomainsPage redesign | T-E.4 | DomainConfig + CodeBlock + StatusDot |
| 23 | TeamPage redesign | T-F.1 | Member Table + ConfirmDialog |
| 24 | SettingsPage redesign | T-F.2 | Sections + DangerZone |
| 25 | ProfilePage redesign | T-F.3 | Avatar + sections + DangerZone |
| 26 | LoginPage redesign | T-G.1 | LoginSplit + FormField + Alert |
| 27 | RegisterPage redesign | T-G.2 | LoginSplit pattern |
| 28 | VerificationPage redesign | T-G.3 | Clean form + EmptyState |
| 29 | RecoveryPage redesign | T-G.4 | Clean form + EmptyState |
| 30 | DeviceSuccessPage redesign | T-G.5 | EmptyState success |
| 31 | VerificationBanner uses Alert | T-E.5 | Alert primitive (Brief #3) |
| 32 | v1.3 plan reconciled | T-H.4 | v1.3 bumped v1.4 with subsume notes |

**Coverage: 32/32 mockup requirements covered (100%)**

### v1.2 .llms.txt alignment matrix (extra)

| # | .llms.txt divergence | Task(s) | Resolution |
|---|---|---|---|
| D-1 | CSS import strategy (`preset` vs 3 separate) | T-A.0 (NEW) | Verify content, decide simplify or document |
| D-2 | Card sub-component naming (Card.Header/Body) | v1.3 T1.3 + T-A.1 acceptance | Enforce namespaced form, grep check |
| D-3 | Bundled deps anti-pattern (Radix/CVA/cmdk redundant) | T-H.2 task 7 + v1.3 T6.1 | Catalog now, remove in v1.3 cleanup |
| D-4 | LoopZeroUIProvider not in .llms.txt | ADR D9 (NEW) | Acknowledge superset, code wins per line 268 |
| D-5 | "14 primitives" misnomer | Changelog v1.2 | Corrected to "10 primitives + 4 composites" |
| D-6 | docs.loopzero.example MDX pages as spec | T-G.1 + multiple tasks | Reference URLs added |
| D-7 | SVG inline removal (Google logo in login.tsx:200) | T-G.1 Deep Dive | Explicit task step |
| D-8 | .llms.txt not cited as source-of-truth | Plan header (NEW section) | Source-of-truth references added |
| D-9 | Anti-pattern validation absent | T-H.2 task 6 (NEW) | Grep raw hex, inline SVG, portals, lint disables |
| D-10 | `pnpm quality:gates` not mentioned | Side note (TBD) | Library-side, not consumer — note for future |

**.llms.txt alignment: 10/10 divergences resolved (100%)**

## Global Definition of Done

- [ ] All 8 phases (A-H) completed
- [ ] All tests passing (vitest)
- [ ] Zero typecheck errors (`tsc -b` clean)
- [ ] Build verde
- [ ] Backward compatibility: routes preserved, auth/JWT/Kratos/BroadcastChannel intact (D3 of v1.3 still holds)
- [ ] LOC dashboard src/ ≤1.400 (target -52% vs T0.0 baseline 2.914)
- [ ] Bundle delta vs T0.0 baseline ≤+30% gzip (D7)
- [ ] Rule 3 clean: zero internal terms in DOM (T-H.2)
- [ ] **Cross-validation `/cross-validation 2026-05-24-dashboard-visual-redesign-mockup-aligned` PASSED** — zero CRÍTICO, zero GRAVE
- [ ] **Evidence**: N/A — UI-only refactor; doesn't affect maturity scores
- [ ] **Dogfood Gate** below PASSED

## Cross-reference with v1.3 plan

| v1.3 Phase/Task | Status pós este plano | Razão |
|---|---|---|
| v1.3 Phase 0 (T0.0, T0.0.bis, T0.1, T0.2, T0.3) | ✅ DONE (commits c18704c3 etc) | Foundation reusable |
| v1.3 Phase 1 (T1.1 Button, T1.2 Badge, T1.3 Card, T1.4 /recovery) | **DEVE rodar ANTES deste plano** | Mecânico, desbloqueia Phase A |
| v1.3 Phase 2 (T2.1 Sidebar, T2.2 PageWrapper, T2.3 VerificationBanner) | **SUBSUMED** | Reescrito como T-C.1 + T-B.2 + T-E.5 deste plano |
| v1.3 Phase 3 (T3.1-T3.5 auth pages) | **SUBSUMED** | Reescrito como T-G.1-T-G.5 deste plano |
| v1.3 Phase 4 (T4.1-T4.5 core pages) | **SUBSUMED** | Reescrito como T-D.1 + T-E.1-T-E.4 deste plano |
| v1.3 Phase 5 (T5.1-T5.3 secondary pages) | **SUBSUMED** | Reescrito como T-F.1-T-F.3 deste plano |
| v1.3 Phase 6 (T6.1-T6.6 cleanup) | **PRESERVED** | Roda APÓS Phase H deste plano |

T-H.4 deste plano documenta o subsume em v1.3 bumping para v1.4.

---

## Dogfood Gate (MANDATORY)

> Plano UI-only; dogfood escopado para fluxos visuais + auth E2E + zero regression em backend.

### Execução

```bash
# 1. Fresh infrastructure
task clean:all && task setup && tilt up
task doctor

# 2. Build + deploy dashboard image
cd cloud/dashboard && docker build -t loopzero-dashboard:test .

# 3. Smoke test estendido per T-H.1 (sponsor side, 13 routes browser)
```

### Pass Criteria

- [ ] `task doctor` reports 38/0 (or current baseline)
- [ ] Login Kratos (email+password + GitHub OAuth) → JWT obtained → Overview renders mockup-aligned
- [ ] Cross-tab logout: BroadcastChannel propagates → all tabs redirect /login
- [ ] All 13 routes render without console errors
- [ ] Recovery flow: Click "Forgot password?" → RecoveryPage → submit email → success state
- [ ] VerificationBanner: appears when email unverified, resends correctly
- [ ] Rule 3: grep clean in DOM (T-H.2)
- [ ] No regression: `cd cli && npm test`, `cd api && go test ./...` green
- [ ] Container rebuild: `docker build` exits 0, `__config.js` generated correctly at runtime

### On Failure

S0/S1 → fix → re-run smoke. S2-S4 documented em `docs/reviews/2026-05-24-dashboard-redesign-dogfood.md`.

---

## Riscos identificados

| Risco | Mitigação |
|---|---|
| `Sidebar.Item` polymorphism (`as={Link}`) não funciona — react-router SPA navigation quebra | T-C.1 task 1 inspeciona .d.ts antes; fallback wrap manual com onClick + navigate |
| `Select` API real diverge do esperado em T-C.2 | T-C.2 task 2 valida API real; ajustar antes de implementar |
| `LoginSplit` primitive não fit no Kratos OIDC flow | T-G.1 fallback para clean Card layout (D5 ADR já permite ambos) |
| `ProjectCard` composite props requer fields que backend não retorna | EC-5 v1.3 já tratou — adapter com defaults |
| Bundle exceeds D7 budget (+30% gzip) | Phase H decide swap LoopZeroUIProvider→ThemeProvider single-theme (D8) |
| Mockup fidelity discordância (sponsor diz "não tá igual") | Sponsor é juiz final; iterate em pequenos commits per region |
| `@loopzero/ui` v0.9.0-next.0 ship breaking change durante implementação | Plano pina exato `0.9.0-next.0` em v1.3 T0.1 — Renovate liberado só em Phase 6 |
| LoginSplit primitive doesn't exist (eu assumi sem verificar) | T-G.1 task pre-flight verify; fallback documented |

## Alternativas rejeitadas

| Alternativa | Por que rejeitada |
|---|---|
| Hand-roll layout sem layout components | Drift visual cross-page; reinvent em cada page |
| Tirar v1.3 Phase 1 do pre-requisite (fazer swap implícito por page) | Mistura mecânico com design — debug fica difícil |
| Single page (Overview) only, deixar outras pages como-is | Quebra coerência visual; "home moderna, resto antigo" UX horrível |
| Implementar Pixel-perfect screenshot regression infra agora | Out of scope; D4 ADR defere para plano sucessor |
| Generate todas as 11 outras pages com IA sem mockup | Drift do design language; sponsor não validou patterns visuais |
| Adicionar features fictícias do mockup Vercel (AI Gateway, Firewall) | Viola D1 + Rule 9 (não inventar features) |

---

## Status

**Created**: 2026-05-24
**Author**: claude-opus-4-7 via `/to-plan`
**Sponsor**: paulo (paulohenriquevn@gmail.com)
**Tier**: T3 (refactor visual + estrutural + novos layout components locais)
**Estimated effort**: 7-12 sessões de trabalho (Phase A: 0.5h; Phase B: 2-3h; Phase C: 3-4h; Phase D: 2-3h; Phase E: 4-6h; Phase F: 2-3h; Phase G: 3-4h; Phase H: 1-2h)
**Dependencies**: v1.3 Phase 1 (mecânico, 1-2 sessões) DEVE rodar antes
**Edge-Case-Review**: PASSED v1.1 — [`docs/plans/2026-05-24-dashboard-visual-redesign-mockup-aligned-edge-cases.md`](./2026-05-24-dashboard-visual-redesign-mockup-aligned-edge-cases.md) (16 edges: 7 MUST FIX incorporados em v1.1, 6 SHOULD TEST adicionados aos TDD sections, 3 DOCUMENT como notas)
**Source-of-truth alignment**: PASSED v1.2 — `.llms.txt` (`cloud/dashboard/.llms.txt`, sync 2026-05-24, v0.9.0-next.0) cross-checked; 10 divergências identificadas e corrigidas inline (D-1 a D-10 no changelog v1.2). Plan v1.2 explicitly references .llms.txt em cada seção relevante.
**Próximo gate**: sponsor approval → v1.3 Phase 1 execution → este plano Phase A (com T-A.0 novo) start
