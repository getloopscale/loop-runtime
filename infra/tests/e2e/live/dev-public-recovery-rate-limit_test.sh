#!/usr/bin/env bash
# Red Team loop-console#89 closure LIVE test — proves Caddy edge rate_limit
# engages on BOTH Kratos recovery zones (T1.5 of plan
# kratos-self-service-rate-limit-systemic v1.1).
#
# Symptom per #89 status report 2026-06-26: 20 recovery flow-creates + 19
# code-submits all returned 200, ZERO 429. This enables (a) email-bomb
# attack via unlimited flow-creates and (b) brute-force recovery code
# guess via unlimited submits.
#
# This LIVE test asserts BOTH recovery buckets engage:
#   Scenario A (recovery_flow_create_throttle, 5/5min, EC-5 canonical):
#     6 sequential GETs to /self-service/recovery/browser — 6th must be 429.
#   Scenario B (recovery_throttle, 3/1min, EC-5 canonical):
#     GET /self-service/recovery/api → flow_id;
#     4 POSTs to /self-service/recovery/methods/code?flow=<id> — 4th must
#     be 429 with Retry-After header.
#
# EC-12 note: this test is safe to run in parallel with
# dev-public-login-rate-limit_test.sh — distinct bucket zones.
#
# Exit codes:
#   0  BOTH recovery buckets engage (6th GET 429 + 4th POST 429)
#   1  bucket(s) fail to engage (regression — closure incomplete)
#   2  BLOCKED (dev-public unreachable, JWT seed missing, flow-init failure)
#
# Usage:
#   bash infra/tests/e2e/live/dev-public-recovery-rate-limit_test.sh \
#     [--skip-live] [--json] [--scenario=A|B] [--help]

set -euo pipefail

TARGET_HOST="${TARGET_HOST:-https://auth-dev.getloopscale.com}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO="all"

# EC-12 canonical: synthesize a unique cf-connecting-ip per run so the
# Caddy rate_limit bucket key is isolated from prior test invocations
# (without this, all curl calls share an empty-string bucket key and prior
# runs persistently consume the 5/5min recovery_flow_create_throttle bucket).
# Matches Cloudflare prod behavior where each client gets its own bucket.
SYNTH_IP_A="${SYNTH_IP_A:-198.51.100.$(( ( RANDOM % 200 ) + 1 ))}"
SYNTH_IP_B="${SYNTH_IP_B:-203.0.113.$(( ( RANDOM % 200 ) + 1 ))}"

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

log()  { if [ "$JSON_OUTPUT" = "0" ]; then printf '[89-recovery-rate] %s\n' "$*" >&2; fi ; }
emit() {
  local verdict="$1" reason="${2:-}"
  if [ "$JSON_OUTPUT" = "1" ]; then
    printf '{"slug":"dev-public-recovery-rate-limit","verdict":"%s","scenario":"%s","reason":"%s"}\n' \
      "$verdict" "$SCENARIO" "$reason"
  else
    printf '[89-recovery-rate] verdict=%s scenario=%s reason=%s\n' "$verdict" "$SCENARIO" "$reason"
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

hermetic_check() {
  log "Hermetic mode (--skip-live) — structural shape only."
  emit "SKIPPED" "hermetic-skip-live"
  exit 0
}

if [ "$SKIP_LIVE" = "1" ]; then
  hermetic_check
fi

preflight_seed

scenario_a() {
  log "Scenario A — recovery_flow_create_throttle (5/5min) — 6 sequential GETs to /recovery/browser"
  local code last_code=""
  for i in $(seq 1 6); do
    code="$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' \
            -H "cf-connecting-ip: $SYNTH_IP_A" \
            "$TARGET_HOST/self-service/recovery/browser" 2>&1 || true)"
    log "  request $i → HTTP $code"
    last_code="$code"
  done
  if [ "$last_code" != "429" ]; then
    log "FAIL: 6th /recovery/browser returned $last_code (expected 429 — recovery_flow_create_throttle 5/5min)"
    emit "FAIL" "flow-create-bucket-did-not-engage-last-code-${last_code}"
    return 1
  fi
  log "PASS: scenario A — recovery_flow_create_throttle engaged on 6th request"
  return 0
}

scenario_b() {
  log "Scenario B — recovery_throttle (3/1min) — flow-init + 4 POSTs to /recovery/methods/code"

  local flow_json flow_id
  flow_json="$(curl -sS --max-time 10 \
                "$TARGET_HOST/self-service/recovery/api" \
                -H "cf-connecting-ip: $SYNTH_IP_B" \
                -H 'Accept: application/json' 2>&1 || true)"
  flow_id="$(printf '%s' "$flow_json" | jq -r '.id // empty' 2>/dev/null || true)"
  if [ -z "$flow_id" ] || [ "$flow_id" = "null" ]; then
    log "BLOCKED: flow-init returned no .id"
    emit "BLOCKED" "recovery-flow-init-failed"
    return 2
  fi
  log "flow_id=$flow_id"

  local code last_code="" retry_after_seen=0 resp retry
  for i in $(seq 1 4); do
    resp="$(curl -sS --max-time 5 -o /dev/null \
            -w '%{http_code}|%{header_json}' \
            -X POST \
            "$TARGET_HOST/self-service/recovery/methods/code?flow=$flow_id" \
            -H "cf-connecting-ip: $SYNTH_IP_B" \
            -H 'Content-Type: application/json' \
            -H 'Accept: application/json' \
            -d '{"email":"probe-'"$i"'@example.invalid","method":"code"}' \
            2>&1 || true)"
    code="${resp%%|*}"
    log "  request $i → HTTP $code"
    last_code="$code"
    if [ "$code" = "429" ]; then
      retry="$(printf '%s' "${resp#*|}" | jq -r '."retry-after"[0] // empty' 2>/dev/null || true)"
      if [ -n "$retry" ]; then
        retry_after_seen=1
      fi
    fi
  done
  if [ "$last_code" != "429" ]; then
    log "FAIL: 4th /recovery/methods/code returned $last_code (expected 429 — recovery_throttle 3/1min)"
    emit "FAIL" "submit-bucket-did-not-engage-last-code-${last_code}"
    return 1
  fi
  if [ "$retry_after_seen" -eq 0 ]; then
    log "FAIL: 429 returned but no Retry-After header observed"
    emit "FAIL" "retry-after-header-missing"
    return 1
  fi
  log "PASS: scenario B — recovery_throttle engaged with Retry-After"
  return 0
}

case "$SCENARIO" in
  A|a) scenario_a ; rc=$? ;;
  B|b) scenario_b ; rc=$? ;;
  all|*)
    scenario_a ; rc_a=$?
    scenario_b ; rc_b=$?
    rc=0
    [ "$rc_a" -ne 0 ] && rc="$rc_a"
    [ "$rc_b" -ne 0 ] && rc="$rc_b"
    ;;
esac

if [ "$rc" -eq 0 ]; then
  emit "PASS" "both-recovery-buckets-engaged"
  exit 0
fi
exit "$rc"
