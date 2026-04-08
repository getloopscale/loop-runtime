# S1/S2 Hardening Plan — Post-Review Remediation

> Actionable fixes for the 4 highest-ROI items from the second FAANG review.
> Reference: Review §4, §6, §7, §8, §11, §13, §14.
> Created: 2026-04-07.

**Principle:** Fix the root cause. No workarounds. Maintain simplicity.

---

## 1. Engine Resolves Role from DB on Proxy Path (S2.3)

**Problem:** When the cloud proxy forwards requests to the engine via `X-Service-Auth`,
it sends `X-LoopZero-Role` as a header. The engine trusts this header blindly. If the shared
secret leaks, an attacker can inject any role (including `owner`) via header.
This creates two authority models in the engine: DB lookup (for Bearer) vs header trust (for proxy).

**Fix:**
- Engine ignores `X-LoopZero-Role` from proxy headers.
- Engine resolves role from `tenant_members` DB table for ALL auth paths.
- Proxy sends only `X-LoopZero-User-ID` and `X-LoopZero-Tenant` (identity, not authorization).
- `X-LoopZero-Email` remains for display/logging purposes only.

**Files:**
- Edit: `api/internal/middleware/auth.go` — service auth path resolves membership from DB
- Edit: `cloud/cmd/main.go` — stop sending `X-LoopZero-Role` header

**DoD:**
- [ ] Engine `AuthWithTTL` service auth path calls `store.GetMemberships()` instead of reading `X-LoopZero-Role`
- [ ] Cloud proxy does NOT send `X-LoopZero-Role` header
- [ ] `X-LoopZero-Email` is used for logging only, not authorization decisions
- [ ] Bearer path and proxy path produce identical `UserContext` for the same user
- [ ] Tests: proxy path with mismatched role header → engine uses DB role, not header
- [ ] All existing tests pass

---

## 2. Alert on Subscription Fallback to Starter (S1.4)

**Problem:** When `subscriptions` table has no row for a tenant, enforcement silently
falls back to `starter`. This is correct during bootstrap (tenant just created, subscription
row about to be committed), but in steady state it masks data corruption or provisioning bugs.
A missing subscription row should be an observable anomaly, not a silent default.

**Fix:**
- Add a metric `loopzero_subscription_fallback_total` that increments every time the fallback fires.
- Log at WARN level when fallback activates outside of a known bootstrap window.
- Enforcement behavior unchanged (still defaults to starter for availability).

**Files:**
- Edit: `cloud/internal/enforcement/limiter.go` — add metric + warn log on fallback
- Edit: `cloud/internal/metrics/metrics.go` — add counter

**DoD:**
- [ ] `loopzero_subscription_fallback_total` counter increments when plan resolution falls back to starter
- [ ] WARN log emitted with `tenant_id` when fallback activates
- [ ] Enforcement still returns `starter` (behavior unchanged — this is observability, not logic change)
- [ ] Metric visible on `/metrics` endpoint
- [ ] Tests: plan resolution with no subscription row → fallback + metric incremented

---

## 3. Dual-Key Rotation for Service Auth Secret (S1.2)

**Problem:** `LOOPZERO_SERVICE_AUTH_SECRET` has no rotation strategy. Unlike the HMAC session
secret (which has `kid` + dual-key), the service auth secret requires coordinated restart
of both cloud and engine to rotate. During the window between restarts, one service
rejects requests from the other.

**Fix:**
- Engine accepts both `LOOPZERO_SERVICE_AUTH_SECRET` and `LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS`.
- Cloud sends the current secret. Engine validates against current first, then previous.
- Rotation: deploy new secret as current + old as previous → after propagation, remove previous.

**Files:**
- Edit: `api/internal/middleware/auth.go` — accept current OR previous service secret
- Edit: `api/cmd/main.go` — read `LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS` env var

**DoD:**
- [ ] Engine validates `X-Service-Auth` against current secret first, then previous
- [ ] `LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS` env var supported (optional, empty = disabled)
- [ ] Request with old secret accepted during rotation window
- [ ] Request with unknown secret rejected
- [ ] Tests: old secret accepted, unknown secret rejected, empty previous = single-key mode
- [ ] All existing tests pass

---

## 4. Remove Obsolete X-XSS-Protection Header + Tighten CSP (S2.4)

**Problem:** `X-XSS-Protection: 1; mode=block` is deprecated in modern browsers (Chrome
removed its XSS Auditor in 2019). It provides no real protection and can create false
confidence. CSP `style-src 'unsafe-inline'` is necessary for styled-components/Tailwind
but should be documented as a known relaxation.

**Fix:**
- Remove `browserXssFilter: true` from Traefik middleware.
- Add comment documenting why `unsafe-inline` exists in style-src.
- Add `base-uri 'self'` and `form-action 'self'` to CSP (hardens against base tag injection
  and form hijacking).

**Files:**
- Edit: `infra/helm/traefik/middlewares.yaml` — remove XSS filter, tighten CSP

**DoD:**
- [ ] `X-XSS-Protection` header no longer set
- [ ] CSP includes `base-uri 'self'` and `form-action 'self'`
- [ ] `style-src 'unsafe-inline'` documented as required for CSS-in-JS framework
- [ ] Dashboard loads correctly (no CSP violations for legitimate resources)
- [ ] Stripe checkout still works

---

## Execution Order

```
1. Engine role from DB (S2.3) ─── ~1h
   ↓ (changes engine auth middleware)
2. Subscription fallback alert (S1.4) ─── ~30min
   ↓ (adds metric, independent)
3. Service auth dual-key (S1.2) ─── ~1h
   ↓ (changes engine auth middleware, builds on item 1)
4. CSP / XSS header cleanup (S2.4) ─── ~15min

Total: ~3h
```

**Dependencies:**
- Item 1 must land before item 3 (both touch `auth.go`)
- Items 2 and 4 are independent of everything

---

## Items NOT in This Plan (Tracked for Future)

| Item | Severity | When |
|------|----------|------|
| mTLS or workload identity for cloud→engine | S1.1 | Infrastructure sprint |
| Distributed rate limiting (Redis) | S1.3 | Scale sprint (when >1 replica) |
| Dual-audience RSA → per-service tokens | S2.1 | Post-GA (requires token exchange) |
| Cloud→Hydra admin auth | S2.2 | Infrastructure sprint |
| CSP nonce-based (remove unsafe-inline) | S2.4 | Frontend sprint |
| Keychain native storage for CLI | S3.1 | CLI polish sprint |
| Latency/contention metrics | S3.2 | Observability sprint |
| user_identity_map removal | S3.3 | 2026-Q3 per deprecation plan |
| Tenant switcher UI + `loopzero team use` | S2.5 | UX sprint |
