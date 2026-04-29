#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-REAL-E2E — Phase 5 (T5.1)
#
# Journey 03 — Secrets lifecycle: set → app vê env → unset → app não vê.
#
# Cobre §9 do spec: loopzero secret set/list/delete.
# Asserções:
#   - loopzero secret set: ExternalSecret CR criada
#   - app pod env tem SECRET_PING após re-deploy
#   - loopzero secret list --json retorna nomes (NUNCA valores)
#   - loopzero secret delete remove ExternalSecret + env var some no próximo deploy
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
# shellcheck disable=SC1091
source "$(dirname "$0")/_common.sh"

PROJECT="secret-app"
APP="api"
ENV="production"
WORK_DIR="/tmp/dogfood-journey-03"
SECRET_NAME="SECRET_PING"
SECRET_VALUE="hello-from-dogfood-journey-03-$(date +%s)"

cleanup_project() {
  local existing
  existing=$("$LOOPZERO_BIN" project list --json 2>/dev/null | python3 -c "
import json,sys
try: print('y' if any(p.get('name')=='$PROJECT' for p in json.load(sys.stdin).get('projects',[])) else 'n')
except: print('n')
" 2>/dev/null || echo "n")
  if [[ "$existing" == "y" ]]; then
    journey_log "$PROJECT exists; deleting"
    "$LOOPZERO_BIN" project delete "$PROJECT" --yes --json >/dev/null 2>&1 || true
    prove_dead "$TENANT_ID" "$PROJECT" "$ENV" 180 || true
  fi
}

journey_step "Setup fixture"
rm -rf "$WORK_DIR"
cp -r "${REPO_ROOT}/infra/tests/dogfood/fixtures/secret-app" "$WORK_DIR"

journey_step "Ensure auth"
ensure_token

journey_step "Idempotency cleanup"
cleanup_project

journey_step "Pre-deploy: loopzero.yaml declares SECRET_PING — must set BEFORE first deploy"
# LoopZero enforces MISSING_SECRETS if loopzero.yaml lists a secret that doesn't
# exist (correct fail-fast). Order of operations:
#   1. loopzero secret set
#   2. loopzero deploy  (with loopzero.yaml.secrets=[SECRET_PING])
#   3. validate pod env
( cd "$WORK_DIR" && "$LOOPZERO_BIN" secret set "${SECRET_NAME}=${SECRET_VALUE}" --env "$ENV" --app "$APP" --json ) > /tmp/journey-03-set.json 2>&1 || \
  journey_fail "loopzero secret set failed: $(cat /tmp/journey-03-set.json)"
journey_log "secret set OK (pre-deploy)"

journey_step "loopzero secret list — nome presente, valor NUNCA"
list_out=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" secret list --env "$ENV" --json 2>&1)
echo "$list_out" | grep -q "$SECRET_NAME" || journey_fail "secret list missing ${SECRET_NAME}: $list_out"
if echo "$list_out" | grep -q "$SECRET_VALUE"; then
  journey_fail "Rule 9 VIOLATION: secret list exposed the VALUE: $list_out"
fi
journey_log "secret list OK (name present, value never exposed)"

journey_step "loopzero deploy (initial — secret already set)"
build_id=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" deploy --env "$ENV" --no-wait --json 2>&1 \
  | tail -1 | python3 -c "import json,sys; print(json.load(sys.stdin).get('build_id',''))" 2>/dev/null)
[[ -n "$build_id" ]] || journey_fail "no build_id"
journey_log "build_id=$build_id"

final=$(wait_build_terminal "$build_id" 1500)
[[ "$final" == "SUCCEEDED" ]] || journey_fail "initial build did not succeed: $final"

HOST="${TENANT_ID}-${PROJECT}-${APP}-${ENV}.loopzero.local"
wait_http_200 "$HOST" 300 "/health" || journey_fail "initial HTTP 200 timeout"
sleep 30  # allow ESO sync to materialize the Secret + pod restart

journey_step "Post-secret check: pod sees SECRET_PING in /secret-check"
body=$(curl -sS --connect-to "${HOST}:443:127.0.0.1:${TRAEFIK_HOST_PORT}" -k "https://${HOST}/secret-check")
echo "$body" | grep -q "\"secret_ping\":\"${SECRET_VALUE}\"" || \
  journey_fail "secret not visible in pod env: $body (expected ${SECRET_VALUE})"
journey_log "pod sees SECRET_PING with correct value (ESO sync OK)"

journey_step "loopzero secret delete (remove)"
( cd "$WORK_DIR" && "$LOOPZERO_BIN" secret delete "$SECRET_NAME" --env "$ENV" --app "$APP" --yes --json ) > /tmp/journey-03-del.json 2>&1 || \
  journey_fail "loopzero secret delete failed: $(cat /tmp/journey-03-del.json)"

journey_step "loopzero secret list — após delete, nome ausente"
list_out=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" secret list --env "$ENV" --json 2>&1)
if echo "$list_out" | grep -q "\"$SECRET_NAME\""; then
  journey_fail "secret list still shows deleted secret: $list_out"
fi
journey_log "secret list confirms delete"

journey_step "Re-deploy with loopzero.yaml still declaring SECRET_PING — expect MISSING_SECRETS (Rule fail-fast)"
out=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" deploy --env "$ENV" --no-wait --json 2>&1 || true)
echo "$out" | grep -q "MISSING_SECRETS" || journey_fail "expected MISSING_SECRETS after delete, got: $out"
journey_log "Rule fail-fast OK: deploy rejected with MISSING_SECRETS"

journey_step "Remove secret from loopzero.yaml + re-deploy must succeed"
# Strip the `secrets:` list from loopzero.yaml so deploy goes through.
python3 - <<'PY'
import yaml
path = "/tmp/dogfood-journey-03/loopzero.yaml"
with open(path) as f:
    data = yaml.safe_load(f)
for app in data.get("apps", {}).values():
    app.pop("secrets", None)
with open(path, "w") as f:
    yaml.safe_dump(data, f, sort_keys=False)
PY
build3=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" deploy --env "$ENV" --no-wait --json 2>&1 \
  | tail -1 | python3 -c "import json,sys; print(json.load(sys.stdin).get('build_id',''))" 2>/dev/null)
[[ -n "$build3" ]] || journey_fail "no build_id (post-delete + loopzero.yaml clean)"
final3=$(wait_build_terminal "$build3" 1500)
[[ "$final3" == "SUCCEEDED" ]] || journey_fail "post-delete build did not succeed: $final3"
sleep 30

journey_step "Post-delete check: pod env no longer has SECRET_PING"
body=$(curl -sS --connect-to "${HOST}:443:127.0.0.1:${TRAEFIK_HOST_PORT}" -k "https://${HOST}/secret-check")
echo "$body" | grep -q '"secret_ping":"unset"' || \
  journey_fail "expected secret_ping=unset after delete+redeploy, got: $body"
journey_log "secret no longer visible in pod env"

journey_step "Prova de Morte"
"$LOOPZERO_BIN" project delete "$PROJECT" --yes --json >/dev/null 2>&1 || journey_fail "project delete failed"
prove_dead "$TENANT_ID" "$PROJECT" "$ENV" 300 || journey_fail "Prova de Morte timeout"

journey_log "Journey 03 GREEN — secrets lifecycle (set/list/delete) validated"
exit 0
