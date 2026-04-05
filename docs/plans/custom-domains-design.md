# Custom Domains — System Design

> Feature: Allow tenants to map their own domains to LoopZero-deployed applications.
> Status: **APPROVED_WITH_CONDITIONS** — MEET-2026-04-05-033
> Author: Paulo + Claude
> Date: 2026-04-05
> Meeting: [MEET-2026-04-05-033](../meetings/records/MEET-2026-04-05-033.md)

---

## 1. Problem Statement

Today, all LoopZero applications are served at auto-generated subdomains under the platform base domain:

```
https://{tenant}-{project}-{app}-{env}.dev.loopzero.example
```

Users cannot use their own domains (e.g., `api.mycompany.com`). This is a blocker for production adoption — no serious product ships on a PaaS subdomain.

**Goal:** Let users bring their own domains with zero infrastructure knowledge, matching Vercel/Netlify DX.

---

## 2. User Experience (CLI-first, Rule 9)

### Happy Path

```bash
# 1. Add custom domain to a specific app
$ loopzero domain add api.mycompany.com --project myapi --app web --env production
✓ Domain registered

  To activate, add these DNS records:

  Type   Name                             Value
  ────── ──────────────────────────────── ──────────────────────────
  CNAME  api.mycompany.com                cname.loopzero.example
  TXT    _loopzero-verify.api.mycompany.com   loopzero-verify=dk8f2m9x3p

  Then run: loopzero domain verify api.mycompany.com

# 2. Verify (after user adds DNS records)
$ loopzero domain verify api.mycompany.com
✓ DNS verified
⟳ TLS certificate being provisioned...

  Check progress: loopzero domain status api.mycompany.com

# 3. Check status
$ loopzero domain status api.mycompany.com
  Domain:      api.mycompany.com
  Project:     myapi
  App:         web
  Environment: production
  Status:      active
  TLS:         ✓ valid (expires 2026-07-04)
  Verified at: 2026-04-05 12:05 UTC

# 4. List domains
$ loopzero domain list --project myapi
  DOMAIN                  APP   ENV          STATUS    TLS
  api.mycompany.com       web   production   active    ✓ valid (expires 2026-07-04)
  staging.mycompany.com   web   staging      pending   ✗ awaiting DNS verification

# 5. Remove domain
$ loopzero domain remove api.mycompany.com --confirm api.mycompany.com
✓ Domain removed

# 6. Deploy shows both URLs
$ loopzero deploy
  ✓ Build succeeded
  ✓ Deploy complete

  URLs:
    https://tn-myapi-web-production.dev.loopzero.example  (platform)
    https://api.mycompany.com                        (custom domain)
```

### Exit Codes (Rule 4)

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Usage error (invalid domain, missing flags) |
| 3 | Domain operation failed (verification failed, TLS error) |
| 4 | Auth error |

### JSON Output (Rule 5, --json)

```json
// loopzero domain add --json
{
  "ok": true,
  "domain": "api.mycompany.com",
  "project": "myapi",
  "app": "web",
  "environment": "production",
  "status": "pending_verification",
  "verification": {
    "type": "txt",
    "name": "_loopzero-verify.api.mycompany.com",
    "value": "loopzero-verify=dk8f2m9x3p"
  },
  "cname_target": "cname.loopzero.example"
}

// loopzero domain verify --json
{
  "ok": true,
  "domain": "api.mycompany.com",
  "status": "verified",
  "verified_at": "2026-04-05T12:05:00Z",
  "message": "Provisioning in progress. Check status with: loopzero domain status api.mycompany.com"
}

// loopzero domain status --json
{
  "ok": true,
  "domain": "api.mycompany.com",
  "project": "myapi",
  "app": "web",
  "environment": "production",
  "status": "active",
  "tls": {
    "status": "issued",
    "expires_at": "2026-07-04T00:00:00Z"
  },
  "created_at": "2026-04-05T12:00:00Z",
  "verified_at": "2026-04-05T12:05:00Z"
}

// loopzero domain list --json
{
  "ok": true,
  "domains": [
    {
      "domain": "api.mycompany.com",
      "project": "myapi",
      "app": "web",
      "environment": "production",
      "status": "active",
      "tls_status": "issued",
      "tls_expires_at": "2026-07-04T00:00:00Z",
      "created_at": "2026-04-05T12:00:00Z",
      "verified_at": "2026-04-05T12:05:00Z"
    }
  ],
  "total": 1
}
```

