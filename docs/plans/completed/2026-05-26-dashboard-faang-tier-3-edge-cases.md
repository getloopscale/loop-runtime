# Edge Case Review — dashboard-faang-tier-3

**Data:** 2026-05-25
**Plano:** `docs/plans/2026-05-26-dashboard-faang-tier-3-plan.md` (v1.0 PROPOSED)
**Tasks analisadas:** 26 (Phase 1: 10, Phase 2: 7, Phase 3: 9)
**Edge cases encontrados:** 23 (MUST FIX: 9, SHOULD TEST: 7, DOCUMENT: 7)

**Filosofia:** front-end (dashboard React/TS). Checklist multi-cell/concurrency/gitops da skill mostly N/A — foco em INPUTS (forms, URLs, query params), ESTADO (optimistic UI, SSE state), I/O (API failures, SSE disconnect), e principalmente **Rule 3 (termos internos na UI)** que vira risco crítico em features Phase 3 (Copy as JSON, CLI signature CodeBlocks).

---

## MUST FIX

### EC-1: Payment Method last4 não tem backend prep documentado

- **Task afetada:** T-DR-1.3
- **Família:** Integration
- **Cenário:** Plan diz "Add Payment Method preview (last4) se data disponível, OR placeholder com CTA". Não confirma se backend `/v1/billing` retorna `payment_method.last4` — pode ser feature backend não shipped.
- **Impacto:** T-DR-1.3 ship com placeholder permanente OR depender de PR backend não roadmap'd.
- **Fix sugerido:** Adicionar pre-condition no plano: "Confirm `/v1/billing` response shape inclui `payment_method` antes de implementar; se não, escopo apenas para enrichment de quota lines + next billing date".

### EC-2: API Keys + Workspace Settings backend endpoints não confirmados

- **Task afetada:** T-DR-1.4
- **Família:** Integration
- **Cenário:** Plan adiciona 2 novos cards (Workspace Settings com Timezone/Default-env/Notifications + API Keys com rotation/list/create). Nenhum endpoint backend mencionado (`/v1/workspace`, `/v1/api-keys` não confirmados existir).
- **Impacto:** Cards ficam "Coming soon" inúteis, OU bloqueiam Phase 1 até backend ship.
- **Fix sugerido:** Adicionar pre-condition: "Confirmar endpoints existem ou negociar backend prep separada. Se backend não pronto, T-DR-1.4 escopa apenas placeholder cards declarando coming-soon (documentado no plano que isso não fecha P2-2 totalmente)".

### EC-3: Motion system não respeita `prefers-reduced-motion`

- **Task afetada:** T-DR-2.1
- **Família:** Format (a11y)
- **Cenário:** DSS v1.1 Motion tokens spec 180ms/320ms transitions sem media query. Usuários com a11y setting `prefers-reduced-motion: reduce` ficam sem honra do setting do OS.
- **Impacto:** WCAG 2.3.3 fail (Motion from Interactions); audit Phase 3 falha Lighthouse a11y ≥95 target.
- **Fix sugerido:** Adicionar em `motion.css`:
  ```css
  @media (prefers-reduced-motion: reduce) {
    :root { --motion-fast: 0ms; --motion-base: 0ms; --motion-slow: 0ms; }
  }
  ```

### EC-4: Optimistic UI não tem rollback path documentado

- **Task afetada:** T-DR-2.7
- **Família:** State
- **Cenário:** Plan diz "optimistic add row before backend confirms" em Team invite. Não menciona rollback quando backend rejeita (email duplicado, plan limit reached HTTP 402).
- **Impacto:** UI mostra row que não existe no backend → user assume invite enviado, na próxima refresh some sem aviso → data loss confusion.
- **Fix sugerido:** Pattern obrigatório no TDD: `onMutate → add optimistic; onError → revert + show Alert destructive`. Test `test_team_invite_rolls_back_on_409_email_exists` MUST exist.

### EC-5: Command Palette monta em auth pages (logged-out)

- **Task afetada:** T-DR-3.1
- **Família:** Permission
- **Cenário:** Plan diz "wire cmd+k global shortcut". Auth pages (login/register/recovery/verification) também responderiam cmd+k → catalog vazio OR exposição de actions sem auth context.
- **Impacto:** UX broken em auth (palette vazia), OU se carregar catalog → security smell (revela commands para non-auth).
- **Fix sugerido:** "Mount CommandPalette dentro de `<DashboardLayout>` (post-auth shell), NUNCA em auth routes. Test `test_command_palette_not_mounted_on_login_route`".

### EC-6: Breadcrumb sem lookup para dynamic route labels

