# Fase 9 — Dashboard (Next.js)

> Web dashboard em `app.loopzero.example` com login, projetos, deploys, e billing.

**Dependências:** Fases 1-6 completas (auth + billing funcionais via CLI).
**Estimativa:** 40-60h (fase mais longa — é um produto web completo)
**Risco:** ALTO — scope creep. Definir MVP rígido.

---

## Contexto

O dashboard é a interface web do LoopZero. Usa PKCE flow para auth (não Device Code — browsers usam redirect, não polling). Consome a mesma API que o CLI (api.loopzero.example).

MVP: login/signup, listar projetos, ver deploys, ver logs, billing portal. Não precisa fazer deploy pelo dashboard (CLI-first).

Deploy do dashboard: via LoopZero mesmo (dogfooding) — é um static frontend, vai para DO Spaces + CF Worker.

---

## Tasks

### T9.1 — Setup Next.js + Tailwind

**O que fazer:**
- Criar `cloud/dashboard/` com Next.js 15 (App Router) + Tailwind CSS
- TypeScript strict mode
- ESLint + Prettier configurados
- Estrutura: `src/app/`, `src/components/`, `src/lib/`
- Deploy: static export (`next export`) → LoopZero static delivery pipeline

**DoD:**
- [ ] `cloud/dashboard/` existe e builda
- [ ] `npm run build` produz static export
- [ ] `npm run dev` roda local na porta 3000
- [ ] TypeScript strict, ESLint configurado
- [ ] Deploy via `loopzero deploy` funciona (loopzero.yaml configurado)

### T9.2 — Auth: PKCE flow

**O que fazer:**
- Login page: `app.loopzero.example/login`
- PKCE flow contra Ory Hydra:
  1. Gera code_verifier + code_challenge
  2. Redirect para Hydra `/oauth2/auth`
  3. Usuário faz login (Kratos self-service UI)
  4. Callback com auth code → troca por JWT
  5. JWT armazenado em HttpOnly cookie (secure, SameSite=Strict)
- Signup: redirect para Kratos registration flow
- Logout: revoke token + clear cookie

**DoD:**
- [ ] Login via PKCE funcional
- [ ] Signup redirect funcional
- [ ] JWT em HttpOnly cookie (não localStorage)
- [ ] Logout limpa sessão
- [ ] Social login (GitHub/Google) funciona no browser
- [ ] Testes: login flow completo

### T9.3 — Pages: Projects

**O que fazer:**
- `/projects` — lista de projetos (GET /v1/projects)
- `/projects/[name]` — detalhe do projeto (GET /v1/projects/{name}/status)
  - Mostra: apps, último deploy, URL de cada app, status
- `/projects/[name]/builds` — histórico de builds
- `/projects/[name]/builds/[id]` — detalhe do build (logs inline)

**DoD:**
- [ ] Listagem de projetos funcional
- [ ] Detalhe com status de cada app
- [ ] Histórico de builds com filtro por env
- [ ] Build detail com logs (consumindo Loki via API)
- [ ] Loading states, error states, empty states

### T9.4 — Pages: Environments

**O que fazer:**
- `/environments` — lista de environments
- `/environments/[name]` — detalhe: active releases por projeto, status

**DoD:**
- [ ] Listagem funcional
- [ ] Detalhe com releases ativas
- [ ] Link para projeto de cada release

### T9.5 — Pages: Team

**O que fazer:**
- `/team` — lista de membros com roles
- Ações: invite member, remove member, change role
- Proteções: owner não pode ser removido, owner-only role changes

**DoD:**
- [ ] Listagem de membros com roles
- [ ] Invite funcional (input email/GitHub username)
- [ ] Remove funcional (com confirmação)
- [ ] Role change funcional (owner only)
- [ ] Mesmas proteções do CLI

### T9.6 — Pages: Billing

**O que fazer:**
- `/billing` — mostra plano atual, uso, seats
- Botão "Manage Subscription" → Stripe Customer Portal
- Botão "Upgrade" → Stripe Checkout (para plano selecionado)
- Mostra: builds usados/limite, storage, próxima invoice estimada

**DoD:**
- [ ] Plano e uso exibidos corretamente
- [ ] Redirect para Stripe Portal funciona
- [ ] Redirect para Stripe Checkout funciona
- [ ] Estimativa de invoice mostrada

### T9.7 — Pages: Settings

**O que fazer:**
- `/settings` — configurações do tenant
  - Nome do tenant
  - Email do owner
  - Danger zone: delete tenant
- `/settings/secrets` — gerenciamento de secrets por environment

**DoD:**
- [ ] Settings page funcional
- [ ] Delete tenant com dupla confirmação
- [ ] Secrets listados (valores nunca expostos)

### T9.8 — Pulumi: deploy dashboard

**O que fazer:**
- Configurar `cloud/dashboard/loopzero.yaml` para deploy como static frontend
- Domínio: `app.loopzero.example`
- CF Worker route para `app.loopzero.example/*`
- Pulumi config para DNS record

**DoD:**
- [ ] `loopzero deploy` no dashboard funciona
- [ ] `app.loopzero.example` acessível com HTTPS
- [ ] CF Worker serve o dashboard
- [ ] DNS configurado via Pulumi

---

## MVP Scope (o que NÃO entra na Fase 9)

- Deploy pelo dashboard (CLI-first — deploy é pelo CLI)
- Rollback pelo dashboard
- Secret creation pelo dashboard (CLI-first)
- Real-time build streaming (polling é suficiente para MVP)
- Dark mode (YAGNI)
- Mobile responsive (desktop-first para dev tools)
- Notifications (email, slack)
- Audit log viewer

---

## Definition of Done — Fase 9

- [x] Next.js 15 scaffold — package.json, tsconfig, tailwind, postcss, next.config (static export)
- [x] PKCE auth flow — src/lib/auth.ts (startPKCEFlow, exchangeCode, S256 challenge)
- [x] API client — src/lib/api.ts (token from cookie, auto-redirect on 401)
- [x] Login page with PKCE + CLI fallback
- [x] Projects page — list from API, empty state, loading
- [x] Environments page — list with status badges
- [x] Team page — member list with roles
- [x] Billing page — plan, usage breakdown, manage plan (portal redirect)
- [x] Settings page — CLI-first guidance, danger zone
- [x] loopzero.yaml for LoopZero deploy (dogfooding)
- [x] Zero internal terms in user-facing content
- [x] `next build` passes — 8 pages exported (1.1MB), 354 deps installed in 25s (Node 20 + legacy-peer-deps)
- [ ] `app.loopzero.example` accessible with HTTPS — requires Pulumi DNS + CF Worker config (operational)
- [ ] E2E tests — deferred to post-deploy iteration (MVP scope)
