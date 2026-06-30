#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#81 closure
# (the "Get your app live" onboarding panel exposed its progress as a
# generic <div> with only an `aria-label="Onboarding progress 60%"`
# string — no `role`, no `aria-valuenow`, no `aria-valuemax`. Assistive
# tech announced it as static text, NOT as a progress indicator —
# inconsistent with the rest of the product where the billing usage +
# plan meters correctly use `role="progressbar"`).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #81 verbatim:
#   * Sign in, open the "Get your app live" onboarding panel.
#   * Pre-fix: the progress element resolved `role=(none)`, only an
#     `aria-label` string was present. Compare with `/billing` usage
#     meters that use `role="progressbar"` + `aria-valuenow/aria-valuemax`.
#   * Post-fix (commit 87058b4 in loopzero-cloud, 2026-06-28):
#       `dashboard/src/components/onboarding/onboarding-panel.tsx:159-167`
#       promotes the OUTER track div to:
#         - role="progressbar"
#         - aria-label="Onboarding progress"
#         - aria-valuenow={percent}
#         - aria-valuemin={0}
#         - aria-valuemax={100}
#         - aria-valuetext={`${percent}%`}
#       The INNER bar (visual gradient) stays `aria-hidden="true"`.
#   * Family of #25 / #51 / #80 (a11y attribute hygiene QA Sprint 1).
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — production entry bundle (index-*.js) carries the canonical fix
#       shape: BOTH `role:"progressbar"` AND `"aria-label":"Onboarding
#       progress"` literals. The onboarding panel is part of the always-
#       loaded dashboard layout, so vite emits it into the main entry.
#   B — same bundle carries BOTH `"aria-valuenow":` AND
#       `"aria-valuemax":100` — proof the value-tracking quadrant shipped
#       (without aria-valuenow + aria-valuemax, AT can't announce a
#       percentage).
#   C — same bundle carries `aria-valuetext` — proof the screen-reader-
#       friendly percent text format shipped (without it AT would
#       announce "60", not "60%").
#
# Slug:           dashboard-onboarding-progressbar-a11y
# Issue:          loopzero-cloud#81
# Family:         #25 / #51 / #80 (a11y attribute hygiene)
# Closed by:      `dashboard/src/components/onboarding/onboarding-panel.tsx:159-167`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#81)
# Companion (CI): infra/tests/check-dev-public-dashboard-onboarding-progressbar-a11y-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-onboarding-progressbar-a11y"
ISSUE_REF="loopzero-cloud#81"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#81 (onboarding progressbar a11y).

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

locate_entry_chunk() {
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
# Scenario A — entry bundle carries `role:"progressbar"` AND
# `"aria-label":"Onboarding progress"`.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — entry bundle carries role:\"progressbar\" + \"aria-label\":\"Onboarding progress\""
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        entry_js="$(locate_entry_chunk)"
        if [ -z "$entry_js" ] ; then
            record_fail A "could not locate /assets/index-*.js entry"
        else
            log "  observed entry chunk: $entry_js"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
            role_count="$(printf '%s' "$chunk_body" | { grep -oF 'role:"progressbar"' || true ; } | wc -l | tr -d ' ')"
            label_count="$(printf '%s' "$chunk_body" | { grep -oF '"Onboarding progress"' || true ; } | wc -l | tr -d ' ')"
            if [ "$role_count" -gt 0 ] && [ "$label_count" -gt 0 ] ; then
                record_pass A "entry has role:\"progressbar\"=$role_count + Onboarding progress label=$label_count"
            else
                record_fail A "entry MISSING canonical fix shape (role=$role_count label=$label_count — pre-fix shape: generic div with only aria-label)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — entry bundle carries `"aria-valuenow":` AND
# `"aria-valuemax":100` — the value-tracking quadrant.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — entry bundle carries aria-valuenow + aria-valuemax:100 (value-tracking quadrant)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        entry_js="$(locate_entry_chunk)"
        if [ -z "$entry_js" ] ; then
            record_fail B "could not locate entry bundle"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
            vn_count="$(printf '%s' "$chunk_body" | { grep -oE '"aria-valuenow":' || true ; } | wc -l | tr -d ' ')"
            vm_count="$(printf '%s' "$chunk_body" | { grep -oF '"aria-valuemax":100' || true ; } | wc -l | tr -d ' ')"
            if [ "$vn_count" -gt 0 ] && [ "$vm_count" -gt 0 ] ; then
                record_pass B "entry has aria-valuenow=$vn_count + aria-valuemax:100=$vm_count"
            else
                record_fail B "entry MISSING value-tracking quadrant (valuenow=$vn_count valuemax=$vm_count)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — entry bundle carries `aria-valuetext` (the percent-text
# format that AT announces as "60%" instead of just "60").
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — entry bundle carries aria-valuetext (percent-text for AT)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        entry_js="$(locate_entry_chunk)"
        if [ -z "$entry_js" ] ; then
            record_fail C "could not locate entry bundle"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
            vt_count="$(printf '%s' "$chunk_body" | { grep -oF 'aria-valuetext' || true ; } | wc -l | tr -d ' ')"
            if [ "$vt_count" -gt 0 ] ; then
                record_pass C "entry has aria-valuetext — $vt_count occurrence(s) (percent-text for AT)"
            else
                record_fail C "entry MISSING aria-valuetext (AT would announce '60' not '60%')"
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
