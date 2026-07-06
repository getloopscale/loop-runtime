#!/usr/bin/env bash
# LIVE bash dogfood for Journey J-03 Verification (code entry + resend cooldown).
#
# Plan: .claude/knowledge-base/plans/2026-06-06-dashboard-journey-j03-verification-plan.md
# Frontend: ResendCooldownButton (60s localStorage cooldown, T1.1) + PinInput
#   native paste/auto-submit (T1.2/T1.3), wired into verification.tsx.
#
# Scenarios (read-only — asserts the served build, never sends a real email):
#   A  the verification page is served (app-dev/verification -> 200 SPA shell).
#   B  the served bundle ships ResendCooldownButton — the "Didn't receive the
#      email? Resend" literal + the persisted-cooldown key are present.
#   C  pre-flight health (app-dev reachable).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md — the
# full email→code→verified path is the sponsor JUNTOS walkthrough
# (docs/operations/m0-live-validation-runbook.md).

set -euo pipefail

APP="${DASHBOARD:-https://app-dev.loopzero.example}"

JSON_MODE=0
SKIP_LIVE=0
SINGLE_SCENARIO=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --json) JSON_MODE=1; shift ;;
    --skip-live) SKIP_LIVE=1; shift ;;
    --scenario) SINGLE_SCENARIO="$2"; shift 2 ;;
    -h|--help) sed -n '2,18p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) shift ;;
  esac
done

log() { printf '[verification-flow-test] %s\n' "$*" >&2; }
warn() { printf '[verification-flow-test] WARN: %s\n' "$*" >&2; }

SCENARIO_PASS=0
SCENARIO_FAIL=0
record_result() {
  local name="$1" verdict="$2" detail="${3:-}"
  if [[ "$verdict" == "PASS" ]]; then
    SCENARIO_PASS=$((SCENARIO_PASS + 1)); log "PASS $name ${detail:+— $detail}"
  else
    SCENARIO_FAIL=$((SCENARIO_FAIL + 1)); log "FAIL $name ${detail:+— $detail}"
  fi
}
want() { [[ -z "$SINGLE_SCENARIO" || "$SINGLE_SCENARIO" == "$1" ]]; }

preflight() {
  local code
  code=$(curl -fsS -o /dev/null -w '%{http_code}' --max-time 15 "$APP/verification" 2>/dev/null || echo 000)
  [[ "$code" -ge 200 && "$code" -lt 400 ]]
}

scenario_a_verification_served() {
  want A || return 0
  local code
  code=$(curl -fsS -o /dev/null -w '%{http_code}' --max-time 20 "$APP/verification" 2>/dev/null || echo 000)
  if [[ "$code" -ge 200 && "$code" -lt 400 ]]; then
    record_result "A:verification-served" PASS "HTTP $code"
  else
    record_result "A:verification-served" FAIL "HTTP $code"
  fi
}

scenario_b_resend_cooldown_shipped() {
  want B || return 0
  # Canonical two-step probe (Cleanroom 2026-07-04): mine SPA index for the
  # verification-*.js code-split chunk reference, then probe that chunk. The
  # /verification HTML alone doesn't reference verification-*.js — it's
  # dynamically imported at route entry, so the original loop over the
  # /verification asset refs only saw bootstrap chunks.
  local html entry_js entry_body chunk chunk_body found_resend=0 found_key=0
  html=$(curl -fsS --max-time 20 "$APP/" 2>/dev/null || echo '')
  entry_js=$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)
  if [[ -n "$entry_js" ]]; then
    entry_body=$(curl -fsS --max-time 20 "$APP$entry_js" 2>/dev/null || echo '')
    for chunk in $(printf '%s' "$entry_body" | grep -oE '[/"]verification-[A-Za-z0-9_-]+\.js' | sed 's|^[/"]||' | sort -u); do
      chunk_body=$(curl -fsS --max-time 20 "$APP/assets/$chunk" 2>/dev/null || echo '')
      printf '%s' "$chunk_body" | grep -qF "Didn't receive the email? Resend" && found_resend=1
      printf '%s' "$chunk_body" | grep -qF 'loopzero-verify-resend-until' && found_key=1
      [[ "$found_resend" == "1" && "$found_key" == "1" ]] && break
    done
  fi
  if [[ "$found_resend" == "1" && "$found_key" == "1" ]]; then
    record_result "B:resend-cooldown-shipped" PASS "resend literal + persisted-cooldown key in bundle"
  else
    record_result "B:resend-cooldown-shipped" FAIL "resend=$found_resend key=$found_key"
  fi
}

scenario_c_preflight() {
  want C || return 0
  if preflight; then record_result "C:preflight" PASS; else record_result "C:preflight" FAIL "app-dev not ready"; fi
}

main() {
  if [[ "$SKIP_LIVE" == "1" ]]; then
    log "--skip-live: structural mode, no network"; exit 0
  fi
  if ! preflight; then warn "pre-flight failed — is dev-public Cleanroom up?"; exit 1; fi
  scenario_c_preflight
  scenario_a_verification_served
  scenario_b_resend_cooldown_shipped
  log "RESULT: $SCENARIO_PASS passed, $SCENARIO_FAIL failed"
  [[ "$JSON_MODE" == "1" ]] && printf '{"journey":"J-03-verification","pass":%d,"fail":%d}\n' "$SCENARIO_PASS" "$SCENARIO_FAIL"
  [[ "$SCENARIO_FAIL" -eq 0 ]]
}

main "$@"
