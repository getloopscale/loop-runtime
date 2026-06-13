# J-25 prod-dev wildcard — operator runbook

**Plan:** `knowledge-base/plans/2026-06-12-dashboard-journey-j25-live-url-discoverability-plan.md` ADR D1
**Engineering commits:** `f2d2106c` (helmfile overlay) + `46186e52` (values-dev.yaml 4-layer canonical) + `<NEXT>` (Caddyfile block)
**Related catches:** #21+#22 of T6.2 session addendum 2026-06-12

## Why this runbook exists

J-25 ships dashboard hero card + Visit button + ExternalLink icon that display the canonical production deploy URL emitted by `api/internal/routes/status.go:252` as `https://<project>-<env>.<BASE_DOMAIN>`. With engineering commits above, BASE_DOMAIN in dev-public profile is `prod-dev.loopzero.example` — so URLs render as `teste-production.prod-dev.loopzero.example`.

For the URL to be **clickable end-to-end** (sponsor browser walkthrough):
- ✓ Engineering side DONE: loopzero-api emits canonical URL (4-layer fix shipped)
- ✓ Engineering side DONE: Caddy block ready in committed `infra/local/Caddyfile`
- ⚠ Operator side PENDING: **Cloudflare DNS wildcard A record** (this runbook)
- ⚠ Operator side PENDING: Caddy reload after the DNS record exists

## Step 1 — Cloudflare DNS wildcard A record

Pattern parallel to existing `*.preview-dev.loopzero.example` (J-23 delivered 2026-06-07).

### Via Cloudflare Dashboard

1. Login to Cloudflare → Select zone `loopzero.example`
2. Go to **DNS → Records**
3. Click **Add record**
4. Fill:
   - **Type**: `A`
   - **Name**: `*.prod-dev` (Cloudflare appends `.loopzero.example` automatically; full hostname becomes `*.prod-dev.loopzero.example`)
   - **IPv4 address**: `<droplet IP — currently 165.227.121.20 for loopzero-e2e-runner>`
   - **Proxy status**: **DNS only** (gray cloud — TLS terminated by Caddy on droplet, NOT Cloudflare proxy)
   - **TTL**: Auto
5. Click **Save**

### Via Cloudflare API (preferred for canonical-by-construction)

```bash
# Pre-requisites:
#   - CF_API_TOKEN env var canonical (read from .env operator config)
#   - CF_ZONE_ID for loopzero.example
#   - DROPLET_IP

export CF_API_TOKEN="..." # operator secret
export DROPLET_IP="165.227.121.20"

# Get zone ID for loopzero.example (one-shot)
CF_ZONE_ID=$(curl -sH "Authorization: Bearer $CF_API_TOKEN" \
  "https://api.cloudflare.com/client/v4/zones?name=loopzero.example" \
  | jq -r '.result[0].id')

# Add the wildcard A record
curl -sX POST "https://api.cloudflare.com/client/v4/zones/$CF_ZONE_ID/dns_records" \
  -H "Authorization: Bearer $CF_API_TOKEN" \
  -H "Content-Type: application/json" \
  --data "{
    \"type\":\"A\",
    \"name\":\"*.prod-dev\",
    \"content\":\"$DROPLET_IP\",
    \"ttl\":1,
    \"proxied\":false,
    \"comment\":\"J-25 production deploy URLs — Caddy + Traefik proxy\"
  }" | jq '.success, .errors'
```

Expected output: `true` + empty errors array.

## Step 2 — Verify DNS propagation

```bash
# Should return $DROPLET_IP within ~30s (CF DNS propagates fast)
dig teste-production.prod-dev.loopzero.example +short

# OR
nslookup teste-production.prod-dev.loopzero.example 1.1.1.1
```

## Step 3 — Caddy reload (picks up new wildcard block)

```bash
# task target reloads Caddy without dropping existing connections
task cloud:caddy:reload

# OR manually:
sudo caddy reload --config /etc/caddy/Caddyfile
```

## Step 4 — End-to-end verification (sponsor walkthrough simulation)

```bash
# After Steps 1-3, the URL should resolve + return HTTP 200..399 if there's
# an active deploy. For "no active deploy yet" projects, expect a 404 from
# Traefik (which is correct — proves the chain is wired).

curl -vL https://teste-production.prod-dev.loopzero.example 2>&1 | head -30
# Expected first few lines:
#   *   Trying <droplet IP>:443...
#   * Connected to teste-production.prod-dev.loopzero.example
#   * TLS handshake (LE prod cert via CF DNS-01)
#   > GET / HTTP/1.1
#   > Host: teste-production.prod-dev.loopzero.example
#   < HTTP/1.1 200 OK  (if active deploy exists)
#   < HTTP/1.1 404 Not Found  (if no deploy yet — still proves chain works)
```

## Step 5 — Sponsor LIVE walkthrough (per `cleanroom-up-live-test-mandate.md`)

Per `rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL — após Steps 1-4 confirmed:

1. `task clean:all && task setup` (~30-45min Cleanroom TOTAL)
2. `tilt up &` + `task doctor` (PASS=38+ FAIL=0)
3. `bash infra/scripts/local/seed-admin-loopzero-dev.sh` (canonical dev user seed)
4. Operator deploys `teste` project via loopzero CLI (gives an active_release.build_id)
5. Sponsor opens `https://app-dev.loopzero.example/projects/teste` in browser
6. Sponsor sees:
   - ✓ Live URL hero card showing `https://teste-production.prod-dev.loopzero.example`
   - ✓ Copy button + Visit button
7. Sponsor clicks Visit → opens `https://teste-production.prod-dev.loopzero.example` HTTP 200..399 in new tab
8. Sponsor verbatim approval recorded → emit `<promise>IMPLEMENTATION_COMPLETE</promise>` for J-25

## Rollback

If anything goes wrong:

```bash
# Step 1 rollback: remove CF DNS record
curl -X DELETE "https://api.cloudflare.com/client/v4/zones/$CF_ZONE_ID/dns_records/<RECORD_ID>" \
  -H "Authorization: Bearer $CF_API_TOKEN"

# Step 3 rollback: revert Caddyfile commit
git revert <NEXT-commit-sha>
task cloud:caddy:reload
```

## Cross-references

- Memory: `feedback_tilt_owned_deployment_yaml_drift` (4-layer canonical pattern)
- Memory: `reference_t62_cleanroom_session_2026_06_12_addendum` (catches #21+#22)
- Plan: `knowledge-base/plans/2026-06-12-dashboard-journey-j25-live-url-discoverability-plan.md`
- Existing pattern: J-23 `*.preview-dev.loopzero.example` (`infra/local/Caddyfile` lines 228-240)
- Mandate: `rules/cleanroom-up-live-test-mandate.md`
- Hermetic gate: `infra/tests/helm/loopzero-api-base-domain-per-env_test.sh` (10 assertions A1-A10)