- **Task afetada:** T-DR-3.2
- **Família:** Format
- **Cenário:** Plan menciona "rotas deep futuras (`/projects/{id}/deployments/{id}`)". `{id}` é UUID ou name não-display-friendly. Breadcrumb mostraria "Projects / 7b1e1e3d-3afa..." em vez de "Projects / acme-web / Deployments / build-42".
- **Impacto:** Breadcrumb ilegível, UX worse than no breadcrumb.
- **Fix sugerido:** Route definition inclui `breadcrumbLabel: (params) => Promise<string>` accessor; Breadcrumb composite carrega labels via hook. Plan adicionar sub-task T-DR-3.2.1 "Breadcrumb label resolver per route".

### EC-7: "Copy as JSON" expõe internal IDs (Rule 3 violation)

- **Task afetada:** T-DR-3.5
- **Família:** Format (Rule 3)
- **Cenário:** Plan adiciona "Copy as JSON" no TopBar kebab — copia page state como JSON. Page state pode incluir `tenant_id`, `cell_id`, `app.spec.cellID`, raw CRD shapes — **Rule 3 prohibits internal terms in user-facing output**.
- **Impacto:** Violação direta da Rule 3 (Forbidden in `--json`). Vaza arquitetura interna.
- **Fix sugerido:** Sanitize layer obrigatório:
  ```ts
  const FORBIDDEN_KEYS = ['tenant_id', 'cell_id', 'cellID', ...];
  function sanitizeForCopy(obj) { /* deep-strip forbidden keys */ }
  ```
  Test `test_copy_as_json_strips_internal_ids` MUST exist.

### EC-8: "Open in CLI" pode incluir credenciais

- **Task afetada:** T-DR-3.5
- **Família:** Permission
- **Cenário:** Plan adiciona "Open in CLI" copying command equivalente. Page state pode ter tokens, API keys, session IDs — copying acidental para clipboard.
- **Impacto:** Credenciais em clipboard, possível leak via apps que monitoram clipboard.
- **Fix sugerido:** Whitelist explícita de campos copiáveis (project name, environment name, action verb). NUNCA copy raw state. Sub-task T-DR-3.5.1.

### EC-9: CodeBlock CLI signature pode vazar termos internos

- **Task afetada:** T-DR-3.8
- **Família:** Format (Rule 3)
- **Cenário:** Plan adiciona CodeBlock "Or use the CLI" em 6 pages. Risco: dev escreve `loopzero argocd sync` ou `loopzero gitops push` (termos forbidden Rule 3) acreditando ser ok porque está em CodeBlock.
- **Impacto:** Rule 3 violation. Audit hook `.claude/hooks/internal-terms.py` deve pegar mas plan não cita.
- **Fix sugerido:** Adicionar ao DoD do T-DR-3.8: "Run `.claude/hooks/internal-terms.py` em todos CodeBlock contents. Commands must use product language only: `loopzero deploy`, `loopzero project list`, NOT `argocd sync`, `kubectl apply`, `gitops push`".

---

## SHOULD TEST

### EC-10: ActionBar height tuning regresso visual em outros consumers

- **Task afetada:** T-DR-1.6
- **Teste sugerido:** `test_action_bar_height_consistent_across_4_consumers` — Overview/Projects/Domains/Team rendering ActionBar todos ≤64px sem clipping.

### EC-11: Button asChild + Link from react-router-dom compatibility

- **Task afetada:** T-DR-1.7
- **Teste sugerido:** `test_projects_open_action_navigates_via_link` — assert `navigate()` chamado UMA vez (not double-fired by Button onClick + Link click).

### EC-12: OAuth providers list pode não estar exposta em session.identity

- **Task afetada:** T-DR-1.5
- **Teste sugerido:** `test_profile_connected_apps_handles_missing_credentials` — quando `session.identity.credentials` é undefined OR vazio, card mostra "No connected apps" sem crash.

### EC-13: Environment list pagination strategy

- **Task afetada:** T-DR-1.8
- **Teste sugerido:** `test_environments_pagination_kicks_in_above_10_rows` — wire DataTable.pagination quando filtered.length > 10 (CD-01 gate). Plano só cobre R-03 hideBelow, esquece CD-01.

### EC-14: Skeleton flash em fetches <100ms

- **Task afetada:** T-DR-2.2
- **Teste sugerido:** `test_skeleton_hidden_when_fetch_under_200ms` — `useDelayedLoading(200)` pattern: só mostra skeleton se fetch demora >200ms (evita flicker).

### EC-15: Breadcrumb degrada para rota não registrada

- **Task afetada:** T-DR-3.2
- **Teste sugerido:** `test_breadcrumb_fallback_for_unknown_route` — rota não cadastrada renderiza "Untitled" ou esconde breadcrumb (não crash).

### EC-16: Multi-tab SSE deduplication

