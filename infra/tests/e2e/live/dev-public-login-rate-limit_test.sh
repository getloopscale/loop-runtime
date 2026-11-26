#!/usr/bin/env bash
# Red Team loop-console#68 closure LIVE test — proves Caddy edge rate_limit
# engages on Kratos self-service login submission flow (T1.4 of plan
# kratos-self-service-rate-limit-systemic v1.1).
#
# Symptom per #68 status report 2026-06-26: 20 login flow-creates + 19
# password-submits all returned 200, ZERO 429, no Retry-After header. This
# leaves /login as a credential-stuffing facilitator.
#
# This LIVE test reproduces the symptom via the canonical 2-step pattern
# (EC-5 absorption — the bucket matches `/self-service/login/methods/*`
# submission endpoint, NOT the `/self-service/login/api` flow-init endpoint;
# hitting only the latter does not engage the bucket):
#   1. GET /self-service/login/api → obtain flow_id
#   2. Burst 30 POSTs to /self-service/login/methods/password?flow=<id>
#      with dummy credentials; assert ≥ 5 of the last 20 return 429 with
#      Retry-After header.
#
# EC-12 note: this test is safe to run in parallel with
# dev-public-recovery-rate-limit_test.sh — distinct bucket zones
# (login_throttle vs recovery_throttle + recovery_flow_create_throttle).
#
# Exit codes:
#   0  bucket engages (≥ 5 of last 20 POSTs return 429 + Retry-After header)
#   1  bucket fails to engage (regression — closure incomplete)
#   2  BLOCKED (dev-public unreachable, JWT seed missing, flow-init failure)
#
# Usage:
#   bash infra/tests/e2e/live/dev-public-login-rate-limit_test.sh \
#     [--skip-live] [--json] [--scenario=A|B] [--help]

set -euo pipefail

TARGET_HOST="${TARGET_HOST:-https://auth-dev.getloopscale.com}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO="all"

# EC-12 canonical: synthesize a unique cf-connecting-ip per run so the
# Caddy rate_limit bucket key is isolated from prior test runs (without
# this, all curl calls share an empty-string bucket key and bucket state
# leaks across runs). Matches CF prod behavior (per-client bucket).
SYNTH_IP="${SYNTH_IP:-192.0.2.$(( ( RANDOM % 200 ) + 1 ))}"

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

log()  { if [ "$JSON_OUTPUT" = "0" ]; then printf '[68-login-rate] %s\n' "$*" >&2; fi ; }
emit() {
  local verdict="$1" reason="${2:-}"
  if [ "$JSON_OUTPUT" = "1" ]; then
    printf '{"slug":"dev-public-login-rate-limit","verdict":"%s","scenario":"%s","reason":"%s"}\n' \
      "$verdict" "$SCENARIO" "$reason"
  else
    printf '[68-login-rate] verdict=%s scenario=%s reason=%s\n' "$verdict" "$SCENARIO" "$reason"
  fi
}

# EC-8 absorption: pre-flight JWT seed check.
preflight_seed() {
  if [ ! -f "$HOME/.loop-runtime/local-dogfood-token.txt" ]; then
    log "BLOCKED: JWT seed missing — run bash infra/scripts/local/seed-admin-loop-runtime-dev.sh --refresh-only"
    emit "BLOCKED" "jwt-seed-missing"
    exit 2
  fi
}

# Hermetic structural mode (CI-safe; no cluster contact).
hermetic_check() {
  log "Hermetic mode (--skip-live) — structural shape only."
  # Trivial structural validators: bash -n already passed; the canonical
  # 2-step pattern + cf-connecting-ip + retry-after assertions are
  # source-coded below. No further validation in hermetic mode.
  emit "SKIPPED" "hermetic-skip-live"
  exit 0
}

if [ "$SKIP_LIVE" = "1" ]; then
  hermetic_check
fi

preflight_seed

