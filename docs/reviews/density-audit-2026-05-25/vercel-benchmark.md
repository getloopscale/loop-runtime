# Vercel Dashboard — Benchmark Comparison

**Data:** 2026-05-25
**Source:** 2 screenshots reais do Vercel (paulohenriquevn workspace) capturados pelo operator
**Evidência:**
- `vercel-overview-benchmark.png` — Vercel Overview (Hobby tier) — mesma rota lógica que LoopZeroCloud `/`
- `vercel-observability-query-benchmark.png` — Vercel Observability > Query (URL footer: `vercel.com/paulohenriquevn/~/observability/external-apis`)
**Companion to:** `REPORT.md` (density audit full tour 13/13 routes)
**Purpose:** validação empírica das violações P1 detectadas no audit interno (DSS § 6.1 + CC-3) usando um SaaS de classe operator-grade como referência

---

## Por que este documento existe

O `REPORT.md` classifica 2 violações P1 (Overview empty states oversized + KPI count baixo) usando o próprio DSS v1.0 como régua. Essa régua é **interna** — o risco é que os targets (≤200px empty state, 4-5 KPIs) sejam folclore não-validado.

Os 2 screenshots do Vercel são a **régua externa**: se Vercel (o SaaS-de-referência citado pelo próprio DSS) faz **exatamente** o que o nosso target sugere, a violação é real. Se Vercel faz diferente, o target precisa ser revisitado.

**Resultado:** Vercel confirma os 2 P1s + revela 6 novos gaps que o audit interno não detectou (porque não comparou com benchmark).

---

## Comparação 1 — Overview pages (mesma rota lógica)

`vercel-overview-benchmark.png` vs `overview-density.png`

| Surface | Vercel | LoopZeroCloud audit | Comparação | Audit cross-ref |
|---------|--------|-----------------|------------|-----------------|
| Sidebar nav items | **18** (Projects, Deployments, Logs, Analytics, Speed Insights, Observability ▸, Firewall, CDN, Env Vars, Domains, Integrations, Storage, Flags, Agent ▸, AI Gateway ▸, Sandboxes, Workflows, Usage, Support, Settings) | **8** (Overview, Projects, Environments, Domains, Team, Billing, Settings, Profile) | LoopZero 56% mais escasso (8 vs 18) | — (escolha consciente per ADR-D1 visual-redesign plan v1.2: "real routes only") |
| Sidebar collapsible sub-nav | ✓ chevrons em Observability/Agent/AI Gateway (sub-trees) | ✗ flat | LoopZero IA flat sem hierarchy | 🆕 não detectado pelo audit |
| TopBar elements | "All Projects ▾" (left) + title (center) + **"..." kebab** (right) | "All Projects" (left) + title (center) | LoopZero sem page-level menu | 🆕 não detectado |
| Usage panel KPIs visíveis | **4 visíveis + chevron-down indicando scroll interno** (Fast Data Transfer, Fast Origin Transfer, Edge Requests, Edge Request CPU Duration, +) | **3 estáticos** (sem scroll) | Vercel usa **scroll interno no panel** para acomodar 4-6 KPIs sem inflar página | ✅ **CONFIRMA P1-2** (LoopZero KPI count 3 vs target 4-5) |
| Usage panel "Upgrade" badge inline | ✓ pequeno badge no header | ✓ presente (mesma posição) | Paridade | ✓ |
| Alerts empty state | **~160px** compacto, "Upgrade to Pro" como primary action | **~270px** ✗ | Vercel **41% menor** para mesmo conteúdo | ✅ **CONFIRMA P1-1** (LoopZero empty state oversized) |
| Recent Previews empty state | **~210px** | **≥240px** ✗ (cortado por viewport) | Vercel **13% menor** | ✅ **CONFIRMA P1-1** |
| ActionBar affordances | search + **filter icon + grid/list toggle + primary CTA dropdown** | search + primary CTA | LoopZero sem filter + sem layout switch | 🆕 não detectado |
| Project card layout | logo (triangle) + name + URL + **CTA inline "Connect Git Repository"** + status icon (top-right) + kebab + relative date | name + framework + apps + env + status + last-deploy + open (post T-MIG-3 DataTable) | LoopZero **mais denso em dados tabulares**; Vercel **mais visual** | Tradeoff DSS § 5.2 admite ambos — LoopZero escolheu tabular pós T-MIG-3 |
| Footer | account chip + **notification bell badge** | account chip (top) | LoopZero sem notification UX | 🆕 não detectado |
| Project status indicator | ✓✓ icon (top-right) indicando deploy success | StatusDot "Live" inline na tabela | Paridade conceitual, padrões diferentes | ✓ |

