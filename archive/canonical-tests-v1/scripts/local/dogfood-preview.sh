#!/usr/bin/env bash
# J-23 #64 of the 4-task batch (sponsor verbatim 2026-06-07 "Adicionar
# teste de PR preview ao dogfood wrapper").
#
# Aggregate runner for the preview-canonical chain dogfood. Invokes:
#   1. infra/tests/check-dev-public-preview-flow-syntax_test.sh
#      (hermetic — bash -n + grep contract assertions)
#   2. operators/internal/tenant/tenant_gitops_test.go canonical helper
#      tests (#69 destination namespace contract)
#   3. operators/internal/preview/... envtest specs
#      (#67 SA on canonical namespace)
#   4. infra/tests/e2e/dev-public-preview-flow_test.sh
#      (LIVE — kubectl + curl asserting the chain end-to-end)
#
# Flags mirror the other dogfood wrappers in this directory:
#   --hermetic-only   Only run hermetic suites (1+2+3 — no LIVE cluster touch)
#   --live-only       Only run the LIVE bash test (4) — assume codebase suites pass
#   --json            Emit machine-readable verdict on stdout
#   --skip-envtest    Skip Ginkgo envtest (~3min) — saves time when iterating
#
# Wired in taskfiles/test.yml as `task test:dogfood:preview`.

set -euo pipefail

WORKDIR="$(cd "$(dirname "$0")/../../../.." && pwd)"
cd "$WORKDIR"

HERMETIC_ONLY=0
LIVE_ONLY=0
JSON_OUTPUT=0
SKIP_ENVTEST=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --hermetic-only) HERMETIC_ONLY=1 ;;
    --live-only)     LIVE_ONLY=1 ;;
    --json)          JSON_OUTPUT=1 ;;
    --skip-envtest)  SKIP_ENVTEST=1 ;;
    -h|--help)
      cat <<USAGE
Usage: $0 [--hermetic-only] [--live-only] [--json] [--skip-envtest]
USAGE
      exit 0 ;;
    *) printf 'Unknown flag: %s\n' "$1" >&2; exit 2 ;;
  esac
  shift
done

TOTAL_PASS=0
TOTAL_FAIL=0
SUITE_RESULTS=()

run_suite() {
  local label="$1"; shift
  local cmd=("$@")
  if [[ "$JSON_OUTPUT" != "1" ]]; then
    printf '\n=== %s ===\n' "$label" >&2
  fi
  if "${cmd[@]}"; then
    SUITE_RESULTS+=("$label:PASS")
    TOTAL_PASS=$((TOTAL_PASS+1))
  else
    SUITE_RESULTS+=("$label:FAIL")
    TOTAL_FAIL=$((TOTAL_FAIL+1))
  fi
}

if [[ "$LIVE_ONLY" != "1" ]]; then
  run_suite "hermetic syntax test" \
    bash infra/tests/check-dev-public-preview-flow-syntax_test.sh

  run_suite "Go #69 canonical destination helper" \
    go test ./operators/internal/tenant/... -run TestCanonicalDestNamespace_J23_69 -count=1

  if [[ "$SKIP_ENVTEST" != "1" ]]; then
    run_suite "Go #67 preview SA envtest" \
      go test ./operators/internal/preview/... -count=1 -short
  fi
fi

if [[ "$HERMETIC_ONLY" != "1" ]]; then
  run_suite "LIVE preview flow (#65 #66 #67 #68 #69 + D2/D3/D5)" \
    bash infra/tests/e2e/dev-public-preview-flow_test.sh
fi

if [[ "$JSON_OUTPUT" == "1" ]]; then
  printf '{"pass":%d,"fail":%d,"suites":[' "$TOTAL_PASS" "$TOTAL_FAIL"
  local first=1
  for r in "${SUITE_RESULTS[@]}"; do
    if [[ "$first" == "1" ]]; then first=0; else printf ','; fi
    printf '"%s"' "$r"
  done
  printf ']}\n'
else
  printf '\n=== AGGREGATE ===\n'
  for r in "${SUITE_RESULTS[@]}"; do printf '  %s\n' "$r"; done
  printf '\nTotal: %d PASS / %d FAIL\n' "$TOTAL_PASS" "$TOTAL_FAIL"
fi

exit "$([ "$TOTAL_FAIL" -eq 0 ] && echo 0 || echo 1)"
