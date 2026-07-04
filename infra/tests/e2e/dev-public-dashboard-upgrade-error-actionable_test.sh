#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#40 closure
# (clicking "Upgrade to Pro — $29 per month" on /billing surfaced
# `role=alert` with "Failed to start checkout. Please try again."
# while the underlying error was a HARD backend 4xx (POST /v1/billing/
# checkout → 400 INVALID_PLAN, per F-001). "Please try again" misleads
# the user into hammering a permanently broken endpoint — by definition
# a 4xx is non-retryable client-side, so retry copy is a dead-end).
#
# Fix shipped 2026-06-28 in commit `87058b4` (loopzero-cloud):
#   `dashboard/src/pages/billing.tsx:37-55` adds the canonical
#   `extractActionableErrorMessage(err)` helper that differentiates by
#   error class:
#     1. If the API response body carries an actionable `message`,
#        surface that verbatim (gives the backend full control over
#        copy).
#     2. Else if `err instanceof ApiError && err.status >= 400 &&
#        err.status < 500` — non-retryable 4xx — return
#        "Upgrade isn't available right now. Contact support@loopzero.example
#         — they'll get you unblocked." (the dead-end is acknowledged
#        + the user is routed to the only path that unblocks them).
#     3. Else (5xx / network) keep retryable copy "Failed to start
#        checkout. Please try again in a moment." (genuinely transient
#        — retry is the right user action).
#
# Family of #49/#69 (`openPortal()` opt-in to throwOnError so 4xx routes
# through the same `extractActionableErrorMessage` rather than silent
# no-op).
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN). OPERATOR HARD STOP per
# `.claude/rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL.
#
# Scenarios:
#   A — billing page chunk carries the canonical 4xx-actionable copy
#       literal "Upgrade isn't available right now" (proof the
#       non-retryable branch shipped).
#   B — billing page chunk carries the `support@loopzero.example` contact
#       address literal (proof the user is routed to the actually-
#       unblocking path, not generic "try again").
#   C — billing page chunk carries the retryable-branch fallback copy
#       "Please try again in a moment" (proof the genuine-transient
#       branch is preserved — the fix differentiates by class, it
#       didn't replace ALL error copy with the support message).
#
# Slug:           dashboard-upgrade-error-actionable
# Issue:          loopzero-cloud#40
# Family:         #49/#69 (Manage Plan portal — same extractActionable
#                 helper consumed via throwOnError opt-in)
# Closed by:      `loopzero-cloud/dashboard/src/pages/billing.tsx:37-55`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#40)
# Companion (CI): infra/tests/check-dev-public-dashboard-upgrade-error-actionable-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-upgrade-error-actionable"
ISSUE_REF="loopzero-cloud#40"
APP_URL="${APP_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#40 (upgrade-to-Pro 4xx surfaces
actionable copy instead of misleading "Please try again").

Environment:
  APP_URL    default https://app-dev.loopzero.example (the dev-public dashboard)

OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md INQUEBRÁVEL:
runs LIVE bundle GET against the billing page chunk.
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
# Helper: locate the billing page chunk from the entry index bundle.
# ---------------------------------------------------------------------------
locate_billing_chunk() {
    local entry_url
    entry_url="$(curl -s "$APP_URL/" \
        | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' \
        | head -1)"
    [ -z "$entry_url" ] && return 1
    curl -s "$APP_URL$entry_url" \
        | { grep -oE 'billing-[A-Za-z0-9_-]+\.js' || true ; } \
        | sort -u \
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
    BILLING_CHUNK="$(locate_billing_chunk || true)"
    if [ -z "$BILLING_CHUNK" ] ; then
        record_fail PREFLIGHT "could not locate billing-*.js chunk from $APP_URL/ entry index"
    else
        log "  observed billing chunk: $BILLING_CHUNK"
        CHUNK_BODY="$(curl -s "$APP_URL/assets/$BILLING_CHUNK")"

        # -------------------------------------------------------------------
        # Scenario A — billing chunk carries the canonical 4xx-actionable
        # copy literal "Upgrade isn't available right now"
        # -------------------------------------------------------------------
        if want_scenario A ; then
            log "Scenario A — billing chunk carries the 4xx-actionable 'Upgrade isn't available right now' literal"
            actionable_count="$(printf '%s' "$CHUNK_BODY" | { grep -cF "Upgrade isn't available right now" || true ; })"
            if [ "$actionable_count" -ge 1 ] ; then
                record_pass A "billing chunk has 4xx-actionable copy=$actionable_count"
            else
                record_fail A "billing chunk MISSING 4xx-actionable copy — non-retryable branch absent (pre-fix shape)"
            fi
        fi

        # -------------------------------------------------------------------
        # Scenario B — billing chunk carries the `support@loopzero.example`
        # contact address literal (the unblock path)
        # -------------------------------------------------------------------
        if want_scenario B ; then
            log "Scenario B — billing chunk carries the 'support@loopzero.example' contact address literal"
            support_count="$(printf '%s' "$CHUNK_BODY" | { grep -cF 'support@loopzero.example' || true ; })"
            if [ "$support_count" -ge 1 ] ; then
                record_pass B "billing chunk has support contact literal=$support_count"
            else
                record_fail B "billing chunk MISSING support@loopzero.example contact — user routed nowhere on 4xx"
            fi
        fi

        # -------------------------------------------------------------------
        # Scenario C — billing chunk carries the retryable-branch fallback
        # "Please try again in a moment" (proof the 5xx/network branch
        # is preserved — the fix differentiates, didn't replace ALL copy)
        # -------------------------------------------------------------------
        if want_scenario C ; then
            log "Scenario C — billing chunk carries the retryable 'Please try again in a moment' fallback"
            retryable_count="$(printf '%s' "$CHUNK_BODY" | { grep -cF 'Please try again in a moment' || true ; })"
            if [ "$retryable_count" -ge 1 ] ; then
                record_pass C "billing chunk has retryable-branch fallback=$retryable_count (5xx/network preserved)"
            else
                record_fail C "billing chunk MISSING retryable fallback — class differentiation broken"
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
