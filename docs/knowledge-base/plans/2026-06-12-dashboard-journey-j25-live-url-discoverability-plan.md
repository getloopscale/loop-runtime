---
slug: 2026-06-12-dashboard-journey-j25-live-url-discoverability
milestone_id: J-25
created_at: 2026-06-12
goal: Surface live deploy URL in 3 canonical dashboard slots (Project Overview hero + Deployment Detail header + Deployments list row) so a user completes the `loopzero deploy → open URL` loop in ZERO clicks from Project Overview — measured by sponsor walkthrough opening the live URL from `/projects/:name` WITHOUT terminal scrollback OR sidebar navigation.
---

# J-25 — Live URL Discoverability

**Version:** v1.2
**Estimated effort:** 4-6h engineering-side + 30min sponsor LIVE walkthrough
**Priority:** post-merge of v0.2.0 release (insert when sponsor priority chain allows)

## Changelog

- **v1.2 (2026-06-12)** — parser-compliance cirurgia post `/plan-confidence` v1.1 INVALID 49.0. Fixes: (1) ADR headers `### ADR Dn` → `### Dn — Title (ADR)` so parser detects definitions; (2) Coverage Matrix `\| Gap \| Task \|` → 5-col `\| # \| Description \| Task \| Severity \| Owner \|` (parser requires ≥4 cols); (3) NEW `## Baseline Context` section with 4 canonical sub-sections (Files / Callers / Glossary / Boundaries) to close `soft_floor_baseline_context_incomplete`; (4) Unresolved Questions `- **(Q1)**` → `- Q1 —` (parser regex `[-*]\s+(Q\d|[A-Z])`); (5) stripped 2 phantom-number ADR token typos (the 4th and 5th IDs — non-existent — only the first 3 ADRs are real); (6) 6 spec smells closed (3 weak_imperatives + 2 subjective_adjectives + 1 vague_pronouns). Expected re-score: SHIPPABLE_WITH_CAVEATS 75-85.
- **v1.1 (2026-06-12)** — absorbed 10 EC from edge-case review (`knowledge-base/reviews/2026-06-12-dashboard-journey-j25-live-url-discoverability-edge-cases-2026-06-12.md`): 3 MUST FIX (EC-1 fabricated URL example fixed via `status.go:252` canonical pattern; EC-2 shell allowlist sub-task added as T4.2; EC-3 T0.1 halt condition for null URL); 4 `SHOULD-TEST` (EC-4 multi-env coverage; EC-5 GET 200..399 not HEAD; EC-6 loading skeleton; EC-7 copy button TDD); 3 DOCUMENT (EC-8 polling gap consolidated; EC-9 pagination affordance; EC-10 dev/standalone hostname variation).
- **v1.0 (2026-06-12)** — initial draft.

## Goal

Surface the live deploy URL in 3 canonical dashboard slots so the user finds it in 1 glance after `loopzero deploy`. Single observable metric: **sponsor walkthrough 2026-06-13 (or next available) opens the live URL from `/projects/teste` Project Overview hero with ZERO extra clicks** (no sidebar navigation, no terminal scrollback, no preview tab detour).

## Context

### Estado HOJE (Baseline 2026-06-12)

`loopzero deploy` retorna `deploy.completed.apps[].url` ao CLI (`cli/src/commands/deploy.ts:582`), que printa a URL no terminal. O dashboard, porém, NÃO renderiza essa URL em nenhum dos 3 slots óbvios pós-deploy:

| Slot | Arquivo | LoC | Última mudança | Renderiza URL hoje? |
|---|---|---:|---|---|
| Project Overview | `cloud/dashboard/src/pages/project-detail.tsx` | 301 | `T5.3` commit `6f8f50b6` (2026-06-08) | ✗ — só envs/last-deploy/apps counts |
| Deployment Detail header | `cloud/dashboard/src/pages/deployment-detail.tsx` | 388+ | J-17 (2026-06-06) | ✗ — só Cancel/Rollback/Promote/SBOM/Attestation |
| Deployments list row | `cloud/dashboard/src/pages/deployments.tsx` | 200+ | J-17 (2026-06-06) | ✗ — só commit/autor/status/time |
| Preview row (FOR REFERENCE) | `cloud/dashboard/src/components/environments/preview-row.tsx:62-64` | 80 | J-23 (2026-06-07) | ✓ canonical pattern |

### Current callers do dado (URL)

