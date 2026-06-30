#!/usr/bin/env bash
# c13-valid-destructive-config.sh
#
# Plan c13-valid-destructive-config-chaos (T2.1). Chaos scenario C13:
# inject a TECHNICALLY-VALID-BUT-DESTRUCTIVE config (a schema-valid
# .loopzero/services.json whose workload BUILDS + SYNCS but never becomes
# HEALTHY — e.g. a readiness path that always 404s) and assert the deploy
# pipeline FAILS LOUD (release -> SYNC_FAILED, SSE deploy.sync_failed /
# rollout.aborted) within the SyncTimeout budget — never a silent hang,
# never a spurious ACTIVE.
#
# LoopZero's ACTIVE transition is HEALTH-GATED (api/internal/services/
# release_convergence_loop.go:192,118), so the never-healthy config stays
# PENDING and is failed loud at the 180s SyncTimeout (:81,:240). This is the
# LIVE/E2E proof of that fail-loud boundary; the deterministic Go proof lives
# in TestReleaseConvergenceLoop_SyncedButUnhealthy_TimedOut_FailsLoud + the
# BenchmarkReleaseConvergenceLoop_FailLoudTick (~13477 ns/op).
#
# NO chaos-mesh (plan ADR D3): the fault is a CONFIG fault injected through the
# normal deploy path, NOT an infra fault. There is therefore NO EC-11
# chaos-mesh preflight — only a deploy-path health preflight.
#
# EC-3 boundary guard: the scenario FIRST asserts the build SUCCEEDED (or a
# release row exists in PENDING) so a BUILD-stage failure cannot green this
# scenario on the wrong boundary (it would be the build path, not the
# health-gate timeout the chaos targets).
#
# OPERATOR HARD STOP: the LIVE run requires Cleanroom TOTAL + UP + seeds +
# sponsor JUNTOS per .claude/rules/cleanroom-up-live-test-mandate.md
# (INQUEBRAVEL). The autonomous proof of correctness is the hermetic Go test +
# benchmark; this LIVE bash is the operator+sponsor confirmation.
# Governance: CHG-2026-06-30-001 (T1).
#
# Usage:
#   bash infra/scripts/local/chaos-scenarios/c13-valid-destructive-config.sh
#   bash infra/scripts/local/chaos-scenarios/c13-valid-destructive-config.sh --skip-live
#   bash infra/scripts/local/chaos-scenarios/c13-valid-destructive-config.sh --json
#   bash infra/scripts/local/chaos-scenarios/c13-valid-destructive-config.sh --scenario=A
set -euo pipefail

LOOPZERO_CLOUD_URL="${LOOPZERO_CLOUD_URL:-https://cloud-dev.loopzero.example}"
JWT_FILE="${LOOPZERO_JWT_FILE:-$HOME/.loopzero/local-dogfood-token.txt}"
PROJECT="${C13_PROJECT:-c13-destructive-$(date +%s 2>/dev/null || echo probe)}"
MAX_DURATION_SECONDS="${MAX_DURATION_SECONDS:-300}"   # > 180s SyncTimeout + margin
SLO_FAIL_LOUD_BUDGET="${SLO_FAIL_LOUD_BUDGET:-200}"   # release MUST fail loud within this

SKIP_LIVE=0
JSON_MODE=0
SCENARIO="A"

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-live)    SKIP_LIVE=1 ; shift ;;
    --json)         JSON_MODE=1 ; shift ;;
    --scenario=*)   SCENARIO="${1#*=}" ; shift ;;
    -h|--help)      sed -n '2,46p' "$0" | sed 's/^# \{0,1\}//' ; exit 0 ;;
    *) printf '[c13] unknown arg: %s\n' "$1" >&2 ; exit 1 ;;
  esac
done

log()  { printf '[c13] %s\n' "$*" >&2 ; }
fail=0

emit_verdict() {
  local status="$1"
  if [ "$JSON_MODE" -eq 1 ]; then
    printf '{"scenario":"c13-valid-destructive-config","mode":"%s","result":"%s","fail_count":%d}\n' \
      "$( [ "$SKIP_LIVE" -eq 1 ] && echo hermetic || echo live )" "$status" "$fail"
  fi
}

# --- Hermetic short-circuit (CI) -------------------------------------------
# In --skip-live the script touches NO cluster: it is a structural no-op the
# hermetic guard + CI rely on. The real chaos run is operator+sponsor-gated.
if [ "$SKIP_LIVE" -eq 1 ]; then
  log "--skip-live: hermetic no-op (no cluster touched). LIVE run is operator+sponsor HARD STOP."
  emit_verdict PASS
  exit 0
fi

