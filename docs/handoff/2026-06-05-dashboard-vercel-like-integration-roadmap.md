# Dashboard Vercel-Like Integration Roadmap

**Data:** 2026-06-05
**Autor:** Engineering (Claude session)
**Status:** ROADMAP (input para planos individuais por jornada — `/to-plan`)
**Cobertura:** todas as 15 jornadas raiz + sub-rotas do `cloud/dashboard/`
**Ambiente alvo:** `dev-public` (`*-dev.loopzero.example` LIVE) → produção paridade

---

## 1. Objetivo

Tornar o `cloud/dashboard/` **100% integrado e funcional via DNS público**, com paridade de expectativa de usuário final estilo **Vercel** — o operador realiza ações pelo portal (deploy, scale, rollback, promote, secrets, domains, invites, etc.) sem precisar do CLI.

Este documento é um **roadmap**, não um plano. Cada jornada listada aqui será materializada como **plano individual** (`docs/plans/YYYY-MM-DD-dashboard-journey-<name>-plan.md`) com tasks granulares, DoD e edge-case-review.

---

## 2. Método: o que "Vercel-like 100% integrado" significa

Cada jornada do dashboard deve satisfazer **5 critérios** simultâneos:

1. **Dados reais do backend** — zero mock; XHR via `engineFetch` / `cloudFetch` contra endpoint canonical; loading + empty + error states explícitos.
2. **Ações via UI (não só leitura)** — cada verbo do CLI `loopzero` que faz sentido na UI tem botão/form/wizard equivalente (escopo abaixo por jornada). Ações destrutivas têm `ConfirmDialog`.
3. **Realtime onde aplicável** — SSE/polling para deploys em andamento, build logs streaming, status de release transitando (ex: `Progressing → Healthy`).
4. **Erros úteis** — falha de API renderiza mensagem product-language (Rule 3) + ação sugerida ("Retry" / "Open in CLI" / "Contact support" se 5xx).
5. **Navegável pelo teclado** — `cmd+k` (command palette) leva a qualquer ação cobrindo verbo do CLI; tab order coerente; acessibilidade A11y básica.

---

## 3. Arquitetura LIVE (validada 2026-06-05)

```
Browser → app-dev.loopzero.example (Cloudflare DNS + Let's Encrypt YE2)
       → Caddy reverse-proxy (host: paulo's machine, PID 3344952)
         ├── /self-service/* → Kratos public (PF 14433, Host rewrite auth-dev)
         ├── /sessions/*     → Kratos public (PF 14433, Host rewrite auth-dev)
         └── /*              → Vite dev :5173
                              ├── /v1/*      proxy → loopzero-cloud (PF 13002)
                              ├── /engine/*  proxy → loopzero-cloud → loopzero-api
                              ├── /oauth2/*  proxy → Hydra (PF 14444)
                              └── SPA assets (React Router)

PFs gerenciados por: infra/scripts/local/port-forward-public-dashboard.sh
  → signal handling + exponential backoff + PID file
```

**Estado de integração network:** ✅ 100% LIVE (probes 401/200 corretos em todos endpoints).
**Estado de integração funcional:** parcial — este roadmap cobre o gap.

---

## 4. Jornadas

### Convenção de cada bloco

```
### J-N — <título>
- Rotas: /xxx [+ sub-rotas]
- Estado atual: <o que já funciona>
- Visão Vercel-like: <o que o usuário deve poder FAZER pela UI>
- Backend deps: <endpoints/serviços canonical>
- Gap principal: <top blocker para chegar em 100%>
- DoD: <critério de aceitação factual high-level>
- Prioridade: P0 (gating) | P1 (importante) | P2 (nice-to-have)
```

---

### J-01 — Login (OIDC sem senha)

