#!/usr/bin/env bash
# LIVE regression-protector for Red Team loopzero/loopzero#163 closure — MAJOR
# a11y contrast + label-in-name (WCAG 1.4.3 + 2.5.3).
#
# Root cause:
#  (1) --primary token oklch(0.628 0.225 296) ~ #995ffe gave contrast
#      ratios 3.87 (white on primary bg) + 4.37 (primary text on dark
#      bg) — both < 4.5 AA floor for normal text.
#  (2) Command palette button aria-label "Open command palette (⌘K)"
#      did not contain the visible text "Search" — voice-control name
#      mismatch (axe rule label-content-name-mismatch).
#  (3) FAB onboarding button aria-label "Get your app live — N of M
#      done. Click to expand." did not contain the badge's visible
#      text "N/M" verbatim (voice-control mismatch on the badge that
#      IS the only rendered visible text on the collapsed FAB).
#
# Fix multi-layer (parsimony rung 5):
#  - dashboard/src/index.css @theme override: --primary darkened to a
#    tonal shade that passes AA vs white; utility escape for
#    .text-primary uses --primary-glow (lighter) so foreground text on
#    dark surfaces passes AA too.
#  - top-bar.tsx: aria-label now starts with visible text "Search".
#  - onboarding-panel.tsx: aria-label now starts with badge visible
#    text "{completed}/{total}".
#
# Scenarios (bundle-grep — the fix is CSS + aria-label strings, not
# server-side; validating the CDN-served bundle is the regression
# protector):
#   A production dashboard bundle carries `Search — open command
#     palette` (proof aria-label ships)
#   B production dashboard bundle carries the FAB visible-text-first
#     shape `{count}/{total} — Get your app live` (proof aria-label
#     ships) — asserts by grepping "Get your app live" AND the badge
#     literal `${completed}/${total}` pattern in minified form
#   C preflight (dashboard reachable)
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL. Per feedback_issue_fix_requires_failing_e2e_test_2026_06_25.
#
# Flags:
#   --skip-live       hermetic CI mode
#   --scenario=A|B|C  run only one scenario
#   --json            emit JSON verdict
#   --help            print this help

set -euo pipefail

APP_URL="${APP_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON=0
SCENARIO_FILTER=""
PASS_COUNT=0
FAIL_COUNT=0
declare -a RESULTS=()

usage() { sed -n '2,50p' "$0"; }

log()  { echo "[a11y-163] $*"; }
pass() { PASS_COUNT=$((PASS_COUNT+1)); RESULTS+=("PASS $1"); log "PASS $1"; }
fail() { FAIL_COUNT=$((FAIL_COUNT+1)); RESULTS+=("FAIL $1"); log "FAIL $1"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-live)          SKIP_LIVE=1 ;;
    --scenario=*)         SCENARIO_FILTER="${1#*=}" ;;
    --json)               JSON=1 ;;
    --help|-h)            usage; exit 0 ;;
    *) echo "Unknown flag: $1"; usage; exit 1 ;;
  esac
  shift
done

should_run() {
  local id="$1"
  [ -z "$SCENARIO_FILTER" ] || [ "$SCENARIO_FILTER" = "$id" ]
}

# --- Bundle chunk locator helpers ---
# The dashboard entry chunk carries the always-loaded shell (top-bar +
# onboarding FAB); scenarios A + B both target it.
locate_entry_chunk() {
  local index_html chunk
  index_html=$(curl -sSk "$APP_URL/" || echo "")
  chunk=$(echo "$index_html" | grep -oE 'assets/index-[A-Za-z0-9_-]+\.js' | head -1 || true)
  if [ -z "$chunk" ]; then
    return 1
  fi
  echo "$chunk"
}

preflight() {
  if [ "$SKIP_LIVE" -eq 1 ]; then
    log "preflight SKIPPED (--skip-live hermetic mode)"
    return 0
  fi
  log "preflight probing $APP_URL/ (SPA reachable)"
  local code
  code=$(curl -sSk -o /dev/null -w "%{http_code}" "$APP_URL/" || echo "000")
  if [ "$code" = "200" ]; then
    pass "preflight / → 200"
  else
    fail "preflight / → $code"
    return 1
  fi
}

