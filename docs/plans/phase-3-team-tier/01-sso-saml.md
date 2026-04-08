# SSO / SAML — Enterprise Auth

> Required for Team ($79) tier. Companies need identity provider integration.
> Estimated effort: 2-3 weeks.

---

## Problem

Auth today is Google OAuth + GitHub OAuth only (via Ory Kratos OIDC). No SAML, no generic OIDC provider configuration per-tenant. Enterprise teams cannot enforce their corporate identity provider.

## Solution

Leverage Ory Kratos's built-in OIDC provider system. Kratos already supports multiple OIDC providers — we need to allow tenants to configure their own.

## Tasks

### 1. Per-tenant OIDC provider configuration

**Files:**
- New: `api/internal/routes/sso.go`
- New DB migration: tenant_sso_config table

**DoD:**
- [ ] Owner can configure OIDC provider for their tenant
- [ ] Config: `client_id`, `client_secret`, `issuer_url`, `provider_type` (SAML, OIDC, Azure AD)
- [ ] Stored encrypted in database
- [ ] Kratos updated dynamically (or per-tenant Kratos config)

### 2. SAML support via Kratos

**DoD:**
- [ ] SAML IdP metadata URL configurable per tenant
- [ ] ACS URL generated per tenant
- [ ] SAML assertion → Kratos identity mapping
- [ ] Test: SAML login with mock IdP

### 3. Enforce SSO for tenant

**DoD:**
- [ ] Owner can toggle "Require SSO" for their tenant
- [ ] When enabled: password login disabled, only SSO allowed
- [ ] Members without SSO session cannot access tenant resources
- [ ] Test: non-SSO login attempt → rejected with "SSO required" message

### 4. CLI + Dashboard support

**DoD:**
- [ ] Dashboard settings page for SSO configuration
- [ ] `loopzero team sso configure` CLI command
- [ ] `loopzero team sso status` shows current config