- **Rotas**: `/login`
- **Estado atual**: ✅ LIVE — OIDC redirect GitHub + Google validado (`dev-public-oidc-redirect_test.sh` 8/8 PASS). Top-level form POST preserva SameSite=Lax. Callback aterrissa em `auth-dev.loopzero.example/self-service/methods/oidc/callback/{provider}` registrado nas OAuth Apps.
- **Visão Vercel-like**: usuário clica "Continue with GitHub" / "Continue with Google", autoriza no provedor, retorna autenticado direto pra `/` (overview). Mensagem clara se erro (provider down, cookie bloqueado, etc.).
- **Backend deps**: Kratos public `/self-service/login/browser`, Hydra OAuth2 (PF 14444), Stripe customer creation idempotente (sign-up flow), `engineFetch /api/v1/auth/whoami`.
- **Gap principal**: paridade login/register/recovery — fluxos de erro (cookie bloqueado, OAuth denied, network) precisam mensagens product-language em vez de JSON-dump genérico do Kratos.
- **DoD**: 5 cenários browser (GH happy, Google happy, OAuth denied, cookie bloqueado, network fail) com screenshots + assertion DOM.
- **Prioridade**: **P0** (gating para todas outras jornadas autenticadas).

### J-02 — Register (sign-up novo tenant)

- **Rotas**: `/register`
- **Estado atual**: ✅ rota wired, OIDC compartilha o handler do login.
- **Visão Vercel-like**: novo usuário cria conta via OAuth, tenant é provisionado (cell-placement automático per RoundRobinEngine), redirect pra onboarding (`empty-projects-hero`). Conta automaticamente vinculada a Stripe Customer (Free plan).
- **Backend deps**: Kratos identity create + after-hooks Webhook → `cloud /v1/auth/register` → `loopzero-api /api/v1/bootstrap` (cria Tenant CR + cell pin).
- **Gap principal**: o flow `Kratos webhook → cloud register → loopzero-api bootstrap → Stripe customer` é assíncrono — UI precisa surface estado de provisionamento (Tenant.Status.Phase=Provisioning → Active) com SSE/polling ou redirect-after-ready.
- **DoD**: novo OAuth account → Tenant CR phase=Active em <60s → primeiro `/projects` carrega com `EmptyProjectsHero`.
- **Prioridade**: **P0** (gating onboarding).

### J-03 — Verification (email code via Resend)

- **Rotas**: `/verification`
- **Estado atual**: ✅ LIVE — Resend courier validado (live-proof L3 zero abandoned messages). `ensureCodeRequested(flowId)` corrige PinInput-empty trap (commit `3c81f5eb`).
- **Visão Vercel-like**: usuário vê PinInput com input de 6 dígitos, recebe email <30s, cola código, conta marcada verified, redirect pra app. Botão "Resend code" com cooldown 60s. Suporta colar código direto da clipboard (paste-detect).
- **Backend deps**: Kratos verification flow, Kratos courier StatefulSet + Resend HTTP API, `engineFetch /api/v1/auth/whoami` (verified flag).
- **Gap principal**: cooldown UI no botão Resend + erro se Resend API key inválida (atualmente mostra Kratos JSON crudo).
- **DoD**: e2e browser test "submit OAuth → email arrives <30s → paste code → /projects carrega" + Resend API failure mostra mensagem product-language.
- **Prioridade**: **P1** (importante mas OIDC flow já pode skipear verification).

### J-04 — Recovery (reset de senha) ✅ COMPLETO 2026-06-05

- **Rotas**: `/recovery`
- **Estado atual**: ✅ **PRODUÇÃO LIVE-VALIDATED** contra `app-dev.loopzero.example` real. Flow completo email → code → password → success. OIDC educational link (D1 — never detective per `account_enumeration.mitigate=true`). Sponsor confirmou manualmente: "Funcionou parabens!" 2026-06-05.
- **Plano**: `.claude/knowledge-base/plans/2026-06-05-dashboard-journey-j04-recovery-plan.md` v1.3 (17 tasks T0.1→T5.4, 9 edge cases EC-1..EC-9).
- **Commits**: `b9362edf` (full implementation + Kratos 422 bug fix) + `4a619baf` (T5.3 Playwright runtime).
- **Validação multi-camada**:
  - 20/20 vitest recovery.test.tsx PASS
  - 5/5 T5.1 bash LIVE contra `auth-dev.loopzero.example` (kubectl scale=0 Kratos + restore)
  - 3/3 T5.3 Playwright chromium headless (Cenários B GitHub + B Google + F EC-4 currying body spy)
  - `task test:dogfood:recovery` 3/3 suites GREEN
