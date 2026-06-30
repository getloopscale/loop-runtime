#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#82 closure
# (the "New project" dialog `Sheet.Description` exposed internal
# roadmap jargon: "Register a new project. The full GitHub picker +
# framework detect wizard lands in a future iter." — three distinct
# anti-patterns: "future iter" is internal developer shorthand,
# "GitHub picker" and "framework detect wizard" are features the user
# has no context for and reading them implies the product is unfinished).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #82 verbatim:
#   * Open /projects → click "New project".
#   * Pre-fix: the Sheet subtitle exposes "future iter" + "GitHub picker"
#     + "framework detect wizard" — all internal roadmap jargon banned
#     by `.claude/rules/public-copy.md`.
#   * Post-fix (commit 87058b4 in loopzero-cloud, 2026-06-28):
#       `dashboard/src/components/sheets/create-project-sheet.tsx:69-71`
#       replaces the Sheet.Description with user-facing copy:
#         "Give your project a name. Connect it to a repository in the
#          next step."
#       All three jargon tokens are GONE from the user-facing bundle.
#   * Family of #59 (same New-project dialog jargon family).
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — production projects page chunk (projects-*.js) carries the new
#       user-facing Sheet.Description literal — proof the copy rewrite
#       shipped.
#   B — same chunk does NOT carry the "future iter" jargon token —
#       proof the internal roadmap shorthand is gone (the most egregious
#       anti-pattern per `public-copy.md`).
#   C — same chunk does NOT carry "GitHub picker" AND does NOT carry
#       "framework detect wizard" — proof both feature-tease tokens are
#       gone (telling users about features that don't exist is a #82
#       anti-pattern, not just the "future iter" phrase).
#
# Slug:           dashboard-create-project-sheet-no-roadmap-jargon
# Issue:          loopzero-cloud#82
# Family:         #59 (same New-project dialog jargon family)
# Closed by:      `dashboard/src/components/sheets/create-project-sheet.tsx:69-71`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#82)
# Companion (CI): infra/tests/check-dev-public-dashboard-create-project-sheet-no-roadmap-jargon-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-create-project-sheet-no-roadmap-jargon"
ISSUE_REF="loopzero-cloud#82"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#82 (New-project dialog no roadmap jargon).

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

locate_projects_chunk() {
    local html entry_js entry_body
    html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
    entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
    entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
    # Match `projects-<hash>.js` but NOT `project-status-*.js`,
    # `project-detail-*.js`, `project-connect-*.js`, `project-scale-*.js`,
    # `project-readiness-*.js`. Anchor on the plural `projects-` token.
    printf '%s' "$entry_body" \
        | { grep -oE '[/"]projects-[A-Za-z0-9_-]+\.js' || true ; } \
        | sed 's|^[/"]||' \
        | head -1
}

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Probe: ${PUBLIC_CLOUD_URL}/projects"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — projects chunk carries the new user-facing Sheet.Description.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — projects chunk carries the new user-facing Sheet.Description literal"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        chunk="$(locate_projects_chunk)"
        if [ -z "$chunk" ] ; then
            record_fail A "could not locate projects page chunk"
        else
            log "  observed page chunk: $chunk"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${chunk}" 2>&1 || true)"
            give_count="$(printf '%s' "$chunk_body" | { grep -oF 'Give your project a name' || true ; } | wc -l | tr -d ' ')"
            connect_count="$(printf '%s' "$chunk_body" | { grep -oF 'Connect it to a repository' || true ; } | wc -l | tr -d ' ')"
            if [ "$give_count" -gt 0 ] && [ "$connect_count" -gt 0 ] ; then
                record_pass A "chunk $chunk has new copy (give=$give_count + connect=$connect_count)"
            else
                record_fail A "chunk $chunk MISSING new user-facing copy (give=$give_count connect=$connect_count)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — projects chunk does NOT carry "future iter" jargon.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — projects chunk does NOT carry 'future iter' roadmap jargon"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        chunk="$(locate_projects_chunk)"
        if [ -z "$chunk" ] ; then
            record_fail B "could not locate projects page chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${chunk}" 2>&1 || true)"
            jargon_count="$(printf '%s' "$chunk_body" | { grep -oF 'future iter' || true ; } | wc -l | tr -d ' ')"
            if [ "$jargon_count" -eq 0 ] ; then
                record_pass B "chunk has NO 'future iter' jargon (=$jargon_count — pre-fix shape absent)"
            else
                record_fail B "chunk has $jargon_count 'future iter' jargon occurrence(s) — regression"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — projects chunk carries NEITHER "GitHub picker" NOR
# "framework detect wizard" feature-tease tokens.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — projects chunk carries NEITHER 'GitHub picker' NOR 'framework detect wizard' tokens"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        chunk="$(locate_projects_chunk)"
        if [ -z "$chunk" ] ; then
            record_fail C "could not locate projects page chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${chunk}" 2>&1 || true)"
            gh_count="$(printf '%s' "$chunk_body" | { grep -oF 'GitHub picker' || true ; } | wc -l | tr -d ' ')"
            fd_count="$(printf '%s' "$chunk_body" | { grep -oF 'framework detect wizard' || true ; } | wc -l | tr -d ' ')"
            if [ "$gh_count" -eq 0 ] && [ "$fd_count" -eq 0 ] ; then
                record_pass C "chunk has no feature-tease tokens (GitHub picker=$gh_count framework detect wizard=$fd_count)"
            else
                record_fail C "chunk has feature-tease tokens (GitHub picker=$gh_count framework detect wizard=$fd_count)"
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