- **Task afetada:** T-DR-3.9
- **Teste sugerido:** `test_sse_uses_broadcast_channel_for_multi_tab` — single SSE connection compartilhada via BroadcastChannel (per existing pattern em `src/lib/api.ts` JWT-em-memória). Evita N conexões simultâneas drainando backend.

---

## DOCUMENT

### EC-17: Overview Alerts banner deve ser conditional (post-feature ship)

- **Risco aceito:** Plan T-DR-1.1 não diz "show banner SOMENTE quando alerts não configurados". Quando feature alerts shipar, banner deveria sumir. **Document:** plan deve adicionar `alerts.length === 0 ? <Banner/> : null` quando feature existir; hoje sempre mostra OK.

### EC-18: Bundle hard gate (180 KB) pode ser violado em Phase 3

- **Risco aceito:** Plan menciona "aceitar 10-15 KB growth justificado por novas primitives" mas hard gate é 180 KB e atual é 145.63 KB → headroom 34.37 KB. Brief #7 ship 5+ primitives (CommandPalette grande). Pode estourar. **Document:** Phase 3 DoD deve incluir bundle measurement OBRIGATÓRIO + escalation path "se excede 180 KB, escopo Phase 3 cortado para fit".

### EC-19: Empty state vs Error state distinção

- **Risco aceito:** T-DR-2.4 fala "actionable CTA OR clear waiting state" mas não distingue de error state (API 500, network fail). Ambos podem renderizar EmptyState. **Document:** plan reservar `<Alert variant="destructive">` para errors, `<EmptyState>` somente para empty data (não-erro). DSS v1.1 documentar diferença.

### EC-20: Usage panel double-scroll UX risk

- **Risco aceito:** T-DR-2.5 adiciona scroll interno em UsageMeter (`max-h-[400px] overflow-y-auto`). Página tem scroll global → double scroll confuso em desktop. **Document:** keep max-h conservative (400px max), prefer scroll global quando possível. Acceptable trade-off para acomodar 5+ KPIs.

### EC-21: Tab close durante optimistic mutation

- **Risco aceito:** Usuário fecha tab durante invite in-flight → optimistic state perdido, backend pode ter completado ou não. **Document:** acceptable para invite flow (idempotent backend); document para futuros casos destructive (delete project) onde optimistic UI requires confirmation modal antes.

### EC-22: Filter/layout state persistence

- **Risco aceito:** T-DR-3.6 não especifica se filter (ActionBar) e layout (grid/list toggle) persiste. **Document:** per-page localStorage (`dashboard:projects:filter`, `dashboard:projects:layout`) — Vercel pattern. Plano deve adicionar nota.

### EC-23: Brief #7 timing risk

- **Risco aceito:** Phase 3 depends on @loopzerokit/ui Brief #7 turnaround. Brief #4 took 24h, Brief #5 took 72h. Brief #7 escopo maior (5+ primitives). Realistic: 1-2 weeks upstream. **Document:** Phase 3 timing estimate deve ter contingency: "se Brief #7 turnaround > 1 week, defer T-DR-3.1 (CommandPalette) + T-DR-3.4 (NotificationBell) para v1.1 e ship Phase 3 com remaining 7 tasks".

---

## Padrões Sistêmicos Detectados

| Padrão | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | Sim | Phase 3 features dependem de Brief #7 — primitives podem ship sem call site se upstream atrasa |
| Project name vs ID | Não | Dashboard usa name consistentemente já |
| Rule 3 (termos internos) | Sim | T-DR-3.5 (Copy as JSON), T-DR-3.8 (CodeBlock signature) — risco crítico se não tratados |
| Single ArgoCD App per tenant | N/A | front-end-only plan |
| CF scan imports | N/A | front-end-only plan |
| ArgoCD notifiers not services | N/A | front-end-only plan |
| State machine discipline | N/A | sem state machine novos |
| `withEnvironmentLock` | N/A | sem mutações backend novas |
| `pg_advisory_xact_lock` | N/A | idem |
| Multi-cell ClusterCredential | N/A | idem |
| gitops.Remove FATAL | N/A | idem |
| Backend-prep gap | **Sim** | T-DR-1.3 (Payment Method), T-DR-1.4 (API Keys + Workspace Settings), T-DR-3.4 (audit_log notification source confirmation), T-DR-3.9 (SSE status endpoint existence) |

**Backend-prep gap é o padrão sistêmico mais perigoso** deste plano. 4 tasks dependem de endpoints/features backend não-confirmados existir. Risk: dashboard "tecnicamente correto" mas funcionalmente vazio (placeholder cards).

---

## Resumo por task

