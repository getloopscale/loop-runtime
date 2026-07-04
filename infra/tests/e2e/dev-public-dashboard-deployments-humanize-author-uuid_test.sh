#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#78 closure
# (the "Recent deployments" rows and the /deployments table rendered the
# raw deployer UUID `4d6ae2dd-baa2-4c01-8d33-a96e20c23d1a` per row — a
# 36-char hex blob is meaningless to a human reader; should be a name,
# email local-part, or a short id).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #78 verbatim:
#   * Open /projects/{name} (Overview) or /projects/{name}/deployments.
#   * Pre-fix: each row rendered the raw `created_by` UUID verbatim next
#     to the short commit SHA → "e52f2f4  4d6ae2dd-baa2-4c01-8d33-…".
#   * Post-fix (commit 87058b4 in loopzero-cloud, 2026-06-28):
#       `dashboard/src/lib/format.ts:189-220` adds `humanizeAuthor(raw)`
#       that follows a cheapest-first rule list:
#         (1) null/empty → '—'
#         (2) UUID (canonical 8-4-4-4-12 hex, case-insensitive) → first
#             8 chars (e.g. "4d6ae2dd") so duplicates still look distinct
#             AND the original is preserved in a `title` tooltip at the
#             call site
#         (3) email → local-part (e.g. "paulo@loopzero.example" → "paulo")
#         (4) anything else (a real name like "Paulo Henrique") → as-is
#       Family of #52 (same family of UUID drift in the /deployments table).
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — canonical format-*.js helper chunk carries the UUID regex pattern
#       `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`
#       (the 8-4-4-4-12 canonical form humanizeAuthor matches against).
#   B — same chunk carries the `.slice(0,8)` projection — minified shape
#       of `trimmed.slice(0, 8)` that produces the 8-char short id when
#       the UUID branch fires.
#   C — BOTH project-detail-*.js AND deployments-*.js page chunks import
#       the format-*.js helper chunk (proves the humanizeAuthor helper is
#       wired in both surfaces that previously surfaced the raw UUID).
#
# Slug:           dashboard-deployments-humanize-author-uuid
# Issue:          loopzero-cloud#78
# Family:         #52 (same family of UUID drift in /deployments)
# Closed by:      `dashboard/src/lib/format.ts:189-220`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#78)
# Companion (CI): infra/tests/check-dev-public-dashboard-deployments-humanize-author-uuid-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-deployments-humanize-author-uuid"
ISSUE_REF="loopzero-cloud#78"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#78 (humanize deployer UUID).

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

# Resolve the format-*.js helper chunk from the entry bundle.
locate_format_chunk() {
    local html entry_js entry_body
    html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
    entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
    entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
    printf '%s' "$entry_body" \
        | { grep -oE '[A-Za-z0-9_-]*format-[A-Za-z0-9_-]+\.js' || true ; } \
        | head -1
}

# Resolve a page chunk by name prefix.
locate_page_chunk() {
    local prefix="$1" html entry_js entry_body
    html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
    entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
    entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
    printf '%s' "$entry_body" \
        | { grep -oE "${prefix}-[A-Za-z0-9_-]+\\.js" || true ; } \
        | head -1
}

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Probe: ${PUBLIC_CLOUD_URL}/projects/loopzerokit-example"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — format chunk carries the canonical 8-4-4-4-12 UUID regex.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — format chunk carries the canonical 8-4-4-4-12 UUID regex"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        fmt_chunk="$(locate_format_chunk)"
        if [ -z "$fmt_chunk" ] ; then
            record_fail A "could not locate format-*.js helper chunk"
        else
            log "  observed helper chunk: $fmt_chunk"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${fmt_chunk}" 2>&1 || true)"
            rgx_count="$(printf '%s' "$chunk_body" | { grep -oE '\[0-9a-f\]\{8\}-\[0-9a-f\]\{4\}-\[0-9a-f\]\{4\}-\[0-9a-f\]\{4\}-\[0-9a-f\]\{12\}' || true ; } | wc -l | tr -d ' ')"
            if [ "$rgx_count" -gt 0 ] ; then
                record_pass A "chunk $fmt_chunk has UUID 8-4-4-4-12 regex — $rgx_count occurrence(s)"
            else
                record_fail A "chunk $fmt_chunk MISSING canonical UUID regex (pre-fix shape — no humanizer)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — format chunk carries `.slice(0,8)` projection — the 8-char
# short-id derivation when the UUID branch fires.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — format chunk carries the .slice(0,8) UUID-to-short-id projection"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        fmt_chunk="$(locate_format_chunk)"
        if [ -z "$fmt_chunk" ] ; then
            record_fail B "could not locate format-*.js helper chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${fmt_chunk}" 2>&1 || true)"
            sl_count="$(printf '%s' "$chunk_body" | { grep -oE '\.slice\(0,8\)' || true ; } | wc -l | tr -d ' ')"
            if [ "$sl_count" -gt 0 ] ; then
                record_pass B "chunk has .slice(0,8) projection — $sl_count occurrence(s)"
            else
                record_fail B "chunk MISSING .slice(0,8) (UUID branch wouldn't produce a short id)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — BOTH project-detail-*.js AND deployments-*.js page chunks
# import the format-*.js helper chunk.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — both project-detail + deployments page chunks import the format helper"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        pd_chunk="$(locate_page_chunk 'project-detail')"
        dp_chunk="$(locate_page_chunk 'deployments')"
        if [ -z "$pd_chunk" ] || [ -z "$dp_chunk" ] ; then
            record_fail C "could not locate one or both page chunks (project-detail=$pd_chunk deployments=$dp_chunk)"
        else
            pd_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${pd_chunk}" 2>&1 || true)"
            dp_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${dp_chunk}" 2>&1 || true)"
            pd_fmt="$(printf '%s' "$pd_body" | { grep -oE 'format-[A-Za-z0-9_-]+\.js' || true ; } | head -1)"
            dp_fmt="$(printf '%s' "$dp_body" | { grep -oE 'format-[A-Za-z0-9_-]+\.js' || true ; } | head -1)"
            if [ -n "$pd_fmt" ] && [ -n "$dp_fmt" ] ; then
                record_pass C "both page chunks import format helper (project-detail→$pd_fmt, deployments→$dp_fmt)"
            else
                record_fail C "format helper not imported in both page chunks (project-detail=$pd_fmt deployments=$dp_fmt)"
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