---

## 3. Architecture

### Traffic Flow (Custom Domain)

```
User browser → api.mycompany.com
    → DNS CNAME → cname.loopzero.example
    → CF Tunnel (catch-all rule routes to Traefik)
    → cloudflared pod (workload cluster)
    → Traefik (matches Host(`api.mycompany.com`) via IngressRoute)
    → TLS termination (cert-manager Let's Encrypt cert via HTTP-01)
    → App Service → Pod
```

> **⚠ BLOCKER (MEET-033):** The current CF Tunnel ingress config only matches `*.dev.loopzero.example`.
> Custom hostnames will hit the catch-all `http_status:404`. The tunnel config must be updated
> to route ALL traffic to Traefik, letting Traefik handle hostname matching via IngressRoute rules.
> See §6.2 for the fix.

### Verification Flow (De-orchestrated — MEET-033 D4)

```
loopzero domain add api.mycompany.com --app web
    ↓
API creates custom_domains row (status: pending_verification)
    ↓
API generates verification token (crypto/rand, 10 bytes hex)
    ↓
Returns instructions: add TXT _loopzero-verify.api.mycompany.com + CNAME to cname.loopzero.example
    ↓
User adds DNS records (CNAME + TXT)
    ↓
loopzero domain verify api.mycompany.com
    ↓
API checks TXT record via DNS lookup (net.LookupTXT)
    ↓
If TXT matches → status: verified (returns immediately)
    ↓
DomainProvisioner (background service) picks up verified domains:
    ↓
    1. Acquires withEnvironmentLock(tenant, project, env)
    2. Fetches active release for (tenant, project, environment)
    3. Re-renders manifests with custom domain added
    4. Gitops push (additive — IngressRoute + Certificate only)
    5. Waits for ArgoCD sync
    6. status → provisioning
    ↓
    7. cert-manager issues Let's Encrypt cert (HTTP-01 challenge)
    8. ExposureController probes https://api.mycompany.com
    ↓
DomainHealthChecker detects cert issued → status: active
```

### Component Map

```
┌──────────────┐     ┌──────────────┐     ┌──────────────────┐
│  CLI          │────▶│  API          │────▶│  PostgreSQL       │
│  loopzero domain  │     │  /domains     │     │  custom_domains   │
│  (add/verify/ │     │  (CRUD +      │     │                  │
│   status/list/│     │   DNS check)  │     │                  │
│   remove)     │     └──────┬───────┘     └──────────────────┘
└──────────────┘            │
                     ┌──────▼────────────────────────────────┐
                     │  Background Services (API process)     │
                     │  ┌──────────────────┐ ┌─────────────┐ │
                     │  │ DomainProvisioner │ │ DomainHealth│ │
                     │  │ verified→active   │ │ Checker     │ │
                     │  └────────┬─────────┘ └──────┬──────┘ │
                     └───────────┼────────────────────┼───────┘
                                │                    │
                     ┌──────────▼────────┐           │ (weekly CNAME re-check,
                     │ Manifest Renderer  │           │  TLS expiry, stale cleanup,
                     │ (renderer.go)      │           │  cluster state convergence)
                     └──────────┬────────┘           │
                                │                    │
                     ┌──────────▼────────┐    ┌──────▼──────────────┐
                     │  Gitea (gitops)    │───▶│  ArgoCD (sync)      │
                     └───────────────────┘    └──────┬──────────────┘
                                                     │
                     ┌───────────────────────────────▼───────────────┐
                     │  Workload Cluster                              │
                     │  ┌─────────────────┐  ┌─────────────────────┐ │
                     │  │ IngressRoute     │  │ Certificate CR       │ │
                     │  │ Host(custom)     │  │ (Let's Encrypt)      │ │
                     │  └────────┬────────┘  └──────────┬──────────┘ │
                     │           │                       │            │
                     │  ┌────────▼────────┐  ┌──────────▼──────────┐ │
                     │  │  Traefik         │  │  cert-manager        │ │
                     │  └─────────────────┘  └─────────────────────┘ │
                     └───────────────────────────────────────────────┘
```

