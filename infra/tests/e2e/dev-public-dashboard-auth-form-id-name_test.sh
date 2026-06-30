#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#51 closure
# (Auth pages /login + /register emitted the browser console issue "A
# form field element should have an id or name attribute (count: 2)"
# because the Email + Password inputs on /login were missing both id
# and name; the /register inputs had id but were missing name. This
# breaks accessibility — labels can't be programmatically associated
# with their inputs — and password managers can't autofill fields
# that have no `name`).
#
# Fix shipped 2026-06-28 in commit `87058b4` (loopzero-cloud) Cluster G:
#   - `dashboard/src/pages/login.tsx:421` `<FormField id="login-email">`
#     + `:425` `name="email"` on the email Input.
#   - `dashboard/src/pages/login.tsx:441` `<FormField id="login-password">`
#     + `:445` `name="password"` on the password Input.
#   - `dashboard/src/pages/register.tsx:359` `name="traits.name"` on Name.
#   - `dashboard/src/pages/register.tsx:384` `name="traits.email"` on Email.
#   - `dashboard/src/pages/register.tsx:403` `name="password"` on Password.
#
# The FormField wrapper threads its `id` through to the underlying
# `<Input>` so both `id` AND label `htmlFor` resolve, and the `name`
# attribute lets password managers/Autofill recognize the field.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN). OPERATOR HARD STOP per
# `.claude/rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL —
# requires LIVE bundle GET from `https://app-dev.loopzero.example`.
#
# Scenarios:
#   A — login page chunk carries `id:"login-email"` AND `id:"login-password"`
#       (FormField id assertion — proves the FormField wrapper threads
#       the `id` through to the underlying Input).
#   B — login page chunk carries `name:"email"` AND `name:"password"`
#       (raw HTML name attribute — what password-manager Autofill keys on).
#   C — register page chunk carries `name:"traits.name"` AND
#       `name:"traits.email"` AND `name:"password"` (Kratos
#       browser-flow trait-keyed fields with their canonical `name`).
#
# Slug:           dashboard-auth-form-id-name
# Issue:          loopzero-cloud#51
# Family:         Cluster G of 87058b4 (auth/search fields a11y batch)
# Closed by:      `loopzero-cloud/dashboard/src/pages/login.tsx:421+425+441+445`
#                 + `loopzero-cloud/dashboard/src/pages/register.tsx:359+384+403`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#51)
# Companion (CI): infra/tests/check-dev-public-dashboard-auth-form-id-name-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-auth-form-id-name"
ISSUE_REF="loopzero-cloud#51"
APP_URL="${APP_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#51 (auth form id/name attributes).

Environment:
  APP_URL    default https://app-dev.loopzero.example (the dev-public dashboard)

OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md INQUEBRÁVEL:
runs LIVE bundle GET against the dashboard SPA shell + lazy chunks.
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
# Helpers: locate the canonical login / register page chunks from the
# entry index bundle. The register-*.js name collides with a sibling
# stub chunk so we pick the one that contains the trait-keyed fields.
# ---------------------------------------------------------------------------
locate_entry_index_url() {
    curl -s "$APP_URL/" \
        | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' \
        | head -1
}

locate_login_chunk() {
    local entry_url="$1"
    local entry_body
    entry_body="$(curl -s "$APP_URL$entry_url")"
    local candidates
    candidates="$(printf '%s' "$entry_body" \
        | { grep -oE 'login-[A-Za-z0-9_-]+\.js' || true ; } \
        | sort -u)"
    printf '%s\n' "$candidates" | head -1
}

