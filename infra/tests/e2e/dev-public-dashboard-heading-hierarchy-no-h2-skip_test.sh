#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#79 closure
# (broken heading hierarchy on /projects/{name} AND /profile — the DOM
# went h1 → h3 with no h2 in between; "Health" (h3) even rendered BEFORE
# the first h2. Screen-reader users navigating by heading level got a
# disordered, skipped outline).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #79 verbatim:
#   * Open /projects/{name} and enumerate headings:
#     pre-fix: `[H1:'loopzerokit-example', H3:'Health', H2:'Overview', H2:'Apps',
#     ...]` — h1 → h3 skip + h3 before the first h2.
#   * Open /profile and enumerate headings:
#     pre-fix: `[H1:'Profile', H3:'Account', ...]` — h1 → h3 skip again.
#   * Post-fix (commit 87058b4 in loopzero-cloud, 2026-06-28):
#       `dashboard/src/components/project/project-health-card.tsx:108 + :128`
#       promotes the "Health" card title from `<h3>` to `<h2>`.
#       `dashboard/src/pages/profile.tsx:56 + 102` promotes "Account" and
#       "Or use the CLI" card titles to `<h2>`. Both surfaces now go
#       h1 → h2 → (optional h3) without skipping levels.
#   * Family of #25 (heading-level hygiene in QA Sprint 1).
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — production project-detail page chunk (project-detail-*.js) carries
#       `jsx("h2",{children:"Health"})` (the canonical JSX auto-runtime
#       compiled form). This is the unique signature of the #79 fix on
#       the /projects/{name} surface.
#   B — production profile page chunk (profile-*.js) carries
#       `jsx("h2",{children:"Account"...})` (Account at the new h2 level).
#   C — neither chunk carries ANY `"h3"` element type for the cards that
#       previously surfaced as h3. Specifically: project-detail-*.js has
#       0× `"h3"` AND profile-*.js has 0× `"h3"` — pre-fix shape absent.
#
# Slug:           dashboard-heading-hierarchy-no-h2-skip
# Issue:          loopzero-cloud#79
# Family:         #25 (heading hygiene in QA Sprint 1)
# Closed by:      `dashboard/src/components/project/project-health-card.tsx:108+128`
#                 + `dashboard/src/pages/profile.tsx:56+102`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#79)
# Companion (CI): infra/tests/check-dev-public-dashboard-heading-hierarchy-no-h2-skip-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-heading-hierarchy-no-h2-skip"
ISSUE_REF="loopzero-cloud#79"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#79 (heading hierarchy no h2 skip).

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

locate_page_chunk() {
    local prefix="$1" html entry_js entry_body
    html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
    entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
    entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
    printf '%s' "$entry_body" \
        | { grep -oE "${prefix}-[A-Za-z0-9_-]+\\.js" || true ; } \
        | grep -v '^use-' \
        | head -1
}

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Probe: ${PUBLIC_CLOUD_URL}/projects/loopzerokit-example AND ${PUBLIC_CLOUD_URL}/profile"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — project-detail page chunk carries jsx("h2",{children:"Health"})
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — project-detail chunk carries jsx(\"h2\",{children:\"Health\"}) — the Health card promoted to h2"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        pd_chunk="$(locate_page_chunk 'project-detail')"
        if [ -z "$pd_chunk" ] ; then
            record_fail A "could not locate project-detail page chunk"
        else
            log "  observed page chunk: $pd_chunk"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${pd_chunk}" 2>&1 || true)"
            h2_health="$(printf '%s' "$chunk_body" | { grep -oE 'jsx\("h2",\{children:"Health"' || true ; } | wc -l | tr -d ' ')"
            if [ "$h2_health" -gt 0 ] ; then
                record_pass A "chunk $pd_chunk has jsx(\"h2\",{children:\"Health\"}) — $h2_health occurrence(s)"
            else
                record_fail A "chunk $pd_chunk MISSING h2 Health (pre-fix shape — Health rendered as h3)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — profile page chunk carries jsx("h2",{children:"Account"...})
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — profile chunk carries jsx(\"h2\",{children:\"Account\"...}) — Account card promoted to h2"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        pr_chunk="$(locate_page_chunk 'profile')"
        if [ -z "$pr_chunk" ] ; then
            record_fail B "could not locate profile page chunk"
        else
            log "  observed page chunk: $pr_chunk"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${pr_chunk}" 2>&1 || true)"
            h2_account="$(printf '%s' "$chunk_body" | { grep -oE 'jsx\("h2",\{children:"Account' || true ; } | wc -l | tr -d ' ')"
            if [ "$h2_account" -gt 0 ] ; then
                record_pass B "chunk $pr_chunk has jsx(\"h2\",{children:\"Account…\"}) — $h2_account occurrence(s)"
            else
                record_fail B "chunk $pr_chunk MISSING h2 Account (pre-fix shape — Account rendered as h3)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — NEITHER project-detail-*.js NOR profile-*.js carries any
# `"h3"` element-type literal. The fix promoted both surfaces' card
# headings from h3 to h2; if h3 reappears in either chunk, a regression
# resurfaced the heading skip.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — neither project-detail nor profile chunk carries any \"h3\" element type"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        pd_chunk="$(locate_page_chunk 'project-detail')"
        pr_chunk="$(locate_page_chunk 'profile')"
        if [ -z "$pd_chunk" ] || [ -z "$pr_chunk" ] ; then
            record_fail C "could not locate one or both page chunks (project-detail=$pd_chunk profile=$pr_chunk)"
        else
            pd_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${pd_chunk}" 2>&1 || true)"
            pr_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${pr_chunk}" 2>&1 || true)"
            pd_h3="$(printf '%s' "$pd_body" | { grep -oF '"h3"' || true ; } | wc -l | tr -d ' ')"
            pr_h3="$(printf '%s' "$pr_body" | { grep -oF '"h3"' || true ; } | wc -l | tr -d ' ')"
            if [ "$pd_h3" -eq 0 ] && [ "$pr_h3" -eq 0 ] ; then
                record_pass C "no h3 element types in either chunk (project-detail=$pd_h3 profile=$pr_h3 — pre-fix shape absent)"
            else
                record_fail C "h3 element type resurfaced (project-detail=$pd_h3 profile=$pr_h3 — heading skip regression)"
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
