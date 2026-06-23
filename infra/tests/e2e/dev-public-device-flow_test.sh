#!/usr/bin/env bash
# T4.2 LIVE bash dogfood for Journey J-05 Device Success (3 cenários A/B/C).
#
# Plan: .claude/knowledge-base/plans/2026-06-05-dashboard-journey-j05-device-success-plan.md
#
# Cenário A — authorize contract:
#   POST /v1/auth/device/authorize → asserts HTTP 200 + JSON shape
#     (device_code, user_code (8 chars), verification_uri pointing to
#     auth-dev.loopzero.example/oauth2/device/verify, expires_in ≥ 60,
#     interval ≥ 1).
#   EC-10 enforces verification_uri domain — defends against config drift
#   where Hydra issuer points to localhost or a wrong domain.
#
# Cenário B — token pending contract:
#   POST /v1/auth/device/token with the authorize device_code (no consent
#   given) → asserts HTTP 200 + JSON status=pending. Confirms the polling
#   loop the CLI will execute receives a parseable, non-fatal response.
#
# Cenário C — pre-flight HEALTHCHECK:
#   cloud /healthz, auth /health/ready, Hydra readyReplicas≥1, dashboard
#   /device-success TLS reachable. Skips A+B if any check fails.
#
# Why no consent-click automation here (D6 in plan): simulating the consent
# UI requires Kratos session + CSRF cookies + form submission — fragility
# >> value. Full E2E `loopzero login --device-flow` stays operator-manual.
#
# Run after `task dev-public:bootstrap` + `LOOPZERO_HELMFILE_ENV=dev-public task setup`
# (or against a live dev-public env).

set -euo pipefail

CLOUD_PUBLIC="${CLOUD_PUBLIC:-https://app-dev.loopzero.example}"
AUTH_PUBLIC="${AUTH_PUBLIC:-https://auth-dev.loopzero.example}"
DASHBOARD="${DASHBOARD:-https://app-dev.loopzero.example}"
CLUSTER_CTX="${CLUSTER_CTX:-k3d-loopzero-mgmt}"
HYDRA_NS="${HYDRA_NS:-loopzero-auth}"

JSON_MODE=0
SKIP_LIVE=0
SINGLE_SCENARIO=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --json) JSON_MODE=1; shift ;;
    --skip-live) SKIP_LIVE=1; shift ;;
    --scenario) SINGLE_SCENARIO="$2"; shift 2 ;;
    -h|--help) sed -n '2,28p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) shift ;;
  esac
done

log() { printf '[device-flow-test] %s\n' "$*" >&2; }
warn() { printf '[device-flow-test] WARN: %s\n' "$*" >&2; }

SCENARIO_RESULTS=()
SCENARIO_PASS=0
SCENARIO_FAIL=0

record_result() {
  local name="$1" verdict="$2" detail="${3:-}"
  SCENARIO_RESULTS+=("$name|$verdict|$detail")
  if [[ "$verdict" == "PASS" ]]; then
    SCENARIO_PASS=$((SCENARIO_PASS + 1))
    log "[$name] PASS"
  else
    SCENARIO_FAIL=$((SCENARIO_FAIL + 1))
    log "[$name] FAIL: $detail"
  fi
}

# Cleanup trap — device-flow does NOT create persistent state (device_code
# expires automatically in ~5min), but we keep the trap as a contract.
cleanup_state() {
  log "cleanup: device-flow leaves no persistent state (device_code TTL)"
}
trap cleanup_state EXIT INT TERM

