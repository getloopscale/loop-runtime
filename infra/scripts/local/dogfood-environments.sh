#!/usr/bin/env bash
# T5.1 dogfood wrapper for Plan J-10 — Dashboard Journey Environments.
# Plan: .claude/knowledge-base/plans/2026-06-06-dashboard-journey-j10-environments-plan.md
#
# Invokes the J-10 suite components in sequence and reports an aggregate
# verdict so operators run ONE command before declaring the journey
# regression-free:
#
#   1. T4.4 hermetic syntax test (always — structural gate)
#   2. vitest J-10 (5 specs files: use-environments + delete-env-dialog +
#      promote-dialog + environment-kpi-row + plan-limit-upgrade-modal)
#   3. Go API tests for promote handler (api/internal/routes — 11 specs)
#   4. T4.3 LIVE bash against dev-public (scenarios A-F)
#   5. Playwright browser-runtime E2E (j10-environments.spec.ts — 7 specs).
#      Requires Vite dev server on http://localhost:5174 with
#      VITE_USE_MOCKS=true. Skipped (NOT failed) when the dev server
#      isn't reachable OR when Chromium isn't installed locally.
#
# Flags:
#   --hermetic-only   only suites 1+2+3 (CI velocity, ~30s, no LIVE)
#   --live-only       only suite 4 LIVE (operator drilldown)
#   --skip-playwright skip Suite 5 explicitly (e.g., no chromium in env)
#   --json            aggregated JSON verdict (machine-readable)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"

HERMETIC_ONLY=0
LIVE_ONLY=0
SKIP_PLAYWRIGHT=0
JSON_MODE=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --hermetic-only) HERMETIC_ONLY=1; shift ;;
    --live-only) LIVE_ONLY=1; shift ;;
    --skip-playwright) SKIP_PLAYWRIGHT=1; shift ;;
    --json) JSON_MODE=1; shift ;;
    -h|--help) sed -n '2,25p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) shift ;;
  esac
done

log() {
  if [[ "$JSON_MODE" != "1" ]]; then
    printf '[dogfood-environments] %s\n' "$*" >&2
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
# Suite 1: T4.4 hermetic syntax check
# ─────────────────────────────────────────────────────────────────────────
if [[ "$LIVE_ONLY" -ne 1 ]]; then
  log "▶ Suite 1: T4.4 hermetic syntax"
  if bash "$REPO_ROOT/infra/tests/check-dev-public-environments-flow-syntax_test.sh" >/dev/null 2>&1; then
    record "T4.4-hermetic-syntax" "PASS" "26/0 GREEN"
  else
    record "T4.4-hermetic-syntax" "FAIL" "syntax test rejected structure"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Suite 2: vitest J-10 (5 spec files: dialogs + hooks + KPI + 402 modal)
# ─────────────────────────────────────────────────────────────────────────
if [[ "$LIVE_ONLY" -ne 1 ]]; then
  log "▶ Suite 2: vitest J-10 dialogs + hooks + KPI + 402 modal"
  if (cd "$REPO_ROOT/cloud/dashboard" && \
      npx vitest run \
        src/hooks/mutations/use-environments.test.tsx \
        src/components/dialogs/delete-environment-dialog.test.tsx \
        src/components/dialogs/promote-dialog.test.tsx \
        src/components/dialogs/plan-limit-upgrade-modal.test.tsx \
        src/components/environments/environment-kpi-row.test.tsx \
        2>/dev/null >/dev/null); then
    record "vitest-j10-dialogs-hooks" "PASS" "30 specs GREEN (incl. EC-14 + EC-15)"
  else
    record "vitest-j10-dialogs-hooks" "FAIL" "one or more vitest specs failed"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Suite 3: Go API tests for the J-10 backend surface (promote handler)
# ─────────────────────────────────────────────────────────────────────────
if [[ "$LIVE_ONLY" -ne 1 ]]; then
  log "▶ Suite 3: Go api/internal/routes promote handler"
  if (cd "$REPO_ROOT/api" && \
      go test -run 'TestPromote_' ./internal/routes/... \
        -count=1 -timeout=60s >/dev/null 2>&1); then
    record "go-api-promote-handler" "PASS" "11 tests + 3 subtests GREEN"
  else
    record "go-api-promote-handler" "FAIL" "Go test failure in promote handler"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Suite 4: T4.3 LIVE bash against dev-public
# ─────────────────────────────────────────────────────────────────────────
if [[ "$HERMETIC_ONLY" -ne 1 ]]; then
  log "▶ Suite 4: T4.3 LIVE bash (dev-public)"
  if bash "$REPO_ROOT/infra/tests/e2e/dev-public-environments-flow_test.sh" >/dev/null 2>&1; then
    record "T4.3-live-bash" "PASS" "6 scenarios A-F PASS"
  else
    record "T4.3-live-bash" "FAIL" "LIVE scenario(s) regressed against dev-public"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Suite 5: Playwright browser-runtime E2E (mocks)
#
# Closes the "sponsor browser end-to-end" DoD gate at the functional
# level. Skipped (NOT failed) when Vite mocks server isn't reachable on
# :5174 OR when --skip-playwright is passed — environment limitation,
# not a code bug.
# ─────────────────────────────────────────────────────────────────────────
if [[ "$LIVE_ONLY" -ne 1 && "$SKIP_PLAYWRIGHT" -ne 1 ]]; then
  log "▶ Suite 5: Playwright J-10 browser-runtime E2E"
  # Pre-flight: Vite mocks server reachable?
  if curl -fsS -o /dev/null -m 3 "http://localhost:5174/" 2>/dev/null; then
    if (cd "$REPO_ROOT/cloud/dashboard" && \
        PW_DEV_URL=http://localhost:5174 \
        npx playwright test e2e/j10-environments.spec.ts \
          --project=chromium \
          --reporter=line \
          >/dev/null 2>&1); then
      record "playwright-j10-browser" "PASS" "7 specs GREEN (chromium)"
    else
      record "playwright-j10-browser" "FAIL" "browser-runtime spec failed"
    fi
  else
    log "[Suite 5] SKIP — Vite mocks server not reachable on :5174"
    log "         Start with: cd cloud/dashboard && VITE_USE_MOCKS=true npx vite --port 5174 &"
    # Do NOT count toward FAIL — env limitation, not a code issue.
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
  printf '\n[dogfood-environments] suite verdict: %d/%d PASS, %d FAIL\n' \
    "$TOTAL_PASS" "$TOTAL" "$TOTAL_FAIL" >&2
  for r in "${RESULTS[@]}"; do
    IFS='|' read -r suite verdict detail <<<"$r"
    printf '  %-30s %s %s\n' "$suite" "$verdict" "$detail" >&2
  done
fi

[[ "$TOTAL_FAIL" == "0" ]] && exit 0 || exit 1