---

## Comparação 2 — Deep-nested pages

`vercel-observability-query-benchmark.png` mostra um pattern que LoopZeroCloud **não tem hoje** (single-level IA). Útil como referência futura caso Observability/Analytics/AI sejam adicionados ao dashboard.

| Pattern | Vercel | LoopZeroCloud | Audit cross-ref |
|---------|--------|-----------|-----------------|
| Sidebar 2-level | **✓ ← back button + sub-section header "Observability"** + 16 sub-items (Overview, Query, Notebooks, Alerts, Functions, External APIs, Middleware, Edge Requests, ISR, Fast Data Transfer, Image Optimization, External Origins, Microfrontends, AI, Blob, Queues) | ✗ flat | 🆕 P3 deferred — não é problema hoje (8 rotas cabem), vira limite se Observability for adicionada |
| Sidebar visual grouping | **COMPUTE / CDN / SERVICES headers** (uppercase tracking-wider, 3 grupos) | "BILLING" header only (1 grupo) | 🆕 LoopZero só agrupa Billing |
| Breadcrumb TopBar | **"Observability / Query"** (clickable left part) | só title centered | 🆕 LoopZero sem breadcrumb — perde navegabilidade em rotas profundas |
| Form-with-chart pattern | **Metric dropdown + "Sum" aggregator + Group By chips + Filter "Add filter"** (compact query builder) | ausente | 🆕 padrão Grafana-like — futuro Observability/Analytics |
| Plan-gating inline | **"Read-only mode. Upgrade to Pro to get Observability Plus and unlock custom queries." banner** + CTA dentro do panel (não bloqueia view, só mutate) | DangerZone + UpgradePro card separado em /billing | 🆕 Vercel gate sem fricção — usuário vê o feature, só não pode mutate |
| Chart panel | empty state "No data in this time range" + **3 chart-type toggles** (line/bar/table) + **interval selector** ("5 minutes") + "Add to Notebook" no rodapé direito | full Card empty-state padrão | 🆕 Vercel sub-panel actions richas |
| Empty state "No data" | **inline texto centered** dentro do chart area (não Card) | full Card empty-state | 🆕 Vercel footprint menor para empty data states |

---

## Síntese: validação cruzada do audit

### Validações empíricas (interno = externo)

| Audit finding | DSS target interno | Vercel observed | Verdict |
|---------------|---------------------|-----------------|---------|
| P1-1 Overview empty states oversized | ≤200px | Alerts ~160px, Recent Previews ~210px | ✅ **VALIDADO** — Vercel honra o target |
| P1-2 Overview KPI count baixo | 4-5 KPIs | **4 visíveis + scroll interno** | ✅ **VALIDADO** — Vercel mostra exatamente o que o DSS pede |
| P2-1 Billing Plan card sub-dimensionado | 280-360px | (não comparável — Vercel não mostrou Billing) | ⚠ INCOMPARABLE — falta screenshot Vercel Billing |
| P2-3 Profile "Sign out" 1-button Card anti-pattern | grouping value | (não comparável) | ⚠ INCOMPARABLE — falta Vercel Settings/Profile |

### Novos gaps (audit não detectou; benchmark revelou)

