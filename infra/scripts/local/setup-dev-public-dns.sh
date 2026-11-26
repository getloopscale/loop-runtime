#!/usr/bin/env bash
# T0.1 of plan 2026-06-04-dev-public-domain-canonicalization-plan.md
# Provisions 3 A records on getloopscale.com zone for dev-public env:
#   auth-dev.getloopscale.com  -> $DROPLET_IP
#   app-dev.getloopscale.com   -> $DROPLET_IP
#
# proxied=false because Caddy on droplet terminates TLS via Let's Encrypt
# HTTP-01 challenge. Cloudflare proxy would intercept :80 and break ACME.
#
# Idempotent: re-running with same IP is no-op via API PATCH. Changing
# DROPLET_IP issues PATCH to update content.
#
# Inputs (env vars):
#   CLOUDFLARE_API_TOKEN  — token with Zone:DNS:Edit on getloopscale.com
#   CF_LOOP_RUNTIME_DEV_ZONE_ID — zone id for getloopscale.com (default committed below)
#   DROPLET_IP            — public IPv4 of this dev droplet
#
# Flags:
#   --dry-run     show what would change without API calls
#   --verify      after upsert, dig +short each FQDN to confirm propagation

set -euo pipefail

DRY_RUN=0
VERIFY=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    --verify) VERIFY=1; shift ;;
    -h|--help) sed -n '2,/^set -euo/p' "$0"; exit 0 ;;
    *) printf 'unknown flag: %s\n' "$1" >&2; exit 1 ;;
  esac
done

: "${CLOUDFLARE_API_TOKEN:?CLOUDFLARE_API_TOKEN required (export from .env)}"
: "${CF_LOOP_RUNTIME_DEV_ZONE_ID:=9f9a41bef0bcb7adf15969c81c303bef}"
: "${DROPLET_IP:?DROPLET_IP required (e.g. DROPLET_IP=165.227.121.20)}"

ZONE="$CF_LOOP_RUNTIME_DEV_ZONE_ID"
TOKEN="$CLOUDFLARE_API_TOKEN"

log() { printf '[setup-dev-public-dns] %s\n' "$*" >&2; }

api() {
  curl -fsS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" "$@"
}

upsert_record() {
  local sub="$1"
  local fqdn="$sub.getloopscale.com"

  local existing
  existing=$(api "https://api.cloudflare.com/client/v4/zones/$ZONE/dns_records?name=$fqdn&type=A" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); r=d.get('result',[]); print(r[0]['id'] if r else '')")

  local body
  body=$(printf '{"type":"A","name":"%s","content":"%s","ttl":1,"proxied":false,"comment":"dev-public env (plan 2026-06-04)"}' \
    "$sub" "$DROPLET_IP")

  if [[ $DRY_RUN -eq 1 ]]; then
    if [[ -n "$existing" ]]; then
      log "would PATCH $fqdn (id=$existing) -> $DROPLET_IP"
    else
      log "would POST $fqdn -> $DROPLET_IP"
    fi
    return
  fi

  local resp
  if [[ -n "$existing" ]]; then
    resp=$(api -X PATCH "https://api.cloudflare.com/client/v4/zones/$ZONE/dns_records/$existing" -d "$body")
  else
    resp=$(api -X POST "https://api.cloudflare.com/client/v4/zones/$ZONE/dns_records" -d "$body")
  fi

  printf '%s' "$resp" | python3 -c "
import sys,json
d=json.load(sys.stdin)
if not d.get('success'):
    print('CF API error for record:', d.get('errors'), file=sys.stderr); sys.exit(1)
r=d.get('result',{})
print(f\"  {r.get('name','?'):35s} {r.get('content','?'):20s} (id={r.get('id','?')})\")"
}

main() {
  log "zone=$ZONE droplet_ip=$DROPLET_IP dry_run=$DRY_RUN verify=$VERIFY"
  for sub in auth-dev app-dev; do
    upsert_record "$sub"
  done

  if [[ $VERIFY -eq 1 && $DRY_RUN -eq 0 ]]; then
    log "waiting 10s for propagation..."
    sleep 10
    local all_ok=1
    for sub in auth-dev app-dev; do
      local got
      got=$(dig +short "$sub.getloopscale.com" @1.1.1.1 2>&1 | head -1)
      if [[ "$got" == "$DROPLET_IP" ]]; then
        log "  $sub.getloopscale.com -> $got OK"
      else
        log "  $sub.getloopscale.com -> '$got' WRONG (expected $DROPLET_IP)"
        all_ok=0
      fi
    done
    [[ $all_ok -eq 1 ]] || exit 1
  fi
}

main "$@"
