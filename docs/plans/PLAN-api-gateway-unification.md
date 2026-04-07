# API Gateway Unification Plan

> Fix the Kratos 404 bug, unify dashboard to same-origin, simplify DX.
> Reference: MEET-2026-04-07-040.
> Created: 2026-04-07.

**Principle:** Fix what's broken. Simplify what's complex. Don't expand blast radius.

---

## Architecture: Before → After

### Before (Broken)
```
cloud.dev.loopzero.example     → DNS exists, NO IngressRoute → 404 on all Kratos flows
dashboard.dev.loopzero.example → nginx (dashboard SPA)
api.dev.loopzero.example       → Traefik routes: auth→cloud, rest→engine
auth.dev.loopzero.example      → Hydra

Dashboard: VITE_API_URL=https://cloud.dev.loopzero.example (404!)
           VITE_ENGINE_URL=https://api.dev.loopzero.example
           engineFetch() → cloudFetch('/engine/...') → proxy → engine
```

### After (Fixed)
```
cloud.dev.loopzero.example     → Traefik routes:
                             /oauth2/*, /.well-known/* → Hydra
                             /self-service/*, /sessions/* → Kratos
                             /v1/auth/*, /v1/billing/*, /auth/*, /engine/*, /metrics → cloud
                             /* → dashboard SPA (fallback)

api.dev.loopzero.example       → (unchanged) CLI + engine

Dashboard: VITE_API_URL="" (relative URLs, same origin)
           apiFetch('/v1/billing')     → cloud service (same origin)
           apiFetch('/engine/api/v1/projects') → cloud proxy → engine
```

### What's Eliminated
- `dashboard.dev.loopzero.example` IngressRoute + TLS cert
- `VITE_ENGINE_URL` build arg
- `engineFetch()` function in dashboard
- `useEngineApi()` hook
- CORS configuration (same origin = no CORS)
- Kratos 404 bug

### What's Kept
- `api.dev.loopzero.example` (CLI endpoint, unchanged)
- `/engine/*` proxy in cloud service (BFF pattern)
- `X-Service-Auth` for cloud→engine (correct boundary)
- Engine RSA-only auth (MEET-039 B3)
- `auth.dev.loopzero.example` (Hydra OAuth2 discovery)
- `argocd/git/grafana.dev.loopzero.example` (separate auth models)

---

## Task 1 — Create `cloud.{domain}` IngressRoute (P0 Bug Fix)

**Problem:** DNS exists, IngressRoute doesn't. Kratos flows 404 in production.

**Fix:** Add `cloud.{domain}` to `PLATFORM_INGRESS_ROUTES` in mgmt-bootstrap.ts with
priority-based path routing. This is NOT a new IngressRoute pattern — `api.{domain}`
already uses the same priority routing (lines 2197-2233).

**Files:**
- Edit: `infra/pulumi/src/components/mgmt-bootstrap.ts` — add cloud IngressRoute with priority routes

**Implementation:**
- Add a new IngressRoute resource `cloud-gateway` with `Host(cloud.{domain})` match
- Priority 300: `PathPrefix(/oauth2) || PathPrefix(/.well-known)` → ory-hydra-public:4444
- Priority 200: `PathPrefix(/self-service) || PathPrefix(/sessions)` → ory-kratos-public:80
- Priority 100: `PathPrefix(/v1/auth) || PathPrefix(/v1/billing) || PathPrefix(/v1/webhooks) || PathPrefix(/auth) || PathPrefix(/engine) || PathPrefix(/metrics) || PathPrefix(/health)` → loopzero-cloud:13002
- Priority 1: `PathPrefix(/)` → loopzero-dashboard:80

