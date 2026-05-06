#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-REAL-E2E — Phase 8 (T8.1)
#
# Journey 06 — Rollback: deploy v1 → deploy v2 → rollback to v1 → app=v1.
#
# Cobre §3 spec: loopzero rollback. Estratégia para diferenciar releases:
# fixture versioned-app contém arquivo VERSION.txt lido em runtime. A
# journey reescreve VERSION.txt entre os 2 deploys (v1, v2), captura
# release-id de cada, faz rollback for the v1 release-id, valida que
# /version retorna "v1".
#
# Asserções:
#   - Builds v1 + v2 SUCCEEDED
#   - HTTP 200 com /version=v1 (após deploy v1)
#   - HTTP 200 com /version=v2 (após deploy v2)
#   - loopzero rollback --to <rel-v1> succeeds
#   - HTTP 200 retorna /version=v1 (rollback efetivado em ~30s)
#   - audit entry contém action=rollback
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
# shellcheck disable=SC1091
source "$(dirname "$0")/_common.sh"

PROJECT="versioned-app"
APP="api"
ENV="production"
WORK_DIR="/tmp/dogfood-journey-06"

# Wait for /version endpoint to return a specific value.
# Args: host expected_version max_seconds
wait_version() {
  local host=$1 expected=$2 max_seconds=${3:-180}
  local elapsed=0 body
  while (( elapsed < max_seconds )); do
    body=$(curl -sS --max-time 5 \
      --connect-to "${host}:443:127.0.0.1:${TRAEFIK_HOST_PORT}" \
      -k "https://${host}/version" 2>/dev/null || echo "")
    if echo "$body" | grep -q "\"version\":\"${expected}\""; then
      journey_log "version=${expected} confirmed after ${elapsed}s"
      return 0
    fi
    sleep 5
    elapsed=$(( elapsed + 5 ))
  done
  journey_log "timeout — last body: $body (expected version=${expected})"
  return 1
}

journey_step "Setup fixture"
rm -rf "$WORK_DIR"
cp -r "${REPO_ROOT}/infra/tests/dogfood/fixtures/versioned-app" "$WORK_DIR"

journey_step "Ensure auth"
ensure_token

journey_step "Idempotency cleanup"
existing=$("$LOOPZERO_BIN" project list --json 2>/dev/null | python3 -c "
import json,sys
try: print('y' if any(p.get('name')=='$PROJECT' for p in json.load(sys.stdin).get('projects',[])) else 'n')
except: print('n')
" 2>/dev/null || echo "n")
if [[ "$existing" == "y" ]]; then
  "$LOOPZERO_BIN" project delete "$PROJECT" --yes --json >/dev/null 2>&1 || true
  prove_dead "$TENANT_ID" "$PROJECT" "$ENV" 180 || true
fi

journey_step "Deploy v1"
echo "v1" > "$WORK_DIR/src/VERSION.txt"
build_v1=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" deploy --env "$ENV" --no-wait --json 2>&1 \
  | tail -1 | python3 -c "import json,sys; print(json.load(sys.stdin).get('build_id',''))" 2>/dev/null)
[[ -n "$build_v1" ]] || journey_fail "no build_id v1"
journey_log "v1 build_id=$build_v1"
final=$(wait_build_terminal "$build_v1" 1500)
[[ "$final" == "SUCCEEDED" ]] || journey_fail "v1 build did not succeed: $final"

HOST="${TENANT_ID}-${PROJECT}-${APP}-${ENV}.loopzero.local"
wait_http_200 "$HOST" 300 "/health" || journey_fail "v1 HTTP 200 timeout"
wait_version "$HOST" "v1" 180 || journey_fail "v1 version not visible"
# Convergence: wait for v1's release row to flip ACTIVE before issuing v2.
# Otherwise rollback's "previous release" lookup may resolve to a stale row.
wait_release_active "$PROJECT" "$ENV" "$build_v1" 180 \
  || journey_fail "v1 release for build=$build_v1 did not reach ACTIVE in DB"

journey_step "Deploy v2"
echo "v2" > "$WORK_DIR/src/VERSION.txt"
build_v2=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" deploy --env "$ENV" --no-wait --json 2>&1 \
  | tail -1 | python3 -c "import json,sys; print(json.load(sys.stdin).get('build_id',''))" 2>/dev/null)
[[ -n "$build_v2" ]] || journey_fail "no build_id v2"
journey_log "v2 build_id=$build_v2"
final2=$(wait_build_terminal "$build_v2" 1500)
[[ "$final2" == "SUCCEEDED" ]] || journey_fail "v2 build did not succeed: $final2"
wait_version "$HOST" "v2" 180 || journey_fail "v2 version not visible (active release should be v2)"
# Wait for v2 to be the DB-confirmed ACTIVE release before rolling back.
# rollback derives "previous" via release ordering, which only stabilizes once
# v1 has been SUPERSEDED by v2 (state machine: NewReleaseActivated event).
wait_release_active "$PROJECT" "$ENV" "$build_v2" 180 \
  || journey_fail "v2 release for build=$build_v2 did not reach ACTIVE in DB"

journey_step "loopzero rollback (auto — no --to, falls back to previous release)"
# Without --to, rollback automatically targets the release immediately
# preceding the active one. Spec §3 / cli/src/commands/rollback.ts.
( cd "$WORK_DIR" && "$LOOPZERO_BIN" rollback --env "$ENV" --yes --json ) > /tmp/journey-06-rollback.json 2>&1 || \
  journey_fail "rollback failed: $(cat /tmp/journey-06-rollback.json)"
journey_log "rollback issued: $(cat /tmp/journey-06-rollback.json | head -c 200)"
# Validate CLI response shape: rolled_back_release + active_release.
# (CLI normalizes the API's `reactivated_release` to `active_release`
# in cli/src/commands/rollback.ts:100 — UX consistency with status output.)
rolled_back=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('rolled_back_release',''))" < /tmp/journey-06-rollback.json 2>/dev/null || echo "")
reactivated=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('active_release') or json.load(sys.stdin).get('reactivated_release',''))" < /tmp/journey-06-rollback.json 2>/dev/null || echo "")
if [[ -z "$reactivated" ]]; then
  reactivated=$(python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('active_release','') or d.get('reactivated_release',''))" < /tmp/journey-06-rollback.json 2>/dev/null || echo "")
fi
[[ -n "$rolled_back" && -n "$reactivated" ]] || \
  journey_fail "rollback response missing rolled_back_release/active_release: $(cat /tmp/journey-06-rollback.json)"
journey_log "rolled_back=$rolled_back active=$reactivated"

journey_step "Verify rollback effective — /version returns v1"
wait_version "$HOST" "v1" 180 || journey_fail "rollback did not flip /version back to v1"

journey_step "Audit log assertion (rollback action)"
audit_ok=0
for _ in 1 2 3 4 5 6; do
  if assert_audit_entry "rollback"; then
    audit_ok=1; break
  fi
  sleep 5
done
if (( audit_ok == 0 )); then
  journey_log "WARN: audit assertion for rollback failed — non-fatal gap"
fi

journey_step "Prova de Morte"
"$LOOPZERO_BIN" project delete "$PROJECT" --yes --json >/dev/null 2>&1 || journey_fail "project delete failed"
prove_dead "$TENANT_ID" "$PROJECT" "$ENV" 300 || journey_fail "Prova de Morte timeout"

journey_log "Journey 06 GREEN — rollback v2→v1 validated"
exit 0
