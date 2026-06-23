#!/usr/bin/env bash
# J-11 Domains plan v1.1 T5.1 — dogfood wrapper.
#
# Aggregates:
#   1. T4.3 hermetic syntax test for the LIVE bash test
#   2. vitest hermetic for hooks + components (20 specs)
#   3. T4.2 LIVE bash dev-public-domains_test.sh (OPERATOR HARD STOP per ADR D5)

set -euo pipefail

HERMETIC_ONLY=0
LIVE_ONLY=0
JSON_OUTPUT=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --hermetic-only) HERMETIC_ONLY=1 ;;
    --live-only) LIVE_ONLY=1 ;;
    --json) JSON_OUTPUT=1 ;;
    --help|-h)
      cat <<EOF
Usage: $(basename "$0") [--hermetic-only] [--live-only] [--json]

J-11 Domains dogfood aggregator. Per ADR D5, the LIVE step is an operator
HARD STOP — runs only when --live-only is passed OR when the default
mode is invoked from a sponsor JUNTOS session.
EOF
      exit 0 ;;
    *) echo "Unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
cd "$REPO_ROOT"

PASS=0
FAIL=0
RESULTS=()

run_suite() {
  local label="$1"; shift
  if "$@" >/dev/null 2>&1; then
    PASS=$((PASS+1))
    RESULTS+=("PASS: $label")
    [[ $JSON_OUTPUT -eq 0 ]] && echo "✓ $label"
  else
    FAIL=$((FAIL+1))
    RESULTS+=("FAIL: $label")
    [[ $JSON_OUTPUT -eq 0 ]] && echo "✗ $label"
  fi
}

if [[ $LIVE_ONLY -eq 0 ]]; then
  run_suite "T4.3 hermetic syntax test" \
    bash infra/tests/check-dev-public-domains-syntax_test.sh

  run_suite "vitest J-11 hooks + components (20 specs)" \
    bash -c "cd cloud/dashboard && npx vitest run src/hooks/use-domain-status-polling.test.tsx src/components/domains/"
fi

if [[ $HERMETIC_ONLY -eq 0 ]]; then
  run_suite "T4.2 LIVE bash dev-public-domains (operator only)" \
    bash infra/tests/e2e/dev-public-domains_test.sh
fi

echo
if [[ $JSON_OUTPUT -eq 1 ]]; then
  jq -n \
    --argjson pass "$PASS" \
    --argjson fail "$FAIL" \
    --arg verdict "$([[ $FAIL -eq 0 ]] && echo PASS || echo FAIL)" \
    --argjson results "$(printf '%s\n' "${RESULTS[@]}" | jq -R . | jq -s .)" \
    '{pass:$pass,fail:$fail,verdict:$verdict,results:$results}'
else
  echo "=== summary: $PASS pass / $FAIL fail ==="
fi

exit $(( FAIL > 0 ? 1 : 0 ))