**DoD:**
- [ ] `cloud.{domain}` IngressRoute exists in Pulumi code
- [ ] TLS certificate issued via cert-manager
- [ ] `https://cloud.dev.loopzero.example/sessions/whoami` returns 401 (Kratos running, not 404)
- [ ] `https://cloud.dev.loopzero.example/.well-known/jwks.json` returns Hydra JWKS
- [ ] `https://cloud.dev.loopzero.example/health` returns cloud health
- [ ] `https://cloud.dev.loopzero.example/` returns dashboard HTML
- [ ] `dashboard.{domain}` IngressRoute removed from `PLATFORM_INGRESS_ROUTES`

---

## Task 2 — Dashboard: Relative URLs + Kill engineFetch

**Problem:** Dashboard uses two hardcoded URLs and two fetch functions.

**Fix:** `VITE_API_URL` becomes empty string. All fetch calls use relative URLs (same origin).
`engineFetch` is removed. `cloudFetch` renamed to `apiFetch`.

**Files:**
- Edit: `cloud/dashboard/src/lib/api.ts` — remove ENGINE_API, rename cloudFetch→apiFetch
- Edit: `cloud/dashboard/src/lib/auth.ts` — use relative URL for Kratos
- Edit: `cloud/dashboard/src/hooks/use-api.ts` — merge useCloudApi+useEngineApi→useApi
- Edit: all dashboard pages that import `useEngineApi` or `engineFetch`
- Edit: `.github/workflows/build-images.yml` — VITE_API_URL="" (or remove), remove VITE_ENGINE_URL

**DoD:**
- [ ] `VITE_ENGINE_URL` does not exist in build args or code
- [ ] `VITE_API_URL` is empty string (relative URLs)
- [ ] `engineFetch` function does not exist
- [ ] Only one fetch function: `apiFetch(path, options)`
- [ ] Only one hook: `useApi(path)` (not useCloudApi/useEngineApi)
- [ ] Dashboard builds and serves correctly at `cloud.{domain}`
- [ ] `fetch('/v1/billing')` works (same origin, relative)
- [ ] `fetch('/engine/api/v1/projects')` works (proxied through cloud)

---

## Task 3 — CLI: Single LOOPZERO_URL

**Problem:** CLI has two env vars: `LOOPZERO_API_URL` (engine) and `LOOPZERO_CLOUD_URL` (auth).

**Fix:** New `LOOPZERO_URL` env var replaces both. Cloud service is the gateway for all CLI calls
too (cloud already routes `/v1/auth/*` for CLI). Engine calls go to `api.{domain}` directly
since CLI has RSA tokens with `aud=loopzero-engine`.

Actually — simpler: CLI keeps using `api.{domain}` for everything. The `api.{domain}` IngressRoute
already routes `/v1/auth/*` to cloud and `/*` to engine. CLI just needs ONE URL.

**Files:**
- Edit: `cli/src/config/constants.ts` — `LOOPZERO_URL` replaces both, defaults to `http://localhost:8080`
- Edit: `cli/src/auth/device-flow.ts` — use `LOOPZERO_URL` for device auth
- Edit: `cli/src/auth/token-refresh.ts` — use `LOOPZERO_URL` for refresh
- Edit: all CLI code that calls `getCloudUrl()`

**DoD:**
- [ ] `LOOPZERO_URL` is the single env var (default: localhost, prod: `https://api.dev.loopzero.example`)
- [ ] `LOOPZERO_API_URL` and `LOOPZERO_CLOUD_URL` accepted as fallbacks with deprecation warning
- [ ] All CLI commands work with only `LOOPZERO_URL` set
- [ ] `loopzero login` works (routes to cloud via Traefik path routing)
- [ ] `loopzero deploy` works (routes to engine via Traefik fallback)
- [ ] Tests updated

---

## Task 4 — Fix Proxy Error Codes (Rule 3)

**Problem:** Proxy errors expose internal terms: `ENGINE_UNAVAILABLE`, `PROXY_ERROR`.

**Fix:** Rename to user-facing terms.

**Files:**
- Edit: `cloud/cmd/main.go` — rename error codes in proxy handler

