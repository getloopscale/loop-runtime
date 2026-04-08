# Execution Plans

> Plans organized by launch phase. Each phase unlocks a pricing tier.
> Reference: `docs/strategy/pricing-reality-check.md`

---

## Phase 1 — Enforcement (Unlocks: Starter + Pro launch)

Without plan enforcement, all tiers have identical access. No upgrade trigger. No revenue.

| Plan | Effort | What it Enables |
|------|--------|-----------------|
| [01-plan-enforcement.md](phase-1-enforcement/01-plan-enforcement.md) | 1-2 days | Projects, builds, environments, seats limited per plan |
| [02-audit-log.md](phase-1-enforcement/02-audit-log.md) | 2-3 days | Mutation events recorded to audit_log table |

**After Phase 1:**
- Starter (Free) works with real limits (1 project, 50 builds, 3 members)
- Pro ($29) works with real limits (3 projects, 500 builds, 10 members)
- Upgrade trigger exists: hit the limit → see "Upgrade to Pro"

---

## Phase 2 — Differentiators (Unlocks: Pro becomes worth $29)

Preview environments and custom domains are the features that make Pro compelling.

| Plan | Effort | What it Enables |
|------|--------|-----------------|
| [01-preview-environments.md](phase-2-differentiators/01-preview-environments.md) | 2-3 weeks | PR → full-stack preview (API + DB + secrets + DNS) |
| [02-custom-domains.md](phase-2-differentiators/02-custom-domains.md) | 1 week | `loopzero domain add example.com` with auto-TLS |

**After Phase 2:**
- Pro has its killer feature (preview environments)
- Custom domains work for Pro+ tiers
- Clear differentiation vs Vercel/Railway

---

## Phase 3 — Team Tier (Unlocks: Team at $79)

Enterprise-grade features that justify the Team price.

| Plan | Effort | What it Enables |
|------|--------|-----------------|
| [01-sso-saml.md](phase-3-team-tier/01-sso-saml.md) | 2-3 weeks | Per-tenant OIDC/SAML provider, "Require SSO" toggle |

**After Phase 3:**
- Team tier has SSO — the #1 enterprise requirement
- Audit log (Phase 1) provides compliance trail
- Team is a credible $79/mo offering

---

## Completed Plans (Archive)

| Plan | Status | Date |
|------|--------|------|
| [PLAN-auth-billing-hardening.md](PLAN-auth-billing-hardening.md) | DONE | 2026-04-07 |
| [PLAN-s1-s2-hardening.md](PLAN-s1-s2-hardening.md) | DONE | 2026-04-07 |
| [PLAN-api-gateway-unification.md](PLAN-api-gateway-unification.md) | DONE | 2026-04-08 |

---

## Timeline

```
Week 1-2:   Phase 1 (enforcement + audit log)
            → Launch Starter (Free) + Pro ($29)

Week 3-5:   Phase 2 (preview environments)
            → Pro becomes the plan it was designed to be

Week 5-6:   Phase 2 (custom domains)
            → Standard PaaS feature, completes Pro

Week 7-10:  Phase 3 (SSO/SAML)
            → Launch Team ($79)

Week 11+:   Enterprise (BYOC, compliance, SLA)
            → Custom pricing
```
