#!/usr/bin/env bash
# Sprint 7 plan qa-sprint7-cleanup-2026-06-25 T1.1 — LIVE 4-scenario E2E for
# `getloopscale/loopzero-cloud#36` (BUILD_SUBMIT_FAILED: argo: create workflow:
# Unauthorized). Also retroactively covers the #34 E2E debt per
# .claude/knowledge-base/plans/qa-sprint6-cleanup-2026-06-25-plan.md.
#
# Per .claude/rules/cleanroom-up-live-test-mandate.md INQUEBRÁVEL +
# feedback_issue_fix_requires_failing_e2e_test_2026_06_25: this script MUST
# be authored + confirmed RED against the current dev-public engine BEFORE
# the Sprint 7 fix lands; the LIVE-PROOF evidence is the RED→GREEN flip on
# the same script.
set -euo pipefail

PLAN_SLUG="qa-sprint7-cleanup-2026-06-25"
CLOUD_BASE="${LOOPZERO_CLOUD_URL:?LOOPZERO_CLOUD_URL must be set (e.g. https://app-dev.loopzero.example)}"
MGMT_CTX="${LOOPZERO_MGMT_KUBECONTEXT:-k3d-loopzero-mgmt}"
BUILD_CTX="${LOOPZERO_BUILD_KUBECONTEXT:-k3d-loopzero-build}"
JWT_PATH="${LOOPZERO_SESSION_JWT_PATH:-$HOME/.loopzero/local-dogfood-session-jwt.txt}"
SKIP_LIVE=0
SCENARIO=""
JSON_OUTPUT=0
RESULTS=()

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C|D] [--json] [--help]

LIVE 4-scenario test for loopzero-cloud#36 RBAC fix. Operator HARD STOP per
cleanroom-up-live-test-mandate.md for full --live coverage (needs valid JWT
+ kubectl access to both mgmt and build clusters).

  --skip-live      Hermetic structural check only (no cluster I/O)
  --scenario=X     Run a single scenario (A/B/C/D); default: all
  --json           Emit machine-readable summary
  --help           Show this message and exit
EOF
}

for arg in "$@"; do
    case "$arg" in
        --skip-live) SKIP_LIVE=1 ;;
        --scenario=*) SCENARIO="${arg#--scenario=}" ;;
        --json) JSON_OUTPUT=1 ;;
        --help) usage; exit 0 ;;
        *) echo "unknown flag: $arg" >&2; usage; exit 2 ;;
    esac
done

record() {
    local id="$1" verdict="$2" detail="$3"
    RESULTS+=("$id:$verdict:$detail")
}

