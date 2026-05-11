# Module 10 — Auth & Billing (Kratos + Hydra + Redis + Stripe + JWT)

**Audit Date:** 2026-05-11
**Auditor:** Claude Opus 4.7 (1M context), code-validated
**Confidence:** 88%
**Maturity:** **3.5 / 4.0**

## TL;DR

Core auth flow production-ready (RSA-only JWT + RFC 8725bis + JWKS circuit breaker + dual-key rotation). 4 plan tiers com fail-closed enforcement. Stripe integration estável com idempotency + signature verification. Bloqueia 4.0 por 2 gaps de segurança documentados (service-auth user↔tenant binding, auth failures não persistem em audit_log), session token rotation undocumented, billing usage placeholder Sprint 11, SSO/SAML não construído.

---

## 1. Inventário Real

| Componente | LOC |
|---|---|
| `cloud/` (31 Go files) | 4,868 |
| `api/internal/middleware/auth*.go` | 950 |
| `api/internal/routes/billing.go` | 186 |
| `pkg/jwt/validator.go` (cross-cut M04) | 448 |
| `pkg/enforcement/limiter.go` (cross-cut M04) | 291 |
| **Total core auth+billing** | **~2,554** |
| Tests (auth middleware, billing, JWT, enforcement) | **932** |

**Stripe integration:**
- `cloud/internal/billing/stripe.go` (184 LOC) — HTTP client com idempotency keys
- `cloud/internal/billing/webhook.go` (329 LOC) — Event processor + signature verification
- `cloud/internal/billing/plans.go` (76 LOC) — Plan config + price ID mapping

**4 Plan tiers:**
- Starter: Free, 1 project, default
- Pro: $29/mo, 3 projects (price ID via `STRIPE_PRICE_PRO`)
- Team: $79/mo, 10 projects (`STRIPE_PRICE_TEAM`)
- Enterprise: Custom (sem hard limit; `plan == "enterprise"` auto-allowed)

**Ory Kratos + Hydra:**
- HYDRA_ADMIN_URL: `http://ory-hydra-admin.loopzero-auth:4445`
- HYDRA_PUBLIC_URL: `http://ory-hydra-public.loopzero-auth:4444`
- JWKS: `http://ory-hydra-public.loopzero-auth:4444/.well-known/jwks.json`
- Session tokens: HMAC-SHA256 com dual-key rotation
- Redis: não coupled diretamente; rate-limiter design para Redis swap

**4 RBAC roles:** owner, admin, member, viewer.

---

## 2. Arquitetura Observada

**JWT validation flow:**
```
Bearer token → auth middleware (auth.go) → TokenValidator.ValidateToken() (5min cache, sha256-hashed keys)
  → pkg/jwt.Validator.Validate():
     1. typ header check (RFC 8725bis §2.6)
     2. RSA signing method type-assert (belt-and-suspenders)
     3. JWKS lookup by kid
     4. Issuer + audience + exp claims
  → Tenant membership DB lookup (NUNCA trust headers, S2.3 principle, auth.go:230)
  → UserContext injected → request
```

**OAuth2 device flow (RFC 8628):**
- `POST /v1/auth/device/authorize` (cloud/internal/routes/device.go:27) → user_code, device_code, verification_uri
- `POST /v1/auth/device/token` com device_code (device.go:51) → access_token, refresh_token

**Tenant auto-provisioning** (`cloud/internal/auth/tenant.go`):
- UUID-based `tn-{16hex}` (collision-free, no TOCTOU)
- Slug derived from email local part (≤60 chars, lowercase, hyphens)
- `pg_advisory_xact_lock(hashtext(userID))` serializa concurrent provisioning per user
- Flow: membership fast-path → lock → re-check → create user → create tenant+slug → create owner membership → create starter subscription (idempotent)

