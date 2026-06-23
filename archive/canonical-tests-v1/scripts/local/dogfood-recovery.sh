#!/usr/bin/env bash
# T5.4 dogfood wrapper for Journey J-04 Recovery (aggregate verdict).
# Plan: .claude/knowledge-base/plans/2026-06-05-dashboard-journey-j04-recovery-plan.md
#
# Invokes the suite components in sequence:
#   1. T5.2 hermetic syntax test (always — gate for T5.1 structure regression)
#   2. T5.1 LIVE bash (5 cenários A/C/C2/D/E against dev-public DNS)
#   3. T5.3 Playwright (2 cenários B/F DOM-driven) — vitest equivalents run
#      via `npx vitest run src/pages/recovery.test.tsx` for hermetic coverage.
#
# Flags:
#   --hermetic-only   only T5.2 + vitest (CI velocidade, ~10s)
#   --live-only       only T5.1 LIVE (skip hermetic; for operator drilldown)
#   --json            aggregated JSON verdict

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

log() { printf '[dogfood-recovery] %s\n' "$*" >&2; }

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
# Suite 1: T5.2 hermetic syntax (allowlist gate + bash -n + structure)
# ─────────────────────────────────────────────────────────────────────────
if [[ "$LIVE_ONLY" -ne 1 ]]; then
  log "▶ Suite 1: T5.2 hermetic syntax"
  if bash "$REPO_ROOT/infra/tests/check-dev-public-recovery-flow-syntax_test.sh" >/dev/null 2>&1; then
    record "T5.2-hermetic-syntax" "PASS" "18/0 GREEN"
  else
    record "T5.2-hermetic-syntax" "FAIL" "syntax test rejected structure"
  fi

  # ───────────────────────────────────────────────────────────────────────
  # Suite 2: vitest hermetic recovery + login (covers Cenários B + F)
  # ───────────────────────────────────────────────────────────────────────
  log "▶ Suite 2: vitest hermetic (covers Cenários B + F via DOM-state mocks)"
  if (cd "$REPO_ROOT/cloud/dashboard" && npx vitest run src/pages/recovery.test.tsx src/pages/login.test.tsx 2>&1 | tail -20) >/tmp/dogfood-recovery-vitest.log 2>&1; then
    record "vitest-hermetic" "PASS" "recovery + login tests GREEN"
  else
    record "vitest-hermetic" "FAIL" "see /tmp/dogfood-recovery-vitest.log"
  fi

  # ───────────────────────────────────────────────────────────────────────
  # Suite 2b: Playwright Cenários B + F (DOM-driven via headless chromium)
  # Requires: Playwright + chromium installed (npx playwright install chromium)
  # Vite dev server must be running on :5173 (Tilt/Caddy stack)
  # ───────────────────────────────────────────────────────────────────────
  if [[ -d "$REPO_ROOT/cloud/dashboard/node_modules/@playwright/test" ]]; then
    log "▶ Suite 2b: Playwright Cenários B + F (DOM-driven)"
    if (cd "$REPO_ROOT/cloud/dashboard" && PW_DEV_URL=http://localhost:5173 npx playwright test e2e/recovery.spec.ts --project=chromium --reporter=list 2>&1 | tail -10) >/tmp/dogfood-recovery-playwright.log 2>&1; then
      record "playwright-dom" "PASS" "3 Cenários (B GitHub + B Google + F EC-4) GREEN"
    else
      record "playwright-dom" "FAIL" "see /tmp/dogfood-recovery-playwright.log"
    fi
  else
    log "▶ Suite 2b: SKIP — Playwright not installed (npx playwright install chromium)"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Suite 3: T5.1 LIVE bash (5 cenários A/C/C2/D/E)
# ─────────────────────────────────────────────────────────────────────────
if [[ "$HERMETIC_ONLY" -ne 1 ]]; then
  log "▶ Suite 3: T5.1 LIVE bash (5 cenários A/C/C2/D/E)"
  if bash "$REPO_ROOT/infra/tests/e2e/dev-public-recovery-flow_test.sh" 2>&1; then
    record "T5.1-live-bash" "PASS" "5/5 cenários GREEN"
  else
    rc=$?
    record "T5.1-live-bash" "FAIL" "exit code $rc — see stderr"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Summary
# ─────────────────────────────────────────────────────────────────────────
log ""
log "════════════════════════════════════════════════════════"
log "  DOGFOOD RECOVERY AGGREGATE: $TOTAL_PASS PASS / $TOTAL_FAIL FAIL"
log "════════════════════════════════════════════════════════"
for r in "${RESULTS[@]}"; do
  log "  $r"
done

if [[ "$JSON_MODE" -eq 1 ]]; then
  local_verdict=$([[ "$TOTAL_FAIL" -eq 0 ]] && echo "PASS" || echo "FAIL")
  printf '{"verdict":"%s","total_pass":%d,"total_fail":%d,"suites":[' \
    "$local_verdict" "$TOTAL_PASS" "$TOTAL_FAIL"
  first=1
  for r in "${RESULTS[@]}"; do
    IFS='|' read -r suite verdict detail <<< "$r"
    [[ "$first" -eq 0 ]] && printf ','
    first=0
    printf '{"suite":"%s","verdict":"%s","detail":"%s"}' "$suite" "$verdict" "${detail//\"/\\\"}"
  done
  printf ']}\n'
fi

[[ "$TOTAL_FAIL" -eq 0 ]]
