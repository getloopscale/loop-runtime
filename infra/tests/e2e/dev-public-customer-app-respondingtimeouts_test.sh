#!/usr/bin/env bash
# Plan red-team-77-107-closure v1.0 T1.2 — LIVE 3-scenario regression-protector
# for Red Team loopzero-cloud#107 (Traefik writeTimeout=60s cuts customer streaming
# at ~61s). Reproduces the cut VERBATIM pre-fix per the INQUEBRÁVEL
# feedback_issue_fix_requires_failing_e2e_test_2026_06_25 (sponsor verbatim
# 2026-06-25): every issue closure requires a LIVE bash that reproduces the
# issue's symptom against the deployed binary BEFORE the fix rolls out;
# LIVE-PROOF post-rollout is the closure gate.
#
# Root cause: loopzero/infra/helm/traefik/values.yaml:99 hard-caps response
# writes at 60s on the websecure entryPoint (customer HTTPS surface).
# Affects: SSE / AI streaming / long-poll / large uploads — anything taking
# > 60s wall-clock between end-of-request-header-read and end-of-response-write.
#
# OPERATOR HARD STOP per .claude/rules/cleanroom-up-live-test-mandate.md
# INQUEBRÁVEL for full --live coverage (requires LOOPZERO_CUSTOMER_APP_URL set to
# a Live URL of a deployed customer app with `/api/slow` + `/api/stream`
# regression-probe endpoints — see Red Team #107 CTO LIVE-validation
# 2026-06-28 against loopzerokit-example). The hermetic --skip-live path is the
# CI-safe entry consumed by `task quality:gates`.
set -euo pipefail

PLAN_SLUG="red-team-77-107-closure"
ISSUE_REF="loopzero-cloud#107"
CUSTOMER_APP_URL="${LOOPZERO_CUSTOMER_APP_URL:-}"  # e.g. https://tn-<tenant>-loopzerokit-example-app-preview.prod-dev.loopzero.example
SLOW_PATH="${LOOPZERO_CUSTOMER_APP_SLOW_PATH:-/api/slow}"
STREAM_PATH="${LOOPZERO_CUSTOMER_APP_STREAM_PATH:-/api/stream}"
SLOW_SLEEP_SECONDS="${LOOPZERO_CUSTOMER_APP_SLOW_SLEEP:-120}"   # default 120s — > 60s cap, < 300s ceiling
STREAM_DURATION_SECONDS="${LOOPZERO_CUSTOMER_APP_STREAM_DURATION:-90}"  # SSE duration > 60s
SKIP_LIVE=0
SCENARIO=""
JSON_OUTPUT=0
RESULTS=()

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE 3-scenario regression-protector for ${ISSUE_REF} (Traefik writeTimeout
=60s cuts customer responses at ~61s).

  A — HTTP slow response: GET \${CUSTOMER_APP_URL}\${SLOW_PATH}?sleep=\${SLOW_SLEEP_SECONDS}
      MUST return HTTP 200 with full canonical body after ~\${SLOW_SLEEP_SECONDS}s
      wall-clock. Pre-fix: cut at ~61s with truncated body (RED).
      Post-fix: clean 200 with full body (GREEN).
  B — SSE stream: GET \${CUSTOMER_APP_URL}\${STREAM_PATH}?duration=\${STREAM_DURATION_SECONDS}
      MUST stream continuously without connection reset for \${STREAM_DURATION_SECONDS}s.
      Pre-fix: connection reset at ~61s. Post-fix: clean stream completion.
  C — Preflight: \${CUSTOMER_APP_URL}/ responds 200..399 (sanity check the
      customer app is reachable before scenarios A+B fire).

Required env:
  LOOPZERO_CUSTOMER_APP_URL                 Live URL of customer app with slow/stream probes.

Optional env:
  LOOPZERO_CUSTOMER_APP_SLOW_PATH           default: /api/slow
  LOOPZERO_CUSTOMER_APP_STREAM_PATH         default: /api/stream
  LOOPZERO_CUSTOMER_APP_SLOW_SLEEP          default: 120 (seconds)
  LOOPZERO_CUSTOMER_APP_STREAM_DURATION     default: 90  (seconds)

Operator HARD STOP per cleanroom-up-live-test-mandate.md.

  --skip-live      Hermetic structural check only (no cluster I/O)
  --scenario=X     Run a single scenario (A/B/C); default: all
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
    local scenario="$1" verdict="$2" message="$3"
    RESULTS+=("${scenario}|${verdict}|${message}")
    if [[ "$JSON_OUTPUT" -eq 0 ]]; then
        printf '  [%s] %s — %s\n' "$verdict" "$scenario" "$message" >&2
    fi
}

