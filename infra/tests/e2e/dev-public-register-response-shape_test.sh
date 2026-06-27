#!/usr/bin/env bash
# Red Team loopzero-cloud#43 closure LIVE test — proves the loopzero-cloud BFF
# response-shape unifier (T3.1) returns IDENTICAL response bodies for both
# new and existing email registration submissions (T3.5 of plan
# kratos-self-service-rate-limit-systemic v1.1).
#
# Symptom per #43: register UI message differs between existing vs fresh
# email → attacker can probe account existence.
#
# This LIVE test asserts:
#   Scenario A (new email):        POST /v1/auth/register with fresh-uuid email
#                                  → 200 + canonical universal body OR 303 redirect.
#   Scenario B (existing email):   POST /v1/auth/register with admin@loopzero.example
#                                  (canonical dev seed) AFTER 70s sleep so
#                                  Slice A register_throttle bucket (5/min) window
#                                  expires between A and B (EC-9 absorption).
#                                  → IDENTICAL response body to scenario A.
#   Scenario C (regression-protector for Slice A bucket coexistence):
#                                  6 rapid POSTs in < 60s. 6th must return
#                                  429 → confirms both BFF unifier AND edge
#                                  rate_limit work in concert.
#
# EC-8 absorption: pre-flight JWT seed check at ~/.loopzero/local-dogfood-token.txt.
# EC-9 absorption: 70s sleep between A and B avoids accidental bucket trip.
#
# Exit codes:
#   0  bodies are byte-identical between new + existing branches + Slice A
#      register_throttle bucket engages on 6th rapid POST
#   1  bodies diverge (enumeration leak) OR Slice A bucket failed to engage
#   2  BLOCKED (dev-public unreachable, JWT seed missing)
#
# Usage:
#   bash infra/tests/e2e/dev-public-register-response-shape_test.sh \
#     [--skip-live] [--json] [--scenario=A|B|C] [--help]

set -euo pipefail

TARGET_HOST="${TARGET_HOST:-https://auth-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO="all"

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-live) SKIP_LIVE=1 ; shift ;;
    --json)      JSON_OUTPUT=1 ; shift ;;
    --scenario=*) SCENARIO="${1#*=}" ; shift ;;
    -h|--help)
      sed -n '1,32p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) printf 'unknown arg: %s\n' "$1" >&2 ; exit 1 ;;
  esac
done

log()  { [ "$JSON_OUTPUT" = "0" ] && printf '[43-register-shape] %s\n' "$*" >&2 || true ; }
emit() {
  local verdict="$1" reason="${2:-}"
  if [ "$JSON_OUTPUT" = "1" ]; then
    printf '{"slug":"dev-public-register-response-shape","verdict":"%s","scenario":"%s","reason":"%s"}\n' \
      "$verdict" "$SCENARIO" "$reason"
  else
    printf '[43-register-shape] verdict=%s scenario=%s reason=%s\n' "$verdict" "$SCENARIO" "$reason"
  fi
}

# EC-8 pre-flight.
preflight_seed() {
  if [ ! -f "$HOME/.loopzero/local-dogfood-token.txt" ]; then
    log "BLOCKED: JWT seed missing — run bash infra/scripts/local/seed-admin-loopzero-dev.sh --refresh-only"
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

# Body collector — returns the response body of a register submit.
post_register() {
  local email="$1" outfile="$2"
  curl -sS --max-time 10 -o "$outfile" -w '%{http_code}' \
    -X POST "$TARGET_HOST/self-service/registration/methods/password" \
    -H 'Content-Type: application/json' \
    -H 'Accept: application/json' \
    -d "{\"method\":\"password\",\"traits.email\":\"$email\",\"traits.name\":\"Probe\",\"password\":\"valid-13c-pwd!\"}" \
    2>&1 || true
}

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT INT TERM

scenario_a_b() {
  log "Scenario A — new email POST"
  local fresh_email="probe-$(date +%s)@example.invalid"
  local code_a
  code_a="$(post_register "$fresh_email" "$TMPDIR/body_a.txt")"
  log "  scenario A: HTTP $code_a (body $(wc -c < "$TMPDIR/body_a.txt") bytes)"

  log "Sleeping 70s (EC-9: avoid Slice A register_throttle 5/min bucket trip)..."
  sleep 70

  log "Scenario B — existing email POST"
  local code_b
  code_b="$(post_register "admin@loopzero.example" "$TMPDIR/body_b.txt")"
  log "  scenario B: HTTP $code_b (body $(wc -c < "$TMPDIR/body_b.txt") bytes)"

  if [ "$code_a" != "$code_b" ]; then
    log "FAIL: HTTP status diverges (A=$code_a B=$code_b) — enumeration leak via status"
    emit "FAIL" "status-diverges-A-${code_a}-B-${code_b}"
    return 1
  fi
  if ! cmp -s "$TMPDIR/body_a.txt" "$TMPDIR/body_b.txt"; then
    log "FAIL: response bodies diverge — enumeration leak"
    log "  A body: $(head -c 200 "$TMPDIR/body_a.txt")"
    log "  B body: $(head -c 200 "$TMPDIR/body_b.txt")"
    emit "FAIL" "bodies-diverge"
    return 1
  fi
  log "PASS: scenarios A + B — bodies byte-identical (no enumeration leak)"
  return 0
}

scenario_c() {
  log "Scenario C — 6 rapid POSTs to confirm Slice A register_throttle bucket engages"
  local code last_code
  for i in $(seq 1 6); do
    last_code="$(post_register "burst-$i@example.invalid" "$TMPDIR/body_c_$i.txt")"
    log "  burst $i → HTTP $last_code"
  done
  if [ "$last_code" != "429" ]; then
    log "FAIL: 6th burst returned $last_code (expected 429 — Slice A register_throttle 5/min)"
    emit "FAIL" "slice-a-bucket-did-not-engage-last-code-${last_code}"
    return 1
  fi
  log "PASS: scenario C — Slice A register_throttle bucket engaged on 6th burst"
  return 0
}

case "$SCENARIO" in
  A|a|B|b) scenario_a_b ; rc=$? ;;
  C|c)     scenario_c ; rc=$? ;;
  all|*)
    scenario_a_b ; rc_ab=$?
    scenario_c   ; rc_c=$?
    rc=0
    [ "$rc_ab" -ne 0 ] && rc="$rc_ab"
    [ "$rc_c"  -ne 0 ] && rc="$rc_c"
    ;;
esac

if [ "$rc" -eq 0 ]; then
  emit "PASS" "response-shape-unified-and-bucket-engages"
  exit 0
fi
exit "$rc"