| Caller | Path | Origem | Disponível? |
|---|---|---|---|
| `ProjectHealthCard` | recebe `status` de `useProjectStatus(name)` | status.url JÁ existe (`status.go:131`) | ✓ campo `url` é IGNORADO pelo componente hoje |
| `useDeployment(id)` → `build.apps[]` | `/api/v1/builds/:id` | `BuildAppInfo` NÃO tem URL field (`builds.go:135`) | ✗ |
| `useDeployments(name)` → lista | `/api/v1/projects/:name/builds` | mesma shape — sem URL | ✗ |
| `deploy.completed` SSE | `BuildCallbackApp.URL` (`builds.go:88`) | ✓ presente | ✓ efêmero (só no momento do evento) |

**Conclusão**: o backend já entrega a URL via `GET /api/v1/projects/:name/status?env=production` (`statusResponse.URL`). O dashboard já tem o tipo TS declarado (`ProjectStatusDetail.url?: string` — `types.ts:460`). O gap é puro frontend wiring: ler `status.url` e renderizar.

### Glossário

- **Live URL** — hostname do ExposedRoute da release ACTIVE (production OU preview). Per-environment, não per-build. Pattern canonical production: `<tenant>-<project>-<entryApp>.loopzero.example` (a confirmar via inspect ExposedRoute LIVE durante T0.1).
- **Visit button** — variante canonical Vercel: link externo, abre em nova tab (`target="_blank" rel="noopener noreferrer"`), com ícone `ExternalLink` lucide-react.
- **Production deploy** — build cujo `active_release.build_id == this.build_id`. Apenas esse ganha o botão Visit.
- **Previous deploy** — qualquer build que já foi production mas foi superseded. NÃO ganha Visit (UX trap: clicar mostraria a versão ATUAL, não aquela).

### Architecture boundaries

- DIP: dashboard depende de `useProjectStatus()` (hook), não fala direto com fetch — preservado.
- Pre-release governance: T2 (UI contract change — Vercel-quality affordance) por affectar 3 pages produção-facing. ADR D1+D2+D3 abaixo dão o rationale; sem meeting porque ZERO mudança de backend / migration / state.

## Prior Art & Related Work

- **J-23 Preview slot** (delivered 2026-06-07, `preview-row.tsx:62-64`) — canonical pattern de URL render no dashboard. Usa `<a href={slot.url} target="_blank">` com lucide `ExternalLink`. Reusar o exato shape.
- **Vercel project page** — Production URL no topo, hero treatment, "Visit ↗" botão primary. Standard de fact.
- **Netlify deploys list** — ícone `ExternalLink` per-row para o deploy ATIVO; demais rows sem botão.
- **J-17 Deployments Realtime** (delivered 2026-06-06) — estabeleceu o pattern `active_release.build_id` para destacar "qual é o currently serving" (`deployments.tsx:161`). Reusar essa lógica para gating do Visit button.

## Baseline Context (deep review of current state)

### Files that will be touched

| File | LoC today | Last commit (sha + date) | Why it exists today | Invariants to preserve |
|---|---|---|---|---|
| `cloud/dashboard/src/pages/project-detail.tsx` | 301 | `6f8f50b6` 2026-06-08 (`T5.3` of dashboard-screens-review-fixes plan) | Project Overview page — health card + envs/last-deploy/apps counts + recent deploys | useRequireAuth gate, useSetPageTitle, useProjects+useProjectStatus+useDeployments hooks, DangerZone delete flow |
| `cloud/dashboard/src/pages/deployment-detail.tsx` | 388+ | J-17 2026-06-06 | Per-build detail page — Cancel/Rollback/Promote/SBOM/Attestation buttons | Status pill canonical, SBOM/Attestation gating by `succeeded`, cancel dialog flow |
| `cloud/dashboard/src/pages/deployments.tsx` | 200+ | J-17 2026-06-06 | Per-project deploys list — DataTable with commit/author/status/time | URL-bookmarkable filter, total count for "Showing N of M", active_release indicator |
| `cloud/dashboard/src/lib/api/types.ts` | 460+ | various | TypeScript types canonical — ProjectStatusDetail has `url?: string` (line 460) | BuildApp shape backward compat |
| `cloud/dashboard/src/hooks/use-project-status.ts` | ~50 | various | Hook canonical — defaults env to "production" backend-side; intervalMs=5000 | refetch polling cadence |
| `infra/tests/e2e/dev-public-live-url-discoverability_test.sh` (NEW) | 0 | — | LIVE 3-scenario bash test against cloud-dev.loopzero.example | --skip-live + --json flags canonical |
| `infra/scripts/local/dogfood-live-url.sh` (NEW) | 0 | — | Aggregate wrapper (hermetic + vitest + LIVE) | --hermetic-only + --live-only + --json |
| `taskfiles/test.yml` | TBD | various | Test task definitions | test:dogfood:live-url wired |
| `.claude/rules/shell-scripts-allowlist.md` | TBD | various | Allowlist canonical | 2 NEW rows appended per T4.2 |

