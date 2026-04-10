#!/bin/bash
# L1 Load Test — 10 tenants, end-to-end, zero intervention
# Prerequisites: DB accessible, API healthy, build cluster ready
# Usage: LOOPZERO_DB_PASS=<pass> bash infra/tests/load/run-l1.sh
set -euo pipefail

DB_HOST="loopzero-infra-dev-sfo3-db-do-user-778930-0.l.db.ondigitalocean.com"
DB_PORT="25060"
DB_USER="doadmin"
DB_NAME="loopzero"
DB_PASS="${LOOPZERO_DB_PASS:?Set LOOPZERO_DB_PASS}"
DB_CA="${LOOPZERO_DB_CA:-/home/paulo/ca-certificate.crt}"

API_URL="https://api.dev.loopzero.example"
SVC_AUTH="dev-service-auth-secret-default"
TENANTS=10
BUNDLE="/tmp/loadtest.bundle"

echo "════════════════════════════════════════"
echo "  L1 LOAD TEST — $TENANTS tenants"
echo "════════════════════════════════════════"

# 1. Create fixture bundle
echo "[1/5] Creating fixture bundle..."
TMPDIR=$(mktemp -d)
cd "$TMPDIR"
cat > package.json << 'EOF'
{"name":"loadtest-app","version":"1.0.0","main":"server.js","scripts":{"start":"node server.js"},"engines":{"node":">=18"}}
EOF
cat > package-lock.json << 'EOF'
{"name":"loadtest-app","version":"1.0.0","lockfileVersion":3,"requires":true,"packages":{"":{"name":"loadtest-app","version":"1.0.0"}}}
EOF
cat > server.js << 'EOF'
const http = require("http");
const port = process.env.PORT || 8080;
http.createServer((req, res) => {
  res.writeHead(200, {"Content-Type": "application/json"});
  res.end(JSON.stringify({status: "ok"}));
}).listen(port, () => console.log("listening on " + port));
EOF
git init -q . && git add -A && git commit -q -m "loadtest"
git bundle create "$BUNDLE" HEAD 2>/dev/null
cd /
rm -rf "$TMPDIR"
echo "  Bundle: $(stat -c%s "$BUNDLE") bytes"

# 2. Seed DB (auth only)
echo "[2/5] Seeding DB ($TENANTS users + tenants + memberships)..."
PGPASSWORD="$DB_PASS" PGSSLROOTCERT="$DB_CA" psql \
  -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -q -c "
INSERT INTO users (id, email, github_id, display_name)
  SELECT format('loadtest-user-%s', i), format('lt%s@lt.dev', i), 9000000+i, format('LT User %s', i)
  FROM generate_series(0, $((TENANTS-1))) i ON CONFLICT DO NOTHING;
INSERT INTO tenants (id, display_name, owner_id, slug)
  SELECT format('loadtest-tn-%s', i), format('LT Tenant %s', i), format('loadtest-user-%s', i), format('loadtest-tn-%s', i)
  FROM generate_series(0, $((TENANTS-1))) i ON CONFLICT DO NOTHING;
INSERT INTO tenant_members (tenant_id, user_id, role)
  SELECT format('loadtest-tn-%s', i), format('loadtest-user-%s', i), 'owner'
  FROM generate_series(0, $((TENANTS-1))) i ON CONFLICT DO NOTHING;
"
echo "  Seeded"

# 3. Create projects via API (triggers EnsureTenant → Tenant CR)
echo "[3/5] Creating $TENANTS projects via API..."
for i in $(seq 0 $((TENANTS-1))); do
  HTTP=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$API_URL/api/v1/projects" \
    -H "X-Service-Auth: $SVC_AUTH" \
    -H "X-LoopZero-User-ID: loadtest-user-$i" \
    -H "X-LoopZero-Tenant: loadtest-tn-$i" \
    -H "X-LoopZero-Email: lt${i}@lt.dev" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"loadtest-app-$i\"}")
  [ "$HTTP" != "201" ] && echo "  WARN: tn-$i HTTP $HTTP" || true
done
echo "  Projects created, waiting for TenantController..."
sleep 15
echo "  Ready"

