# Edge Case Review — 2026-05-25-dashboard-visual-polish

**Data:** 2026-05-25
**Plano:** `docs/plans/2026-05-25-dashboard-visual-polish-plan.md` v1.0
**Tasks analisadas:** 11 (T-A.1, T-A.2, T-B.1, T-B.2, T-B.3, T-C.1, T-C.2, T-D.1, T-D.2, T-D.3, T-E.1)
**Edge cases NOVOS encontrados:** 14 (MUST FIX: 3, SHOULD TEST: 8, DOCUMENT: 3)
**Já triados inline no plano:** 7 (não duplicados aqui)

---

## MUST FIX (3) — bloquear implementação até integrar ao AC

### EC-1: Mock `submitFlow()` mutando `window.location.href` quebraria o dev loop

- **Task afetada:** T-B.2
- **Família:** State
- **Cenário:** Em `lib/kratos.ts#submitFlow()`, a versão real chama `window.location.href = data.redirect_browser_to` em resposta de successful submission. Se o mock guard NÃO interceptar isso e a página chamar submit no dev loop com `VITE_USE_MOCKS=true VITE_MOCK_LOGGED_OUT=true`, navegação real acontece pra URL fake (`#mock-submit`), quebrando a sessão de validação visual.
- **Impacto:** Dev loop redireciona inesperadamente; operador perde estado/screenshots; "por que minha página piscou e foi pra outra rota?" — confuso e desabilitante.
- **Fix sugerido:** No mock guard em `submitFlow()`, retornar `{state: 'mocked', ui: {...}}` sem mutar window. Adicionar AC explícita em T-B.2: "Mock `submitFlow` NUNCA toca `window.location`, retorna apenas response object."

### EC-2: `formatDate('')` quebra porque empty string ≠ null/undefined

- **Task afetada:** T-C.1
- **Família:** Input
- **Cenário:** Plan diz "null/undefined returns em-dash" mas empty string `''` passa o `if (!iso)` guard? Em JS `!'' === true`, então sim — early return. **MAS** se algum dia o guard mudar para `if (iso === null || iso === undefined)`, empty string passaria e `new Date('')` retorna `Invalid Date`, e `Invalid Date.toLocaleDateString()` retorna `'Invalid Date'` — render quebra.
- **Impacto:** Defensive coding gap. Hoje o `!iso` cobre, mas é fragile. Test concretiza.
- **Fix sugerido:** Adicionar `test_format_date_returns_em_dash_for_empty_string` no T-C.1 TDD. Mantém o `if (!iso)` guard intencional, documenta porque é seguro.

### EC-3: T-C.2 muda formato user-visible "DD/MM/YYYY" → "Mon DD, YYYY" sem atualizar test assertions

- **Task afetada:** T-C.2
- **Família:** Format
- **Cenário:** Pages atuais usam `new Date(iso).toLocaleDateString()` que retorna formato locale-do-browser (em PT-BR = "10/04/2026"). T-C.2 migra para `formatDate(iso, 'date')` que retorna en-US "Apr 10, 2026" (per ADR-D1). Testes existentes que assertem texto exato quebram:
  - `domains.test.tsx`: assertion sobre "Verified 10/04/2026" ou similar
  - `team.test.tsx`: assertion sobre "2026-01-15"
  - `billing.test.tsx`: assertion sobre date range
- **Impacto:** Test suite vai de 94/94 → ~88/94 após T-C.2 commit; CI vermelho.
- **Fix sugerido:** Adicionar à AC do T-C.2: "Atualizar todas as assertions de date string nos 4 test files no MESMO commit; vitest 94/94 green requirement persiste." Concreta com `grep -rn "10/04/2026\|2026-01-15\|toLocaleDateString" src/pages/*.test.tsx`.

---

## SHOULD TEST (8) — adicionar 1 linha de teste ao TDD existente

### EC-4: Empty projects list → Select renderiza apenas "All Projects" sentinel

- **Task afetada:** T-A.1
- **Teste sugerido:** `test_top_bar_select_handles_empty_projects` — mock `useProjects()` returning `{projects: [], loading: false}`, assert Select trigger present + popover only shows "All Projects" item (no `acme-*` items).

### EC-5: `BillingResponse.limit: null` (Enterprise tier unlimited plan)

