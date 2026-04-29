#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP / T2.3 — offline test for generate-runtime-secrets.sh.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RUNTIME_SCRIPT="${SCRIPT_DIR}/../generate-runtime-secrets.sh"

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

export LOOPZERO_CF_TUNNEL_TOKEN="REDACTED_SECRET_039"
export LOOPZERO_DB_HOST="db.example.com"
export LOOPZERO_DB_USER="loopzero"
export LOOPZERO_DB_PASSWORD="secret"

log "Running generate-runtime-secrets.sh dev runtime..."
bash "$RUNTIME_SCRIPT" dev runtime 2>/dev/null

export PATH="$ORIG_PATH"

if grep -q "cloudflared-tunnel-token" "$MOCK_LOG"; then
  ok "cloudflared-tunnel-token Secret created (D9)"
else
  fail "D9" "cloudflared-tunnel-token Secret missing"
fi

if grep -qE "token=REDACTED_SECRET_039" "$MOCK_LOG"; then
  ok "tunnel token value passed correctly"
else
  fail "tunnel-token-value" "value not in kubectl args"
fi

if grep -q "loopzero-eso-source" "$MOCK_LOG"; then
  ok "loopzero-eso-source Secret created (ESO connection)"
else
  fail "ESO" "loopzero-eso-source not created"
fi

log ""
log "RESULT: ${PASS} PASS, ${FAIL} FAIL"
[[ "$FAIL" == "0" ]] && exit 0 || exit 1
