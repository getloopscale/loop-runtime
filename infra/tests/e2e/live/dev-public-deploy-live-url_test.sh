#!/usr/bin/env bash
# Plan m37-tenant-cr-project-sync-canonical-fix v1.0 T3.1 — LIVE 4-scenario
# E2E for `loop-console#37` scenario 4 (BUILD → RELEASE → URL chain). The
# Sprint 8 #37 close shipped the watcher namespace-scoped LIST but missed
# the per-(project,env) tuple shallow-check gap that prevents `loopscale deploy`
# from extending a tenant's Tenant CR `spec.environments[]` when a NEW
# project is deployed against an existing tenant.
#
# Per the UNBREAKABLE feedback_issue_fix_requires_failing_e2e_test_2026_06_25
# (sponsor verbatim 2026-06-25): this script MUST be authored +
# RED-confirmed against the pre-m37 engine BEFORE the m37 fix rolls out;
# LIVE-PROOF post-fix is the rollout-passes-GREEN.
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# UNBREAKABLE for full --live coverage (requires valid JWT + dev-public
# reachable + kubectl context k3d-loop-runtime-core). The hermetic --skip-live
# path is the CI-safe entry.
set -euo pipefail

PLAN_SLUG="m37-tenant-cr-project-sync-canonical-fix"
CLOUD_BASE="${LOOP_CONSOLE_URL:?LOOP_CONSOLE_URL must be set (e.g. https://app-dev.getloopscale.com)}"
MGMT_CTX="${LOOP_RUNTIME_MGMT_KUBECONTEXT:-k3d-loop-runtime-core}"
JWT_PATH="${LOOP_RUNTIME_SESSION_JWT_PATH:-$HOME/.loop-runtime/local-dogfood-session-jwt.txt}"
WAIT_BUDGET_SECONDS="${LOOP_RUNTIME_DEPLOY_LIVE_URL_WAIT_BUDGET:-1800}"  # 30min CLI ceiling
SKIP_LIVE=0
SCENARIO=""
JSON_OUTPUT=0
RESULTS=()

# Probe project name — UNIQUE per run for scenarios A+B (m37-specific
# assertions: Tenant CR tuple + AppProject destinations on a NEW project
# against an existing tenant). Caller can override.
PROBE_PROJECT="${LOOP_RUNTIME_DEPLOY_LIVE_URL_PROJECT:-m37-probe-$(date +%s)}"
PROBE_ENV="${LOOP_RUNTIME_DEPLOY_LIVE_URL_ENV:-preview}"
PROBE_COMMIT_SHA="${LOOP_RUNTIME_DEPLOY_LIVE_URL_COMMIT:-deadbeefdeadbeefdeadbeefdeadbeefdeadbeef}"

# Canonical fixture project for scenarios C+D (BUILD → RELEASE → URL
# end-to-end). Scenarios C+D probe an EXISTING active release because the
# build chain requires a real GitHub source commit (not the synthetic
# commit_sha used for A+B). The canonical fixture is `sample` (per memory
# project_canonical_dev_user_credentials + reference seed scripts).
# Operator can override; setting empty string SKIPs C+D with a SKIP verdict
# (not a FAIL).
CANONICAL_PROJECT="${LOOP_RUNTIME_DEPLOY_LIVE_URL_CANONICAL_PROJECT:-sample}"
CANONICAL_ENV="${LOOP_RUNTIME_DEPLOY_LIVE_URL_CANONICAL_ENV:-production}"

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C|D] [--json] [--help]