- **Bug crítico capturado em produção** (LIVE-only — escapou de 369 vitest tests): Kratos browser-mode retorna HTTP 422 + `browser_location_change_required` + `redirect_browser_to: /settings?flow=<new>` quando code é verificado (NÃO `state: passed_challenge`). Memória sistêmica: `[[reference_kratos_browser_mode_422_redirect_2026_06_05]]` — para journeys cookie-stateful via Kratos OIDC, Playwright headless é MANDATÓRIO porque vitest+jsdom abstrai protocolos cookie/CSP/cross-origin.
- **Prioridade original P2 → entregue antecipadamente** por sponsor request durante sessão 2026-06-05 (sponsor identificou bug LIVE → fix-forward loop completou T0-T5 + bug fix em 1 sessão).
- **DoD original (2 cenários)**: ambos cobertos — password-based via Cenário A (T5.1) + OIDC-only graceful redirect via OIDC educational links (T3.1, sempre visíveis preservando enumeration mitigation).

### J-05 — Device Success (OAuth device-flow confirmation) — 🟡 ENGINEERING COMPLETE 2026-06-05

- **Rotas**: `/device-success`
- **Estado atual**: ✅ ENGINEERING COMPLETE — page completa + Hydra config + counter + tests. Operator gates pendentes (T0.1 recon LIVE + LIVE manual `loopzero login` + `/dogfood full` + `/cross-validation`).
- **Visão Vercel-like**: ✅ entregue — confirmation page com auto-close 5s + "Back to dashboard" + accessibility (prefers-reduced-motion) + fallback when browser blocks `window.close`.
- **Backend deps**: ✅ Hydra `urls.post_device_done_url` configurado em `infra/helm/ory-hydra/values.yaml.gotmpl` com `trimSuffix "/"` defense (EC-8); dev-public.yaml docs `uiBaseUrl` controla 4 URLs.
- **Telemetry**: ✅ counter `loopzero_device_flow_browser_success_total` + browser beacon para `/v1/telemetry/events` reusando handler existente (KISS — sem endpoint duplicado).
- **Plano**: `.claude/knowledge-base/plans/2026-06-05-dashboard-journey-j05-device-success-plan.md` v1.1 com 11 edge cases absorvidos.
- **Implementation**: commit `1644e60f` — 14 files, +1173/-3 LOC.
- **Tests aggregate**: **51/0 GREEN assertions** engineering-side
  - 9/9 vitest (`cd cloud/dashboard && npx vitest run src/pages/device-success.test.tsx`) — 4.93s
  - 11/11 Go telemetry (`go test ./cloud/tests/unit/ -run TestTelemetry`) — 8 originais + 3 J-05 (counter+, counter not++, rejected paths)
  - 19/0 hermetic LIVE-bash structure (`bash infra/tests/check-dev-public-device-flow-syntax_test.sh`)
  - 6/0 hermetic helm-values (`bash infra/tests/helm/ory-hydra-device-urls_test.sh`)
  - 2/2 Playwright chromium headless (`npx playwright test e2e/device-success.spec.ts`) — 11.3s
  - 4/4 dogfood wrapper hermetic (`task test:dogfood:device --hermetic-only`)
- **HARD STOPs operator** (não atingíveis no Ralph loop sem cluster live):
  - **T0.1 recon LIVE** — kubectl/curl/Playwright trace contra dev-public real
  - **3/3 LIVE bash** — requer DNS `cloud-dev.loopzero.example` + `auth-dev.loopzero.example` reachable
  - **LIVE manual** `loopzero login --device-flow` end-to-end + counter ≥ 1 em Prometheus
  - **`/dogfood full`** health ≥ 70 (4-cluster k3d quente)
  - **`/cross-validation`** APROVADO via skill manual
- **Prioridade**: **P1** entregue engineering-side (antecipado).

### J-06 — Overview (home autenticada)

