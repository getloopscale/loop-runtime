#!/usr/bin/env bash
# T3+T4 LIVE e2e test of plan 2026-06-04-dev-public-domain-canonicalization
#
# Validates Kratos OIDC flow for dev-public env:
#   1. /self-service/login/browser creates flow + redirects to app-dev/login
#   2. POST flow.action with method=oidc + provider={google,github} returns
#      HTTP 303 with Location header pointing at accounts.google.com OR
#      github.com (NOT localhost or pod hostname)
#   3. The redirect_uri query param sent to the OAuth provider equals
#      https://auth-dev.getloopscale.com/self-service/methods/oidc/callback/{provider}
#
# This is a LIVE test (touches https://auth-dev.getloopscale.com). Run after
# `task dev-public:bootstrap` + `LOOPZERO_HELMFILE_ENV=dev-public task setup`.

set -euo pipefail

KRATOS_PUBLIC="${KRATOS_PUBLIC:-https://auth-dev.getloopscale.com}"
DASHBOARD="${DASHBOARD:-https://app-dev.getloopscale.com}"
EXPECTED_CALLBACK_BASE="${EXPECTED_CALLBACK_BASE:-https://auth-dev.getloopscale.com}"

PASS=0
FAIL=0

log() { printf '[oidc-redirect-test] %s\n' "$*" >&2; }

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

# --- Setup ---
TMP_COOKIES=$(mktemp)
trap 'rm -f "$TMP_COOKIES"' EXIT

# --- Step 1: init flow ---
log "Step 1: GET browser login flow"
FINAL_URL=$(curl -fsS -c "$TMP_COOKIES" -L -o /dev/null -w '%{url_effective}' \
  "$KRATOS_PUBLIC/self-service/login/browser?return_to=$DASHBOARD/" || echo "ERR")
log "  Final URL: $FINAL_URL"
# Regex must not interpolate $DASHBOARD into [[ =~ ]] (slash escaping); compare prefix instead.
if [[ "$FINAL_URL" == "$DASHBOARD/login?flow="* ]]; then
  assert "browser flow redirects to dashboard /login?flow=..." 1
else
  assert "browser flow redirects to dashboard /login?flow=..." 0
fi

FLOW_ID=$(printf '%s' "$FINAL_URL" | grep -oE 'flow=[a-f0-9-]+' | head -1 | sed 's/flow=//')

# --- Step 2: fetch flow JSON for csrf + action ---
log "Step 2: fetch flow JSON"
FLOW=$(curl -fsS -b "$TMP_COOKIES" \
  "$KRATOS_PUBLIC/self-service/login/flows?id=$FLOW_ID" \
  -H "Accept: application/json")

ACTION=$(printf '%s' "$FLOW" | python3 -c "import sys,json; print(json.load(sys.stdin)['ui']['action'])")
CSRF=$(printf '%s' "$FLOW" | python3 -c "
import sys,json
d=json.load(sys.stdin)
for n in d.get('ui',{}).get('nodes',[]):
    if n.get('attributes',{}).get('name')=='csrf_token':
        print(n['attributes'].get('value','')); break
")

if [[ "$ACTION" == "$KRATOS_PUBLIC/self-service/login?flow=$FLOW_ID" ]]; then
  assert "flow.action is auth-dev hostname (not pod hostname)" 1
else
  log "    actual action: $ACTION"
  assert "flow.action is auth-dev hostname (not pod hostname)" 0
fi

# --- Step 3+4: POST OIDC for each provider, decode redirect_uri ---
test_provider() {
  local provider="$1" expected_host="$2"

  log "Provider: $provider"
  local resp_file
  resp_file=$(mktemp)

  curl -fsS -b "$TMP_COOKIES" -c "$TMP_COOKIES" -i -o "$resp_file" \
    -X POST "$ACTION" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    --data-urlencode "csrf_token=$CSRF" \
    --data-urlencode "method=oidc" \
    --data-urlencode "provider=$provider" || true

  local status_code
  status_code=$(head -1 "$resp_file" | grep -oE 'HTTP/[0-9.]+ [0-9]+' | awk '{print $2}')

  local location
  location=$(grep -i '^location:' "$resp_file" | head -1 | sed 's/^[Ll]ocation:\s*//' | tr -d '\r')

  if [[ "$status_code" == "303" ]]; then
    assert "$provider POST returns HTTP 303" 1
  else
    log "    actual status: $status_code"
    assert "$provider POST returns HTTP 303" 0
  fi

  # Decode the redirect_uri query param
  local redirect_uri
  redirect_uri=$(printf '%s' "$location" | python3 -c "
import sys, urllib.parse
url = sys.stdin.read().strip()
parsed = urllib.parse.urlparse(url)
qs = urllib.parse.parse_qs(parsed.query)
print(qs.get('redirect_uri',[''])[0])")

  if [[ "$redirect_uri" == "$EXPECTED_CALLBACK_BASE/self-service/methods/oidc/callback/$provider" ]]; then
    assert "$provider redirect_uri uses dev callback ($redirect_uri)" 1
  else
    log "    actual redirect_uri: $redirect_uri"
    assert "$provider redirect_uri uses dev callback" 0
  fi

  # Verify host of Location is the expected OAuth provider
  local loc_host
  loc_host=$(printf '%s' "$location" | python3 -c "
import sys, urllib.parse
print(urllib.parse.urlparse(sys.stdin.read().strip()).netloc)")

  if [[ "$loc_host" == "$expected_host" ]]; then
    assert "$provider Location host is $expected_host" 1
  else
    log "    actual host: $loc_host"
    assert "$provider Location host is $expected_host" 0
  fi

  rm -f "$resp_file"
}

test_provider "google" "accounts.google.com"

# GitHub re-init: each OIDC POST consumes the flow; need fresh one
log "Re-init flow for GitHub provider"
FINAL_URL=$(curl -fsS -c "$TMP_COOKIES" -L -o /dev/null -w '%{url_effective}' \
  "$KRATOS_PUBLIC/self-service/login/browser?return_to=$DASHBOARD/")
FLOW_ID=$(printf '%s' "$FINAL_URL" | grep -oE 'flow=[a-f0-9-]+' | head -1 | sed 's/flow=//')
FLOW=$(curl -fsS -b "$TMP_COOKIES" \
  "$KRATOS_PUBLIC/self-service/login/flows?id=$FLOW_ID" \
  -H "Accept: application/json")
ACTION=$(printf '%s' "$FLOW" | python3 -c "import sys,json; print(json.load(sys.stdin)['ui']['action'])")
CSRF=$(printf '%s' "$FLOW" | python3 -c "
import sys,json
d=json.load(sys.stdin)
for n in d.get('ui',{}).get('nodes',[]):
    if n.get('attributes',{}).get('name')=='csrf_token':
        print(n['attributes'].get('value','')); break
")

test_provider "github" "github.com"

printf '\n[oidc-redirect-test] summary: PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]] || exit 1