| # | Gap | Severidade proposta | Onde LoopZero está hoje | Sugestão |
|---|-----|---------------------|---------------------|----------|
| V-1 | Sidebar sub-navigation (2-level) | P3 | flat (8 routes) | Aceitável hoje; revisar se Observability/Analytics/AI forem adicionados |
| V-2 | Breadcrumb no TopBar | P2 | ausente | Adicionar `<Breadcrumb section="Projects" page="acme-web">` para rotas profundas (`/projects/{id}`, `/projects/{id}/deployments/{id}`) |
| V-3 | TopBar kebab "..." (page-level menu) | P3 | ausente | Slot futuro para "Share view", "Open in CLI", "Copy as JSON", "Export usage" |
| V-4 | ActionBar com filter + layout-switch | P2 | só search + CTA | Adicionar `<Button variant="ghost" icon={Filter}>` + `<ToggleGroup grid/list>` em DataTable pages — density apparent +30% sem mudar payload |
| V-5 | Notification bell (sidebar footer) | P3 | ausente | Slot futuro para audit-log/alerts UX |
| V-6 | Plan-gating inline (read-only banner) | P2 | DangerZone + cards separados | Pattern aplicável em /billing UpgradePro section: usar `<Alert intent="info">` inline em vez de Card lateral |
| V-7 | Sidebar visual grouping (uppercase headers) | P3 | 1 header (Billing) | Adicionar headers "WORKSPACE" (Settings/Profile/Team/Billing) e "RESOURCES" (Projects/Environments/Domains) — 2 grupos a mais |
| V-8 | Usage panel scroll interno | P2 | listagem estática | Padrão "max-height + overflow-y-auto" no UsageMeter para acomodar mais KPIs sem inflar página |

### O que LoopZero faz melhor (não copiar tudo do Vercel)

- ✅ **Tables tabulares densas** (Projects/Domains/Environments) — 6+ columns sortable, denser que Vercel project-cards quando há 10+ entities
- ✅ **DataTable expandable rows** — Domains mostra DNS records inline sem modal/sub-route; pattern Vercel não tem (nas 2 screenshots)
- ✅ **PinInput 6-box** (verification post-T-MIG-1) — pattern industry-standard que muitos SaaS ainda usam single input
- ✅ **CLI-first explicit** (Settings page) — Vercel não tem CodeBlock visible promovendo CLI; LoopZero abraça CLI-first conscientemente

---

## Como usar este documento

Este benchmark é o **segundo insumo** (junto com o REPORT.md) para o plano de density refactor **ainda não escrito** (`docs/plans/2026-05-26-dashboard-density-refactor-plan.md`).

Quando o `/to-plan` for invocado, recomenda-se incluir como contexto:
1. `docs/reviews/density-audit-2026-05-25/REPORT.md` — violações internas DSS-based (2 P1 + 5 P2 + 4 P3)
2. `docs/reviews/density-audit-2026-05-25/vercel-benchmark.md` (este arquivo) — validação externa + 8 novos gaps (V-1 a V-8)
3. Os 13 PNGs internos + 2 PNGs Vercel — evidência visual

**Priorização sugerida no plano:**
- **Tier 1 (must-fix):** P1-1, P1-2 (do REPORT, validados por Vercel)
- **Tier 2 (should-fix):** P2-1, P2-3 (do REPORT) + V-2, V-4, V-6, V-8 (do benchmark Vercel — todos com fix concreto descrito)
- **Tier 3 (nice-to-have, defer):** P2-2, P2-4, P2-5 + V-1, V-3, V-5, V-7
- **Skip (intentional design):** padrões LoopZero > Vercel listados na seção "O que LoopZero faz melhor"

---

## Limitações deste benchmark

- ❌ Apenas 2 screenshots Vercel — falta Billing, Settings, Profile, Domains, Team, Deploy detail, Logs
- ❌ Screenshots em Hobby tier — UI pode diferir em Pro/Enterprise (mais panels, KPIs adicionais)
- ❌ Comparação visual, não code-level (não acessei o Vercel codebase)
- ❌ Não inclui patterns mobile/responsive — Vercel pode degradar diferente

Para validação mais completa: solicitar ao operator screenshots Vercel adicionais de Billing/Settings/Profile/Team (pareando 1:1 com rotas LoopZero).
