# Backend Pre-flight Report — Dashboard FAANG-Tier 3 Phase 0

**Data:** 2026-05-25 (executado em Ralph loop autonomous mode)
**Plan:** `docs/plans/2026-05-26-dashboard-faang-tier-3-plan.md` v1.1
**Phase:** 0 — Backend Pre-flight (T-DR-0.1..0.6)
**Methodology:** read-only verification via `grep -rn` em `api/internal/{routes,services,sse,server}/`. SSE k3d local não disponível esta iteração — verifications pure-source (sem `curl` smoke).
**Status:** COMPLETE — 5 verdicts documentados, 0 commits consumer-side.

---

## Resumo executivo

| Task | Endpoint/Feature | Verdict | Decision para Phase 1 task afetada |
|------|------------------|---------|-----------------------------------|
| T-DR-0.1 | `/v1/billing.payment_method.last4` | **RESCOPE** | T-DR-1.3 escopa apenas quota lines (no Payment Method preview) |
| T-DR-0.2 | `/v1/workspace` endpoints (Timezone, Default Env, Notifications) | **RESCOPE** | T-DR-1.4 (parte Workspace) vira "Coming Soon" placeholder OR removido — CLI-only per DSS § 9.1 |
| T-DR-0.3 | `/v1/api-keys` endpoints (rotation/list/create) | **RESCOPE** | T-DR-1.4 (parte API Keys) removido — CLI-only per DSS § 9.1 |
| T-DR-0.4 | SSE audit_log endpoint | **READY (REST only)** | T-DR-3.4 wire REST polling 30s; sem real-time SSE |
| T-DR-0.5 | SSE status updates (Application/Release) | **RESCOPE** | T-DR-3.9 defer para v1.1 OR escopo limitado a refresh polling |

**Cross-cutting decision:** 4/5 verdicts RESCOPE confirma a tese do edge-case-plan review — **backend-prep gap sistêmico**. Sem Phase 0, Phase 1+ teria shipado 4 cards/features funcionalmente vazios.

---

## T-DR-0.1 — Payment Method last4 backend endpoint

### Verification

```bash
grep -rn "payment_method\|last4" api/internal/services/ api/internal/routes/
# → ZERO hits
```

`api/internal/routes/billing.go` linha 71-75:

```go
type BillingResponse struct {
    Plan  string                       `json:"plan"`
    Team  string                       `json:"team"`
    Usage map[string]BillingUsageEntry `json:"usage"`
}
```

### Verdict: **RESCOPE**

- Backend `BillingResponse` shape é estritamente `{plan, team, usage}`
- Sem `payment_method`, sem `period_start`, sem `period_end`, sem `last4`, sem `next_billing_date`
- Frontend `billing.tsx` declara `period_start?` / `period_end?` optional — mas backend NÃO retorna (frontend usa fallback "Current period")

### Decisão para T-DR-1.3 (Billing Plan card enrichment)

T-DR-1.3 v1.1 escopa apenas:
- ✅ Remover `font-bold` redundante (L212, slop T-06 fail) — não-blocker
- ✅ Enrich Plan card com 3-line quota summary inline (data já em `usage` map)
- ❌ **REMOVED:** Payment Method preview (last4) — backend não suporta
- ❌ **REMOVED:** Next billing date display — backend não retorna `period_end`

**Negotiation alternative:** backend PR ~1-2 days adicionando Stripe `customer.subscription.current_period_end` + `customer.payment_method.card.last4` à `BillingResponse`. Caller dispatch via Stripe SDK already exists (per CHANGELOG.md "POST /v1/billing/portal"). **NOT pursued autonomously** — requires backend team sign-off.

---

## T-DR-0.2 — Workspace Settings endpoints

### Verification

```bash
grep -rn "workspace\|timezone\|notification_pref\|default_environment" api/internal/routes/ api/internal/services/
# → Hits são todos sobre BuildKit /workspace path (não relacionado a user prefs)
```

### Verdict: **RESCOPE**

- Zero endpoints `/v1/workspace`, `/v1/workspace/timezone`, `/v1/workspace/notifications`, `/v1/workspace/default_environment`
- BuildKit hits são sobre filesystem workspace para builds — não user/tenant preferences

### Decisão para T-DR-1.4 (Settings Workspace card)

