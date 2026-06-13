# Local-Public Dashboard Demo (Caddy + k3d)

> T7.4 of plan `2026-05-29-dashboard-public-integration-canonicalization`.
>
> **2026-06-04 — dev-public canonicalization update** (plan
> `2026-06-04-dev-public-domain-canonicalization`): legacy
> `app/cloud/auth.loopzero.example` continue to work and are reserved for future
> prod cutover. The CANONICAL dev infra for this droplet now uses the
> `-dev` subdomain pattern (`app-dev/cloud-dev/auth-dev.loopzero.example`).
> Run `task dev-public:bootstrap` (instead of the multi-step manual sequence
> below) for the dev-public flow.
>
> **2026-06-04 — task setup:local soft-deprecation** (D2 of plan
> 2026-06-04): provisioning a local k3d cluster on the developer's personal
> laptop produces too much divergence vs production (no public DNS, no LE
> TLS, no Stripe webhook delivery, OAuth callbacks broken). The canonical
> dev workflow is now `ssh` into the dev droplet + `task dev-public:bootstrap`.
> The legacy `task setup` continues to work for component-isolated debug but
> is no longer the documented dev entry point.

This runbook documents how to bring up a fully functional LoopZero dashboard on a
public-DNS hostname (`app/cloud/auth.loopzero.example` legacy or
`app-dev/cloud-dev/auth-dev.loopzero.example` canonical) backed by local k3d
clusters and a Caddy edge providing Let's Encrypt TLS. Use this for:

- **End-to-end product demos** without DOKS provisioning costs.
- **Stripe integration validation** (real `stripe trigger` events).
- **OIDC dev** with browser-real cookie flow (cookie scoped to `loopzero.example`).
- **Audit-trail and onboarding rehearsals** with seeded data visible across all dashboard pages.

For component-isolated debug (no public DNS needed), legacy `task setup` +
`tilt up` still works but is no longer the canonical dev flow.

---

## Prerequisites (one-time droplet setup)

1. A host with a public IP and `:80` + `:443` reachable from the Internet.
2. Cloudflare DNS A records pointing `app.loopzero.example`, `cloud.loopzero.example`,
   and `auth.loopzero.example` at the host IP.
3. Caddy >= 2.7 installed (`$HOME/bin/caddy` works; the task uses `command -v caddy` first).
4. `stripe` CLI installed AND `stripe login` already run (one-time).
   v1.1 EC-D2: `task cloud:stripe:listen` pre-checks this.
5. `kubectl`, `helmfile`, `helm`, `task`, `k3d`, `psql` available on PATH.
6. **Sudo for Caddy** — Caddy binds :80/:443; the operator must allow `sudo`
   without TTY prompt for the `caddy start --config` command.
   v1.1 EC-D5: documented requirement.

## Cold boot (~25 minutes)

```bash
# 1. Recover or provision the k3d clusters
task k3d:up                                 # 4 clusters (mgmt+build+workload+runtime-2)

# 2. Install the 16 mgmt-cluster releases the dashboard demo needs.
#    Skips harbor/opencost/chaos-mesh/monitoring/loki/tempo/otel/blackbox/etc.
#    v1.1 EC-01 guards against empty selector + v1.1 EC-02 includes the
#    platform/preview operators that are essential for tenant provisioning.
task infra:install:mgmt-dashboard-only ENV=standalone-public

# 3. Bring up the public-flow plumbing (port-forwards + Caddy + seed data)
task cloud:port-forward:up                  # 4 persistent port-forwards w/ backoff
task cloud:caddy:up                         # binds :80/:443 (sudo)
task demo:seed                              # 2 projects + 8 builds + 5 releases

# 4. (Optional) Stripe webhook validation. Run in a separate terminal:
task cloud:stripe:listen
#    Copy the printed whsec_* and store it as a K8s Secret:
WHSEC='<paste whsec here>'
kubectl --context k3d-loopzero-mgmt -n loopzero-system create secret generic loopzero-stripe-webhook \
  --from-literal=STRIPE_WEBHOOK_SECRET="$WHSEC" \
  --dry-run=client -o yaml | kubectl --context k3d-loopzero-mgmt apply -f -
kubectl --context k3d-loopzero-mgmt -n loopzero-system rollout restart deploy/loopzero-cloud

# 5. Bring up vite dev server for the dashboard (separate terminal)
cd cloud/dashboard && npm run dev           # binds :5173
```