- **Rotas**: `/`
- **Estado atual**: ✅ wired — usa `useProjects` hook. KPIs + atividade recente + CTA "Deploy your first app".
- **Visão Vercel-like**: KPI strip (Projects, Deployments last 7d, Active environments, Spend MTD), atividade recente (últimos 10 deploys + status), gráfico de deploys por dia, atalho "Create Project" + "Connect Repo".
- **Backend deps**: `engineFetch /api/v1/projects`, `engineFetch /api/v1/builds?limit=10`, `cloudFetch /v1/billing` (spend MTD), `engineFetch /api/v1/environments`.
- **Gap principal**: KPI strip atual usa contadores estáticos; falta hook agregador para spend MTD + deploys-by-day. Onboarding hero para teams sem nenhum projeto precisa ser revalidado (mostrar `EmptyProjectsHero` integrado com Stripe pricing).
- **DoD**: 3 fixtures de browser test: (a) team com 0 projetos → hero, (b) team com N projetos → KPIs preenchidos com dados reais do API, (c) Pro team com spend>$0 → KPI billing populado.
- **Prioridade**: **P0** (porta de entrada de toda jornada autenticada).

### J-07 — Projects List

- **Rotas**: `/projects`
- **Estado atual**: ✅ LIVE — `useProjects` hook + `Table` + `EmptyProjectsHero` + `CreateProjectSheet`. Visual consistency per 2026-05-26 sponsor directive.
- **Visão Vercel-like**: tabela com colunas (Name, Framework, Latest deploy, Last activity, Status), filtros + search, "Create Project" sheet abrindo wizard (name → repo connect → first deploy). Bulk actions: select + delete múltiplos. Link clicável pra `/projects/:name`.
- **Backend deps**: `engineFetch /api/v1/projects` (GET, POST), `/api/v1/builds?project=X&latest=true`.
- **Gap principal**: `CreateProjectSheet` precisa wizardizar: name → connect repo (J-09) → trigger first deploy. Bulk delete não existe.
- **DoD**: criar projeto via UI → aparece na lista → click leva a `/projects/:name` com EmptyState (sem deploys ainda).
- **Prioridade**: **P0** (centro do produto).

### J-08 — Project Detail + sub-rotas

- **Rotas**: `/projects/:name` + sub-rotas via sidebar:
  - `/projects/:name` (Overview)
  - `/projects/:name/deployments` (List)
  - `/projects/:name/deployments/:id` (Detail) + `/build-logs` + `/build-sbom` + `/build-attestation`
  - `/projects/:name/status`
  - `/projects/:name/logs` (runtime logs streaming)
  - `/projects/:name/scale`
  - `/projects/:name/readiness`
  - `/projects/:name/secrets`
  - `/projects/:name/previews`
  - `/projects/:name/connect` (repo)
- **Estado atual**: ✅ todas páginas existem (`project-*.tsx`, `deployments.tsx`, `deployment-detail.tsx`, `build-*.tsx`, `previews.tsx`). Hooks: `useDeployments`, `useDeployment`, `useLogStream`, `useReadinessSignals`, `useSecrets`, `usePreviews`, `useProjectStatus`.
- **Visão Vercel-like**: Overview com latest deploy + status banner. **Botões de ação**: "Deploy" (trigger build novo do branch), "Rollback to <previous>", "Promote staging → production", "Scale" (slider replicas), "Cancel deploy" (in-progress), "Redeploy". Build logs com tail + download. SBOM viewer + attestation download. Secrets CRUD (add/rotate/delete) com masked values. Previews list com link público + delete.
- **Backend deps**: cobertura completa `loopzero-api` — `POST /api/v1/builds`, `POST /api/v1/promote`, `POST /api/v1/rollback`, `PATCH /api/v1/projects/:name/scale`, `POST /api/v1/builds/:id/cancel`, `GET /api/v1/builds/:id/logs` (SSE), `GET /api/v1/builds/:id/sbom`, `GET /api/v1/builds/:id/attestation`, secrets CRUD via cloud `/v1/projects/:name/secrets`, previews via loopzero-api.
- **Gap principal**: **a jornada mais densa**. Estimar como **5 planos independentes** depois (J-08a Deploy/Rollback/Promote, J-08b Build logs+SBOM+attest, J-08c Scale, J-08d Secrets, J-08e Previews). Cada ação é botão na UI atualmente ausente ou parcial.
- **DoD**: 1 fluxo crítico end-to-end browser: "Connect repo → first deploy → ver build logs streaming → deploy Healthy → click rollback → release ROLLED_BACK". Outras sub-rotas têm DoDs próprios nos planos.
- **Prioridade**: **P0** (é onde o produto vive).