scenario_a() {
  log "Scenario A — flow-init + burst 30 POSTs to /self-service/login/methods/password"

  # Step 1: GET /self-service/login/api → flow_id
  local flow_json flow_id
  flow_json="$(curl -sS --max-time 10 \
                "$TARGET_HOST/self-service/login/api" \
                -H "cf-connecting-ip: $SYNTH_IP" \
                -H 'Accept: application/json' 2>&1 || true)"
  flow_id="$(printf '%s' "$flow_json" | jq -r '.id // empty' 2>/dev/null || true)"
  if [ -z "$flow_id" ] || [ "$flow_id" = "null" ]; then
    log "BLOCKED: flow-init returned no .id — response: $(printf '%s' "$flow_json" | head -c 200)"
    emit "BLOCKED" "flow-init-failed"
    return 2
  fi
  log "flow_id=$flow_id"

  # Step 2: burst 30 POSTs to the submission endpoint.
  local hits_429=0 retry_after_seen=0
  local resp code retry
  for i in $(seq 1 30); do
    resp="$(curl -sS --max-time 5 -o /dev/null \
            -w '%{http_code}|%{header_json}' \
            -X POST \
            "$TARGET_HOST/self-service/login/methods/password?flow=$flow_id" \
            -H "cf-connecting-ip: $SYNTH_IP" \
            -H 'Content-Type: application/json' \
            -H 'Accept: application/json' \
            -d '{"identifier":"probe-'"$i"'@example.invalid","password":"wrong-pwd-probe","method":"password"}' \
            2>&1 || true)"
    code="${resp%%|*}"
    if [ "$code" = "429" ]; then
      hits_429=$((hits_429 + 1))
      # Check Retry-After in the header_json (curl 7.84+).
      retry="$(printf '%s' "${resp#*|}" | jq -r '."retry-after"[0] // empty' 2>/dev/null || true)"
      if [ -n "$retry" ]; then
        retry_after_seen=1
      fi
    fi
  done
  log "scenario-A summary: hits_429=$hits_429/30 retry_after_header_seen=$retry_after_seen"

  # Assertion: at least 5 of the 30 must be 429 (bucket is 10/min — burst
  # consumes 10 then 20 should hit 429).
  if [ "$hits_429" -lt 5 ]; then
    log "FAIL: only $hits_429/30 requests returned 429 (expected ≥ 5 for 10/min bucket)"
    emit "FAIL" "bucket-did-not-engage-only-${hits_429}-of-30-429"
    return 1
  fi
  if [ "$retry_after_seen" -eq 0 ]; then
    log "FAIL: 429 returned but no Retry-After header observed"
    emit "FAIL" "retry-after-header-missing"
    return 1
  fi
  log "PASS: scenario A — login_throttle bucket engaged with Retry-After"
  return 0
}

scenario_b() {
  # F-tests-1 absorption: NEVER silently return 0 in default mode. The
  # window-expiration regression-protector is a 65s wait + burst; if the
  # operator opts in via `INCLUDE_SLOW_SCENARIOS=1`, run it for real. If
  # not, emit verdict=SKIPPED with explicit reason so the operator + CI
  # cannot mistake "skipped" for "passed" (per Unbreakable Rule 3 honest
  # BLOCKED > false PASS). The case-statement honors this by mapping the
  # SKIPPED exit-code 3 back to verdict=SKIPPED at the top level.
  if [ "${INCLUDE_SLOW_SCENARIOS:-0}" != "1" ]; then
    log "Scenario B — SKIPPED (window-expiration would block 65s; rerun with INCLUDE_SLOW_SCENARIOS=1)"
    emit "SKIPPED" "scenario-b-requires-INCLUDE_SLOW_SCENARIOS=1"
    return 3
  fi

  log "Scenario B — window-expiration regression-protector (real 65s wait)"
  # Burn the bucket first (10 hits in < 60s).
  local flow_json flow_id
  flow_json="$(curl -sS --max-time 10 \
                "$TARGET_HOST/self-service/login/api" \
                -H "cf-connecting-ip: $SYNTH_IP" \
                -H 'Accept: application/json' 2>&1 || true)"
  flow_id="$(printf '%s' "$flow_json" | jq -r '.id // empty' 2>/dev/null || true)"
  if [ -z "$flow_id" ] || [ "$flow_id" = "null" ]; then
    log "BLOCKED: flow-init returned no .id"
    emit "BLOCKED" "scenario-b-flow-init-failed"
    return 2
  fi
  local pre_window_hits_429=0 code
  for i in $(seq 1 15); do
    code="$(curl -sS --max-time 5 -o /dev/null -w '%{http_code}' \
            -X POST "$TARGET_HOST/self-service/login/methods/password?flow=$flow_id" \
            -H "cf-connecting-ip: $SYNTH_IP" \
            -H 'Content-Type: application/json' \
            -H 'Accept: application/json' \
            -d '{"identifier":"warmup'"$i"'@example.invalid","password":"x","method":"password"}' \
            2>&1 || true)"
    [ "$code" = "429" ] && pre_window_hits_429=$((pre_window_hits_429 + 1))
  done
  log "Pre-window: bucket burned, $pre_window_hits_429/15 returned 429"
  log "Waiting 65s for window expiration..."
  sleep 65
  # Post-window single probe MUST return non-429 (bucket reset).
  local post_code
  post_code="$(curl -sS --max-time 5 -o /dev/null -w '%{http_code}' \
            -X POST "$TARGET_HOST/self-service/login/methods/password?flow=$flow_id" \
            -H "cf-connecting-ip: $SYNTH_IP" \
            -H 'Content-Type: application/json' \
            -H 'Accept: application/json' \
            -d '{"identifier":"postwin@example.invalid","password":"x","method":"password"}' \
            2>&1 || true)"
  log "Post-window probe → HTTP $post_code (must NOT be 429)"
  if [ "$post_code" = "429" ]; then
    emit "FAIL" "scenario-b-window-did-not-expire-bucket-still-engaged"
    return 1
  fi
  log "PASS: scenario B — login_throttle window expired correctly"
  return 0
}

# F-tests-1: every `scenario_X` invocation uses `|| rc=$?` so set -e does
# NOT abort on the SKIPPED exit-code 3. The default `rc_x=0` prefix covers
# the success path where the function returned 0 (|| does not fire).
case "$SCENARIO" in
  A|a) rc=0 ; scenario_a || rc=$? ;;
  B|b) rc=0 ; scenario_b || rc=$? ;;
  all|*)
    rc_a=0 ; scenario_a || rc_a=$?
    if [ "$rc_a" -eq 0 ]; then
      rc_b=0 ; scenario_b || rc_b=$?
      if [ "$rc_b" -eq 3 ]; then
        # A passed, B honestly SKIPPED (operator did not opt-in via
        # INCLUDE_SLOW_SCENARIOS=1). emit() already fired inside scenario_b
        # with a JSON SKIPPED line; emit a SECOND aggregate line so the
        # overall verdict is honest (PASS_WITH_SKIPPED_B is the canonical
        # ecosystem verdict per cycle-rule-schema for partial pass).
        emit "PASS_WITH_SKIPPED_B" "scenario-a-pass-scenario-b-requires-INCLUDE_SLOW_SCENARIOS=1"
        exit 0
      fi
      rc=$rc_b
    else
      rc=$rc_a
    fi
    ;;
esac

if [ "$rc" -eq 0 ]; then
  emit "PASS" "login-rate-limit-engaged"
  exit 0
fi
if [ "$rc" -eq 3 ]; then
  # Standalone --scenario=B without INCLUDE_SLOW_SCENARIOS=1 → SKIPPED.
  # emit() already fired inside scenario_b(). Exit 0 so the operator's
  # CI does not flag a skip as a failure.
  exit 0
fi
exit "$rc"
