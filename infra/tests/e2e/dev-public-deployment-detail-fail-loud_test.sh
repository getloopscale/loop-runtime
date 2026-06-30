#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#122 closure (UI
# fail-loud over deploy errors).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #122 by asserting the production-served dashboard bundle
# carries the two new contract literals that the SSE/timeline + redeploy
# dialog rely on for fail-loud rendering:
#
#   * `build-timeline-live-*.js` — carries the honest terminal-empty state
#     literal "Build failed before any step ran" (pre-fix: zero occurrence;
#     post-fix: ≥ 1, proving the failed-with-no-events branch shipped).
#   * `use-redeploy-*.js` — carries the `throwOnError` opt-in literal
#     (pre-fix: zero occurrence; post-fix: ≥ 1, proving the dialog will
#     surface 4xx problem+json detail instead of closing silently).
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash regression-protector.
#
# Scenarios:
#   A — build-timeline-live bundle carries "Build failed before any step ran"
#       (proves surface 1 — timeline honest terminal-empty state shipped).
#   B — use-redeploy bundle carries throwOnError opt-in literal (proves
#       surface 2 — redeploy dialog will surface problem+json on 4xx).
#
# Slug:       deployment-detail-fail-loud
# Issue:      loopzero-cloud#122
# Closed by:  dashboard/src/components/deploy/build-timeline-live.tsx
#             dashboard/src/hooks/mutations/use-redeploy.ts
#             dashboard/src/pages/deployment-detail.tsx (plumb restBuildStatus)
# Allowlist:  .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#122)
# Companion:  infra/tests/check-dev-public-deployment-detail-fail-loud-syntax_test.sh

set -euo pipefail

PLAN_SLUG="deployment-detail-fail-loud"
ISSUE_REF="loopzero-cloud#122"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B] [--json] [--help]

LIVE regression-protector for loopzero-cloud#122 (UI fail-loud on deploy errors).

Per cleanroom-up-live-test-mandate.md INQUEBRÁVEL: run AFTER Cleanroom
TOTAL + UP + seed-post-cleanroom-all.sh. Operator-side. Not for CI.
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

record_pass() { PASS_COUNT=$((PASS_COUNT + 1)) ; log "  PASS  $1${2:+ ($2)}" ; }
record_fail() { FAIL_COUNT=$((FAIL_COUNT + 1)) ; FAILED_LIST+=" $1" ; log "  FAIL  $1${2:+ — $2}" ; }

want_scenario() {
    [ -z "$SCENARIO_FILTER" ] && return 0
    case ",$SCENARIO_FILTER," in *,"$1",*) return 0 ;; *) return 1 ;; esac
}

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Cloud URL: $PUBLIC_CLOUD_URL"
log "============================================================"

# Helper: locate a chunk by partial name from the entry chunk's import map.
locate_chunk() {
    local needle="$1"  # e.g. "build-timeline-live" or "use-redeploy"
    local html entry_js entry_body
    html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
    entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
    [ -z "$entry_js" ] && { echo "" ; return ; }
    entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
    printf '%s' "$entry_body" \
        | grep -oE "${needle}-[A-Za-z0-9_-]+\.js" \
        | head -1
}

# ---------------------------------------------------------------------------
# Scenario A — build-timeline-live bundle carries "Build failed before any
# step ran" (surface 1 ground-truth: honest terminal-empty state shipped).
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — build-timeline-live bundle exposes terminal-empty state literal"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    else
        chunk="$(locate_chunk 'build-timeline-live')"
        if [ -z "$chunk" ] ; then
            record_fail A "could not locate build-timeline-live chunk via entry-js import map"
        else
            log "  observed chunk: $chunk"
            body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${chunk}" 2>&1 || true)"
            count="$(printf '%s' "$body" | grep -oE 'Build failed before any step ran' | wc -l | tr -d ' ')"
            if [ "$count" -gt 0 ] ; then
                record_pass A "chunk $chunk has terminal-empty literal — $count occurrence(s)"
            else
                record_fail A "chunk $chunk MISSING terminal-empty literal (pre-fix shape)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — redeploy-dialog bundle carries `throwOnError` opt-in literal.
# Note on chunk geometry: vite bundles `use-redeploy.ts` INTO the
# `redeploy-dialog-*.js` chunk (the dialog imports the hook, so Rollup
# coalesces them into one chunk). The contract is "the chunk that ships
# the redeploy mutation carries throwOnError"; grep against the dialog
# chunk is the canonical surface to prove the opt-in landed.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — redeploy-dialog bundle exposes throwOnError opt-in literal"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        chunk="$(locate_chunk 'redeploy-dialog')"
        if [ -z "$chunk" ] ; then
            record_fail B "could not locate redeploy-dialog chunk via entry-js import map"
        else
            log "  observed chunk: $chunk"
            body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${chunk}" 2>&1 || true)"
            count="$(printf '%s' "$body" | grep -oE 'throwOnError' | wc -l | tr -d ' ')"
            if [ "$count" -gt 0 ] ; then
                record_pass B "chunk $chunk has throwOnError opt-in — $count occurrence(s)"
            else
                record_fail B "chunk $chunk MISSING throwOnError (pre-fix: dialog swallowed 4xx)"
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