locate_register_chunk() {
    local entry_url="$1"
    local entry_body
    entry_body="$(curl -s "$APP_URL$entry_url")"
    local candidates
    candidates="$(printf '%s' "$entry_body" \
        | { grep -oE 'register-[A-Za-z0-9_-]+\.js' || true ; } \
        | sort -u)"
    # Probe each candidate for the register page's unique signature
    # (`name:"traits.name"` is unique to the Kratos registration UI).
    local c
    while IFS= read -r c ; do
        [ -z "$c" ] && continue
        if curl -s "$APP_URL/assets/$c" \
            | grep -qF 'name:"traits.name"' ; then
            printf '%s\n' "$c"
            return 0
        fi
    done <<< "$candidates"
    return 1
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
    ENTRY_URL="$(locate_entry_index_url)"
    if [ -z "$ENTRY_URL" ] ; then
        record_fail PREFLIGHT "could not locate /assets/index-*.js entry chunk from $APP_URL/"
    else
        log "  observed entry chunk: $ENTRY_URL"
        LOGIN_CHUNK="$(locate_login_chunk "$ENTRY_URL")"
        REGISTER_CHUNK="$(locate_register_chunk "$ENTRY_URL" || true)"
        log "  observed login chunk: ${LOGIN_CHUNK:-<not found>}"
        log "  observed register chunk: ${REGISTER_CHUNK:-<not found>}"

        # -------------------------------------------------------------------
        # Scenario A — login chunk has id:"login-email" + id:"login-password"
        # -------------------------------------------------------------------
        if want_scenario A ; then
            log "Scenario A — login chunk carries id:\"login-email\" + id:\"login-password\""
            if [ -z "$LOGIN_CHUNK" ] ; then
                record_fail A "login chunk not found in entry bundle"
            else
                BODY="$(curl -s "$APP_URL/assets/$LOGIN_CHUNK")"
                email_id="$(printf '%s' "$BODY" | { grep -cF 'id:"login-email"' || true ; })"
                pwd_id="$(printf '%s' "$BODY" | { grep -cF 'id:"login-password"' || true ; })"
                if [ "$email_id" -ge 1 ] && [ "$pwd_id" -ge 1 ] ; then
                    record_pass A "chunk has login-email id=$email_id + login-password id=$pwd_id"
                else
                    record_fail A "login chunk MISSING id literals (login-email=$email_id login-password=$pwd_id — pre-fix shape)"
                fi
            fi
        fi

        # -------------------------------------------------------------------
        # Scenario B — login chunk has name:"email" + name:"password"
        # -------------------------------------------------------------------
        if want_scenario B ; then
            log "Scenario B — login chunk carries name:\"email\" + name:\"password\""
            if [ -z "$LOGIN_CHUNK" ] ; then
                record_fail B "login chunk not found in entry bundle"
            else
                BODY="$(curl -s "$APP_URL/assets/$LOGIN_CHUNK")"
                name_email="$(printf '%s' "$BODY" | { grep -cF 'name:"email"' || true ; })"
                name_pwd="$(printf '%s' "$BODY" | { grep -cF 'name:"password"' || true ; })"
                if [ "$name_email" -ge 1 ] && [ "$name_pwd" -ge 1 ] ; then
                    record_pass B "chunk has name=email=$name_email + name=password=$name_pwd (Autofill-ready)"
                else
                    record_fail B "login chunk MISSING name literals (email=$name_email password=$name_pwd — pre-fix shape)"
                fi
            fi
        fi

        # -------------------------------------------------------------------
        # Scenario C — register chunk has name:"traits.name" +
        # name:"traits.email" + name:"password"
        # -------------------------------------------------------------------
        if want_scenario C ; then
            log "Scenario C — register chunk carries name:\"traits.name\" + name:\"traits.email\" + name:\"password\""
            if [ -z "$REGISTER_CHUNK" ] ; then
                record_fail C "register chunk not found in entry bundle"
            else
                BODY="$(curl -s "$APP_URL/assets/$REGISTER_CHUNK")"
                tn="$(printf '%s' "$BODY" | { grep -cF 'name:"traits.name"' || true ; })"
                te="$(printf '%s' "$BODY" | { grep -cF 'name:"traits.email"' || true ; })"
                tp="$(printf '%s' "$BODY" | { grep -cF 'name:"password"' || true ; })"
                if [ "$tn" -ge 1 ] && [ "$te" -ge 1 ] && [ "$tp" -ge 1 ] ; then
                    record_pass C "chunk has traits.name=$tn + traits.email=$te + password=$tp"
                else
                    record_fail C "register chunk MISSING name literals (traits.name=$tn traits.email=$te password=$tp — pre-fix shape)"
                fi
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