T-DR-1.4 (parte Workspace Settings):
- ❌ **REMOVED:** Workspace Settings card inteiro
- **Rationale per DSS § 9.1 CLI-first:** preferences são CLI domain (`loopzero config set timezone=...`). Adicionar GUI mid-pre-release sem backend é placeholder waste.
- **Mitigation para P2-2 (Settings sparseness density):** T-DR-1.4 escopa apenas mais 1 card alternativo: **"Connected integrations"** (read-only display de Cloudflare/Stripe/GitHub state, dados já fluem) OR **defer P2-2 fix** documentando que Settings page é intencionalmente sparse pre-launch (CLI-first).

**Negotiation alternative:** backend PR ~2 days criando CRUD `/v1/workspace/{timezone,default_env,notifications}`. **NOT pursued** — fora de roadmap backend.

---

## T-DR-0.3 — API Keys endpoints

### Verification

```bash
grep -rn "api[-_]key\|apikey\|ApiKey\|/v1/api-keys\|/v1/keys" api/internal/routes/ api/internal/services/
# → ZERO hits (JWT-related matches excluded)
```

### Verdict: **RESCOPE**

- Zero endpoints `/v1/api-keys`, `/v1/keys`, `/v1/auth/api-keys`
- Authentication é JWT-only (RSA-only per CHANGELOG.md) — sem rotation API

### Decisão para T-DR-1.4 (Settings API Keys card)

T-DR-1.4 (parte API Keys):
- ❌ **REMOVED:** API Keys card inteiro
- **Rationale:** API Keys = future feature (não roadmap atual). Authentication atual = JWT via session.

**Negotiation alternative:** backend PR ~3 days (auth model + rotation + 1-time-display + audit log). **NOT pursued** — major feature, requires PM sign-off + ADR.

---

## T-DR-0.4 — SSE para audit_log (Notification Center source)

### Verification

```bash
grep -rn "audit.*sse\|sse.*audit\|audit_log.*stream" api/internal/sse/ api/internal/routes/
# → ZERO hits

grep -rn "audit_log\|/v1/audit\|AuditHandler" api/internal/routes/ api/internal/server/
# → api/internal/routes/audit.go: GET /api/v1/audit (REST only, filterable + paginated)
# → audit.go:35: "GET /api/v1/audit?action=...&resource_type=...&since=...&limit=...&offset=..."
```

### Verdict: **READY (REST only, no SSE)**

- `/api/v1/audit` GET endpoint existe + funcional (per CHANGELOG.md "audit log filterable")
- SSE existe APENAS para builds (`/api/v1/builds/{id}/stream`) + logs (`/api/v1/builds/{id}/logs/stream`, `/api/v1/projects/{name}/logs/stream`)
- Audit log NÃO tem SSE multiplex — tabela `sse_events` é build-scoped (`build_id` FK)

### Decisão para T-DR-3.4 (Notification Center)

T-DR-3.4 v1.1 escopa:
- ✅ Notification Center wire **REST polling 30s** contra `/api/v1/audit?since=$now-1h&limit=20`
- ❌ Real-time SSE updates (no infrastructure backend)
- **Mitigation UX:** "Last refreshed Nm ago" indicator + manual refresh button
- **Trade-off accepted (EC-??):** 30s polling latency vs real-time is acceptable for notification bell (not deploy/build live status); aligns com Vercel benchmark pattern (notification bell polling, not push)

**Negotiation alternative:** backend PR ~2 days adding SSE multiplex à audit_log (separate sse_audit_events table tenant-scoped). **NOT pursued** — REST polling cobre 90% UX value at zero backend cost.

---

## T-DR-0.5 — SSE para live status updates (Application/Release badges)

### Verification

```bash
grep -rn "status.*sse\|application.status.*stream" api/internal/sse/ api/internal/routes/
# → ZERO hits
```

`api/internal/sse/event_store.go`:
- `sse_events` table = build-scoped only (`build_id` FK, line 62)
- Event types existentes: `build.started`, `build.completed`, `deploy.*`, `rollout.*` (per `stream_handler.go`)

SSE routes registered:
- `/api/v1/builds/{id}/stream` (build events)
- `/api/v1/builds/{id}/logs/stream` (build logs)
- `/api/v1/projects/{name}/logs/stream` (runtime logs)

### Verdict: **RESCOPE**

- Zero infra SSE para Application/Release status changes outside deploy context
- Status badges em /projects, /domains, /environments hoje mostram snapshot (last refresh), NÃO live updates
- Backend PR para adicionar tenant-scoped status SSE = ~1-2 weeks (new table, status emit hooks em Application/Release state machines, multi-tenant filter)

### Decisão para T-DR-3.9 (SSE live status badges)

T-DR-3.9 v1.1 **defer para v1.1 pós-GA** OR escopo limitado a:
- ✅ Status badges atualizam em **refresh do user** (já é o pattern atual)
- ✅ Auto-refresh 60s (silencioso, sem indicador visual) — pode usar React Query refetchInterval
- ❌ True real-time SSE — descontinuado deste plan

