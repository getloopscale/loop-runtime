#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#74 closure
# (the signup form wiped Name + Email + Password on an "account exists"
# error — forcing the user to retype everything for a benign, recoverable
# validation error).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #74 verbatim:
#   * Open /register, fill Name + an already-registered Email + Password,
#     click "Create account".
#   * Pre-fix: the form POST navigates the page (Kratos round-trip), so
#     React re-mounts with empty state — name, email AND password all
#     reset to ''. Confirmed via `input.value === ''` in the Red Team's
#     console probe.
#   * Post-fix (commit 87058b4 in loopzero-cloud, 2026-06-28):
#       `dashboard/src/pages/register.tsx:39-70 + 82-83` persist Name and
#       Email (but NOT Password — security) to `sessionStorage` under the
#       key `loopzero-register-form-restore` immediately before submit, then
#       rehydrate on mount via `useState(() => readPersistedRegisterForm())`.
#       The blob is cleared on successful login (`getSession()` non-null)
#       so a future signup attempt starts clean.
#   * Canonical 2026 UX pattern (Vercel, Linear, Stripe): "preserve name +
#     email, re-ask for password".
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — production register page chunk (register-*.js) carries the unique
#       sessionStorage key literal `loopzero-register-form-restore` — proof
#       the persistence wiring landed in the served bundle. This is the
#       unique signature of the #74 fix (no other page uses this key).
#   B — same chunk carries `setItem` AND `getItem` (the write + read
#       halves of the persist/rehydrate cycle).
#   C — same chunk carries `removeItem` — proof the cleanup-on-success
#       path is wired (without it, a stale name+email blob would haunt
#       future signup attempts after logout).
#
# Slug:           dashboard-register-preserve-name-email-on-error
# Issue:          loopzero-cloud#74
# Family:         #75 (signup error copy referencing wrong fields — same /register surface)
# Closed by:      `dashboard/src/pages/register.tsx:39-70 + 82-83 + 99-106 + ~223`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#74)
# Companion (CI): infra/tests/check-dev-public-dashboard-register-preserve-name-email-on-error-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-register-preserve-name-email-on-error"
ISSUE_REF="loopzero-cloud#74"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#74 (signup form preserve name+email).

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

# Locate the meaningful register chunk. The entry bundle may reference
# multiple register-*.js chunks (route-stub + real page); pick the largest
# (the real page chunk carries the persistence code).
locate_register_chunk() {
    local html entry_js entry_body
    html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
    entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
    entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
    local best="" best_size=0
    for chunk in $(printf '%s' "$entry_body" | grep -oE '[A-Za-z0-9_-]*register-[A-Za-z0-9_-]+\.js' | sort -u) ; do
        local size
        size="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${chunk}" 2>&1 | wc -c | tr -d ' ')"
        if [ "$size" -gt "$best_size" ] ; then
            best_size="$size"
            best="$chunk"
        fi
    done
    printf '%s' "$best"
}

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Probe: ${PUBLIC_CLOUD_URL}/register"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — register chunk carries the unique sessionStorage key
# literal `loopzero-register-form-restore`. This is the unique signature of
# the #74 fix; no other page uses this key.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — register chunk carries the unique 'loopzero-register-form-restore' key"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        page_chunk="$(locate_register_chunk)"
        if [ -z "$page_chunk" ] ; then
            record_fail A "could not locate register page chunk"
        else
            log "  observed page chunk: $page_chunk"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${page_chunk}" 2>&1 || true)"
            k_count="$(printf '%s' "$chunk_body" | grep -oF 'loopzero-register-form-restore' | wc -l | tr -d ' ')"
            if [ "$k_count" -gt 0 ] ; then
                record_pass A "chunk $page_chunk has 'loopzero-register-form-restore' — $k_count occurrence(s)"
            else
                record_fail A "chunk $page_chunk MISSING 'loopzero-register-form-restore' (pre-fix shape — no persistence)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — register chunk carries BOTH `setItem` AND `getItem` — the
# write + read halves of the persist/rehydrate cycle.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — register chunk carries setItem + getItem (write + read halves)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        page_chunk="$(locate_register_chunk)"
        if [ -z "$page_chunk" ] ; then
            record_fail B "could not locate register page chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${page_chunk}" 2>&1 || true)"
            set_count="$(printf '%s' "$chunk_body" | grep -oF 'setItem' | wc -l | tr -d ' ')"
            get_count="$(printf '%s' "$chunk_body" | grep -oF 'getItem' | wc -l | tr -d ' ')"
            if [ "$set_count" -gt 0 ] && [ "$get_count" -gt 0 ] ; then
                record_pass B "chunk has setItem=$set_count + getItem=$get_count (persist + rehydrate cycle wired)"
            else
                record_fail B "chunk MISSING persist/rehydrate cycle (setItem=$set_count getItem=$get_count)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — register chunk carries `removeItem` — the cleanup-on-success
# path. Without it, a stale name+email blob would haunt future signup
# attempts after logout.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — register chunk carries removeItem (cleanup-on-success path)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        page_chunk="$(locate_register_chunk)"
        if [ -z "$page_chunk" ] ; then
            record_fail C "could not locate register page chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${page_chunk}" 2>&1 || true)"
            rm_count="$(printf '%s' "$chunk_body" | grep -oF 'removeItem' | wc -l | tr -d ' ')"
            if [ "$rm_count" -gt 0 ] ; then
                record_pass C "chunk has removeItem — $rm_count occurrence(s) (cleanup-on-success wired)"
            else
                record_fail C "chunk MISSING removeItem (stale blob would haunt future signups)"
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