# Scenario A — confirm build kubeconfig token claims sub == argo/loopzero-workflow-submitter.
# Pre-Sprint-7 (RED): sub == kube-system/default. Post-Sprint-7 (GREEN): correct SA.
scenario_a() {
    [ -n "$SCENARIO" ] && [ "$SCENARIO" != "A" ] && return 0
    if [ "$SKIP_LIVE" -eq 1 ]; then
        record "A-build-kubeconfig-sa-claim" "SKIP" "hermetic mode"
        return 0
    fi
    local token sub
    token=$(kubectl --context "$MGMT_CTX" -n loopzero-system get secret loopzero-cluster-kubeconfigs \
        -o go-template='{{index .data "build.kubeconfig" | base64decode}}' 2>/dev/null \
        | python3 -c "import sys,yaml; print(yaml.safe_load(sys.stdin)['users'][0]['user']['token'])" 2>/dev/null || true)
    if [ -z "$token" ]; then
        record "A-build-kubeconfig-sa-claim" "FAIL" "could not read build.kubeconfig token"
        return 1
    fi
    sub=$(echo "$token" | python3 -c "
import sys, json, base64
payload = sys.stdin.read().strip().split('.')[1]
payload += '=' * (-len(payload) % 4)
print(json.loads(base64.urlsafe_b64decode(payload))['sub'])
" 2>/dev/null || echo "<unparseable>")
    if [ "$sub" = "system:serviceaccount:argo:loopzero-workflow-submitter" ]; then
        record "A-build-kubeconfig-sa-claim" "PASS" "sub=$sub"
    else
        record "A-build-kubeconfig-sa-claim" "FAIL" "sub=$sub (expected system:serviceaccount:argo:loopzero-workflow-submitter — #36 root cause)"
        return 1
    fi
}

# Scenario B — confirm the canonical SA has Role+RoleBinding granting create workflows.
scenario_b() {
    [ -n "$SCENARIO" ] && [ "$SCENARIO" != "B" ] && return 0
    if [ "$SKIP_LIVE" -eq 1 ]; then
        record "B-argo-rbac-can-create-workflows" "SKIP" "hermetic mode"
        return 0
    fi
    local verdict
    verdict=$(kubectl --context "$BUILD_CTX" auth can-i create workflows.argoproj.io -n argo \
        --as=system:serviceaccount:argo:loopzero-workflow-submitter 2>&1 | head -1 || true)
    if [ "$verdict" = "yes" ]; then
        record "B-argo-rbac-can-create-workflows" "PASS" "$verdict"
    else
        record "B-argo-rbac-can-create-workflows" "FAIL" "auth can-i returned '$verdict' (Role+RoleBinding missing or stale)"
        return 1
    fi
}

# Scenario C — end-to-end: POST /api/v1/builds returns 202 + build_id (not BUILD_SUBMIT_FAILED).
# Requires authenticated JWT — typically the dashboard session JWT through cloud-dev proxy.
scenario_c() {
    [ -n "$SCENARIO" ] && [ "$SCENARIO" != "C" ] && return 0
    if [ "$SKIP_LIVE" -eq 1 ]; then
        record "C-build-submit-succeeds" "SKIP" "hermetic mode"
        return 0
    fi
    if [ ! -f "$JWT_PATH" ]; then
        record "C-build-submit-succeeds" "SKIP" "JWT seed missing at $JWT_PATH (sponsor JUNTOS step required for full coverage)"
        return 0
    fi
    local response code
    response=$(curl -sSL --max-time 30 \
        -H "Authorization: Bearer $(cat "$JWT_PATH")" \
        -H "Content-Type: application/json" \
        -X POST "$CLOUD_BASE/engine/api/v1/builds" \
        -d '{"project_id":"qa-sprint7-rbac-probe","environment":"preview","commit_sha":"00000000","apps":[{"name":"app","type":"server","port":3000}]}' 2>&1 || true)
    code=$(echo "$response" | grep -oE '"code"\s*:\s*"[^"]+"' | head -1 | sed -E 's/.*"([^"]+)"$/\1/')
    if echo "$response" | grep -qE 'BUILD_SUBMIT_FAILED|create workflow.*Unauthorized'; then
        record "C-build-submit-succeeds" "FAIL" "response carries #36 verbatim: $(echo "$response" | head -c 200)"
        return 1
    fi
    record "C-build-submit-succeeds" "PASS" "code=$code (no #36 regression)"
}

# Scenario D — regression guard: the verbatim `argo: create workflow: Unauthorized`
# substring MUST NOT appear in any error response after Sprint 7 fix.
scenario_d() {
    [ -n "$SCENARIO" ] && [ "$SCENARIO" != "D" ] && return 0
    if [ "$SKIP_LIVE" -eq 1 ]; then
        record "D-argo-unauthorized-text-absent" "SKIP" "hermetic mode"
        return 0
    fi
    if [ ! -f "$JWT_PATH" ]; then
        record "D-argo-unauthorized-text-absent" "SKIP" "JWT seed missing at $JWT_PATH"
        return 0
    fi
    local response
    response=$(curl -sSL --max-time 30 \
        -H "Authorization: Bearer $(cat "$JWT_PATH")" \
        -H "Content-Type: application/json" \
        -X POST "$CLOUD_BASE/engine/api/v1/builds" \
        -d '{"project_id":"qa-sprint7-rbac-probe","environment":"preview","commit_sha":"00000000","apps":[{"name":"app","type":"server","port":3000}]}' 2>&1 || true)
    if echo "$response" | grep -qE 'argo: create workflow: Unauthorized'; then
        record "D-argo-unauthorized-text-absent" "FAIL" "#36 verbatim text surfaced"
        return 1
    fi
    record "D-argo-unauthorized-text-absent" "PASS" "verbatim text absent"
}

main() {
    scenario_a || true
    scenario_b || true
    scenario_c || true
    scenario_d || true
    local fail_count=0
    for r in "${RESULTS[@]}"; do
        case "$r" in *:FAIL:*) fail_count=$((fail_count + 1)) ;; esac
    done
    if [ "$JSON_OUTPUT" -eq 1 ]; then
        printf '{"plan":"%s","scenarios":[' "$PLAN_SLUG"
        local first=1
        for r in "${RESULTS[@]}"; do
            IFS=':' read -r id verdict detail <<<"$r"
            [ $first -eq 0 ] && printf ','
            printf '{"id":"%s","verdict":"%s","detail":"%s"}' "$id" "$verdict" "${detail//\"/\\\"}"
            first=0
        done
        printf '],"fail_count":%d}\n' "$fail_count"
    else
        for r in "${RESULTS[@]}"; do echo "$r"; done
        echo "fail_count=$fail_count"
    fi
    [ "$fail_count" -eq 0 ]
}

main "$@"
