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

### J-05 — Device Success (OAuth device-flow confirmation) — ✅ COMPLETO 2026-06-05 (LIVE END-TO-END)

- **Rotas**: `/device-success`
- **Estado atual**: ✅ **DELIVERED + LIVE-VALIDATED end-to-end** — page completa + Hydra config canonical + counter + tests + JWT real entregue + refresh lifecycle completo per RFC 8628. Sponsor LIVE-validated cross-device 2026-06-05.
- **Visão Vercel-like**: ✅ entregue — confirmation page com auto-close 5s + "Back to dashboard" + accessibility (prefers-reduced-motion) + fallback when browser blocks `window.close`.
- **Backend deps**: ✅ Hydra v2.6 nested `urls.device.{verification,success}` configurado em `infra/helm/ory-hydra/values.yaml.gotmpl` (key `post_device_done_url` foi descartada — rejeitada pelo schema Hydra v2.6); `urls.self.{issuer,public}` + `urls.{login,consent}` apontando para FQDNs canônicos públicos.
- **Telemetry**: ✅ counter `loopzero_device_flow_browser_success_total` + browser beacon para `/v1/telemetry/events` reusando handler existente (KISS — sem endpoint duplicado).
- **Plano**: `.claude/knowledge-base/plans/2026-06-05-dashboard-journey-j05-device-success-plan.md` v1.1 com 11 edge cases absorvidos.
- **Implementation cascade**: 11 commits (`8e6cf840` plan → `1644e60f` base → `87d64e78` roadmap → `ee2bcbfb` CLI flag → `0e2630e1` Hydra urls → `0dd1b3ea` device-verify UI + Caddy → `d36c98cc` Go tests + Playwright → `18602410` urls.login fix → `9e009d3a` ConsentRequest nested → `094aa8c0` roadmap done → `efa98575` audit+xval → `e8ad8bd6` audience whitelist).
- **Tests aggregate**: **~1530 GREEN assertions** (hermetic + LIVE)
  - 15/15 vitest dashboard (9 device-success + 6 device-verify)
  - 22/22 Go cloud (11 telemetry + 6 device handler + 5 hydra schema + audience guard)
  - 39/39 CLI login (35 baseline + 4 J-05 flag)
  - 1401/1401 CLI full suite (zero regressions)
  - 5/5 Playwright chromium headless (2 device-success + 3 device-verify) — 6.8s
  - 19/0 hermetic LIVE-bash structure
  - 10/0 hermetic helm-values (`urls.device.*` + EC-8 + regression guard)
  - 13/0 LIVE bash dogfood (A authorize + B token-pending + C pre-flight 4 + D accept contract 3)
  - 6/6 dogfood wrapper aggregate `task test:dogfood:device`
- **LIVE end-to-end validated 2026-06-05**:
  - Sponsor `loopzero login --device-flow` returned `Authenticated as paulohenrique.java@gmail.com` + `Team: tn-3e07e6cdd2ffe904` + `Token expires: in 1440 minutes`. Cross-device flow (CLI on cloud shell + browser on personal laptop) per RFC 8628 — works first-class.
  - JWT real RS256 saved at `~/.loopzero/config.json` (1345 chars), decoded payload: `aud=[loopzero-cloud,loopzero-engine]`, `client_id=loopzero-cli`, `ext.email=paulohenrique.java@gmail.com`, `scp=[openid,offline_access]`, `sub=fb1ad8db-…` (Kratos identity ID). Refresh token + token_expires_at persisted.
  - **Runtime-metric proof**: `loopzero_device_flow_browser_success_total = 4` LIVE at `kubectl port-forward svc/loopzero-cloud 13002:13002 && curl /metrics`.
  - LIVE bash dogfood 13/0 PASS against dev-public real.
  - `task test:dogfood:device` 6/6 PASS aggregate (hermetic + Go + vitest + Playwright + LIVE).
- **6 LIVE bugs caught + fixed during iter cascade** (memória sistêmica `[[reference_hydra_v2_device_flow_complete_chain_2026_06_05]]`):
  1. CLI `--device-flow` flag não registrado → `error: unknown option` (commit `ee2bcbfb`)
  2. Hydra `urls.self.public` hardcoded cluster-internal → verification_uri leaked `*.svc.cluster.local` (commit `be7d66f0`)
  3. Hydra v2.6 schema `urls.post_device_done_url` not allowed; correct keys are nested `urls.device.{verification,success}` (commit `0dd1b3ea`)
  4. Caddy `auth-dev.loopzero.example` não roteava `/oauth2/*` para Hydra public (commit `0dd1b3ea`)
  5. `urls.login` apontava para Kratos UI dashboard (sem handler login_challenge) ao invés de cloud consent handler `/auth/login` (commit `18602410`)
  6. Cloud `ConsentRequest.ClientID` esperava flat `client_id` mas Hydra v2 nests `client.client_id` → 403 "Unauthorized client" (commit `9e009d3a`)
  7. **F-03 caught durante `/dogfood --env=dev-public --scope=j05-device-flow`**: `loopzero-cli` Hydra client sem `audience` whitelist → refresh_token rejeitado com `Requested audience 'loopzero-cloud' has not been whitelisted by the OAuth 2.0 Client`. Refresh era efetivamente single-use, quebrando RFC 8628 lifecycle. Fix: `RegisterClient` payload inclui `audience: [loopzero-cloud, loopzero-engine]` + Hydra admin PUT LIVE (commit `e8ad8bd6`).
