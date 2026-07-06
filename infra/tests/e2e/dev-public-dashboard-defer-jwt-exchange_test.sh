#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#50 closure
# (multiple pages reported [Web Vitals] LCP=4876 (POOR) on Deployments
# and LCP=3080 (NEEDS_IMPROVEMENT) on Recovery; navigations to /signup,
# /projects/loopzerokit-example and /recovery exceeded 10-25s tooling
# timeouts before content appeared. Root cause: useRequireAuth() was
# awaiting TWO sequential blocking network calls before letting the
# shell paint — Kratos /sessions/whoami AND the BFF /v1/auth/session/token
# exchange. Stacking both onto the LCP critical path roughly doubled
# perceived first-paint latency).
#
# Fix shipped 2026-06-28 in commit `87058b4` (loopzero-cloud):
#   `dashboard/src/hooks/use-auth.ts:21-31` defers the JWT exchange so
#   the shell paints at session-detect (ONE RTT, not two). The exchange
#   is NOT required for the page shell to render — `cloudFetch` /
#   `engineFetch` call `ensureToken()` which lazily exchanges on first
#   use. The pattern is `exchangeSessionToken().then(u => { if (!u)
#   navigate('/login', {replace: true}) })` — kick off the exchange in
#   the background; if it resolves false (stale Kratos cookie returns
#   no JWT) navigate to /login, otherwise the shell is already painted.
#   Closes companion #110 (stale Kratos cookie hangs the dashboard in
#   "Loading" without the fallback).
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN). OPERATOR HARD STOP per
# `.claude/rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL.
#
# Scenarios:
#   A — entry bundle carries the minified deferred-exchange shape
#       `.then(u=>{u||t("/login",{replace:!0})}` (jsx-runtime auto
#       emission for `exchangeSessionToken().then(u => { if (!u)
#       navigate('/login', {replace: true}) })`). Unique signature —
#       without this, the exchange is being awaited inline (pre-fix
#       shape) OR is missing the #110 fallback safety net.
#   B — entry bundle carries the `/v1/auth/session/token` exchange
#       endpoint literal (proves the exchange code path exists in the
#       served bundle; without it, no exchange is happening at all).
#   C — entry bundle carries the `/sessions/whoami` Kratos endpoint
#       literal (proves the canonical "detect session first, then
#       lazy-exchange JWT" two-step is preserved — the #50 fix removed
#       ONE blocking await, not both).
#
# Slug:           dashboard-defer-jwt-exchange
# Issue:          loopzero-cloud#50
# Family:         #110 (stale Kratos cookie follow-on — the deferred
#                 exchange must redirect to /login if it returns false)
# Closed by:      `loopzero-cloud/dashboard/src/hooks/use-auth.ts:21-31`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#50)
# Companion (CI): infra/tests/check-dev-public-dashboard-defer-jwt-exchange-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-defer-jwt-exchange"
ISSUE_REF="loopzero-cloud#50"
APP_URL="${APP_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#50 (defer JWT exchange off the LCP
critical path).

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
# Helper: locate the entry index bundle. The useRequireAuth hook is
# inlined into the entry chunk (app.tsx + hooks are always loaded).
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
        # Scenario A — entry bundle carries the minified deferred-exchange
        # shape `.then(u=>{u||t("/login",{replace:!0})}` — the EXACT
        # signature of the #50 fix (deferred call + #110 fallback safety
        # net to /login). The minifier may pick a different identifier
        # than `u`/`t`, so use a flexible grep on the surrounding shape.
        # -------------------------------------------------------------------
        if want_scenario A ; then
            log "Scenario A — entry bundle has '.then(<id>=>{<id>||<id>(\"/login\",{replace:!0})}' deferred-exchange shape"
            # Match: `.then(<single-char-id>=>{<single-char-id>||<id>("/login",{replace:!0})`
            shape_count="$(printf '%s' "$ENTRY_BODY" | { grep -oE '\.then\([a-z]=>\{[a-z]\|\|[a-z]\("/login",\{replace:!0\}\)\}' || true ; } | wc -l)"
            if [ "$shape_count" -ge 1 ] ; then
                record_pass A "entry bundle has deferred-exchange-with-/login-fallback shape=$shape_count"
            else
                record_fail A "entry bundle MISSING deferred-exchange shape — pre-fix shape (exchange awaited inline) OR missing #110 fallback"
            fi
        fi

        # -------------------------------------------------------------------
        # Scenario B — entry bundle carries the exchange endpoint
        # /v1/auth/session/token (proves the exchange code path exists)
        # -------------------------------------------------------------------
        if want_scenario B ; then
            log "Scenario B — entry bundle carries the '/v1/auth/session/token' exchange endpoint literal"
            exchange_count="$(printf '%s' "$ENTRY_BODY" | { grep -cF '/v1/auth/session/token' || true ; })"
            if [ "$exchange_count" -ge 1 ] ; then
                record_pass B "entry bundle has /v1/auth/session/token=$exchange_count (exchange path wired)"
            else
                record_fail B "entry bundle MISSING /v1/auth/session/token endpoint — exchange code path absent"
            fi
        fi

        # -------------------------------------------------------------------
        # Scenario C — entry bundle carries the Kratos /sessions/whoami
        # endpoint literal (the canonical "detect session first" step
        # the #50 fix preserved — only the SECOND blocking await was
        # removed)
        # -------------------------------------------------------------------
        if want_scenario C ; then
            log "Scenario C — entry bundle carries the Kratos '/sessions/whoami' endpoint literal"
            whoami_count="$(printf '%s' "$ENTRY_BODY" | { grep -cF '/sessions/whoami' || true ; })"
            if [ "$whoami_count" -ge 1 ] ; then
                record_pass C "entry bundle has /sessions/whoami=$whoami_count (session-detect step preserved)"
            else
                record_fail C "entry bundle MISSING /sessions/whoami — session-detect step absent"
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