**Stripe integration:**
- Checkout: idempotency key `checkout-{tenantID}-{priceID}-{hourBucket}` (prevent double-click, allow retry após 1h)
- Webhook signature: HMAC-SHA256 with 5min freshness window (MEET-038), `hmac.Equal` (no timing oracle)
- Event types: checkout.session.completed, customer.subscription.{updated,deleted}, invoice.{paid,payment_failed}
- Idempotent processing via `event_id + status='processed'` gate
- Plan source of truth: `subscriptions` table (NÃO denormalized)

**Token cache eviction** (`auth.go:385-437`):
- Bounded 10,000 entries
- Evicts expired entries first, então oldest

**Dual-key rotation** (pkg/jwt/validator.go:206-243):
- `SessionTokenIssuer.WithPreviousSecret(oldSecret)` enables zero-downtime rotation
- `kid` header (first 8 hex chars SHA256(secret)) identifica key during rotation

---

## 3. Contratos Públicos

**Authorization header:** `Bearer <access_token>` (auth.go:263-268).

**Access token claims (Hydra-issued, RSA-signed):**
```go
type Claims struct {
  Subject, Email, Name, TenantID, Role, Scope string
  Ext struct { Email, Name, TenantID string }  // consent session
  RegisteredClaims  // iss, aud, exp, nbf, iat
}
```

**Session claims (HMAC-signed, 24h lifetime):**
```go
type SessionClaims struct { Subject, Email, Name, TenantID string; RegisteredClaims }
```

**Multi-tenant headers:**
- `X-LoopZero-Tenant` — quando user tem múltiplas memberships
- `X-Service-Auth` — internal service-to-service shared secret (+ `_PREVIOUS` for rotation)
- `X-Admin-Key` — platform admin API key (+ `_PREVIOUS` for rotation)

**Plan limit error response:**
```json
{"code":"PLAN_LIMIT_EXCEEDED", "plan":"pro", "limit":"projects",
 "current":3, "max":3, "upgrade_url":"..."}
```

**Stripe webhook signature:**
- Format: `t=<timestamp>,v1=<signature>`
- HMAC: SHA256(timestamp + "." + payload)
- Freshness: ≤300s
- Comparison: `hmac.Equal()` (constant-time)

---

## 4. Cobertura de Testes

**Auth middleware** (`api/tests/unit/auth_middleware_test.go`, 504 LOC):
- Bearer token parsing (valid/missing/invalid)
- Tenant membership resolution (single/multiple)
- Service auth fallback
- Cache behavior (5min TTL, eviction)
- User context injection
- X-LoopZero-Tenant header handling

**JWT validation** (cross-cut M04):
- RS256 signing verification
- typ header validation
- kid lookup + fallback refresh
- Issuer/audience/exp validation
- Circuit breaker (Hydra outage resilience)
- JWKS fetch + cache TTL

**Plan enforcement** (`pkg/enforcement/limiter_test.go` + `cloud/tests/unit/enforcement_test.go`):
- CheckLimit (hard/soft semantics)
- CheckValue (per-resource cap)
- Enterprise auto-allow
- Fail-closed em missing limit row
- Fallback para starter plan em subscription miss

**Stripe webhook** (`cloud/tests/unit/billing_test.go`):
- Signature verification (valid/invalid/stale timestamp)
- Event idempotency (already processed skip)
- Checkout completion, subscription update/delete, invoice events
- webhook_events table tracking

**E2E:** `infra/tests/e2e/cli_billing_test.go` (CLI `loopzero billing` + portal redirect).

---

## 5. Gaps e Dívida Técnica

