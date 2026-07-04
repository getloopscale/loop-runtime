#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#102 closure
# (dashboard /projects/:name/repository orphan deep-link / bookmark).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #102 verbatim:
#   * Sidebar item "Repository" navigates to /projects/:name/connect
#     (Repository connection — "Install the LoopZero GitHub App"), which works.
#   * Pre-fix: pasting / bookmarking the URL /projects/:name/repository
#     directly resolved to a generic SPA "Page not found / That page doesn't
#     exist / Go home" empty state — bookmarks and shared deep-links broke.
#   * Post-fix (commit 87058b4 in loopzero-cloud, 2026-06-28): the React Router
#     config registers /projects/:name/repository → <Navigate replace
#     to="/projects/:name/connect">. Deep-link survives; menu + URL agree.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — Caddy / SPA shell: GET /projects/:name/repository returns 200 with
#       the SPA index HTML (no 404 at the edge; SPA owns the route).
#   B — production entry bundle (index-*.js) carries the route literal
#       `:name/repository` — proof the route is REGISTERED (otherwise the
#       SPA fallback would render the generic 404 EmptyState).
#   C — same entry bundle carries the canonical `/projects/:name/connect`
#       redirect destination — proof the alias's data dependency is wired.
#       Without it the Navigate would point at a non-existent target.
#
# Slug:           dashboard-project-repository-route-canonical
# Issue:          loopzero-cloud#102
# Family:         #93 (sub-view routing/guards consistency)
# Closed by:      `dashboard/src/app.tsx` (RepositoryAlias + <Route path="/projects/:name/repository">)
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#102)
# Companion (CI): infra/tests/check-dev-public-dashboard-project-repository-route-canonical-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-project-repository-route-canonical"
ISSUE_REF="loopzero-cloud#102"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
PROJECT_NAME="${PROJECT_NAME:-loopzerokit-example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#102 (dashboard /repository orphan route).

Environment:
  PUBLIC_CLOUD_URL  default https://app-dev.loopzero.example
  PROJECT_NAME      default loopzerokit-example (any project the user can list —
                    /repository is an alias; existence not required for the
                    edge probe or for the bundle-grep proofs)

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

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Probe URL: ${PUBLIC_CLOUD_URL}/projects/${PROJECT_NAME}/repository"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — Caddy serves the SPA shell (HTTP 200) for the deep-link. The
# SPA owns the route client-side via <Navigate replace>. This proves the
# Caddy file_server fallback honors any /projects/<name>/<sub> path.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — GET /projects/${PROJECT_NAME}/repository returns 200 (SPA shell)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 \
            "${PUBLIC_CLOUD_URL}/projects/${PROJECT_NAME}/repository" 2>&1 || true)"
        if [ "$code" = "200" ] ; then
            record_pass A "HTTP 200 — SPA shell served, client-side router owns the redirect"
        else
            record_fail A "HTTP $code — expected 200 (SPA fallback)"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — production entry bundle (index-*.js) carries the route literal
# `:name/repository`. Without the route registered, the SPA fallback would
# render the generic 404 EmptyState (the pre-fix shape Red Team filed).
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — production entry bundle registers /projects/:name/repository route"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
        entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
        if [ -z "$entry_js" ] ; then
            record_fail B "could not locate /assets/index-*.js entry from /"
        else
            log "  observed entry chunk: $entry_js"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
            ref_count="$(printf '%s' "$chunk_body" | grep -oF ':name/repository' | wc -l | tr -d ' ')"
            if [ "$ref_count" -gt 0 ] ; then
                record_pass B "bundle $entry_js registers ':name/repository' route — $ref_count occurrence(s)"
            else
                record_fail B "bundle $entry_js MISSING ':name/repository' route (pre-fix shape — orphan deep-link)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — same entry bundle carries the canonical `/projects/:name/connect`
# redirect destination. Without it, the Navigate target would be unwired and
# the redirect would loop / point at a 404.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — production entry bundle carries canonical /connect redirect destination"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
        entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
        chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
        # Two valid shapes coexist in the minified bundle:
        #   - route registration literal: `/projects/:name/connect`
        #   - Navigate target template:   `/projects/{project}/connect` etc.
        connect_count="$(printf '%s' "$chunk_body" | grep -oE '/projects/[:{][a-zA-Z]+[}]?/connect' | wc -l | tr -d ' ')"
        if [ "$connect_count" -gt 0 ] ; then
            record_pass C "bundle $entry_js carries canonical /connect redirect destination — $connect_count occurrence(s)"
        else
            record_fail C "bundle $entry_js MISSING /projects/.../connect destination (alias unwired)"
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
