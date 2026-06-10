# Session Token Rotation Runbook

> **Plan reference:** T3.2 of `docs/plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md`
>
> **Closes:** M10 Auth/Billing MEDIUM gap — refresh_token support existed (HydraClient.RefreshToken + CLI auth refresh + API token cache TTL 5min) but rotation policy was UNDOCUMENTED.

## Purpose

Responder com clareza às 4 perguntas que estavam abertas:
1. **Quando** o CLI triggers refresh? Proactively at what % of lifetime?
2. **Server-side ou client-driven** rotation?
3. **Redis session TTL** e GC policy?
4. **Fallback** se refresh failure?

## Architecture

```
Browser/CLI ──── device flow / login ────► Hydra (OIDC) ──► access_token (1h TTL)
                                              │                + refresh_token (24h TTL)
                                              ▼
                                          Kratos (session) ──► Redis session (24h)
                                              │
                                              ▼
                                          API verify ──► Hydra introspect (5min cache)
```

## Policy Decisions

| Question | Decision | Rationale |
|---|---|---|
| When CLI refresh triggers? | **Proactively at 80% of token lifetime** (default Hydra 1h → refresh at 48min) | Avoids 401 from request mid-call; preserves UX |
| Server-side rotation? | **NO — client-driven** | Preserves stateless API; aligns OAuth2/OIDC spec |
| Redis session storage TTL? | **24h** (per MEET-2026-04-09-043) | Balance UX (avoid frequent re-auth) vs security (limit hijack window) |
| Session GC policy? | **TTL-based eviction**; orphaned sessions cleared on user logout | Redis built-in EXPIRE handles passive eviction |
| Refresh failure → fallback? | **Device flow re-auth (loopzero login)** | Most resilient; user remains in control |

## Refresh Flow (Happy Path)

### 1. CLI ensureFreshToken pre-call check

```typescript
// cli/src/auth/token-refresh.ts:ensureFreshToken
// Invocado antes de TODO request HTTP em ApiClient

const config = configStore.read();
const expiresAt = config.token_expires_at; // ISO timestamp
const ttlMs = new Date(expiresAt).getTime() - Date.now();
const lifetimeMs = config.token_expires_in_seconds * 1000; // typically 3600 * 1000

if (ttlMs < lifetimeMs * 0.2) {
  // < 20% remaining → refresh proactively
  await refreshNow();
}
```

### 2. CLI invokes Hydra refresh endpoint

```typescript
// cli/src/auth/device-flow.ts:refreshAccessToken
const resp = await fetch(`${cloudUrl}/oauth2/token`, {
  method: 'POST',
  body: new URLSearchParams({
    grant_type: 'refresh_token',
    refresh_token: configStore.getRefreshToken(),
    client_id: 'loopzero-cli',
  }),
});

const { access_token, refresh_token, expires_in } = await resp.json();
configStore.saveToken(access_token, email, team, refresh_token, expires_in);
```

### 3. CLI persists new token atomically

```typescript
// cli/src/config/store.ts:saveToken
// Atomic write via temp file + rename para evitar partial write
const tmp = configPath + '.tmp';
fs.writeFileSync(tmp, JSON.stringify(config, null, 2));
fs.renameSync(tmp, configPath);
```

### 4. Subsequent requests use new access_token

Same `ApiClient` instance picks up updated token via `configStore.getToken()` lookup on each request (no in-memory cache stale).

## Server-Side Cache (API)

Tokens introspected via Hydra are cached **5 minutes** server-side (per `api/internal/middleware/auth_failure_audit.go` + `pkg/jwt`).

```go
// pkg/jwt/cache.go
const TokenCacheTTL = 5 * time.Minute
```

Cache key: SHA256(access_token). Cache hit → skip Hydra round-trip (~50ms saved per request). Cache miss → Hydra `/oauth2/introspect` call, populate cache.

**Implicação:** após refresh, **old access_token continua valid no API por até 5min** (cached). Não é security risk porque:
- Old token expira em Hydra-side
- API ainda verifica `exp` claim antes de cache hit

## Operator Runbook

### Manual rotation trigger (forced refresh)

```bash
loopzero auth refresh
# Saída: "Token refreshed. New expiry: 2026-05-27T01:00:00Z"
```

