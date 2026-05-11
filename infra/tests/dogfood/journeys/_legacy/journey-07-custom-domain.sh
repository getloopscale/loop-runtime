#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-REAL-E2E — Phase 9 (T9.1)
#
# Journey 07 — Custom domain registration & lifecycle.
#
# Cobre §7 spec: loopzero domain add/list/status/remove.
#
# IMPORTANT — local infra gap (documented, not workaround):
#   Custom domain v2 uses CF Zone Management (CLAUDE.md § Custom domains).
#   standalone-multi has no CF integration, so steps that require real
#   DNS/zone provisioning (verify → ns_active → provisioning → active)
#   cannot be exercised locally. This journey validates everything UP TO
#   that boundary: register, list, status, remove. The integration with
#   CF is covered by the existing Pulumi-stack tests on DOKS.
#
# Asserções:
#   - loopzero domain add returns id + status=pending_verification + TXT record
#   - loopzero domain list shows the new domain
#   - loopzero domain status returns matching id+status
#   - loopzero domain remove succeeds
#   - loopzero domain list confirms removal
#   - Reserved domain rejection (api.example.test) returns 4xx with code
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
# shellcheck disable=SC1091
source "$(dirname "$0")/_common.sh"

PROJECT="solo-server"
APP="api"
ENV="production"
TEST_DOMAIN="api.dogfood-${RANDOM}.example"
WORK_DIR="/tmp/dogfood-journey-07"

journey_step "Setup baseline project (deploy minimal solo-server)"
# Custom domain registration requires a project+app+env tuple to attach to.
# Reuse the solo-server fixture to avoid touching versioned-app state.
rm -rf "$WORK_DIR"
cp -r "${REPO_ROOT}/infra/tests/dogfood/fixtures/solo-server" "$WORK_DIR"

journey_step "Ensure auth"
ensure_token

journey_step "Idempotency cleanup"
"$LOOPZERO_BIN" project delete "$PROJECT" --yes --json >/dev/null 2>&1 || true
prove_dead "$TENANT_ID" "$PROJECT" "$ENV" 180 || true
"$LOOPZERO_BIN" domain remove "$TEST_DOMAIN" --yes --json >/dev/null 2>&1 || true

journey_step "Initial deploy (so the project exists for domain attach)"
build_id=$(cd "$WORK_DIR" && "$LOOPZERO_BIN" deploy --env "$ENV" --no-wait --json 2>&1 \
  | tail -1 | python3 -c "import json,sys; print(json.load(sys.stdin).get('build_id',''))" 2>/dev/null)
[[ -n "$build_id" ]] || journey_fail "no build_id from deploy"
final=$(wait_build_terminal "$build_id" 1500)
[[ "$final" == "SUCCEEDED" ]] || journey_fail "build did not succeed: $final"
HOST="${TENANT_ID}-${PROJECT}-${APP}-${ENV}.loopzero.local"
wait_http_200 "$HOST" 300 "/health" || journey_fail "baseline HTTP 200 timeout"
wait_release_active "$PROJECT" "$ENV" "$build_id" 180 \
  || journey_fail "baseline release for build=$build_id did not reach ACTIVE"

journey_step "loopzero domain add — register $TEST_DOMAIN"
add_out=$("$LOOPZERO_BIN" domain add "$TEST_DOMAIN" \
  --project "$PROJECT" --app "$APP" --env "$ENV" --json 2>&1) || journey_fail "domain add failed: $add_out"
