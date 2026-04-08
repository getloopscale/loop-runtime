# Pricing vs Reality — Honest Cross-Reference

> What we can actually sell today vs what needs to be built.
> Every claim verified against code. 2026-04-08.

---

## Feature Matrix: Promised vs Implemented

### Core Workflows

| Feature | Promised | Reality | Evidence |
|---------|----------|---------|----------|
| `loopzero deploy` | All tiers | **WORKS** | Argo Workflows + BuildKit, tested on DO |
| `loopzero logs` | All tiers | **WORKS** | Loki integration, `api/internal/routes/logs.go` |
| `loopzero rollback` | Pro+ | **WORKS** | Validated 3s, version verified (H6B) |
| `loopzero status` | All tiers | **WORKS** | Handler wired in main.go |
| `loopzero secret set/list/delete` | All tiers | **WORKS** | ESO sync < 30s, AES-256 at rest |
| `loopzero init` | All tiers | **WORKS** | Creates loopzero.yaml, validates config |
| `loopzero validate` | All tiers | **WORKS** | Config validation, framework detection |
| `loopzero login` (Device Code) | All tiers | **WORKS** | RFC 8628 compliant, Google+GitHub OIDC |
| `loopzero project create/list/delete` | All tiers | **WORKS** | Full CRUD via API |
| `loopzero billing` | All tiers | **WORKS** | Stripe checkout, portal, usage display |

### Infrastructure

| Feature | Promised | Reality | Evidence |
|---------|----------|---------|----------|
| Git push → build → deploy | All tiers | **WORKS** | Argo Workflows pipeline, DO validated |
| TLS certificates (auto) | All tiers | **WORKS** | cert-manager + Let's Encrypt |
| Build cache (S3) | All tiers | **WORKS** | BuildKit + MinIO CWTs (H5) |
| Background workers | All tiers | **WORKS** | `type: worker` in loopzero.yaml |
| Static frontend (CDN) | All tiers | **WORKS** | CF Worker + DO Spaces (Sprint 10) |
| Monorepo detection | All tiers | **WORKS** | Turbo/Nx/Lerna/pnpm detection |
| Framework detection | All tiers | **WORKS** | 10 frameworks supported |
| Health checks | All tiers | **WORKS** | Readiness/liveness probes on all pods |

### Observability

| Feature | Promised | Reality | Evidence |
|---------|----------|---------|----------|
| Logs via CLI | All tiers | **WORKS** | Loki cross-cluster, `loopzero logs` |
| Prometheus metrics | All tiers | **WORKS** | kube-prometheus-stack deployed |
| Grafana dashboards | All tiers | **WORKS** | grafana.dev.loopzero.example |
| Alertmanager | All tiers | **WORKS** | 4 SLO rules + Traefik alerts |
| Log retention 7/30/90 days | Per tier | **WORKS** | Loki retention configurable |

### Auth & Billing

| Feature | Promised | Reality | Evidence |
|---------|----------|---------|----------|
| Google/GitHub login | All tiers | **WORKS** | Kratos OIDC, tested today |
| JWT auth (RSA + HMAC) | All tiers | **WORKS** | pkg/jwt, audience validated |
| Stripe checkout | Pro+ | **WORKS** | Checkout + webhooks + portal |
| Plan display in CLI/dashboard | All tiers | **WORKS** | `loopzero billing`, dashboard /billing |
| Team members + roles | All tiers | **WORKS** | owner/admin/member RBAC |

---

## WHAT DOES NOT WORK (Honest)

### NOT IMPLEMENTED — Cannot Sell

| Feature | Promised in Plan | Reality | Impact |
|---------|-----------------|---------|--------|
| **Preview environments** | Pro+ ("per-PR, full-stack") | **CRD exists, NO controller. Sprint 5 not started.** | Cannot promise this. It's the "killer feature" that doesn't exist. |
| **Custom domains** | Pro+ | **Separate module (customdomain/), not in core.** | Cannot promise this without integration work. |
| **SSO / SAML** | Team | **Not implemented. Only GitHub/Google OAuth.** | Cannot promise SSO to Team tier customers. |
| **Audit log** | Team | **Table exists, never written to in runtime.** | Cannot promise compliance audit trail. |
| **Plan limit enforcement** | All tiers | **Code exists but ZERO routes use it. Builds, projects, seats — nothing is checked.** | Free users get the same as Enterprise. No upgrade trigger. |
| **WebSockets** | Implied ("real containers") | **Not implemented. Uses SSE/HTTP polling.** | Don't promise WebSocket support. |
| **BYOC** | Enterprise | **Architecture supports it. Not productized.** | Cannot sell BYOC without onboarding flow. |

### PARTIAL — Works But Incomplete

| Feature | Status | What's Missing |
|---------|--------|----------------|
| Environments | Works (create/delete via API) | Only "production" auto-created. Staging/preview require manual CLI calls. |
| Secrets (ESO) | Works (sync < 30s) | AppSecret controller not implemented. Lifecycle is manual. |
| Builds/month tracking | Metering records builds | Enforcement middleware not wired. No blocking when limit hit. |
| Seat limits | Team members work | No enforcement of seat count per plan. |
| Build timeout per plan | plan_limits has timeout | Not read by build submission. All builds get same timeout. |