Use cases:
- Operator suspeita comprometimento (refresh imediato + revoke old session)
- Pre-meeting / pre-presentation (garante token long enough)
- Após Hydra restart (cache invalidation needed)

### Force-revoke ALL sessions for a user

Emergency procedure (suspected account compromise):

```bash
# Step 1: Hydra revoke (CLI tokens)
kubectl exec -n loopzero-system hydra-0 -- hydra revoke access-token \
  --endpoint http://localhost:4445 \
  <access_token>

# Step 2: Kratos revoke (web sessions)
kubectl exec -n loopzero-system kratos-0 -- kratos identities sessions delete \
  --identity-id <user-uuid>

# Step 3: Redis flush (all sessions for user)
kubectl exec -n loopzero-system redis-master-0 -- redis-cli \
  --scan --pattern "session:${user_uuid}:*" | \
  xargs -I{} kubectl exec -n loopzero-system redis-master-0 -- redis-cli DEL {}

# Step 4: Audit
psql -c "INSERT INTO audit_log (action, actor, target, metadata) VALUES (
  'session_revoke_all', '<operator>', '<user>', '{\"reason\":\"compromise\"}')"
```

User precisará re-autenticar via `loopzero login` (device flow) ou web login.

### Inspect active sessions for a user

```bash
# Redis sessions
kubectl exec -n loopzero-system redis-master-0 -- redis-cli \
  --scan --pattern "session:${user_uuid}:*"

# Hydra tokens
kubectl exec -n loopzero-system hydra-0 -- hydra list oauth2-clients \
  --endpoint http://localhost:4445 | grep $user_uuid
```

## Rotation TTL Reference

| Layer | Lifetime | Refresh strategy |
|---|---|---|
| Access token (Hydra) | 1h (configurable via HYDRA_OAUTH2_TTL) | CLI auto-refresh at 80% |
| Refresh token (Hydra) | 24h | NOT auto-refreshed; user re-auth via loopzero login when expires |
| Session cookie (Kratos) | 24h | Sliding window — extends on activity |
| Redis session | 24h | EXPIRE auto-eviction |
| API token cache (memory) | 5min | LRU eviction |
| Service-auth Ed25519 (operators) | 1h | Operators mint fresh via `seed-local-user.sh` |

## Failure Modes

| Sintoma | Causa provável | Recovery |
|---|---|---|
| `loopzero deploy` retorna 401 mid-call | Token expired entre pre-check e request | Auto-retry once com fresh token (em curso T2.7 enhancement) |
| `loopzero auth refresh` retorna `invalid_grant` | refresh_token expired (>24h) | Re-auth via `loopzero login` |
| Multiple 401s em rajada | Hydra restart invalidated cache + introspect failing | Wait 30s; if persists, check `kubectl logs hydra-0` |
| Web user session cancelada inesperadamente | Redis evictou OR Kratos session expired | User re-login (Hydra device flow OR Kratos identity browser) |
| Operator notice token cached em CI (security issue) | CI runner persisted ~/.loopzero/config.json | Use ephemeral auth: `loopzero login --token $CI_TOKEN`; do NOT persist |

## SLO

| Métrica | Target |
|---|---|
| Refresh latency (CLI proactive) | p95 < 500ms |
| Refresh success rate | > 99.5% |
| Token cache hit ratio (API) | > 95% (warm cache) |
| Redis session memory | < 100MB para 10k active users |

## Audit + Compliance

Toda refresh + revoke escreve `audit_log`:
- `action: token_refresh | session_revoke | session_revoke_all`
- `actor: <user OR operator>`
- `metadata: {old_jti, new_jti, source: cli|web|operator}`

Query auditoria de refresh activity últimas 24h:
```sql
SELECT actor, COUNT(*) AS refresh_count
FROM audit_log
WHERE action = 'token_refresh' AND timestamp > NOW() - INTERVAL '24 hours'
GROUP BY actor
ORDER BY refresh_count DESC;
```

## Related

- ADR — refresh token flow (MEET-2026-04-09-043)
- `cli/src/auth/device-flow.ts`
- `cli/src/auth/token-refresh.ts`
- `cli/src/commands/auth-refresh.ts`
- `pkg/jwt/cache.go`
- Ory Hydra docs: https://www.ory.sh/docs/hydra