# 4. Upload + Build (all tenants simultaneously)
echo "[4/5] Uploading bundles + triggering $TENANTS builds..."
BIDS=""
for i in $(seq 0 $((TENANTS-1))); do
  COMMIT=$(curl -s -X POST "$API_URL/api/v1/source/upload" \
    -H "X-Service-Auth: $SVC_AUTH" \
    -H "X-LoopZero-User-ID: loadtest-user-$i" \
    -H "X-LoopZero-Tenant: loadtest-tn-$i" \
    -H "X-LoopZero-Email: lt${i}@lt.dev" \
    -F "bundle=@$BUNDLE" \
    -F "loopzero_yaml=version: 2
project: loadtest-app-$i
apps:
  api:
    build: buildpack
    framework: node
    render: server
    port: 8080" | jq -r '.commit_sha')

  BID=$(curl -s -X POST "$API_URL/api/v1/builds" \
    -H "X-Service-Auth: $SVC_AUTH" \
    -H "X-LoopZero-User-ID: loadtest-user-$i" \
    -H "X-LoopZero-Tenant: loadtest-tn-$i" \
    -H "X-LoopZero-Email: lt${i}@lt.dev" \
    -H "Content-Type: application/json" \
    -H "Idempotency-Key: l1-$(date +%s)-$i" \
    -d "{\"project_id\":\"loadtest-app-$i\",\"environment\":\"production\",\"commit_sha\":\"$COMMIT\",\"apps\":[{\"name\":\"api\",\"build\":\"buildpack\",\"framework\":\"node\",\"render\":\"server\",\"port\":8080}]}" | jq -r '.build_id')
  BIDS="$BIDS $i:$BID"
  echo "  tn-$i: $BID"
done

# 5. Monitor builds
echo "[5/5] Monitoring builds..."
START=$(date +%s)
for round in $(seq 1 50); do
  sleep 20
  ELAPSED=$(($(date +%s) - START))
  S=0; F=0; R=0
  for entry in $BIDS; do
    i=$(echo "$entry" | cut -d: -f1)
    bid=$(echo "$entry" | cut -d: -f2)
    STATUS=$(curl -s "$API_URL/api/v1/builds/$bid" \
      -H "X-Service-Auth: $SVC_AUTH" \
      -H "X-LoopZero-User-ID: loadtest-user-$i" \
      -H "X-LoopZero-Tenant: loadtest-tn-$i" \
      -H "X-LoopZero-Email: lt${i}@lt.dev" | jq -r '.status')
    case "$STATUS" in SUCCEEDED) S=$((S+1));; FAILED|CANCELLED) F=$((F+1));; *) R=$((R+1));; esac
  done
  echo "  [${ELAPSED}s] OK=$S RUNNING=$R FAILED=$F"
  [ $((S+F)) -eq "$TENANTS" ] && break
done

# Report
echo ""
echo "════════════════════════════════════════"
echo "  L1 RESULTS"
echo "════════════════════════════════════════"
TOTAL_S=0; TOTAL_F=0
for entry in $BIDS; do
  i=$(echo "$entry" | cut -d: -f1)
  bid=$(echo "$entry" | cut -d: -f2)
  RESP=$(curl -s "$API_URL/api/v1/builds/$bid" \
    -H "X-Service-Auth: $SVC_AUTH" \
    -H "X-LoopZero-User-ID: loadtest-user-$i" \
    -H "X-LoopZero-Tenant: loadtest-tn-$i" \
    -H "X-LoopZero-Email: lt${i}@lt.dev")
  ST=$(echo "$RESP" | jq -r '.status')
  DUR=$(echo "$RESP" | jq -r '.duration_seconds')
  ERR=$(echo "$RESP" | jq -r '.error // empty')
  echo "  tn-$i: $ST (${DUR}s) $ERR"
  [ "$ST" = "SUCCEEDED" ] && TOTAL_S=$((TOTAL_S+1)) || TOTAL_F=$((TOTAL_F+1))
done
echo "────────────────────────────────────────"
echo "  SUCCEEDED: $TOTAL_S / $TENANTS"
echo "  FAILED:    $TOTAL_F / $TENANTS"
echo "════════════════════════════════════════"