- **Task afetada:** T-A.2
- **Teste sugerido:** `test_overview_usage_renders_unlimited_when_limit_null` — fixture com `usage['Edge Requests']: {used: 5_000_000, limit: null}`, assert UsageMeter renders "5M / ∞" ou "5M / Unlimited" (definir convenção em ADR-D1.bis).

### EC-6: `used > limit` (quota exceeded scenario)

- **Task afetada:** T-A.2
- **Teste sugerido:** `test_overview_usage_renders_over_quota` — fixture `{used: 1200, limit: 1000}`, assert UsageMeter mostra "1.2K / 1K" + variant warning/destructive (bar at 100%, valor real exibido). Real scenario quando usuário passou do limite mas ainda não foi suspenso.

### EC-7: Recovery flow 2-phase transition + verification `state: 'passed_challenge'`

- **Task afetada:** T-B.2
- **Teste sugerido:** Adicionar a `recovery.test.tsx` (já existe): `test_recovery_mock_supports_phase_transition_email_to_code` — confirma fixture tem ambas as phases + `state: 'passed_challenge'` para o success path renderizar.

### EC-8: `formatNumber(Infinity)` retorna "∞" mas pode confundir

- **Task afetada:** T-C.1
- **Teste sugerido:** `test_format_number_handles_infinity` — assert `formatNumber(Infinity, 'compact')` returns em-dash (mesma família de NaN, guard único `if (!Number.isFinite(value)) return '—'`).

### EC-9: Multi-word status snake_case (`'pending_verification'`) cap-only-first

- **Task afetada:** T-D.2
- **Teste sugerido:** `test_status_label_title_cases_multi_word` — `statusLabel('pending_verification')` should return `'Pending Verification'` (split underscore → cap each → join). Fix em 2 linhas:
  ```ts
  return s.split('_').map(w => w[0].toUpperCase() + w.slice(1)).join(' ');
  ```

### EC-10: Multi-`@` email (`john@doe@malformed.com`) — split returns 3 parts

- **Task afetada:** T-D.1
- **Teste sugerido:** `test_mask_email_handles_multiple_at_signs` — `maskEmail('a@b@c.com')` should return `'a@b@c.com'` (passthrough OR canonical fallback). Fix: `if (email.split('@').length !== 2) return email;` no topo. Defensive.

### EC-11: TopBar Select com project name muito longo (>40 chars)

- **Task afetada:** T-A.1
- **Teste sugerido:** `test_top_bar_select_handles_long_project_name` — fixture com project `name: 'this-is-a-very-long-project-name-near-k8s-limit-of-63-chars'`, assert Select.Item renders + (visual overflow OK contanto que truncate via CSS `text-overflow: ellipsis`). Pode ser AC visual em vez de test puro.

---

## DOCUMENT (3) — risco aceito, nota no plano

### EC-12: Long project name visual overflow (≥40 chars)

- **Risco aceito:** Project names ≤63 chars (K8s limit) podem ser longos. @loopzero/ui Select usa CSS truncation; visual fica `acme-very-long-project-...` em vez de quebrar layout. Não é bug, é design choice já handled pelo primitive. Adicionar nota em T-A.1 referenciando.

### EC-13: `formatNumber(value, 'bytes')` — 1024 vs 1000 convention

- **Risco aceito:** "Fast Data Transfer" é medido em GB (provavelmente 1000-base SI, não 1024-base IEC). API contract não está documentado. T-C.1 deve usar 1000-base (`new Intl.NumberFormat(..., { notation: 'compact' })` faz isso natively). Adicionar ADR-D4 ao plano: "bytes intent uses SI 1000-base (KB/MB/GB), not IEC 1024-base (KiB/MiB/GiB). Matches storage industry convention (DigitalOcean, AWS, GCP all use SI in billing surfaces)."

### EC-14: Port 5173 conflict durante T-B.3 screenshot tour

- **Risco aceito:** Se outro Vite dev server estiver rodando, Vite usa 5174 e o script de screenshots aponta 5173 (falha silenciosa). Adicionar nota em T-B.3 workflow: "Antes do tour, rodar `pgrep -f vite | xargs -r kill` para garantir 5173 livre. Ou hardcodar `--port 5173 --strictPort` no comando."

---

## Padrões Sistêmicos do LoopZero Detectados