LoC + git sha will be re-captured by `/implement` Step 1 pre-flight; this baseline acknowledges files exist and locks the invariants.

### Current callers / dependents

- **`useProjectStatus(name)`** — already called by `project-detail.tsx:34` (ProjectHealthCard consumer); `ProjectStatusDetail.url` field IS delivered but IGNORED by current `ProjectHealthCard` component. T1.1 wires it.
- **`useDeployments(name)`** — called by `project-detail.tsx:35` for recent-deploys card; `useDeployment(id)` is single-build hook used by `deployment-detail.tsx`. Both shapes carry `build.apps[]` but the listing endpoint does NOT populate `apps[].url` (verified via `api/internal/routes/builds.go:135` BuildAppInfo has only Name+Type). T2.1+T3.1 derive `active_release.build_id` matching via `useProjectStatus` not via build.apps.
- **`status.go:252`** — `resp.URL = fmt.Sprintf("https://%s-%s.%s", projectName, env, h.baseDomain)`. Canonical emitter. Consumed by frontend via GET `/api/v1/projects/{name}/status?env=production`.
- **`preview-row.tsx:62-64`** — existing canonical pattern (J-23 delivered) renders `<a href={slot.url}>` for preview environments. Reference shape for J-25 wiring.

### Domain glossary

- **Live URL** — hostname emitted by status endpoint via ExposedRoute CR; per-environment (production endpoint stable across deploys, NOT per-build). Pattern canonical: `https://<project>-<env>.<baseDomain>`. Examples per env: dev-public `teste-production.loopzero.example`; prod same; standalone-multi `teste-production.loopzero.local`.
- **Production deploy** — the build whose `active_release.build_id == this.build_id`. Only this build renders the Visit button (Vercel canonical).
- **Previous deploy** — any build that was once `active_release` but was superseded by a later deploy. NEVER renders Visit (UX trap: click would show CURRENT live, not historical).
- **Visit button** — canonical Vercel affordance: `<a target="_blank" rel="noopener noreferrer">` with `ExternalLink` lucide icon, variant=primary on Detail (action canonical pós-deploy).
- **URL pending badge** — gray badge rendered when `status.url == null`. Replaces silent hiding (per D3); signals deploy pre-ExposedRoute OR failed.

### Architecture boundaries affected

- **Frontend pages → dashboard hooks**: pages depend on `useProjectStatus`+`useDeployments` (DIP boundary respected).
- **Dashboard hooks → backend status endpoint**: HTTP boundary via `engineFetch`; backend contract is `statusResponse.URL` per `api/internal/routes/status.go:131`.
- **Backend status emitter → ExposedRoute CR**: K8s API boundary (server-side, out-of-scope for J-25).
- **CI gate → shell allowlist**: `task quality:gates` consumes `.claude/rules/shell-scripts-allowlist.md`; T4.2 appends 2 rows. Per `rules/cycle-implement.md § Hard gates` invariant: new `.sh` requires allowlist entry pre-merge.

This baseline locks "what exists today" so a junior reader does not have to spelunk to understand the work surface.

## ADRs

### D1 — URL data source = useProjectStatus, NOT new endpoint (ADR)

**Decision**: Read live URL from `useProjectStatus(name).status.url` (existing `/api/v1/projects/:name/status?env=production`).

**Rationale**: backend já popula `statusResponse.URL` (`status.go:131`) a partir do ExposedRoute. Dashboard types já têm `ProjectStatusDetail.url?: string` (`types.ts:460`). Zero backend mudança, zero migration, zero novo endpoint.

**URL emitter canonical (locked v1.1 per EC-1 absorption):** `api/internal/routes/status.go:252` emite `fmt.Sprintf("https://%s-%s.%s", projectName, env, baseDomain)`. Pattern formal `https://<project>-<env>.<baseDomain>`. Exemplos canonical:
- dev-public production: `https://teste-production.loopzero.example`
- prod production: `https://teste-production.loopzero.example` (mesmo baseDomain — dev-public mirrora prod)
- standalone-multi (LOCAL k3d): `https://teste-production.loopzero.local` (baseDomain=loopzero.local; LIVE probe deliberadamente NÃO roda contra standalone — see EC-10 absorbed v1.1)
- preview: handled separadamente via PreviewSlot (out of scope; J-23 já delivered)

