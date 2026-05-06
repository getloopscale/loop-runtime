#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-REAL-E2E — Phase 6 (T6.1)
#
# Journey 04 — Scale: fixed replicas + over-cap rejection.
#
# Cobre §3 spec: loopzero scale.
# Asserções:
#   - loopzero scale --replicas 3 → Deployment.spec.replicas == 3
#   - loopzero scale --replicas 11 (over Pro cap=5) → 4xx rejection
#   - HTTP 200 ainda OK após scale up
#
# Note: spec says loopzero scale stub — DB persistence missing, but this
# journey validates the API behavior for both happy + cap rejection.
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
# shellcheck disable=SC1091
source "$(dirname "$0")/_common.sh"

PROJECT="solo-server"
APP="api"
ENV="production"
WORK_DIR="/tmp/dogfood-journey-04"

journey_step "Setup fixture (reuse solo-server)"
rm -rf "$WORK_DIR"
cp -r "${REPO_ROOT}/infra/tests/dogfood/fixtures/solo-server" "$WORK_DIR"

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

journey_step "Initial deploy"
build_id=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" deploy --env "$ENV" --no-wait --json 2>&1 \
  | tail -1 | python3 -c "import json,sys; print(json.load(sys.stdin).get('build_id',''))" 2>/dev/null)
[[ -n "$build_id" ]] || journey_fail "no build_id"
final=$(wait_build_terminal "$build_id" 1500)
[[ "$final" == "SUCCEEDED" ]] || journey_fail "build did not succeed: $final"

HOST="${TENANT_ID}-${PROJECT}-${APP}-${ENV}.loopzero.local"
wait_http_200 "$HOST" 300 "/health" || journey_fail "HTTP 200 timeout"

journey_step "loopzero scale --replicas 2 (B40 fix: DB→deploy→K8s chain LIVE)"
# CHG-2026-05-05-DOGFOOD-B40: scale handler persists project NAME in
# app_scaling_config; ScalingResolver reads it during deploy submit;
# enforceScaling validates resolved config against plan caps; renderer
# emits Deployment.spec.replicas matching DB. End-to-end chain closure.
#
# Replicas=2 chosen (within Starter-tier cap of 2). Replicas=3 is
# rejected with REPLICAS_EXCEED_PLAN — exercised separately below.
( cd "$WORK_DIR" && "$LOOPZERO_BIN" scale --app "$APP" --env "$ENV" --replicas 2 --json ) > /tmp/journey-04-scale.json 2>&1 || \
  journey_fail "scale --replicas 2 failed: $(cat /tmp/journey-04-scale.json)"
journey_log "scale 2 response: $(head -c 200 /tmp/journey-04-scale.json)"
status=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))" < /tmp/journey-04-scale.json 2>/dev/null || echo "")
got_replicas=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('replicas',''))" < /tmp/journey-04-scale.json 2>/dev/null || echo "")
case "$status" in
  applied|no_change|acknowledged) : ;;
  *) journey_fail "expected status in {applied,no_change,acknowledged}, got: $status" ;;
esac
[[ "$got_replicas" == "2" ]] || journey_fail "expected replicas=2 in response, got: $got_replicas"
journey_log "Scale contract OK: status=$status replicas=2"

journey_step "Idempotency: re-run same scale → no_change"
( cd "$WORK_DIR" && "$LOOPZERO_BIN" scale --app "$APP" --env "$ENV" --replicas 2 --json ) > /tmp/journey-04-idem.json 2>&1 || \
  journey_fail "idempotent scale failed"
idem_status=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))" < /tmp/journey-04-idem.json 2>/dev/null || echo "")
[[ "$idem_status" == "no_change" ]] || journey_fail "expected idempotent status=no_change, got: $idem_status"
journey_log "Idempotency confirmed: status=no_change"

journey_step "loopzero deploy (apply scaling to cluster — D3: loopzero scale doesn't auto-deploy)"
build_id=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" deploy --env "$ENV" --no-wait --json 2>&1 \
  | tail -1 | python3 -c "import json,sys; print(json.load(sys.stdin).get('build_id',''))" 2>/dev/null)
[[ -n "$build_id" ]] || journey_fail "no build_id from redeploy"
final=$(wait_build_terminal "$build_id" 1500)
[[ "$final" == "SUCCEEDED" ]] || journey_fail "redeploy did not succeed: $final"
sleep 5  # allow K8s to apply

journey_step "kubectl asserts Deployment.spec.replicas=2 (B40 fix LIVE proof)"
NS="loopzero-${TENANT_ID}-${ENV}"
DEPLOY_NAME="${PROJECT}-${APP}"
ACTUAL_REPLICAS=$(kubectl --context "$WORKLOAD_CTX" -n "$NS" get deploy "$DEPLOY_NAME" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "")
[[ "$ACTUAL_REPLICAS" == "2" ]] || journey_fail "B40 chain broken: expected K8s replicas=2, got '$ACTUAL_REPLICAS' (DB row written but renderer/resolver not propagating)"
journey_log "B40 LIVE proof: kubectl deploy.spec.replicas=$ACTUAL_REPLICAS (matches scale)"

journey_step "Plan-cap probe: scale --replicas 3 in Starter (cap=2) → REPLICAS_EXCEED_PLAN"
out=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" scale --app "$APP" --env "$ENV" --replicas 3 --json 2>&1 || true)
echo "$out" > /tmp/journey-04-overcap.json
# scale handler itself doesn't enforce caps (D3 — only deploy enforces).
# So scale 3 succeeds at handler level (DB write), but next loopzero deploy
# rejects it. Test BOTH: scale handler accepts; deploy rejects.
journey_log "over-cap scale handler response: $(head -c 200 /tmp/journey-04-overcap.json)"

# Now deploy with cap-bypass row in DB → enforceScaling should reject.
overcap_deploy=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" deploy --env "$ENV" --no-wait --json 2>&1 || true)
echo "$overcap_deploy" > /tmp/journey-04-overcap-deploy.json
err_code=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('code',''))" < /tmp/journey-04-overcap-deploy.json 2>/dev/null || echo "")
[[ "$err_code" == "REPLICAS_EXCEED_PLAN" ]] || journey_fail "expected REPLICAS_EXCEED_PLAN on over-cap deploy, got code=$err_code (response: $(head -c 200 /tmp/journey-04-overcap-deploy.json))"
journey_log "Plan-cap LIVE: deploy rejected with REPLICAS_EXCEED_PLAN (resolved scaling enforced — MEET-2026-04-23-059 Constraint 6)"

# Reset to within-cap before HTTP probe
( cd "$WORK_DIR" && "$LOOPZERO_BIN" scale --app "$APP" --env "$ENV" --replicas 2 --json ) >/dev/null 2>&1 || true

journey_step "HTTP 200 still OK"
wait_http_200 "$HOST" 60 "/health" || journey_fail "HTTP 200 broke after scale"

journey_step "Audit log assertion (build.submitted emitted on deploy)"
assert_audit_entry "build.submitted" "" \
  || journey_fail "audit log missing build.submitted entries"

journey_step "Prova de Morte"
"$LOOPZERO_BIN" project delete "$PROJECT" --yes --json >/dev/null 2>&1 || journey_fail "project delete failed"
prove_dead "$TENANT_ID" "$PROJECT" "$ENV" 300 || journey_fail "Prova de Morte timeout"

journey_log "Journey 04 GREEN — scale fixed replicas + plan-cap probe"
exit 0
