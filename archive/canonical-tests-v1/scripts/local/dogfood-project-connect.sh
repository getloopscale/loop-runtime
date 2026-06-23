#!/usr/bin/env bash
# T6.2 dogfood wrapper for Journey J-09 Project Connect (aggregate verdict).
# Plan: .claude/knowledge-base/plans/2026-06-05-dashboard-journey-j09-project-connect-plan.md
#
# Suites:
#   1. T4.1 hermetic syntax (LIVE bash structure gate, 22 asserts)
#   2. vitest project-connect.test.tsx (10 tests — T1.2 + T2.2 + T3.1)
#   3. Go engine tests (TestListRepositories + TestRepositories_Connect)
#   4. Go cloud tests (TestInitiateInstall + TestCallback)
#   5. Go webhook tests (TestWebhook + counter + correlation_id)
#   6. Playwright project-connect.spec.ts (4 cenários — T6.1)
#   7. T4.1 LIVE bash (4 cenários A/B/C/D against dev-public)
#
# EC-5 anti-skip default: SKIP without --allow-skip counts as FAIL. Mirrors
# the J-04/J-05 lesson "SKIPPED ≠ PASSED".
#
# Flags:
#   --hermetic-only   only Suites 1-6 (skip LIVE bash, no network)
#   --live-only       only Suite 7 LIVE (operator drilldown)
#   --json            aggregated JSON verdict on stdout
#   --allow-skip      degrade Playwright SKIP from FAIL to neutral

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

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

