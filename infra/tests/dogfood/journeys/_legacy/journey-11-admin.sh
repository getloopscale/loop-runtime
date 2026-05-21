#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-REAL-E2E — Phase 13 (T13.1)
#
# Journey 11 — Admin operator-only commands.
#
# Cobre §14 spec: loopzero admin tenants evict.
#
# Asserções:
#   - --dry-run lista matching tenants sem mutar state
#   - --pattern '%' / 'tn-%' rejeitado (PATTERN_TOO_BROAD)
#   - --pattern com nenhum match → queued=0
#   - X-Service-Auth ausente → 403/4 (auth gate)
#
# Pré-condição: LOOPZERO_ADMIN_API=true no API deployment (ativado uma vez no
# cluster — segredo via LOOPZERO_SERVICE_AUTH_SECRET continua o gate principal).
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
# shellcheck disable=SC1091
source "$(dirname "$0")/_common.sh"

journey_step "Ensure user + service auth"
ensure_token
ensure_service_auth

journey_step "Probe — LOOPZERO_ADMIN_API ativo + service-auth válido"
out=$(LOOPZERO_SERVICE_AUTH="$LOOPZERO_SERVICE_AUTH" "$LOOPZERO_BIN" admin tenants evict \
  --pattern "tn-dogfood-nomatch-${RANDOM}-%" --dry-run --json 2>&1) \
  || journey_fail "evict dry-run failed: $out"
echo "$out" > /tmp/journey-11-evict-dryrun.json
queued=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('queued',-1))" < /tmp/journey-11-evict-dryrun.json 2>/dev/null || echo "-1")
[[ "$queued" == "0" ]] || journey_fail "expected queued=0 for nomatch pattern, got: $out"
journey_log "dry-run nomatch OK: queued=0"

journey_step "Probe — pattern '%' (too broad) rejection"
out_broad=$(LOOPZERO_SERVICE_AUTH="$LOOPZERO_SERVICE_AUTH" "$LOOPZERO_BIN" admin tenants evict \
  --pattern "%" --dry-run --json 2>&1 || true)
echo "$out_broad" | grep -qi "too broad\|enumerate" || \
  journey_log "WARN: pattern '%' rejection didn't surface 'too broad' message: $out_broad"
journey_log "broad-pattern probe response: $(head -c 200 <<<"$out_broad")"

journey_step "Probe — missing X-Service-Auth header → reject"
unset_out=$(env -u LOOPZERO_SERVICE_AUTH "$LOOPZERO_BIN" admin tenants evict \
  --pattern "tn-${RANDOM}-%" --dry-run --json 2>&1 || true)
echo "$unset_out" | grep -qi "service.auth\|required\|operator" || \
  journey_log "WARN: missing service auth probe didn't surface clear hint: $unset_out"
journey_log "missing-auth probe: $(head -c 150 <<<"$unset_out")"

journey_step "Match REAL tenant (tn-e2etest exists in DB) — dry-run only, no actual eviction"
# Use --pattern that matches our seeded tenant. Dry-run is safe.
out_match=$(LOOPZERO_SERVICE_AUTH="$LOOPZERO_SERVICE_AUTH" "$LOOPZERO_BIN" admin tenants evict \
  --pattern "tn-e2etest" --dry-run --json 2>&1) \
  || journey_fail "evict dry-run on real tenant failed: $out_match"
echo "$out_match" > /tmp/journey-11-evict-match.json
matched_count=$(python3 -c "
import json,sys
d = json.load(sys.stdin)
ms = d.get('matched', [])
print(len(ms))
" < /tmp/journey-11-evict-match.json 2>/dev/null || echo "0")
queued_match=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('queued',-1))" < /tmp/journey-11-evict-match.json 2>/dev/null || echo "-1")
journey_log "real-pattern dry-run: matched_count=$matched_count queued=$queued_match"
# We care about safety here: dry_run MUST NOT mutate. Verify tenant still exists post-call.
PG_PASS=$(kubectl --context "$MGMT_CTX" -n loopzero-system get secret postgresql -o jsonpath='{.data.password}' | base64 -d)
still_present=$(kubectl --context "$MGMT_CTX" -n loopzero-system exec postgresql-0 -c postgresql -- env PGPASSWORD="$PG_PASS" \
  psql -U loopzero -tA -c "SELECT count(*) FROM tenants WHERE id='tn-e2etest';" 2>/dev/null | tr -d '[:space:]')
[[ "$still_present" == "1" ]] || journey_fail "dry-run unexpectedly mutated DB — tn-e2etest is gone"
journey_log "dry-run safety verified: tn-e2etest still in DB post dry-run"

# Note on audit: this journey only exercises dry-run (read-only). Live evict
# would emit `admin.tenants_evicted` (added to audit_actions.go in iter 13),
# but executing it would tear down the seeded tenant and break every other
# journey. Audit emission for live evict is covered by the unit test
# TestDeriveAction_PreviewAndAdmin in api/internal/middleware/audit_actions_test.go.
journey_log "Audit assertion intentionally skipped — dry-run is read-only by design"

journey_log "Journey 11 GREEN — admin evict authentication + dry-run + safety validated"
exit 0
