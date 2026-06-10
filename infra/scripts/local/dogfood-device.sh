#!/usr/bin/env bash
# T5.1 dogfood wrapper for Journey J-05 Device Success (aggregate verdict).
# Plan: .claude/knowledge-base/plans/2026-06-05-dashboard-journey-j05-device-success-plan.md
#
# Suites:
#   1. T4.2 hermetic syntax test (always — locks the LIVE bash structure)
#   2. T2.1 hermetic helm-values test (locks Hydra post_device_done_url contract)
#   3. vitest device-success.test.tsx (9 tests — T1.1 + T1.2)
#   4. Playwright device-success.spec.ts (2 cenários — T4.1)
#   5. T4.2 LIVE bash (3 cenários A/B/C against dev-public)
#
# EC-5 anti-skip default:
#   If Playwright deps are absent, Suite 4 is RECORDED as SKIP and the
#   aggregate verdict marks FAIL (exit non-zero). Override with
#   `--allow-skip` for CI lean configurations that explicitly consent.
#   This replicates the J-04 lesson "SKIPPED ≠ PASSED" — silent green
#   mascarando cobertura ausente is FORBIDDEN.
#
# Flags:
#   --hermetic-only   only Suites 1 + 2 + 3 + 4 (skip LIVE bash, ~15s)
#   --live-only       only Suite 5 LIVE (operator drilldown)
#   --json            aggregated JSON verdict on stdout
#   --allow-skip      degrade Playwright SKIP from FAIL to neutral

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"

HERMETIC_ONLY=0
LIVE_ONLY=0
JSON_MODE=0
ALLOW_SKIP=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --hermetic-only) HERMETIC_ONLY=1; shift ;;
    --live-only) LIVE_ONLY=1; shift ;;
    --json) JSON_MODE=1; shift ;;
    --allow-skip) ALLOW_SKIP=1; shift ;;
    -h|--help) sed -n '2,22p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) shift ;;
  esac
done

log() { printf '[dogfood-device] %s\n' "$*" >&2; }

RESULTS=()
TOTAL_PASS=0
TOTAL_FAIL=0
TOTAL_SKIP=0

record() {
  local suite="$1" verdict="$2" detail="${3:-}"
  RESULTS+=("$suite|$verdict|$detail")
  case "$verdict" in
    PASS) TOTAL_PASS=$((TOTAL_PASS + 1)); log "[$suite] PASS" ;;
    SKIP)
      TOTAL_SKIP=$((TOTAL_SKIP + 1))
      if [[ "$ALLOW_SKIP" -eq 0 ]]; then
        # EC-5: SKIP without explicit consent counts as FAIL.
        TOTAL_FAIL=$((TOTAL_FAIL + 1))
        log "[$suite] SKIP: $detail — treating as FAIL (override: --allow-skip)"
      else
        log "[$suite] SKIP (allow-skip enabled): $detail"
      fi
      ;;
    *)
      TOTAL_FAIL=$((TOTAL_FAIL + 1))
      log "[$suite] FAIL — $detail"
      ;;
  esac
}

# ─────────────────────────────────────────────────────────────────────────
# Suite 1: T4.2 hermetic syntax
# ─────────────────────────────────────────────────────────────────────────
if [[ "$LIVE_ONLY" -ne 1 ]]; then
  log "▶ Suite 1: T4.2 hermetic syntax (LIVE bash structure gate)"
  if bash "$REPO_ROOT/infra/tests/check-dev-public-device-flow-syntax_test.sh" >/dev/null 2>&1; then
    record "T4.2-hermetic-syntax" "PASS" "19/0 GREEN"
  else
    record "T4.2-hermetic-syntax" "FAIL" "syntax test rejected structure"
  fi

  # ───────────────────────────────────────────────────────────────────────
  # Suite 2: T2.1 hermetic helm-values
  # ───────────────────────────────────────────────────────────────────────
  log "▶ Suite 2: T2.1 hermetic helm-values (Hydra post_device_done_url contract)"
  if bash "$REPO_ROOT/infra/tests/helm/ory-hydra-device-urls_test.sh" >/dev/null 2>&1; then
    record "T2.1-helm-values" "PASS" "6/0 GREEN"
  else
    record "T2.1-helm-values" "FAIL" "Hydra contract regression"
  fi

  # ───────────────────────────────────────────────────────────────────────
  # Suite 3: vitest hermetic (covers T1.1 + T1.2)
  # ───────────────────────────────────────────────────────────────────────
  log "▶ Suite 3: vitest hermetic (device-success.test.tsx)"
  if (cd "$REPO_ROOT/cloud/dashboard" && npx vitest run src/pages/device-success.test.tsx 2>&1 | tail -10) >/tmp/dogfood-device-vitest.log 2>&1; then
    record "vitest-hermetic" "PASS" "device-success tests GREEN"
  else
    record "vitest-hermetic" "FAIL" "see /tmp/dogfood-device-vitest.log"
  fi

  # ───────────────────────────────────────────────────────────────────────
  # Suite 4: Playwright cenários (T4.1)
  # ───────────────────────────────────────────────────────────────────────
  if [[ -d "$REPO_ROOT/cloud/dashboard/node_modules/@playwright/test" ]]; then
    log "▶ Suite 4: Playwright cenários (auto-close + Back link)"
    if (cd "$REPO_ROOT/cloud/dashboard" && PW_DEV_URL=http://localhost:5173 npx playwright test e2e/device-success.spec.ts --project=chromium --reporter=list 2>&1 | tail -10) >/tmp/dogfood-device-playwright.log 2>&1; then
      record "playwright-dom" "PASS" "2 cenários GREEN"
    else
      record "playwright-dom" "FAIL" "see /tmp/dogfood-device-playwright.log"
    fi
  else
    # EC-5: SKIP treated as FAIL unless --allow-skip
    record "playwright-dom" "SKIP" "Playwright deps absent (run: npx playwright install chromium)"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Suite 5: T4.2 LIVE bash (3 cenários A/B/C)
# ─────────────────────────────────────────────────────────────────────────
if [[ "$HERMETIC_ONLY" -ne 1 ]]; then
  log "▶ Suite 5: T4.2 LIVE bash (3 cenários A/B/C)"
  if bash "$REPO_ROOT/infra/tests/e2e/dev-public-device-flow_test.sh" >/tmp/dogfood-device-live.log 2>&1; then
    record "T4.2-live-bash" "PASS" "3/3 cenários GREEN"
  else
    rc=$?
    record "T4.2-live-bash" "FAIL" "exit code $rc — see /tmp/dogfood-device-live.log"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Summary
# ─────────────────────────────────────────────────────────────────────────
log ""
log "════════════════════════════════════════════════════════"
log "  DOGFOOD DEVICE AGGREGATE: $TOTAL_PASS PASS / $TOTAL_FAIL FAIL / $TOTAL_SKIP SKIP"
log "════════════════════════════════════════════════════════"
for r in "${RESULTS[@]}"; do
  log "  $r"
done

if [[ "$JSON_MODE" -eq 1 ]]; then
  local_verdict=$([[ "$TOTAL_FAIL" -eq 0 ]] && echo "PASS" || echo "FAIL")
  printf '{"verdict":"%s","total_pass":%d,"total_fail":%d,"skipped":%d,"suites":[' \
    "$local_verdict" "$TOTAL_PASS" "$TOTAL_FAIL" "$TOTAL_SKIP"
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