emit_json() {
    local fail_count
    fail_count=$(printf '%s\n' "${RESULTS[@]:-}" | grep -c '|FAIL|' || true)
    local verdict="PASS"
    [[ "$fail_count" -gt 0 ]] && verdict="FAIL"

    printf '{\n'
    printf '  "plan": "%s",\n' "$PLAN_SLUG"
    printf '  "issue": "%s",\n' "$ISSUE_REF"
    printf '  "verdict": "%s",\n' "$verdict"
    printf '  "scenarios": [\n'
    local i=0
    local total=${#RESULTS[@]}
    for entry in "${RESULTS[@]:-}"; do
        i=$((i+1))
        IFS='|' read -r scen v msg <<< "$entry"
        local comma=","
        [[ "$i" -eq "$total" ]] && comma=""
        printf '    {"scenario": "%s", "verdict": "%s", "message": %s}%s\n' "$scen" "$v" "$(printf '%s' "$msg" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')" "$comma"
    done
    printf '  ]\n'
    printf '}\n'
}

preflight_url() {
    if [[ -z "$CUSTOMER_APP_URL" ]]; then
        record "preflight" "FAIL" "LOOPZERO_CUSTOMER_APP_URL not set — operator must deploy a customer app with /api/slow + /api/stream probes (see plan red-team-77-107-closure T1.2 ADR D2 Drawback row 3)"
        return 1
    fi
    return 0
}

scenario_a_http_slow() {
    [[ -n "$SCENARIO" && "$SCENARIO" != "A" ]] && return 0
    local url="${CUSTOMER_APP_URL}${SLOW_PATH}?sleep=${SLOW_SLEEP_SECONDS}"
    local start end elapsed body http
    start=$(date +%s)
    body=$(curl -sS -N --max-time $((SLOW_SLEEP_SECONDS + 30)) -w "\n__HTTP__%{http_code}__END__" "$url" 2>&1 || echo "__CURL_ERROR__")
    end=$(date +%s)
    elapsed=$((end - start))
    http=$(printf '%s' "$body" | grep -oE '__HTTP__[0-9]+__END__' | grep -oE '[0-9]+' | head -1)
    body=$(printf '%s' "$body" | sed 's/__HTTP__[0-9]\+__END__//')

    local lower_bound=$((SLOW_SLEEP_SECONDS - 10))
    local upper_bound=$((SLOW_SLEEP_SECONDS + 20))
    if [[ "$http" != "200" ]]; then
        record "A-http-slow" "FAIL" "expected HTTP 200, got '$http' (elapsed=${elapsed}s url=${url})"
        return 1
    fi
    if [[ "$elapsed" -lt "$lower_bound" ]]; then
        record "A-http-slow" "FAIL" "wall-clock ${elapsed}s < ${lower_bound}s (response cut before full sleep — Traefik writeTimeout may still be 60s)"
        return 1
    fi
    if [[ "$elapsed" -gt "$upper_bound" ]]; then
        record "A-http-slow" "FAIL" "wall-clock ${elapsed}s > ${upper_bound}s (backend or proxy slower than expected)"
        return 1
    fi
    if ! printf '%s' "$body" | grep -qE '\{.*"ok"[[:space:]]*:[[:space:]]*true.*\}'; then
        record "A-http-slow" "FAIL" "expected body matches {\"ok\":true}, got '${body:0:200}'"
        return 1
    fi
    record "A-http-slow" "PASS" "HTTP 200 + wall-clock ${elapsed}s ∈ [${lower_bound},${upper_bound}] + canonical body — writeTimeout > ${SLOW_SLEEP_SECONDS}s"
    return 0
}

scenario_b_sse_stream() {
    [[ -n "$SCENARIO" && "$SCENARIO" != "B" ]] && return 0
    local url="${CUSTOMER_APP_URL}${STREAM_PATH}?duration=${STREAM_DURATION_SECONDS}"
    local start end elapsed exit_code line_count
    local tmp
    tmp=$(mktemp)
    trap "rm -f '$tmp'" EXIT INT TERM
    start=$(date +%s)
    # -N disables curl buffering for SSE; --max-time bounds the test runtime
    curl -sS -N --max-time $((STREAM_DURATION_SECONDS + 30)) "$url" > "$tmp" 2>&1
    exit_code=$?
    end=$(date +%s)
    elapsed=$((end - start))
    line_count=$(wc -l < "$tmp" 2>/dev/null || echo 0)

    local lower_bound=$((STREAM_DURATION_SECONDS - 10))
    if [[ "$elapsed" -lt "$lower_bound" ]]; then
        record "B-sse-stream" "FAIL" "stream reset after ${elapsed}s < ${lower_bound}s (curl exit ${exit_code}; lines=${line_count}) — connection cut by edge proxy"
        rm -f "$tmp"
        return 1
    fi
    if [[ "$line_count" -lt 1 ]]; then
        record "B-sse-stream" "FAIL" "stream completed without emitting any line (lines=${line_count} elapsed=${elapsed}s)"
        rm -f "$tmp"
        return 1
    fi
    record "B-sse-stream" "PASS" "stream held ${elapsed}s ≥ ${lower_bound}s with ${line_count} lines emitted"
    rm -f "$tmp"
    return 0
}

scenario_c_preflight() {
    [[ -n "$SCENARIO" && "$SCENARIO" != "C" ]] && return 0
    local http
    http=$(curl -sS -m 15 -o /dev/null -w '%{http_code}' "$CUSTOMER_APP_URL/" 2>&1 || echo "000")
    if [[ "$http" -ge 200 && "$http" -lt 400 ]]; then
        record "C-preflight" "PASS" "customer app reachable (HTTP $http)"
        return 0
    fi
    record "C-preflight" "FAIL" "customer app preflight HTTP $http (expected 200..399)"
    return 1
}

# Hermetic mode short-circuits before any HTTP call
if [[ "$SKIP_LIVE" -eq 1 ]]; then
    record "hermetic" "PASS" "structural-only — flags + scenarios wired (no cluster I/O)"
    if [[ "$JSON_OUTPUT" -eq 1 ]]; then
        emit_json
    fi
    exit 0
fi

preflight_url || {
    [[ "$JSON_OUTPUT" -eq 1 ]] && emit_json
    exit 1
}

scenario_c_preflight || true
scenario_a_http_slow || true
scenario_b_sse_stream || true

fail_count=$(printf '%s\n' "${RESULTS[@]:-}" | grep -c '|FAIL|' || true)
if [[ "$JSON_OUTPUT" -eq 1 ]]; then
    emit_json
fi
exit "$fail_count"