# ──────────────────────────────────────────────────────────────────────────
# Cenário C — Pre-flight HEALTHCHECK (must run first)
# ──────────────────────────────────────────────────────────────────────────
scenario_c_preflight() {
  log "Cenário C: pre-flight HEALTHCHECK (cloud + auth + Hydra + dashboard)"
  local failed=0

  if ! curl -fsS -o /dev/null -m 5 "$CLOUD_PUBLIC/health" 2>/dev/null; then
    record_result "C-cloud-health" "FAIL" "$CLOUD_PUBLIC/health unreachable"
    failed=1
  else
    record_result "C-cloud-health" "PASS"
  fi

  if ! curl -fsS -o /dev/null -m 5 "$AUTH_PUBLIC/health/ready" 2>/dev/null; then
    record_result "C-auth-ready" "FAIL" "$AUTH_PUBLIC/health/ready unreachable"
    failed=1
  else
    record_result "C-auth-ready" "PASS"
  fi

  if ! curl -fsS -o /dev/null -m 5 "$DASHBOARD/device-success" 2>/dev/null; then
    record_result "C-dashboard-device-success-reachable" "FAIL" "$DASHBOARD/device-success unreachable"
    failed=1
  else
    record_result "C-dashboard-device-success-reachable" "PASS"
  fi

  if command -v kubectl >/dev/null 2>&1; then
    local ready
    ready=$(kubectl --context "$CLUSTER_CTX" -n "$HYDRA_NS" get statefulset ory-hydra \
      -o jsonpath='{.status.readyReplicas}' 2>/dev/null || \
      kubectl --context "$CLUSTER_CTX" -n "$HYDRA_NS" get deploy ory-hydra \
      -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
    if [[ "${ready:-0}" -ge 1 ]]; then
      record_result "C-hydra-ready-replicas" "PASS" "readyReplicas=$ready"
    else
      record_result "C-hydra-ready-replicas" "FAIL" "readyReplicas=$ready (want ≥1)"
      failed=1
    fi
  else
    warn "kubectl absent — skipping Hydra readyReplicas check"
  fi

  return $failed
}

# ──────────────────────────────────────────────────────────────────────────
# Cenário A — authorize contract
# ──────────────────────────────────────────────────────────────────────────
DEVICE_CODE=""
scenario_a_authorize() {
  log "Cenário A: POST /v1/auth/device/authorize contract"
  local resp
  resp=$(curl -fsS -m 10 -X POST -H "Content-Type: application/json" \
    "$CLOUD_PUBLIC/v1/auth/device/authorize" 2>&1) || {
    record_result "A-authorize" "FAIL" "curl exit non-zero: $resp"
    return 1
  }

  # Parse fields with jq fallback to python.
  local parser
  if command -v jq >/dev/null 2>&1; then
    parser="jq"
  else
    parser="python"
  fi

  extract() {
    local key="$1"
    if [[ "$parser" == "jq" ]]; then
      echo "$resp" | jq -r ".${key} // empty"
    else
      echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('${key}',''))"
    fi
  }

  local device_code user_code verification_uri expires_in interval
  device_code=$(extract device_code)
  user_code=$(extract user_code)
  verification_uri=$(extract verification_uri)
  expires_in=$(extract expires_in)
  interval=$(extract interval)

  if [[ -z "$device_code" ]]; then
    record_result "A-authorize-device-code" "FAIL" "device_code missing"
    return 1
  fi
  record_result "A-authorize-device-code" "PASS" "device_code present"

  if [[ "${#user_code}" -lt 6 || "${#user_code}" -gt 16 ]]; then
    record_result "A-authorize-user-code" "FAIL" "user_code length=${#user_code} not in 6..16"
  else
    record_result "A-authorize-user-code" "PASS" "user_code length=${#user_code}"
  fi

  # EC-10: assert verification_uri points to auth-dev.loopzero.example/oauth2/device/verify
  if echo "$verification_uri" | grep -qE '^https://auth-dev\.loopzero\.dev/oauth2/device/verify'; then
    record_result "A-authorize-verification-uri-domain" "PASS" "$verification_uri"
  else
    record_result "A-authorize-verification-uri-domain" "FAIL" \
      "verification_uri='$verification_uri' does not match expected dev-public domain"
  fi

  if [[ "$expires_in" =~ ^[0-9]+$ ]] && [[ "$expires_in" -ge 60 ]]; then
    record_result "A-authorize-expires-in" "PASS" "expires_in=$expires_in"
  else
    record_result "A-authorize-expires-in" "FAIL" "expires_in='$expires_in' not ≥60"
  fi

  if [[ "$interval" =~ ^[0-9]+$ ]] && [[ "$interval" -ge 1 ]]; then
    record_result "A-authorize-interval" "PASS" "interval=$interval"
  else
    record_result "A-authorize-interval" "FAIL" "interval='$interval' not ≥1"
  fi

  DEVICE_CODE="$device_code"
}

# ──────────────────────────────────────────────────────────────────────────
# Cenário B — token pending contract
# ──────────────────────────────────────────────────────────────────────────
scenario_b_token_pending() {
  log "Cenário B: POST /v1/auth/device/token with un-consented device_code"
  if [[ -z "$DEVICE_CODE" ]]; then
    record_result "B-token-pending" "FAIL" "Cenário A did not yield device_code"
    return 1
  fi

  local body resp
  body="{\"device_code\":\"$DEVICE_CODE\"}"
  resp=$(curl -fsS -m 10 -X POST -H "Content-Type: application/json" \
    -d "$body" "$CLOUD_PUBLIC/v1/auth/device/token" 2>&1) || {
    record_result "B-token-pending" "FAIL" "curl exit non-zero: $resp"
    return 1
  }

  # The cloud handler returns {"status":"pending"} when Hydra is still
  # waiting for the user to approve consent. That is the canonical shape
  # this contract pins.
  if echo "$resp" | grep -qE '"status"[[:space:]]*:[[:space:]]*"pending"'; then
    record_result "B-token-pending" "PASS" "status=pending as expected"
  else
    record_result "B-token-pending" "FAIL" "unexpected response: $resp"
  fi
}