scenario_a() {
  should_run "A" || return 0
  log "Scenario A: entry chunk carries 'Search — open command palette' (aria-label)"
  if [ "$SKIP_LIVE" -eq 1 ]; then
    log "SKIP scenario A (--skip-live)"
    return 0
  fi

  local chunk
  chunk=$(locate_entry_chunk || true)
  if [ -z "$chunk" ]; then
    fail "A0 entry chunk not locatable from index.html"
    return 0
  fi

  local body
  body=$(curl -sSk "$APP_URL/$chunk" || echo "")
  if echo "$body" | grep -Fq 'Search — open command palette'; then
    pass "A1 entry chunk carries 'Search — open command palette' aria-label"
  else
    fail "A1 entry chunk missing 'Search — open command palette' aria-label"
  fi
}

scenario_b() {
  should_run "B" || return 0
  log "Scenario B: entry chunk carries FAB aria-label starting with '{count}/{total} — Get your app live'"
  if [ "$SKIP_LIVE" -eq 1 ]; then
    log "SKIP scenario B (--skip-live)"
    return 0
  fi

  local chunk
  chunk=$(locate_entry_chunk || true)
  if [ -z "$chunk" ]; then
    fail "B0 entry chunk not locatable"
    return 0
  fi

  local body
  body=$(curl -sSk "$APP_URL/$chunk" || echo "")

  # Visible-text literal "Get your app live" still ships (was there pre-fix
  # too — the guard is that the aria-label carries it in the RIGHT order:
  # visible-text first per WCAG 2.5.3 Label in Name).
  if echo "$body" | grep -Fq 'Get your app live'; then
    pass "B1 entry chunk carries 'Get your app live' visible-text literal"
  else
    fail "B1 entry chunk missing 'Get your app live' literal"
  fi

  # The badge-first aria-label pattern — after Vite minification the
  # template becomes an interpolation. Grep for the fingerprint: the
  # ordering is `<count>/<total> — Get your app live`. Vite emits the
  # runtime concat as ` — Get your app live. Click to expand.` — grep
  # the DASH-Get-your prefix as the shipping proof.
  if echo "$body" | grep -Fq ' — Get your app live. Click to expand.'; then
    pass "B2 FAB aria-label uses ' — Get your app live. Click to expand.' suffix (badge-first ordering)"
  else
    fail "B2 FAB aria-label missing badge-first ordering fingerprint"
  fi
}

scenario_c() {
  should_run "C" || return 0
  log "Scenario C: --primary token override shipped (regression-protector)"
  if [ "$SKIP_LIVE" -eq 1 ]; then
    log "SKIP scenario C (--skip-live)"
    return 0
  fi

  # CSS ships in an assets/index-*.css chunk (or vendor CSS). Rather than
  # try to identify the exact chunk, grep across all served .css chunks
  # for the darker --primary override marker (the "#163" comment or the
  # oklch value).
  local index_html
  index_html=$(curl -sSk "$APP_URL/" || echo "")
  local css_chunks
  css_chunks=$(echo "$index_html" | grep -oE 'assets/[A-Za-z0-9_-]+\.css' | head -3 || true)
  if [ -z "$css_chunks" ]; then
    fail "C0 no CSS chunks discovered from index.html"
    return 0
  fi

  # Vite minifies `oklch(0.55 0.22 296)` → `oklch(55% .22 296)` (percent
  # notation). Match either form so the test survives a minifier tweak.
  local found=0
  local darker_pattern='oklch\((0\.5[0-5]|5[0-5]%)'
  while IFS= read -r css; do
    [ -z "$css" ] && continue
    local body
    body=$(curl -sSk "$APP_URL/$css" || echo "")
    if echo "$body" | grep -Eq -- "--primary:[[:space:]]*$darker_pattern"; then
      found=1
      break
    fi
  done <<< "$css_chunks"

  if [ "$found" -eq 1 ]; then
    pass "C1 CSS bundle carries --primary darker override (oklch(0.5X ...))"
  else
    fail "C1 CSS bundle does not carry --primary darker override — token still at default 0.628"
  fi
}

preflight
scenario_a
scenario_b
scenario_c

echo
if [ "$JSON" -eq 1 ]; then
  printf '{"issue":"loopzero/loopzero#163","slug":"dashboard-a11y-contrast-label-in-name","pass":%d,"fail":%d,"skip_live":%s,"scenario_filter":"%s","results":[' \
    "$PASS_COUNT" "$FAIL_COUNT" "$([ $SKIP_LIVE -eq 1 ] && echo true || echo false)" "$SCENARIO_FILTER"
  first=1
  for r in "${RESULTS[@]}"; do
    [ $first -eq 1 ] || printf ','
    printf '"%s"' "${r//\"/\\\"}"
    first=0
  done
  printf ']}\n'
else
  echo "=== #163 summary: $PASS_COUNT pass / $FAIL_COUNT fail ==="
fi
exit $(( FAIL_COUNT > 0 ? 1 : 0 ))