### J-09 — Project Connect (repo integration)

- **Rotas**: `/projects/:name/connect`
- **Estado atual**: página `project-connect.tsx` existe.
- **Visão Vercel-like**: install GitHub App `LoopZero Platform` (ID 3319955) via OAuth, selecionar repo + branch, validar `loopzero.yaml` presente (ou wizard gera um), trigger first build. Suporta GitLab + Bitbucket (P2).
- **Backend deps**: GitHub App install URL, `engineFetch /api/v1/source/install` (callback handler), webhook de `push` registra auto-deploy.
- **Gap principal**: install flow + webhook auto-deploy precisa ser browser-validado em dev-public (sponsor mandate "ZERO mock").
- **DoD**: instalar GitHub App em conta real → escolher repo → push commit → ArgoCD App aparece com release ACTIVE.
- **Prioridade**: **P0** (sem connect, projeto não deploya).

### J-10 — Environments

- **Rotas**: `/environments`
- **Estado atual**: `environments.tsx` + `CreateEnvironmentSheet` + `environments.test.tsx`.
- **Visão Vercel-like**: lista de envs (production, staging, custom + previews), criar env custom via sheet (nome + cell pin opcional), deletar env (com confirm — destructive), promover env → env (promote source → target). KPI por env (replicas, uptime).
- **Backend deps**: `engineFetch /api/v1/environments` CRUD, env-level secrets, promote handler.
- **Gap principal**: promote flow staging→production via UI (atualmente só CLI `loopzero promote`), confirm destrutivo no delete.
- **DoD**: criar env custom via UI → deploy nela → promote pra prod → verificar release SUPERSEDED na staging + ACTIVE na prod.
- **Prioridade**: **P1** (importante mas production+staging cobrem maioria dos casos).

### J-11 — Domains

- **Rotas**: `/domains`
- **Estado atual**: ✅ `domains.tsx` + `AddDomainSheet` + `domains.test.tsx`. CodeBlock + StatusDot + CopyButton.
- **Visão Vercel-like**: lista domains com status (Verified, Provisioning, Active, Degraded, Failed), add domain (sheet → input → verify NS instructions → CF Zone created → cert issued auto), delete domain (com confirm), re-verify button para Failed/Degraded.
- **Backend deps**: `engineFetch /api/v1/domains` CRUD, CF Zone Management (Cloudflare API via loopzero-api).
- **Gap principal**: instruções NS change visuais (mostrar nameservers Cloudflare assignados pra usuário copiar pro registrador), polling de verification status.
- **DoD**: adicionar domain `loopzero-test.example` via UI → ver instruction NS records → simular DNS change → status ACTIVE em <5min (k3d local usa fixture) ou 24h (prod CF Zone real).
- **Prioridade**: **P1** (importante para custom domains; usuários Free não usam).

### J-12 — Team (RBAC + invites)

- **Rotas**: `/team`
- **Estado atual**: `team.tsx` + `InviteMemberSheet` + Avatar/Select/ConfirmDialog.
- **Visão Vercel-like**: tabela de membros (avatar, email, role, joined), invite por email com role selector (Owner/Member/Viewer), revoke/role-change pelo Owner. Suporta team switch (top bar) quando usuário tem múltiplos teams.
- **Backend deps**: cloud `/v1/teams/:id/members` CRUD, Kratos identity lookup, invite email via Resend.
- **Gap principal**: invite flow precisa Kratos identity-create-on-first-login + email Resend; revoke + role-change UI.
- **DoD**: convidar email novo → email chega via Resend → clica link → OIDC signup → aparece no team com role atribuído. Owner pode mudar role + revoke.
- **Prioridade**: **P1** (Team tier Early Access; público GA é Pro single-seat).