| Task | Edges | MUST FIX | SHOULD TEST | DOCUMENT |
|------|-------|----------|-------------|----------|
| T-DR-1.1 | 1 | 0 | 0 | 1 (EC-17) |
| T-DR-1.2 | 0 | 0 | 0 | 0 |
| T-DR-1.3 | 1 | 1 (EC-1) | 0 | 0 |
| T-DR-1.4 | 1 | 1 (EC-2) | 0 | 0 |
| T-DR-1.5 | 1 | 0 | 1 (EC-12) | 0 |
| T-DR-1.6 | 1 | 0 | 1 (EC-10) | 0 |
| T-DR-1.7 | 1 | 0 | 1 (EC-11) | 0 |
| T-DR-1.8 | 1 | 0 | 1 (EC-13) | 0 |
| T-DR-1.9 | 0 | 0 | 0 | 0 |
| T-DR-1.10 | 1 | 0 | 0 | 1 (EC-18) |
| T-DR-2.1 | 1 | 1 (EC-3) | 0 | 0 |
| T-DR-2.2 | 1 | 0 | 1 (EC-14) | 0 |
| T-DR-2.3 | 0 | 0 | 0 | 0 |
| T-DR-2.4 | 1 | 0 | 0 | 1 (EC-19) |
| T-DR-2.5 | 1 | 0 | 0 | 1 (EC-20) |
| T-DR-2.6 | 0 | 0 | 0 | 0 (covered by EC-3 reduced-motion) |
| T-DR-2.7 | 2 | 1 (EC-4) | 0 | 1 (EC-21) |
| T-DR-3.1 | 1 | 1 (EC-5) | 0 | 0 |
| T-DR-3.2 | 2 | 1 (EC-6) | 1 (EC-15) | 0 |
| T-DR-3.3 | 0 | 0 | 0 | 0 |
| T-DR-3.4 | 1 | 0 | 0 | 1 (EC-?: native browser notif YAGNI) |
| T-DR-3.5 | 2 | 2 (EC-7, EC-8) | 0 | 0 |
| T-DR-3.6 | 1 | 0 | 0 | 1 (EC-22) |
| T-DR-3.7 | 0 | 0 | 0 | 0 |
| T-DR-3.8 | 1 | 1 (EC-9) | 0 | 0 |
| T-DR-3.9 | 2 | 0 | 1 (EC-16) | 1 (EC-?: backend endpoint confirmation) |
| Phase 3 (global) | 1 | 0 | 0 | 1 (EC-23) |
| **Total** | **23** | **9** | **7** | **7** |

---

## Veredicto

**PLANO PRECISA DE AJUSTE.**

Razões:
1. **Backend-prep gap sistêmico** — 4 tasks dependem de endpoints não-confirmados (T-DR-1.3 payment, T-DR-1.4 API keys/workspace, T-DR-3.4 audit_log SSE, T-DR-3.9 status SSE). Plano deve adicionar **Phase 0 — Backend pre-flight** ou explicitamente escopar cards como "Coming Soon" placeholders quando backend não pronto.
2. **Rule 3 risk em Phase 3** — 3 MUST FIX (EC-7, EC-8, EC-9) sobre termos internos / credenciais em "Copy as JSON" + "Open in CLI" + CodeBlock signature. Hooks existem (`.claude/hooks/internal-terms.py`) mas plan deve EXPLICITAMENTE wire validation em DoD.
3. **A11y risk em Phase 2** — `prefers-reduced-motion` não respeitado quebra WCAG + Lighthouse target ≥95.
4. **Brief #7 contingency missing** — Phase 3 timing depende de upstream sem fallback documentado.

**Ajustes recomendados (em ordem de prioridade):**

1. **Adicionar Phase 0 (sub-week):** Backend pre-flight — verificar 4 endpoints. Documentar gaps. Decidir per-task: ship com backend OR ship com placeholder. → resolve EC-1, EC-2, parts de EC-9, EC-?
2. **Bumpar MUST FIX para sub-tasks explícitas** em T-DR-2.1 (motion media query), T-DR-2.7 (optimistic rollback), T-DR-3.1 (cmd+k mount scope), T-DR-3.2 (breadcrumb labels), T-DR-3.5 (sanitize copy + whitelist CLI command), T-DR-3.8 (Rule 3 audit em CodeBlock content). → resolve EC-3, 4, 5, 6, 7, 8, 9
3. **Adicionar 7 testes obrigatórios** ao TDD section (Phase-specific). → resolve EC-10..16
4. **Documentar 7 risk-accepted notes** como subsection "Known risks" no plano. → resolve EC-17..23
5. **Bump plan v1.0 → v1.1** com edge cases incorporados (per Plan Amendment Policy DoD redefinition).

Pós-ajustes plano pode ir para meeting + ratification cleanly.
