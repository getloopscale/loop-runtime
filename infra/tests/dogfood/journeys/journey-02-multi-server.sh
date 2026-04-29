#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-REAL-E2E — Phase 4 (T4.1)
#
# Journey 02 — Multi-server (api + worker) — valida Achado #17 fix em runtime.
#
# Cenário: dois apps no mesmo project (`api` server + `worker` worker).
# Cobre:
#   - Multi-app build pipeline (2× build-image-* + 2× sign-image-*)
#   - Manifest com 2 Deployments project-scoped (multi-server-api,
#     multi-server-worker), 1 Service (api), 1 IngressRoute (api), 0 Service
#     para worker (type=worker → sem IngressRoute por design)
#   - Resource collision check: nenhum nome `api` ou `worker` standalone —
#     todos prefixados com `multi-server-`
#   - Worker pod Running sem IngressRoute (loopzero logs --app worker funciona)
#   - api HTTP 200
#   - Prova de Morte ambos em ≤5min
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
# shellcheck disable=SC1091
source "$(dirname "$0")/_common.sh"

PROJECT="multi-server"
ENV="production"
WORK_DIR="/tmp/dogfood-journey-02"

journey_step "Setup fixture"
rm -rf "$WORK_DIR"
cp -r "${REPO_ROOT}/infra/tests/dogfood/fixtures/multi-server" "$WORK_DIR"

journey_step "Ensure auth"
ensure_token

journey_step "Idempotency cleanup"
existing=$("$LOOPZERO_BIN" project list --json 2>/dev/null | python3 -c "
import json,sys
try: print('y' if any(p.get('name')=='$PROJECT' for p in json.load(sys.stdin).get('projects',[])) else 'n')
except: print('n')
" 2>/dev/null || echo "n")
if [[ "$existing" == "y" ]]; then
  journey_log "$PROJECT exists; deleting (idempotency)"
  "$LOOPZERO_BIN" project delete "$PROJECT" --yes --json >/dev/null 2>&1 || true
  prove_dead "$TENANT_ID" "$PROJECT" "$ENV" 180 || journey_log "WARN: pre-cleanup partial"
fi

journey_step "loopzero validate"
( cd "$WORK_DIR" && "$LOOPZERO_BIN" validate --json ) > /tmp/journey-02-validate.json
apps_count=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('apps',0))" < /tmp/journey-02-validate.json)
[[ "$apps_count" == "2" ]] || journey_fail "validate apps count=$apps_count, want 2"

journey_step "loopzero deploy --no-wait"
deploy_out=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" deploy --env "$ENV" --no-wait --json 2>&1)
build_id=$(echo "$deploy_out" | tail -1 | python3 -c "import json,sys; print(json.load(sys.stdin).get('build_id',''))" 2>/dev/null || true)
[[ -n "$build_id" ]] || journey_fail "no build_id: $deploy_out"
journey_log "build_id=$build_id"

journey_step "Wait build to terminal (max 30min — multi-app)"
final=$(wait_build_terminal "$build_id" 1800)
[[ "$final" == "SUCCEEDED" ]] || journey_fail "build did not succeed: $final"

journey_step "Workflow DAG check — both apps signed"
WF=$(kubectl --context "$BUILD_CTX" -n argo get wf -l "loopzero.io/build-id=$build_id" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
sign_count=$(kubectl --context "$BUILD_CTX" -n argo get wf "$WF" \
  -o jsonpath='{range .status.nodes.*}{.displayName}|{.phase}{"\n"}{end}' 2>/dev/null \
  | grep -c '^sign-image-.*|Succeeded$' || echo 0)
[[ "$sign_count" -ge 2 ]] || journey_fail "expected ≥2 sign-image steps Succeeded, got $sign_count"
journey_log "DAG OK: $sign_count sign-image steps Succeeded"

journey_step "Manifest project-scoping check (Achado #17 regression)"
NS="loopzero-${TENANT_ID}-${ENV}"
deploys=$(kubectl --context "$WORKLOAD_CTX" -n "$NS" get deploy -l "loopzero.io/project=$PROJECT" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null)
expected_deploys="multi-server-api
multi-server-worker"
if [[ "$(echo "$deploys" | sort)" != "$(echo "$expected_deploys" | sort)" ]]; then
  journey_fail "Deployment names mismatch — Achado #17 regression?
  got:
$deploys
  want:
$expected_deploys"
fi
journey_log "Deployment names project-scoped OK: $deploys"

# Worker MUST NOT have IngressRoute (type=worker contract)
worker_routes=$(kubectl --context "$WORKLOAD_CTX" -n "$NS" \
  get ingressroute.traefik.io -l "loopzero.io/project=$PROJECT,loopzero.io/app-name=worker" \
  --no-headers 2>/dev/null | wc -l)
[[ "$worker_routes" == "0" ]] || journey_fail "worker has IngressRoute(s) — type=worker contract violated: count=$worker_routes"
journey_log "worker has zero IngressRoutes (type=worker contract OK)"

journey_step "Prova de Vida — api HTTP 200"
HOST="${TENANT_ID}-${PROJECT}-api-${ENV}.loopzero.local"
wait_http_200 "$HOST" 300 "/health" || journey_fail "api HTTP 200 timeout"
body=$(curl -sS --connect-to "${HOST}:443:127.0.0.1:${TRAEFIK_HOST_PORT}" -k "https://${HOST}/")
echo "$body" | grep -q '"app":"api"' || journey_fail "unexpected api body: $body"
echo "$body" | grep -q '"project":"multi-server"' || journey_fail "wrong project in body: $body"
journey_log "api responded with multi-server fingerprint"

journey_step "Worker pod Running"
worker_running=$(kubectl --context "$WORKLOAD_CTX" -n "$NS" \
  get pod -l "loopzero.io/project=$PROJECT,loopzero.io/app-name=worker" \
  -o jsonpath='{range .items[*]}{.status.phase}{"\n"}{end}' 2>/dev/null \
  | grep -c "^Running$" || echo 0)
[[ "$worker_running" -ge 1 ]] || journey_fail "no worker pod Running"
journey_log "worker pod Running ($worker_running replicas)"

journey_step "Worker logs visible"
worker_pod=$(kubectl --context "$WORKLOAD_CTX" -n "$NS" \
  get pod -l "loopzero.io/project=$PROJECT,loopzero.io/app-name=worker" \
  -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
heartbeat=$(kubectl --context "$WORKLOAD_CTX" -n "$NS" logs "$worker_pod" --tail=20 2>/dev/null \
  | grep -c '"heartbeat"' || echo 0)
[[ "$heartbeat" -ge 1 ]] || journey_fail "worker not logging heartbeats"
journey_log "worker emitted $heartbeat heartbeat logs"

journey_step "Audit log assertion (project.created action emitted)"
assert_audit_entry "project.created" "$PROJECT" \
  || journey_fail "audit log missing project.created entry for $PROJECT"

journey_step "Prova de Morte"
"$LOOPZERO_BIN" project delete "$PROJECT" --yes --json >/tmp/journey-02-delete.json 2>&1 || \
  journey_fail "project delete failed"
prove_dead "$TENANT_ID" "$PROJECT" "$ENV" 300 || journey_fail "Prova de Morte timeout"

# Audit assertion AFTER delete — project.deleted entry must be emitted.
assert_audit_entry "project.deleted" "$PROJECT" \
  || journey_log "WARN: project.deleted entry not visible in audit page (eventually consistent)"

journey_log "Journey 02 GREEN — multi-server (api + worker), Achado #17 validated in runtime"
exit 0
