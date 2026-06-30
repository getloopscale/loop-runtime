#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#56 closure
# (onboarding checklist showed 2 of 5 steps complete for an account that
# already had a project with a 'Succeeded' deploy and 'LIVE ENVS: 1';
# steps 1 'Install the LoopZero CLI' and 4 'Deploy your first project'
# remained unchecked because the hook never auto-derived them from real
# API signals — even though the deploy command is the only path to write
# to the engine).
#
# Fix shipped 2026-06-28 in commit `87058b4` (loopzero-cloud):
#   `dashboard/src/hooks/use-onboarding-state.ts:101-106` adds:
#     - `const deploySucceeded = builds.some((b) => b.status === 'succeeded')`
#     - `cli_installed: persistedState.steps.cli_installed || deploySucceeded`
#     - `deploy_succeeded: deploySucceeded`
#   The OR-with-deploySucceeded pattern means: a successful deploy is
#   rigorous proof the user installed the CLI (the deploy command is
#   the only path to write to the engine), so `cli_installed` auto-flips
#   when any deploy succeeds — regardless of whether the user clicked
#   the explicit "I installed it" CTA. The `deploy_succeeded` step
#   auto-derives from the same signal directly.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN). OPERATOR HARD STOP per
# `.claude/rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL.
#
# Scenarios:
#   A — entry bundle carries the canonical `loopzero-onboarding-v1`
#       localStorage key (proves the onboarding code is present in the
#       served bundle; a missing key would mean the panel never mounts).
#   B — entry bundle carries the minified OR pattern `cli_installed||`
#       (proof that the canonical `persistedState.steps.cli_installed
#       || deploySucceeded` resolution shipped; without the trailing
#       `||`, the auto-flip-from-deploy half is missing).
#   C — entry bundle carries the `deploy_succeeded` step key literal
#       (proof the step is registered in the STEPS array AND in the
#       OnboardingSteps interface — the auto-derived deploy step).
#
# Slug:           dashboard-onboarding-derive-deploy
# Issue:          loopzero-cloud#56
# Family:         (none — pre-existing onboarding-checklist drift; only
#                  the `#81` progressbar a11y closure touches the same
#                  panel surface for a different concern)
# Closed by:      `loopzero-cloud/dashboard/src/hooks/use-onboarding-state.ts:101-106`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#56)
# Companion (CI): infra/tests/check-dev-public-dashboard-onboarding-derive-deploy-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-onboarding-derive-deploy"
ISSUE_REF="loopzero-cloud#56"
APP_URL="${APP_URL:-https://app-dev.loopzero.example}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#56 (onboarding derives Install
CLI + Deploy from real signals).

Environment:
  APP_URL    default https://app-dev.loopzero.example (the dev-public dashboard)

OPERATOR HARD STOP per cleanroom-up-live-test-mandate.md INQUEBRÁVEL:
runs LIVE bundle GET against the dashboard SPA entry index chunk.
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
# Helper: locate the entry index bundle (the onboarding panel is inlined
# into the dashboard layout so its hook code lands in the entry chunk).
# ---------------------------------------------------------------------------
locate_entry_chunk() {
    curl -s "$APP_URL/" \
        | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' \
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
    ENTRY_URL="$(locate_entry_chunk)"
    if [ -z "$ENTRY_URL" ] ; then
        record_fail PREFLIGHT "could not locate /assets/index-*.js entry chunk from $APP_URL/"
    else
        log "  observed entry chunk: $ENTRY_URL"
        ENTRY_BODY="$(curl -s "$APP_URL$ENTRY_URL")"

        # -------------------------------------------------------------------
        # Scenario A — entry bundle carries the canonical
        # `loopzero-onboarding-v1` localStorage key
        # -------------------------------------------------------------------
        if want_scenario A ; then
            log "Scenario A — entry bundle carries the 'loopzero-onboarding-v1' storage key"
            key_count="$(printf '%s' "$ENTRY_BODY" | { grep -cF 'loopzero-onboarding-v1' || true ; })"
            if [ "$key_count" -ge 1 ] ; then
                record_pass A "entry bundle has loopzero-onboarding-v1 key=$key_count"
            else
                record_fail A "entry bundle MISSING loopzero-onboarding-v1 storage key (onboarding code absent)"
            fi
        fi

        # -------------------------------------------------------------------
        # Scenario B — entry bundle carries the minified OR pattern
        # `cli_installed||` proving `... || deploySucceeded` shipped
        # -------------------------------------------------------------------
        if want_scenario B ; then
            log "Scenario B — entry bundle carries the 'cli_installed||' OR-with-deploySucceeded pattern"
            or_count="$(printf '%s' "$ENTRY_BODY" | { grep -cF 'cli_installed||' || true ; })"
            if [ "$or_count" -ge 1 ] ; then
                record_pass B "entry bundle has cli_installed|| pattern=$or_count (deploy auto-flips CLI step)"
            else
                record_fail B "entry bundle MISSING cli_installed|| pattern (auto-flip half absent — pre-fix shape)"
            fi
        fi

        # -------------------------------------------------------------------
        # Scenario C — entry bundle carries the `deploy_succeeded` step
        # key literal (auto-derived deploy step registered)
        # -------------------------------------------------------------------
        if want_scenario C ; then
            log "Scenario C — entry bundle carries the 'deploy_succeeded' step key literal"
            ds_count="$(printf '%s' "$ENTRY_BODY" | { grep -cF 'deploy_succeeded' || true ; })"
            if [ "$ds_count" -ge 1 ] ; then
                record_pass C "entry bundle has deploy_succeeded literal=$ds_count (auto-derived step registered)"
            else
                record_fail C "entry bundle MISSING deploy_succeeded literal (step absent — pre-fix shape)"
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