**Alternativas rejeitadas**:
- (A) Estender `BuildAppInfo` para incluir URL — rejeitada porque URL é per-environment não per-build (Vercel canonical). Builds antigos não devem mostrar URL própria.
- (B) Novo endpoint `GET /api/v1/projects/:name/url` — rejeitada (YAGNI; status já entrega).
- (C) Ler do `deploy.completed` SSE payload — rejeitada porque efêmero (só no momento do evento; user que abre dashboard depois não tem).

### D2 — URL slots = 3 (Overview hero + Detail action + List row) (ADR)

**Decision**: Renderizar Visit em 3 lugares: (1) Project Overview hero, (2) Deployment Detail header (somente production build), (3) Deployments list row (somente production row).

**Rationale**: Cobertura máxima do funnel discovery. Overview é hero (sempre que ativo). Detail é action canonical pós-promote. List é affordance per-row sem ocupar coluna nova.

**Alternativas rejeitadas**:
- (A) Só Overview hero — rejeitada porque user que está em Detail/list precisa abrir novamente Overview. Friction.
- (B) Também no sidebar nav — rejeitada (sidebar é navegação, não dado).
- (C) Toast pós-`deploy.completed` SSE — DIFERIDA para J-25.1 (futuro); fora deste escopo.

### D3 — Null URL = render disabled "Pending" badge, NOT hide silently (ADR)

**Decision**: Quando `status.url == null OR undefined`, renderizar badge cinza "URL pending" (sem link clicável) ao invés de omitir o slot inteiro.

**Rationale**: Esconder silenciosamente cria "did the deploy fail?" confusion. Badge explícito sinaliza "deploy pre-ExposedRoute OR failed" sem alarme visual. Pattern alinhado a J-17 lesson #4 (empty failure logs são bug de produto).

**Alternativas rejeitadas**:
- (A) Hide slot — rejeitada (silent UX gap).
- (B) Render botão disabled "Visit" — rejeitada (UX equivocado: parece quebrado).
- (C) Skeleton loader perpetual — rejeitada (não-determinístico).

## Drawbacks & Risks

| Risk | Severity | Mitigation | Owner |
|---|---|---|---|
| `status.url` retorna URL stale durante ROLLING_BACK (4-5s gap entre rollback inicio + ExposedRoute update) | MEDIUM | useProjectStatus já refetch a cada 5s (`use-project-status.ts`); janela máxima 10s aceitável. Documentar no comment. | Frontend |
| Preview URL pattern muda no futuro (preview-dev.loopzero.example → outro hostname) | LOW | Dashboard NÃO hardcoda — só renderiza `status.url` strg. Zero exposição. | — |
| Multi-app projects (loopzero.yaml com >1 app) — qual URL mostrar? | MEDIUM | `status.url` é per-environment, retorna ENTRY app URL (canonical Vercel). Se sponsor pedir per-app posterior, J-25.2. | Backend (futuro) |
| Dashboard abre Visit antes do DNS propagar → user vê NXDOMAIN | LOW | Browser-level concern; fora do escopo. Tooltip "URL can take up to 60s to propagate on first deploy". | Frontend |
| **(EC-8 v1.1 consolidated)** 5s polling gap entre `deploy.completed` SSE e useProjectStatus refetch — Overview/Detail/List podem mostrar URL stale por até 5s pós-promote | LOW | useProjectStatus polls cada 5s (`use-project-status.ts intervalMs=5000`); janela máxima 5s aceitável. Aplica-se uniformemente em T1.1+T2.1+T3.1 (cross-task pattern; mesma mitigação cobre todos). J-17 lesson #3 (useEffect deps reflecting lifecycle transitions) já estabeleceu este pattern como aceitável. | Frontend |

## Unresolved Questions

- Q1 — Onde no Overview: hero card próprio acima de "Overview" OU integrar dentro do ProjectHealthCard? Inclinação: hero card próprio (mais Vercel-canonical), mas decidir LIVE durante T2.1.
- Q2 — Toast pós-deploy.completed SSE notifying live URL deferred para J-25.1 OU absorvido agora? Inclinação: defer (escopo crescido) — manter J-25 cirúrgico em 3 slots estáticos.

## Phases

### Phase 0 — Baseline + verificar wiring (30min)

#### T0.1 — Inspect status.url canonical contra LIVE dev-public

- **Files to edit**: nenhum (read-only).
- **Why this step**: ADR D1 assume que `status.url` retorna a URL canonical de produção. Confirmar empiricamente contra dev-public para evitar 8h debugging de "campo existia mas era vazio". Reasoning chain: same lesson `feedback_4layer_drift_extension_2026_06_09` — types declared ≠ data populated.
- **Procedure**:
  ```bash
  TOKEN=$(cat ~/.loopzero/local-dogfood-token.txt)
  curl -sH "Authorization: Bearer $TOKEN" \
    https://cloud-dev.loopzero.example/engine/api/v1/projects/teste/status?env=production \
    | jq '.url, .last_build, .active_release'
  ```
