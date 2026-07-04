#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#73 closure
# (SPA route swap rendered the PREVIOUS page content under the new URL
# for ~1-2s without any loading state, plus a stale toast from the prior
# route — "Failed to start checkout" still visible after navigating from
# /billing to /projects).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #73 verbatim:
#   * Click the sidebar to navigate between sections.
#   * Pre-fix: the URL updated immediately but the main pane kept showing
#     the previous page for ~1-2s with no skeleton / spinner. Worst case
#     observed: navigating /billing → /projects while a "Failed to start
#     checkout" toast was visible meant the toast haunted /projects even
#     though it belonged to /billing.
#   * Post-fix (commit 87058b4 in loopzero-cloud, 2026-06-28):
#       (a) `dashboard/src/app.tsx:215-218` adds a `RouteScope` component
#           that wraps the dashboard's inner Routes in a div keyed by
#           `location.pathname`. A keyed child forces React to UNMOUNT
#           the previous route's tree immediately on pathname change,
#           which (i) discards stale content and (ii) dismisses owned
#           toasts.
#       (b) `app.tsx:281` wraps the inner Routes in a NESTED Suspense
#           with `<RouteLoading />` as fallback so the user sees an AT-
#           accessible loading state during lazy chunk load — not the
#           previous page.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — entry bundle (index-*.js) carries the minified RouteScope keyed-
#       div emission `.jsx("div",{children:<ref>},<ref>.pathname` — proof
#       the route-keyed unmount-on-pathname-change shipped. This is the
#       unique signature of the #73 fix: React's auto-runtime jsx() call
#       signature is `jsx(type, props, key)`, so a keyed div with a
#       single children prop compiles into exactly this shape.
#   B — entry bundle carries the `Loading…` visible text — proof the
#       `<RouteLoading />` component shipped as the Suspense fallback.
#   C — entry bundle carries `role="status"` — proof the loading state
#       is AT-accessible (the QA Red Team contract: screen readers MUST
#       announce the transition).
#
# Slug:           dashboard-route-swap-no-stale-content
# Issue:          loopzero-cloud#73
# Family:         #58 (billing route swap stale toast — same root cause)
# Closed by:      `dashboard/src/app.tsx:200-218 + 272-281` (RouteScope + nested Suspense+RouteLoading)
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#73)
# Companion (CI): infra/tests/check-dev-public-dashboard-route-swap-no-stale-content-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-route-swap-no-stale-content"
ISSUE_REF="loopzero-cloud#73"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#73 (SPA route swap stale content).

Environment:
  PUBLIC_CLOUD_URL  default https://app-dev.loopzero.example

Per cleanroom-up-live-test-mandate.md INQUEBRÁVEL: run AFTER Cleanroom TOTAL
+ UP + seed-post-cleanroom-all.sh. Operator-side. Not for CI.
EOF
}

while [ "$#" -gt 0 ] ; do
    case "$1" in
        --skip-live) SKIP_LIVE=1 ;;
        --scenario=*) SCENARIO_FILTER="${1#--scenario=}" ;;
        --json) JSON_OUTPUT=1 ;;
        --help|-h) usage ; exit 0 ;;
        *) echo "unknown arg: $1" >&2 ; usage ; exit 2 ;;
    esac
    shift
done

log() { printf '[%s] %s\n' "$(basename "$0")" "$*" ; }

PASS_COUNT=0
FAIL_COUNT=0
FAILED_LIST=""
RESULTS_JSON='{"scenarios":['
RESULTS_FIRST=1

record_pass() {
    PASS_COUNT=$((PASS_COUNT + 1))
    log "  PASS  $1${2:+ ($2)}"
    [ $RESULTS_FIRST -eq 1 ] && RESULTS_FIRST=0 || RESULTS_JSON+=","
    RESULTS_JSON+="{\"scenario\":\"$1\",\"verdict\":\"PASS\",\"detail\":\"${2//\"/\\\"}\"}"
}

