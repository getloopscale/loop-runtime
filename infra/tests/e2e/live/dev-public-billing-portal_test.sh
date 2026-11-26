#!/usr/bin/env bash
# T4 LIVE e2e test of plan 2026-06-04-dev-public-domain-canonicalization
#
# Validates the full chain login → JWT exchange → /v1/billing/portal returns
# a Stripe Customer Portal URL. Regression-protection for the 2026-06-04
# bug where LOOPRUNTIME_STRIPE_SECRET_KEY in .env was sk_test_*SX4hL (rotated/
# invalid) while the working canonical key is rk_test_*TZGhm — cloud
# returned 401 from Stripe API. Operator browser-validated by clicking
# "Manage Plan" + receiving "Failed to open billing portal".
#
# Flow:
#   1. POST /self-service/login/browser → flow id
#   2. POST flow.action with admin@getloopscale.com password → cookie set
#   3. POST cloud-dev/v1/auth/session/token → JWT
#   4. GET cloud-dev/v1/billing/portal with Bearer JWT → expects
#      billing.stripe.com/p/session/* URL
#
# Env overrides (defaults to dev-public):
#   KRATOS_PUBLIC      default https://auth-dev.getloopscale.com
#   CLOUD_HOST         default https://cloud-dev.getloopscale.com
#   TEST_USER          default admin@getloopscale.com
#   TEST_PASSWORD      default loopruntime-dev-password
#
# Exit: 0 if Stripe portal URL returned; 1 otherwise (with diagnostic).

set -euo pipefail

KRATOS_PUBLIC="${KRATOS_PUBLIC:-https://auth-dev.getloopscale.com}"
CLOUD_HOST="${CLOUD_HOST:-https://app-dev.getloopscale.com}"
TEST_USER="${TEST_USER:-admin@getloopscale.com}"
TEST_PASSWORD="${TEST_PASSWORD:-loopruntime-dev-password}"

PASS=0
FAIL=0

log() { printf '[billing-portal-test] %s\n' "$*" >&2; }

assert() {
  local desc="$1" cond="$2"
  if [[ "$cond" == "1" ]]; then
    printf '  PASS: %s\n' "$desc"
    PASS=$((PASS + 1))
  else
    printf '  FAIL: %s\n' "$desc"
    FAIL=$((FAIL + 1))
  fi
}

TMP_COOKIES=$(mktemp)
trap 'rm -f "$TMP_COOKIES" /tmp/billing-portal-flow.json /tmp/billing-portal-login.json /tmp/billing-portal-jwt.json /tmp/billing-portal-resp.json' EXIT

# --- Step 1: init flow ---
log "Step 1: init login flow"
curl -fsS -c "$TMP_COOKIES" \
  "$KRATOS_PUBLIC/self-service/login/browser" \
  -H "Accept: application/json" \
  -o /tmp/billing-portal-flow.json || true

FLOW_ID=$(python3 -c "import json; print(json.load(open('/tmp/billing-portal-flow.json')).get('id',''))")
CSRF=$(python3 -c "
import json
d = json.load(open('/tmp/billing-portal-flow.json'))
for n in d.get('ui',{}).get('nodes',[]):
    if n.get('attributes',{}).get('name')=='csrf_token':
        print(n['attributes'].get('value','')); break
")

if [[ -n "$FLOW_ID" && -n "$CSRF" ]]; then
  assert "flow created with csrf" 1
else
  assert "flow created with csrf" 0
  printf '\n[billing-portal-test] summary PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
  exit 1
fi

# --- Step 2: login ---
log "Step 2: login as $TEST_USER"
curl -fsS -b "$TMP_COOKIES" -c "$TMP_COOKIES" -X POST \
  "$KRATOS_PUBLIC/self-service/login?flow=$FLOW_ID" \
  -H "Content-Type: application/json" \
  -d "{\"method\":\"password\",\"identifier\":\"$TEST_USER\",\"password\":\"$TEST_PASSWORD\",\"csrf_token\":\"$CSRF\"}" \
  -o /tmp/billing-portal-login.json -w "%{http_code}\n" > /tmp/billing-portal-status.txt

LOGIN_CODE=$(cat /tmp/billing-portal-status.txt)
SESSION_OK=$(python3 -c "
import json
d = json.load(open('/tmp/billing-portal-login.json'))
print('1' if d.get('session',{}).get('active') else '0')
" 2>/dev/null || echo "0")

if [[ "$LOGIN_CODE" == "200" && "$SESSION_OK" == "1" ]]; then
  assert "login HTTP 200 with active session" 1
else
  assert "login HTTP 200 with active session" 0
  log "  login http=$LOGIN_CODE session_active=$SESSION_OK"
  python3 -c "
import json
d = json.load(open('/tmp/billing-portal-login.json'))
for m in d.get('ui',{}).get('messages',[]):
    print(f\"  kratos msg: {m.get('text','')[:200]}\")
" 2>/dev/null
  printf '\n[billing-portal-test] summary PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
  exit 1
fi

# --- Step 3: JWT exchange ---
log "Step 3: exchange Kratos session for JWT"
curl -fsS -b "$TMP_COOKIES" -X POST \
  "$CLOUD_HOST/v1/auth/session/token" \
  -o /tmp/billing-portal-jwt.json -w "%{http_code}\n" > /tmp/billing-portal-status.txt || true

JWT_CODE=$(cat /tmp/billing-portal-status.txt)
JWT=$(python3 -c "import json; print(json.load(open('/tmp/billing-portal-jwt.json')).get('access_token',''))")

if [[ "$JWT_CODE" == "200" && -n "$JWT" ]]; then
  assert "JWT exchange HTTP 200 with access_token (len=${#JWT})" 1
else
  assert "JWT exchange HTTP 200 with access_token" 0
  log "  http=$JWT_CODE body_head: $(head -c 200 /tmp/billing-portal-jwt.json)"
  printf '\n[billing-portal-test] summary PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
  exit 1
fi

# --- Step 4: billing portal ---
log "Step 4: GET /v1/billing/portal with JWT"
curl -fsS "$CLOUD_HOST/v1/billing/portal" \
  -H "Authorization: Bearer $JWT" \
  -o /tmp/billing-portal-resp.json -w "%{http_code}\n" > /tmp/billing-portal-status.txt || true

PORTAL_CODE=$(cat /tmp/billing-portal-status.txt)
PORTAL_URL=$(python3 -c "import json; print(json.load(open('/tmp/billing-portal-resp.json')).get('url',''))" 2>/dev/null || echo "")

if [[ "$PORTAL_CODE" == "200" ]]; then
  assert "billing portal HTTP 200" 1
else
  assert "billing portal HTTP 200" 0
  log "  body head: $(head -c 200 /tmp/billing-portal-resp.json)"
fi

if [[ "$PORTAL_URL" =~ ^https://billing\.stripe\.com/p/session/ ]]; then
  assert "portal URL is billing.stripe.com/p/session/* (Stripe API working)" 1
else
  assert "portal URL is billing.stripe.com/p/session/* (Stripe API working)" 0
  log "  actual URL: $PORTAL_URL"
fi

printf '\n[billing-portal-test] summary PASS=%d FAIL=%d\n' "$PASS" "$FAIL"

[[ "$FAIL" -eq 0 ]] || exit 1
