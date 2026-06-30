#!/usr/bin/env bash
# Dogfood aggregator for Red Team loopzero-cloud#121.
# Wires S1 hermetic syntax + S2 vitest deployments suite + S3 LIVE bash.
set -euo pipefail

SLUG="dashboard-deployments-active-row-attribution"
HERMETIC_ONLY=0
LIVE_ONLY=0
JSON_OUTPUT=0
RESULTS=()

for arg in "$@"; do
  case "$arg" in
    --hermetic-only) HERMETIC_ONLY=1 ;;
    --live-only) LIVE_ONLY=1 ;;
    --json) JSON_OUTPUT=1 ;;
    --help)
      cat <<EOF
Usage: $(basename "$0") [--hermetic-only] [--live-only] [--json] [--help]

Aggregate dogfood wrapper for slug ${SLUG} (closes Red Team #121).
EOF
      exit 0
      ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done

if [ "$HERMETIC_ONLY" -eq 1 ] && [ "$LIVE_ONLY" -eq 1 ]; then
  echo "mutual exclusion: --hermetic-only and --live-only" >&2; exit 2
fi

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

run_suite() {
  local id="$1" cmd="$2"
  if (cd "$REPO_ROOT" && eval "$cmd") >/dev/null 2>&1; then
    RESULTS+=("$id:PASS")
  else
    RESULTS+=("$id:FAIL")
  fi
}

if [ "$LIVE_ONLY" -eq 0 ]; then
  run_suite "S1-hermetic-syntax" "bash infra/tests/check-dev-public-dashboard-deployments-active-row-attribution-syntax_test.sh"
fi

if [ "$LIVE_ONLY" -eq 0 ]; then
  run_suite "S2-vitest-deployments" "cd ../loopzero-cloud/dashboard && npm test -- deployments.test.tsx --run"
fi

if [ "$HERMETIC_ONLY" -eq 0 ]; then
  run_suite "S3-live-active-row-attribution" "bash infra/tests/e2e/dev-public-dashboard-deployments-active-row-attribution_test.sh"
fi

fail_count=0
for r in "${RESULTS[@]}"; do
  case "$r" in *:FAIL) fail_count=$((fail_count + 1)) ;; esac
done

if [ "$JSON_OUTPUT" -eq 1 ]; then
  printf '{"slug":"%s","suites":[' "$SLUG"
  first=1
  for r in "${RESULTS[@]}"; do
    IFS=':' read -r id verdict <<<"$r"
    [ $first -eq 0 ] && printf ','
    printf '{"id":"%s","verdict":"%s"}' "$id" "$verdict"
    first=0
  done
  printf '],"fail_count":%d}\n' "$fail_count"
else
  for r in "${RESULTS[@]}"; do echo "$r"; done
  echo "fail_count=$fail_count"
fi

[ "$fail_count" -eq 0 ]
