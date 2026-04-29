#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-REAL-E2E — Phase 7 (T7.1)
#
# Journey 05 — Promote staging → production.
#
# Asserções:
#   - Deploy versioned-app v1 em staging → /version=v1
#   - loopzero promote --from staging --to production
#   - Production responde mesma versão (v1)
#   - State machine: ambos environments têm release ACTIVE pointing ao
#     mesmo build_id
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
# shellcheck disable=SC1091
source "$(dirname "$0")/_common.sh"

PROJECT="versioned-app"
APP="api"
WORK_DIR="/tmp/dogfood-journey-05"

wait_version() {
  local host=$1 expected=$2 max_seconds=${3:-180}
  local elapsed=0 body
  while (( elapsed < max_seconds )); do
    body=$(curl -sS --max-time 5 \
      --connect-to "${host}:443:127.0.0.1:${TRAEFIK_HOST_PORT}" \
      -k "https://${host}/version" 2>/dev/null || echo "")
    if echo "$body" | grep -q "\"version\":\"${expected}\""; then
      journey_log "version=${expected} confirmed at $host after ${elapsed}s"
      return 0
    fi
    sleep 5
    elapsed=$(( elapsed + 5 ))
  done
  journey_log "timeout — last body: $body"
  return 1
}

journey_step "Setup fixture"
rm -rf "$WORK_DIR"
cp -r "${REPO_ROOT}/infra/tests/dogfood/fixtures/versioned-app" "$WORK_DIR"
echo "v1" > "$WORK_DIR/src/VERSION.txt"

journey_step "Ensure auth"
ensure_token

journey_step "Idempotency cleanup (both staging + production)"
"$LOOPZERO_BIN" project delete "$PROJECT" --yes --json >/dev/null 2>&1 || true
prove_dead "$TENANT_ID" "$PROJECT" "production" 180 || true
prove_dead "$TENANT_ID" "$PROJECT" "staging" 60 || true

journey_step "Ensure staging Environment exists (idempotent)"
# LoopZero only creates environments on demand. Spec §6: loopzero env create.
# Staging must exist before deploy targets it.
env_list=$("$LOOPZERO_BIN" env list --json 2>&1 || echo "{}")
if ! echo "$env_list" | grep -q '"staging"'; then
  journey_log "staging env absent; creating"
  ( cd "$WORK_DIR" && "$LOOPZERO_BIN" env create staging --json ) >/tmp/journey-05-env-create.json 2>&1 || \
    journey_fail "env create staging failed: $(cat /tmp/journey-05-env-create.json)"
  # Wait for Environment CR to reach Active phase (controller reconciles)
  for _ in $(seq 1 30); do
    phase=$(kubectl --context "$MGMT_CTX" -n loopzero-system get environment "${TENANT_ID}-staging" \
      -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
    if [[ "$phase" == "Active" ]]; then
      journey_log "staging Environment Active"
      break
    fi
    sleep 5
  done
fi

journey_step "Deploy v1 to staging"
build_staging=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" deploy --env staging --no-wait --json 2>&1 \
  | tail -1 | python3 -c "import json,sys; print(json.load(sys.stdin).get('build_id',''))" 2>/dev/null)
[[ -n "$build_staging" ]] || journey_fail "no build_id (staging deploy)"
journey_log "staging build_id=$build_staging"
final=$(wait_build_terminal "$build_staging" 1500)
[[ "$final" == "SUCCEEDED" ]] || journey_fail "staging build did not succeed: $final"

STAGING_HOST="${TENANT_ID}-${PROJECT}-${APP}-staging.loopzero.local"
wait_http_200 "$STAGING_HOST" 300 "/health" || journey_fail "staging HTTP 200 timeout"
wait_version "$STAGING_HOST" "v1" 180 || journey_fail "staging version=v1 not visible"
# Promote reads release state from DB; HTTP 200 + version response precede
# the convergence loop's transition to ACTIVE. Wait for DB readiness too.
wait_release_active "$PROJECT" "staging" "$build_staging" 180 \
  || journey_fail "staging release for build=$build_staging did not reach ACTIVE in DB"

journey_step "loopzero promote staging → production"
( cd "$WORK_DIR" && "$LOOPZERO_BIN" promote --from staging --to production --yes --json ) > /tmp/journey-05-promote.json 2>&1 || \
  journey_fail "promote failed: $(cat /tmp/journey-05-promote.json)"
journey_log "promote response: $(cat /tmp/journey-05-promote.json | head -c 250)"

journey_step "Production responds with same version (v1)"
PROD_HOST="${TENANT_ID}-${PROJECT}-${APP}-production.loopzero.local"
wait_http_200 "$PROD_HOST" 300 "/health" || journey_fail "production HTTP 200 timeout"
wait_version "$PROD_HOST" "v1" 180 || journey_fail "production version=v1 not visible after promote"
# Wait for production release to reach DB ACTIVE — promote creates the row
# in PENDING; convergence loop activates after Argo confirms healthy. Otherwise
# the assertion below races with that transition.
wait_release_active "$PROJECT" "production" "$build_staging" 180 \
  || journey_fail "production release for build=$build_staging did not reach ACTIVE in DB"

journey_step "Validate DB state — both envs ACTIVE pointing to same build_id"
PG_PASS=$(kubectl --context "$MGMT_CTX" -n loopzero-system get secret postgresql -o jsonpath='{.data.password}' | base64 -d)
state=$(kubectl --context "$MGMT_CTX" -n loopzero-system exec postgresql-0 -c postgresql -- env PGPASSWORD="$PG_PASS" \
  psql -U loopzero -t -c "SELECT environment, status, build_id FROM releases WHERE project_id='$PROJECT' AND status='ACTIVE' ORDER BY environment;" 2>/dev/null || echo "")
journey_log "DB ACTIVE releases:
$state"
# Strict check: BOTH staging AND production must be ACTIVE — accepting only
# one row was a false-positive that hid the cross-env activation race.
active_envs=$(echo "$state" | awk 'NF{print $1}' | sort -u | grep -v '^$')
expect_envs=$(printf "production\nstaging")
[[ "$active_envs" == "$expect_envs" ]] || journey_fail "expected both envs ACTIVE, got: '$active_envs'"
unique_builds=$(echo "$state" | awk 'NF{print $5}' | sort -u | grep -v '^$' | wc -l)
[[ "$unique_builds" == "1" ]] || journey_fail "expected single build_id across both envs, got $unique_builds: $state"
journey_log "promote verified: both envs ACTIVE with same build_id"

journey_step "Prova de Morte"
"$LOOPZERO_BIN" project delete "$PROJECT" --yes --json >/dev/null 2>&1 || journey_fail "project delete failed"
prove_dead "$TENANT_ID" "$PROJECT" "production" 300 || journey_fail "Prova de Morte production timeout"

journey_log "Journey 05 GREEN — promote staging → production validated"
exit 0
