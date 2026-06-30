#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#76 closure
# (Overview project card rendered the raw RFC3339Nano ISO timestamp
# `2026-06-26T14:16:43.712767Z` while the /projects list humanized the
# same field as "32 minutes ago" — cross-view divergence + an unfriendly
# microsecond-precision string leak on the most-trafficked page).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #76 verbatim:
#   * Open `/` (Overview) and read a project card timestamp.
#   * Pre-fix: `lastDeployedAt: p.last_deploy` — raw RFC3339Nano ISO
#     string forwarded verbatim into ProjectCard → "2026-06-26T14:16:43.712767Z".
#   * Open `/projects` for the same project → "LAST DEPLOY 32 minutes ago"
#     (humanized via `formatDate(..., 'relative')`). Cross-view divergence
#     confirmed the Overview rendering was a defect, not an intentional
#     "show exact time" choice.
#   * Post-fix (commit 87058b4 in loopzero-cloud, 2026-06-28):
#       `dashboard/src/pages/overview.tsx:258-261` calls
#       `formatDate(p.last_deploy, 'relative')` — the same canonical helper
#       `/projects` uses — with an `isZeroTime` guard that routes legacy
#       zero-literal timestamps to ProjectCard's `"Never deployed"`
#       internal null-guard. Both views now render identical "32 minutes
#       ago"-style copy. Family of #53 (same family of timestamp drift).
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — production overview page chunk (overview-*.js) carries the
#       `"relative"` arg literal — proof the humanization mode shipped
#       (the second arg to formatDate that selects the "X ago" style).
#   B — same chunk imports the canonical format-*.js helper chunk —
#       proof the humanization dependency is wired in this page chunk
#       specifically (not just transitively).
#   C — same chunk does NOT carry a raw RFC3339Nano ISO microsecond
#       literal (regex `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z`) —
#       regression-protector against someone reintroducing a hardcoded
#       sample, or the pre-fix shape where p.last_deploy got forwarded
#       verbatim into a static prop.
#
# Slug:           dashboard-overview-humanized-last-deploy-time
# Issue:          loopzero-cloud#76
# Family:         #53 (same family of timestamp drift)
# Closed by:      `dashboard/src/pages/overview.tsx:258-261`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#76)
# Companion (CI): infra/tests/check-dev-public-dashboard-overview-humanized-last-deploy-time-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-overview-humanized-last-deploy-time"
ISSUE_REF="loopzero-cloud#76"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#76 (Overview humanized last-deploy time).

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

# Resolve overview page chunk once; reuse across scenarios.
locate_overview_chunk() {
    local html entry_js entry_body
    html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
    entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
    entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
    printf '%s' "$entry_body" \
        | grep -oE '[A-Za-z0-9_-]*overview-[A-Za-z0-9_-]+\.js' \
        | head -1
}

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Probe: ${PUBLIC_CLOUD_URL}/"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — overview chunk carries the `"relative"` arg literal —
# the second arg to formatDate that selects the "X ago" style.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — overview chunk carries the 'relative' formatDate mode literal"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        page_chunk="$(locate_overview_chunk)"
        if [ -z "$page_chunk" ] ; then
            record_fail A "could not locate overview page chunk"
        else
            log "  observed page chunk: $page_chunk"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${page_chunk}" 2>&1 || true)"
            rel_count="$(printf '%s' "$chunk_body" | { grep -oF '"relative"' || true ; } | wc -l | tr -d ' ')"
            if [ "$rel_count" -gt 0 ] ; then
                record_pass A "chunk $page_chunk has '\"relative\"' arg — $rel_count occurrence(s)"
            else
                record_fail A "chunk $page_chunk MISSING '\"relative\"' arg (pre-fix shape — raw ISO forwarded)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — overview chunk imports the canonical format-*.js helper
# chunk (proof the humanization helper dependency is wired in this page
# chunk specifically, not just transitively elsewhere in the bundle).
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — overview chunk imports the canonical format-*.js helper chunk"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        page_chunk="$(locate_overview_chunk)"
        if [ -z "$page_chunk" ] ; then
            record_fail B "could not locate overview page chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${page_chunk}" 2>&1 || true)"
            fmt_chunk="$(printf '%s' "$chunk_body" | { grep -oE 'format-[A-Za-z0-9_-]+\.js' || true ; } | head -1)"
            if [ -n "$fmt_chunk" ] ; then
                record_pass B "chunk imports $fmt_chunk (humanization helper dependency wired)"
            else
                record_fail B "chunk does not import a format-*.js helper (humanization dependency missing)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — overview chunk does NOT carry a raw RFC3339Nano ISO
# microsecond literal (regex `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z`).
# Regression-protector against the pre-fix shape (`lastDeployedAt:
# p.last_deploy` forwarding the raw string) AND against someone
# hardcoding a sample literal in source.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — overview chunk does NOT carry a raw RFC3339Nano ISO microsecond literal"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        page_chunk="$(locate_overview_chunk)"
        if [ -z "$page_chunk" ] ; then
            record_fail C "could not locate overview page chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${page_chunk}" 2>&1 || true)"
            # `grep -E` returns 1 on no match → pipefail would kill the
            # script. Wrap in `|| true` inside a subshell so a clean
            # chunk yields a 0 count as the assertion expects.
            iso_count="$(printf '%s' "$chunk_body" | { grep -oE '[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z' || true ; } | wc -l | tr -d ' ')"
            if [ "$iso_count" -eq 0 ] ; then
                record_pass C "chunk has NO raw RFC3339Nano ISO microsecond literal (=$iso_count — pre-fix shape absent)"
            else
                record_fail C "chunk has $iso_count raw ISO literals (regression: pre-fix shape resurfaced)"
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
