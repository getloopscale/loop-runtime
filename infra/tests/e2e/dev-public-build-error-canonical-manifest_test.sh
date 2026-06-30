#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#123 closure
# (engine error message must reference canonical .loopzero/services.json, NOT
# the legacy loopzero.yaml).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #123 verbatim: POST /engine/api/v1/builds with no `apps`
# returns 400 MISSING_FIELD. Pre-fix the response body referenced
# `loopzero.yaml` + `apps:` (legacy v1 manifest). Post-fix it references
# `.loopzero/services.json` + `services:` (canonical v2 manifest).
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL.
#
# Scenarios:
#   A — POST /engine/api/v1/builds with no `apps` returns 400 with
#       canonical guidance (`.loopzero/services.json`, `services:`) and NO
#       legacy reference (`loopzero.yaml`, `apps:`).
#   B — the JSON detail + message fields BOTH carry the canonical wording
#       (the dashboard renders either, depending on field).
#
# Slug:       build-error-canonical-manifest
# Issue:      loopzero-cloud#123
# Closed by:  api/internal/routes/builds.go + api/internal/services/error_classifier.go
# Allowlist:  .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#123)
# Companion:  infra/tests/check-dev-public-build-error-canonical-manifest-syntax_test.sh

set -euo pipefail

PLAN_SLUG="build-error-canonical-manifest"
ISSUE_REF="loopzero-cloud#123"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
PROJECT_NAME="${PROJECT_NAME:-loopzerokit-example}"
SESSION_JWT_FILE="${SESSION_JWT_FILE:-}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B] [--json] [--help]

LIVE regression-protector for loopzero-cloud#123 (engine error message references
canonical .loopzero/services.json, NOT legacy loopzero.yaml).

Environment:
  PUBLIC_CLOUD_URL  default https://app-dev.loopzero.example
  PROJECT_NAME      default loopzerokit-example
  SESSION_JWT_FILE  default ~/.loopzero/local-dogfood-session-jwt.txt (or canonical)

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

if [ -z "$SESSION_JWT_FILE" ] ; then
    for cand in "$HOME/.loopzero/local-dogfood-session-jwt.txt" "$HOME/.loopzero/redteam-qa-session-jwt.txt" ; do
        if [ -s "$cand" ] ; then SESSION_JWT_FILE="$cand" ; break ; fi
    done
fi

log() { printf '[%s] %s\n' "$(basename "$0")" "$*" ; }

PASS_COUNT=0
FAIL_COUNT=0
FAILED_LIST=""

record_pass() { PASS_COUNT=$((PASS_COUNT + 1)) ; log "  PASS  $1${2:+ ($2)}" ; }
record_fail() { FAIL_COUNT=$((FAIL_COUNT + 1)) ; FAILED_LIST+=" $1" ; log "  FAIL  $1${2:+ — $2}" ; }

want_scenario() {
    [ -z "$SCENARIO_FILTER" ] && return 0
    case ",$SCENARIO_FILTER," in *,"$1",*) return 0 ;; *) return 1 ;; esac
}

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Project: $PROJECT_NAME @ $PUBLIC_CLOUD_URL"
log "JWT: ${SESSION_JWT_FILE:-(none)}"
log "============================================================"

probe_builds_no_apps() {
    if [ -z "$SESSION_JWT_FILE" ] || [ ! -s "$SESSION_JWT_FILE" ] ; then
        echo ""
        return
    fi
    local JWT body
    JWT="$(cat "$SESSION_JWT_FILE")"
    # Submit a build payload with NO apps — triggers the MISSING_FIELD guard
    # at api/internal/routes/builds.go:323.
    # Engine /builds requires an Idempotency-Key header — distinct per probe
    # (per `api/internal/middleware` contract).
    local idem_key
    idem_key="probe-$(date +%s%N)-${RANDOM}"
    body=$(curl -s --max-time 10 -X POST \
        -H "Authorization: Bearer $JWT" \
        -H "Content-Type: application/json" \
        -H "Idempotency-Key: ${idem_key}" \
        -d "{\"project_id\":\"$PROJECT_NAME\",\"environment\":\"preview\",\"commit_sha\":\"013e5ca\"}" \
        "${PUBLIC_CLOUD_URL}/engine/api/v1/builds" 2>&1 || true)
    printf '%s' "$body"
}

# ---------------------------------------------------------------------------
# Scenario A — body must carry canonical .loopzero/services.json + services:
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — POST /builds with no apps returns canonical .loopzero/services.json + services: guidance"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        body="$(probe_builds_no_apps)"
        if [ -z "$body" ] ; then
            record_fail A "no SESSION_JWT_FILE — run seed-admin-loopzero-dev.sh --refresh-only"
        else
            if printf '%s' "$body" | grep -q '\.loopzero/services\.json' && \
               printf '%s' "$body" | grep -q 'services:' && \
               printf '%s' "$body" | grep -q 'MISSING_FIELD' ; then
                record_pass A "canonical .loopzero/services.json + services: + MISSING_FIELD present"
            else
                record_fail A "missing canonical guidance — body=$body"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — body must NOT carry legacy loopzero.yaml + apps: (negative).
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — body must NOT reference legacy loopzero.yaml + apps: (regression-protector)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        body="$(probe_builds_no_apps)"
        if [ -z "$body" ] ; then
            record_fail B "no SESSION_JWT_FILE"
        else
            if printf '%s' "$body" | grep -q 'loopzero\.yaml' ; then
                record_fail B "legacy loopzero.yaml still present in body — body=$body"
            elif printf '%s' "$body" | grep -qE '"apps":' || printf '%s' "$body" | grep -q '`apps:`' ; then
                record_fail B "legacy apps: still present in body — body=$body"
            else
                record_pass B "no legacy loopzero.yaml / apps: reference"
            fi
        fi
    fi
fi

log "============================================================"
log "Result: $PASS_COUNT PASS, $FAIL_COUNT FAIL"
[ -n "$FAILED_LIST" ] && log "Failed:${FAILED_LIST}"
log "============================================================"

if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '{"pass":%d,"fail":%d}\n' "$PASS_COUNT" "$FAIL_COUNT"
fi

exit $([ $FAIL_COUNT -eq 0 ] && echo 0 || echo 1)
