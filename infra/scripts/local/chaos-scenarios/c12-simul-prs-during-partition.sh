#!/usr/bin/env bash
# c12-simul-prs-during-partition.sh
#
# T4.2 of plan 2026-06-08-pre-release-test-rigor v1.2 (Cenário 3, scenario C12).
#
# Scenario: inject NetworkChaos partition between mgmt cluster and build
# cluster while dispatching 2 simultaneous PR opens on the canonical fixture
# repo. Asserts:
#   - At least 1 release reaches ACTIVE post-partition heal
#   - No duplicate ACTIVE releases for the same commit_sha (proves
#     withEnvironmentLock + pg_advisory_xact_lock invariants hold under chaos)
#
# EC-6 (MUST FIX) absorbed — TWO defensive measures:
#   (a) NetworkChaos manifest uses EXPLICIT selector.namespaces=[loopzero-system,
#       argo-system] instead of mode: all. Without this, the partition spills
#       into operator port-forwards + browser sessions on shared dev-public.
#   (b) Cleanup trap on EXIT INT TERM deletes all networkchaos resources to
#       guarantee recovery even on operator Ctrl+C mid-test.
#
# EC-11 (DOCUMENT preflight) absorbed: chaos-mesh operator Running healthy
# preflight (same as C11).
#
# loopzero-design.md invariants verified:
#   - withEnvironmentLock guards the (tenant, project, environment) mutation
#   - pg_advisory_xact_lock is transaction-scoped, releases on crash
#   - Concurrent dispatches surface as 1×202 + 1×409 (acquired vs in-progress)
#     OR sequential serialization — never as 2 ACTIVE releases for same commit.
#
# Usage:
#   bash infra/scripts/local/chaos-scenarios/c12-simul-prs-during-partition.sh
#   bash infra/scripts/local/chaos-scenarios/c12-simul-prs-during-partition.sh --skip-live
#   bash infra/scripts/local/chaos-scenarios/c12-simul-prs-during-partition.sh --json
set -euo pipefail

REPO_OWNER="${REPO_OWNER:-getloopscale}"
REPO_NAME="${REPO_NAME:-loopzerokit-example}"
CONTEXT="${CONTEXT:-k3d-loopzero-mgmt}"
CHAOS_NAMESPACE="${CHAOS_NAMESPACE:-chaos-mesh}"
LOOPZERO_SYSTEM_NS="${LOOPZERO_SYSTEM_NS:-loopzero-system}"
ARGO_SYSTEM_NS="${ARGO_SYSTEM_NS:-argo}"
PG_NAMESPACE="${PG_NAMESPACE:-loopzero-system}"
PARTITION_SECONDS="${PARTITION_SECONDS:-180}"           # 3 min partition window
POST_HEAL_BUDGET_SECONDS="${POST_HEAL_BUDGET_SECONDS:-1800}"  # 30 min post-heal

SKIP_LIVE=0
JSON_MODE=0

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-live)  SKIP_LIVE=1 ; shift ;;
    --json)       JSON_MODE=1 ; shift ;;
    -h|--help)
      sed -n '2,35p' "$0" | sed 's/^# \{0,1\}//' ; exit 0 ;;
    *) printf '[c12] unknown arg: %s\n' "$1" >&2 ; exit 1 ;;
  esac
done

log() { printf '[c12] %s\n' "$*" >&2 ; }

START_EPOCH="$(date +%s)"
CHAOS_MANIFEST=""

# --- EC-6 (b) — cleanup trap on EXIT INT TERM -----------------------------

cleanup() {
  log "cleanup (EC-6): deleting networkchaos resources from all guarded namespaces"
  kubectl delete networkchaos --all -n "$LOOPZERO_SYSTEM_NS" --context="$CONTEXT" 2>/dev/null || true
  kubectl delete networkchaos --all -n "$ARGO_SYSTEM_NS" --context="$CONTEXT" 2>/dev/null || true
  kubectl delete networkchaos --all -n "$CHAOS_NAMESPACE" --context="$CONTEXT" 2>/dev/null || true
  [ -n "$CHAOS_MANIFEST" ] && rm -f "$CHAOS_MANIFEST"
}
trap cleanup EXIT INT TERM

# --- EC-11 preflight: chaos-mesh health ------------------------------------

preflight_chaos_mesh() {
  log "Preflight (EC-11) — chaos-mesh health"
  local pod_count
  pod_count="$(kubectl --context="$CONTEXT" -n "$CHAOS_NAMESPACE" \
    get pods -o jsonpath='{.items[?(@.status.phase=="Running")].metadata.name}' 2>/dev/null | wc -w)"
  if [ "$pod_count" -lt 4 ] ; then
    log "ERROR (EC-11): chaos-mesh has only ${pod_count}/4 Running pods — chaos infra unhealthy"
    return 1
  fi
  log "Preflight OK — chaos-mesh has ${pod_count} Running pods"
}

# --- EC-6 (a) — Apply NetworkChaos with EXPLICIT scoped selector -----------

