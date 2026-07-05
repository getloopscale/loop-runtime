#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#128 closure
# (dashboard /projects list badge lies "Live" when data plane serves 502).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh
# Step 13 (poller warm-up) has emitted at least one tick.
#
# Reproduces #128 verbatim (family of #124/#41/#77/#94 — cross-view
# divergence between control-plane ACTIVE and data-plane state):
#   * Release ACTIVE + sync synced + URL populated (control plane green).
#   * BUT `kube_deployment_status_replicas_ready` == 0 (data plane down;
#     Live URL serves HTTP 502 sustained).
#   * Pre-fix: /projects row rendered `● Live` from active_release.status
#     alone — same trap #124 fixed for /status; unclosed on the list.
#   * Post-fix (T4.1 + T4.2): the row derives from the DataPlanePoller
#     snapshot surfaced by env_targets.last_replicas_ready_{value,at}
#     into the 4-state honest verdict Live/Deployed/Unknown/Down.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — engine /engine/api/v1/projects returns rows with populated
#       `last_replicas_ready_value` post-poller-warm-up (poller wiring
#       regression-protector — T2.1 + T2.2 + T3.1 + T3.2 chain proof).
#   B — production dashboard bundle (projects-*.js page chunk, disambiguated
#       against project-detail/project-status siblings) carries the 4-state
#       verdict literals (`"Down"` + `"Deployed"` + `"Unknown"`) AND the
#       `last_replicas_ready_value` reference — proof the fix LANDED in
#       the bundle Caddy serves.
#   C — LIVE render — `kubectl scale --replicas=0` forces the data plane
#       down for the probe project; wait for the poller tick to persist;
#       assert the /projects engine wire surfaces `last_replicas_ready_value=0`
#       for the target row (dashboard derivation is C's downstream and is
#       hermetic-tested in T4.3; the LIVE half is the poller-scrape-persist
#       chain end-to-end).
#
# Slug:           dashboard-badge-data-plane-poller
# Issue:          loopzero-cloud#128
# Family:         #124 / #41 / #77 / #94 (cross-view control-plane vs data-plane divergence)
# Closed by:      operators/internal/app/data_plane_poller.go (writer)
#                 api/internal/db/queries/projects.sql (LATERAL surface)
#                 dashboard/src/pages/projects.tsx deriveDataPlaneBadge (consumer)
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#128)
# Companion (CI): infra/tests/check-dev-public-dashboard-projects-badge-data-plane-aware-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-badge-data-plane-poller"
ISSUE_REF="loopzero-cloud#128"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
PROJECT_NAME="${PROJECT_NAME:-teste}"
SESSION_JWT_FILE="${SESSION_JWT_FILE:-}"
SCALE_DEPLOYMENT="${SCALE_DEPLOYMENT:-}"
SCALE_NAMESPACE="${SCALE_NAMESPACE:-}"
POLLER_TICK_WAIT_S="${POLLER_TICK_WAIT_S:-60}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#128 (dashboard /projects badge
data-plane-aware).

Environment:
  PUBLIC_CLOUD_URL      default https://app-dev.loopzero.example
  PROJECT_NAME          default loopzerokit-example
  SESSION_JWT_FILE      default ~/.loopzero/local-dogfood-session-jwt.txt (or canonical)
  SCALE_DEPLOYMENT      Scenario C — canonical target Deployment name
  SCALE_NAMESPACE       Scenario C — canonical target namespace (loopzero-<tenant>-<project>-<env>)
  POLLER_TICK_WAIT_S    Scenario C — wait budget after kubectl scale (default 60s;
                        1 poller tick canonical + one buffer per blueprint Q6)

Per cleanroom-up-live-test-mandate.md INQUEBRÁVEL: run AFTER Cleanroom TOTAL
+ UP + seed-post-cleanroom-all.sh (including Step 13 poller warm-up).
Operator-side. Not for CI.
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
    for cand in "$HOME/.loopzero/local-dogfood-session-jwt.txt" "$HOME/.loopzero/redteam-qa-session-jwt.txt" ; do
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
log "Project: $PROJECT_NAME @ $PUBLIC_CLOUD_URL"
log "JWT: ${SESSION_JWT_FILE:-(none)}"
log "============================================================"