**Mitigation UX:** página exibe "Last updated Ns ago" sutil no canto da TopBar (similar a Vercel pattern). 60s refresh é "feels close to real-time" sem backend work.

**Negotiation alternative:** backend PR ~1-2 weeks adicionando tenant-scoped status SSE infrastructure. **NOT pursued autonomously** — escopo grande, prioridade dependente de produto.

---

## T-DR-0.6 — Phase 0 closure summary

### Impacto consolidado no plan v1.1

| Phase 1 task | Pre-Phase-0 scope | Post-Phase-0 scope | Delta |
|--------------|-------------------|---------------------|-------|
| T-DR-1.3 (Billing) | Plan card + Payment Method + next billing | Plan card quota only (no payment, no next billing) | −60% scope |
| T-DR-1.4 (Settings) | Workspace + API Keys cards | "Connected integrations" card only OR defer P2-2 | −80% scope |
| T-DR-3.4 (Notification) | SSE real-time | REST polling 30s | UX OK, infra mudou |
| T-DR-3.9 (Live status) | SSE wire | 60s polling refresh | Defer real-time |

**Net result:** 0 tasks blocked, 4 tasks rescoped. Phase 1 pode prosseguir com scopes ajustados após sponsor ratification dos rescopes.

### Gates met (per Plan v1.1 Phase 0 DoD)

- [x] 5 verdicts documented
- [x] 0 NEGOTIATE PRs blocking (all RESCOPE decisions documented; sem backend PRs em andamento)
- [ ] **PENDING:** Sponsor explicit ratification dos 4 rescope decisions (T-DR-1.3, 1.4, 3.4, 3.9)
- [ ] **PENDING:** T-DR-1.3, 1.4, 3.4, 3.9 specs atualizadas no plan v1.1 → v1.2 com scope final
- [ ] **PENDING:** Sponsor explicit go-ahead para Phase 1

### Recommendation

**Pause antes de Phase 1.** Mesmo com Phase 0 read-only complete:
- 4 tasks afetadas por rescope precisam de decisão sponsor (não tem default safe — RESCOPE pode ser "defer feature inteiro" OR "minimum viable card")
- Sem ratification, Phase 1 commit faz scope creep (implementa rescope sem sponsor sign-off)

**Próxima ação requerida:** sponsor review este report + decide per rescope:
- T-DR-1.3 → enrich quota lines only (preserva P2-1 close), OR drop T-DR-1.3 inteiro (P2-1 não closed)
- T-DR-1.4 → "Connected integrations" card alternativo (closes P2-2 alt), OR defer P2-2 (documentado CLI-first)
- T-DR-3.4 → REST polling 30s (closes V-5), OR defer V-5
- T-DR-3.9 → 60s polling refresh (no live SSE), OR defer V-status-realtime

---

## Verification trail (reproducible)

All commands rodadas em `/home/paulo/Projetos/loopzero/loopzero`:

```bash
# T-DR-0.1
grep -rn "payment_method\|last4" api/internal/services/ api/internal/routes/
sed -n '60,110p' api/internal/routes/billing.go

# T-DR-0.2
grep -rn "workspace\|timezone\|notification_pref\|default_environment" api/internal/routes/ api/internal/services/

# T-DR-0.3
grep -rn "api[-_]key\|apikey\|ApiKey\|/v1/api-keys\|/v1/keys" api/internal/routes/ api/internal/services/

# T-DR-0.4
grep -rn "audit.*sse\|sse.*audit\|audit_log.*stream" api/internal/routes/ api/internal/sse/ api/internal/services/
grep -rn "audit_log\|/v1/audit\|AuditHandler" api/internal/routes/ api/internal/server/

# T-DR-0.5
grep -rn "type.*Event\|sse.*status\|status.*stream" api/internal/sse/
find api/internal/sse -type f -name "*.go" -not -name "*_test*"
grep -rn "/events\|/stream\|EventStream\|sse.Handler" api/internal/server/router.go
```

Para reproduzir: re-run os greps acima após mudanças backend. Verdicts podem mudar se backend adicionar endpoints.

---

## Out of scope (não verificado nesta Phase 0)

- Live `curl` smoke tests contra `/api/v1/*` (k3d não rodando esta sessão)
- Backend test coverage dos endpoints existentes
- Performance/latency dos endpoints REST
- RBAC/auth checks per endpoint

Esses ficam para Phase 1+ implementation (testing during consumer integration).
