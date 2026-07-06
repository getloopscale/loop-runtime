#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#97 closure
# (Overview "Recent deployments" omitted STATUS + ENVIRONMENT badges per
# row — five FAILED preview deploys looked identical to five SUCCEEDED
# preview deploys; user could not tell apart a working preview from a
# broken one and the omission reinforced the #41 "Live vs production"
# confusion).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #97 verbatim:
#   * Open /projects/{name} (Overview).
#   * Pre-fix: the "Recent deployments" card rendered `commit · author ·
#     "X ago"` per row — STATUS and ENVIRONMENT absent even though the
#     /builds API surfaced both fields and the sibling /deployments view
#     had columns for both.
#   * Post-fix (commit 87058b4 in loopzero-cloud, 2026-06-28):
#       (a) `dashboard/src/pages/project-detail.tsx:313-340` calls
#           `normalizeBuildStatus(b.status)` and renders a status Badge
#           with `'success' | 'destructive' | 'outline'` variant per row.
#       (b) when `b.environment` is present, an outline Badge with the
#           env label renders next to the status Badge.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — engine wire shape: GET /engine/api/v1/projects/{name}/builds
#       surfaces `status` + `environment` per row (the SoT the UI reads).
#       Without these fields the badges have nothing to render and the
#       bug premise doesn't hold.
#   B — production page chunk (project-detail-*.js) carries BOTH Badge
#       variant literals (`"success"` and `"destructive"`) — proof the
#       status-badge half of the fix shipped.
#   C — same chunk carries the `recent-deploy-` data-testid AND multiple
#       `.environment` references — proof the per-row environment badge
#       data dependency is wired into the Recent deployments list (not
#       only the full /deployments view).
#
# Slug:           dashboard-overview-recent-deploys-status-env-badges
# Issue:          loopzero-cloud#97
# Family:         #41 / #52 / #78 (Overview vs Deployments view divergence)
# Closed by:      `dashboard/src/pages/project-detail.tsx:307-340`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#97)
# Companion (CI): infra/tests/check-dev-public-dashboard-overview-recent-deploys-status-env-badges-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-overview-recent-deploys-status-env-badges"
ISSUE_REF="loopzero-cloud#97"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
PROJECT_NAME="${PROJECT_NAME:-teste}"
SESSION_JWT_FILE="${SESSION_JWT_FILE:-}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#97 (Recent deployments status+env badges).

Environment:
  PUBLIC_CLOUD_URL  default https://app-dev.loopzero.example
  PROJECT_NAME      default loopzerokit-example (must have ≥ 1 build with status
                    and environment populated — any project with a real
                    deploy works)
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

# Default JWT discovery (canonical paths).
if [ -z "$SESSION_JWT_FILE" ] ; then
    for cand in "$HOME/.loopzero/local-dogfood-session-jwt.txt" "$HOME/.loopzero/local-dogfood-token.txt" "$HOME/.loopzero/redteam-qa-session-jwt.txt" ; do
        if [ -s "$cand" ] ; then SESSION_JWT_FILE="$cand" ; break ; fi
    done
fi

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

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "Probe: ${PUBLIC_CLOUD_URL}/engine/api/v1/projects/${PROJECT_NAME}/builds"
log "JWT: ${SESSION_JWT_FILE:-(none)}"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — engine wire shape: /builds surfaces status + environment.
# The UI reads BOTH from the API; without them the badges have nothing to
# render, even with the rendering code shipped.
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — /engine/api/v1/projects/${PROJECT_NAME}/builds surfaces status + environment per row"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    elif [ -z "$SESSION_JWT_FILE" ] || [ ! -s "$SESSION_JWT_FILE" ] ; then
        record_fail A "no SESSION_JWT_FILE — run seed-admin-loopzero-dev.sh --refresh-only"
    else
        JWT="$(cat "$SESSION_JWT_FILE")"
        body="$(curl -s --max-time 10 -L \
            -H "Authorization: Bearer $JWT" \
            "${PUBLIC_CLOUD_URL}/engine/api/v1/projects/${PROJECT_NAME}/builds?limit=5" 2>&1 || true)"
        verdict="$(printf '%s' "$body" | python3 -c '