---

## REVISED PRICING — Only What We Can Deliver

### What We Can Honestly Sell Today

```
┌───────────────────────────────────────────────────────────────────────┐
│  STARTER (Free)           │  PRO ($29/mo)             │  TEAM ($79/mo)│
│  ─────────────────        │  ────────────────────     │  ──────────── │
│                           │                           │               │
│  1 project (app)          │  3 projects (apps)        │  10 projects  │
│  1 environment            │  3 environments           │  10 envs      │
│  Unlimited builds ¹       │  Unlimited builds ¹       │  Unlimited    │
│  3 team members           │  10 team members          │  25 members   │
│  Community support        │  Email support            │  Priority     │
│                           │  Custom domains ²         │  Custom dom.  │
│  Logs (7 days)            │  Logs (30 days)           │  Logs (90d)   │
│  Metrics + Grafana        │  Metrics + alerts         │  Metrics+alert│
│  512 MB RAM / service     │  2 GB RAM / service       │  8 GB RAM     │
│  Build cache (shared)     │  Build cache (dedicated)  │  Build cache  │
│  `loopzero deploy`            │  `loopzero rollback`          │  Rollback     │
│  `loopzero logs`              │  `loopzero secret`            │  Secrets      │
│  `loopzero billing`           │  Stripe billing           │  Stripe       │
│                           │                           │               │
│  Free forever             │  No overage charges       │  No overage   │
│  Commercial use OK        │  Full observability       │  Full obs.    │
└───────────────────────────────────────────────────────────────────────┘

¹ Plan enforcement not yet wired. All tiers get same access until enforcement ships.
² Custom domains require integration work (module exists, not in core).
```

### Features REMOVED From Plans (Until Implemented)

| Feature | Was In | Reason | Sprint to Build |
|---------|--------|--------|-----------------|
| Preview environments | Pro+ | No controller. Core feature gap. | Sprint 5 |
| SSO / SAML | Team | Not implemented at all. | Sprint 6+ |
| Audit log | Team | Table exists but empty. | 2-3 days of work |
| Plan enforcement | All | Middleware unwired. | 1-2 days of work |
| Custom domains | Pro+ | Separate module, needs integration. | Sprint in progress |
| BYOC | Enterprise | Architecture ready, no onboarding. | Future sprint |
| WebSockets | (implied) | Not supported, don't promise. | Not planned |

---

## WHAT TO BUILD BEFORE LAUNCH

### Priority 1 — Without These, No Paid Plan Makes Sense

| Item | Effort | Why Critical |
|------|--------|-------------|
| **Wire plan enforcement** | 1-2 days | Without this, Starter=Pro=Team=Enterprise. No upgrade trigger. No revenue. |
| **Seat limit enforcement** | 1 day | Same problem — unlimited members on free. |

### Priority 2 — Differentiators That Justify Pro ($29)

| Item | Effort | Why Critical |
|------|--------|-------------|
| **Preview environments** | 2-3 weeks | The "killer feature" in the pricing doc. Without it, Pro has no unique differentiator vs Starter+manual. |
| **Custom domain integration** | 1 week | Standard PaaS feature. Module exists, needs wiring to core. |

### Priority 3 — Required for Team ($79) Tier

| Item | Effort | Why Critical |
|------|--------|-------------|
| **Audit log (wire it)** | 2-3 days | AuditAdapter exists. Just wire it in main.go. |
| **SSO / SAML** | 2-3 weeks | Enterprise requirement. Kratos supports it via OIDC providers. |

---

## HONEST GO-TO-MARKET ASSESSMENT

### Can we launch Starter (Free) today?
**YES.** Everything works: deploy, logs, rollback, secrets, billing display, auth. The only gap is enforcement — but on free tier, "unlimited" is actually fine for adoption.

### Can we launch Pro ($29) today?
**NO.** The differentiator is preview environments, and they don't exist. Without them, Pro is just "Starter with more RAM" — not compelling enough to pay $29/mo.

**Alternative:** Launch Pro WITHOUT preview environments, with these differentiators:
- 3 projects (vs 1)
- 3 environments (vs 1)  
- Custom domains (if integrated)
- `loopzero rollback`
- Email support
- 30-day log retention

This is an honest Pro tier. Not as strong as the original vision, but real.

### Can we launch Team ($79) today?
**NO.** No SSO, no audit log, no plan enforcement. Team customers expect these.

### Recommended Launch Plan

```
Phase 1 (NOW):
  - Launch Starter (Free) — everything works
  - Wire plan enforcement (1-2 days)
  - Launch Pro ($29) without preview envs — honest differentiators

Phase 2 (2-3 weeks):
  - Preview environments (Sprint 5)
  - Custom domain integration
  - Pro becomes the plan it was designed to be

Phase 3 (1-2 months):
  - Audit log wiring
  - SSO via Kratos OIDC providers
  - Launch Team ($79)

Phase 4 (Future):
  - BYOC productization
  - Launch Enterprise
```