---

## 4. Data Model

### Migration 000014: custom_domains table

```sql
-- Migration 000014: Custom domains for tenant applications
-- Custom Domain feature (MEET-2026-04-05-033)

CREATE TABLE IF NOT EXISTS custom_domains (
    id                 TEXT PRIMARY KEY,          -- "dom-" + 8 hex chars
    tenant_id          TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_name       TEXT NOT NULL,             -- project name (not UUID, per Known Pattern)
    app_name           TEXT NOT NULL,             -- specific app within the project
    environment        TEXT NOT NULL,
    domain             TEXT NOT NULL,             -- "api.mycompany.com"
    status             TEXT NOT NULL DEFAULT 'pending_verification'
                       CHECK (status IN (
                           'pending_verification',
                           'verified',
                           'provisioning',
                           'active',
                           'failed',
                           'removing'
                       )),
    verification_token TEXT NOT NULL,             -- "loopzero-verify=xxxxxxxxxxxx"
    verified_at        TIMESTAMPTZ,
    tls_status         TEXT DEFAULT 'pending'
                       CHECK (tls_status IN ('pending', 'issuing', 'issued', 'failed')),
    tls_expires_at     TIMESTAMPTZ,
    failure_reason     TEXT,
    last_cname_check   TIMESTAMPTZ,              -- last weekly CNAME re-verification
    cname_consecutive_failures INT DEFAULT 0,     -- consecutive CNAME check failures
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One domain globally unique (no two tenants can claim the same domain)
CREATE UNIQUE INDEX IF NOT EXISTS idx_custom_domains_domain
    ON custom_domains(domain);

-- Lookup by tenant + project + app + environment
CREATE INDEX IF NOT EXISTS idx_custom_domains_tenant_project_app_env
    ON custom_domains(tenant_id, project_name, app_name, environment);

-- Background service: find domains needing provisioning
CREATE INDEX IF NOT EXISTS idx_custom_domains_status
    ON custom_domains(status) WHERE status IN ('verified', 'provisioning', 'removing');

-- Background service: find domains needing CNAME re-check
CREATE INDEX IF NOT EXISTS idx_custom_domains_active_cname_check
    ON custom_domains(last_cname_check) WHERE status = 'active';

-- Cascade on tenant deletion (MEET-033 Q3)
-- ON DELETE CASCADE on tenant_id handles project deletion cleanup

COMMENT ON TABLE custom_domains IS 'User-owned custom domains mapped to LoopZero applications';
COMMENT ON COLUMN custom_domains.domain IS 'FQDN owned by the user (e.g., api.mycompany.com)';
COMMENT ON COLUMN custom_domains.app_name IS 'Specific app within the project this domain routes to';
COMMENT ON COLUMN custom_domains.verification_token IS 'TXT record value for DNS ownership verification';
COMMENT ON COLUMN custom_domains.status IS 'Lifecycle: pending_verification → verified → provisioning → active | failed → removing';
COMMENT ON COLUMN custom_domains.cname_consecutive_failures IS 'Consecutive weekly CNAME check failures. At 2 (14 days), domain degrades to failed';
```

### State Machine (Enriched — MEET-033)

```
                              ┌──────────┐
                     ┌───────▶│  failed   │◀─────────────────┐
                     │        └─────┬────┘                   │
                     │              │ retry                   │ provision_fail
                     │              ▼                         │ OR cname_lost (2 weeks)
               provision_fail  ┌──────────┐            ┌─────┴──────┐
                     │         │ verified  │───────────▶│provisioning│
                     │         └──────────┘  provision  └─────┬──────┘
                     │              ▲                          │
                     │              │ verify                   │ activate
                     │              │                          ▼
                ┌────┴───────────┐  │                    ┌──────────┐
                │ pending_       │──┘                    │  active   │
                │ verification   │                       └─────┬────┘
                └────┬───────────┘                             │
                     │                                         │
                     │ remove          remove ┌────────────────┘
                     ▼                        ▼
                ┌──────────┐
                │ removing  │ → (row deleted after gitops cleanup)
                └──────────┘
```

