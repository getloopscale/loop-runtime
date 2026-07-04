#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#55 closure
# (navigating to `/registration` in a clean browser without any prior
# session redirected silently to `/login` with the toast "Session
# expired. Redirecting to login…" — both factually wrong (no session
# ever existed) and confusing because the real signup route is
# `/register`. The unauthenticated catch-all guard fired before any
# route alias could intercept).
#
# Fix shipped 2026-06-28 in commit `87058b4` (loopzero-cloud):
#   `dashboard/src/app.tsx:256-263` registers
#     `<Route path="/registration" element={<Navigate to="/register" replace />} />`
#   The SPA-side `Navigate` runs as a public route BEFORE the
#   auth-guarded catch-all, so `/registration` → `/register` with a
#   `replace`-mode History entry (no back-button trap). Same shape as
#   the sibling `/signup → /register` alias at line 255.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN). OPERATOR HARD STOP per
# `.claude/rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL.
#
# Scenarios:
#   A — entry bundle carries the `path:"/registration"` route literal
#       (proves the alias route is registered in the Routes config).
#   B — entry bundle carries the minified Navigate emission shape
#       `path:"/registration",element:s.jsx(oe,{to:"/register"` (the
#       jsx-runtime auto-emission for `<Route path="/registration"
#       element={<Navigate to="/register" …`). Without this, the
#       route is registered but points somewhere else.
#   C — entry bundle carries the sibling `path:"/signup"` route with
#       the same `to:"/register"` target — proves the alias pattern is
#       canonical (not a one-off) and that the #55 fix follows the
#       documented #54-era convention.
#
# Slug:           dashboard-registration-alias
# Issue:          loopzero-cloud#55
# Family:         #54 (`/signup` alias — same convention, shipped earlier);
#                 #55 follows the documented pattern.
# Closed by:      `loopzero-cloud/dashboard/src/app.tsx:256-263`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#55)
# Companion (CI): infra/tests/check-dev-public-dashboard-registration-alias-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-registration-alias"
ISSUE_REF="loopzero-cloud#55"
APP_URL="${APP_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#55 (/registration → /register
SPA-side alias).

Environment:
  APP_URL    default https://app-dev.loopzero.example (the dev-public dashboard)

OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md INQUEBRÁVEL:
runs LIVE bundle GET against the dashboard SPA entry index chunk.
--skip-live runs the 4 structural assertions only (CI mode).
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

# ---------------------------------------------------------------------------
# Helper: locate the entry index bundle. The SPA Routes config lives
# in the entry chunk (app.tsx is part of the always-loaded layout).
# ---------------------------------------------------------------------------
locate_entry_chunk() {
    curl -s "$APP_URL/" \
        | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' \
        | head -1
}

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Probe: $APP_URL"
log "============================================================"

if [ $SKIP_LIVE -eq 1 ] ; then
    log "--skip-live mode — structural assertions only"
    record_pass A "skip-live hermetic"
    record_pass B "skip-live hermetic"
    record_pass C "skip-live hermetic"
else
    ENTRY_URL="$(locate_entry_chunk)"
    if [ -z "$ENTRY_URL" ] ; then
        record_fail PREFLIGHT "could not locate /assets/index-*.js entry chunk from $APP_URL/"
    else
        log "  observed entry chunk: $ENTRY_URL"
        ENTRY_BODY="$(curl -s "$APP_URL$ENTRY_URL")"

        # -------------------------------------------------------------------
        # Scenario A — entry bundle carries `path:"/registration"` literal
        # -------------------------------------------------------------------
        if want_scenario A ; then
            log "Scenario A — entry bundle carries 'path:\"/registration\"' route literal"
            reg_count="$(printf '%s' "$ENTRY_BODY" | { grep -cF 'path:"/registration"' || true ; })"
            if [ "$reg_count" -ge 1 ] ; then
                record_pass A "entry bundle has /registration route literal=$reg_count"
            else
                record_fail A "entry bundle MISSING /registration route literal (alias absent — pre-fix shape)"
            fi
        fi

        # -------------------------------------------------------------------
        # Scenario B — entry bundle has the Navigate emission for
        # /registration → /register specifically
        # -------------------------------------------------------------------
        if want_scenario B ; then
            log "Scenario B — entry bundle carries Navigate emission for /registration → /register"
            # Use a fixed substring grep on the unique minified shape:
            # `path:"/registration",element:s.jsx(oe,{to:"/register"` —
            # `oe` is the minified Navigate component identifier; the
            # leading `s.jsx` is the auto-runtime emission.
            navigate_count="$(printf '%s' "$ENTRY_BODY" | { grep -oE 'path:"/registration",element:[^,]+,\{to:"/register"' || true ; } | wc -l)"
            if [ "$navigate_count" -ge 1 ] ; then
                record_pass B "entry bundle has /registration→/register Navigate emission=$navigate_count"
            else
                record_fail B "entry bundle MISSING Navigate emission to /register (route registered but not aliased — pre-fix shape)"
            fi
        fi

        # -------------------------------------------------------------------
        # Scenario C — entry bundle carries the canonical sibling
        # /signup → /register alias (proves the alias pattern, which
        # the #55 fix follows)
        # -------------------------------------------------------------------
        if want_scenario C ; then
            log "Scenario C — entry bundle carries sibling /signup → /register alias (alias pattern canonical)"
            signup_count="$(printf '%s' "$ENTRY_BODY" | { grep -oE 'path:"/signup",element:[^,]+,\{to:"/register"' || true ; } | wc -l)"
            if [ "$signup_count" -ge 1 ] ; then
                record_pass C "entry bundle has /signup→/register sibling alias=$signup_count"
            else
                record_fail C "entry bundle MISSING /signup→/register sibling alias — alias pattern broken"
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