- **Dogfood QA** (skill `/dogfood --env=dev-public --scope=j05-device-flow`):
  - Pre-flight: 9/9 PASS (binário + endpoints + Hydra config canonical + counter baseline)
  - Cenários J5-A..J5-F: 6/6 PASS (cross-device login + flag + JWT + counter + Rule 3 + refresh cycle)
  - Veredito: ✅ **Pronto para uso real** com RFC 8628 lifecycle completo (login + refresh + 24h auto-renew)
  - Achados backlog: F-01 (HTML comments S3), F-02 (Hydra abort log S4), F-04 (CLI seed-script path S3) — nenhum S0/S1/S2 aberto
  - Audit recon: `.claude/knowledge-base/audits/2026-06-05-j05-device-flow-recon.md`
  - Cross-validation: `.claude/knowledge-base/reviews/cross-validation/2026-06-05-dashboard-journey-j05-device-success-xval-2026-06-05.md` — APROVADO COM RESSALVAS
- **Operator follow-ups** (não bloqueiam J-05 — out-of-scope):
  - `/dogfood full` sprint-wide health ≥ 70 (requer 4-cluster k3d + workload deploys)
  - Remover env override `URLS_SELF_ISSUER` no Hydra Deployment → JWT `iss` claim usar FQDN público
  - `helmfile sync` canonicaliza values.yaml.gotmpl para standalone-multi + drill
- **Prioridade**: **P1** entregue + LIVE-validated cross-device + RFC 8628 lifecycle completo.

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
- **Gap principal**: `CreateProjectSheet` precisa wizardizar: name → connect repo (J-09 agora ✅ disponível) → trigger first deploy. Bulk delete não existe. Após J-09 LIVE, o caminho "name → /projects/:name/connect → install GitHub App → pick repo → POST /connect → push → release ACTIVE" funciona end-to-end; o que falta é colapsar isso em um sheet único.
- **DoD**: criar projeto via UI → aparece na lista → click leva a `/projects/:name` com EmptyState (sem deploys ainda).
- **Prioridade**: **P0** (centro do produto).

### J-08 — Project Detail Overview (umbrella)

- **Rotas**: `/projects/:name` (Overview)
- **Estado atual**: ✅ página existe (`project-detail.tsx`), `useProjectStatus` hook, `ProjectHealthCard` com 4 SignalPills (null-guarded em `d61350a6`).
- **Visão Vercel-like**: hero card "latest deployment" com sha + branch + status + tempo + link logs. Health summary (replicas, deploy_status, 5xx%, alerts) com link drill-down. Sidebar navega para 8 sub-jornadas (cada uma JORNADA INDIVIDUAL P0 abaixo).
- **Gap principal**: hero card "latest deployment" mostra dados mas falta CTA principal "Deploy" (visible button) + link "View all deployments". Tab strip / sidebar nav consistente.
- **DoD**: usuário entra em `/projects/teste` → vê latest deploy + health → consegue navegar para qualquer das 8 sub-jornadas em ≤1 click.
- **Prioridade**: **P0** (centro do produto). Reduzido em escopo — sub-rotas viraram jornadas individuais J-17 a J-24.

---

> **Sponsor decision 2026-06-06**: as 8 sub-rotas que eram "J-08 sub-actions" foram promovidas a jornadas individuais P0. Justificativa: cada uma é o que o usuário FAZ no produto depois de ter o repo conectado. Tratá-las como subitens de uma única jornada subestima o trabalho e atrasa o produto core.

### J-17 — Deployments List + Detail

