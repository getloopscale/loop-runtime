# Local-Public Dashboard Demo (Caddy + k3d)

> T7.4 of plan `2026-05-29-dashboard-public-integration-canonicalization`.

This runbook documents how to bring up a fully functional LoopZero dashboard on a
public-DNS hostname (`app/cloud/auth.loopzero.example`) backed by local k3d
clusters and a Caddy edge providing Let's Encrypt TLS. Use this for:

- **End-to-end product demos** without DOKS provisioning costs.
- **Stripe integration validation** (real `stripe trigger` events).
- **OIDC dev** with browser-real cookie flow (cookie scoped to `loopzero.example`).
- **Audit-trail and onboarding rehearsals** with seeded data visible across all dashboard pages.

For ephemeral local dev (no public DNS), use `task setup` + `tilt up` instead.

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

- Plan: `.claude/knowledge-base/plans/2026-05-29-dashboard-public-integration-canonicalization-plan.md`
- Helmfile env: `infra/helmfile/environments/standalone-public.yaml`
- Caddy config: `infra/local/Caddyfile`
- Port-forward script: `infra/scripts/local/port-forward-public-dashboard.sh`
- Seed script: `infra/scripts/local/seed-demo-data.sh`
- Recovery script: `infra/scripts/local/recover-mgmt-degraded.sh`