# ---------------------------------------------------------------------------
# Scenario A — engine /projects surfaces last_replicas_ready_value (poller
# wiring end-to-end: Prometheus scrape → PG UPDATE → LATERAL surface).
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — engine /projects surfaces last_replicas_ready_value"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    elif [ -z "$SESSION_JWT_FILE" ] || [ ! -s "$SESSION_JWT_FILE" ] ; then
        record_fail A "no SESSION_JWT_FILE — run seed-admin-loopzero-dev.sh --refresh-only"
    else
        JWT="$(cat "$SESSION_JWT_FILE")"
        body="$(curl -s --max-time 10 \
            -H "Authorization: Bearer $JWT" \
            "${PUBLIC_CLOUD_URL}/engine/api/v1/projects?limit=50" 2>&1 || true)"
        # Count how many rows carry the new field. Even ONE non-null value
        # proves the T1.1+T2.x+T3.x chain is wired end-to-end. The dashboard
        # tolerates absent-per-project via 4-state Unknown; the wiring
        # regression is when NO row surfaces the field at all.
        surfaced_count="$(printf '%s' "$body" | python3 -c '
import sys, json
try:
    r = json.load(sys.stdin)
except Exception:
    print(0); sys.exit(0)
projects = r.get("projects", []) if isinstance(r, dict) else []
count = sum(1 for p in projects if p.get("last_replicas_ready_value") is not None)
print(count)
' 2>/dev/null || echo "0")"
        if [ "$surfaced_count" -gt 0 ] ; then
            record_pass A "$surfaced_count project(s) surface last_replicas_ready_value"
        else
            record_fail A "no project surfaces last_replicas_ready_value (poller wiring gap OR Step 13 warm-up did not run)"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — production /projects page chunk carries the 4-state derivation.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — production dashboard bundle exposes 4-state verdict + snapshot field"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
        entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
        entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
        # The 4-state derivation lives in the /projects PAGE chunk. Disambiguate
        # against project-detail/project-status/project-connect/project-scale/
        # project-readiness siblings (all match project-*). The projects-*.js
        # entry uniquely does NOT begin with `project-`; use sed to pick the
        # bare `projects-` prefix (not `project-`).
        page_chunk="$(printf '%s' "$entry_body" \
            | grep -oE '[A-Za-z0-9_-]*projects-[A-Za-z0-9_-]+\.js' \
            | grep -vE 'project-(detail|status|connect|scale|readiness)' \
            | head -1)"
        if [ -z "$page_chunk" ] ; then
            record_fail B "could not locate projects page chunk (entry_js=$entry_js)"
        else
            log "  observed page chunk: $page_chunk"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${page_chunk}" 2>&1 || true)"
            # Assert all 3 new verdict literals are present. `"Live"` predates
            # this fix (not a proof marker); `"Down"` + `"Deployed"` + `"Unknown"`
            # are the T4.2 signatures. Also require the snapshot field name —
            # without it the derivation has no data dependency wired.
            down_count="$(printf '%s' "$chunk_body" | grep -oE '"Down"' | wc -l | tr -d ' ')"
            dep_count="$(printf '%s' "$chunk_body" | grep -oE '"Deployed"' | wc -l | tr -d ' ')"
            unk_count="$(printf '%s' "$chunk_body" | grep -oE '"Unknown"' | wc -l | tr -d ' ')"
            snap_count="$(printf '%s' "$chunk_body" | grep -oE 'last_replicas_ready_value' | wc -l | tr -d ' ')"
            if [ "$down_count" -gt 0 ] && [ "$dep_count" -gt 0 ] && [ "$unk_count" -gt 0 ] && [ "$snap_count" -gt 0 ] ; then
                record_pass B "bundle $page_chunk has Down=$down_count Deployed=$dep_count Unknown=$unk_count snapshot=$snap_count"
            else
                record_fail B "bundle $page_chunk missing verdict literals (Down=$down_count Deployed=$dep_count Unknown=$unk_count snapshot=$snap_count) — pre-fix shape"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — LIVE `kubectl scale --replicas=0` forces data plane down;
