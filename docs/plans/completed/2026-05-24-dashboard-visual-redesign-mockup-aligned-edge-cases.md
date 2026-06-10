# Edge Case Review — 2026-05-24-dashboard-visual-redesign-mockup-aligned

**Data:** 2026-05-24
**Plano:** `docs/plans/2026-05-24-dashboard-visual-redesign-mockup-aligned-plan.md` v1.0
**Tasks analisadas:** 22 (T-A.1, T-B.1, T-B.2, T-B.3, T-C.1, T-C.2, T-C.3, T-D.1, T-E.1, T-E.2, T-E.3, T-E.4, T-E.5, T-F.1, T-F.2, T-F.3, T-G.1, T-G.2, T-G.3, T-G.4, T-G.5, T-H.1-4)
**Edge cases encontrados:** 16 (MUST FIX: 7, SHOULD TEST: 6, DOCUMENT: 3)

> **Honesty note**: este plano é UI-only refactor — os patterns sistêmicos clássicos do LoopZero (multi-cell, GitOps, advisory_lock, exit codes) NÃO se aplicam. Edges concentram-se em (a) coisas que assumi sem verificar (APIs de primitives, shape de endpoints backend), (b) wiring entre componentes que o plano deixou ambíguo, (c) compatibility entre tokens antigos (`text-white`) e novos (`text-foreground`).

---

## MUST FIX (7)

### EC-1: Sidebar.Item polymorphism para react-router SPA navigation

- **Task afetada:** T-C.1
- **Família:** Integration / Format
- **Cenário:** Plano T-C.1 usa `<Sidebar.Item as="a" href="/projects">`. `as="a"` produz tag `<a>` HTML padrão — clicar dispara **full page reload** (browser navigation), NÃO react-router SPA navigation. Resultado: cada clique no sidebar recarrega o bundle inteiro, perde JWT em memória, força re-auth.
- **Impacto:** UX quebrada. SPA deixa de ser SPA. JWT em memória perdido a cada click do menu (segundo D3 do plano, esse estado é INTACTO — mas a interação básica do sidebar quebra esse invariante).
- **Fix sugerido:** Adicionar à T-C.1 task 1 (audit `.d.ts`): "se Sidebar.Item aceita `as={Component}` polymorphic, passar `as={Link} to={path}` em vez de `as=\"a\" href={path}`. Se NÃO aceitar Component generic, wrap manualmente: `<Link to={path}><Sidebar.Item icon={Icon}>{label}</Sidebar.Item></Link>` (Link external, Item internal — sacrifica algum styling de active state, ajustar via location.pathname dentro do Item)."

### EC-2: `pageTitle` prop de DashboardLayout sem mecanismo de set por page

- **Task afetada:** T-B.2, T-C.3
- **Família:** State / Format
- **Cenário:** T-B.2 define `DashboardLayout({ pageTitle })` mas o `app.tsx` renderiza `<DashboardLayout><Routes>...</Routes></DashboardLayout>` — páginas filhas não têm como passar `pageTitle` para o ancestral. Resultado: TopBar renderiza sem título OU sempre o mesmo título OU mockup spec da Overview ("Overview" centralizado) impossível de cumprir.
- **Impacto:** TopBar fica sem identificação da página atual. Mockup fidelity (D2) falha — Overview literal requer "Overview" no top bar center slot.
- **Fix sugerido:** Adicionar à T-B.2: criar `src/hooks/use-page-meta.ts` minimal — `<PageMetaContext.Provider>` em DashboardLayout, `usePageMeta()` hook em cada page chama `setPageTitle('Overview')` em useEffect. Equivalente em 15 LOC. Alternative simpler: derive pageTitle from location.pathname mapping em DashboardLayout (single source of truth, no per-page wiring). Plano deve escolher uma das duas explicitamente.

### EC-3: TopBar project selector duplica fetch de `/api/v1/projects` com Overview

