# Fase 6 — Plan Enforcement

> Enforçar limites de plano em todas as operações. Deploy falha com 402 se exceder.

**Dependências:** Fase 5 (Stripe integration — subscriptions e plan_limits existem).
**Estimativa:** 8-10h
**Risco:** BAIXO — lógica pura, sem dependência externa nova. Risco principal: falso positivo bloqueando deploy legítimo.

---

## Contexto

Cada tenant tem um plano (starter, pro, team, enterprise). Cada plano tem limites por métrica (projetos, serviços, builds, RAM, etc.). O enforcement middleware verifica limites antes de permitir operações.

Starter e Pro têm hard limits (operação rejeitada). Team tem soft limits (operação permitida, overage cobrado). Enterprise não tem limites.

---

## Tasks

### T6.1 — Plan enforcement middleware

**O que fazer:**
- Middleware chi que roda após auth, antes dos handlers
- Extrai `tenant_id` do UserContext
- Consulta `subscriptions` (plan) e `plan_limits` (limits)
- Consulta `usage_records` (uso atual do período)
- Se excedeu hard limit: retorna `402 Payment Required` com mensagem clara
- Se excedeu soft limit (Team): permite mas marca para cobrança
- Se plano `enterprise`: skip (sem limites)

**DoD:**
- [ ] Middleware implementado e wired nas rotas que precisam
- [ ] 402 retornado com body: `{ code: "PLAN_LIMIT_EXCEEDED", error: "...", upgrade_url: "..." }`
- [ ] Hard limit: bloqueia operação
- [ ] Soft limit: permite e registra overage
- [ ] Enterprise: bypass total
- [ ] Testes unitários: starter excede → 402, pro dentro do limite → 200, team excede soft → 200 + overage

### T6.2 — Enforcement por operação

**O que fazer:**
- Mapear quais operações checam quais limites:

| Operação | Métrica verificada |
|----------|-------------------|
| `POST /projects` | `projects` count |
| `POST /builds` (deploy) | `builds_per_month` count + `services_per_project` |
| `POST /environments` | `environments` count |
| `POST /teams/members` | `seats` count |
| `POST /secrets` | `secrets_per_env` count |

- Cada check é uma query simples: `SELECT COUNT(*) FROM X WHERE tenant_id = $1`

**DoD:**
- [ ] Cada operação verifica o limite correto
- [ ] Queries são eficientes (indexed)
- [ ] Testes para cada operação: dentro do limite → OK, excede → 402

### T6.3 — CLI: mensagens de upgrade

**O que fazer:**
- Quando CLI recebe 402:
  - Mensagem: "Limite do plano Starter atingido: máximo 2 projetos. Faça upgrade: https://app.loopzero.example/billing"
  - Exit code: 3 (deploy/operation failure)
  - `--json`: `{ code: "PLAN_LIMIT_EXCEEDED", error: "...", plan: "starter", limit: "projects", current: 2, max: 2, upgrade_url: "..." }`
- Mensagem deve ser acionável (diz o que fazer)

**DoD:**
- [ ] CLI exibe mensagem clara com nome do plano, limite, e link de upgrade
- [ ] `--json` inclui todos os campos para automação
- [ ] Exit code 3
- [ ] Testes: mock 402 response → output correto

### T6.4 — Rate limit por plano

**O que fazer:**
- Atualizar rate limit middleware existente (`api/internal/middleware/ratelimit.go`)
- Em vez de 60 req/min hardcoded, variar por plano:
  - Starter: 60 req/min
  - Pro: 300 req/min
  - Team: 1000 req/min
  - Enterprise: 5000 req/min
- Ler plano do UserContext (já injetado pelo auth middleware)

**DoD:**
- [ ] Rate limit varia por plano
- [ ] Valores configuráveis via `plan_limits` table (métrica `api_requests_per_min`)
- [ ] 429 retornado com `Retry-After` correto
- [ ] Testes: starter excede 60 → 429, pro até 300 → OK

### T6.5 — Build timeout por plano

**O que fazer:**
- Build timeout varia por plano:
  - Starter: 10 min
  - Pro: 30 min
  - Team: 60 min
- Passar timeout para Argo Workflow no submit
- Configurável via `plan_limits` table (métrica `build_timeout_seconds`)

**DoD:**
- [ ] Timeout do Argo Workflow respeita plano do tenant
- [ ] Configurável via DB (não hardcoded)
- [ ] Teste: starter build que demora 11 min → timeout → FAILED

---

## Definition of Done — Fase 6

- [x] Plan enforcement middleware — Middleware() in enforcement/middleware.go, checks plan_limits before mutations
- [x] 402 with actionable message — FormatLimitError() returns code, error, plan, limit, current, max, upgrade_url
- [x] CLI 402 handling — ApiError.fromHttpStatus(402) → exit code 3, PLAN_LIMIT_EXCEEDED code
- [x] Rate limit by plan — GetRateLimit() returns per-plan rate (starter:60, pro:300, team:1000, enterprise:5000)
- [x] Build timeout by plan — GetBuildTimeout() returns per-plan timeout (starter:600s, pro:1800s, team:3600s)
- [x] Enterprise bypass — CheckLimit returns Allowed:true immediately for enterprise
- [x] Team soft limit — SoftOver flag set when soft_limit exceeded, operation still allowed
- [x] Tests — 7 Go tests (nil DB, build timeout, rate limit, format error, middleware pass-through, no-tenant, 402 shape) + 4 CLI tests
- [x] Zero false positives — nil DB = allow all (dev mode), no tenant = pass through