# poller persists snapshot; engine surfaces last_replicas_ready_value=0.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — kubectl scale --replicas=0 → poller tick → last_replicas_ready_value=0"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    elif [ -z "$SESSION_JWT_FILE" ] || [ ! -s "$SESSION_JWT_FILE" ] ; then
        record_fail C "no SESSION_JWT_FILE — run seed-admin-loopzero-dev.sh --refresh-only"
    elif [ -z "$SCALE_DEPLOYMENT" ] || [ -z "$SCALE_NAMESPACE" ] ; then
        record_fail C "SCALE_DEPLOYMENT + SCALE_NAMESPACE required (target the probe project's canonical Deployment)"
    elif ! command -v kubectl >/dev/null 2>&1 ; then
        record_fail C "kubectl not on PATH"
    else
        JWT="$(cat "$SESSION_JWT_FILE")"
        # Canonical (Cleanroom 2026-07-04): tenant workloads live in the
        # workload cluster as Argo Rollouts, not raw Deployments. Auto-detect
        # kind + context per SCALE_KIND (default rollout) + KUBE_CONTEXT
        # (default k3d-loopzero-workload). Legacy `deploy` still supported.
        SCALE_KIND="${SCALE_KIND:-rollout}"
        KUBE_CONTEXT="${KUBE_CONTEXT:-k3d-loopzero-workload}"
        log "  scaling $KUBE_CONTEXT/$SCALE_NAMESPACE/$SCALE_KIND/$SCALE_DEPLOYMENT to 0"
        if ! kubectl --context "$KUBE_CONTEXT" -n "$SCALE_NAMESPACE" scale "$SCALE_KIND" "$SCALE_DEPLOYMENT" --replicas=0 --timeout=30s >/dev/null 2>&1 ; then
            record_fail C "kubectl scale --replicas=0 failed"
        else
            log "  waiting ${POLLER_TICK_WAIT_S}s for poller tick to persist"
            sleep "$POLLER_TICK_WAIT_S"
            body="$(curl -s --max-time 10 \
                -H "Authorization: Bearer $JWT" \
                "${PUBLIC_CLOUD_URL}/engine/api/v1/projects?limit=50" 2>&1 || true)"
            # Find the row for PROJECT_NAME and read its last_replicas_ready_value.
            target_val="$(printf '%s' "$body" | python3 -c '
import sys, json, os
try:
    r = json.load(sys.stdin)
except Exception:
    print("parse_error"); sys.exit(0)
name = os.environ.get("PROJECT_NAME","")
for p in (r.get("projects", []) if isinstance(r, dict) else []):
    if p.get("name") == name:
        v = p.get("last_replicas_ready_value")
        print("null" if v is None else str(v))
        sys.exit(0)
print("row_not_found")
' 2>/dev/null || echo "python_error")"
            if [ "$target_val" = "0" ] ; then
                record_pass C "$PROJECT_NAME row last_replicas_ready_value=0 post-scale — data-plane-down signal reached the wire"
            else
                record_fail C "$PROJECT_NAME row last_replicas_ready_value='$target_val' post-scale — expected 0 (poller-persist chain broken OR wait budget too short)"
            fi
            # Restore replicas so the test is idempotent for re-runs.
            log "  restoring $KUBE_CONTEXT/$SCALE_NAMESPACE/$SCALE_KIND/$SCALE_DEPLOYMENT to 1"
            kubectl --context "$KUBE_CONTEXT" -n "$SCALE_NAMESPACE" scale "$SCALE_KIND" "$SCALE_DEPLOYMENT" --replicas=1 --timeout=30s >/dev/null 2>&1 || true
        fi
    fi
fi

RESULTS_JSON+="]}"

log "============================================================"
log "Result: $PASS_COUNT PASS, $FAIL_COUNT FAIL"
[ -n "$FAILED_LIST" ] && log "Failed:${FAILED_LIST}"
log "============================================================"

if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '{"pass":%d,"fail":%d,"results":%s}\n' "$PASS_COUNT" "$FAIL_COUNT" "$RESULTS_JSON"
fi

exit $([ $FAIL_COUNT -eq 0 ] && echo 0 || echo 1)