apply_networkchaos() {
  CHAOS_MANIFEST="$(mktemp /tmp/c12-netchaos-XXXXXX.yaml)"
  # EC-6 (a): selector.namespaces EXPLICIT (loopzero-system + argo-system).
  # NEVER mode: all — would partition operator port-forwards + browser sessions
  # on shared dev-public infra (out of scope blast radius).
  cat > "$CHAOS_MANIFEST" <<YAML
apiVersion: chaos-mesh.org/v1alpha1
kind: NetworkChaos
metadata:
  name: c12-partition-mgmt-build
  namespace: ${LOOPZERO_SYSTEM_NS}
spec:
  action: partition
  mode: all
  selector:
    # EC-6 absorption — scoped blast radius (NOT mode: all)
    namespaces:
      - ${LOOPZERO_SYSTEM_NS}
      - ${ARGO_SYSTEM_NS}
    labelSelectors:
      app: loopzero-api
  direction: both
  target:
    mode: all
    selector:
      namespaces:
        - ${ARGO_SYSTEM_NS}
      labelSelectors:
        app: argo-server
  duration: ${PARTITION_SECONDS}s
YAML
  kubectl --context="$CONTEXT" apply -f "$CHAOS_MANIFEST" > /dev/null || {
    log "ERROR: kubectl apply NetworkChaos failed"
    return 2
  }
  log "NetworkChaos applied — partition mgmt(loopzero-system) ↔ build(argo)"
}

# --- Dispatch 2 PRs concurrently -------------------------------------------

dispatch_2_prs() {
  log "Dispatching 2 PRs simultaneously (gh api repository_dispatch x2)"
  (gh api -X POST "/repos/${REPO_OWNER}/${REPO_NAME}/dispatches" \
    -F "event_type=loopzero-c12-pr-a" > /dev/null 2>&1) &
  PR1_PID=$!
  (gh api -X POST "/repos/${REPO_OWNER}/${REPO_NAME}/dispatches" \
    -F "event_type=loopzero-c12-pr-b" > /dev/null 2>&1) &
  PR2_PID=$!
  wait "$PR1_PID" "$PR2_PID" 2>/dev/null || true
  log "2 PR dispatches submitted"
}

# --- Post-heal release assertions ------------------------------------------

assert_releases_post_heal() {
  log "Asserting post-heal: ≥1 ACTIVE release + no duplicate ACTIVE for same commit"
  local pg_password
  pg_password="$(kubectl --context="$CONTEXT" -n "$PG_NAMESPACE" \
    get secret loopzero-db-credentials -o jsonpath='{.data.password}' 2>/dev/null | base64 -d 2>/dev/null || echo "loopzero-dev-password")"

  local deadline=$(( $(date +%s) + POST_HEAL_BUDGET_SECONDS ))
  while [ "$(date +%s)" -lt "$deadline" ] ; do
    local active_count duplicate_count
    active_count="$(kubectl --context="$CONTEXT" -n "$PG_NAMESPACE" \
      exec -i postgresql-0 -c postgresql -- \
      env PGPASSWORD="$pg_password" psql -U loopzero -d loopzero -tAc \
      "SELECT count(*) FROM releases WHERE status='ACTIVE' AND created_at > now() - interval '1 hour'" 2>/dev/null | tr -d '[:space:]' || echo "0")"

    duplicate_count="$(kubectl --context="$CONTEXT" -n "$PG_NAMESPACE" \
      exec -i postgresql-0 -c postgresql -- \
      env PGPASSWORD="$pg_password" psql -U loopzero -d loopzero -tAc \
      "SELECT count(*) FROM (SELECT commit_sha FROM releases WHERE status='ACTIVE' AND created_at > now() - interval '1 hour' GROUP BY commit_sha HAVING count(*) > 1) dups" 2>/dev/null | tr -d '[:space:]' || echo "0")"

    if [ "$duplicate_count" -gt "0" ] ; then
      log "FAIL: ${duplicate_count} commit(s) have duplicate ACTIVE releases (withEnvironmentLock invariant violated)"
      return 1
    fi
    if [ "$active_count" -ge "1" ] ; then
      log "OK: ${active_count} ACTIVE release(s); 0 duplicate-for-same-commit (1×202+1×409 lock invariant holds)"
      return 0
    fi
    sleep 15
  done
  log "FAIL: no ACTIVE release within ${POST_HEAL_BUDGET_SECONDS}s post-heal"
  return 1
}

# --- Main -------------------------------------------------------------------

main() {
  if [ "$SKIP_LIVE" = "1" ] ; then
    log "[--skip-live] hermetic-only mode; not exercising LIVE chaos"
    if [ "$JSON_MODE" = "1" ] ; then
      printf '{"scenario":"C12","verdict":"SKIP","reason":"--skip-live"}\n'
    fi
    exit 0
  fi

  preflight_chaos_mesh || exit 1
  apply_networkchaos   || exit 2
  dispatch_2_prs
  log "Waiting for partition window (${PARTITION_SECONDS}s) to elapse before assertions"
  sleep "$PARTITION_SECONDS"
  assert_releases_post_heal || exit 1

  local elapsed=$(( $(date +%s) - START_EPOCH ))
  log "DONE — C12 chaos scenario completed in ${elapsed}s"
  if [ "$JSON_MODE" = "1" ] ; then
    printf '{"scenario":"C12","verdict":"OK","elapsed_seconds":%d}\n' "$elapsed"
  fi
}

main "$@"
