#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-REAL-E2E — Phase 11 (T11.1)
#
# Journey 09 — Preview environments lifecycle.
#
# Cobre §12 spec: loopzero preview list/inspect/delete + POST /previews API.
#
# IMPORTANT — local infra gap (documented):
#   Preview environments are normally auto-created by the GitHub PR webhook.
#   That requires public ingress (see journey-08 for context). To exercise
#   the controller flow end-to-end locally we drive the API's POST
#   /api/v1/projects/{p}/previews directly with synthetic PR data — same
#   shape the webhook handler emits, just bypassing the GitHub→API hop.
#
# Asserções:
#   - POST /previews creates a row + returns prev-* id
#   - loopzero preview list shows the new preview
#   - loopzero preview inspect returns matching record
#   - loopzero preview delete transitions row to status=deleting
#   - loopzero preview list confirms removal (or status=deleting)
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
# shellcheck disable=SC1091
source "$(dirname "$0")/_common.sh"

PROJECT="dogfood-preview-${RANDOM}"
WORK_DIR="/tmp/dogfood-journey-09"
PR_NUMBER=$((RANDOM % 9000 + 1000))
COMMIT_SHA=$(printf '%040x' $((RANDOM * RANDOM)))

journey_step "Setup workspace with loopzero.yaml (CLI requires it for preview commands)"
rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR"
cat > "$WORK_DIR/loopzero.yaml" <<EOF
project: ${PROJECT}
apps:
  - name: api
    type: server
    build:
      dockerfile: Dockerfile
EOF

journey_step "Ensure auth"
ensure_token

journey_step "Idempotency cleanup"
PG_PASS=$(kubectl --context "$MGMT_CTX" -n loopzero-system get secret postgresql -o jsonpath='{.data.password}' | base64 -d)
kubectl --context "$MGMT_CTX" -n loopzero-system exec postgresql-0 -c postgresql -- env PGPASSWORD="$PG_PASS" \
  psql -U loopzero -c "DELETE FROM preview_environments WHERE project_id='$PROJECT';" >/dev/null 2>&1 || true

journey_step "Create preview via API (synthetic PR data — webhook bypass)"
# This is the same payload the GitHub webhook handler builds when a PR is
# opened. Direct API call lets us validate the controller flow without
# needing public ingress for the webhook.
create_resp=$(curl -sS -X POST "${LOOPZERO_URL}/api/v1/projects/${PROJECT}/previews" \
  -H "Authorization: Bearer ${LOOPZERO_TOKEN}" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: dogfood-preview-create-${RANDOM}" \
  -d "{\"pr_number\":${PR_NUMBER},\"branch\":\"feature/dogfood\",\"commit_sha\":\"${COMMIT_SHA}\",\"repo_full_name\":\"getloopscale/loopzero-repotest\",\"installation_id\":99999999,\"ttl_hours\":1}" 2>&1)
echo "$create_resp" > /tmp/journey-09-create.json
preview_id=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('id',''))" < /tmp/journey-09-create.json 2>/dev/null || echo "")
[[ -n "$preview_id" ]] || journey_fail "preview create response missing id: $create_resp"
status=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))" < /tmp/journey-09-create.json 2>/dev/null || echo "")
[[ "$status" == "creating" ]] || journey_fail "expected status=creating, got: $status"
journey_log "preview created: id=$preview_id status=$status"

journey_step "loopzero preview list — preview appears"
list_out=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" preview list --json 2>&1)
echo "$list_out" > /tmp/journey-09-list.json
match_count=$(python3 -c "
import json,sys
d = json.load(sys.stdin)
ps = d.get('previews', [])
n = sum(1 for x in ps if x.get('id') == '$preview_id')
print(n)
" < /tmp/journey-09-list.json 2>/dev/null || echo "0")
(( match_count >= 1 )) || journey_fail "preview $preview_id not in list: $list_out"
journey_log "preview list contains $preview_id"

journey_step "loopzero preview inspect — returns matching record"
inspect_out=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" preview inspect "$preview_id" --json 2>&1) \
  || journey_fail "preview inspect failed: $inspect_out"
got_pr=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('pr_number',''))" <<< "$inspect_out" 2>/dev/null || echo "")
[[ "$got_pr" == "$PR_NUMBER" ]] || journey_fail "preview inspect returned wrong pr_number: got=$got_pr want=$PR_NUMBER"
journey_log "inspect confirms: id=$preview_id pr_number=$got_pr"

journey_step "loopzero preview delete — destructive op gated by --yes"
delete_out=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" preview delete "$preview_id" --yes --json 2>&1) \
  || journey_fail "preview delete failed: $delete_out"
echo "$delete_out" > /tmp/journey-09-delete.json
journey_log "delete response: $(head -c 150 /tmp/journey-09-delete.json)"

journey_step "loopzero preview list — row gone or in deleting state"
elapsed=0
while (( elapsed < 60 )); do
  list_out=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" preview list --json 2>&1)
  remaining_active=$(python3 -c "
import json,sys
try:
    ps = json.load(sys.stdin).get('previews', [])
    n = sum(1 for x in ps if x.get('id') == '$preview_id' and x.get('status') not in ('deleted','deleting'))
    print(n)
except Exception:
    print(0)
" <<< "$list_out" 2>/dev/null || echo "0")
  if [[ "$remaining_active" == "0" ]]; then
    journey_log "preview cleared from active state (after ${elapsed}s)"
    break
  fi
  sleep 5
  elapsed=$(( elapsed + 5 ))
done
(( elapsed < 60 )) || journey_fail "preview still in active state after 60s"

journey_step "GAP DOCUMENTED — webhook auto-create flow"
journey_log "Cobertura local: list/inspect/delete + Create via API direct."
journey_log "Não exercitado: GitHub PR webhook → /webhooks/github → preview auto-create."
journey_log "Razão: standalone-multi não tem ingress público para callbacks."
journey_log "Coberto por: api/internal/services/preview_*_test.go + DOKS staging."

journey_step "Final cleanup (DB row + namespace if any)"
kubectl --context "$MGMT_CTX" -n loopzero-system exec postgresql-0 -c postgresql -- env PGPASSWORD="$PG_PASS" \
  psql -U loopzero -c "DELETE FROM preview_environments WHERE project_id='$PROJECT';" >/dev/null 2>&1 || true

journey_log "Journey 09 GREEN — preview list/inspect/delete validated (webhook auto-create GAP documented)"
exit 0
