#!/usr/bin/env bash
# T6.1-equivalent aggregate dogfood wrapper for J-25 Live URL Discoverability.
#
# T4.1 of plan 2026-06-12-dashboard-journey-j25-live-url-discoverability v1.2.
#
# Invokes 4 suites in canonical order:
#   1. T0.1 probe hermetic — dev-public-status-url-probe_test.sh --skip-live
#   2. T4.1 LIVE bash hermetic — dev-public-live-url-discoverability_test.sh --skip-live
#   3. Vitest J-25 components — npx vitest run src/components/project/
#   4. T0.1 + T4.1 LIVE — full chain (operator HARD STOP per cleanroom mandate)
#
# Flags:
#   --hermetic-only   skip LIVE (suites 1-3 only)
#   --live-only       skip vitest (suites 1, 2, 4)
#   --json            machine-readable verdict
#   --help            print usage

set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
HERMETIC_ONLY=0
LIVE_ONLY=0
JSON_OUTPUT=0

usage() {
  cat <<USAGE >&2
Usage: $0 [--hermetic-only] [--live-only] [--json] [--help]
Aggregate dogfood wrapper for J-25 Live URL Discoverability.
USAGE
}

while (($#)); do
  case "$1" in
    --hermetic-only) HERMETIC_ONLY=1; shift ;;
    --live-only)     LIVE_ONLY=1; shift ;;
    --json)          JSON_OUTPUT=1; shift ;;
    --help|-h)       usage; exit 0 ;;
    *)               echo "Unknown flag: $1" >&2; usage; exit 2 ;;
  esac
done

PASS=0
FAIL=0
RESULTS=()

log() { [[ $JSON_OUTPUT -eq 0 ]] && printf '[dogfood-live-url] %s\n' "$*" >&2 || true; }

run_suite() {
  local name="$1"; shift
  log ""
  log "=== $name ==="
  if "$@"; then
    RESULTS+=("$name:PASS")
    PASS=$((PASS + 1))
    log "  PASS: $name"
  else
    RESULTS+=("$name:FAIL")
    FAIL=$((FAIL + 1))
    log "  FAIL: $name"
  fi
}

if [[ $LIVE_ONLY -eq 0 ]]; then
  run_suite "T0.1-hermetic" \
    bash "$REPO_ROOT/infra/tests/e2e/dev-public-status-url-probe_test.sh" --skip-live --json
  run_suite "T4.1-hermetic" \
    bash "$REPO_ROOT/infra/tests/e2e/dev-public-live-url-discoverability_test.sh" --skip-live --json
  run_suite "vitest-j25" \
    bash -c "cd $REPO_ROOT/cloud/dashboard && npx vitest run src/components/project/ 2>&1 | tail -3"
fi

if [[ $HERMETIC_ONLY -eq 0 ]]; then
  run_suite "T0.1-LIVE" \
    bash "$REPO_ROOT/infra/tests/e2e/dev-public-status-url-probe_test.sh"
  run_suite "T4.1-LIVE" \
    bash "$REPO_ROOT/infra/tests/e2e/dev-public-live-url-discoverability_test.sh"
fi

if [[ $JSON_OUTPUT -eq 1 ]]; then
  R=$(printf '"%s",' "${RESULTS[@]}" | sed 's/,$//')
  printf '{"pass":%d,"fail":%d,"verdict":"%s","results":[%s]}\n' \
    "$PASS" "$FAIL" "$([[ $FAIL -eq 0 ]] && echo PASS || echo FAIL)" "$R"
else
  log ""
  log "=== aggregate verdict: PASS=$PASS FAIL=$FAIL ==="
fi

[[ $FAIL -eq 0 ]] && exit 0 || exit 1
