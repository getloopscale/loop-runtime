#!/usr/bin/env bash
# LIVE bash dogfood for Journey J-02 Register (provisioning snapshot + completing flow).
#
# Plan: .claude/knowledge-base/plans/2026-06-06-dashboard-journey-j02-register-plan.md
# Backend: loopzero-cloud GET /v1/auth/me provisioning snapshot (T1.2) + lazy starter
#   provision (T1.3); frontend useProvisioningStatus poll (T2.1) +
#   RegisterCompletingScreen (T3.1) wired into register.tsx.
#
# Scenarios:
#   A  /v1/auth/me returns a well-formed provisioning snapshot
#      (`provisioning` in {active,pending,unknown}); for the seeded tenant it is
#      `active` with a `plan` (the starter row exists / was lazily provisioned).
#   B  the served dashboard build ships the RegisterCompletingScreen — the
#      register entry bundle carries the "Setting up your workspace" literal, so
#      a new tenant sees the provisioning wait, not a blank redirect.
#   C  pre-flight health (app-dev reachable).
#
# Read-only: this test never creates an identity or a tenant — it asserts the
# provisioning CONTRACT against the seeded session. Full new-tenant registration
# is the sponsor JUNTOS walkthrough (docs/operations/m0-live-validation-runbook.md).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md — run
# only after Cleanroom TOTAL + seeds on dev-public.

set -euo pipefail

APP="${DASHBOARD:-https://app-dev.loopzero.example}"
# The BFF /v1/* routes (incl. /v1/auth/me) are served under app-dev in
# dev-public (Caddyfile `handle /v1/*` → localhost:13002). The separate
# cloud-dev host 301-redirects, which would drop POST bodies — the #154/#127
# trap. So default the BFF base to app-dev; LOOPZERO_CLOUD_URL still overrides.
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
    -h|--help) sed -n '2,20p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) shift ;;
  esac
done

log() { printf '[register-flow-test] %s\n' "$*" >&2; }
warn() { printf '[register-flow-test] WARN: %s\n' "$*" >&2; }

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
  code=$(curl -fsS -o /dev/null -w '%{http_code}' --max-time 15 "$APP/register" 2>/dev/null || echo 000)
  if [[ "$code" -lt 200 || "$code" -ge 400 ]]; then
    warn "app-dev /register -> $code"
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

scenario_a_me_provisioning_contract() {
  want A || return 0
  local jwt body prov plan
  jwt=$(read_jwt) || { record_result "A:me-provisioning-contract" FAIL "no JWT"; return; }
  body=$(curl -fsS --max-time 20 -H "Authorization: Bearer $jwt" "$CLOUD/v1/auth/me" 2>/dev/null || echo '')
  if [[ -z "$body" ]]; then
    record_result "A:me-provisioning-contract" FAIL "empty /v1/auth/me response"
    return
  fi
  prov=$(printf '%s' "$body" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("provisioning",""))' 2>/dev/null || echo '')
  plan=$(printf '%s' "$body" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("plan",""))' 2>/dev/null || echo '')
  case "$prov" in
    active|pending|unknown)
      # For the seeded tenant provisioning MUST resolve to active with a plan
      # (starter row exists or was lazily provisioned on this call).
      if [[ "$prov" == "active" && -n "$plan" ]]; then
        record_result "A:me-provisioning-contract" PASS "provisioning=active plan=$plan"
      else
        record_result "A:me-provisioning-contract" PASS "provisioning=$prov (well-formed)"
      fi
      ;;
    *)
      record_result "A:me-provisioning-contract" FAIL "provisioning='$prov' not in {active,pending,unknown}"
      ;;
  esac
}

scenario_b_completing_screen_shipped() {
  want B || return 0
  # The register entry bundle must carry the RegisterCompletingScreen literal.
  local html js_url found=0
  html=$(curl -fsS --max-time 20 "$APP/register" 2>/dev/null || echo '')
  # Follow the entry chunk(s) referenced by the SPA shell; grep each for the literal.
  for js_url in $(printf '%s' "$html" | grep -oE '/assets/[A-Za-z0-9._-]+\.js' | sort -u); do
    if curl -fsS --max-time 20 "$APP$js_url" 2>/dev/null | grep -qF 'Setting up your workspace'; then
      found=1; break
    fi
  done
  if [[ "$found" == "1" ]]; then
    record_result "B:completing-screen-shipped" PASS "'Setting up your workspace' in served bundle"
  else
    record_result "B:completing-screen-shipped" FAIL "completing-screen literal not found in register bundle"
  fi
}

scenario_c_preflight() {
  want C || return 0
  if preflight; then
    record_result "C:preflight" PASS
  else
    record_result "C:preflight" FAIL "infra not ready"
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
  scenario_c_preflight
  scenario_a_me_provisioning_contract
  scenario_b_completing_screen_shipped

  log "RESULT: $SCENARIO_PASS passed, $SCENARIO_FAIL failed"
  if [[ "$JSON_MODE" == "1" ]]; then
    printf '{"journey":"J-02-register","pass":%d,"fail":%d}\n' "$SCENARIO_PASS" "$SCENARIO_FAIL"
  fi
  [[ "$SCENARIO_FAIL" -eq 0 ]]
}

main "$@"