### J-13 — Billing (Stripe parity)

- **Rotas**: `/billing` + `/settings/billing` (espelho)
- **Estado atual**: ✅ LIVE — Stripe Customer Portal validado (live-proof L3 PASS). `PlanBadge` + `UsageMeter`. Webhook split per-env (`we_1TeVZFJF08yCtMK1RiMjeLc0`).
- **Visão Vercel-like**: plan card atual + usage meters (projects used / cap, builds this month, bandwidth, etc.), "Upgrade to Pro" button → Stripe Checkout, "Manage subscription" → Stripe Portal, lista de invoices com download PDF, payment method, billing email.
- **Backend deps**: `cloudFetch /v1/billing` (GET), `/v1/billing/checkout` (POST), `/v1/billing/portal` (POST), Stripe webhook `checkout.session.completed` + `customer.subscription.*`.
- **Gap principal**: invoices PDF lista + download (Stripe Portal já cobre, mas UX nativa é mais Vercel-like). Upgrade flow (`/billing?upgrade=pro`) integrado com URL param polling-by-`subscription_id` (já implementado per memory).
- **DoD**: Free user → click Upgrade → Stripe Checkout → pagamento → webhook plan=pro → UI reflete em <30s. Pro user → click Manage → portal → cancela → webhook → plan=starter ao final do período.
- **Prioridade**: **P0** (revenue gate).

### J-14 — Audit Log

- **Rotas**: `/audit`
- **Estado atual**: ✅ `audit.tsx` com filtros via `useQueryState` (nuqs), tabela com severity badges.
- **Visão Vercel-like**: lista paginada de audit entries (action, resource_type, actor, timestamp, severity), filtros (action, severity, date range), export CSV.
- **Backend deps**: `engineFetch /api/v1/audit?action=X&severity=Y&since=Z`.
- **Gap principal**: export CSV botão; filter chips visuais; "view raw JSON" expandable por linha.
- **DoD**: filtrar `severity=critical` → ver apenas entries críticos → export CSV → arquivo válido.
- **Prioridade**: **P2** (importante para compliance Pro/Team, não bloqueia adoção Free).

### J-15 — Profile

- **Rotas**: `/profile`
- **Estado atual**: ✅ `profile.tsx` + Avatar + DangerZone + ConfirmDialog. Usa `logout()` + `engineFetch`.
- **Visão Vercel-like**: foto avatar editável (upload), display name, email (read-only — vem do OIDC provider), linked OAuth providers (GitHub + Google badges), API tokens (create, list, revoke), Sessions (devices logged in, revoke individual), Danger zone (delete account).
- **Backend deps**: Kratos identity update, `engineFetch /api/v1/users/me` (PATCH), tokens via cloud `/v1/users/me/tokens`, sessions via Kratos `/sessions`.
- **Gap principal**: avatar upload (Cloudflare R2 ou Spaces), API tokens CRUD, sessions list.
- **DoD**: editar display name → persiste → reload mantém. Criar token → copy → testa `loopzero login --token <X>` → funciona.
- **Prioridade**: **P1** (importante para devs que querem CLI auth via token).

### J-16 — Settings (workspace)

- **Rotas**: `/settings` + sub-rotas:
  - `/settings` (General)
  - `/settings/members` (espelho de `/team`)
  - `/settings/billing` (espelho de `/billing`)
  - + Developer subgroup (webhooks, API logs, etc. per sidebar)
- **Estado atual**: rotas wired no sidebar; algumas páginas reusam de `/team` e `/billing`.
- **Visão Vercel-like**: General (workspace name, slug, default region/cell, default branch), Members (espelho J-12), Billing (espelho J-13), Webhooks (deploy notifications), API logs (request logs for tokens).
- **Backend deps**: cloud `/v1/teams/:id` CRUD, webhooks store, API access logs (depends on observability stack).
- **Gap principal**: General page (workspace metadata edit), Webhooks CRUD, API logs view.
- **DoD**: editar workspace name → persistir; criar webhook → push commit → ver delivery log.
- **Prioridade**: **P1** (settings completos são Vercel-parity expectation).