**Mapping:**
- `PROXY_ERROR` → `SERVICE_ERROR`
- `ENGINE_UNAVAILABLE` → `SERVICE_UNAVAILABLE`
- Message: "Failed to reach engine API" → "Service temporarily unavailable. Try again."

**DoD:**
- [ ] No error code or message contains "engine", "proxy", or internal terms
- [ ] Grep for `ENGINE_UNAVAILABLE|PROXY_ERROR|engine API` returns zero in user-facing code
- [ ] `X-Correlation-Id` forwarded through proxy to engine

---

## Task 5 — Traefik Alerting (SRE Requirement)

**Problem:** Zero Traefik alerts exist. Traefik is the single point of entry for all traffic.

**Fix:** Add PrometheusRules for Traefik health.

**Files:**
- Edit: `infra/helm/monitoring/manifests/prometheusrule-critical.yaml` — add LoopZeroTraefikDown
- Edit: `infra/helm/monitoring/manifests/prometheusrule-warning.yaml` — add LoopZeroTraefikHighErrorRate

**Rules:**
```yaml
- alert: LoopZeroTraefikDown
  expr: absent(up{job="mgmt-traefik-platform-system"} == 1)
  for: 1m
  labels: { severity: critical }
  annotations:
    summary: "Traefik is down — all platform traffic is blocked"

- alert: LoopZeroTraefikHighErrorRate
  expr: rate(traefik_service_requests_total{code=~"5.."}[5m]) / rate(traefik_service_requests_total[5m]) > 0.05
  for: 5m
  labels: { severity: warning }
  annotations:
    summary: "Traefik error rate > 5% for 5 minutes"
```

**DoD:**
- [ ] `LoopZeroTraefikDown` alert exists in PrometheusRules
- [ ] `LoopZeroTraefikHighErrorRate` alert exists in PrometheusRules
- [ ] Alerts validated (Prometheus can scrape Traefik metrics)

---

## Task 6 — Remove CORS Middleware from Cloud Service

**Problem:** CORS middleware was added for cross-origin dashboard. With same-origin, it's unnecessary.

**Fix:** Remove CORS middleware from cloud/cmd/main.go. Same-origin requests don't need CORS.

**Files:**
- Edit: `cloud/cmd/main.go` — remove CORS middleware block

**DoD:**
- [ ] No CORS middleware in cloud service
- [ ] Dashboard works at `cloud.{domain}` without CORS headers
- [ ] `DASHBOARD_CORS_ORIGIN` env var removed from config

---

## Execution Order

```
1. Task 1 — IngressRoute (P0 bug fix) ─── 1h
   ↓
2. Task 2 — Dashboard relative URLs ───── 1.5h
   ↓
3. Task 3 — CLI single LOOPZERO_URL ────────── 1h
   ↓ (parallel with 4,5,6)
4. Task 4 — Error code cleanup ─────────── 30min
5. Task 5 — Traefik alerting ──────────── 30min
6. Task 6 — Remove CORS ──────────────── 15min

Total: ~5h
```

**Dependencies:**
- Task 2 depends on Task 1 (dashboard must be served at cloud.{domain})
- Tasks 3,4,5,6 are independent of each other
- All tasks depend on Task 1 for validation on DigitalOcean

---

## Items NOT in This Plan

| Item | Reason | When |
|------|--------|------|
| Hydra token exchange (RSA for dashboard) | Requires OAuth2 RFC 8693 implementation | Future sprint |
| Remove `/engine/*` proxy entirely | Requires dashboard RSA tokens first | After token exchange |
| Remove `X-Service-Auth` mechanism | Requires proxy removal first | After token exchange |
| Consolidate argocd/git/grafana subdomains | Protocol requirements prevent consolidation | Never (correct as-is) |
| Distributed rate limiting | Separate concern, covered in S1.3 | Scale sprint |