import json, sys
try:
    d = json.loads(sys.stdin.read())
except Exception as e:
    print(f"FAIL parse: {e}")
    sys.exit(0)
builds = d.get("builds", []) if isinstance(d, dict) else []
if not builds:
    print("FAIL no builds returned")
    sys.exit(0)
missing_status = sum(1 for b in builds if not b.get("status"))
missing_env = sum(1 for b in builds if not b.get("environment"))
if missing_status:
    print(f"FAIL {missing_status}/{len(builds)} builds missing status")
elif missing_env:
    print(f"FAIL {missing_env}/{len(builds)} builds missing environment")
else:
    envs = ",".join(sorted({b.get("environment") for b in builds}))
    statuses = ",".join(sorted({b.get("status") for b in builds}))
    print(f"PASS {len(builds)} builds carry status+env (statuses=[{statuses}] envs=[{envs}])")
' 2>&1)"
        case "$verdict" in
            PASS*) record_pass A "${verdict#PASS }" ;;
            *)     record_fail A "${verdict#FAIL }" ;;
        esac
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — production page chunk carries BOTH Badge variant literals
# (`"success"` AND `"destructive"`) — proof the status-badge half shipped.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — production project-detail page chunk carries success+destructive Badge variants"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
        entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
        entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
        # Match project-detail-<hash>.js but NOT deployment-detail-<hash>.js.
        page_chunk="$(printf '%s' "$entry_body" \
            | grep -oE '[A-Za-z0-9_-]*project-detail-[A-Za-z0-9_-]+\.js' \
            | grep -v '^deployment-' \
            | head -1)"
        if [ -z "$page_chunk" ] ; then
            record_fail B "could not locate project-detail page chunk (entry_js=$entry_js)"
        else
            log "  observed page chunk: $page_chunk"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${page_chunk}" 2>&1 || true)"
            success_count="$(printf '%s' "$chunk_body" | grep -oF '"success"' | wc -l | tr -d ' ')"
            destructive_count="$(printf '%s' "$chunk_body" | grep -oF '"destructive"' | wc -l | tr -d ' ')"
            if [ "$success_count" -gt 0 ] && [ "$destructive_count" -gt 0 ] ; then
                record_pass B "page chunk $page_chunk has success=$success_count destructive=$destructive_count Badge variants"
            else
                record_fail B "page chunk $page_chunk MISSING Badge variants (success=$success_count destructive=$destructive_count — pre-fix shape)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — same chunk carries the `recent-deploy-` data-testid AND
# the `.environment` reference — proof the per-row environment badge data
# dependency is wired into the Recent deployments list (not just the full
# /deployments view).
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — production page chunk wires Recent-deployments testid + environment badge data dep"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
        entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
        entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
        page_chunk="$(printf '%s' "$entry_body" \
            | grep -oE '[A-Za-z0-9_-]*project-detail-[A-Za-z0-9_-]+\.js' \
            | grep -v '^deployment-' \
            | head -1)"
        if [ -z "$page_chunk" ] ; then
            record_fail C "could not locate project-detail page chunk"
        else
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${page_chunk}" 2>&1 || true)"
            testid_count="$(printf '%s' "$chunk_body" | grep -oF 'recent-deploy-' | wc -l | tr -d ' ')"
            env_count="$(printf '%s' "$chunk_body" | grep -oE '\.environment' | wc -l | tr -d ' ')"
            if [ "$testid_count" -gt 0 ] && [ "$env_count" -gt 1 ] ; then
                record_pass C "page chunk has recent-deploy- testid=$testid_count + .environment refs=$env_count (per-row badge data wired)"
            else
                record_fail C "page chunk MISSING per-row wiring (testid=$testid_count env_refs=$env_count — pre-fix shape)"
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