- **Task afetada:** T-C.2, T-D.1
- **Família:** Resource / Timing
- **Cenário:** T-C.2 cita "Select primitive — TODO: fetch /api/v1/projects in T-D.1" mas TopBar renderiza em **todas as 8 dashboard pages**, não só Overview. Se TopBar fetcha quando montado E Overview fetcha quando montado, mesma chamada `/api/v1/projects` roda 2x em paralelo em cada page load. Dobra latência percebida + dobra carga no backend.
- **Impacto:** Backend recebe 2× chamadas redundantes em cada page load. Overview/Projects/Environments/etc. — sob carga, isso multiplica.
- **Fix sugerido:** Adicionar à T-C.2 (NEW task ou expand task 2): criar `src/hooks/use-projects.ts` — wraps `useEngineApi('/api/v1/projects')` num módulo singleton com SWR-style cache (60s TTL). DashboardLayout invoca uma vez via Provider context. TopBar + Overview + Projects consomem via context. 25 LOC. Alternative: aceitar dupla chamada agora (caches em browser nível resolverão), documentar como DOCUMENT em vez de MUST FIX se sponsor preferir KISS.

### EC-4: `/v1/billing` response shape assumption não verificada

- **Task afetada:** T-D.1, T-E.3
- **Família:** Format / Integration
- **Cenário:** T-D.1 assume `useCloudApi<UsageResponse>('/v1/billing')` retorna `{ dataTransferGB, originTransferGB, requests }`. Mas `cloud/internal/routes/billing.go` GetBilling handler (analisado em sessão anterior — análise do módulo cloud/) retorna **plan + status + period**, NÃO métricas de usage. Métricas de usage estão em `cost_counters` table (cost aggregator escreve), separada do endpoint /v1/billing.
- **Impacto:** UsageMeter renderiza com `value=0, max=100` para todas as 3 métricas (defaults). Mockup fidelity quebra — mostra usage mas sempre zerado.
- **Fix sugerido:** Adicionar à T-D.1 task 0 (NEW pre-step): "Ler `cloud/internal/routes/billing.go` GetBilling handler para confirmar response shape REAL. Se não inclui usage metrics, adicionar nota: 'UsageMeter renderiza com placeholder data até endpoint /v1/billing/usage ou /v1/usage existir — abrir issue cloud-api separada'". Mockup fidelity aceita placeholders zerados (mockup tem `0/100GB` literal).

### EC-5: Endpoint de invoice history pode não existir em `cloud/`

- **Task afetada:** T-E.3
- **Família:** Integration
- **Cenário:** T-E.3 (BillingPage) propõe Table com "invoice history" populada. Mas Brief #2 explicitamente disse "Billing invoices > 12 months may exceed page" como cenário FUTURO de Pagination — sugerindo histórico nem é endpoint canônico no Stripe→cloud pipeline. cloud BillingHandler tem `GetBilling` + `Portal` + `Checkout` + `Webhook` — não vejo `GetInvoices`.
- **Impacto:** Page renderiza Table vazia ou crash em response inesperado.
- **Fix sugerido:** Adicionar à T-E.3 task 0 (NEW pre-step): "Verificar se cloud expõe `/v1/billing/invoices` ou similar. Se NÃO existe: BillingPage v1 omite invoice Table; renderiza só Plan summary + Usage card. Invoice Table fica como follow-up plan quando endpoint shipar (Stripe API stripe.invoices.list pode ser chamado, mas requer endpoint cloud novo)."

### EC-6: `text-white` → `text-foreground` migração não especificada