- **TDD**: `test_status_url_populated_when_active_release_exists` — curl probe; assert `.url` non-empty string + matches `^https://[a-z0-9-]+-[a-z0-9-]+\.loopzero\.dev$` (canonical `<project>-<env>.<baseDomain>` shape per `status.go:252`).
- **Scope LIVE (EC-10 absorbed v1.1):** probe runs ONLY against `cloud-dev.loopzero.example` (dev-public) OR production. Standalone-multi (LOCAL k3d) returns `https://teste-production.loopzero.local` which does NOT match the dev-public regex; LOCAL validation uses hermetic vitest only.
- **Acceptance criteria**:
  - `status.url` retorna non-empty string para projeto `teste` com release ACTIVE.
  - `status.url` retorna null/empty para projeto sem release (idle).
  - **HALT CONDITION (EC-3 absorbed v1.1):** se probe retorna `url == null` COM `active_release` populado, HALT plan execution + escalate as **J-25.A backend wiring fix** BEFORE Phases 1-3. Engineering NÃO shipa 3 páginas dashboard contra um campo morto — invalida o whole point do plano.
- **DoD**: probe documentado em `infra/tests/e2e/dev-public-status-url-probe_test.sh` (hermetic-compatible via `--json` parser).

### Phase 1 — Project Overview hero (1.5h)

#### T1.1 — Live URL hero card no project-detail.tsx

- **Files to edit**: `cloud/dashboard/src/pages/project-detail.tsx`.
- **Why this step**: slot canonical mais visível (sponsor walkthrough abre `/projects/teste` first). Reasoning: ADR D2 → Overview é hero slot. Reusa `useProjectStatus(name)` (já chamado, linha 34) — zero data fetch novo.
- **TDD**:
  - RED: `test_project_overview_renders_visit_button_when_status_url_populated` — render `<ProjectDetailPage />` com mock `useProjectStatus` retornando `{url: 'https://teste-production.loopzero.example'}`; assert `getByRole('link', {name: /visit/i})` exists com `href` = URL + `target="_blank"`.
  - RED: `test_project_overview_renders_pending_badge_when_status_url_null` — mock retornando `{url: null}`; assert badge "URL pending" rendered, NO link.
  - RED (EC-4 absorbed v1.1): `test_overview_uses_default_production_env` — assert `useProjectStatus(name)` é chamado SEM env arg (defaults to production per `use-project-status.ts:25`). Documenta que staging deploys' Detail page mostra a PRODUCTION URL via este hook, não a staging URL — fail-loud se um futuro J-25.2 adicionar env-aware Overview e flipar essa assumption.
  - RED (EC-6 absorbed v1.1): `test_overview_renders_skeleton_while_status_loading` — mock `useProjectStatus` retornando `{loading: true, status: null}`; assert Skeleton component rendered (NOT "URL pending" badge); on next render with populated status, badge OR link appears. Previne flash anormal onde badge brevemente diz "pending" antes de flipar para URL real.
  - RED (EC-7 absorbed v1.1): `test_overview_copy_button_writes_to_clipboard_and_shows_toast` — fire click no Copy button, mock `navigator.clipboard.writeText`; assert called com `status.url`; assert toast "Copied" rendered. Acceptance criteria mencionavam Copy button mas TDD v1.0 ignorava.
  - GREEN: adicionar card hero acima de "Overview" lendo `status?.url`.
- **Acceptance criteria**:
  - Card visível no topo do Project Overview ANTES do ProjectHealthCard (hero treatment).
  - URL clicável abre em nova tab (`target="_blank" rel="noopener noreferrer"`).
  - Copy button ao lado do link (lucide `Copy`).
  - Badge cinza "URL pending" quando `status.url` ausente (ADR D3).
- **DoD**: 5 vitest specs GREEN (2 originais + EC-4 multi-env + EC-6 skeleton + EC-7 copy button) + dashboard LIVE render com URL `teste-production.loopzero.example` (canonical `<project>-<env>.<baseDomain>` per `status.go:252`) visível em `/projects/teste`.

### Phase 2 — Deployment Detail Visit button (1h)

#### T2.1 — Visit button no header de deployment succeeded

