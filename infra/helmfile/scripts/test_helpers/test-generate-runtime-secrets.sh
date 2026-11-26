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

export LOOP_RUNTIME_CF_TUNNEL_TOKEN="REDACTED_SECRET_039"
export LOOP_RUNTIME_DB_HOST="db.example.com"
export LOOP_RUNTIME_DB_USER="loop_runtime"
export LOOP_RUNTIME_DB_PASSWORD="secret"

# Issue #117 — generate-runtime-secrets.sh now REFUSES to run without an
# explicit KUBECONFIG, because it must never inherit an ambient kubecontext
# when writing runtime Secrets. This harness is a caller, so it honours the
# same contract a real caller does (taskfiles/infra.yml pins via
# `KUBECONFIG=$(k3d kubeconfig write loop-runtime-cell-0)`).
#
# The file is a throwaway: kubectl is shimmed by mock-kubectl.sh on PATH and
# never reads it. Pointing at a real cluster here would be wrong — the value
# exists to satisfy, and therefore exercise, the pinning contract itself.
MOCK_KUBECONFIG=$(mktemp)
export KUBECONFIG="$MOCK_KUBECONFIG"
trap "rm -rf $MOCK_LOG $MOCK_BIN $MOCK_KUBECONFIG" EXIT

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

if grep -q "loop-runtime-eso-source" "$MOCK_LOG"; then
  ok "loop-runtime-eso-source Secret created (ESO connection)"
else
  fail "ESO" "loop-runtime-eso-source not created"
fi

log ""
log "RESULT: ${PASS} PASS, ${FAIL} FAIL"
[[ "$FAIL" == "0" ]] && exit 0 || exit 1