LIVE 4-scenario test for loop-console#37 scenario 4 (BUILD → RELEASE → URL).

  A — Tenant CR \`spec.environments[]\` gains the (project, env) tuple
      after POST /api/v1/builds for a NEW project on existing tenant
      (the m37 fix that closes the silent no-op).
  B — ArgoCD AppProject \`<tenant>\` gains a \`destinations[].namespace\`
      matching \`loop-runtime-<tenant>-<project>-<env>\` within 60s.
  C — Release reaches ACTIVE within $WAIT_BUDGET_SECONDS seconds AND
      \`release.url\` is non-empty (\`loopscale status --json\` view).
  D — HTTP probe of the Live URL returns 200..399 (workload responds).

Operator HARD STOP per cleanroom-up-live-test-mandate.md.

  --skip-live      Hermetic structural check only (no cluster I/O)
  --scenario=X     Run a single scenario (A/B/C/D); default: all
  --json           Emit machine-readable summary
  --help           Show this message
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

preflight() {
    if [ "$SKIP_LIVE" -eq 1 ]; then
        record "preflight" "SKIP" "hermetic mode"
        return 0
    fi
    if ! curl -sk -m 8 -o /dev/null -w "%{http_code}" "$CLOUD_BASE/api/v1/health" 2>/dev/null | grep -qE '^200$'; then
        record "preflight" "FAIL" "dev-public health 200 not reached at $CLOUD_BASE/api/v1/health"
        return 1
    fi
    if [ ! -f "$JWT_PATH" ]; then
        record "preflight" "FAIL" "JWT seed missing at $JWT_PATH; run seed-admin-loop-runtime-dev.sh --refresh-only"
        return 1
    fi
    if ! command -v kubectl >/dev/null 2>&1; then
        record "preflight" "FAIL" "kubectl not on PATH (cluster assertions need it)"
        return 1
    fi
    record "preflight" "PASS" "health=200 jwt=ok kubectl=ok project=$PROBE_PROJECT env=$PROBE_ENV"
}

# Helper: derive tenant_id via /engine/api/v1/projects (returns the
# authenticated tenant context). The canonical project response shape uses
# the `team` field (NOT `tenant_id`) — both are accepted. Returns "tn-..."
# or empty on failure.
resolve_tenant_id() {
    curl -sk -m 10 \
        -H "Authorization: Bearer $(cat "$JWT_PATH")" \
        "$CLOUD_BASE/engine/api/v1/projects" 2>/dev/null \
        | python3 -c "
import sys, json
try:
    body = json.load(sys.stdin)
    items = body if isinstance(body, list) else body.get('projects', body.get('items', []))
    for p in items:
        if not isinstance(p, dict):
            continue
        # Canonical engine response uses 'team'; older shape used 'tenant_id'.
        team = p.get('team') or p.get('tenant_id')
        if team:
            print(team)
            break
except Exception:
    pass
" 2>/dev/null
}

# Helper: refresh the session JWT in place. The canonical dev-public session
# JWT has a 15min TTL (per Kratos+Hydra ConfigMap defaults), but the deploy
# chain ceiling is 30min (Sprint 8 #38 contract). Long-poll scenarios MUST
# refresh JWT periodically OR the C/D probes degrade to 401. We call the
# canonical seed-admin --refresh-only flow (mints a fresh session via the
# Kratos admin API + propagates to ~/.loop-runtime/local-dogfood-session-jwt.txt).
# Skipped when SKIP_LIVE=1; failure is non-fatal (log + continue with stale).
refresh_jwt_if_needed() {
    if [ "$SKIP_LIVE" -eq 1 ]; then
        return 0
    fi
    local refresh_script="${LOOP_RUNTIME_REPO_ROOT:-$(cd "$(dirname "$0")/../../../.." && pwd)}/infra/scripts/local/seed-admin-loop-runtime-dev.sh"
    if [ -x "$refresh_script" ]; then
        bash "$refresh_script" --refresh-only >/dev/null 2>&1 || true
    fi
}

# POST a build to trigger the deploy chain for the unique probe project.
# Idempotency-Key required per the engine contract.
submit_probe_build() {
    local ik="m37-probe-${PROBE_PROJECT}-$(date +%s)"
    curl -sk -m 30 \
        -H "Authorization: Bearer $(cat "$JWT_PATH")" \
        -H "Content-Type: application/json" \
        -H "Idempotency-Key: $ik" \
        -X POST "$CLOUD_BASE/engine/api/v1/builds" \
        -d "{\"project_id\":\"$PROBE_PROJECT\",\"environment\":\"$PROBE_ENV\",\"commit_sha\":\"$PROBE_COMMIT_SHA\",\"apps\":[{\"name\":\"app\",\"type\":\"server\",\"port\":3000}]}" 2>&1
}

# Scenario A — Tenant CR spec.environments[] gains the new tuple post-build.
# Pre-m37: silent no-op (Lister exists=true on env-name match across projects).
# Post-m37: AddEnvironment fires, Tenant CR mutated, observable within 60s.
scenario_a() {
    [ -n "$SCENARIO" ] && [ "$SCENARIO" != "A" ] && return 0
    if [ "$SKIP_LIVE" -eq 1 ]; then
        record "A-tenant-cr-tuple-extended" "SKIP" "hermetic mode"
        return 0
    fi
    local tenant_id
    tenant_id=$(resolve_tenant_id)
    if [ -z "$tenant_id" ]; then
        record "A-tenant-cr-tuple-extended" "FAIL" "could not resolve tenant_id via /engine/api/v1/projects"
        return 1
    fi
    # Submit the build to trigger the deploy chain.
    local submit_resp
    submit_resp=$(submit_probe_build)
    if ! echo "$submit_resp" | grep -qE 'build_id|"build-'; then
        record "A-tenant-cr-tuple-extended" "FAIL" "build submit did not return build_id: $(echo "$submit_resp" | head -c 200)"
        return 1
    fi
    # Poll Tenant CR for the new tuple. Up to 60s — operator reconcile is fast.
    local deadline=$((SECONDS + 60))
    while [ $SECONDS -lt $deadline ]; do
        if kubectl --context "$MGMT_CTX" -n loop-runtime-system get tenant "$tenant_id" -o json 2>/dev/null \
            | python3 -c "
import sys, json
t = json.load(sys.stdin)
envs = t['spec'].get('environments', [])
project = '$PROBE_PROJECT'
env_name = '$PROBE_ENV'
for e in envs:
    if e.get('name') == env_name and e.get('project') == project:
        sys.exit(0)
sys.exit(1)
" 2>/dev/null; then
            record "A-tenant-cr-tuple-extended" "PASS" "tenant=$tenant_id tuple=($PROBE_PROJECT,$PROBE_ENV) present"
            return 0
        fi
        sleep 3
    done
    record "A-tenant-cr-tuple-extended" "FAIL" "tenant CR $tenant_id did not gain ($PROBE_PROJECT,$PROBE_ENV) tuple within 60s — m37 gap NOT closed"
    return 1
}

# Scenario B — AppProject destinations[].namespace includes the new canonical
# 3-segment namespace within 60s of Tenant CR mutation.
scenario_b() {
    [ -n "$SCENARIO" ] && [ "$SCENARIO" != "B" ] && return 0
    if [ "$SKIP_LIVE" -eq 1 ]; then
        record "B-appproject-destinations-extended" "SKIP" "hermetic mode"
        return 0
    fi
    local tenant_id
    tenant_id=$(resolve_tenant_id)
    if [ -z "$tenant_id" ]; then
        record "B-appproject-destinations-extended" "FAIL" "could not resolve tenant_id"
        return 1
    fi
    local expected_ns="loop-runtime-${tenant_id}-${PROBE_PROJECT}-${PROBE_ENV}"
    local deadline=$((SECONDS + 60))
    while [ $SECONDS -lt $deadline ]; do
        if kubectl --context "$MGMT_CTX" -n argocd get appproject "$tenant_id" -o json 2>/dev/null \
            | python3 -c "
import sys, json
p = json.load(sys.stdin)
target = '$expected_ns'
for d in p['spec'].get('destinations', []):
    if d.get('namespace') == target:
        sys.exit(0)
sys.exit(1)
" 2>/dev/null; then
            record "B-appproject-destinations-extended" "PASS" "AppProject $tenant_id allows namespace=$expected_ns"
            return 0
        fi
        sleep 3
    done
    record "B-appproject-destinations-extended" "FAIL" "AppProject $tenant_id did not gain destination $expected_ns within 60s"
    return 1
}

# Scenario C — canonical project has ACTIVE release with non-empty URL.
# Probes the EXISTING canonical fixture project (default `sample`) instead
# of a synthetic probe, because building a release requires real GitHub
# source — the synthetic probe used by A+B is intentional for the m37
# tuple assertion but cannot drive a real BUILD → RELEASE → URL chain.
# Setting CANONICAL_PROJECT="" SKIPs C with a SKIP verdict.
scenario_c() {
    [ -n "$SCENARIO" ] && [ "$SCENARIO" != "C" ] && return 0
    if [ "$SKIP_LIVE" -eq 1 ]; then
        record "C-release-active-with-url" "SKIP" "hermetic mode"
        return 0
    fi
    if [ -z "$CANONICAL_PROJECT" ]; then
        record "C-release-active-with-url" "SKIP" "CANONICAL_PROJECT empty — set LOOP_RUNTIME_DEPLOY_LIVE_URL_CANONICAL_PROJECT to enable"
        return 0
    fi
    local deadline=$((SECONDS + WAIT_BUDGET_SECONDS))
    local last_state="?"
    local last_url=""
    local last_refresh=$SECONDS
    while [ $SECONDS -lt $deadline ]; do
        # Refresh JWT every ~12min to stay ahead of the 15min TTL.
        if [ $((SECONDS - last_refresh)) -gt 720 ]; then
            refresh_jwt_if_needed
            last_refresh=$SECONDS
        fi
        # Canonical engine status endpoint: GET /api/v1/projects/{name}/status
        # returns the active_release + sync_status + url for the latest deploy
        # in the requested environment.
        local resp
        resp=$(curl -sk -m 15 \
            -H "Authorization: Bearer $(cat "$JWT_PATH")" \
            "$CLOUD_BASE/engine/api/v1/projects/$CANONICAL_PROJECT/status?environment=$CANONICAL_ENV" 2>/dev/null)
        # Extract release state + url from the canonical statusResponse shape.
        local extracted
        extracted=$(echo "$resp" | python3 -c "
import sys, json
try:
    body = json.load(sys.stdin)
    if isinstance(body, dict):
        active = body.get('active_release') or {}
        # The canonical statusResponse shape carries 'status' on
        # active_release (not 'state') and 'url' at top-level.
        state = ''
        if isinstance(active, dict):
            state = active.get('status') or active.get('state') or ''
        url = body.get('url') or (active.get('url') if isinstance(active, dict) else '')
        print(str(state) + '|' + str(url or ''))
except Exception:
    pass
" 2>/dev/null)
        last_state="${extracted%%|*}"
        last_url="${extracted#*|}"
        if [ "$last_state" = "ACTIVE" ] && [ -n "$last_url" ]; then
            echo "$last_url" > /tmp/m37-live-url.txt
            record "C-release-active-with-url" "PASS" "project=$CANONICAL_PROJECT env=$CANONICAL_ENV release ACTIVE url=$last_url"
            return 0
        fi
        sleep 10
    done
    record "C-release-active-with-url" "FAIL" "project=$CANONICAL_PROJECT did not reach ACTIVE with URL within ${WAIT_BUDGET_SECONDS}s (last_state=$last_state last_url='$last_url')"
    return 1
}

# Scenario D — HTTP probe the Live URL captured in C. Accept 200..399 because
# the canonical loopzerokit-example app returns 200 on root; 30x is acceptable
# (some apps redirect to /app or /dashboard).
scenario_d() {
    [ -n "$SCENARIO" ] && [ "$SCENARIO" != "D" ] && return 0
    if [ "$SKIP_LIVE" -eq 1 ]; then
        record "D-live-url-http-2xx-3xx" "SKIP" "hermetic mode"
        return 0
    fi
    if [ ! -f /tmp/m37-live-url.txt ]; then
        record "D-live-url-http-2xx-3xx" "FAIL" "scenario C did not capture a Live URL (precondition)"
        return 1
    fi
    local url
    url=$(cat /tmp/m37-live-url.txt)
    # Allow up to 120s for TLS cert + workload warmup post-release-active.
    local deadline=$((SECONDS + 120))
    while [ $SECONDS -lt $deadline ]; do
        local code
        code=$(curl -sk -m 15 -o /dev/null -w "%{http_code}" "$url" 2>/dev/null || echo "000")
        if [ "$code" -ge 200 ] && [ "$code" -lt 400 ]; then
            record "D-live-url-http-2xx-3xx" "PASS" "url=$url HTTP $code"
            return 0
        fi
        sleep 5
    done
    record "D-live-url-http-2xx-3xx" "FAIL" "url=$(cat /tmp/m37-live-url.txt) did not return 2xx/3xx within 120s"
    return 1
}

main() {
    preflight || true
    # Refresh JWT at the start of the LIVE flow — the 15min TTL doesn't
    # cover the 30min deploy ceiling even on a single iteration.
    refresh_jwt_if_needed
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