---

## 5. Cross-Cutting (paralelo a todas jornadas)

Estes itens NÃO viram planos próprios — atravessam todas as jornadas e devem ser tracked nos planos individuais quando relevante.

### CC-01 — Loading / Empty / Error states padronizados

- Skeleton loader em listas (3-5 rows).
- `EmptyState` component com CTA específico por jornada.
- Error states: 401 → redirect `/login`; 403 → "Insufficient permissions"; 4xx → mensagem do API; 5xx → "Service temporarily unavailable, retry" + retry button; network → "Check your connection".

### CC-02 — Command Palette (`cmd+k`)

- ✅ `use-command-palette` hook existe.
- Cobrir todos verbos do CLI: "Deploy <project>", "Rollback <project>", "Promote staging → prod", "Create env", "Add domain", "Invite teammate", "Open billing".

### CC-03 — Realtime / SSE

- Build logs streaming: `useLogStream` ✅ existe.
- Deploy progress (Argo Workflow phases): SSE event source `/api/v1/builds/:id/events`.
- Release transitions (PENDING → ACTIVE / Healthy / Failed): SSE ou polling 5s.

### CC-04 — Auth gating + redirect-after-login

- `useRequireAuth` ✅ existe.
- Preservar URL pretendida: `/login?next=/projects/foo` redireciona pós-OIDC. (Atualmente: redirect sempre pra `/`.)

### CC-05 — Mobile responsive

- ❌ Não validado.
- Vercel-parity: ler-only em mobile é OK; ações destrutivas e wizards podem requerer desktop.

### CC-06 — Telemetry / observability

- OpenTelemetry tracing dashboard → API via Correlation-ID header.
- Sentry-style error capture pra UI crashes (component error boundaries já existem via `<Boundaried>`).

### CC-07 — i18n (deferred)

- Vercel suporta multi-idioma — LoopZero manda em pt+en por padrão. Estrutura `@loopzero/ui` precisa suportar string overrides. **P2** post-GA.

### CC-08 — Playwright headless MANDATÓRIO para journeys via Kratos OIDC (LESSON 2026-06-05)

Validação capturada durante J-04 implementation (`[[reference_kratos_browser_mode_422_redirect_2026_06_05]]`):

**Padrão sistêmico**: para qualquer jornada que toca Kratos browser-flow (login, register, verification, recovery, settings, OIDC callbacks), **vitest + jsdom NÃO é suficiente** porque abstrai 4 protocolos críticos:

1. **Cookie SameSite cross-origin** — `credentials: include` em XHR para auth-dev.loopzero.example a partir de app-dev.loopzero.example
2. **CSP `connect-src` directives** — fixtures com `http://localhost/...` violam CSP em dev real (CSP blocks fetch). Use same-origin relativo `/self-service/...`.
3. **Kratos browser-mode response shapes** — HTTP 422 + `error.id: browser_location_change_required` + `redirect_browser_to` (success-with-redirect protocol) NÃO é canônico nos mocks `{state: 'passed_challenge'}`. Detectar AMBOS path antes do `state` check.
4. **CORS preflight + Set-Cookie** — somente browser real executa o full handshake.

**Bug que escapou de 369 vitest tests + 5 LIVE bash PASS**: Kratos 422 redirect tratado como erro genérico → TypeError em `getFlowMessages(undefined.messages)` → user vê "Recovery service temporarily unavailable". Só apareceu quando sponsor testou manualmente.

**DoD obrigatório para J-01, J-02, J-03, J-04 (Kratos-touching)**:
- [ ] Playwright spec covering at least 1 happy path + 1 EC-* edge case via DOM interaction
- [ ] Playwright route fixtures use same-origin relative URLs (NOT `http://localhost/...`)
- [ ] Playwright runs against Vite dev :5173 (Caddy proxy stack same as production)
- [ ] T5.4-style wrapper detects Playwright presence via `node_modules/@playwright/test` existence