- **Task afetada:** Phase D, E, F, G (todas as pages)
- **Família:** Format
- **Cenário:** Dashboard atual usa `text-white` literal em ~30+ sites (login.tsx:163-238, overview.tsx, app.tsx wrapper divs, etc.). @loopzerokit/ui design system expõe tokens semânticos como `text-foreground`, `text-card-foreground`, `text-muted-foreground`. Em Violet Forge dark, `text-foreground` resolve para branco-quase mas COM tonalidade do tema (não pure white). Mistura `text-white` (literal) + `text-foreground` (token) cria inconsistência sutil em qualquer mudança futura de tema.
- **Impacto:** Drift visual e drift de manutenibilidade. Se algum dia adicionar tema light, `text-white` fica ilegível em fundo claro enquanto `text-foreground` adapta.
- **Fix sugerido:** Adicionar a CADA phase (D, E, F, G): "Substituir `text-white` por `text-foreground` em qualquer page editada. Substituir `bg-surface-0..4` (locais do Tailwind v4 @theme block) por equivalente `@loopzerokit/ui` token quando aplicável (bg-card, bg-background). T6.2 do v1.3 pruning já planejado mas tem que rodar APÓS este plano." OU adicionar à T-H.2 (Rule 3 audit): grep adicional por `text-white\|bg-surface-` deve retornar zero.

### EC-7: T-D.1 search input não está wired com filter state

- **Task afetada:** T-D.1
- **Família:** State
- **Cenário:** T-D.1 Deep Dive mostra `<ActionBar searchPlaceholder="Search Projects..." primaryAction={...} />` mas **sem prop `onSearchChange`**. Plus o test `test_overview_search_input_filters_projects` exige que typing filtre — mas implementation example não tem state para isso. Mismatch entre TDD e exemplo de código.
- **Impacto:** Teste falhará no GREEN porque implementation não tem o filter wiring. Ou desenvolvedor "ajeita rapidinho" e adiciona sem rigor.
- **Fix sugerido:** Atualizar T-D.1 Deep Dive code example:
   ```tsx
   const [searchQuery, setSearchQuery] = useState('');
   const filteredProjects = projects.filter(p => p.name.toLowerCase().includes(searchQuery.toLowerCase()));
   // ActionBar: searchValue={searchQuery} onSearchChange={setSearchQuery}
   // Then render filteredProjects in the right col
   ```

---

## SHOULD TEST (6)

### EC-8: `LoginSplit` + `SocialAuthRow` real API verification

- **Task afetada:** T-G.1, T-G.2
- **Teste sugerido:** `test_login_split_renders_with_provider_array` — antes de implementar, ler `node_modules/@loopzerokit/ui/dist/index.d.ts` para confirmar (a) LoginSplit prop shape (provavelmente `brand`, `form` slots), (b) SocialAuthRow `providers` shape (array de strings vs objects). Adaptar exemplo no plano antes de codar para evitar retrabalho.

### EC-9: ProjectCard adapter (carry-over EC-5 do edge-case-review v1.3)

- **Task afetada:** T-E.1, T-D.1
- **Teste sugerido:** `test_project_card_handles_missing_optional_fields` — mock Project com `apps=undefined`, `last_deploy=undefined`. Adapter (EC-5 v1.3 já specificou) deve defaultar. Confirmar que adapter está aplicado em AMBOS T-D.1 (Overview) e T-E.1 (Projects) — não duplicar lógica de defaulting.

### EC-10: Auth flow integration — `lib/auth.test.ts` (131 LOC) ainda passa

- **Task afetada:** T-G.1 (Login), Phase H
- **Teste sugerido:** `cd cloud/dashboard && npm test -- lib/auth` deve retornar exit 0 ao final de Phase G. Plano não menciona preservar auth.test.ts — adicionar à T-H validation explicitly. Isso protege `isEmailVerified` logic + Kratos session handling que ÉINTOCADO per D3 v1.3.

### EC-11: `UsageMeter` props match upstream Brief #1

- **Task afetada:** T-D.1
- **Teste sugerido:** `test_usage_meter_accepts_specced_props` — ler `node_modules/@loopzerokit/ui/dist/index.d.ts` para `UsageMeterProps`. Brief #1 specced `{ title, action, metrics: [{ label, value, max, unit, formatter? }] }`. Implementação upstream pode ter divergido. Adapt em T-D.1 antes de codar.

### EC-12: Table cols layout em pages diferentes

