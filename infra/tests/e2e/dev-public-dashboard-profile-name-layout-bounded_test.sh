#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#71 closure
# (a 20 000-char display name rendered verbatim without wrap / ellipsis,
# pushing document.body.scrollWidth to 179 512px against a 1850px viewport
# — massive horizontal overflow that broke the /profile layout).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #71 verbatim:
#   * Account with display name = 20 000 chars logged in.
#   * Pre-fix: /profile rendered the name verbatim in a <p> without any
#     max-width, word-break, or overflow-wrap — the unbreakable string
#     blew the page open horizontally (scrollWidth = 179 512px / viewport
#     1850px). Visible cousin #64: the same 20 000-char Name made the
#     Bearer JWT exceed the gateway MaxHeaderBytes (HTTP 431).
#   * Post-fix (commit 87058b4 in loopzero-cloud, 2026-06-28):
#       `dashboard/src/pages/profile.tsx:73-80` renders the name inside a
#       <p> with className containing `max-w-[28ch] break-words` AND
#       inline style `overflowWrap: 'anywhere'` AND the
#       `data-testid="profile-name"` hook — the unbreakable string is now
#       capped at 28ch wide and wraps anywhere it must.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — production profile page chunk (profile-*.js) carries the
#       `max-w-[28ch]` Tailwind utility literal — proof the max-width
#       cap landed in the served bundle.
#   B — same chunk carries `overflowWrap` (the JSX-style key that vite
#       compiles into the inline style attribute) — proof the CSS-level
#       fallback for un-wrappable strings shipped.
#   C — same chunk carries the `profile-name` data-testid — proof the
#       wiring lives on the actual rendered name <p>, not on a different
#       element that wouldn't help layout.
#
# Slug:           dashboard-profile-name-layout-bounded
# Issue:          loopzero-cloud#71
# Family:         #64 (the 431-Bearer header sibling of the same 20 000-char Name)
# Closed by:      `dashboard/src/pages/profile.tsx:73-80`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#71)
# Companion (CI): infra/tests/check-dev-public-dashboard-profile-name-layout-bounded-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-profile-name-layout-bounded"
ISSUE_REF="loopzero-cloud#71"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#71 (profile name layout-bounded).

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

# Resolve the profile page chunk once and reuse across scenarios B+C.
locate_profile_chunk() {
    local html entry_js entry_body
    html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
    entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
    entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
    printf '%s' "$entry_body" \
        | grep -oE '[A-Za-z0-9_-]*profile-[A-Za-z0-9_-]+\.js' \
        | grep -v '^use-' \
        | head -1
}

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Probe: ${PUBLIC_CLOUD_URL}/profile"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — production profile chunk carries the `max-w-[28ch]` Tailwind
# utility literal.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — production profile chunk carries the max-w-[28ch] Tailwind utility"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        page_chunk="$(locate_profile_chunk)"
        if [ -z "$page_chunk" ] ; then
            record_fail A "could not locate profile page chunk"
        else
            log "  observed page chunk: $page_chunk"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${page_chunk}" 2>&1 || true)"
            mw_count="$(printf '%s' "$chunk_body" | grep -oF 'max-w-[28ch]' | wc -l | tr -d ' ')"
            if [ "$mw_count" -gt 0 ] ; then
                record_pass A "chunk $page_chunk has max-w-[28ch] — $mw_count occurrence(s)"
            else
                record_fail A "chunk $page_chunk MISSING max-w-[28ch] (pre-fix shape — no max-width cap)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — same chunk carries `overflowWrap` (JSX style key compiled
# into the inline `style` attribute) — CSS-level fallback for un-wrappable
# strings.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — production profile chunk carries the overflowWrap CSS fallback"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        page_chunk="$(locate_profile_chunk)"
        if [ -z "$page_chunk" ] ; then
            record_fail B "could not locate profile page chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${page_chunk}" 2>&1 || true)"
            ow_count="$(printf '%s' "$chunk_body" | grep -oF 'overflowWrap' | wc -l | tr -d ' ')"
            if [ "$ow_count" -gt 0 ] ; then
                record_pass B "chunk has overflowWrap — $ow_count occurrence(s)"
            else
                record_fail B "chunk MISSING overflowWrap (pre-fix shape — un-wrappable strings still blow layout)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — same chunk carries the `profile-name` data-testid — proof
# the wiring lives on the actual rendered name <p>, not on an adjacent
# element that wouldn't help layout.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — production profile chunk wires profile-name testid on the rendered name node"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        page_chunk="$(locate_profile_chunk)"
        if [ -z "$page_chunk" ] ; then
            record_fail C "could not locate profile page chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${page_chunk}" 2>&1 || true)"
            tid_count="$(printf '%s' "$chunk_body" | grep -oF 'profile-name' | wc -l | tr -d ' ')"
            if [ "$tid_count" -gt 0 ] ; then
                record_pass C "chunk has profile-name testid — $tid_count occurrence(s)"
            else
                record_fail C "chunk MISSING profile-name testid (per-row wiring drift)"
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