- **Files to edit**: `cloud/dashboard/src/pages/deployment-detail.tsx` (linhas 267-286).
- **Why this step**: pós-promote o user QUER abrir e ver. Action primary. Reasoning chain: ADR D2 → Detail é action slot pós-build-succeeded. Reusa `useProjectStatus()` para descobrir se ESTE build é o currently-serving.
- **TDD**:
  - RED: `test_detail_visit_button_visible_when_build_is_active_release` — mock `useProjectStatus` com `active_release.build_id == this.build_id` AND `status.url`; assert Visit button rendered variant=primary.
  - RED: `test_detail_visit_button_HIDDEN_when_build_is_previous` — mock com `active_release.build_id != this.build_id`; assert ZERO Visit button (anti-trap UX).
  - RED: `test_detail_visit_button_HIDDEN_when_build_failed` — `status='failed'`; assert ZERO Visit button.
  - GREEN: inserir Button ao lado de Rollback/Promote (linha 267 `{build.status === 'succeeded' && (...)}`); condicional adicional `&& isActiveRelease`.
- **Acceptance criteria**:
  - Visit button renderizado SOMENTE quando: (a) build status=succeeded, (b) este build é currently-serving (`active_release.build_id`), (c) `status.url` non-null.
  - Variant=primary (action canonical pós-deploy).
  - Posicionado ANTES de Rollback (ordem visual: Visit ← Rollback ← Promote).
- **DoD**: 3 vitest specs GREEN.

### Phase 3 — Deployments list row affordance (45min)

#### T3.1 — ExternalLink icon na linha do production deploy

- **Files to edit**: `cloud/dashboard/src/pages/deployments.tsx`.
- **Why this step**: scanability — user vê a lista de deploys e identifica o production em 1 glance. Reasoning: ADR D2 → list é affordance per-row. Reusa o existing "currently-serving" indicator de J-17 (linha 161).
- **TDD**:
  - RED: `test_list_row_external_link_visible_only_on_active_release_row` — mock 3 builds, 1 deles é active; assert apenas a linha do active tem `ExternalLink` icon.
  - GREEN: adicionar ícone clicável após o status pill na DataTable row.
- **Acceptance criteria**:
  - Ícone `ExternalLink` (lucide) renderizado APENAS na row cujo `build.id == active_release.build_id`.
  - Click abre `status.url` em nova tab.
  - Tooltip "Open live URL" no hover.
  - **Known affordance gap (EC-9 absorbed v1.1):** se `active_release.build_id` está em página 1 mas user fez scroll para página 2 da DataTable, o ícone NÃO fica visível. Pagination-level "current production" indicator é deliberadamente OUT OF SCOPE — user pode sortar/filtrar para retornar à página 1. Documentado como known gap, NÃO bug.
- **DoD**: 1 vitest spec GREEN.

### Phase 4 — LIVE bash regression test + CI allowlist (1h — was 45min, +15min for T4.2)

#### T4.1 — Dogfood LIVE test against dev-public

- **Files to edit**: `infra/tests/e2e/dev-public-live-url-discoverability_test.sh` (NEW).
- **Why this step**: hermetic + vitest provam contract; LIVE bash prova chain real end-to-end (memory `feedback_4layer_drift_extension_2026_06_09`). Status.url pode mudar shape em upgrade futuro do backend; LIVE test trava regressão.
- **TDD scenarios**:
  - SC-A: `GET /api/v1/projects/teste/status?env=production` retorna `url` matching `^https://[a-z0-9-]+-[a-z0-9-]+\.loopzero\.dev$` (canonical shape `<project>-<env>.<baseDomain>` per `status.go:252`).
  - SC-B (EC-5 absorbed v1.1): `curl -sLo /dev/null -w '%{http_code}' <url>` retorna HTTP code ∈ **200..399** (aceita 200 + 3xx redirects; muitos apps respondem 302 root → /index ou 301 http → https). HEAD evitado porque diversos apps retornam 405 Method Not Allowed (especialmente Express + Next.js sem opt-in HEAD handler).
  - SC-C: `status.active_release.build_id` non-empty (precondition para Visit button render).
- **Acceptance criteria**:
  - Script aceita `--skip-live` (hermetic CI mode).
  - Exit 0 quando 3/3 SC pass.
  - `--json` output canonical.
- **DoD**: 3/3 LIVE PASS against `cloud-dev.loopzero.example`.

#### T4.2 — Shell allowlist entries (EC-2 absorbed v1.1)

