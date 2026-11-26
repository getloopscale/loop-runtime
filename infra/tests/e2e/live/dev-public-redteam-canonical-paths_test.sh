#!/usr/bin/env bash
# Red Team loop-console#68 + #43 post-fix-verification regression-protector
# LIVE test. Reproduces the EXACT attack vector the Red Team used on 2026-06-27
# AFTER my first round of fixes (which only covered `/self-service/{login,
# registration}/methods/*`, missing the canonical browser-flow submit paths
# `/self-service/{login,registration}` without the `/methods/` suffix).
#
# Scenarios:
#   A — #68 login throttle via canonical path: 12 wrong-pwd POSTs to
#       `/self-service/login?flow=<id>` MUST surface ≥ 2 × HTTP 429.
#   B — #43 enumeration via canonical path: byte-identical universal
#       response body for NEW vs EXISTING email POSTs to
#       `/self-service/registration?flow=<id>` (browser flow). NEW and
#       EXISTING (admin@getloopscale.com) MUST return IDENTICAL status + body.
#
# Per `.claude/rules/cleanroom-up-live-test-mandate.md` UNBREAKABLE +
# `feedback_always_rebuild_push_rollout_for_red_team_2026_06_27`: this is
# operator HARD STOP; Red Team can rerun it against dev-public to validate
# the canonical-path closure.
#
# Exit codes:
#   0  both scenarios PASS (Red Team #68 + #43 closed via canonical paths)
#   1  scenario(s) FAIL (regression — closure incomplete on canonical paths)
#   2  BLOCKED (dev-public unreachable, flow init failed)
#
# Usage:
#   bash infra/tests/e2e/live/dev-public-redteam-canonical-paths_test.sh \
#     [--skip-live] [--json] [--scenario=A|B] [--help]

set -euo pipefail

TARGET_HOST="${TARGET_HOST:-https://auth-dev.getloopscale.com}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO="all"

# Per-run synthetic IP avoids cross-run bucket pollution (EC-12 canonical).
SYNTH_IP="${SYNTH_IP:-192.0.2.$(( ( RANDOM % 200 ) + 1 ))}"
SYNTH_IP_B="${SYNTH_IP_B:-198.51.100.$(( ( RANDOM % 200 ) + 1 ))}"

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-live) SKIP_LIVE=1 ; shift ;;
    --json)      JSON_OUTPUT=1 ; shift ;;
    --scenario=*) SCENARIO="${1#*=}" ; shift ;;
    -h|--help)
      sed -n '1,29p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) printf 'unknown arg: %s\n' "$1" >&2 ; exit 1 ;;
  esac
done

log()  { if [ "$JSON_OUTPUT" = "0" ]; then printf '[redteam-canonical] %s\n' "$*" >&2; fi ; }
emit() {
  local verdict="$1" reason="${2:-}"
  if [ "$JSON_OUTPUT" = "1" ]; then
    printf '{"slug":"dev-public-redteam-canonical-paths","verdict":"%s","scenario":"%s","reason":"%s"}\n' \
      "$verdict" "$SCENARIO" "$reason"
  else
    printf '[redteam-canonical] verdict=%s scenario=%s reason=%s\n' "$verdict" "$SCENARIO" "$reason"
  fi
}

if [ "$SKIP_LIVE" = "1" ]; then
  log "Hermetic --skip-live mode — no LIVE invocation."
  emit "SKIPPED" "hermetic-skip-live"
  exit 0
fi

scenario_a() {
  log "Scenario A — #68 canonical-path login throttle (12 wrong-pwd to /self-service/login)"
  local flow_json flow_id
  flow_json="$(curl -sS --max-time 10 \
                -H "cf-connecting-ip: $SYNTH_IP" \
                "$TARGET_HOST/self-service/login/api" 2>&1 || true)"
  flow_id="$(printf '%s' "$flow_json" | jq -r '.id // empty' 2>/dev/null || true)"
  if [ -z "$flow_id" ] || [ "$flow_id" = "null" ]; then
    log "BLOCKED: login flow-init returned no .id"
    emit "BLOCKED" "scenario-a-flow-init-failed"
    return 2
  fi
  local hits_429=0 code
  for i in $(seq 1 12); do
    code="$(curl -sS --max-time 5 -o /dev/null -w '%{http_code}' \
            -X POST "$TARGET_HOST/self-service/login?flow=$flow_id" \
            -H "cf-connecting-ip: $SYNTH_IP" \
            -H 'Content-Type: application/json' \
            -H 'Accept: application/json' \
            -d '{"identifier":"admin@getloopscale.com","password":"wrong-pwd","method":"password"}' \
            2>&1 || true)"
    [ "$code" = "429" ] && hits_429=$((hits_429 + 1))
    log "  req $i (canonical /self-service/login) → $code"
  done
  if [ "$hits_429" -lt 2 ]; then
    log "FAIL: only $hits_429/12 requests returned 429 (expected ≥ 2 — bucket 10/min should engage on 11th+12th)"
    emit "FAIL" "scenario-a-canonical-path-not-throttled-only-${hits_429}-of-12-429"
    return 1
  fi
  log "PASS: scenario A — canonical-path /self-service/login engaged login_throttle ($hits_429/12 = 429)"
  return 0
}

