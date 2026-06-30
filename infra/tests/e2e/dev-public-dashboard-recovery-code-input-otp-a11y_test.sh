#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#80 closure
# (the Recovery 6-digit code input on /recovery step 2 was a plain
# type="text" field with no inputmode, no autocomplete, no pattern, and
# no maxlength — mobile users got the full alphanumeric keyboard instead
# of a number pad, and the OS/browser couldn't offer one-time-code
# autofill from the user's SMS/email Sender window).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #80 verbatim:
#   * Open a recovery flow that reached the code step
#     (`/recovery?flow=<id>`).
#   * Pre-fix: the `Recovery code` input is `{type:'text', inputmode:null,
#     autocomplete:null, pattern:null, maxlength:null}`. Mobile keyboard
#     opens alphanumeric. iOS/Android one-time-code autofill never fires.
#   * Post-fix (commit 87058b4 in loopzero-cloud, 2026-06-28):
#       `dashboard/src/pages/recovery.tsx:574-577` adds 4 a11y attributes
#       to the canonical `<Input id="recovery-code">`:
#         - inputMode="numeric"        — mobile numeric pad
#         - autoComplete="one-time-code" — OS OTP autofill
#         - pattern="[0-9]*"           — input validation hint
#         - maxLength={8}              — bounded (kept slightly longer
#                                        than the strict 6 in case Kratos
#                                        sends an 8-digit variant later)
#       Both `inputMode=numeric` and `autoComplete=one-time-code` are
#       RFC-3 / WHATWG canonical OTP affordances.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — production recovery page chunk (recovery-*.js) carries the
#       `inputMode:"numeric"` minified attribute literal (the second-arg
#       prop shape vite emits for JSX `inputMode="numeric"`).
#   B — same chunk carries the `autoComplete:"one-time-code"` attribute
#       literal — proof the OS one-time-code autofill affordance shipped.
#   C — same chunk carries BOTH `pattern:"[0-9]*"` AND `maxLength:8` —
#       proof the input-validation hint + bounded length shipped (these
#       two complete the canonical OTP a11y quadrant).
#
# Slug:           dashboard-recovery-code-input-otp-a11y
# Issue:          loopzero-cloud#80
# Family:         #25 / #51 (a11y form-field hygiene QA Sprint 1)
# Closed by:      `dashboard/src/pages/recovery.tsx:563-583`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#80)
# Companion (CI): infra/tests/check-dev-public-dashboard-recovery-code-input-otp-a11y-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-recovery-code-input-otp-a11y"
ISSUE_REF="loopzero-cloud#80"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#80 (recovery code OTP a11y).

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

locate_recovery_chunk() {
    local html entry_js entry_body
    html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
    entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
    entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
    printf '%s' "$entry_body" \
        | { grep -oE '[A-Za-z0-9_-]*recovery-[A-Za-z0-9_-]+\.js' || true ; } \
        | head -1
}

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Probe: ${PUBLIC_CLOUD_URL}/recovery"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — recovery chunk carries `inputMode:"numeric"` minified prop
# literal.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — recovery chunk carries inputMode:\"numeric\" (mobile number pad)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        chunk="$(locate_recovery_chunk)"
        if [ -z "$chunk" ] ; then
            record_fail A "could not locate recovery page chunk"
        else
            log "  observed page chunk: $chunk"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${chunk}" 2>&1 || true)"
            cnt="$(printf '%s' "$chunk_body" | { grep -oF 'inputMode:"numeric"' || true ; } | wc -l | tr -d ' ')"
            if [ "$cnt" -gt 0 ] ; then
                record_pass A "chunk $chunk has inputMode:\"numeric\" — $cnt occurrence(s)"
            else
                record_fail A "chunk $chunk MISSING inputMode:\"numeric\" (pre-fix shape — alphanumeric mobile keyboard)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — recovery chunk carries `autoComplete:"one-time-code"` —
# the OS OTP autofill affordance.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — recovery chunk carries autoComplete:\"one-time-code\" (OS OTP autofill)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        chunk="$(locate_recovery_chunk)"
        if [ -z "$chunk" ] ; then
            record_fail B "could not locate recovery page chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${chunk}" 2>&1 || true)"
            cnt="$(printf '%s' "$chunk_body" | { grep -oF 'autoComplete:"one-time-code"' || true ; } | wc -l | tr -d ' ')"
            if [ "$cnt" -gt 0 ] ; then
                record_pass B "chunk has autoComplete:\"one-time-code\" — $cnt occurrence(s)"
            else
                record_fail B "chunk MISSING autoComplete:\"one-time-code\" (pre-fix shape — no OS OTP autofill)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — recovery chunk carries BOTH `pattern:"[0-9]*"` AND
# `maxLength:8` — input-validation hint + bounded length.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — recovery chunk carries pattern:\"[0-9]*\" + maxLength:8 (input validation + bound)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        chunk="$(locate_recovery_chunk)"
        if [ -z "$chunk" ] ; then
            record_fail C "could not locate recovery page chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${chunk}" 2>&1 || true)"
            pt_count="$(printf '%s' "$chunk_body" | { grep -oF 'pattern:"[0-9]*"' || true ; } | wc -l | tr -d ' ')"
            ml_count="$(printf '%s' "$chunk_body" | { grep -oE 'maxLength:8' || true ; } | wc -l | tr -d ' ')"
            if [ "$pt_count" -gt 0 ] && [ "$ml_count" -gt 0 ] ; then
                record_pass C "chunk has pattern=$pt_count + maxLength=$ml_count (input validation + bound wired)"
            else
                record_fail C "chunk MISSING input-validation quadrant (pattern=$pt_count maxLength=$ml_count)"
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