- **Files to edit**: `.claude/rules/shell-scripts-allowlist.md` (append 2 rows under § Post-baseline additions).
- **Why this step**: per `.claude/rules/shell-scripts-allowlist.md` D3 (plan 2026-05-22-shell-script-strategic-plan v1.3), TODO novo `.sh` em repo é proibido por default. CI `task quality:gates` invoca `loopzero-ops quality no-new-bash` que parseia o allowlist + roda `git diff --diff-filter=AR` — se T4.1 adicionar 2 novos scripts SEM rows correspondentes neste allowlist, CI bloqueia o merge. Edge-case review caught this gap em v1.0 antes que halt-loop emitisse BLOCKED on final integration validation.
- **Procedure**: append duas linhas na seção apropriada do allowlist:
  ```
  | infra/tests/e2e/dev-public-live-url-discoverability_test.sh | ~210 | KEEP-OPERATOR 2026-06-XX — T4.1 of plan 2026-06-12-dashboard-journey-j25-live-url-discoverability. 3 LIVE scenarios A/B/C against cloud-dev.loopzero.example: status.url shape, HTTP 200..399 reachability, active_release precondition. Flags --skip-live + --json. Pure orchestration: curl + jq + python3; irreducible per shell-allowlist Justificativa rule "live-cluster HTTP probe". |
  | infra/scripts/local/dogfood-live-url.sh | ~120 | KEEP-OPERATOR 2026-06-XX — aggregate wrapper invoking T4.1 hermetic syntax + vitest J-25 specs + T4.1 LIVE 3-scenario. Flags --hermetic-only, --live-only, --json. Wired in taskfiles/test.yml as task test:dogfood:live-url. |
  ```
- **TDD**: `task quality:gates` deve PASS após o append (validates allowlist syntax via `loopzero-ops quality no-new-bash`).
- **Acceptance criteria**:
  - Ambas as rows presentes no allowlist com formato exato `| path | LoC | justificativa |`.
  - LoC count alinhado ao real LoC dos scripts (post-T4.1 implementation).
  - `task quality:gates` PASS local + CI.
- **DoD**: CI gate green; nenhum BLOCKED de `no-new-bash` no integration validation final.

### Phase 5 — Sponsor LIVE browser walkthrough (HARD STOP)

#### T5.1 — Sponsor verbatim approval

- **Procedure**: sponsor opens `https://app-dev.loopzero.example/projects/teste`, sees Live URL hero, clicks Visit, lands on `https://teste-production.loopzero.example` (canonical `<project>-<env>.<baseDomain>` per `status.go:252`) showing the app HTTP 200..399.
- **Acceptance criteria**: sponsor verbatim `Confirmado` OR `Ficou ótimo` (literal quote — wrap in code style to mark as artifact not subjective claim).
- **DoD**: sponsor LIVE approval recorded em CHANGELOG + commit body cita verbatim.

## Coverage Matrix

| # | Description | Task | Severity | Owner |
|---|---|---|---|---|
| g01 | URL hidden in Project Overview | T1.1 | HIGH | Frontend |
| g02 | URL hidden in Deployment Detail | T2.1 | HIGH | Frontend |
| g03 | URL hidden in Deployments list | T3.1 | MEDIUM | Frontend |
| g04 | No regression test for status.url shape | T0.1 + T4.1 | MEDIUM | Frontend |
| g05 | Null URL renders silently (current behavior) | T1.1 (per D3) | MEDIUM | Frontend |
| g06 | Previous deploys show misleading URL | T2.1 (anti-trap guard) | MEDIUM | Frontend |
| g07 | Multi-app URL ambiguity | out-of-scope — deferred to J-25.2 if sponsor requests per-app | LOW | Backend (futuro) |
| g08 | EC-1 v1.1 fabricated URL example in plan | D1 + T0.1 procedure + T5.1 procedure (all updated to canonical) | HIGH | Plan author |
| g09 | EC-2 v1.1 shell allowlist CI block | T4.2 (NEW sub-task) | HIGH | Plan author |
| g10 | EC-3 v1.1 T0.1 no halt condition on null URL | T0.1 acceptance criteria (HALT + escalate J-25.A) | HIGH | Plan author |
| g11 | EC-4 v1.1 multi-env Overview always production | T1.1 TDD `test_overview_uses_default_production_env` | MEDIUM | Frontend |
| g12 | EC-5 v1.1 HEAD 405 trap | T4.1 SC-B (GET + 200..399) | MEDIUM | Frontend |
| g13 | EC-6 v1.1 loading flash undefined → URL | T1.1 TDD `test_overview_renders_skeleton_while_status_loading` | MEDIUM | Frontend |
| g14 | EC-7 v1.1 copy button no TDD | T1.1 TDD `test_overview_copy_button_writes_to_clipboard_and_shows_toast` | LOW | Frontend |
| g15 | EC-8 v1.1 5s polling gap (consolidated) | T1.1 + T2.1 + T3.1 (cross-task pattern — documented in Drawbacks & Risks row; per-task acceptance inherits) | LOW | Frontend |
| g16 | EC-9 v1.1 pagination affordance gap | T3.1 acceptance (known gap, documented) | LOW | Frontend |
| g17 | EC-10 v1.1 dev/standalone hostname variation | T0.1 procedure scope note (LIVE probe dev-public/prod only) | LOW | Frontend |