**Transitions:**
| From | Event | To | Trigger |
|------|-------|----|---------|
| pending_verification | verify | verified | `loopzero domain verify` (TXT match) |
| pending_verification | remove | removing | `loopzero domain remove` |
| verified | provision | provisioning | DomainProvisioner (background) |
| verified | remove | removing | `loopzero domain remove` |
| provisioning | activate | active | DomainHealthChecker (cert issued + reachable) |
| provisioning | provision_fail | failed | DomainProvisioner (gitops/sync error) |
| active | cname_lost | failed | DomainHealthChecker (2 consecutive weekly failures) |
| active | remove | removing | `loopzero domain remove` |
| failed | retry | verified | `loopzero domain verify` (re-triggers provisioning) |
| failed | remove | removing | `loopzero domain remove` |
| removing | deleted | (row deleted) | After gitops cleanup confirmed |

---

## 5. API Endpoints

All endpoints require authentication. Tenant isolation via `middleware.GetUserContext(ctx)`.
All mutations that touch gitops use `withEnvironmentLock` (Rule 6).

### POST /api/v1/domains

Register a custom domain for a specific app.
Supports `Idempotency-Key` header.

```
Request:
{
    "domain": "api.mycompany.com",
    "project": "myapi",
    "app": "web",
    "environment": "production"
}

Response 201:
{
    "id": "dom-a1b2c3d4",
    "domain": "api.mycompany.com",
    "project": "myapi",
    "app": "web",
    "environment": "production",
    "status": "pending_verification",
    "verification": {
        "txt_name": "_loopzero-verify.api.mycompany.com",
        "txt_value": "loopzero-verify=dk8f2m9x3p"
    },
    "cname_target": "cname.loopzero.example"
}

Errors:
- 400: Invalid domain format
- 400: Domain is a platform subdomain (see §6.3 blocklist)
- 400: Domain contains wildcard
- 409: Domain already registered (by any tenant)
- 404: Project, app, or environment not found
```

### POST /api/v1/domains/{domain}/verify

DNS verification check. Returns immediately — does NOT trigger provisioning.
Idempotent: returns 200 for already-verified domains.

```
Response 200 (verified):
{
    "domain": "api.mycompany.com",
    "status": "verified",
    "verified_at": "2026-04-05T12:05:00Z",
    "message": "Provisioning in progress. Check status with: loopzero domain status api.mycompany.com"
}

Response 200 (not yet):
{
    "domain": "api.mycompany.com",
    "status": "pending_verification",
    "verification_error": "TXT record not found for _loopzero-verify.api.mycompany.com",
    "hint": "DNS changes can take up to 48 hours to propagate. Most providers update within 5 minutes."
}

Response 200 (already active):
{
    "domain": "api.mycompany.com",
    "status": "active"
}

Errors:
- 404: Domain not found (or not owned by this tenant)
```

### GET /api/v1/domains

List domains for the current tenant. Supports pagination and filters.

```
Query params: ?project=X&app=Y&environment=Z&limit=50&offset=0

Response 200:
{
    "domains": [...],
    "total": 5,
    "limit": 50,
    "offset": 0
}
```

### GET /api/v1/domains/{domain}

Get details for a specific domain (used by `loopzero domain status`).

```
Response 200:
{
    "id": "dom-a1b2c3d4",
    "domain": "api.mycompany.com",
    "project": "myapi",
    "app": "web",
    "environment": "production",
    "status": "active",
    "tls": {
        "status": "issued",
        "expires_at": "2026-07-04T00:00:00Z"
    },
    "verification": {
        "txt_name": "_loopzero-verify.api.mycompany.com",
        "txt_value": "loopzero-verify=dk8f2m9x3p"
    },
    "cname_target": "cname.loopzero.example",
    "created_at": "2026-04-05T12:00:00Z",
    "verified_at": "2026-04-05T12:05:00Z"
}
```

### DELETE /api/v1/domains/{domain}