After step 5, open `https://app.loopzero.example` in a browser.

## Daily start (post-reboot, ~3 minutes)

If the droplet rebooted, k3d clusters survive (state on disk) but everything
else must be restarted:

```bash
task cloud:port-forward:up
task cloud:caddy:up
# vite dev (separate terminal)
cd cloud/dashboard && npm run dev
```

## Validation

```bash
# Reachability + TLS
curl -I https://app.loopzero.example/
curl     https://cloud.loopzero.example/health
curl     https://auth.loopzero.example/health/alive

# Login flow + JWT exchange + engine proxy
bash infra/tests/e2e/dashboard-engine-proxy_test.sh
# Expected: HTTP 200 + 2 projects (api-gateway, web-app)

# All 9 dashboard routes
bash infra/tests/e2e/dashboard-pages-public-smoke_test.sh
# Expected: 9 routes return 200/304

# Stripe webhook (requires stripe listen running)
bash infra/tests/e2e/stripe-webhook-checkout-completed_test.sh
# Expected: subscriptions.plan or audit_log row updated
```

## Test credentials

| Field | Value |
|-------|-------|
| Email | `paulo@loopzero.test` |
| Password | `LoopZeroDev2026!` |
| Identity ID | `3095bb15-0b35-47c2-b6d6-3af72676f40d` |
| Tenant ID | `tn-8a5caa142b958008` |
| Stripe test cards | `4242 4242 4242 4242` (any future date, any CVC) |

The Stripe API key is a **restricted test key** (`rk_test_*`) and grants
full access to the Stripe test environment. Never expose it in logs.

## Pages with seed data after `task demo:seed`

After running `task demo:seed`, the following dashboard pages show meaningful content:

| Route | Content |
|-------|---------|
| `/` (Overview) | 2 projects + recent activity summary |
| `/projects` | `api-gateway`, `web-app` |
| `/projects/api-gateway` | 5 builds (3 SUCCEEDED, 1 FAILED, 1 RUNNING), 3 releases (1 ACTIVE) |
| `/projects/web-app` | 3 builds, 2 releases |
| `/audit` | 12 entries |
| `/environments` | production targets for both projects |
| `/billing` | Pro plan + usage limits |
| `/settings` | profile + LoopZero session |

## Shutdown

```bash
task cloud:caddy:down
task cloud:port-forward:down
# Optional: stop vite (Ctrl-C the dev process)
# Clusters keep running; use `task k3d:down` to free CPU/RAM completely.
```

## Troubleshooting

| Symptom | Diagnosis | Fix |
|---------|-----------|-----|
| `curl https://cloud.loopzero.example/health` HTTP 502 | port-forward dropped (pod recreated) | `task cloud:port-forward:status` — supervisor should auto-retry; if PID file empty, `task cloud:port-forward:up` |
| `/v1/auth/session/token` returns `TENANT_ERROR 500` | `users.username NOT NULL` violation (pre-Apr 2026 schema) | This bug was fixed in commit `a9243d47`. Pull and `task k3d:recover-degraded` |
| `/engine/api/v1/projects` returns 401 `Invalid service authentication` | `LOOPZERO_SERVICE_AUTH_SECRET` drift between cloud and api | T1.1 of this plan fixes via shared Secret. `kubectl get secret loopzero-service-auth` and verify both deployments use envFrom |
| Browser blocks `app.loopzero.example` with CORS error on XHR to `auth.loopzero.example` | Kratos CORS config missing | T2.1 of this plan adds CORS to chart. Verify via `helm template` or `kubectl get cm ory-kratos-config -o yaml | grep cors` |
| `task cloud:caddy:up` reports "port 80 in use" | nginx/apache on droplet, OR previous caddy instance | `task cloud:caddy:down` first; if still bound, `sudo ss -tlnp \| grep :80` and stop the conflicting service |
| `task cloud:stripe:listen` exits with "stripe CLI not configured" | one-time setup not done | `stripe login` (interactive — copies a code to a browser) |
| `task demo:seed` exits 1 with "user paulo@loopzero.test does not exist" | tenant not yet provisioned | Log in once via the dashboard (POST `/v1/auth/session/token`), then re-run seed |

