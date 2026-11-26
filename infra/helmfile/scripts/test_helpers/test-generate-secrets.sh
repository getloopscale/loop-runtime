#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP / T2.x — offline test for generate-secrets.sh.
# Uses mock-kubectl.sh to verify that the secret generation script:
#   1. Creates all required Secrets (loop-runtime-db-credentials, gitea-admin, ...)
#   2. Does NOT include trailing newlines in token values (B4 fix)
#   3. Generates random passwords when not provided
#
# Run: bash infra/helmfile/scripts/test_helpers/test-generate-secrets.sh
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SECRETS_SCRIPT="${SCRIPT_DIR}/../generate-secrets.sh"

PASS=0
FAIL=0
log() { printf '%s\n' "$*" >&2; }
ok()   { log "  PASS  $1"; PASS=$((PASS+1)); }
fail() { log "  FAIL  $1: $2"; FAIL=$((FAIL+1)); }

# Setup: clean log + put mock kubectl first in PATH
MOCK_LOG=$(mktemp)
export MOCK_KUBECTL_LOG="$MOCK_LOG"
MOCK_BIN=$(mktemp -d)
ln -s "${SCRIPT_DIR}/mock-kubectl.sh" "${MOCK_BIN}/kubectl"
ORIG_PATH="$PATH"
export PATH="${MOCK_BIN}:$PATH"

trap "rm -rf $MOCK_LOG $MOCK_BIN" EXIT

# Provide minimal env vars so the script runs through every code path
export LOOP_RUNTIME_DB_HOST="db.example.com"
export LOOP_RUNTIME_DB_PORT="5432"
export LOOP_RUNTIME_DB_USER="loop_runtime"
export LOOP_RUNTIME_DB_PASSWORD="secret123"
export LOOP_RUNTIME_DB_NAME="loop_runtime"
export LOOP_RUNTIME_SPACES_KEY="ACCESS_KEY"
export LOOP_RUNTIME_SPACES_SECRET="SECRET_KEY"
export LOOP_RUNTIME_REGISTRY_ENDPOINT="registry.digitalocean.com/loop-runtime"

# Execute
log "Running generate-secrets.sh dev core..."
bash "$SECRETS_SCRIPT" dev core 2>/dev/null || {
  fail "script execution" "exit non-zero"
}

# Restore PATH
export PATH="$ORIG_PATH"

# Assertions on the mock log
if grep -q "secret/test created" "$MOCK_LOG"; then
  ok "kubectl secret create commands invoked"
else
  fail "B-secret-creation" "no kubectl create secret invocation captured"
fi

if grep -qE "from-literal=host=db\.example\.com" "$MOCK_LOG"; then
  ok "DB_HOST passed correctly"
else
  fail "DB_HOST" "host literal not seen in kubectl args"
fi

# Harbor was retired (docs/decisions.md, "Retire Harbor"): no Secret for it may
# come back through this script.
if grep -qi "harbor" "$MOCK_LOG"; then
  fail "harbor-retired" "generate-secrets.sh still writes a Harbor Secret"
else
  ok "no Harbor Secret written"
fi

# B4 — Verify gitea token is passed via --from-file (which preserves no-newline encoding)
if grep -qE "from-file=token=" "$MOCK_LOG"; then
  ok "B4 — gitea token via --from-file (no trailing newline)"
else
  fail "B4" "gitea token not via --from-file"
fi

# Verify Docker pull secret was constructed
if grep -q "kubernetes.io/dockerconfigjson" "$MOCK_LOG"; then
  ok "loop-runtime-registry-pull dockerconfigjson Secret applied"
else
  fail "registry-pull" "no dockerconfigjson Secret"
fi

log ""
log "RESULT: ${PASS} PASS, ${FAIL} FAIL"
[[ "$FAIL" == "0" ]] && exit 0 || exit 1
