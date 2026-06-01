#!/usr/bin/env bash
# T5.1 of plan 2026-05-29-dashboard-public-integration-canonicalization
#
# LIVE E2E: login paulo@loopzero.test → exchange Kratos session for cloud JWT →
# hit /engine/api/v1/projects → assert HTTP 200 + 2 seeded projects.
#
# Prereqs (operator-managed):
#   - Caddy up, port-forwards up, seed populated
#   - loopzero-service-auth Secret aligned across cloud and api (T1.1)
#   - mgmt cluster shows app=loopzero-cloud + app=loopzero-api Ready
#
# v1.1 EC-D3: JWT TTL is 15min — this test takes <30s, fits in 1 mint.
# v1.1 dependency: test will SKIP (exit 77) if seed data missing (T6.1).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

CLOUD_URL="${CLOUD_URL:-https://cloud.loopzero.example}"
AUTH_URL="${AUTH_URL:-https://auth.loopzero.example}"
EMAIL="${EMAIL:-paulo@loopzero.test}"
PASSWORD="${PASSWORD:-LoopZeroDev2026!}"
TENANT_ID="${TENANT_ID:-tn-8a5caa142b958008}"

# Pre-flight: reachability
for url in "$CLOUD_URL/health" "$AUTH_URL/health/alive"; do
  if ! curl -sk -o /dev/null -w "%{http_code}" --max-time 5 "$url" | grep -Eq '^(200|204)$'; then
    echo "SKIP: $url unreachable — bring caddy + port-forwards up first"
    exit 77
  fi
done

# Pre-flight: seed data present
PG_PASS=$(kubectl --context k3d-loopzero-mgmt -n loopzero-system get secret loopzero-db-credentials -o jsonpath='{.data.postgres-password}' | base64 -d 2>/dev/null || echo "")
if [ -z "$PG_PASS" ]; then
  echo "SKIP: cannot reach mgmt cluster PG credentials"
  exit 77
fi
PSQL="kubectl --context k3d-loopzero-mgmt -n loopzero-system exec -i postgresql-0 -c postgresql -- env PGPASSWORD=$PG_PASS psql -U postgres -d loopzero -At"
PROJECT_COUNT=$($PSQL -c "SELECT count(*) FROM projects WHERE tenant_id = '$TENANT_ID'" 2>/dev/null || echo "0")
if [ "$PROJECT_COUNT" -lt 2 ]; then
  echo "SKIP: only $PROJECT_COUNT projects seeded for $TENANT_ID — run task demo:seed first"
  exit 77
fi

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# Step 1: init Kratos browser login flow
curl -sk -L -c "$TMPDIR/cookies" \
  -H "Accept: application/json" \
  -o "$TMPDIR/flow.json" \
  "$AUTH_URL/self-service/login/browser" >/dev/null

CSRF=$(python3 -c "
import json
d = json.load(open('$TMPDIR/flow.json'))
print(next((n['attributes']['value'] for n in d['ui']['nodes']
            if n.get('attributes',{}).get('name')=='csrf_token'), ''))
")
ACTION=$(python3 -c "import json; print(json.load(open('$TMPDIR/flow.json'))['ui']['action'])")

if [ -z "$CSRF" ] || [ -z "$ACTION" ]; then
  echo "FAIL: could not parse Kratos login flow (CSRF=$CSRF ACTION=$ACTION)"
  exit 1
fi

# Step 2: submit password login
curl -sk -D "$TMPDIR/headers" -c "$TMPDIR/cookies" -b "$TMPDIR/cookies" \
  -H "Accept: application/json" -H "Content-Type: application/json" \
  -X POST "$ACTION" \
  -d "{\"csrf_token\":\"$CSRF\",\"identifier\":\"$EMAIL\",\"method\":\"password\",\"password\":\"$PASSWORD\"}" \
  -o "$TMPDIR/login.json" -w "Login HTTP %{http_code}\n"

SESS_COOKIE=$(grep -i "^set-cookie: ory_kratos_session" "$TMPDIR/headers" | sed 's/^[Ss]et-[Cc]ookie: //' | sed 's/;.*$//')
if [ -z "$SESS_COOKIE" ]; then
  echo "FAIL: no ory_kratos_session cookie issued"
  cat "$TMPDIR/login.json" | head -c 300
  exit 1
fi

# Step 3: exchange Kratos session for cloud JWT
JWT_JSON=$(curl -sk -X POST -H "Cookie: $SESS_COOKIE" -H "Accept: application/json" "$CLOUD_URL/v1/auth/session/token")
JWT=$(echo "$JWT_JSON" | python3 -c "import sys,json; print(json.load(sys.stdin).get('access_token',''))")

if [ -z "$JWT" ] || [ "$JWT" = "null" ]; then
  echo "FAIL: no JWT issued"
  echo "$JWT_JSON" | head -c 300
  exit 1
fi
echo "JWT minted (length=${#JWT})"

# Step 4: hit engine proxy
PROJECTS_HTTP=$(curl -sk -o "$TMPDIR/projects.json" -w "%{http_code}" \
  -H "Authorization: Bearer $JWT" \
  "$CLOUD_URL/engine/api/v1/projects")

PASS=0
FAIL=0
check() {
  local name="$1"; shift
  if "$@"; then echo "  PASS: $name"; PASS=$((PASS+1)); else echo "  FAIL: $name"; FAIL=$((FAIL+1)); fi
}

check "/engine/api/v1/projects returns HTTP 200" test "$PROJECTS_HTTP" = "200"

PROJECT_COUNT_API=$(python3 -c "
import json, sys
try:
    d = json.load(open('$TMPDIR/projects.json'))
    items = d if isinstance(d, list) else d.get('projects', d.get('items', []))
    print(len(items))
except Exception as e:
    print(0)
")

check "API returns >= 2 projects" test "$PROJECT_COUNT_API" -ge 2
echo "  API returned $PROJECT_COUNT_API projects (expected >= 2)"

echo ""
echo "============================="
echo "PASS=$PASS  FAIL=$FAIL"
echo "============================="
[ "$FAIL" -eq 0 ]