Remove a custom domain. Triggers async cleanup via DomainProvisioner.

```
Response 200:
{
    "ok": true,
    "domain": "api.mycompany.com",
    "status": "removing"
}

Errors:
- 404: Domain not found
```

---

## 6. DNS & Infrastructure

### 6.1 TXT Record Verification

```go
// VerifyDomain checks DNS TXT records for ownership proof.
func VerifyDomain(ctx context.Context, domain, expectedToken string) (bool, error) {
    txtName := "_loopzero-verify." + domain
    records, err := net.DefaultResolver.LookupTXT(ctx, txtName)
    if err != nil {
        return false, nil  // DNS lookup failed — record may not exist yet
    }
    for _, record := range records {
        if record == expectedToken {
            return true, nil
        }
    }
    return false, nil
}
```

### 6.2 CF Tunnel Config Fix (BLOCKER)

The tunnel ingress must route ALL incoming traffic to Traefik, not just `*.{baseDomain}`:

```typescript
// infra/pulumi/src/providers/cloudflare/tunnel.ts
// BEFORE (broken for custom domains):
ingress: [
    { hostname: `*.${baseDomain}`, service: traefikService, originRequest: { noTlsVerify: true } },
    { service: "http_status:404" },  // catch-all rejects custom domains
]

// AFTER (fixed):
ingress: [
    { hostname: `*.${baseDomain}`, service: traefikService, originRequest: { noTlsVerify: true } },
    { hostname: `cname.${baseDomain}`, service: traefikService, originRequest: { noTlsVerify: true } },
    // Catch-all: route ANY hostname that resolves to this tunnel to Traefik.
    // Traefik handles hostname matching via IngressRoute Host() rules.
    // Unknown hostnames get Traefik's default 404.
    { service: traefikService, originRequest: { noTlsVerify: true } },
]
```

### 6.3 CNAME Target (Pulumi)

```typescript
// New DNS record: cname.loopzero.example → tunnel
new cloudflare.DnsRecord("custom-domain-cname-target", {
    zoneId: cfZoneId,
    name: "cname",
    type: "CNAME",
    content: `${tunnelId}.cfargotunnel.com`,
    ttl: 1,  // auto
    proxied: true,
});
```

### 6.4 Validation Rules

1. **Domain format:** Valid FQDN (RFC 1123), no trailing dot, no wildcard, no protocol prefix
2. **Max length:** 253 characters (DNS limit)
3. **Platform domain blocklist (exhaustive):**
   - `*.loopzero.example` (and `*.dev.loopzero.example`, `*.staging.loopzero.example`, `*.prod.loopzero.example`)
   - All infra subdomains: `api.`, `argocd.`, `git.`, `grafana.`, `cloud.`, `auth.`, `monitoring.`, `cname.`
   - Configurable via environment variable `DOMAIN_BLOCKLIST` for additional entries
4. **Reserved domains:** `localhost`, `*.local`, `*.internal`, `*.example.com`, RFC 2606 names
5. **Globally unique:** Enforced by unique index on `domain` column

### 6.5 HTTP-01 Challenge Path (BLOCKER — Spike Required)

Before Phase 2 begins, validate this path works:

```
Let's Encrypt → http://api.mycompany.com/.well-known/acme-challenge/{token}
    → DNS CNAME → cname.loopzero.example
    → CF Tunnel → cloudflared → Traefik
    → cert-manager solver pod (temporary, serves challenge response)
    → 200 OK with challenge token
    → Certificate issued
```

**Spike acceptance criteria:**
- [ ] Custom hostname CNAME resolves through CF Tunnel to Traefik
- [ ] cert-manager HTTP-01 solver creates temporary pod/ingress
- [ ] Let's Encrypt challenge succeeds and certificate is issued
- [ ] Certificate appears as K8s Secret in workload namespace

**Fallback if spike fails:** Cloudflare for SaaS (SSL for SaaS) — paid, but handles TLS automatically for custom domains at the edge.

---

## 7. Manifest Generation Changes

### Domain-Derived Naming (MEET-033 — not index-based)

Resource names are derived from the domain to prevent churn when domains are added/removed:

```go
// domainResourceSuffix generates a stable, short suffix from a domain name.
// Example: "api.mycompany.com" → "cd-api-mycompany-com" (sanitized, max 63 chars)
func domainResourceSuffix(domain string) string {
    sanitized := naming.Sanitize(strings.ReplaceAll(domain, ".", "-"))
    return "cd-" + sanitized  // "cd" prefix = custom domain
}
```

### Manifest Renderer Extension

```go
// In renderer.go — buildIngressResources()
for _, app := range input.Apps {
    // 1. Auto-generated hostname (always present)
    autoHostname := appHostname(input.TenantID, input.ProjectID, app.Name, input.Environment, input.BaseDomain)
    resources = append(resources,
        buildCertificate(autoHostname, "tls-"+app.Name, input.ClusterIssuerName),
        buildIngressRoute(autoHostname, app.Name, app.Port),
    )

    // 2. Custom domains (additive only — each gets its own IngressRoute + Certificate)
    for _, cd := range app.CustomDomains {
        suffix := domainResourceSuffix(cd.Domain)
        resources = append(resources,
            buildCertificate(cd.Domain, "tls-"+suffix, input.ClusterIssuerName),
            buildIngressRoute(cd.Domain, suffix, app.Port),
        )
    }
}
```

### AppManifestInput Extension

```go
type CustomDomainInput struct {
    Domain string  // FQDN (e.g., "api.mycompany.com")
    ID     string  // domain row ID for traceability
}

type AppManifestInput struct {
    // ... existing fields ...
    CustomDomains []CustomDomainInput  // NEW — active custom domains for this app
}
```

---

## 8. Background Services

### 8.1 DomainProvisioner

Picks up domains in `verified` and `removing` states. Runs every 30 seconds.

```go
type DomainProvisioner struct {
    store     DomainStore
    renderer  ManifestRenderer
    gitops    GitopsPusher
    interval  time.Duration  // 30s
}

func (p *DomainProvisioner) Run(ctx context.Context) {
    ticker := time.NewTicker(p.interval)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            p.provisionVerifiedDomains(ctx)  // verified → provisioning → (wait for activation)
            p.removeDomainsMarkedForDeletion(ctx)  // removing → (gitops cleanup) → delete row
        }
    }
}

// Uses SELECT FOR UPDATE SKIP LOCKED for HA coordination (MEET-033)
// Uses withEnvironmentLock for gitops mutations (Rule 6)
```

### 8.2 DomainHealthChecker

Monitors active domains. Runs every 5 minutes.

```go
type DomainHealthChecker struct {
    store    DomainStore
    interval time.Duration  // 5m
}

func (h *DomainHealthChecker) Run(ctx context.Context) {
    ticker := time.NewTicker(h.interval)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            h.autoVerifyPendingDomains(ctx)     // Background auto-verify (convenience)
            h.checkActiveDomainHealth(ctx)       // Weekly CNAME re-check (MEET-033 security)
            h.checkTLSExpiry(ctx)                // Warn if cert expires < 30 days
            h.cleanupStalePending(ctx)           // Remove domains pending > 7 days
            h.convergeCluserState(ctx)           // Re-provision if IngressRoute missing (MEET-033 D4)
        }
    }
}
```

**Weekly CNAME Re-verification (MEET-033 D7):**
- Check active domains where `last_cname_check` is older than 7 days
- DNS lookup `CNAME` for the custom domain
- If CNAME does not resolve to `cname.{baseDomain}`: increment `cname_consecutive_failures`
- At 2 consecutive failures (14 days): transition `active → failed`, remove IngressRoute via gitops
- Grace period configurable via `DOMAIN_CNAME_GRACE_DAYS` env var (default: 14)

**Cluster State Convergence (MEET-033 D4 enhancement):**
- For `active` domains, verify that the IngressRoute + Certificate exist in the workload cluster
- If missing (ArgoCD sync failure, manual deletion): re-trigger provisioning
- Prevents DB-says-active-but-cluster-disagrees divergence

---

## 9. CLI Commands

### loopzero domain add