- **Task afetada:** T-E.2, T-E.3, T-F.1
- **Teste sugerido:** `test_table_renders_with_aligned_numeric_cells` — Table de Brief #2 ship `align` + `numeric` props per cell. Em Environments (T-E.2), Billing (T-E.3), Team (T-F.1) — cada implementação deve testar que numeric cells (amounts, dates, sizes) use `numeric=true` + alignment right. Sem isso, valores ficam left-aligned e visualmente sujos.

### EC-13: OIDC vanilla DOM `form.submit()` preservation through redesign

- **Task afetada:** T-G.1
- **Teste sugerido:** `test_login_oidc_uses_vanilla_dom_form_submit_not_fetch` — assert que click no botão "Continue with GitHub" cria `<form>` via `document.createElement`, popula 3 hidden inputs, e chama `form.submit()`. NÃO `fetch(action, {method:'POST'})`. EC-4 v1.3 já specificou — adicionar test rigoroso para garantir refactor mockup-aligned não quebra OAuth flow.

---

## DOCUMENT (3)

### EC-14: AccountMenu plan tier hard-coded a "hobby"

- **Risco aceito:** T-C.1 mostra `<AccountMenu plan="hobby">` com TODO note. Hard-codar é temporário, não-grave (usuário sempre vê "Hobby" badge independente do plano real). Fix vem em T-E.3 (Billing page) que wira `/v1/billing` data. Antes disso, badge é cosmético. Aceitar como WIP, documentar como visible-warning em commit message T-C.1.

### EC-15: Lucide React dual-instance bundle bloat risk

- **Risco aceito:** D4 v1.3 manteve lucide-react como direct dep. @loopzerokit/ui também bundla lucide-react (per llms.txt "Already bundled"). Pode resultar em 2 versões coexistindo se versions divergirem. Bundle inflation max ~50KB (lucide é tree-shakeable mas core helper duplicado). Aceitar como debt; Phase 6 v1.3 (T6.1) pode revisar via `npm dedupe`. Não é blocker.

### EC-16: Bundle exceeds D7 threshold (+30% gzip)

- **Risco aceito:** D7 já especifica fallback (D8 Phase H decisão `<LoopZeroUIProvider>` vs `<ThemeProvider themes={[violetForge]}>`). Se medição em T-H.3 mostra +30%+, sponsor decide single-theme provider, descarta 9/10 themes. Trivial mitigação 5-line edit. Não precisa de pre-emptive action.

---

## Padrões Sistemicos Detectados

| Padrão | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | Sim | EC-2 (pageTitle prop sem mecanismo de set) — primitiva existe, wiring entre layers ausente |
| Correct code in wrong place | Não | — |
| Project name vs project ID | Não (UI consome, não gera) | — |
| ArgoCD notifiers not services | N/A (sem ArgoCD) | — |
| Single ArgoCD App per tenant | N/A | — |
| CF scan imports conflicting apex | N/A | — |
| External lib API drift | Sim | EC-1, EC-8, EC-11, EC-12 — múltiplas APIs de @loopzerokit/ui assumidas sem leitura do .d.ts |
| Backend endpoint shape assumption | Sim | EC-4 (/v1/billing), EC-5 (invoice history) |
| Token/style migration incompleta | Sim | EC-6 (text-white legado coexiste com text-foreground novo) |

---

## Resumo