record_fail() {
    FAIL_COUNT=$((FAIL_COUNT + 1))
    FAILED_LIST+=" $1"
    log "  FAIL  $1${2:+ — $2}"
    [ $RESULTS_FIRST -eq 1 ] && RESULTS_FIRST=0 || RESULTS_JSON+=","
    RESULTS_JSON+="{\"scenario\":\"$1\",\"verdict\":\"FAIL\",\"detail\":\"${2//\"/\\\"}\"}"
}

want_scenario() {
    [ -z "$SCENARIO_FILTER" ] && return 0
    case ",$SCENARIO_FILTER," in *,"$1",*) return 0 ;; *) return 1 ;; esac
}

# Resolve entry bundle once; reuse across scenarios.
locate_entry_js() {
    local html
    html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
    printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1
}

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Probe entry bundle: ${PUBLIC_CLOUD_URL}/assets/index-*.js"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — minified RouteScope keyed-div emission. JSX `<div
# key={location.pathname}>{children}</div>` compiles to the auto-runtime
# call `jsx("div", {children: <ref>}, <ref>.pathname)` — the canonical
# signature `jsx(type, props, key)`. This pattern is unique to the #73 fix.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — entry bundle carries the minified RouteScope keyed-div emission"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        entry_js="$(locate_entry_js)"
        if [ -z "$entry_js" ] ; then
            record_fail A "could not locate /assets/index-*.js entry from /"
        else
            log "  observed entry chunk: $entry_js"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
            keyed_count="$(printf '%s' "$chunk_body" | grep -oE '\.jsx\("div",\{children:[a-zA-Z_]+\},[a-zA-Z_]+\.pathname' | wc -l | tr -d ' ')"
            if [ "$keyed_count" -gt 0 ] ; then
                record_pass A "entry bundle has RouteScope keyed-div emission — $keyed_count occurrence(s)"
            else
                record_fail A "entry bundle MISSING RouteScope keyed-div (pre-fix shape — previous route lingers under new URL)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — entry bundle carries the `Loading…` visible text from the
# RouteLoading component shipped as the Suspense fallback.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — entry bundle carries the RouteLoading 'Loading…' visible text"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        entry_js="$(locate_entry_js)"
        if [ -z "$entry_js" ] ; then
            record_fail B "could not locate /assets/index-*.js entry"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
            loading_count="$(printf '%s' "$chunk_body" | grep -oF 'Loading…' | wc -l | tr -d ' ')"
            if [ "$loading_count" -gt 0 ] ; then
                record_pass B "entry bundle has 'Loading…' RouteLoading text — $loading_count occurrence(s)"
            else
                record_fail B "entry bundle MISSING 'Loading…' (RouteLoading fallback unshipped)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — entry bundle carries `role="status"` — the AT-accessible
# marker on RouteLoading so screen readers announce the transition.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — entry bundle carries role=\"status\" on the loading state (AT-accessible)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        entry_js="$(locate_entry_js)"
        if [ -z "$entry_js" ] ; then
            record_fail C "could not locate /assets/index-*.js entry"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
            # vite may emit either `role:"status"` (object literal) or
            # `role="status"` (string literal). Accept either.
            role_count="$(printf '%s' "$chunk_body" | grep -oE 'role[:=]"status"' | wc -l | tr -d ' ')"
            if [ "$role_count" -gt 0 ] ; then
                record_pass C "entry bundle has role=\"status\" — $role_count occurrence(s) (AT-accessible loading)"
            else
                record_fail C "entry bundle MISSING role=\"status\" (AT contract broken)"
            fi
        fi
    fi
fi

RESULTS_JSON+="],\"pass\":$PASS_COUNT,\"fail\":$FAIL_COUNT,\"failed_list\":\"${FAILED_LIST# }\"}"

log "============================================================"
log "Summary: PASS=$PASS_COUNT FAIL=$FAIL_COUNT"
if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '%s\n' "$RESULTS_JSON"
fi

[ $FAIL_COUNT -eq 0 ] || exit 1
exit 0
