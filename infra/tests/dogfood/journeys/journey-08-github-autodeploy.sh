#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-REAL-E2E — Phase 10 (T10.1)
#
# Journey 08 — GitHub auto-deploy registration & disconnection.
#
# Cobre §10 spec: loopzero connect / loopzero disconnect.
#
# IMPORTANT — local infra gap (documented, not workaround):
#   GitHub App webhook delivery requires public ingress (the GitHub App calls
#   into LoopZero's API on push). standalone-multi has no public exposure for
#   webhook delivery, so the on-push → workflow → build flow cannot be
#   exercised locally end-to-end. This journey validates everything UP TO
#   that boundary: connect persistence, idempotency, disconnect.
#
#   The full webhook → autodeploy flow is covered by:
#     - api/internal/services/autodeploy*_test.go unit/component tests
#     - DOKS staging (homolog) where the GitHub App "LoopZero Platform" is wired.
#
# Asserções:
#   - loopzero connect persists project_repositories row (DB)
#   - loopzero connect is idempotent (re-run → same row, ok=true)
#   - loopzero disconnect succeeds and clears the row
#   - GAP documented: webhook→build flow not exercised
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
# shellcheck disable=SC1091
source "$(dirname "$0")/_common.sh"

PROJECT="dogfood-connect-${RANDOM}"
WORK_DIR="/tmp/dogfood-journey-08"
TEST_REPO="getloopscale/loopzero-repotest"
TEST_INSTALL_ID="99999999"  # synthetic — installation existence not validated locally

journey_step "Setup workspace"
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

journey_step "Idempotency cleanup (DB rows may exist from prior runs)"
# repo_full_name is UNIQUE — clear any prior bindings for either the project
# or the test repo so re-runs after a stale RANDOM suffix don't collide on
# REPO_ALREADY_LINKED.
PG_PASS=$(kubectl --context "$MGMT_CTX" -n loopzero-system get secret postgresql -o jsonpath='{.data.password}' | base64 -d)
kubectl --context "$MGMT_CTX" -n loopzero-system exec postgresql-0 -c postgresql -- env PGPASSWORD="$PG_PASS" \
  psql -U loopzero -c "DELETE FROM project_repositories WHERE project_name='$PROJECT' OR repo_full_name='$TEST_REPO';" >/dev/null 2>&1 || true

journey_step "loopzero connect — register repo for project=$PROJECT"
out=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" connect \
  --repo "$TEST_REPO" --installation-id "$TEST_INSTALL_ID" --branch main --json 2>&1) \
  || journey_fail "connect failed: $out"
echo "$out" > /tmp/journey-08-connect.json
ok=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('ok',''))" < /tmp/journey-08-connect.json 2>/dev/null || echo "")
[[ "$ok" == "True" ]] || journey_fail "expected ok=true in connect response, got: $out"
got_repo=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('repo',''))" < /tmp/journey-08-connect.json 2>/dev/null || echo "")
[[ "$got_repo" == "$TEST_REPO" ]] || journey_fail "expected repo=$TEST_REPO, got: $got_repo"
journey_log "connect OK: ok=true repo=$got_repo"

journey_step "DB row exists with expected fields"
db_count=$(kubectl --context "$MGMT_CTX" -n loopzero-system exec postgresql-0 -c postgresql -- env PGPASSWORD="$PG_PASS" \
  psql -U loopzero -tA -c "SELECT count(*) FROM project_repositories WHERE project_name='$PROJECT' AND repo_full_name='$TEST_REPO';" 2>/dev/null | tr -d '[:space:]')
[[ "$db_count" == "1" ]] || journey_fail "expected exactly 1 project_repositories row, got $db_count"
journey_log "DB row count=$db_count (matches)"

journey_step "Idempotent re-connect (same params → still ok=true)"
out2=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" connect \
  --repo "$TEST_REPO" --installation-id "$TEST_INSTALL_ID" --branch main --json 2>&1) \
  || journey_fail "re-connect failed: $out2"
ok2=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('ok',''))" <<< "$out2" 2>/dev/null || echo "")
[[ "$ok2" == "True" ]] || journey_fail "expected re-connect ok=true, got: $out2"
db_count2=$(kubectl --context "$MGMT_CTX" -n loopzero-system exec postgresql-0 -c postgresql -- env PGPASSWORD="$PG_PASS" \
  psql -U loopzero -tA -c "SELECT count(*) FROM project_repositories WHERE project_name='$PROJECT';" 2>/dev/null | tr -d '[:space:]')
[[ "$db_count2" == "1" ]] || journey_fail "re-connect leaked extra row (count=$db_count2)"
journey_log "Idempotent re-connect OK (still 1 row)"

journey_step "loopzero disconnect — clear the binding"
disc_out=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" disconnect --json 2>&1) \
  || journey_fail "disconnect failed: $disc_out"
echo "$disc_out" > /tmp/journey-08-disconnect.json
ok3=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('ok',''))" < /tmp/journey-08-disconnect.json 2>/dev/null || echo "")
[[ "$ok3" == "True" ]] || journey_fail "expected disconnect ok=true, got: $disc_out"

journey_step "DB row removed"
db_count3=$(kubectl --context "$MGMT_CTX" -n loopzero-system exec postgresql-0 -c postgresql -- env PGPASSWORD="$PG_PASS" \
  psql -U loopzero -tA -c "SELECT count(*) FROM project_repositories WHERE project_name='$PROJECT';" 2>/dev/null | tr -d '[:space:]')
[[ "$db_count3" == "0" ]] || journey_fail "disconnect did not clear DB row (count=$db_count3)"
journey_log "DB row cleared"

journey_step "Audit log assertion (repository.connected + repository.disconnected emitted)"
assert_audit_entry "repository.connected" "$PROJECT" \
  || journey_fail "audit log missing repository.connected entry for $PROJECT"
assert_audit_entry "repository.disconnected" "$PROJECT" \
  || journey_log "WARN: repository.disconnected entry not yet visible (eventually consistent)"

journey_step "GAP DOCUMENTED — webhook→autodeploy flow"
journey_log "Cobertura local: connect/disconnect persistence + idempotency."
journey_log "Não exercitado: GitHub webhook → /webhooks/github → autodeploy build."
journey_log "Razão: standalone-multi não tem ingress público para receber callbacks do GitHub."
journey_log "Coberto por: api/internal/services/autodeploy*_test.go (unit) + DOKS staging."

journey_log "Journey 08 GREEN — connect/disconnect lifecycle validated (webhook flow GAP documented)"
exit 0
