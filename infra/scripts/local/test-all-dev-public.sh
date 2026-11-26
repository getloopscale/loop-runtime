#!/usr/bin/env bash
# Aggregator: runs all 9 dev-public tests (5 hermetic + 4 LIVE) in sequence.
# T8.1+ of plan 2026-06-04-dev-public-domain-canonicalization.
#
# Use as:
#   - Pre-merge gate (run after any dev-public script edit)
#   - Daily health check before working with dev-public env
#   - CI integration point (single command, single exit code)
#
# Flags:
#   --hermetic-only      skip LIVE tests (CI mode, ~3s)
#   --live-only          skip hermetic tests (operational gate, ~30s)
#   --bail-on-fail       exit on first failing suite
#   --json               machine-readable final verdict

set -euo pipefail

HERMETIC_ONLY=0
LIVE_ONLY=0
BAIL=0
EMIT_JSON=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --hermetic-only) HERMETIC_ONLY=1; shift ;;
    --live-only) LIVE_ONLY=1; shift ;;
    --bail-on-fail) BAIL=1; shift ;;
    --json) EMIT_JSON=1; shift ;;
    -h|--help) sed -n '2,/^set -euo/p' "$0"; exit 0 ;;
    *) printf 'unknown flag: %s\n' "$1" >&2; exit 1 ;;
  esac
done

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"

SUITES_PASSED=0
SUITES_FAILED=0
TOTAL_ASSERTS=0
declare -A SUITE_VERDICT
declare -A SUITE_ASSERTS

log() { printf '[test-all-dev-public] %s\n' "$*" >&2; }
hr()  { printf '─────────────────────────────────────────────\n' >&2; }

run_suite() {
  local name="$1" cmd="$2"
  hr
  log "SUITE: $name"
  hr

  local output
  if output=$(eval "$cmd" 2>&1); then
    SUITE_VERDICT["$name"]="PASS"
    SUITES_PASSED=$((SUITES_PASSED + 1))
    # Extract last "PASS: N" or "PASS=N" count
    local count
    count=$(printf '%s' "$output" | grep -oE 'PASS[=:] ?[0-9]+' | tail -1 | grep -oE '[0-9]+' || echo "?")
    SUITE_ASSERTS["$name"]="$count"
    if [[ "$count" =~ ^[0-9]+$ ]]; then
      TOTAL_ASSERTS=$((TOTAL_ASSERTS + count))
    fi
    log "→ PASS ($count asserts)"
  else
    SUITE_VERDICT["$name"]="FAIL"
    SUITES_FAILED=$((SUITES_FAILED + 1))
    SUITE_ASSERTS["$name"]="?"
    log "→ FAIL — output below:"
    printf '%s\n' "$output" | tail -15 >&2
    if [[ $BAIL -eq 1 ]]; then
      log "BAIL on first failure"
      exit 1
    fi
  fi
}

# ============================================================================
# Suites
# ============================================================================

run_hermetic_suites() {
  run_suite "hermetic: dev-public-env (Go gate)" \
    "/tmp/loopruntime-ops quality dev-public-env"

  run_suite "hermetic: check-live-proof-syntax_test.sh" \
    "bash $REPO_ROOT/infra/tests/check-live-proof-syntax_test.sh"

  run_suite "hermetic: check-trio-dev-public-syntax_test.sh" \
    "bash $REPO_ROOT/infra/tests/check-trio-dev-public-syntax_test.sh"

  run_suite "hermetic: check-dev-public-scripts-syntax_test.sh" \
    "bash $REPO_ROOT/infra/tests/check-dev-public-scripts-syntax_test.sh"
}

run_live_suites() {
  run_suite "LIVE: dev-public-oidc-redirect_test.sh" \
    "bash $REPO_ROOT/infra/tests/e2e/live/dev-public-oidc-redirect_test.sh"

  run_suite "LIVE: dev-public-billing-portal_test.sh" \
    "bash $REPO_ROOT/infra/tests/e2e/live/dev-public-billing-portal_test.sh"

  run_suite "LIVE: live-proof-dev-public.sh" \
    "bash $REPO_ROOT/archive/canonical-tests-v1/scripts/local/live-proof-dev-public.sh"

  run_suite "LIVE: trio-dev-public.sh" \
    "bash $REPO_ROOT/infra/scripts/local/trio-dev-public.sh"
}

# ============================================================================
# Main
# ============================================================================

log "starting (hermetic-only=$HERMETIC_ONLY live-only=$LIVE_ONLY bail=$BAIL)"

if [[ $LIVE_ONLY -eq 0 ]]; then
  run_hermetic_suites
fi

if [[ $HERMETIC_ONLY -eq 0 ]]; then
  run_live_suites
fi

# Summary
hr
log "AGGREGATE VERDICT"
hr
for name in "${!SUITE_VERDICT[@]}"; do
  printf '  %-58s %s (%s asserts)\n' \
    "$name" "${SUITE_VERDICT[$name]}" "${SUITE_ASSERTS[$name]}" >&2
done
hr
log "Suites PASS=$SUITES_PASSED FAIL=$SUITES_FAILED"
log "Total assertions PASS=$TOTAL_ASSERTS"

if [[ $EMIT_JSON -eq 1 ]]; then
  python3 -c "
import json
print(json.dumps({
  'suites_passed': $SUITES_PASSED,
  'suites_failed': $SUITES_FAILED,
  'total_assertions_passed': $TOTAL_ASSERTS,
  'verdict': 'GREEN' if $SUITES_FAILED == 0 else 'RED',
}, indent=2))"
fi

[[ "$SUITES_FAILED" -eq 0 ]] || exit 1