## Global DoD

- [ ] vitest GREEN (**10 specs new** post-v1.1 absorption — was 6) — `cd cloud/dashboard && npm test`
  - 5 em T1.1 (visit button + pending badge + multi-env default + loading skeleton + copy button)
  - 3 em T2.1 (active = Visit visible / previous = hidden / failed = hidden)
  - 1 em T3.1 (ExternalLink only on active row)
  - 1 em T0.1 hermetic (status.url shape parser)
- [ ] `tsc --noEmit` 0 errors
- [ ] ESLint 0 warnings
- [ ] LIVE bash `infra/tests/e2e/dev-public-live-url-discoverability_test.sh` 3/3 PASS
- [ ] Dogfood wrapper canonical `infra/scripts/local/dogfood-live-url.sh` (NEW) invoca hermetic + LIVE — aggregate green
- [ ] **Shell allowlist (EC-2 absorbed v1.1):** 2 novas rows presentes em `.claude/rules/shell-scripts-allowlist.md` § Post-baseline additions; `task quality:gates` PASS (no-new-bash gate green)
- [ ] Wire em `taskfiles/test.yml` como `task test:dogfood:live-url`
- [ ] Sponsor LIVE browser walkthrough verbatim approval
- [ ] CHANGELOG `[Unreleased]` § Added entry
- [ ] CLAUDE.md J-25 row updated com ✅ DELIVERED + commit hash
- [ ] No internal terms in user-facing text (Rule 3 — "Visit" canonical, NOT "open route")

## Edge Cases

> **Namespace note (v1.1):** the inline list below uses plan-internal numbering (EC-1..EC-6) describing **runtime edge cases the implementation must handle**. Note distinction from the review-numbered ECs in `knowledge-base/reviews/2026-06-12-dashboard-journey-j25-live-url-discoverability-edge-cases-2026-06-12.md` (which describe **plan-quality gaps** absorbed into v1.1 and tagged "EC-N v1.1" throughout this document). Same prefix, different namespaces; cross-references always specify which.

- **EC-1**: `status.url` retorna URL com path (`/v1/foo`) ao invés de só hostname → render canonical (URL.parse válido); link funciona normal.
- **EC-2**: User clica Visit em `dev-public` mas dev tunnel está down → browser-level concern, fora do escopo. Tooltip pre-existe "URL can take ~60s on first deploy".
- **EC-3**: Project sem release ACTIVE (estado idle) → `status.url == null` → badge "URL pending" (ADR D3).
- **EC-4**: Rollback em progresso (5s gap) → URL temporariamente stale; useProjectStatus refetch a cada 5s mitiga.
- **EC-5**: useProjectStatus returns 401 (auth expired) → ProjectHealthCard já trata; hero card NÃO renderiza (skeleton).
- **EC-6**: Copy button click sem permissão clipboard (Safari quirk) → fallback `document.execCommand('copy')`; toast "Copied" still shown.

## Lessons-learned applied

- **#5 (J-17 REST + SSE coexistem)**: applied at Phase 0 — explicit `useProjectStatus()` polling refetch (zero SSE for URL).
- **#4 (J-17 empty failure logs)**: applied at ADR D3 — null URL renders explicit badge, never silent omit.
- **#10 (J-17 sponsor scope-extension)**: pre-cleared — J-25 escopo é `cloud/dashboard/src/pages/{project-detail,deployment-detail,deployments}.tsx` + LIVE test + dogfood wrapper. No cross-domain extension needed.

## Final Phase: Integration Validation

Run after Phases 0-5 closure:

1. `tsc --noEmit` + ESLint clean
2. `npm test` GREEN (10 specs total — see Global DoD)
3. `task quality:gates` PASS (validates allowlist + 4-layer drift gates per `feedback_4layer_drift_extension_2026_06_09`)
4. LIVE bash `dev-public-live-url-discoverability_test.sh` 3/3 PASS
5. Sponsor browser walkthrough JUNTOS — opens URL from Overview in 0 clicks
6. CHANGELOG entry
7. Commit message: `feat(dashboard): J-25 Live URL Discoverability — 3 slots canonical`
8. Mark CLAUDE.md J-25 ✅ DELIVERED 2026-06-XX

The plan is NOT complete until step 8 lands.
