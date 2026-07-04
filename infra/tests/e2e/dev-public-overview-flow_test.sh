#!/usr/bin/env bash
# LIVE bash dogfood for Journey J-06 Overview (KPI strip: plan_price + deploys aggregate).
#
# Plan: .claude/knowledge-base/plans/2026-06-06-dashboard-journey-j06-overview-plan.md
# Design: docs/decisions/2026-07-03-j06-overview-backend-read-model-boundaries.md
#         + docs/meetings/records/MEET-2026-07-03-003.md
# Backend: loopzero-cloud GET /v1/billing plan_price (D1, observed from Stripe) +
#   engine GET /api/v1/builds/stats?window=7d deploys aggregate (D2, tenant-scoped);
#   frontend useOverviewKPIs + OverviewKPIStrip wired into overview.tsx.
#
# Scenarios:
#   A  /v1/billing emits the additive `plan_price` value object (honest recurring
#      list price; NOT a `current_spend`/"Spend MTD" field). For the seeded tenant
#      it is present with amount_minor+currency+interval, OR omitted (enterprise /
#      not-yet-persisted) — never a fabricated 0.
#   B  /engine/api/v1/builds/stats?window=7d returns 200 with a dense (7-entry)
#      UTC by_day series + last_7d_count + timezone "UTC" (server-side aggregate,
#      tenant-scoped — the client never re-aggregates).
#   C  the served dashboard build ships the OverviewKPIStrip — the entry bundle
#      carries the "Deploys (7d)" + "Plan" KPI labels (honest "Plan", not "Spend").
#
# Read-only: asserts the CONTRACT against the seeded session. No mutation.
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md — run
# only after Cleanroom TOTAL + seeds on dev-public.

set -euo pipefail

APP="${DASHBOARD:-https://app-dev.loopzero.example}"
# The BFF (/v1/*) and the engine (/engine/api/v1/*) are both served under app-dev
# in dev-public (Caddyfile handle /v1/* → :13002, handle /engine/* → engine).
# cloud-dev 301-redirects, dropping POST bodies — the #154/#127 trap.
CLOUD="${LOOPZERO_CLOUD_URL:-$APP}"
JWT_FILE="${LOOPZERO_JWT_FILE:-$HOME/.loopzero/local-dogfood-token.txt}"

JSON_MODE=0
SKIP_LIVE=0
SINGLE_SCENARIO=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --json) JSON_MODE=1; shift ;;
    --skip-live) SKIP_LIVE=1; shift ;;
    --scenario) SINGLE_SCENARIO="$2"; shift 2 ;;
    -h|--help) sed -n '2,30p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) shift ;;
  esac
done

log() { printf '[overview-flow-test] %s\n' "$*" >&2; }
warn() { printf '[overview-flow-test] WARN: %s\n' "$*" >&2; }

SCENARIO_PASS=0
SCENARIO_FAIL=0
record_result() {
  local name="$1" verdict="$2" detail="${3:-}"
  if [[ "$verdict" == "PASS" ]]; then
    SCENARIO_PASS=$((SCENARIO_PASS + 1))
    log "PASS $name ${detail:+— $detail}"
  else
    SCENARIO_FAIL=$((SCENARIO_FAIL + 1))
    log "FAIL $name ${detail:+— $detail}"
  fi
}

want() { [[ -z "$SINGLE_SCENARIO" || "$SINGLE_SCENARIO" == "$1" ]]; }

preflight() {
  log "pre-flight: $APP reachable"
  local code
  code=$(curl -fsS -o /dev/null -w '%{http_code}' --max-time 15 "$APP/" 2>/dev/null || echo 000)
  if [[ "$code" -lt 200 || "$code" -ge 400 ]]; then
    warn "app-dev / -> $code"
    return 1
  fi
  return 0
}

read_jwt() {
  if [[ ! -f "$JWT_FILE" ]]; then
    warn "JWT seed missing at $JWT_FILE — run seed-admin-loopzero-dev.sh"
    return 1
  fi
  tr -d '[:space:]' < "$JWT_FILE"
}