log() { printf '[dogfood-project-connect] %s\n' "$*" >&2; }

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
# Hermetic suites (1-6)
# ─────────────────────────────────────────────────────────────────────────
if [[ "$LIVE_ONLY" -ne 1 ]]; then
  log "▶ Suite 1: T4.1 hermetic syntax (LIVE bash structure gate)"
  if bash "$REPO_ROOT/infra/tests/check-dev-public-github-webhook-syntax_test.sh" >/dev/null 2>&1; then
    record "T4.1-hermetic-syntax" "PASS" "22/22 asserts GREEN"
  else
    record "T4.1-hermetic-syntax" "FAIL" "syntax test rejected structure"
  fi

  log "▶ Suite 2: vitest project-connect.test.tsx (T1.2 + T2.2 + T3.1)"
  if (cd "$REPO_ROOT/cloud/dashboard" && npx vitest run src/pages/project-connect.test.tsx 2>&1 | tail -10) >/tmp/dogfood-project-connect-vitest.log 2>&1; then
    record "vitest-hermetic" "PASS" "10/10 project-connect tests GREEN"
  else
    record "vitest-hermetic" "FAIL" "see /tmp/dogfood-project-connect-vitest.log"
  fi

  log "▶ Suite 3: Go engine tests (TestListRepositories + TestRepositories_Connect)"
  if (cd "$REPO_ROOT/api" && go test ./internal/routes/ -run "TestListRepositories|TestRepositories_Connect" -count=1 2>&1 | tail -3) >/tmp/dogfood-project-connect-engine.log 2>&1; then
    record "go-engine-tests" "PASS" "TestListRepositories + TestRepositories_Connect GREEN"
  else
    record "go-engine-tests" "FAIL" "see /tmp/dogfood-project-connect-engine.log"
  fi

  log "▶ Suite 4: Go cloud tests (TestInitiateInstall + TestCallback)"
  if (cd "$REPO_ROOT/cloud" && go test ./internal/routes/ -run "TestInitiateInstall|TestCallback" -count=1 2>&1 | tail -3) >/tmp/dogfood-project-connect-cloud.log 2>&1; then
    record "go-cloud-tests" "PASS" "TestInitiateInstall + TestCallback GREEN"
  else
    record "go-cloud-tests" "FAIL" "see /tmp/dogfood-project-connect-cloud.log"
  fi

  log "▶ Suite 5: Go webhook tests (TestWebhook + counter + correlation_id)"
  if (cd "$REPO_ROOT/api" && go test ./internal/github/webhook/ -run "TestWebhook" -count=1 2>&1 | tail -3) >/tmp/dogfood-project-connect-webhook.log 2>&1; then
    record "go-webhook-tests" "PASS" "TestWebhook suite GREEN"
  else
    record "go-webhook-tests" "FAIL" "see /tmp/dogfood-project-connect-webhook.log"
  fi

  if [[ ! -d "$REPO_ROOT/cloud/dashboard/node_modules/@playwright/test" ]]; then
    record "playwright-dom" "SKIP" "Playwright deps absent (run: npx playwright install chromium)"
  elif ! curl -sf --max-time 2 "${PW_DEV_URL:-http://localhost:5173}/" >/dev/null 2>&1; then
    # Playwright needs a live dev server AND working cross-origin stubs
    # for /sessions/whoami + /api/v1/auth/session/token. When no server is
    # up the cenários all redirect to /login. Treat as SKIP so the
    # operator knows it's a runtime precondition, not a regression.
    record "playwright-dom" "SKIP" "dev server at ${PW_DEV_URL:-http://localhost:5173} not reachable (run: cd cloud/dashboard && npm run dev)"
  else
    log "▶ Suite 6: Playwright project-connect.spec.ts (4 cenários — T6.1)"
    if (cd "$REPO_ROOT/cloud/dashboard" && PW_DEV_URL="${PW_DEV_URL:-http://localhost:5173}" npx playwright test e2e/project-connect.spec.ts --project=chromium --reporter=list 2>&1 | tail -10) >/tmp/dogfood-project-connect-playwright.log 2>&1; then
      record "playwright-dom" "PASS" "4 cenários GREEN"
    else
      record "playwright-dom" "FAIL" "see /tmp/dogfood-project-connect-playwright.log"
    fi
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# LIVE suite (7)
# ─────────────────────────────────────────────────────────────────────────
if [[ "$HERMETIC_ONLY" -ne 1 ]]; then
  log "▶ Suite 7: T4.1 LIVE bash (4 cenários A/B/C/D against dev-public)"
  if [[ -z "${LOOPZERO_GITHUB_WEBHOOK_SECRET:-}" ]]; then
    # CANONICAL 2026-06-16: resolve from K8s Secret loopzero-github-webhook (loopzero-system).
    # Seeded post-Cleanroom by seed-github-app-dev.sh per
    # feedback_post_cleanroom_seed_github_app_canonical. NO env-var-only path.
    LOOPZERO_GITHUB_WEBHOOK_SECRET="$(kubectl --context "${MGMT_CTX:-k3d-loopzero-mgmt}" -n loopzero-system \
      get secret loopzero-github-webhook -o jsonpath='{.data.value}' 2>/dev/null | base64 -d 2>/dev/null || true)"
    export LOOPZERO_GITHUB_WEBHOOK_SECRET
  fi
  if [[ -z "${LOOPZERO_GITHUB_WEBHOOK_SECRET:-}" ]]; then
    record "T4.1-live-bash" "SKIP" "loopzero-github-webhook Secret not seeded (run seed-github-app-dev.sh)"
  else
    if bash "$REPO_ROOT/infra/tests/e2e/dev-public-github-webhook_test.sh" >/tmp/dogfood-project-connect-live.log 2>&1; then
      record "T4.1-live-bash" "PASS" "4/4 cenários GREEN"
    else
      rc=$?
      record "T4.1-live-bash" "FAIL" "exit code $rc — see /tmp/dogfood-project-connect-live.log"
    fi
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Summary
# ─────────────────────────────────────────────────────────────────────────
log ""
log "════════════════════════════════════════════════════════"
log "  DOGFOOD PROJECT-CONNECT AGGREGATE: $TOTAL_PASS PASS / $TOTAL_FAIL FAIL / $TOTAL_SKIP SKIP"
log "════════════════════════════════════════════════════════"
for r in "${RESULTS[@]}"; do
  log "  $r"
done

if [[ "$JSON_MODE" -eq 1 ]]; then
  verdict=$([[ "$TOTAL_FAIL" -eq 0 ]] && echo "PASS" || echo "FAIL")
  printf '{"verdict":"%s","total_pass":%d,"total_fail":%d,"skipped":%d,"suites":[' \
    "$verdict" "$TOTAL_PASS" "$TOTAL_FAIL" "$TOTAL_SKIP"
  first=1
  for r in "${RESULTS[@]}"; do
    IFS='|' read -r suite vrd detail <<< "$r"
    [[ "$first" -eq 0 ]] && printf ','
    first=0
    printf '{"suite":"%s","verdict":"%s","detail":"%s"}' "$suite" "$vrd" "${detail//\"/\\\"}"
  done
  printf ']}\n'
fi

[[ "$TOTAL_FAIL" -eq 0 ]]