## Service-auth rotation

See [service-auth-rotation.md](./service-auth-rotation.md). Zero-downtime via
`LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS` window.

## Cross-references

- Plan (parent): `.claude/knowledge-base/plans/2026-05-29-dashboard-public-integration-canonicalization-plan.md`
- Plan (dev-public): `.claude/knowledge-base/plans/2026-06-04-dev-public-domain-canonicalization-plan.md`
- Helmfile env (legacy): `infra/helmfile/environments/standalone-public.yaml`
- Helmfile env (dev-public): `infra/helmfile/environments/dev-public.yaml`
- Caddy config: `infra/local/Caddyfile`
- DNS bootstrap: `infra/scripts/local/setup-dev-public-dns.sh`
- Caddy bootstrap: `infra/scripts/local/caddy-up-dev-public.sh`
- Caddy DNS-01 build (preview wildcard TLS): `infra/scripts/local/caddy-build-with-cloudflare.sh`
- Port-forward script: `infra/scripts/local/port-forward-public-dashboard.sh`
- Seed script: `infra/scripts/local/seed-demo-data.sh`
- Recovery script: `infra/scripts/local/recover-mgmt-degraded.sh`
- Legacy-namespace migration: `infra/scripts/local/migrate-legacy-namespaces.sh`

### Preview wildcard TLS (J-23 #68, 2026-06-07)

The `*.preview-dev.loopzero.example` wildcard in `infra/local/Caddyfile` uses
DNS-01 (Cloudflare TXT challenge) instead of HTTP-01. Per-host HTTP-01
against a wildcard hits LE's "50 certs/week per Registered Domain" rate
limit within a single dogfood session — DNS-01 is the only path that
scales to per-PR previews.

Stock Caddy ships without DNS providers. Build the augmented binary once:

```bash
bash infra/scripts/local/caddy-build-with-cloudflare.sh   # idempotent
caddy list-modules | grep dns.providers.cloudflare       # must print module
```

At runtime, `CF_API_TOKEN` (Zone:DNS:Edit on loopzero.example) MUST be in the
Caddy process env. Verify the token first:

```bash
curl -fsS https://api.cloudflare.com/client/v4/user/tokens/verify \
  -H "Authorization: Bearer $CF_API_TOKEN"
```

Then export and reload:

```bash
export CF_API_TOKEN=…
task cloud:caddy:up
```

LE issues a single wildcard cert that covers every existing AND future
PR preview hostname — zero per-PR config thereafter.

---

## Dev-public quick start (2026-06-04 canonical)

For the operator droplet (`loopzero-e2e-runner`), the canonical bootstrap
sequence is:

```bash
# 1. Populate env (one-time on droplet — copy .env from secure source)
export $(grep -E '^LOOPZERO_|^CLOUDFLARE_|^DROPLET_IP' /home/paulo/loopzero/.env | xargs)
export DROPLET_IP=165.227.121.20

# 2. Provision DNS + Caddy + (next) helmfile env=dev-public
task dev-public:bootstrap

# 3. Verify TLS + DNS green
infra/scripts/local/caddy-up-dev-public.sh --verify

# 4. Wait for k3d clusters + run platform sync
LOOPZERO_HELMFILE_ENV=dev-public task setup

# 5. Visit https://app-dev.loopzero.example/login
```

## Pending operator actions (Phase 3+4 of plan 2026-06-04)

These cannot be automated; they require browser access to provider consoles.
Operator action ledger:

### Phase 3 — Dev OAuth apps

1. **GitHub**: https://github.com/settings/developers → New OAuth App
   - Application name: `LoopZero (dev)`
   - Homepage URL: `https://app-dev.loopzero.example`
   - Callback URL: `https://auth-dev.loopzero.example/self-service/methods/oidc/callback/github`
   - Save Client ID + Client Secret in `.env` as `LOOPZERO_GITHUB_CLIENT_ID_DEV`
     + `LOOPZERO_GITHUB_CLIENT_SECRET_DEV`.

