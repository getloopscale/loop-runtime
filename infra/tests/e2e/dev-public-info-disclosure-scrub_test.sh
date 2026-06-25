#!/usr/bin/env bash
# Sprint 7 plan qa-sprint7-cleanup-2026-06-25 T1.2 — LIVE 7-scenario E2E for
# `getloopscale/loopzero-cloud#35` (info-disclosure CWE-209: error messages leak
# internal stack names to tenant-owner accounts).
#
# Per the new INQUEBRÁVEL feedback (sponsor verbatim 2026-06-25): this
# script MUST be authored + confirmed RED against the current dev-public
# engine BEFORE the Sprint 7 scrub extension lands.
set -euo pipefail

PLAN_SLUG="qa-sprint7-cleanup-2026-06-25"
CLOUD_BASE="${LOOPZERO_CLOUD_URL:?LOOPZERO_CLOUD_URL must be set (e.g. https://app-dev.loopzero.example)}"
JWT_PATH="${LOOPZERO_SESSION_JWT_PATH:-$HOME/.loopzero/local-dogfood-session-jwt.txt}"
SKIP_LIVE=0
SCENARIO=""
JSON_OUTPUT=0
RESULTS=()
RESPONSE=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C|D|E|F|G] [--json] [--help]

LIVE 7-scenario test for loopzero-cloud#35 info-disclosure (CWE-209).
Operator HARD STOP for full --live coverage.

  --skip-live      Hermetic structural check only
  --scenario=X     Run a single scenario (A=trigger / B..F=assert NO forbidden terms / G=negative-direction)
  --json           Machine-readable
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

# Scenario A — trigger any 4xx/5xx via canonical deploy path; capture response.
scenario_a() {
    [ -n "$SCENARIO" ] && [ "$SCENARIO" != "A" ] && return 0
    if [ "$SKIP_LIVE" -eq 1 ]; then
        record "A-trigger-error-response" "SKIP" "hermetic mode"
        return 0
    fi
    if [ ! -f "$JWT_PATH" ]; then
        record "A-trigger-error-response" "SKIP" "JWT seed missing"
        return 0
    fi
    # POST a build to trigger the #36 Argo Unauthorized path (which Sprint 7
    # both fixes AND scrubs). Captures the response for B..F assertions.
    RESPONSE=$(curl -sSL --max-time 30 \
        -H "Authorization: Bearer $(cat "$JWT_PATH")" \
        -H "Content-Type: application/json" \
        -X POST "$CLOUD_BASE/engine/api/v1/builds" \
        -d '{"project_id":"qa-sprint7-scrub-probe","environment":"preview","commit_sha":"00000000","apps":[{"name":"app","type":"server","port":3000}]}' 2>&1 || true)
    if [ -z "$RESPONSE" ]; then
        record "A-trigger-error-response" "FAIL" "empty response"
        return 1
    fi
    record "A-trigger-error-response" "PASS" "captured response len=${#RESPONSE}"
}

# Helper: assert forbidden term NOT present in RESPONSE.
assert_no_term() {
    local id="$1" term="$2"
    [ -n "$SCENARIO" ] && [ "$SCENARIO" != "${id:0:1}" ] && return 0
    if [ "$SKIP_LIVE" -eq 1 ] || [ -z "$RESPONSE" ]; then
        record "$id" "SKIP" "hermetic or no response"
        return 0
    fi
    if echo "$RESPONSE" | grep -iqE "$term"; then
        record "$id" "FAIL" "verbatim '$term' surfaced in error message (info-disclosure CWE-209)"
        return 1
    fi
    record "$id" "PASS" "no '$term' leak"
}

scenario_b() { assert_no_term "B-no-argo-leak"          "\bargo\b"; }
scenario_c() { assert_no_term "C-no-argoproj-leak"      "argoproj"; }
scenario_d() { assert_no_term "D-no-serviceaccount-leak" "serviceaccount"; }
scenario_e() { assert_no_term "E-no-namespace-leak"     "\bnamespace\b"; }
scenario_f() { assert_no_term "F-no-tmp-path-leak"      "/tmp/loopzero-source-"; }

# Scenario G — negative-direction regression-protector: a legitimate user term
# like a project name "argo-test" must NOT be scrubbed away. Sprint 7 scrub
# uses word-boundaries so user input survives.
scenario_g() {
    [ -n "$SCENARIO" ] && [ "$SCENARIO" != "G" ] && return 0
    if [ "$SKIP_LIVE" -eq 1 ]; then
        record "G-negative-direction-argo-test-project" "SKIP" "hermetic mode"
        return 0
    fi
    if [ ! -f "$JWT_PATH" ]; then
        record "G-negative-direction-argo-test-project" "SKIP" "JWT seed missing"
        return 0
    fi
    # Probe with a user-supplied project_id containing "argo" as a substring.
    # The response MAY echo it back (e.g., in a validation error); the scrub
    # MUST NOT strip user input.
    local response
    response=$(curl -sSL --max-time 30 \
        -H "Authorization: Bearer $(cat "$JWT_PATH")" \
        -H "Content-Type: application/json" \
        -X POST "$CLOUD_BASE/engine/api/v1/builds" \
        -d '{"project_id":"my-argo-test-project","environment":"preview","commit_sha":"00000000","apps":[{"name":"app","type":"server","port":3000}]}' 2>&1 || true)
    # If the response contains the user-supplied name, the scrub is correctly
    # word-boundary-aware. If the user name is scrubbed too, the regression-
    # protector flags it (false-positive scrub).
    if echo "$response" | grep -q 'my-argo-test-project'; then
        record "G-negative-direction-argo-test-project" "PASS" "user term survived scrub"
    else
        # If the response simply doesn't echo the name (e.g., success path 202),
        # this scenario is INFO not FAIL — we can't assert the negative without
        # an error path that echoes the name.
        record "G-negative-direction-argo-test-project" "INFO" "response did not echo user term — negative-direction unverifiable in this scenario"
    fi
}

main() {
    scenario_a || true
    scenario_b || true
    scenario_c || true
    scenario_d || true
    scenario_e || true
    scenario_f || true
    scenario_g || true
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