**Reusable artifacts** (já wired em J-04 — copiar+adaptar):
- `cloud/dashboard/playwright.config.ts` (configured for chromium/firefox/webkit/mobile-chrome)
- `cloud/dashboard/e2e/recovery.spec.ts` (route interception + body spy pattern)
- `infra/scripts/local/dogfood-recovery.sh` Suite 2b (conditional Playwright invocation)
- `task test:dogfood:recovery` wrapper (3-suite aggregate verdict)

**Install gate** (~30s, 113MB chromium download):
```bash
cd cloud/dashboard && npx playwright install chromium
npx playwright install-deps chromium  # system libs libatk-1.0.so.0 etc
```

---

## 6. Ordem de execução sugerida

Por dependência + prioridade:

```
WAVE 1 (P0 — gating, ~3 semanas)
  J-01 Login + J-02 Register + J-05 Device Success
  → J-06 Overview (depende de Login)
  → J-07 Projects List + J-09 Connect Repo
  → J-08 Project Detail core actions (Deploy + Rollback)
  → J-13 Billing (revenue gate)

WAVE 2 (P1 — completude, ~3 semanas)
  J-03 Verification (UX polish)
  J-08 sub-actions (Scale, Secrets, Previews, Build logs, SBOM, Attest)
  J-10 Environments (promote flow)
  J-11 Domains (NS instructions polish)
  J-12 Team (invites + RBAC)
  J-15 Profile (API tokens)
  J-16 Settings General + Webhooks

WAVE 3 (P2 — polish, ~2 semanas)
  J-04 Recovery — ✅ COMPLETO 2026-06-05 (antecipado; LIVE-validated)
  J-14 Audit export CSV + filter chips
  CC-05 Mobile responsive
  CC-07 i18n

WAVE 4 (cross-cutting validation)
  CC-01..CC-04 testes integrados em todas jornadas
  Full browser e2e suite (Playwright + dev-public)
  Performance budgets (Lighthouse ≥90 LCP/FID/CLS)
```

---

## 7. Open Questions (resolver antes de spawn-ar planos individuais)

1. **Database de teams** — UI mostra "switch team" quando usuário tem multi-team membership. Webhooks de team invite precisam idempotência. Validar schema `team_memberships` antes de J-12.
2. **API tokens scope** — atualmente JWT do CLI tem mesmas permissões que session web. Vercel scopa tokens por permission. **Decisão pendente:** introduzir scoped tokens v1 ou v1.x?
3. **GitLab/Bitbucket support em J-09** — Vercel-parity dá conexão Git multi-provider. **Decisão:** v1.0 apenas GitHub, GitLab/Bitbucket post-launch.
4. **Mobile** — usuários de LoopZero são devs, dev no mobile é raro. **Decisão:** read-only mobile parity, ações destrutivas show "Switch to desktop".
5. **A11y** — qual nível WCAG 2.1 (AA padrão, AAA aspiracional)? Decisão para configurar nos testes.
6. **Bundle size budget** — Vite dev mode é fácil; production build precisa budget. Lighthouse already exists (`lighthouserc.json`).

---

## 8. Próximo passo

Para cada jornada P0 (J-01, J-02, J-05, J-06, J-07, J-08, J-09, J-13), spawn-ar plano individual:

```
/to-plan "Plan for dashboard journey J-XX <name>: <visão Vercel-like resumida>.
         Inputs: este roadmap, current page + hooks, CLI verbs paridade.
         Output: docs/plans/2026-06-XX-dashboard-journey-jXX-<slug>-plan.md
         com tasks granulares, DoD por task, edge-case-review."
```

Cada plano herda Cross-Cutting CC-01..CC-04 como acceptance criteria implícito.

---

**Status:** Roadmap **APROVADO PENDENTE** (aguarda validação do sponsor das prioridades de wave + open questions).
**Audit trail:** este file commitado em `docs/handoff/`, referenciável por cada plano individual via `[[2026-06-05-dashboard-vercel-like-integration-roadmap]]`.