```typescript
class DomainAddCommand {
    async execute(opts: {
        domain: string;
        project: string;
        app: string;          // REQUIRED (MEET-033)
        environment: string;
        json?: boolean;
    }): Promise<number> {
        // 1. Validate domain format locally (fast feedback)
        // 2. POST /api/v1/domains with Idempotency-Key
        // 3. Display verification instructions (human) or JSON
        // Exit 0 success, 1 validation, 3 API error, 4 auth
    }
}
```

### loopzero domain verify

```typescript
class DomainVerifyCommand {
    async execute(opts: { domain: string; json?: boolean }): Promise<number> {
        // 1. POST /api/v1/domains/{domain}/verify
        // 2. If verified: show success + "provisioning in progress" message
        // 3. If not: show specific DNS guidance (record not found, wrong value, etc.)
        // 4. Include DNS propagation hint in error messages
    }
}
```

### loopzero domain status (NEW — MEET-033)

```typescript
class DomainStatusCommand {
    async execute(opts: { domain: string; json?: boolean }): Promise<number> {
        // 1. GET /api/v1/domains/{domain}
        // 2. Show full domain details: status, TLS, verification, timestamps
    }
}
```

### loopzero domain list

```typescript
class DomainListCommand {
    async execute(opts: {
        project?: string;
        app?: string;
        env?: string;
        json?: boolean;
    }): Promise<number> {
        // 1. GET /api/v1/domains?project=X&app=Y&environment=Z
        // 2. Table output (human) or JSON with pagination
    }
}
```

### loopzero domain remove

```typescript
class DomainRemoveCommand {
    async execute(opts: { domain: string; confirm?: string; json?: boolean }): Promise<number> {
        // 1. Require --confirm <domain> (safety guard, same pattern as project-delete)
        // 2. DELETE /api/v1/domains/{domain}
        // 3. Show "Domain removal in progress"
    }
}
```

---

## 10. Security

1. **Domain takeover prevention:** TXT verification proves DNS control before any traffic routes
2. **Dangling domain mitigation (MEET-033):** Weekly CNAME re-check → IngressRoute removal after 14 days of CNAME loss
3. **Stale domain cleanup:** Domains pending verification > 7 days are auto-removed
4. **No wildcard custom domains:** Only exact FQDNs (no `*.mycompany.com`)
5. **Platform domain protection:** Exhaustive configurable blocklist (see §6.4)
6. **Tenant isolation:** Domain belongs to exactly one tenant (unique index + API-level tenant check)
7. **Certificate scope:** One Let's Encrypt cert per domain (no SAN sharing — MEET-033 Q4)
8. **Cascade on delete:** Custom domains deleted when tenant is deleted (ON DELETE CASCADE — MEET-033 Q3)

---

## 11. Observability

### Prometheus Metrics

```
loopzero_custom_domains_total{tenant, status}              — gauge, domain count by status
loopzero_domain_verification_duration_seconds{result}      — histogram, verification latency
loopzero_domain_provisioning_duration_seconds{result}      — histogram, provisioning latency
loopzero_domain_tls_expiry_days{domain, tenant}            — gauge, days until cert expiry
loopzero_domain_verification_failures_total{tenant}        — counter, failed verifications
loopzero_domain_cname_check_failures_total{tenant}         — counter, CNAME re-check failures
loopzero_domain_health_checker_errors_total{check}         — counter, health checker errors
```

### Alerts

```yaml
- alert: DomainTLSExpiringSoon
  expr: loopzero_domain_tls_expiry_days < 14
  for: 1h
  labels:
    severity: warning
  annotations:
    summary: "Custom domain TLS certificate expiring soon"

- alert: DomainVerificationStale
  expr: loopzero_custom_domains_total{status="pending_verification"} > 0
  for: 48h
  labels:
    severity: info

- alert: DomainProvisioningStuck
  expr: loopzero_custom_domains_total{status="provisioning"} > 0
  for: 1h
  labels:
    severity: warning

- alert: DomainCNAMELost
  expr: loopzero_domain_cname_check_failures_total > 0
  for: 24h
  labels:
    severity: warning
```

---

## 12. Plan Enforcement

Custom domain limits per plan (reuses existing enforcement middleware from `cloud/`):

