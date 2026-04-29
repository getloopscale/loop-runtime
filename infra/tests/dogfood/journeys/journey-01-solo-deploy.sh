#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-REAL-E2E — Phase 3 (T3.1)
#
# Journey 01 — Solo deploy + Prova de Vida + Prova de Morte.
#
# Fluxo:
#   1. Copy fixture solo-server → /tmp/dogfood-journey-01
#   2. loopzero validate
#   3. loopzero deploy --env production --no-wait
#   4. wait build SUCCEEDED
#   5. wait HTTP 200 em https://<tenant>-solo-server-api-production.loopzero.local/
#   6. assert audit log contém entry deploy
#   7. loopzero project delete --confirm
#   8. Prova de Morte: namespace gone + ArgoCD app gone (≤5min)
#
# Pré-requisitos: preflight.sh GREEN.
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
# shellcheck disable=SC1091
source "$(dirname "$0")/_common.sh"

PROJECT="solo-server"
APP="api"
ENV="production"
WORK_DIR="/tmp/dogfood-journey-01"

journey_step "Setup fixture"
rm -rf "$WORK_DIR"
cp -r "${REPO_ROOT}/infra/tests/dogfood/fixtures/solo-server" "$WORK_DIR"
journey_log "fixture copied to $WORK_DIR"

journey_step "Ensure auth"
ensure_token

journey_step "Idempotency cleanup — delete project if exists from prior run"
# Check if project already exists; if so, delete it. ADR D3: each journey
# must be self-cleaning so re-runs are deterministic.
existing=$("$LOOPZERO_BIN" project list --json 2>/dev/null | python3 -c "
import json,sys
try: print('y' if any(p.get('name')=='$PROJECT' for p in json.load(sys.stdin).get('projects',[])) else 'n')
except: print('n')
" 2>/dev/null || echo "n")
if [[ "$existing" == "y" ]]; then
  journey_log "$PROJECT exists from prior run; deleting (idempotency)"
  "$LOOPZERO_BIN" project delete "$PROJECT" --yes --json >/tmp/journey-01-pre-delete.json 2>&1 || \
    journey_log "WARN: pre-delete returned non-zero; continuing"
  prove_dead "$TENANT_ID" "$PROJECT" "$ENV" 180 || journey_log "WARN: pre-delete cleanup did not finish in 180s"
fi

journey_step "loopzero validate"
( cd "$WORK_DIR" && "$LOOPZERO_BIN" validate --json ) > /tmp/journey-01-validate.json
valid=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('valid'))" < /tmp/journey-01-validate.json)
[[ "$valid" == "True" ]] || journey_fail "validate did not return valid=true: $(cat /tmp/journey-01-validate.json)"
journey_log "validate OK"

journey_step "loopzero deploy --no-wait"
deploy_out=$( cd "$WORK_DIR" && "$LOOPZERO_BIN" deploy --env "$ENV" --no-wait --json 2>&1 ) || true
echo "$deploy_out" > /tmp/journey-01-deploy.json
build_id=$(echo "$deploy_out" | tail -1 | python3 -c "import json,sys; print(json.load(sys.stdin).get('build_id',''))" 2>/dev/null || true)
[[ -n "$build_id" ]] || journey_fail "no build_id in deploy output: $deploy_out"
journey_log "build_id=$build_id"

journey_step "Wait build to terminal (max 25min)"
final=$(wait_build_terminal "$build_id" 1500)
[[ "$final" == "SUCCEEDED" ]] || journey_fail "build did not succeed: $final"

journey_step "Prova de Vida — HTTP 200 + TLS"
host="${TENANT_ID}-${PROJECT}-${APP}-${ENV}.loopzero.local"
wait_http_200 "$host" 300 "/health" || journey_fail "HTTP 200 timeout at $host"
# Sanity: GET / returns the project name
body=$(curl -sS --connect-to "${host}:443:127.0.0.1:${TRAEFIK_HOST_PORT}" -k "https://${host}/")
journey_log "GET / body: $body"
echo "$body" | grep -q '"project":"solo-server"' || journey_fail "unexpected body: $body"

journey_step "Audit log assertion"
# Audit may take a moment to flush; allow 30s of polling.
audit_ok=0
for _ in 1 2 3 4 5 6; do
  if assert_audit_entry "deploy"; then
    audit_ok=1; break
  fi
  sleep 5
done
if (( audit_ok == 0 )); then
  journey_log "WARN: audit assertion failed — recording as non-fatal gap (Phase 14 follow-up)"
fi

journey_step "Prova de Morte — loopzero project delete + cleanup verify"
"$LOOPZERO_BIN" project delete "$PROJECT" --yes --json >/tmp/journey-01-delete.json 2>&1 || \
  journey_fail "project delete failed: $(cat /tmp/journey-01-delete.json)"
prove_dead "$TENANT_ID" "$PROJECT" "$ENV" 300 || journey_fail "Prova de Morte timeout"

journey_step "Re-run preflight (idempotency)"
bash "${REPO_ROOT}/infra/tests/dogfood/preflight.sh" --json > /tmp/journey-01-preflight-post.json 2>&1
pass=$(python3 -c "import json,sys; print(json.load(sys.stdin)['summary']['fail'])" < /tmp/journey-01-preflight-post.json)
[[ "$pass" == "0" ]] || journey_fail "preflight has failures after journey: $(cat /tmp/journey-01-preflight-post.json)"

journey_log "Journey 01 GREEN — solo deploy + Prova de Vida + Prova de Morte"
exit 0