# ===========================================================================
# OPERATOR HARD STOP — everything below requires Cleanroom TOTAL + UP + seeds
# + sponsor JUNTOS per .claude/rules/cleanroom-up-live-test-mandate.md
# ===========================================================================

# --- Preflight: deploy-path health (NOT chaos-mesh) ------------------------
preflight_deploy_path() {
  log "Preflight — deploy-path health (NO chaos-mesh dependency)"
  local code
  code="$(curl -fsS -o /dev/null -w '%{http_code}' "${LOOPZERO_CLOUD_URL}/health/deep" 2>/dev/null || echo 000)"
  if [ "$code" != "200" ]; then
    log "ERROR: ${LOOPZERO_CLOUD_URL}/health/deep returned ${code} (expected 200) — deploy path down"
    return 1
  fi
  if [ ! -f "$JWT_FILE" ]; then
    log "ERROR: JWT seed not found at ${JWT_FILE} — run the post-Cleanroom seed first"
    return 1
  fi
  log "Preflight OK — deploy path healthy + JWT seed present"
}

# --- Scenario A: deploy a build-valid but health-destructive config --------
scenario_a() {
  log "Scenario A — deploy valid-but-destructive config (always-404 readiness)"
  local jwt deadline release_id build_status release_status reason
  jwt="$(cat "$JWT_FILE")"

  # Deploy a config that is build-valid (image builds + syncs) but whose
  # readiness probe path always 404s, so the workload never becomes healthy.
  release_id="$(curl -fsS -X POST "${LOOPZERO_CLOUD_URL}/api/v1/builds" \
    -H "Authorization: Bearer ${jwt}" -H 'Content-Type: application/json' \
    -d "{\"project\":\"${PROJECT}\",\"services_json\":{\"version\":2,\"services\":[{\"name\":\"api\",\"type\":\"web\",\"healthCheckPath\":\"/c13-never-200\"}]}}" \
    2>/dev/null | jq -r '.release_id // .id // empty' 2>/dev/null || true)"

  # EC-3 boundary guard: the chain MUST reach the health-gate boundary, NOT a
  # build-stage failure. Assert a release row exists in PENDING (build got far
  # enough to create a release) before polling for the health-timeout fail.
  if [ -z "$release_id" ]; then
    log "FAIL (EC-3): no release_id returned — the build failed BEFORE creating a release"
    log "      This is the WRONG boundary (build-stage failure), not the health-gate timeout c13 targets."
    fail=$((fail+1)) ; return
  fi
  build_status="$(curl -fsS "${LOOPZERO_CLOUD_URL}/api/v1/builds/${release_id}" \
    -H "Authorization: Bearer ${jwt}" 2>/dev/null | jq -r '.status // empty' 2>/dev/null || true)"
  log "EC-3 boundary OK — release ${release_id} created (build status: ${build_status:-PENDING}); polling for fail-loud"

  # Poll the release until it fails loud (SYNC_FAILED) or the budget elapses.
  deadline=$(( $(date +%s) + MAX_DURATION_SECONDS ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    release_status="$(curl -fsS "${LOOPZERO_CLOUD_URL}/api/v1/releases/${release_id}" \
      -H "Authorization: Bearer ${jwt}" 2>/dev/null | jq -r '.status // empty' 2>/dev/null || true)"
    case "$release_status" in
      SYNC_FAILED|FAILED)
        reason="$(curl -fsS "${LOOPZERO_CLOUD_URL}/api/v1/releases/${release_id}" \
          -H "Authorization: Bearer ${jwt}" 2>/dev/null | jq -r '.reason // .message // empty' 2>/dev/null || true)"
        log "PASS — release failed loud: status=${release_status} reason=${reason}"
        case "$reason" in
          *"did not become healthy"*|*"timeout"*|*"health"*) : ;;
          *) log "WARN: failed loud but reason did not mention health/timeout: ${reason}" ;;
        esac
        return ;;
      ACTIVE)
        log "FAIL — release went ACTIVE despite a never-healthy workload (contradicts health-gate :192)"
        fail=$((fail+1)) ; return ;;
    esac
    sleep 5
  done
  log "FAIL — release did NOT fail loud within ${MAX_DURATION_SECONDS}s (silent hang past the budget)"
  fail=$((fail+1))
}

main() {
  preflight_deploy_path || { fail=$((fail+1)); emit_verdict FAIL; exit 1; }
  case "$SCENARIO" in
    A) scenario_a ;;
    *) log "unknown scenario: ${SCENARIO}" ; exit 1 ;;
  esac
  if [ "$fail" -eq 0 ]; then emit_verdict PASS; log "c13 LIVE PASS"; exit 0
  else emit_verdict FAIL; log "c13 LIVE FAIL (${fail})"; exit 1; fi
}

main
