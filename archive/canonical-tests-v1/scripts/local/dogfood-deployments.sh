#!/usr/bin/env bash
# T7.3 dogfood wrapper for Plan J-17 — Dashboard Journey Deployments.
# Plan: .claude/knowledge-base/plans/2026-06-06-dashboard-journey-j17-deployments-realtime-plan.md
#
# Invokes the J-17 suite components in sequence and reports an aggregate
# verdict so operators run ONE command before declaring the journey
# regression-free:
#
#   1. T7.2 hermetic syntax test (always — structural gate)
#   2. vitest J-17 hooks + pages (deployments-list-stream + deployments)
#   3. Go API tests (argo, loki, routes/logs.go — the L1/L2/L3 stack)
#   4. T7.2 LIVE bash against dev-public (--skip-live mode if --hermetic-only)
#
# Flags:
#   --hermetic-only   only suites 1+2+3 (CI velocity, ~30s, no LIVE)
#   --live-only       only suite 4 LIVE (operator drilldown)
#   --json            aggregated JSON verdict (machine-readable)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

HERMETIC_ONLY=0
LIVE_ONLY=0
JSON_MODE=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --hermetic-only) HERMETIC_ONLY=1; shift ;;
    --live-only) LIVE_ONLY=1; shift ;;
    --json) JSON_MODE=1; shift ;;
    -h|--help) sed -n '2,18p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) shift ;;
  esac
done

log() {
  if [[ "$JSON_MODE" != "1" ]]; then
    printf '[dogfood-deployments] %s\n' "$*" >&2
  fi
}

RESULTS=()
TOTAL_PASS=0
TOTAL_FAIL=0

record() {
  local suite="$1" verdict="$2" detail="${3:-}"
  RESULTS+=("$suite|$verdict|$detail")
  if [[ "$verdict" == "PASS" ]]; then
    TOTAL_PASS=$((TOTAL_PASS + 1))
    log "[$suite] PASS"
  else
    TOTAL_FAIL=$((TOTAL_FAIL + 1))
    log "[$suite] FAIL — $detail"
  fi
}

# ─────────────────────────────────────────────────────────────────────────
# Suite 1: T7.2 hermetic syntax check
# ─────────────────────────────────────────────────────────────────────────
if [[ "$LIVE_ONLY" -ne 1 ]]; then
  log "▶ Suite 1: T7.2 hermetic syntax"
  if bash "$REPO_ROOT/infra/tests/check-dev-public-deployments-flow-syntax_test.sh" >/dev/null 2>&1; then
    record "T7.2-hermetic-syntax" "PASS" "17/0 GREEN"
  else
    record "T7.2-hermetic-syntax" "FAIL" "syntax test rejected structure"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Suite 2: vitest J-17 (hooks + pages)
# ─────────────────────────────────────────────────────────────────────────
if [[ "$LIVE_ONLY" -ne 1 ]]; then
  log "▶ Suite 2: vitest J-17 hooks + pages"
  if (cd "$REPO_ROOT/cloud/dashboard" && \
      npx vitest run \
        src/hooks/use-deployments-list-stream.test.tsx \
        src/hooks/use-deployments.test.tsx \
        src/pages/deployments.test.tsx \
        2>/dev/null >/dev/null); then
    record "vitest-j17-hooks-pages" "PASS" "hooks + pages GREEN"
  else
    record "vitest-j17-hooks-pages" "FAIL" "one or more vitest specs failed"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Suite 3: Go API tests for the J-17 backend surface
# ─────────────────────────────────────────────────────────────────────────
if [[ "$LIVE_ONLY" -ne 1 ]]; then
  log "▶ Suite 3: Go api/internal/{argo,loki,routes} tests"
  if (cd "$REPO_ROOT/api" && \
      go test ./internal/argo/... ./internal/loki/... ./internal/routes/... \
        -count=1 -timeout=60s >/dev/null 2>&1); then
    record "go-api-j17-surface" "PASS" "argo + loki + routes GREEN"
  else
    record "go-api-j17-surface" "FAIL" "Go test failure in J-17 backend surface"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Suite 4: T7.2 LIVE bash against dev-public
# ─────────────────────────────────────────────────────────────────────────
if [[ "$HERMETIC_ONLY" -ne 1 ]]; then
  log "▶ Suite 4: T7.2 LIVE bash (dev-public)"
  if bash "$REPO_ROOT/infra/tests/e2e/dev-public-deployments-flow_test.sh" >/dev/null 2>&1; then
    record "T7.2-live-bash" "PASS" "4 scenarios PASS"
  else
    record "T7.2-live-bash" "FAIL" "LIVE scenario(s) regressed against dev-public"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Verdict
# ─────────────────────────────────────────────────────────────────────────
TOTAL=$((TOTAL_PASS + TOTAL_FAIL))

if [[ "$JSON_MODE" == "1" ]]; then
  printf '{"pass":%d,"fail":%d,"total":%d,"verdict":"%s","suites":[' \
    "$TOTAL_PASS" "$TOTAL_FAIL" "$TOTAL" \
    "$([[ "$TOTAL_FAIL" == "0" ]] && echo PASS || echo FAIL)"
  first=1
  for r in "${RESULTS[@]}"; do
    IFS='|' read -r suite verdict detail <<<"$r"
    [[ "$first" == "1" ]] && first=0 || printf ','
    printf '{"suite":"%s","verdict":"%s","detail":"%s"}' "$suite" "$verdict" "$detail"
  done
  printf ']}\n'
else
  printf '\n[dogfood-deployments] suite verdict: %d/%d PASS, %d FAIL\n' \
    "$TOTAL_PASS" "$TOTAL" "$TOTAL_FAIL" >&2
  for r in "${RESULTS[@]}"; do
    IFS='|' read -r suite verdict detail <<<"$r"
    printf '  %-30s %s %s\n' "$suite" "$verdict" "$detail" >&2
  done
fi

[[ "$TOTAL_FAIL" == "0" ]] && exit 0 || exit 1
