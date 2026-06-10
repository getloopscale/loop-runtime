#!/usr/bin/env bash
# T4.2 hermetic syntax + structure test for
# infra/tests/e2e/dev-public-device-flow_test.sh.
#
# Validates structural invariants WITHOUT executing the LIVE test:
#   - bash -n syntax sanity
#   - 3 cenário functions defined (A authorize, B token-pending, C preflight)
#   - cleanup trap registered (EXIT INT TERM)
#   - pre-flight HEALTHCHECK present
#   - 3 flags wired (--json, --skip-live, --scenario)
#   - allowlist entry present
#
# Hermetic: zero network, zero kubectl. Pure text inspection.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TARGET="$REPO_ROOT/infra/tests/e2e/dev-public-device-flow_test.sh"
ALLOWLIST="$REPO_ROOT/.claude/rules/shell-scripts-allowlist.md"

pass=0
fail=0

log() { printf '[device-flow-syntax] %s\n' "$*" >&2; }

assert_grep() {
  local desc="$1" pattern="$2" file="${3:-$TARGET}"
  if grep -qE "$pattern" "$file"; then
    log "PASS [$desc]"
    pass=$((pass + 1))
  else
    log "FAIL [$desc]: pattern not matched: $pattern"
    fail=$((fail + 1))
  fi
}

if [[ ! -f "$TARGET" ]]; then
  log "FAIL: target script not found: $TARGET"
  exit 1
fi

# (1) bash -n syntax check
if bash -n "$TARGET" 2>/dev/null; then
  log "PASS [bash -n syntax]"
  pass=$((pass + 1))
else
  log "FAIL [bash -n syntax]: parse error in $TARGET"
  fail=$((fail + 1))
fi

# (2) 3 scenario functions defined
assert_grep "scenario_a_authorize defined" '^scenario_a_authorize\(\)'
assert_grep "scenario_b_token_pending defined" '^scenario_b_token_pending\(\)'
assert_grep "scenario_c_preflight defined" '^scenario_c_preflight\(\)'

# (3) Cleanup trap registered with EXIT INT TERM
assert_grep "trap EXIT INT TERM" 'trap[[:space:]]+cleanup_state[[:space:]]+EXIT[[:space:]]+INT[[:space:]]+TERM'

# (4) Pre-flight HEALTHCHECK references cloud + auth endpoints
assert_grep "pre-flight checks cloud /healthz" 'CLOUD_PUBLIC.*/healthz'
assert_grep "pre-flight checks auth /health/ready" 'AUTH_PUBLIC.*/health/ready'
assert_grep "pre-flight checks dashboard /device-success" 'DASHBOARD.*/device-success'

# (5) Three flags wired
assert_grep "--json flag" '\-\-json'
assert_grep "--skip-live flag" '\-\-skip-live'
assert_grep "--scenario flag" '\-\-scenario'

# (6) EC-10 verification_uri domain assertion present
assert_grep "EC-10 verification_uri domain regex" 'auth-dev\\\.loopzero\\\.dev/oauth2/device/verify'

# (7) Cenário A asserts JSON shape (device_code, user_code, verification_uri,
#     expires_in, interval)
assert_grep "A asserts device_code" 'device_code missing'
assert_grep "A asserts user_code length" 'user_code length'
assert_grep "A asserts expires_in ≥ 60" 'expires_in.*60'
assert_grep "A asserts interval ≥ 1" 'interval.*1'

# (8) Cenário B asserts {"status":"pending"} contract
assert_grep "B asserts status=pending" 'status.*pending'

# (9) JSON output mode
assert_grep "JSON aggregate output" 'verdict.*pass.*fail.*scenarios'

# (10) Allowlist entry present
assert_grep "allowlist entry present" \
  'infra/tests/e2e/dev-public-device-flow_test.sh' \
  "$ALLOWLIST"

log ""
log "════════════════════════════════════════════════════════"
log "  device-flow-syntax aggregate: $pass PASS / $fail FAIL"
log "════════════════════════════════════════════════════════"

[[ "$fail" -eq 0 ]]