scenario_b() {
  log "Scenario B — #43 canonical-path register enumeration (browser flow new vs existing email)"
  local jar_new jar_existing flow_new flow_existing csrf_new csrf_existing
  jar_new="$(mktemp)"
  jar_existing="$(mktemp)"
  trap 'rm -f "$jar_new" "$jar_existing"' RETURN

  # NEW email path.
  curl -sS -c "$jar_new" -H "cf-connecting-ip: $SYNTH_IP_B" \
       "$TARGET_HOST/self-service/registration/browser" -L >/dev/null 2>&1 || true
  flow_new="$(curl -sS -b "$jar_new" -c "$jar_new" -H "cf-connecting-ip: $SYNTH_IP_B" \
              "$TARGET_HOST/self-service/registration/api" 2>&1 | jq -r '.id // empty' 2>/dev/null || true)"
  if [ -z "$flow_new" ] || [ "$flow_new" = "null" ]; then
    log "BLOCKED: NEW-email flow-init failed"
    emit "BLOCKED" "scenario-b-new-flow-init-failed"
    return 2
  fi
  csrf_new="$(curl -sS -b "$jar_new" -H "cf-connecting-ip: $SYNTH_IP_B" \
              "$TARGET_HOST/self-service/registration/flows?id=$flow_new" 2>&1 \
              | jq -r '.ui.nodes[] | select(.attributes.name=="csrf_token") | .attributes.value' \
              2>/dev/null | head -1)"
  local new_email body_new code_new
  new_email="newprobe-$(date +%s)-$RANDOM@example.invalid"
  body_new="$(curl -sS -b "$jar_new" -c "$jar_new" -H "cf-connecting-ip: $SYNTH_IP_B" \
              -X POST "$TARGET_HOST/self-service/registration?flow=$flow_new" \
              -H 'Content-Type: application/json' -H 'Accept: application/json' \
              -d "{\"method\":\"password\",\"csrf_token\":\"$csrf_new\",\"traits\":{\"email\":\"$new_email\",\"name\":\"T\"},\"password\":\"ValidPwd13c-$(date +%s)$RANDOM\"}" \
              -w '\n--HTTP=%{http_code}--' 2>&1 || true)"
  code_new="$(printf '%s' "$body_new" | grep -oE 'HTTP=[0-9]+' | head -1 | sed 's/HTTP=//')"
  log "  NEW email ($new_email) HTTP=$code_new"

  # EXISTING email path.
  curl -sS -c "$jar_existing" -H "cf-connecting-ip: $SYNTH_IP_B" \
       "$TARGET_HOST/self-service/registration/browser" -L >/dev/null 2>&1 || true
  flow_existing="$(curl -sS -b "$jar_existing" -c "$jar_existing" -H "cf-connecting-ip: $SYNTH_IP_B" \
                   "$TARGET_HOST/self-service/registration/api" 2>&1 | jq -r '.id // empty' 2>/dev/null || true)"
  if [ -z "$flow_existing" ] || [ "$flow_existing" = "null" ]; then
    log "BLOCKED: EXISTING-email flow-init failed"
    emit "BLOCKED" "scenario-b-existing-flow-init-failed"
    return 2
  fi
  csrf_existing="$(curl -sS -b "$jar_existing" -H "cf-connecting-ip: $SYNTH_IP_B" \
                   "$TARGET_HOST/self-service/registration/flows?id=$flow_existing" 2>&1 \
                   | jq -r '.ui.nodes[] | select(.attributes.name=="csrf_token") | .attributes.value' \
                   2>/dev/null | head -1)"
  local body_existing code_existing
  body_existing="$(curl -sS -b "$jar_existing" -c "$jar_existing" -H "cf-connecting-ip: $SYNTH_IP_B" \
                   -X POST "$TARGET_HOST/self-service/registration?flow=$flow_existing" \
                   -H 'Content-Type: application/json' -H 'Accept: application/json' \
                   -d "{\"method\":\"password\",\"csrf_token\":\"$csrf_existing\",\"traits\":{\"email\":\"admin@getloopscale.com\",\"name\":\"T\"},\"password\":\"ValidPwd13c-$(date +%s)$RANDOM\"}" \
                   -w '\n--HTTP=%{http_code}--' 2>&1 || true)"
  code_existing="$(printf '%s' "$body_existing" | grep -oE 'HTTP=[0-9]+' | head -1 | sed 's/HTTP=//')"
  log "  EXISTING email (admin@getloopscale.com) HTTP=$code_existing"

  # Strip the cURL --write-out suffix to compare just the response body.
  local body_new_clean body_existing_clean
  body_new_clean="$(printf '%s' "$body_new" | sed 's/--HTTP=.*--//' | tr -d '\n')"
  body_existing_clean="$(printf '%s' "$body_existing" | sed 's/--HTTP=.*--//' | tr -d '\n')"

  if [ "$code_new" != "$code_existing" ]; then
    log "FAIL: status diverges new=$code_new vs existing=$code_existing — enumeration leak"
    emit "FAIL" "scenario-b-canonical-status-diverges-new-${code_new}-existing-${code_existing}"
    return 1
  fi
  if [ "$body_new_clean" != "$body_existing_clean" ]; then
    log "FAIL: bodies diverge — enumeration leak via response body"
    log "  NEW body: $(printf '%s' "$body_new_clean" | head -c 120)"
    log "  EXISTING body: $(printf '%s' "$body_existing_clean" | head -c 120)"
    emit "FAIL" "scenario-b-canonical-bodies-diverge"
    return 1
  fi
  log "PASS: scenario B — canonical /self-service/registration browser flow returns byte-identical universal response for new + existing email"
  return 0
}

rc=0
case "$SCENARIO" in
  A|a) rc=0 ; scenario_a || rc=$? ;;
  B|b) rc=0 ; scenario_b || rc=$? ;;
  all|*)
    rc=0 ; scenario_a || rc=$?
    if [ "$rc" -eq 0 ]; then
      scenario_b || rc=$?
    fi
    ;;
esac

if [ "$rc" -eq 0 ]; then
  emit "PASS" "redteam-canonical-paths-closed"
  exit 0
fi
exit "$rc"