2. **Google**: https://console.cloud.google.com/apis/credentials
   - Create new OAuth client (Web application)
   - JS origins: `https://auth-dev.loopzero.example`, `https://app-dev.loopzero.example`
   - Redirect URI: `https://auth-dev.loopzero.example/self-service/methods/oidc/callback/google`
   - Save Client ID + Client Secret as `LOOPZERO_GOOGLE_CLIENT_ID_DEV`
     + `LOOPZERO_GOOGLE_CLIENT_SECRET_DEV`.

3. Re-run `LOOPZERO_HELMFILE_ENV=dev-public task setup` so helmfile sync renders
   the ConfigMap with the new `*_DEV` env vars. `dev-public.yaml` already
   wires these via env interp.

### Phase 4 — Dev Stripe webhook endpoint

1. **Stripe Dashboard** (Test mode): https://dashboard.stripe.com/test/webhooks
   - Add endpoint URL: `https://cloud-dev.loopzero.example/v1/webhooks/stripe`
   - Events: `checkout.session.completed`, `customer.subscription.created`,
     `customer.subscription.updated`, `customer.subscription.deleted`,
     `invoice.paid`
   - Save webhook secret as `LOOPZERO_STRIPE_WEBHOOK_SECRET_DEV` in `.env`.

2. Create K8s Secret:
   ```bash
   kubectl --context k3d-loopzero-mgmt -n loopzero-system create secret generic loopzero-stripe-webhook-dev \
     --from-literal=STRIPE_WEBHOOK_SECRET="$LOOPZERO_STRIPE_WEBHOOK_SECRET_DEV" \
     --dry-run=client -o yaml | kubectl apply -f -
   ```

3. Verify with `stripe trigger checkout.session.completed
   --webhook-endpoint we_xxx` and tail `kubectl logs -n loopzero-system
   deployment/loopzero-cloud` for `webhook event processed`.

### Phase 8 — LIVE end-to-end smoke

After Phase 3+4 above:

1. Browser: `https://app-dev.loopzero.example/login` → Continue with Google → autoriza
   → redirect ao dashboard com sessão válida.
2. Browser: `https://app-dev.loopzero.example/billing` → Upgrade to Pro → checkout
   → DB row em `subscriptions` muda para `plan=pro`.
3. Browser: `https://app-dev.loopzero.example/verification` → Resend → email chega
   na inbox via Resend.
4. Trio LIVE: `/cleanroom --env=dev-public` + `/dogfood --env=dev-public` +
   `/chaos-test --env=dev-public` + `/live-proof --env=dev-public` all GREEN.

---

## Canonical dev user — `admin@loopzero.example`

Since 2026-06-05 (memory `project_canonical_dev_user`),
`admin@loopzero.example` is the canonical user for all dev/dev-public/dogfood/
live-proof/e2e validation against `*-dev.loopzero.example`.

Seed (or re-seed, or re-mint the session) idempotently via:

```bash
# Full seed: ensures Kratos identity + 3 PG tables (tenant_members +
# user_identity_map + active_tenant_id) + fresh session token; verifies
# the session against /sessions/whoami and /engine/api/v1/projects.
bash infra/scripts/local/seed-admin-loopzero-dev.sh --verify

# Re-mint only: skips PG mutations + identity-create; just re-mints the
# session token. Use this when a test hits 401 mid-run because the
# session expired (lifespan 24h per Kratos config).
bash infra/scripts/local/seed-admin-loopzero-dev.sh --refresh-only --verify
```

The session token is persisted to `~/.loopzero/admin-loopzero-dev-session.txt`
(mode `0600`). Tests consume it via:

```bash
TOKEN=$(cat ~/.loopzero/admin-loopzero-dev-session.txt)
curl -b "ory_kratos_session=${TOKEN}" https://cloud-dev.loopzero.example/engine/api/v1/projects
# → 200 with the admin tenant's projects
```

If `/engine/api/v1/projects` returns **403 NO_ACTIVE_TENANT** after
running the seed, the EC-3 auxiliary tables are out of sync — re-run
`seed-admin-loopzero-dev.sh` without `--refresh-only` to rebuild them.

T0.2 of plan
`.claude/knowledge-base/plans/2026-06-08-pre-release-test-rigor-plan.md`
v1.2 ships this seed as the canonical entry point that all Cenário 1-4
test scripts depend on; the hermetic structural test lives at
`infra/tests/check-seed-admin-loopzero-dev_test.sh` (31 asserts).