- **Rotas**: `/projects/:name/deployments`, `/projects/:name/deployments/:id`
- **Estado atual**: ✅ páginas existem (`deployments.tsx` + `deployment-detail.tsx`). Hook `useDeployments`.
- **Visão Vercel-like**: tabela ordenada por created_at desc com colunas (sha curto, branch, environment, status, duration, sender). Filter chips (status=Running|Succeeded|Failed, env, branch). Detail page mostra timeline (Queued → Running → Releasing → Active) + commit metadata + 4 sub-tabs (Logs, SBOM, Attestation, Manifest).
- **Botões de ação**: "Deploy" (trigger build novo da branch atual), "Cancel" (para QUEUED/RUNNING), "Rollback to this" (para release ATIVA anterior), "Redeploy" (mesmo sha).
- **Backend deps**: `GET /api/v1/builds?project=X`, `GET /api/v1/builds/:id`, `POST /api/v1/builds` (deploy), `POST /api/v1/builds/:id/cancel`, `POST /api/v1/rollback`.
- **Gap principal**: filter chips ausentes, sub-tabs na detail page (build logs+sbom+attestation+manifest), ações "Rollback to this" e "Redeploy" como buttons.
- **DoD**: tabela LIVE com 1+ deploy, click no row → detail page com timeline + sub-tabs trabalhando, click "Rollback to this" → release ROLLED_BACK no DB.
- **Prioridade**: **P0** (depois do connect funcionar, ver deploys é a operação #1).

### J-18 — Status / Runtime Health

- **Rotas**: `/projects/:name/status`
- **Estado atual**: ✅ página existe + backend default env="production" (commit `8ac14210`).
- **Visão Vercel-like**: ProjectHealthCard expandido — KPI strip (uptime %, replicas_ready/desired, latency p95, error rate, requests/min), grid de SignalPills para 8 sinais, link para Grafana dashboard com mesmo correlation_id. Histórico 7 dias por sinal.
- **Backend deps**: `GET /api/v1/projects/:name/status?env=`, `GET /api/v1/projects/:name/metrics?range=7d` (Prometheus-backed).
- **Gap principal**: histórico 7 dias ausente (hoje só snapshot atual), link Grafana ausente, deep-dive por replica (drill-down não-existe).
- **DoD**: status page LIVE para um projeto rodando, refresh a cada 5s sem flicker, all 8 SignalPills com valor real OU "—" honest.
- **Prioridade**: **P0** (debugging produtivo depende de status legível).

### J-19 — Runtime Logs Streaming

- **Rotas**: `/projects/:name/logs`
- **Estado atual**: ✅ página existe (`project-logs.tsx`), hook `useLogStream`, SSE wired.
- **Visão Vercel-like**: terminal-style log viewer (monospace, ANSI colors), filtros (replica, severity, time range, search box), download .log, "Follow" toggle (auto-scroll), pause button. Loki backend para historical query + SSE para live tail.
- **Backend deps**: `GET /api/v1/projects/:name/logs/stream?env=` (SSE), `GET /api/v1/projects/:name/logs?since=&until=&q=` para query histórica.
- **Gap principal**: search box + filtros não testados LIVE (Loki cross-cluster pode estar gappy em dev-public), ANSI parsing, download .log, "Follow" sem flicker.
- **DoD**: deploy app + tail logs LIVE → filtrar por severity=ERROR + search string → download como .log.
- **Prioridade**: **P0** (debugging em produção sem logs = produto inviável).

### J-20 — Scale (replicas + HPA)

- **Rotas**: `/projects/:name/scale`
- **Estado atual**: ✅ página existe (`project-scale.tsx`), hook `useScale`.
- **Visão Vercel-like**: slider replicas (min/max/current) com cost preview ($X/mo delta), HPA toggle (CPU/memory target), KEDA scale-to-zero opt-in (para Starter plan). Validate contra plan limits (Starter=2, Pro=5, Team=10) com 402 error UX.
- **Backend deps**: `PATCH /api/v1/projects/:name/scale`, plan enforcement em `pkg/enforcement`.
- **Gap principal**: cost preview ausente (já tem `monthlyCostDelta` mas sem ProxyToActual), HPA modal não tem todos opções, KEDA opt-in UX.
- **DoD**: scale up 1→3 replicas via UI → `kubectl get rollout` reflete 3 → cost preview consistente com Stripe.
- **Prioridade**: **P0** (escalar é operação #2 mais comum depois deploy).

### J-21 — Readiness (deploy gates)

- **Rotas**: `/projects/:name/readiness`
- **Estado atual**: ✅ página existe (`project-readiness.tsx`), hook `useReadinessSignals` (já consume status fields).
- **Visão Vercel-like**: checklist visual do que está faltando para "production-grade" (auto_deploy on, HPA configured, custom domain, secrets rotated, 2+ replicas, alerts configured). Cada item linkable → vai pra sub-rota relevante.
- **Backend deps**: query agregada — projeto + scale config + domain status + alerts. Pode ser pure frontend agregando hooks existentes.
- **Gap principal**: lista de checks definida mas não-acoplada a `/projects/teste`, links cross-rota.
- **DoD**: 8 checks visíveis, cada um com Pass/Fail/Warn, link → sub-rota faz a ação.
- **Prioridade**: **P0** (Vercel Production Readiness é vendido como diferenciador; pode virar "LoopZero Readiness Score").

### J-22 — Secrets CRUD

- **Rotas**: `/projects/:name/secrets`
- **Estado atual**: ✅ página existe (`project-secrets.tsx`), hook `useSecrets`.
- **Visão Vercel-like**: tabela (Name, Last rotated, Env scopes), "Add secret" sheet (key + value + env multi-select), "Rotate" (replace value), "Delete" (com confirm destrutivo). Values sempre masked exceto durante create. Bulk import via .env file upload.
- **Backend deps**: cloud `/v1/projects/:name/secrets` CRUD (via cloud BFF que sabe de Stripe plan limits + audit log), engine `secret-store` + ESO sync para K8s Secrets no workload cluster.
- **Gap principal**: bulk .env upload não-existe, audit log de rotation não-exposto, confirm destrutivo no delete, env multi-select na add sheet.
- **DoD**: add secret via UI → restart pod → env var presente no container → rotate → restart → novo valor → delete → ausente.
- **Prioridade**: **P0** (qualquer app real precisa secrets; sem isso, LoopZero é demo).

### J-23 — Previews (per-PR ephemerais)

- **Rotas**: `/projects/:name/previews`
- **Estado atual**: ✅ página existe (`previews.tsx`), hook `usePreviews`. PreviewEnvironment CRD shipped em 2026-05-XX.
- **Visão Vercel-like**: tabela de previews ativos (PR #, branch, sha, status, URL pública, TTL restante, criador). Click → link público. "Delete" com confirm. Mostrar histórico de previews fechados (últimos 30d).
- **Backend deps**: `GET /api/v1/projects/:name/previews`, `DELETE /api/v1/projects/:name/previews/:id`. PR webhook do GitHub (T4.1 já wired) cria preview automaticamente; close PR limpa.
- **Gap principal**: link público nem sempre acessível em dev-public (precisa rota cert-manager LE), TTL countdown live, histórico de previews fechados.
- **DoD**: abrir PR no repo conectado → preview LIVE em <5min → click link → app responde → close PR → preview cleanup.
- **Prioridade**: **P0** (Vercel previews são killer feature; sem isso paridade é claudicante).

### J-24 — Configuration (loopzero.yaml editor + env config)

- **Rotas**: `/projects/:name/configuration` (NOVA)
- **Estado atual**: 🔴 página NÃO existe.
- **Visão Vercel-like**: editor YAML (Monaco) do `loopzero.yaml` atual do repo + diff contra HEAD, "Save" commita o YAML de volta via App token (PR para o repo OU commit direto se permission allows). Mostra environment variables build-time vs runtime. Framework override (auto-detect vs explicit).
- **Backend deps**: `GET /api/v1/projects/:name/config` (lê loopzero.yaml do último build), `PATCH /api/v1/projects/:name/config` (faz commit no repo via App token), validar YAML server-side antes do commit.
- **Gap principal**: página inteira não existe; backend não tem `/config` route.
- **DoD**: ler loopzero.yaml LIVE no editor → modificar `apps[].build` → save → commit aparece no GitHub → próximo push usa nova config.
- **Prioridade**: **P0** (sem isso, qualquer mudança de config requer terminal git; rompe a promessa Vercel-like).

### J-09 — Project Connect (repo integration) — ✅ COMPLETO 2026-06-06 (LIVE END-TO-END)

- **Rotas**: `/projects/:name/connect`
- **Estado atual**: install button + polling FSM + callback merge `?installation_id` + picker UI + payload canonical {repo, installation_id, branch, auto_deploy} + connected card. Plan 2026-06-05-dashboard-journey-j09-project-connect-plan.md 12/12 tasks engineering shipped.
- **Visão Vercel-like**: ✅ install GitHub App `LoopZero Platform` (ID 3319955, slug `loopzero-platform`), selecionar repo, branch pre-fillado do default_branch, POST `/connect` persiste `project_repositories`, push commit → release ACTIVE em <90s. GitLab/Bitbucket continuam P2.
- **Backend wired**:
  - T1.1 cloud install + callback (`/v1/integrations/github/install` + `/callback`) com 2 paths (state cookie + Setup URL flow)
  - T2.1 engine `/api/v1/integrations/github/installations/{id}/repositories` com cache 60s + tenant boundary
  - T3.1+T3.2 payload canonical + sqlc auto_deploy persistence
  - T4.1+T4.2 webhook handler com HMAC + counter labeled + correlation_id propagation
  - T5.1 loopzero.yaml validation no Connect handler
- **LIVE confirmado 2026-06-06 05:18 UTC**: push commit `457e31e` em `getloopscale/loopzero-repotest` → webhook (200) → auto-deploy triggered → source download → loopzero.yaml parsed (apps=2) → git bundle → MinIO → Argo Workflow `loopzero-build-build-3c02294c80a43599-gxlg7` → 14 steps Succeeded → build `build-3c02294c80a43599` SUCCEEDED → release `rel-e0cf879307b98900` **ACTIVE**.
- **DoD original**: instalar GitHub App em conta real → escolher repo → push commit → ArgoCD App aparece com release ACTIVE → ✅ TODOS atendidos LIVE.
- **Sessão dogfood**: 11 bugs LIVE catalogados + 11 commits. Padrões sistêmicos extraídos em memory `reference_j09_live_e2e_complete_2026_06_06`. Commits:
  - `fc676d24` caddy: route /v1/integrations/github/* on app-dev → cloud
  - `6fae48dc` feat(j09): wire SLUG/APP_PUBLIC_URL/AUTH_COOKIE_DOMAIN into loopzero-cloud chart
  - `31372570` fix(loopzero-cloud): pin ENGINE_API_URL canonical :3001
  - `8ac14210` fix(api): default /projects/{name}/status env to "production"
  - `d61350a6` fix(dashboard): null-guard ProjectHealthCard for projects without releases
  - `e409c7b7` fix(dashboard): merge ?installation_id query param into FSM state
  - `b289a04f` fix(dashboard): treat 404 NOT_CONNECTED as valid not-installed state
  - `56c5fdd1` fix(cloud): accept Setup URL callbacks without CSRF state cookie
  - `ee2af102` feat(j09): wire GITHUB_APP_ID via ExternalSecret + dev-public values
  - `5a26b542` fix(api): GET /projects/{name}/connect emits connected:true
  - `678c85d1` fix(caddy): route /api/v1/webhooks/github on cloud-dev to loopzero-api direct
- **Follow-ups conhecidos (P2)**:
  - Persistir ClusterRoleBinding `loopzero-build-sa-argo-admin` no chart `argo-workflows` (fix LIVE foi `kubectl apply` direto)
  - Persistir build.kubeconfig SA token rotation no helmfile sync (token TTL 24h)
  - GitHub App webhook UI no dashboard pra rotacionar secret sem mexer K8s
- **Prioridade**: ~P0~ → ✅ ENTREGUE

### J-10 — Environments ✅ DELIVERED (engineering, 2026-06-06)

- **Rotas**: `/environments`
- **Estado entregue**: refactor v1.0 → v1.1 — `PromoteDialog` canonical
  `{from, to}` body (fixed v1.0 buggy `{from_environment, to_environment}`
  silently ignored by backend), `DeleteEnvironmentDialog` NEW (cascade
  warning workloads/secrets/domains + exact-name type-to-confirm +
  Cancel default focus via ref + ARIA aria-describedby live region),
  `PlanLimitUpgradeModal` NEW (global 402 interceptor → window event
  `loopzero:plan-limit` → Crown icon + CTA `/billing`), `EnvironmentKPIRow`
  NEW (3 signals replicas + uptime % + last_deploy with sha truncated
  to 7, honest "—" for undefined).
- **Backend deps**: `engineFetch /api/v1/environments` CRUD, env-level
  secrets, promote handler — todos LIVE-validated.
- **DoD engineering**:
  - vitest 26/26 GREEN (5 spec files: use-environments, delete-env-dialog, promote-dialog, KPI, 402 modal)
  - Go promote handler 11 tests + 3 subtests GREEN (includes `RegressionV10BodyShape` pinning canonical body)
  - LIVE bash 6/6 scenarios A-F PASS (A create, B delete, C promote
    body shape, D KPI shape, E 402 enforcement, F concurrent
    idempotent under K8s optimistic-CC)
  - Dogfood wrapper `task test:dogfood:environments` 4/4 PASS
  - Baseline: `docs/operations/baselines/2026-06-06-j10-delivery-engineering.md`
- **DoD remaining (operator/sponsor HARD STOP)**:
  - Sponsor manual browser end-to-end at `https://app-dev.loopzero.example`:
    criar env "qa" → deploy → promote → 402 → delete cycle.
- **Lessons-learned**:
  - Pattern: 402 interceptor as global window event = every surface
    gets upgrade modal free without per-call wiring.
  - Pattern: handler-level body-shape regression test pinning the
    HTTP contract (prevents silent backend-ignores-bad-keys recurrence).
  - Pattern: scenario F "exists exactly once" outcome assertion is
    semantically richer than "first wins 201 + second 409" because
    K8s optimistic-CC + idempotent AddEnvironment IS valid by design.
- **Prioridade**: P1 → ✅ shipped.

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
WAVE 1 (P0 — autenticação + connect, ~2 semanas) — ENTREGUE PARCIALMENTE
  ✅ J-01 Login + J-02 Register + J-05 Device Success (RFC 8628 + 7 LIVE bugs)
  ✅ J-09 Connect Repo (push → release ACTIVE LIVE + 11 LIVE bugs)
  → J-06 Overview
  → J-07 Projects List (wizard agora destravado pelo J-09)
  → J-08 Project Detail Overview (umbrella, reduced scope)
  → J-13 Billing (revenue gate)

WAVE 2 (P0 — PRODUTO CORE, ~5-6 semanas) — sponsor decision 2026-06-06
  Sub-rotas do project detail viraram jornadas P0 individuais.
  Sem elas, o usuário não consegue usar LoopZero no dia-a-dia.

  J-17 Deployments List + Detail (deploy/cancel/rollback/redeploy)
  J-18 Status / Runtime Health (KPIs + history 7d + Grafana link)
  J-19 Runtime Logs Streaming (SSE + Loki query + .log download)
  J-20 Scale (slider replicas + HPA + cost preview)
  J-21 Readiness (production-grade checklist)
  J-22 Secrets CRUD (add/rotate/delete + .env bulk import)
  J-23 Previews (per-PR ephemeral envs)
  J-24 Configuration (loopzero.yaml editor + commit via App token) [NEW]

WAVE 3 (P1 — completude, ~3 semanas)
  J-03 Verification (UX polish)
  J-10 Environments (promote flow staging → prod)
  J-11 Domains (NS instructions polish)
  J-12 Team (invites + RBAC)
  J-15 Profile (API tokens)
  J-16 Settings General + Webhooks
  J-08-followup Build logs/SBOM/attestation sub-tabs (se J-17 deixou pendente)

WAVE 4 (P2 — polish, ~2 semanas)
  ✅ J-04 Recovery (delivered 2026-06-05)
  J-14 Audit export CSV + filter chips
  CC-05 Mobile responsive
  CC-07 i18n

WAVE 5 (cross-cutting validation)
  CC-01..CC-04 testes integrados em todas jornadas
  Full browser e2e suite (Playwright + dev-public)
  Performance budgets (Lighthouse ≥90 LCP/FID/CLS)
```

### Progresso atualizado 2026-06-06

| Jornada | Status | Notas |
|---------|--------|-------|
| J-01 Login | 🟡 funcional | polish pendente |
| J-02 Register | 🟡 funcional | polish pendente |
| J-04 Recovery | ✅ | 2026-06-05 LIVE |
| J-05 Device Success | ✅ | 2026-06-05 + 7 LIVE bugs |
| J-06 Overview | 🔴 não iniciado | depende de J-01 LIVE |
| J-07 Projects List | 🟡 lista existe; wizard pendente | J-09 disponível para wizard |
| J-08 Project Detail | 🟡 página existe; reduced scope | sub-rotas viraram J-17..J-24 |
| **J-09 Connect Repo** | **✅ LIVE END-TO-END** | 2026-06-06 + 11 LIVE bugs. Push → release ACTIVE <90s |
| **J-17 Deployments** | 🟡 página existe; ações pendentes | **P0 — deploy/cancel/rollback/redeploy** |
| **J-18 Status** | 🟡 página existe; history pendente | **P0 — debugging produtivo** |
| **J-19 Runtime Logs** | 🟡 página existe; LIVE-test pendente | **P0 — sem logs = produto inviável** |
| **J-20 Scale** | 🟡 página existe; cost preview pendente | **P0 — escalar é operação #2** |
| **J-21 Readiness** | 🟡 hook existe; UI checklist pendente | **P0 — LoopZero Readiness Score** |
| **J-22 Secrets** | 🟡 página existe; bulk import pendente | **P0 — sem secrets, LoopZero é demo** |
| **J-23 Previews** | 🟡 página existe; LIVE-test pendente | **P0 — Vercel parity** |
| **J-24 Configuration** | 🔴 NOVA — não existe | **P0 — editor loopzero.yaml + commit** |
| J-10 Environments | ✅ DELIVERED (eng) | engineering DoD complete 2026-06-06: PromoteDialog canonical {from,to} + DeleteEnvironmentDialog (cascade warning + type-to-confirm) + PlanLimitUpgradeModal (402 global) + EnvironmentKPIRow (3 signals). vitest 26/0 + Go promote handler 11+3 subtests + LIVE bash 6/6 (A create, B delete, C promote canonical body, D KPI shape, E 402 enforcement, F concurrent idempotent) + dogfood wrapper 4/4. Baseline `docs/operations/baselines/2026-06-06-j10-delivery-engineering.md`. Sponsor manual browser end-to-end = remaining HARD STOP. |
| J-11 Domains | ✅ funcional | polish (P1) |
| J-12 Team | 🟡 lista existe | invites pendente (P1) |
| J-13 Billing | 🟡 LIVE básico | revenue gate (P0) |
| J-14 Audit Log | 🟡 funcional | export pendente (P2) |
| J-15 Profile | 🟡 funcional | API tokens pendente (P1) |
| J-16 Settings | 🟡 parcial | (P1) |

### Why J-17..J-24 viraram P0 (decisão 2026-06-06)

A versão anterior do roadmap tratava essas 8 sub-rotas como "J-08 sub-actions" em WAVE 2 P1. **Erro de priorização**: depois de J-09 conectar o repo, o que o usuário FAZ no produto vive nessas 8 telas. Tratá-las como secundárias atrasa o produto core e força o usuário a fall-back para CLI em operações essenciais.

**Princípio adotado:** se a operação aparece como verbo no `loopzero` CLI E está documentada na "Quick reference" do README, ela é P0 no dashboard. Os 8 verbos cobertos:

| CLI verb | Dashboard journey |
|----------|-------------------|
| `loopzero deploy` | J-17 (Deployments) |
| `loopzero status` | J-18 (Status) |
| `loopzero logs` | J-19 (Runtime Logs) |
| `loopzero scale` | J-20 (Scale) |
| `loopzero secret set/list/delete` | J-22 (Secrets) |
| `loopzero preview list/inspect/delete` | J-23 (Previews) |
| `loopzero cancel`, `loopzero rollback` | J-17 (sub-tabs no Detail) |
| `loopzero validate` (parse loopzero.yaml) | J-24 (Configuration) |

J-21 Readiness não tem CLI 1:1 mas representa o "Vercel Production Readiness" diferenciador de marketing — vale virar P0 pelo posicionamento.

### Próximas jornadas habilitadas pelo J-09 completo

- **J-07 wizard**: `CreateProjectSheet` agora pode usar fluxo "name → /connect → install → pick → POST" provado funcional.
- **J-17 first deploy view**: depois de J-09, qualquer push em branch=main dispara build → release ACTIVE. J-17 só precisa exibir esse build/release que J-09 já gera + adicionar botões de ação.
- **CC-03 SSE**: o release ACTIVE é gravado no DB pelo convergence loop; J-17, J-18, J-19 podem escutar `release.activated` para auto-update sem reload.

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

### P0 jornadas ainda abertas (ordem sugerida para spawn-ar planos)

Já entregue: ✅ J-01 + J-02 + J-05 + J-09 + J-04 + J-17.

**Sequência recomendada (cada item destrava o seguinte para dogfood real):**

1. ✅ **J-17 Deployments List + Detail** — ENTREGUE 2026-06-06 LIVE-validated contra `admin@loopzero.example` em `app-dev.loopzero.example` (8/8 PASS T7.2 scenarios A/B/C/D). SSE timeline + LogsTab com 4-layer marker filter + tail-on-failure + RedeployDialog + filter chips status/env + Refresh + Load more pagination + Running-ghost fix. Dogfood wrapper `task test:dogfood:deployments`. Lições aprendidas registradas abaixo.
2. **J-18 Status / Runtime Health** — depois de ver o deploy, precisa ver se está saudável. Atrelado a J-17 mas pode rodar em paralelo.
3. **J-19 Runtime Logs Streaming** — quando deploy falha, log é o primeiro lugar onde olhar. P0 absoluto.
4. **J-22 Secrets CRUD** — sem secrets, apps reais não rodam. Pode ser feito em paralelo a J-17/18/19.
5. **J-17b sub-tabs (build logs/SBOM/attestation)** — depois de J-19 (que resolve runtime logs SSE), as build sub-tabs reaproveitam infra similar.
6. **J-20 Scale** — depois de ter deploy + status visíveis, ajustar replicas é a próxima ação natural.
7. **J-23 Previews** — Vercel killer feature; precisa de J-17 + J-19 funcionando.
8. **J-24 Configuration** — última P0; permite que usuário edite loopzero.yaml sem terminal.
9. **J-21 Readiness** — agrega sinais de TODAS as P0 anteriores; faz sentido vir por último.
10. **J-07 wizard + J-06 Overview + J-13 Billing** — as P0 não-Detail; podem ir em paralelo conforme J-17..J-19 destravam UI patterns.

Para cada uma:

```
/to-plan "Plan for dashboard journey J-XX <name>: <visão Vercel-like resumida>.
         Inputs: este roadmap, current page + hooks, CLI verbs paridade.
         Output: docs/plans/2026-06-XX-dashboard-journey-jXX-<slug>-plan.md
         com tasks granulares, DoD por task, edge-case-review."
```

Cada plano herda Cross-Cutting CC-01..CC-04 como acceptance criteria implícito + **DoD LIVE end-to-end em dev-public** (sem mock; pattern provado em J-04, J-05, J-09).

### Lessons-learned aplicáveis a TODAS as P0 (J-17..J-24)

1. **Drift 3-layer**: declare env vars / config nas 3 camadas (Go default + helm chart + helmfile per-env override). Sintoma característico: 404 com `duration_ms:0`. Memory `feedback_drift_3layer_canonical_pattern`.
2. **Backend↔TS contract**: campos numéricos/string que o backend pode omitir DEVEM ser `?` opcional no `lib/api/types.ts`, e o componente DEVE renderizar `—` quando undefined. Senão crash silent no first deploy. Memory `reference_j09_live_e2e_complete_2026_06_06` Padrão 2.
3. **HMAC-only edge routing**: qualquer endpoint que recebe POST com signature header (webhooks, callbacks externos) deve ser roteado DIRETO de Caddy/CF para loopzero-api, sem passar pelo loopzero-cloud JWT proxy. Memory `feedback_webhook_edge_boundary_pattern`.
4. **Schema vs runtime**: backend pode retornar 404 com payload válido (ex: `NOT_CONNECTED`). Normalize na borda do frontend (`engineFetch` null → `{empty:true}`) em vez de propagar `null` que vira FSM stuck. Memory `reference_j09_live_e2e_complete_2026_06_06` Padrão 5.
5. **Playwright headless contra dev-public**: vitest+jsdom abstrai protocolos cookie/CSP/cross-origin que só aparecem em LIVE. Memory `reference_kratos_browser_mode_422_redirect_2026_06_05`.

### Lessons-learned J-17 Deployments (2026-06-06) — pragmáticas para J-18..J-24

Patterns transferíveis para qualquer journey que combina **lifecycle realtime + listagem paginada + sub-views detalhadas**. Cada lição é estruturada como **Lesson → Why → Apply**.

#### Arquitetura

1. **Marker-based log filtering (4-layer)** — Quando logs precisam ser categorizados E filtrados, NÃO crie esquema custom de pubsub. Use markers textuais (`::loopzero:: phase=X step=Y level=Z msg="..."`) que (a) Promtail extrai como labels indexáveis, (b) LogQL filtra server-side via label, (c) Frontend strip o prefixo via regex de 1 linha. Pattern grep-friendly + zero JSON parsing em bash + não vaza versão (CVE targeting surface).
   - **Why**: vimos que o usuário NÃO quer ver `Starting Workflow Executor argo=true version=v4.0.3 goVersion=go1.22.3` no LogsTab. Markers separam "user-curated" de "operator debug" com 1 LogQL switch.
   - **Apply**: para J-19 Runtime Logs Streaming, replicar o mesmo pattern em runtime app logs (app emite `::loopzero:: phase=runtime msg="..."` opcional; debug mode mostra raw). Promtail config canonical em `infra/helm/promtail/values.yaml.gotmpl`.

2. **Terminal status é absorvente em qualquer stream reducer** — Quando um reducer Frontend combina REST snapshot + SSE replay, estados terminais (`succeeded`, `failed`, `cancelled`) NUNCA podem regredir para `running`. SSE replay é potencialmente fora de ordem (FIFO cap) e parcial.
   - **Why**: bug "Running ghost" — pílula Running aparecia em rows de builds SUCCEEDED por causa de `build.started{X}` antigo retransmitido quando o `build.succeeded{X}` pair já tinha sido evicted do buffer.
   - **Apply**: em qualquer lifecycle UI (J-18 health probes, J-19 log levels, J-20 scale state, J-21 readiness gates) implementar reducer que **filtra eventos não-monotônicos** antes de aplicar. Pattern canonical em `cloud/dashboard/src/hooks/use-deployments-list-stream.ts` (TERMINAL_STATUSES set + early continue).

3. **useEffect deps precisam refletir transições, não só IDs** — `[id]` não é suficiente quando o CONTEÚDO muda BETWEEN id changes (build roda → termina, novo log chunk chega após terminal). Adicione a deriva de estado (`isTerminal`, `isHealthy`, `isReady`) como dep.
   - **Why**: LogsTab nunca re-fetchava após build SUCCEEDED, mostrando snapshot parcial dos logs (logs continuam chegando ao Loki APÓS o pod terminar e o frontend não pegava).
   - **Apply**: em J-18 (health flapping), J-19 (log stream connection lifecycle), J-20 (scale converged?), adicione **derived state na dep array**. Pattern em `logs-tab.tsx` `useEffect[id, isTerminal]`.

4. **Empty failure logs são bug de produto, não feature** — `failed (exit 1)` sem contexto = unusable. Falha SEMPRE deve capturar stdout/stderr e replay no user view.
   - **Why**: sponsor verbatim "se aconteceu um erro no build ele vai logar?". Falha sem contexto força operator a re-rodar manualmente para ver o erro real.
   - **Apply**: em J-19 runtime logs, capturar últimas N linhas do crash via `tee` no entrypoint (não confiar só no kubelet `terminationMessagePolicy`). Pattern em `api/internal/argo/loopzero_log.go` `wrapWithLoopZeroMarkers` (tail -50 + sed escape `"` + `\` + truncate 400 chars/linha).

5. **REST + SSE coexistem; Refresh manual é obrigatório** — Realtime via SSE não dispensa botão Refresh. User precisa de controle explícito para forçar re-snapshot independente do stream (backfill out-of-band, dúvida sobre staleness, debugging).
   - **Why**: sponsor pediu o botão explicitamente. SSE pode estar desconectada silenciosamente; sem Refresh manual, user perde confiança em "ver o estado real".
   - **Apply**: TODA página que usa SSE (J-18, J-19, J-20, J-21, J-23) DEVE expor Refresh button visual com ícone girando durante `isFetching`, disabled durante in-flight. Pattern em `deployments.tsx` (RefreshCw + animate-spin).

6. **Backend dirige UX paginação desde dia 1** — Endpoint que retorna lista DEVE retornar `total` no response shape. UI com "Showing N of M" + "Load more" custa 30 LOC se backend já tem `?limit=` + `total`; vira refactor se backend só retorna array.
   - **Why**: sem `total`, frontend não sabe se há mais (load more vira chute) e não sabe quando esconder o button. Sponsor preocupação "milhares de builds" foi resolvida em 1 commit porque backend já tinha `total`.
   - **Apply**: em J-18 (health history), J-19 (log archive), J-22 (secrets), J-23 (previews), J-14 (audit), garantir response shape `{items: [], total: N}` server-side. Cap em 50/request, "Load more" bumps cliente. Pattern em `useDeployments` + `build_list.go:71`.

#### Operacional

7. **LIVE bash test é a ÚNICA defesa contra regressão de infra de SSE/Caddy/Proxy** — vitest+jsdom não valida: Caddy SSE buffering, BFF io.Copy semantics, JWT TTL real, cross-cluster routing, Cookie/CSP/cross-origin. **Cada P0 journey precisa ter um `infra/tests/e2e/dev-public-<journey>-flow_test.sh`** com pelo menos um scenario que abre SSE/cookie real e assert eventos chegam em <Ns.
   - **Why**: dessa sessão: o bug do BFF io.Copy buffering que quebrou SSE no Vite proxy NUNCA seria caught por vitest. Layer 1+2+3 markers só apareceram corretamente no Loki — sem o scenario C do T7.2, regressão silenciosa.
   - **Apply**: template canonical em `infra/tests/e2e/dev-public-deployments-flow_test.sh` (4 scenarios A/B/C/D + companion hermetic). Para cada J-XX, criar `dev-public-jXX-flow_test.sh` + `check-dev-public-jXX-flow-syntax_test.sh` + 2 entries no allowlist.

8. **Dogfood wrapper aggregator é o vetor canonical do projeto** — Pattern unificado `task test:dogfood:<journey>` que agrega hermetic syntax + vitest + Go + LIVE bash em 1 comando. Operator/CI roda um único comando antes de release.
   - **Why**: J-04 + J-05 + J-09 + J-17 todos seguem o mesmo template; manter consistência reduz cognitive load + previne "esqueci de rodar layer X".
   - **Apply**: template em `infra/scripts/local/dogfood-deployments.sh` (3 flags: `--hermetic-only`/`--live-only`/`--json`). Wire em `taskfiles/test.yml` como `test:dogfood:<journey>`. Para J-18..J-24, replicar 1:1.

9. **Tilt manual mode pode travar — bypass via docker build + k3d image import** — Quando Tilt fica `updateStatus: pending` por horas/dias, `tilt trigger <resource>` pode não iniciar o build. Bypass: `docker build -f <chart>/Dockerfile -t loopzero-X:tag-N .` → `k3d image import loopzero-X:tag-N -c loopzero-mgmt` → `kubectl set image deploy/loopzero-X loopzero-X=loopzero-X:tag-N`.
   - **Why**: Dessa sessão Tilt ficou `pending` desde 2026-06-04 e `tilt trigger` não destravava. Bypass deployou j17-iter7 em 60s.
   - **Apply**: documentar no runbook de dev-loop. Especialmente importante para iter rápido entre code change e LIVE validation.

10. **Cloud-dev vs app-dev host mapping** — Não assumir host por convenção. `cloud-dev.loopzero.example` retornou 404 em toda rota nessa env; `app-dev.loopzero.example` é o host real do BFF + dashboard. **Confirme via curl antes de qualquer test LIVE**.
    - **Why**: dessa sessão, T7.2 default `CLOUD_URL=cloud-dev` quebrou; mudar para `app-dev` deu 8/8 PASS.
    - **Apply**: env var `CLOUD_URL` SEMPRE override-able no test bash. Documentar mapping canonical em `docs/operations/dev-public-hosts.md` (a criar): "dashboard + BFF + engine proxy → `app-dev.loopzero.example`; auth (Kratos+Hydra) → `auth-dev.loopzero.example`; runtime apps → `*.loopzero.live` ou similar".

#### Processo

11. **JWT TTL ~12-60min — testes LIVE devem aceitar injeção, não re-mintar** — Scripts hermetic devem ler JWT de variável de ambiente OU arquivo `~/.loopzero/local-dogfood-token.txt`. Re-mint via Hydra dentro do script é fragile (Hydra port-forward, secret namespace asymmetry, audience mismatch).
    - **Why**: dessa sessão, o token sponsor expira em 12min; tive que copiar para `/tmp` + apontar HOME=fakeroot para rodar T7.2 sem sobrescrever local seed.
    - **Apply**: TODO test LIVE deve documentar `JWT_PATH=~/.loopzero/<env>-token.txt` env var. Para validação com sessão real do sponsor: instruir extração via DevTools (`Authorization: Bearer <token>` header de qualquer XHR).

12. **Sponsor scope-extension verbatim approval** — Quando governance-gate bloqueia edit cross-domain (api/ + cloud/ + infra/ na mesma feature), NÃO criar bypass alternativo. Estender escopo em `.claude/governance-gate.local.md` citando verbatim a autorização do sponsor + rationale.
    - **Why**: cross-cutting é a natureza de UI realtime; bypass duplica lógica canonical (ex: BuildStore lookup em cmd vs adapters). Memory `feedback_sponsor_scope_extension_pattern`.
    - **Apply**: padrão consolidado de J-09 + J-17. Para J-18..J-24, esperar mesma necessidade (cada journey toca backend + cloud BFF + frontend).

---

## 7. Open Questions (resolver antes de spawn-ar planos individuais)

(seção 7 abaixo da seção 8 propositadamente — 8 é o roadmap de ação, 7 são as decisões pendentes que só precisam ser resolvidas se algum plano específico esbarrar nelas)

---

**Status:** Roadmap **EM EXECUÇÃO** (J-09 entregue 2026-06-06; sponsor decision 2026-06-06 promoveu J-17..J-24 a P0; WAVE 2 redefinida; **J-17 entregue 2026-06-06 LIVE-validated 8/8 PASS** contra dev-public real — lessons-learned J-17 registrados na §8).
**Audit trail:** este file commitado em `docs/handoff/`, referenciável por cada plano individual via `[[2026-06-05-dashboard-vercel-like-integration-roadmap]]`.