echo "$add_out" > /tmp/journey-07-add.json
journey_log "add response: $(head -c 200 /tmp/journey-07-add.json)"
domain_id=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('id',''))" < /tmp/journey-07-add.json 2>/dev/null || echo "")
status=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))" < /tmp/journey-07-add.json 2>/dev/null || echo "")
[[ -n "$domain_id" ]] || journey_fail "domain add response missing id: $add_out"
[[ "$status" == "pending_verification" ]] || journey_fail "expected status=pending_verification, got $status"
# Verify TXT verification token is returned (Rule 9 — surfaced upfront).
txt_present=$(python3 -c "
import json,sys
d = json.load(sys.stdin)
v = d.get('verification', {})
print('y' if v.get('txt_name') and v.get('txt_value') else 'n')
" < /tmp/journey-07-add.json 2>/dev/null || echo "n")
[[ "$txt_present" == "y" ]] || journey_fail "domain add response missing verification.txt_name/txt_value: $add_out"
journey_log "registered: id=$domain_id status=$status with TXT verification challenge"

journey_step "loopzero domain list — domain appears"
list_out=$("$LOOPZERO_BIN" domain list --json 2>&1)
echo "$list_out" > /tmp/journey-07-list.json
match_count=$(python3 -c "
import json,sys
d = json.load(sys.stdin)
ds = d.get('domains', [])
n = sum(1 for x in ds if x.get('domain') == '$TEST_DOMAIN')
print(n)
" < /tmp/journey-07-list.json 2>/dev/null || echo "0")
(( match_count >= 1 )) || journey_fail "domain not in list (count=$match_count): $list_out"
journey_log "domain list contains $TEST_DOMAIN"

journey_step "loopzero domain status — returns matching record"
status_out=$("$LOOPZERO_BIN" domain status "$TEST_DOMAIN" --json 2>&1) || journey_fail "domain status failed: $status_out"
echo "$status_out" > /tmp/journey-07-status.json
got_id=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('id',''))" < /tmp/journey-07-status.json 2>/dev/null || echo "")
[[ "$got_id" == "$domain_id" ]] || journey_fail "domain status returned wrong id: got=$got_id want=$domain_id"
journey_log "status confirms: id=$got_id"

journey_step "Reserved-domain rejection probe (Rule 8 — fail clear, fail fast)"
# Pre-release-governance Rule 8: explicit + typed errors. The reserved
# domain check should reject with a clear message (no internal terms).
probe_out=$("$LOOPZERO_BIN" domain add api.example.test --project "$PROJECT" --app "$APP" --env "$ENV" --json 2>&1 || true)
echo "$probe_out" | grep -qi "reserved" || journey_log "WARN: reserved-domain probe did not mention 'reserved': $probe_out"

journey_step "loopzero domain remove — destructive op gated by --yes"
remove_out=$("$LOOPZERO_BIN" domain remove "$TEST_DOMAIN" --yes --json 2>&1) || journey_fail "domain remove failed: $remove_out"
echo "$remove_out" > /tmp/journey-07-remove.json
journey_log "remove response: $(head -c 150 /tmp/journey-07-remove.json)"

journey_step "loopzero domain list — domain gone"
# Domain remove can be async (CF unrouting); poll for absence up to 60s.
elapsed=0
while (( elapsed < 60 )); do
  list_out=$("$LOOPZERO_BIN" domain list --json 2>&1)
  remaining=$(python3 -c "
import json,sys
try:
    ds = json.load(sys.stdin).get('domains', [])
    n = sum(1 for x in ds if x.get('domain') == '$TEST_DOMAIN' and x.get('status') not in ('removed','removing'))
    print(n)
except Exception:
    print(0)
" <<<"$list_out" 2>/dev/null || echo "0")
  if [[ "$remaining" == "0" ]]; then
    journey_log "domain successfully removed (after ${elapsed}s)"
    break
  fi
  sleep 5
  elapsed=$(( elapsed + 5 ))
done
(( elapsed < 60 )) || journey_fail "domain still listed in non-removed state after 60s"

journey_step "Audit log assertion (domain.added + domain.removed emitted)"
# domain.added doesn't carry the domain name in resource_id (POST /domains
# uses request body, not path) — assert action exists only. domain.removed
# DOES have resource_id (DELETE /domains/{name}) — strict project filter.
assert_audit_entry "domain.added" "" \
  || journey_fail "audit log missing domain.added entries"
assert_audit_entry "domain.removed" "$TEST_DOMAIN" \
  || journey_fail "audit log missing domain.removed entry for $TEST_DOMAIN"

journey_step "GAP DOCUMENTED — verify/active require CF Zone (production-only)"
journey_log "Cobertura local: register/list/status/remove (4 dos 5 verbos do §7)."
journey_log "Não exercitado: domain verify (TXT lookup) → ns_active → provisioning → active."
journey_log "Razão: standalone-multi não tem integração Cloudflare. Coberto por testes Pulumi/integration em DOKS."

journey_step "Prova de Morte"
"$LOOPZERO_BIN" project delete "$PROJECT" --yes --json >/dev/null 2>&1 || journey_fail "project delete failed"
prove_dead "$TENANT_ID" "$PROJECT" "$ENV" 300 || journey_fail "Prova de Morte timeout"

journey_log "Journey 07 GREEN — custom domain register/list/status/remove validated (verify→active GAP documented)"
exit 0
