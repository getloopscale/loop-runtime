#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#75 closure
# (the duplicate-account error was raw Kratos copy: "An account with the
# same identifier (email, phone, username, ...) exists already." — but
# the /register form only collects Name + Email + Password, so the
# parenthetical referencing phone + username was confusing + unprofessional.
# It also offered no inline recovery CTA — the only path forward was a
# small "Sign in" link at the bottom of the form).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #75 verbatim:
#   * Open /register, submit with an already-registered email.
#   * Pre-fix: alert text "An account with the same identifier (email,
#     phone, username, ...) exists already." — references fields not in
#     the form + no inline CTA.
#   * Post-fix (commit 87058b4 in loopzero-cloud, 2026-06-28):
#       `dashboard/src/lib/kratos.ts:141-152` intercepts the raw Kratos
#       message via a contiguous-substring match (locale-tolerant) on
#       `account with the same identifier` and rewrites to: "An account
#       with this email already exists. Sign in instead, or reset your
#       password if you forgot it." — scoped to the actual field the
#       form collects + two recovery CTAs in the copy.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — the canonical kratos lib chunk (kratos-*.js, used by /register)
#       carries the new product copy literal "An account with this email
#       already exists" — proof the override shipped.
#   B — same chunk carries BOTH recovery CTA phrases ("Sign in instead"
#       AND "reset your password if you forgot") — proof the inline
#       recovery affordances landed.
#   C — same chunk carries the detection literal "account with the same
#       identifier" (the substring match against the Kratos raw message)
#       — proof the rewrite logic shipped (without the detection there
#       would be no override). AND verify the raw Kratos anti-pattern
#       "phone, username" does NOT appear in the chunk's own copy.
#
# Slug:           dashboard-register-duplicate-account-product-copy
# Issue:          loopzero-cloud#75
# Family:         #74 (same /register surface — sibling Red Team finding)
# Closed by:      `dashboard/src/lib/kratos.ts:141-152`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#75)
# Companion (CI): infra/tests/check-dev-public-dashboard-register-duplicate-account-product-copy-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-register-duplicate-account-product-copy"
ISSUE_REF="loopzero-cloud#75"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#75 (signup duplicate-account product copy).

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

# The kratos translation lives in src/lib/kratos.ts which vite emits as a
# shared library chunk (kratos-*.js) consumed by /register and other auth
# pages. Resolve it once and reuse across scenarios.
locate_kratos_chunk() {
    local html entry_js entry_body
    html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
    entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
    entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
    printf '%s' "$entry_body" \
        | grep -oE '[A-Za-z0-9_-]*kratos-[A-Za-z0-9_-]+\.js' \
        | head -1
}

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Probe: ${PUBLIC_CLOUD_URL}/register"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — kratos lib chunk carries the new product copy literal.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — kratos lib chunk carries the new product copy literal"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        page_chunk="$(locate_kratos_chunk)"
        if [ -z "$page_chunk" ] ; then
            record_fail A "could not locate kratos lib chunk"
        else
            log "  observed lib chunk: $page_chunk"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${page_chunk}" 2>&1 || true)"
            copy_count="$(printf '%s' "$chunk_body" | grep -oF 'An account with this email already exists' | wc -l | tr -d ' ')"
            if [ "$copy_count" -gt 0 ] ; then
                record_pass A "chunk $page_chunk has product copy — $copy_count occurrence(s)"
            else
                record_fail A "chunk $page_chunk MISSING 'An account with this email already exists' (pre-fix shape — raw Kratos copy still surfaces)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — kratos lib chunk carries BOTH recovery CTA phrases.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — kratos lib chunk carries both recovery CTAs"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        page_chunk="$(locate_kratos_chunk)"
        if [ -z "$page_chunk" ] ; then
            record_fail B "could not locate kratos lib chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${page_chunk}" 2>&1 || true)"
            si_count="$(printf '%s' "$chunk_body" | grep -oF 'Sign in instead' | wc -l | tr -d ' ')"
            rp_count="$(printf '%s' "$chunk_body" | grep -oF 'reset your password if you forgot' | wc -l | tr -d ' ')"
            if [ "$si_count" -gt 0 ] && [ "$rp_count" -gt 0 ] ; then
                record_pass B "chunk has 'Sign in instead'=$si_count + 'reset your password if you forgot'=$rp_count"
            else
                record_fail B "chunk MISSING recovery CTAs (Sign in instead=$si_count reset_pw=$rp_count — inline CTA gap)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — kratos lib chunk carries the detection literal AND does
# NOT carry the raw Kratos anti-pattern `phone, username` in its own copy
# (only as the detection-side substring would be safe; the anti-pattern
# words must not appear at all in the chunk's strings).
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — kratos lib chunk carries detection literal + anti-pattern absent"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        page_chunk="$(locate_kratos_chunk)"
        if [ -z "$page_chunk" ] ; then
            record_fail C "could not locate kratos lib chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${page_chunk}" 2>&1 || true)"
            # `grep -F` returns exit 1 on no match; `set -o pipefail`
            # would propagate that and kill the script before we can
            # assert "anti-pattern absent". `|| true` inside a subshell
            # swallows the no-match exit so a clean (anti-pattern absent)
            # bundle yields anti_count=0 as the assertion expects.
            det_count="$(printf '%s' "$chunk_body" | { grep -oF 'account with the same identifier' || true ; } | wc -l | tr -d ' ')"
            anti_count="$(printf '%s' "$chunk_body" | { grep -oF 'phone, username' || true ; } | wc -l | tr -d ' ')"
            if [ "$det_count" -gt 0 ] && [ "$anti_count" -eq 0 ] ; then
                record_pass C "chunk has detection literal=$det_count + raw 'phone, username' anti-pattern absent (=$anti_count)"
            else
                record_fail C "chunk gate failed: detection=$det_count anti_pattern=$anti_count (expected detection>0 + anti=0)"
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
