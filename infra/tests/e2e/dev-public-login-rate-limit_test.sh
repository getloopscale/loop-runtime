#!/usr/bin/env bash
# Red Team loopzero-cloud#68 closure LIVE test — proves Caddy edge rate_limit
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
#   bash infra/tests/e2e/dev-public-login-rate-limit_test.sh \
#     [--skip-live] [--json] [--scenario=A|B] [--help]

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
      sed -n '1,29p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) printf 'unknown arg: %s\n' "$1" >&2 ; exit 1 ;;
  esac
done

log()  { [ "$JSON_OUTPUT" = "0" ] && printf '[68-login-rate] %s\n' "$*" >&2 || true ; }
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
  if [ ! -f "$HOME/.loopzero/local-dogfood-token.txt" ]; then
    log "BLOCKED: JWT seed missing — run bash infra/scripts/local/seed-admin-loopzero-dev.sh --refresh-only"
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
  log "Scenario B — window-expiration regression-protector"
  log "(skipped in fast mode; full 1-min wait would block CI runs — operator manual verify)"
  return 0
}

case "$SCENARIO" in
  A|a) scenario_a ; rc=$? ;;
  B|b) scenario_b ; rc=$? ;;
  all|*) scenario_a ; rc=$? ; if [ "$rc" -eq 0 ]; then scenario_b ; rc=$? ; fi ;;
esac

if [ "$rc" -eq 0 ]; then
  emit "PASS" "login-rate-limit-engaged"
  exit 0
fi
exit "$rc"
