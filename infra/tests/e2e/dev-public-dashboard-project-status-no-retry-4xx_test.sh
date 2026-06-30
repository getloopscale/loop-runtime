#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#96 closure
# (dashboard /projects/<nonexistent>/status fires 5x GET 404 — default
# React Query retry policy applied to a deterministic 4xx response).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #96 verbatim:
#   * Open the Overview of a non-existent project (deleted, typo'd, or
#     deep-linked).
#   * Pre-fix: the dashboard's useProjectStatus hook did NOT opt into
#     throwOnError, so engineFetch returned `null` on 4xx. TanStack Query
#     treats that as a generic failure and applies its default 3-retry
#     policy — total 5 GET 404s per page open. Network waste + delayed
#     "Project not found" empty state.
#   * Post-fix (commit 87058b4 in loopzero-cloud, 2026-06-28):
#       (a) `dashboard/src/hooks/use-project-status.ts` opts into
#           `engineFetch(..., { throwOnError: true })` so a 4xx becomes a
#           typed ApiError TanStack Query can inspect.
#       (b) the hook's `retry` predicate short-circuits when the thrown
#           ApiError's status is in [400, 500): `return false` — only 5xx
#           and network errors keep the default retry policy.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — engine wire shape: GET /engine/api/v1/projects/<nonexistent>/status
#       returns 404 once (the SoT the hook reads). Without 404 the bug
#       premise doesn't hold.
#   B — production hook chunk (use-project-status-*.js) carries the
#       `throwOnError` opt-in — proof the fail-loud half of the fix shipped.
#   C — same hook chunk carries the minified retry predicate that returns
#       false for `status >= 400 && status < 500` — proof the no-retry-on-4xx
#       half of the fix shipped (without this, throwOnError alone would
#       still get retried 3x as a generic error).
#
# Slug:           dashboard-project-status-no-retry-4xx
# Issue:          loopzero-cloud#96
# Family:         #41 / #63 / #77 / #122 (engineFetch fail-loud systemic)
# Closed by:      `dashboard/src/hooks/use-project-status.ts:41-61`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#96)
# Companion (CI): infra/tests/check-dev-public-dashboard-project-status-no-retry-4xx-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-project-status-no-retry-4xx"
ISSUE_REF="loopzero-cloud#96"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
GHOST_PROJECT="${GHOST_PROJECT:-zzz-nonexistent-9281}"
SESSION_JWT_FILE="${SESSION_JWT_FILE:-}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#96 (no-retry on 4xx for /status).

Environment:
  PUBLIC_CLOUD_URL  default https://app-dev.loopzero.example
  GHOST_PROJECT     default zzz-nonexistent-9281 (any deterministic 404 name —
                    a deleted / typo'd / never-existed project)
  SESSION_JWT_FILE  default ~/.loopzero/local-dogfood-session-jwt.txt (or canonical)

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

# Default JWT discovery (canonical paths).
if [ -z "$SESSION_JWT_FILE" ] ; then
    for cand in "$HOME/.loopzero/local-dogfood-session-jwt.txt" "$HOME/.loopzero/local-dogfood-token.txt" "$HOME/.loopzero/redteam-qa-session-jwt.txt" ; do
        if [ -s "$cand" ] ; then SESSION_JWT_FILE="$cand" ; break ; fi
    done
fi

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
log "Probe URL: ${PUBLIC_CLOUD_URL}/engine/api/v1/projects/${GHOST_PROJECT}/status"
log "JWT: ${SESSION_JWT_FILE:-(none)}"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — engine wire shape: GET /status for a non-existent project
# returns 404 (the deterministic 4xx the hook used to retry). Without it
# the bug premise doesn't hold.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — GET /engine/api/v1/projects/${GHOST_PROJECT}/status returns 404 (deterministic 4xx)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    elif [ -z "$SESSION_JWT_FILE" ] || [ ! -s "$SESSION_JWT_FILE" ] ; then
        record_fail A "no SESSION_JWT_FILE — run seed-admin-loopzero-dev.sh --refresh-only"
    else
        JWT="$(cat "$SESSION_JWT_FILE")"
        code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 -L \
            -H "Authorization: Bearer $JWT" \
            "${PUBLIC_CLOUD_URL}/engine/api/v1/projects/${GHOST_PROJECT}/status" 2>&1 || true)"
        if [ "$code" = "404" ] ; then
            record_pass A "HTTP 404 — engine SoT confirms deterministic 4xx"
        else
            record_fail A "HTTP $code — expected 404 (need a ghost project name that truly does not exist)"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — production hook chunk carries the `throwOnError` opt-in.
# Without it, engineFetch's default `T | null` path makes every non-2xx
# look like a transient failure → default 3-retry policy fires.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — production hook chunk exposes throwOnError opt-in"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
        entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
        entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
        hook_chunk="$(printf '%s' "$entry_body" \
            | grep -oE 'use-project-status-[A-Za-z0-9_-]+\.js' \
            | head -1)"
        if [ -z "$hook_chunk" ] ; then
            record_fail B "could not locate use-project-status hook chunk (entry_js=$entry_js)"
        else
            log "  observed hook chunk: $hook_chunk"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${hook_chunk}" 2>&1 || true)"
            opt_count="$(printf '%s' "$chunk_body" | grep -oF 'throwOnError' | wc -l | tr -d ' ')"
            if [ "$opt_count" -gt 0 ] ; then
                record_pass B "hook chunk $hook_chunk has throwOnError opt-in — $opt_count occurrence(s)"
            else
                record_fail B "hook chunk $hook_chunk MISSING throwOnError opt-in (pre-fix shape)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — same hook chunk carries the minified retry predicate
# `status>=400&&n.status<500` (the exact form vite/rollup emits for
# `err.status >= 400 && err.status < 500 => return false`).
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — production hook chunk carries the no-retry-on-4xx predicate"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
        entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
        entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
        hook_chunk="$(printf '%s' "$entry_body" \
            | grep -oE 'use-project-status-[A-Za-z0-9_-]+\.js' \
            | head -1)"
        if [ -z "$hook_chunk" ] ; then
            record_fail C "could not locate use-project-status hook chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${hook_chunk}" 2>&1 || true)"
            # Vite/rollup emits the predicate as `status>=400&&<ref>.status<500`
            # (single-character minified var, e.g. `n.status<500`).
            pred_count="$(printf '%s' "$chunk_body" | grep -oE 'status>=400&&[a-zA-Z_][a-zA-Z0-9_]*\.status<500' | wc -l | tr -d ' ')"
            if [ "$pred_count" -gt 0 ] ; then
                record_pass C "hook chunk $hook_chunk has no-retry-on-4xx predicate — $pred_count occurrence(s)"
            else
                record_fail C "hook chunk $hook_chunk MISSING no-retry-on-4xx predicate (pre-fix shape — default 3-retry policy active)"
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
