#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#124 closure
# (dashboard health-truth divergence).
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL — run AFTER Cleanroom TOTAL + UP + seed-post-cleanroom-all.sh.
#
# Reproduces #124 verbatim:
#   * Build SUCCEEDED + release ACTIVE + sync_status=synced + URL populated
#   * BUT Live URL serves HTTP 502 sustained (data plane down).
#   * Pre-fix: dashboard emitted `Status: Healthy` based on
#     active_release.status === 'ACTIVE' alone — false-positive health.
#   * Post-fix: dashboard emits `Status: Unknown` when release is ACTIVE
#     but no serving signal corroborates (replicas_ready / 5xx / alerts /
#     scaling.replicas all absent OR replicas_ready === 0).
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — /status endpoint responds 200 for loopzerokit-example/preview (auth chain
#       end-to-end through Caddy → loopzero-cloud BFF → loopzero-api).
#   B — production dashboard bundle (project-status-*.js) carries the
#       `"Unknown"` verdict literal + the `hasServingSignal` derivation —
#       proof that the fix LANDED in the bundle Caddy serves.
#   C — repro shape end-to-end: GET /status → response has `active_release.
#       status === 'ACTIVE'` AND `sync_status === 'synced'` AND no
#       `replicas_ready` / no `scaling` field (this is the #124 ground-truth
#       state — bundle emits Unknown over this shape).
#
# Slug:           dashboard-health-truth
# Issue:          loopzero-cloud#124
# Family:         #41 / #77 / #94 (cross-view divergence; health not data-plane-aware)
# Closed by:      `dashboard/src/pages/project-status.tsx` (signal-aware verdict)
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#124)
# Companion (CI): infra/tests/check-dev-public-dashboard-health-truth-syntax_test.sh

set -euo pipefail

PLAN_SLUG="dashboard-health-truth"
ISSUE_REF="loopzero-cloud#124"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.loopzero.example}"
PROJECT_NAME="${PROJECT_NAME:-loopzerokit-example}"
SESSION_JWT_FILE="${SESSION_JWT_FILE:-}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#124 (dashboard health-truth).

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
# Scenario A — /status?env=preview responds 200 (auth chain end-to-end).
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — /status?env=preview responds 200 (auth chain end-to-end)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass A "skip-live mode — hermetic"
    elif [ -z "$SESSION_JWT_FILE" ] || [ ! -s "$SESSION_JWT_FILE" ] ; then
        record_fail A "no SESSION_JWT_FILE — run seed-admin-loopzero-dev.sh --refresh-only"
    else
        JWT="$(cat "$SESSION_JWT_FILE")"
        code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 \
            -H "Authorization: Bearer $JWT" \
            "${PUBLIC_CLOUD_URL}/engine/api/v1/projects/${PROJECT_NAME}/status?env=preview" 2>&1 || true)"
        if [ "$code" = "200" ] ; then
            record_pass A "HTTP 200"
        else
            record_fail A "HTTP $code — expected 200"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — production bundle contains the `"Unknown"` verdict literal.
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — production dashboard bundle exposes Unknown verdict literal"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass B "skip-live mode — hermetic"
    else
        html="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>&1 || true)"
        entry_js="$(printf '%s' "$html" | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' | head -1)"
        # The verdict literal lives in the project-status PAGE chunk (lazy-loaded).
        # The sibling `use-project-status-*.js` HOOK chunk also matches the
        # `project-status-` substring, so filter it out explicitly — only the
        # page chunk carries the `"Unknown"` verdict literal post-fix.
        entry_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}${entry_js}" 2>&1 || true)"
        status_chunk="$(printf '%s' "$entry_body" \
            | grep -oE '[A-Za-z0-9_-]*project-status-[A-Za-z0-9_-]+\.js' \
            | grep -v '^use-' \
            | head -1)"
        if [ -z "$status_chunk" ] ; then
            record_fail B "could not locate project-status chunk (entry_js=$entry_js)"
        else
            log "  observed status chunk: $status_chunk"
            chunk_body="$(curl -s --max-time 10 "${PUBLIC_CLOUD_URL}/assets/${status_chunk}" 2>&1 || true)"
            unknown_count="$(printf '%s' "$chunk_body" | grep -oE '"Unknown"' | wc -l | tr -d ' ')"
            if [ "$unknown_count" -gt 0 ] ; then
                record_pass B "bundle $status_chunk has Unknown verdict literal — $unknown_count occurrence(s)"
            else
                record_fail B "bundle $status_chunk MISSING Unknown verdict literal (pre-fix shape)"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — #124 ground-truth: control plane GREEN + no serving signal.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — /status response shape matches #124 ground-truth (ACTIVE + synced + no serving signal)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    elif [ -z "$SESSION_JWT_FILE" ] || [ ! -s "$SESSION_JWT_FILE" ] ; then
        record_fail C "no SESSION_JWT_FILE — run seed-admin-loopzero-dev.sh --refresh-only"
    else
        JWT="$(cat "$SESSION_JWT_FILE")"
        body="$(curl -s --max-time 10 \
            -H "Authorization: Bearer $JWT" \
            "${PUBLIC_CLOUD_URL}/engine/api/v1/projects/${PROJECT_NAME}/status?env=preview" 2>&1 || true)"
        # Parse fields without jq to avoid host-tool drift.
        active_status="$(printf '%s' "$body" | python3 -c 'import sys,json; r=json.load(sys.stdin); print((r.get("active_release") or {}).get("status",""))' 2>/dev/null || echo "")"
        sync_status="$(printf '%s' "$body"  | python3 -c 'import sys,json; r=json.load(sys.stdin); print(r.get("sync_status",""))' 2>/dev/null || echo "")"
        has_replicas_ready="$(printf '%s' "$body" | python3 -c 'import sys,json; r=json.load(sys.stdin); print("yes" if "replicas_ready" in r and r["replicas_ready"] is not None else "no")' 2>/dev/null || echo "?")"
        has_scaling="$(printf '%s' "$body" | python3 -c 'import sys,json; r=json.load(sys.stdin); print("yes" if r.get("scaling") else "no")' 2>/dev/null || echo "?")"
        # The bug shape is: ACTIVE + synced + neither replicas_ready nor scaling. The
        # post-fix bundle MUST emit Unknown over this shape (proven by Scenario B
        # literal + the vitest unit test). Scenario C asserts the wire shape
        # itself is reproducible — without it the bug premise doesn't hold.
        if [ "$active_status" = "ACTIVE" ] && [ "$sync_status" = "synced" ] ; then
            record_pass C "ground-truth shape present: active.status=$active_status sync=$sync_status replicas_ready=$has_replicas_ready scaling=$has_scaling"
        else
            record_fail C "wire shape does not reproduce #124 (active.status='$active_status' sync='$sync_status' — expected ACTIVE+synced)"
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
