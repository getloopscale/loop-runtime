# Custom Domains — Integration into Core

> Module exists in `customdomain/`. Needs integration into main LoopZero.
> Estimated effort: 1 week.

---

## Problem

Custom domain support exists as a separate module (`customdomain/`) with its own API routes, validation, and DNS verification. But it's not wired into the core LoopZero API or CLI. Users cannot add custom domains via `loopzero` CLI or the dashboard.

## Solution

Integrate the custom domain module into the main API and CLI.

## Tasks

### 1. Integrate domain API routes

**Files:**
- Move/adapt: `customdomain/api/internal/routes/domains.go` → `api/internal/routes/domains.go`
- Edit: `api/internal/server/server.go` — register domain routes

**Endpoints:**
- `POST /api/v1/projects/{name}/domains` — add custom domain
- `GET /api/v1/projects/{name}/domains` — list domains
- `DELETE /api/v1/projects/{name}/domains/{domain}` — remove domain

**DoD:**
- [ ] Domain CRUD endpoints registered on engine API
- [ ] Domain validation (format, no reserved domains, DNS verification)
- [ ] Requires Pro plan or higher
- [ ] Test: add domain → returns pending verification status

### 2. DNS verification flow

**DoD:**
- [ ] User adds domain → system returns CNAME target
- [ ] Background job checks DNS propagation
- [ ] Once verified: TLS cert issued via cert-manager
- [ ] Domain status: `pending` → `verifying` → `active` → `error`
- [ ] Dashboard shows verification instructions

### 3. CLI command — `loopzero domain`

**Files:**
- New: `cli/src/commands/domain.ts`

**Commands:**
- `loopzero domain add example.com`
- `loopzero domain list`
- `loopzero domain verify example.com`
- `loopzero domain remove example.com`

**DoD:**
- [ ] `loopzero domain add` returns CNAME instructions
- [ ] `loopzero domain list` shows status of all domains
- [ ] `loopzero domain verify` triggers verification check
- [ ] `--json` output for all commands

### 4. IngressRoute generation

**DoD:**
- [ ] Verified domain gets Traefik IngressRoute on runtime cluster
- [ ] TLS certificate auto-provisioned via Let's Encrypt
- [ ] Traffic routed to correct service in correct namespace
- [ ] Domain removal cleans up IngressRoute and cert
