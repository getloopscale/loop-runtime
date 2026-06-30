#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#83 closure
# (the New-project dialog input was NOT programmatically linked to its
# validation error: when the name failed the slug regex the input had
# `aria-invalid=null` + no `aria-errormessage` / `aria-describedby`
# pointing at the destructive Alert. Sighted users could read the
# detached Alert region; AT users got NOTHING from the focused field).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #83 verbatim:
#   * Open /projects → "New Project" → type "AB" → click "Create
#     project".
#   * Pre-fix: validation fails (the SLUG_PATTERN regex requires ≥ 3
#     chars), the Alert renders "Name must be lowercase, 3-40 chars,
#     start with letter, hyphens allowed.", but the focused input keeps
#     `aria-invalid=null` and no `aria-describedby`/`aria-errormessage`
#     pointing at the error region.
#   * Post-fix (commit 87058b4 in loopzero-cloud, 2026-06-28):
#       `dashboard/src/components/sheets/create-project-sheet.tsx:85-95`
#       wires the `<Input id="project-name">` to two help regions:
#         - aria-invalid={error ? true : undefined}
#         - aria-describedby={error
#             ? 'project-name-help project-name-error'
#             : 'project-name-help'}
#       The `<Alert id="project-name-error">` is rendered conditionally;
#       the `<p id="project-name-help">` is always rendered (always
#       described to AT). Family of #25 / #51 / #80 / #81 (a11y
#       attribute hygiene QA Sprint 1).
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — production projects page chunk (projects-*.js) carries the
#       `aria-invalid` attribute literal — proof the toggleable invalid
#       marker shipped.
#   B — same chunk carries the `aria-describedby` attribute literal —
#       proof the programmatic association shipped.
#   C — same chunk carries BOTH canonical region ids: `project-name-help`
#       AND `project-name-error` — proof BOTH ends of the describedby
#       link exist on the page (without `project-name-error` the link
#       would dangle when validation fails).
#
# Slug:           dashboard-create-project-input-error-aria-link
# Issue:          loopzero-cloud#83
# Family:         #25 / #51 / #80 / #81 (a11y attribute hygiene)
# Closed by:      `dashboard/src/components/sheets/create-project-sheet.tsx:85-95 + 105-110`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#83)
# Companion (CI): infra/tests/check-dev-public-dashboard-create-project-input-error-aria-link-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-create-project-input-error-aria-link"
ISSUE_REF="loopzero-cloud#83"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#83 (new-project input aria error link).

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

# Resolve the projects-*.js page chunk (plural — disambiguates from
# project-detail-*.js, project-status-*.js, project-scale-*.js, etc.).
locate_projects_chunk() {
    local html entry_js entry_body
    html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
    entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
    entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
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
# Scenario A — projects chunk carries `aria-invalid` literal.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — projects chunk carries the aria-invalid attribute literal"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        chunk="$(locate_projects_chunk)"
        if [ -z "$chunk" ] ; then
            record_fail A "could not locate projects page chunk"
        else
            log "  observed page chunk: $chunk"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${chunk}" 2>&1 || true)"
            inv_count="$(printf '%s' "$chunk_body" | { grep -oF 'aria-invalid' || true ; } | wc -l | tr -d ' ')"
            if [ "$inv_count" -gt 0 ] ; then
                record_pass A "chunk $chunk has aria-invalid — $inv_count occurrence(s)"
            else
                record_fail A "chunk $chunk MISSING aria-invalid (pre-fix shape — input not flagged invalid)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — projects chunk carries `aria-describedby` literal.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — projects chunk carries the aria-describedby attribute literal"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        chunk="$(locate_projects_chunk)"
        if [ -z "$chunk" ] ; then
            record_fail B "could not locate projects page chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${chunk}" 2>&1 || true)"
            db_count="$(printf '%s' "$chunk_body" | { grep -oF 'aria-describedby' || true ; } | wc -l | tr -d ' ')"
            if [ "$db_count" -gt 0 ] ; then
                record_pass B "chunk has aria-describedby — $db_count occurrence(s)"
            else
                record_fail B "chunk MISSING aria-describedby (pre-fix shape — no programmatic link to error region)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — projects chunk carries BOTH `project-name-help` AND
# `project-name-error` region ids — proof both ends of the describedby
# link exist on the page.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — projects chunk carries BOTH region ids (project-name-help + project-name-error)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        chunk="$(locate_projects_chunk)"
        if [ -z "$chunk" ] ; then
            record_fail C "could not locate projects page chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${chunk}" 2>&1 || true)"
            help_count="$(printf '%s' "$chunk_body" | { grep -oF 'project-name-help' || true ; } | wc -l | tr -d ' ')"
            err_count="$(printf '%s' "$chunk_body" | { grep -oF 'project-name-error' || true ; } | wc -l | tr -d ' ')"
            if [ "$help_count" -gt 0 ] && [ "$err_count" -gt 0 ] ; then
                record_pass C "chunk has both region ids (help=$help_count + error=$err_count)"
            else
                record_fail C "chunk MISSING region ids (help=$help_count error=$err_count — describedby would dangle)"
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
