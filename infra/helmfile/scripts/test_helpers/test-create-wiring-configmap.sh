#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP / T1.4 — offline test for create-wiring-configmap.sh.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
WIRING_SCRIPT="${SCRIPT_DIR}/../create-wiring-configmap.sh"

PASS=0
FAIL=0
log() { printf '%s\n' "$*" >&2; }
ok()   { log "  PASS  $1"; PASS=$((PASS+1)); }
fail() { log "  FAIL  $1: $2"; FAIL=$((FAIL+1)); }

MOCK_LOG=$(mktemp)
export MOCK_KUBECTL_LOG="$MOCK_LOG"
MOCK_BIN=$(mktemp -d)
ln -s "${SCRIPT_DIR}/mock-kubectl.sh" "${MOCK_BIN}/kubectl"
ORIG_PATH="$PATH"
export PATH="${MOCK_BIN}:$PATH"
trap "rm -rf $MOCK_LOG $MOCK_BIN" EXIT

export LOOPRUNTIME_DB_HOST="db.example.com"
export LOOPRUNTIME_DB_USER="loopruntime"
export LOOPRUNTIME_DB_NAME="loopruntime"
export LOOPRUNTIME_REGISTRY_ENDPOINT="registry.digitalocean.com/loopruntime"
export LOOPRUNTIME_BUCKET_NAME="loopruntime-static"
export LOOPRUNTIME_BUCKET_ENDPOINT="https://sfo3.digitaloceanspaces.com"
export LOOPRUNTIME_CF_KV_NAMESPACE="abc123"
export LOOPRUNTIME_DOMAIN="dev.getloopscale.com"
export LOOPRUNTIME_MGMT_INGRESS_IP="1.2.3.4"
export LOOPRUNTIME_RUNTIME_INGRESS_IP="1.2.3.5"

log "Running create-wiring-configmap.sh dev..."
bash "$WIRING_SCRIPT" dev 2>/dev/null

export PATH="$ORIG_PATH"

# Assert ConfigMap was applied
if grep -q "kind: ConfigMap" "$MOCK_LOG"; then
  ok "ConfigMap manifest applied"
else
  fail "ConfigMap" "no ConfigMap kind in stdin"
fi

if grep -q "loopruntime-platform-topology" "$MOCK_LOG"; then
  ok "loopruntime-platform-topology ConfigMap name correct"
else
  fail "name" "loopruntime-platform-topology not in args"
fi

# Verify the data section includes all expected fields
for field in "DB_HOST" "REGISTRY_ENDPOINT" "STORAGE_BUCKET" "MGMT_INGRESS_IP" "RUNTIME_INGRESS_IP" "GITEA_INTERNAL_URL"; do
  if grep -q "$field" "$MOCK_LOG"; then
    ok "ConfigMap contains $field"
  else
    fail "$field" "missing from ConfigMap"
  fi
done

# Verify env values were interpolated
if grep -qE 'DB_HOST: "db\.example\.com"' "$MOCK_LOG"; then
  ok "DB_HOST value correctly interpolated"
else
  fail "interpolation" "DB_HOST value not interpolated"
fi

log ""
log "RESULT: ${PASS} PASS, ${FAIL} FAIL"
[[ "$FAIL" == "0" ]] && exit 0 || exit 1