| Padrão | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | **Sim (auto-resolvido)** | T-A.2 É o fix de "Overview UsageMeter not wired to /v1/billing" |
| Correct code in wrong place | Não | Frontend polish, sem relocação |
| Project name vs ID | Não | Display only, sem DB queries |
| ArgoCD notifiers vs services | Não | N/A (frontend) |
| Single ArgoCD App per tenant | Não | N/A (frontend) |
| CF scan imports conflicting apex | Não | N/A (frontend) |
| Rule 3 termos internos | Não | Plano não emite CLI output novo |

Único padrão tocado: T-A.2 explicitamente conserta uma instância de "Implemented but not wired" (UsageMeter existe + chamada de API existe — mas não estavam conectadas).

---

## Resumo por Task

| Task | Edges novos | MUST FIX | SHOULD TEST | DOCUMENT |
|------|-------------|----------|-------------|----------|
| T-A.1 TopBar Select | 2 | 0 | 1 (EC-4) | 1 (EC-12 long name overflow) |
| T-A.2 Overview /v1/billing | 2 | 0 | 2 (EC-5, EC-6) | 0 |
| T-B.1 LOGGED_OUT flag | 0 | 0 | 0 | 0 |
| T-B.2 Kratos fixtures | 2 | 1 (EC-1) | 1 (EC-7) | 0 |
| T-B.3 Re-tour screenshots | 1 | 0 | 0 | 1 (EC-14 port) |
| T-C.1 lib/format.ts | 4 | 1 (EC-2) | 2 (EC-8 Infinity, EC-11 long name) | 1 (EC-13 bytes) |
| T-C.2 Migrate formatters | 1 | 1 (EC-3) | 0 | 0 |
| T-D.1 Email mask | 1 | 0 | 1 (EC-10) | 0 |
| T-D.2 Status capitalize | 1 | 0 | 1 (EC-9) | 0 |
| T-D.3 Avatar shape | 0 | 0 | 0 | 0 |
| T-E.1 CHANGELOG | 0 | 0 | 0 | 0 |
| **TOTAL** | **14** | **3** | **8** | **3** |

---

## Veredicto

**PLANO PRECISA DE AJUSTE MENOR (não bloqueante para sponsor approval, mas obrigatório antes da execução)**

Critério: 3 MUST FIX precisam ser dobrados nos Acceptance Criteria das tasks correspondentes ANTES da implementação começar.

### Ajustes a fazer no plano (v1.0 → v1.1)

| MUST FIX | Onde adicionar |
|----------|----------------|
| **EC-1** Mock submitFlow não muta window.location | T-B.2 AC: nova linha "Mock `submitFlow` NUNCA toca `window.location`, retorna apenas response object" |
| **EC-2** formatDate empty string guard test | T-C.1 TDD: adicionar `RED: test_format_date_returns_em_dash_for_empty_string` |
| **EC-3** T-C.2 atualizar test assertions no mesmo commit | T-C.2 AC: nova linha "vitest 94/94 green requirement persiste; atualizar assertions de date string em 4 test files no MESMO commit" |

Os 8 SHOULD TEST viram linhas adicionais no `TDD` block das tasks correspondentes (RED: test_*).

Os 3 DOCUMENT viram parágrafos pequenos no "Edge Case" ou "Deep Dive" sections.

### Ajustes opcionais (defensive engineering)

- **ADR-D4 bytes SI vs IEC** (EC-13) — meio-paragrafo no Decisions section do T-C.1
- Comentário em `dev-mocks.ts#submitFlow` mock dizendo "DEV-ONLY: never call window.location.href, would break dev loop tour" — preserva intent ao código futuro

### Probabilidade de cada MUST FIX se manifestar se não corrigido

- **EC-1**: 100% — qualquer click em "Sign in" / "Verify" no dev tour com mock dispara
- **EC-2**: 5% hoje (defensive); 50% se alguém refatorar o guard depois
- **EC-3**: 100% — toda PR de T-C.2 vai quebrar CI sem atualizar testes

### Estimativa de impacto dos ajustes

- 3 MUST FIX → ~10 LOC adicionais distribuídos
- 8 SHOULD TEST → ~16 LOC de RED test entries
- 3 DOCUMENT → ~10 LOC de prose
- **Total: +36 LOC no plano** (~5% growth, mantém within 2h10min effort estimate)

---

## Próximo passo

Reincorporar os 14 edges ao plano via amendment v1.1 (in-place edit) OU continuar para implementação aceitando que os 3 MUST FIX serão tratados durante execução (risk-tolerated).

Recomendação: amendment v1.1 antes de executar — custo é trivial e elimina retrabalho.