| Item | Severidade | Locação/Evidência |
|---|---|---|
| **G1: Service-auth path sem binding user↔tenant criptográfico** | **MÉDIA-HIGH** | `auth.go:224-254` — relies on internal network trust |
| **G5: Session token rotation procedure undocumented** | **MÉDIA** | `pkg/jwt/validator.go:206-243` — code suporta, sem runbook |
| **G6: Auth failures não persistem em audit_log** | **MÉDIA** | Só logged em slog; sem forensic trail |
| **F1: SSO/SAML não construído** | INTENCIONAL | CLAUDE.md confirma (enterprise post-launch) |
| **F2: Cross-tenant data leakage tests parciais** | MÉDIA | Sem teste de tenant A → tenant B isolation |
| **F4: Subscription grace period não built** | BAIXA | `status IN ('active','trialing')` only |
| **D1: Billing usage map placeholder Sprint 11** | MÉDIA | `routes/billing.go:101-105` empty map |
| **D2: Stripe price ID default-to-pro vulnerability** | BAIXA | `webhook.go:149-150` — defaults if not in mapping |
| **D3: Tenant slug uniqueness via suffix** | BAIXA | display_name não atualizado, só slug |
| G3: Webhook timestamp window 5min replay surface | BAIXA | `webhook.go:301-315` MEET-038 mandate |
| G7: Webhook event sem defensive re-validation | BAIXA | Caller responsibility |

---

## 6. Avaliação de Maturidade

**Score: 3.5 / 4.0** (confidence 88%)

### Evidências

1. **T1 (commit+test)** — `pkg/jwt/validator.go:37` (RS256 whitelist); `cloud/internal/billing/webhook.go` (HMAC signature verify, hmac.Equal); `api/internal/middleware/auth.go:230` (DB role re-resolution); 504 LOC auth middleware tests + JWT validation edge cases.
2. **T2 (LIVE log)** — 12/12 cleanroom GREEN com auth flow LIVE (2026-05-05); canary 80/80 GREEN com plan limits enforce (2026-05-07); cli_billing_test.go E2E GREEN.
3. **T3 (CI run)** — `ci.yml` + cloud workflow runs `go test ./cloud/...` + billing E2E.

### Raciocínio

**Sustenta 3.0-3.5:**
- RSA-only JWT enforcement com RFC 8725bis hardening (typ header)
- JWKS circuit breaker para Hydra outage resilience
- Token cache sha256-hashed keys (heap dump defense)
- Dual-key rotation code-ready (access + session tokens)
- Tenant auto-provisioning com `pg_advisory_xact_lock(hashtext(userID))`
- 4 plan tiers + fail-closed enforcement + soft limits (Team plan)
- Stripe signature verification (HMAC-SHA256 + 5min freshness + constant-time)
- Stripe webhook idempotency (`event_id + status='processed'` gate)
- Service-auth fallback re-resolves role from DB (NUNCA trust headers)

**Bloqueia 4.0:**
- **G1**: service-auth path sem binding criptográfico user↔tenant (compromised cloud → tenant escalation)
- **G5**: session token secret rotation runbook NÃO documentado (manual, error-prone)
- **G6**: auth failures não persistem em audit_log table (forensic gap)
- **F2**: cross-tenant data leakage tests parciais (no defense-in-depth test)
- **D1**: billing usage placeholder Sprint 11 (CLI mostra "no usage data")
- **D2**: Stripe price ID default-to-pro pode causar wrong-plan charge
- **F1**: SSO/SAML não built (enterprise gap, intentional pre-release)
- **F4**: subscription cancellation sem grace period (loss imediata access)

### Caminho para 3.8-4.0

1. **[HIGH]** Documentar session token rotation runbook (G5)
2. **[HIGH]** Adicionar auth failure events em audit_log table (G6)
3. **[MÉDIA]** Cross-tenant data leakage tests (F2): criar 2 tenants, assert tenant A não lê tenant B
4. **[MÉDIA]** Cryptographically bind user_id + tenant_id em X-Service-Auth (HMAC headers, G1)
5. **[MÉDIA]** Fix Stripe price ID — reject 400 se não em PlanConfig map (D2)
6. **[BAIXA]** Implementar 7-day grace period em subscription cancellation (F4)
7. **[BAIXA]** Wire billing usage data (Sprint 11 work, D1)

---

**Próximas auditorias relacionadas:** pkg/ (M04, JWT details), CLI (M01, device flow client), State Machines (M11, Tenant lifecycle).