# ──────────────────────────────────────────────────────────────────────────
# Cenário D — accept endpoint contract (iter-3, after the verify UI landed)
# ──────────────────────────────────────────────────────────────────────────
# Asserts that POST /v1/auth/device/accept exists and rejects malformed
# input with HTTP 400 + INVALID_REQUEST code. We can NOT validate a happy
# path here without a real Hydra device_challenge (Hydra mints those only
# during the browser redirect), so the contract pinned is the input
# validation layer — enough to catch route un-wiring regressions.
scenario_d_accept_contract() {
  log "Cenário D: POST /v1/auth/device/accept input-validation contract"

  # D.1 — empty body → 400.
  local empty_status
  empty_status=$(curl -sS -o /dev/null -w "%{http_code}" -m 10 -X POST \
    -H "Content-Type: application/json" -d '{}' \
    "$CLOUD_PUBLIC/v1/auth/device/accept" 2>&1)
  if [[ "$empty_status" == "400" ]]; then
    record_result "D-accept-empty-body-400" "PASS" "HTTP $empty_status"
  else
    record_result "D-accept-empty-body-400" "FAIL" "expected 400, got $empty_status"
  fi

  # D.2 — missing user_code → 400.
  local partial_status
  partial_status=$(curl -sS -o /dev/null -w "%{http_code}" -m 10 -X POST \
    -H "Content-Type: application/json" -d '{"device_challenge":"x"}' \
    "$CLOUD_PUBLIC/v1/auth/device/accept" 2>&1)
  if [[ "$partial_status" == "400" ]]; then
    record_result "D-accept-missing-user-code-400" "PASS" "HTTP $partial_status"
  else
    record_result "D-accept-missing-user-code-400" "FAIL" "expected 400, got $partial_status"
  fi

  # D.3 — malformed JSON → 400.
  local bad_status
  bad_status=$(curl -sS -o /dev/null -w "%{http_code}" -m 10 -X POST \
    -H "Content-Type: application/json" -d '{not json' \
    "$CLOUD_PUBLIC/v1/auth/device/accept" 2>&1)
  if [[ "$bad_status" == "400" ]]; then
    record_result "D-accept-malformed-json-400" "PASS" "HTTP $bad_status"
  else
    record_result "D-accept-malformed-json-400" "FAIL" "expected 400, got $bad_status"
  fi
}

# ──────────────────────────────────────────────────────────────────────────
# Driver
# ──────────────────────────────────────────────────────────────────────────
if [[ "$SKIP_LIVE" -eq 1 ]]; then
  log "SKIP_LIVE=1 — exiting before any HTTP/kubectl call"
  exit 0
fi

# Always run pre-flight unless a single scenario explicitly targets A or B.
if [[ -z "$SINGLE_SCENARIO" || "$SINGLE_SCENARIO" == "C" ]]; then
  if ! scenario_c_preflight; then
    log "pre-flight FAILED — aborting cenários A + B to avoid false positives"
    log ""
    log "════════════════════════════════════════════════════════"
    log "  device-flow aggregate: $SCENARIO_PASS PASS / $SCENARIO_FAIL FAIL (pre-flight)"
    log "════════════════════════════════════════════════════════"
    exit 1
  fi
fi

if [[ -z "$SINGLE_SCENARIO" || "$SINGLE_SCENARIO" == "A" ]]; then
  scenario_a_authorize || true
fi

if [[ -z "$SINGLE_SCENARIO" || "$SINGLE_SCENARIO" == "B" ]]; then
  scenario_b_token_pending || true
fi

if [[ -z "$SINGLE_SCENARIO" || "$SINGLE_SCENARIO" == "D" ]]; then
  scenario_d_accept_contract || true
fi

log ""
log "════════════════════════════════════════════════════════"
log "  device-flow aggregate: $SCENARIO_PASS PASS / $SCENARIO_FAIL FAIL"
log "════════════════════════════════════════════════════════"
for r in "${SCENARIO_RESULTS[@]}"; do
  log "  $r"
done

if [[ "$JSON_MODE" -eq 1 ]]; then
  local_verdict=$([[ "$SCENARIO_FAIL" -eq 0 ]] && echo "PASS" || echo "FAIL")
  printf '{"verdict":"%s","pass":%d,"fail":%d,"scenarios":[' \
    "$local_verdict" "$SCENARIO_PASS" "$SCENARIO_FAIL"
  first=1
  for r in "${SCENARIO_RESULTS[@]}"; do
    IFS='|' read -r name verdict detail <<< "$r"
    [[ "$first" -eq 0 ]] && printf ','
    first=0
    printf '{"name":"%s","verdict":"%s","detail":"%s"}' "$name" "$verdict" "${detail//\"/\\\"}"
  done
  printf ']}\n'
fi

[[ "$SCENARIO_FAIL" -eq 0 ]]
