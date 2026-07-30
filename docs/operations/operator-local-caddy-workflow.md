# Operator-local Caddy workflow (app-dev.loopzero.example)

> Plan `caddyfile-app-dev-prod-bundle-file-server-canonical` v1 — closes Red Team loopzero-cloud#66.
> Supersedes the prior plan `caddyfile-csp-vite-dev-vs-prod-bundle` v1.1 workflow (which mistakenly served Vite dev publicly).

## Why this exists

The operator-local Caddy (`task cloud:caddy:up`) IS publicly accessible via Cloudflare DNS — `app-dev.loopzero.example` resolves to the operator's droplet. Therefore the public surface MUST serve the production-bundled dashboard (`dashboard/dist/`), NOT the Vite dev server which would leak source TypeScript + HMR runtime + `node_modules/.vite/` cache publicly.

## Required env var

Caddyfile reads `DASHBOARD_DIST` (with fallback `./dashboard-dist` if unset). Set it to the absolute path of the freshly-built `dashboard/dist/` directory:

```bash
export DASHBOARD_DIST="$(cd ~/loopzero-cloud/loopzero-cloud/dashboard/dist && pwd)"
```

## Build-before-serve sequence

```bash
# 1. Build the prod-bundle (or `npm install && npm run build` from a fresh clone)
cd ~/loopzero-cloud/loopzero-cloud/dashboard
npm run build

# 2. Set env var (re-confirm path after build)
export DASHBOARD_DIST="$(cd dist && pwd)"
ls -la "$DASHBOARD_DIST/index.html"   # should print the freshly-built index.html

# 3. Start (or reload) Caddy
cd ~/loopzero-cloud/loopzero
sudo caddy reload --config infra/local/Caddyfile
#  — OR for cold start —
sudo task cloud:caddy:up
```

## Verifying

```bash
# Should return HTTP 200 + canonical CSP + 4 preserved security headers + NO Vite-source paths
curl -sI https://app-dev.loopzero.example/

# Should return HTTP 404 — Vite source files don't exist in dashboard/dist
curl -sI https://app-dev.loopzero.example/src/main.tsx
curl -sI https://app-dev.loopzero.example/@vite/client
curl -sI https://app-dev.loopzero.example/node_modules/.vite/deps/react.js

# Should return HTTP 200 with index.html body (SPA client-side route fallback)
curl -s https://app-dev.loopzero.example/device-verify | grep -q "<title>"
```

## Operator iteration loop (dashboard code changes)

When iterating on the dashboard locally:

- **For HMR fast feedback:** open `http://localhost:5173/` directly (Vite dev server). This path NEVER goes through Caddy + Cloudflare; safe to use unbundled.
- **For pre-deploy LIVE testing:** `npm run build` in `loopzero-cloud/dashboard/`, then Caddy `file_server` picks up the new `dist/` files automatically — no reload needed for static files.

## Anti-patterns (do NOT do)

- ❌ Re-introduce `reverse_proxy @ws localhost:5173` in the `app-dev.loopzero.example` block — caught by `cmd/loopzero-ops/internal/quality/caddyfilecspgate` Go gate, fails `task quality:gates`.
- ❌ Skip `npm run build` and hope `dashboard-dist/` from a previous run still works — operator runbook explicitly requires the build step pre-Caddy-up.
- ❌ Remove the canonical CSP from the `header { ... }` block (D2 invariant; caught by the gate).
- ❌ Use Vite dev as the public Caddy upstream "because HMR is convenient" — Red Team #66 was filed for exactly this regression.

## Cross-references

- Plan: `.claude/knowledge-base/plans/caddyfile-app-dev-prod-bundle-file-server-canonical-plan.md`
- Go gate: `cmd/loopzero-ops/internal/quality/caddyfilecspgate/` (asserts the canonical shape)
- Caddyfile: `infra/local/Caddyfile § app-dev.loopzero.example`
- Red Team issue: getloopscale/loopzero-cloud#66
- Prior plan superseded: `.claude/knowledge-base/plans/caddyfile-csp-vite-dev-vs-prod-bundle-plan.md` v1.1 (ADR D2 was wrong)