| Task | Edges encontrados | MUST FIX | SHOULD TEST | DOCUMENT |
|------|-------------------|----------|-------------|----------|
| T-A.1 | 0 | 0 | 0 | 0 |
| T-B.1 | 0 | 0 | 0 | 0 |
| T-B.2 | 1 | 1 (EC-2) | 0 | 0 |
| T-B.3 | 0 | 0 | 0 | 0 |
| T-C.1 | 2 | 1 (EC-1) | 0 | 1 (EC-14) |
| T-C.2 | 1 | 1 (EC-3) | 0 | 0 |
| T-C.3 | 0 | 0 | 0 | 0 |
| T-D.1 | 4 | 2 (EC-4, EC-7) | 2 (EC-9, EC-11) | 0 |
| T-E.1 | 1 | 0 | 1 (EC-9) | 0 |
| T-E.2 | 1 | 0 | 1 (EC-12) | 0 |
| T-E.3 | 2 | 1 (EC-5) | 1 (EC-12) | 0 |
| T-E.4 | 0 | 0 | 0 | 0 |
| T-E.5 | 0 | 0 | 0 | 0 |
| T-F.1 | 1 | 0 | 1 (EC-12) | 0 |
| T-F.2 | 0 | 0 | 0 | 0 |
| T-F.3 | 0 | 0 | 0 | 0 |
| T-G.1 | 3 | 0 | 2 (EC-8, EC-13) | 0 |
| T-G.2 | 1 | 0 | 1 (EC-8) | 0 |
| T-G.3-T-G.5 | 0 | 0 | 0 | 0 |
| T-H.1 | 0 | 0 | 0 | 0 |
| T-H.2 | 1 | 1 (EC-6) | 0 | 0 |
| T-H.3 | 0 | 0 | 0 | 1 (EC-16) |
| T-H.4 | 0 | 0 | 0 | 0 |
| Cross-cutting | 2 | 0 | 1 (EC-10) | 1 (EC-15) |

**Total**: 16 edge cases (7 MUST FIX, 6 SHOULD TEST, 3 DOCUMENT)

---

## Veredicto

**PLANO PRECISA DE AJUSTE** — 7 MUST FIX itens precisam ser incorporados antes da implementação começar.

### Ajustes obrigatórios no plano (v1.0 → v1.1)

1. **T-C.1 reescrever**: Sidebar.Item polymorphism — adicionar fallback wrap manual com Link quando `as={Component}` não funciona (EC-1)
2. **T-B.2 / T-C.3 expandir**: pageTitle prop mechanism — decidir Context provider vs location.pathname mapping (EC-2)
3. **T-C.2 expandir**: criar `src/hooks/use-projects.ts` para evitar dupla chamada `/api/v1/projects` (EC-3)
4. **T-D.1 + T-E.3 adicionar pre-step**: verificar response shape de `/v1/billing` antes de assumir (EC-4)
5. **T-E.3 adicionar pre-step**: verificar se endpoint `/v1/billing/invoices` existe; se não, omitir invoice Table de v1 (EC-5)
6. **Phase D/E/F/G nota global + T-H.2 grep estendido**: migração `text-white` → `text-foreground`, `bg-surface-*` → tokens semantic (EC-6)
7. **T-D.1 code example fix**: adicionar `searchQuery` state + filter (EC-7)

### Recomendações não-bloqueantes (SHOULD TEST)

- EC-8 (LoginSplit/SocialAuthRow API verify), EC-9 (ProjectCard adapter), EC-10 (auth.test.ts preserved), EC-11 (UsageMeter props), EC-12 (Table numeric alignment), EC-13 (OIDC vanilla DOM rigor) — adicionar aos TDD sections respectivos

### Risks aceitos (DOCUMENT)

- EC-14, EC-15, EC-16 — adicionar como notas operacionais ao plano

### Próximo passo

Plano principal (`docs/plans/2026-05-24-dashboard-visual-redesign-mockup-aligned-plan.md`) deve ser **bumped para v1.1** com os 7 MUST FIX incorporados antes de invocar `/implement` ou iniciar Phase A. Esta revisão citada no commit message do bump (`Co-discovered-by: /edge-case-plan EC-1 through EC-7`).

A boa notícia: dos 7 MUST FIX, 4 são "verify before assume" (EC-4, EC-5, EC-8, EC-11) — baratos de resolver. 2 são wiring decisions (EC-2, EC-3) — clarificação no plano resolve. 1 é refactor pattern (EC-6) — adicionar à validação. Nenhum é redesign fundamental.