| Plan | Custom Domains per Environment |
|------|-------------------------------|
| Starter | 0 (platform subdomain only) |
| Pro | 3 |
| Team | 10 |
| Enterprise | Unlimited |

---

## 13. Implementation Phases

### Phase 0: Blockers (MUST complete first)

- [ ] **Fix CF Tunnel ingress config** — route all traffic to Traefik (§6.2)
- [ ] **HTTP-01 spike** — validate Let's Encrypt challenge through CNAME → CF Tunnel (§6.5)
- [ ] **`cname.loopzero.example` DNS record** — Pulumi provisioning (§6.3)

### Phase 1: Core (MVP)

- [ ] Migration 000014: `custom_domains` table with `app_name` column
- [ ] `DomainStore` (CRUD + verification query + `SELECT FOR UPDATE SKIP LOCKED`)
- [ ] Domain validation logic (FQDN, blocklist, reserved names)
- [ ] DNS verification logic (`net.LookupTXT`)
- [ ] API routes: POST/GET/DELETE /domains, POST /domains/{domain}/verify, GET /domains/{domain}
- [ ] CLI: `loopzero domain add/verify/status/list/remove` with `--app` flag
- [ ] Unit tests for all components
- [ ] Integration test: domain add + verify with environment lock

### Phase 2: Provisioning

- [ ] Manifest renderer: custom domain IngressRoute + Certificate (domain-derived naming)
- [ ] `DomainProvisioner` background service (verified → provisioning, removing → deleted)
- [ ] `DomainHealthChecker` background service (auto-verify, CNAME re-check, TLS expiry, stale cleanup, cluster convergence)
- [ ] `withEnvironmentLock` for all gitops-touching domain mutations
- [ ] `loopzero deploy` output shows custom domain URLs
- [ ] Integration test: concurrent deploy + domain verify (no gitops corruption)
- [ ] Integration test: domain remove cleans up IngressRoute + Certificate

### Phase 3: Polish

- [ ] Plan enforcement (domain limits per tier)
- [ ] Prometheus metrics + Grafana dashboard panel
- [ ] Alert rules (TLS expiry, stale, stuck provisioning, CNAME loss)
- [ ] Dashboard UI page (domains management)
- [ ] E2E test on DigitalOcean
- [ ] User documentation (DNS setup guide, troubleshooting)

---

## 14. Decisions (Approved — MEET-2026-04-05-033)

| # | Decision | Chosen | Alternatives Rejected |
|---|----------|--------|-----------------------|
| D1 | Verification method | TXT for verification + CNAME for routing | CNAME-only (can't verify apex), both (unnecessary complexity) |
| D2 | Traffic routing | CNAME to `cname.loopzero.example` (via CF Tunnel) | A record to LB (loses DDoS protection) |
| D3 | TLS issuance | HTTP-01 via cert-manager (requires spike) | DNS-01 (needs user zone access), CF for SaaS (paid fallback) |
| D4 | State management | DB-only (PostgreSQL) + DomainHealthChecker | New CRD (over-engineering for user-intent metadata) |
| D5 | Hostname coexistence | Keep both auto-generated + custom | Replace auto (loses fallback) |
| D6 | Apex domain support | CNAME only (document ALIAS for apex) | A record support (defeats tunnel) |
| D7 | Re-verification | Weekly CNAME re-check, 14-day grace, IngressRoute removal | Trust-once (security risk), 72h grace (too aggressive) |
| D8 | Scope | Per-app (`--app` flag required) | Per-environment (ambiguous with multi-app projects) |

---

## 15. Open Questions (Resolved — MEET-033)

| # | Question | Resolution |
|---|----------|------------|
| Q1 | `loopzero deploy` shows both URLs? | YES — auto-generated + all active custom domains |
| Q2 | Automatic or explicit verification? | EXPLICIT (`loopzero domain verify`) + background auto-verify for convenience |
| Q3 | Custom domains on project delete? | CASCADE DELETE — rows deleted, manifests cleaned via gitops |
| Q4 | SAN certs or one per domain? | ONE CERT PER DOMAIN — simpler lifecycle |