scenario_a_billing_plan_price() {
  want A || return 0
  local jwt body
  jwt=$(read_jwt) || { record_result "A:billing-plan-price" FAIL "no JWT"; return; }
  body=$(curl -fsS --max-time 20 -H "Authorization: Bearer $jwt" "$CLOUD/v1/billing" 2>/dev/null || echo '')
  if [[ -z "$body" ]]; then
    record_result "A:billing-plan-price" FAIL "empty /v1/billing response"
    return
  fi
  # Must NEVER expose a dishonest "current_spend"/"Spend MTD" money field.
  if printf '%s' "$body" | grep -qE 'current_spend|spend_mtd'; then
    record_result "A:billing-plan-price" FAIL "dishonest spend field present (must be plan_price)"
    return
  fi
  local shape
  shape=$(printf '%s' "$body" | python3 -c '
import sys, json
d = json.load(sys.stdin)
pp = d.get("plan_price")
if pp is None:
    print("omitted")  # enterprise / not-yet-persisted — valid (honest, no fake 0)
elif isinstance(pp, dict) and "amount_minor" in pp and "currency" in pp and "interval" in pp:
    print("present:%s:%s" % (pp["amount_minor"], pp["currency"]))
else:
    print("malformed")
' 2>/dev/null || echo 'malformed')
  case "$shape" in
    present:*|omitted)
      record_result "A:billing-plan-price" PASS "plan_price=$shape (honest value object / omit)" ;;
    *)
      record_result "A:billing-plan-price" FAIL "plan_price shape invalid: $shape" ;;
  esac
}

scenario_b_deploys_stats() {
  want B || return 0
  local jwt body
  jwt=$(read_jwt) || { record_result "B:deploys-stats" FAIL "no JWT"; return; }
  body=$(curl -fsS --max-time 20 -H "Authorization: Bearer $jwt" "$CLOUD/engine/api/v1/builds/stats?window=7d" 2>/dev/null || echo '')
  if [[ -z "$body" ]]; then
    record_result "B:deploys-stats" FAIL "empty /builds/stats response"
    return
  fi
  local verdict
  verdict=$(printf '%s' "$body" | python3 -c '
import sys, json
d = json.load(sys.stdin)
by_day = d.get("by_day")
ok = (
    d.get("timezone") == "UTC"
    and d.get("window_days") == 7
    and isinstance(by_day, list) and len(by_day) == 7      # dense, zero-filled
    and "last_7d_count" in d
    and all("date" in b and "count" in b for b in by_day)
)
print("ok" if ok else "bad:%s" % json.dumps({"window_days": d.get("window_days"), "n": len(by_day) if isinstance(by_day, list) else None, "tz": d.get("timezone")}))
' 2>/dev/null || echo 'bad:parse')
  if [[ "$verdict" == "ok" ]]; then
    record_result "B:deploys-stats" PASS "dense 7-day UTC series + last_7d_count"
  else
    record_result "B:deploys-stats" FAIL "$verdict"
  fi
}

scenario_c_kpi_strip_shipped() {
  want C || return 0
  local html js_url found_deploys=0 found_plan=0
  html=$(curl -fsS --max-time 20 "$APP/" 2>/dev/null || echo '')
  for js_url in $(printf '%s' "$html" | grep -oE '/assets/[A-Za-z0-9._-]+\.js' | sort -u); do
    local chunk
    chunk=$(curl -fsS --max-time 20 "$APP$js_url" 2>/dev/null || echo '')
    printf '%s' "$chunk" | grep -qF 'Deploys (7d)' && found_deploys=1
    printf '%s' "$chunk" | grep -qF 'Active envs' && found_plan=1
    [[ "$found_deploys" == 1 && "$found_plan" == 1 ]] && break
  done
  if [[ "$found_deploys" == 1 && "$found_plan" == 1 ]]; then
    record_result "C:kpi-strip-shipped" PASS "'Deploys (7d)' + 'Active envs' in served bundle"
  else
    record_result "C:kpi-strip-shipped" FAIL "KPI strip literals not found (deploys=$found_deploys active=$found_plan)"
  fi
}

main() {
  if [[ "$SKIP_LIVE" == "1" ]]; then
    log "--skip-live: structural mode, no network"
    exit 0
  fi
  if ! preflight; then
    warn "pre-flight failed — is dev-public Cleanroom up?"
    exit 1
  fi
  scenario_a_billing_plan_price
  scenario_b_deploys_stats
  scenario_c_kpi_strip_shipped

  log "RESULT: $SCENARIO_PASS passed, $SCENARIO_FAIL failed"
  if [[ "$JSON_MODE" == "1" ]]; then
    printf '{"journey":"J-06-overview","pass":%d,"fail":%d}\n' "$SCENARIO_PASS" "$SCENARIO_FAIL"
  fi
  [[ "$SCENARIO_FAIL" -eq 0 ]]
}

main "$@"
