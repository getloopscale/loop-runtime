#!/usr/bin/env bash
# validate-themory-wireup.sh — confirm loopruntime-cloud Pod consumed THEMORY_API_KEY
# Secret + instantiated memoryHandler (memory proxy enabled log line).
#
# Per plan v1.3 § Phase 4 / T4.1 of loopruntime-cloud-themory-e2e-finalize-plan.md.
# Run AFTER `tilt up` brought the Pod to Ready.
#
# Sequence:
#   1. EC-11: Secret existence guard (avoids Pod stuck in CreateContainerConfigError)
#   2. Wait for loopruntime-cloud Pod Ready (kubectl wait, 120s timeout)
#   3. EC-7: tail Pod logs with --since=2m (avoids stale evidence from prior restarts)
#   4. Assert grep "memory proxy enabled" — the canonical wired-OK signal from
#      cloud/cmd/main.go:185
#   5. Negative assertion: refuse if "THEMORY_BASE_URL not set" present in same window
#   6. Print success + exit 0

set -euo pipefail

readonly K8S_NAMESPACE="${K8S_NAMESPACE:-loopruntime-system}"
readonly K8S_SECRET_NAME="${K8S_SECRET_NAME:-themory-dev-key}"
readonly POD_LABEL="${POD_LABEL:-app=loopruntime-cloud}"
readonly POD_READY_TIMEOUT="${POD_READY_TIMEOUT:-120s}"
readonly LOG_SINCE="${LOG_SINCE:-2m}"
readonly LOG_GREP_TIMEOUT="${LOG_GREP_TIMEOUT:-60}"
readonly EXPECT_LINE="${EXPECT_LINE:-memory proxy enabled}"
readonly NEGATIVE_LINE="${NEGATIVE_LINE:-THEMORY_BASE_URL not set}"

readonly C_RED='\033[0;31m'
readonly C_GREEN='\033[0;32m'
readonly C_YELLOW='\033[0;33m'
readonly C_RESET='\033[0m'

log_info()  { printf "%b[INFO]%b  %s\n" "$C_GREEN" "$C_RESET" "$1" >&2; }
log_warn()  { printf "%b[WARN]%b  %s\n" "$C_YELLOW" "$C_RESET" "$1" >&2; }
log_fatal() { printf "%b[FATAL]%b %s\n" "$C_RED" "$C_RESET" "$1" >&2; exit 1; }

ensure_secret_present() {
  kubectl -n "$K8S_NAMESPACE" get secret "$K8S_SECRET_NAME" >/dev/null 2>&1 \
    || log_fatal "Secret $K8S_SECRET_NAME missing in $K8S_NAMESPACE — run bootstrap-themory-key.sh first (else Pod stuck in CreateContainerConfigError)"
  log_info "Secret $K8S_SECRET_NAME present"
}

wait_for_pod_ready() {
  log_info "Waiting up to $POD_READY_TIMEOUT for loopruntime-cloud Pod (label $POD_LABEL) Ready"
  if ! kubectl wait --for=condition=ready pod \
        -l "$POD_LABEL" \
        -n "$K8S_NAMESPACE" \
        --timeout="$POD_READY_TIMEOUT" >/dev/null 2>&1; then
    log_warn "Pod did not reach Ready within $POD_READY_TIMEOUT — diagnostic:"
    kubectl -n "$K8S_NAMESPACE" describe pod -l "$POD_LABEL" 2>&1 \
      | grep -A 3 "CreateContainerConfigError\|ImagePullBackOff\|CrashLoopBackOff" \
      | head -10 >&2
    log_fatal "Pod not Ready — see describe excerpt above"
  fi
  log_info "Pod Ready"
}

pod_name() {
  kubectl -n "$K8S_NAMESPACE" get pods \
    -l "$POD_LABEL" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null
}

assert_memory_proxy_enabled() {
  local pod
  pod="$(pod_name)"
  [ -n "$pod" ] || log_fatal "Could not resolve pod name with label $POD_LABEL"
  log_info "Polling logs of $pod (--since=$LOG_SINCE, up to ${LOG_GREP_TIMEOUT}s)"

  local elapsed=0
  while [ "$elapsed" -lt "$LOG_GREP_TIMEOUT" ]; do
    local logs
    logs="$(kubectl -n "$K8S_NAMESPACE" logs "$pod" --since="$LOG_SINCE" 2>/dev/null || true)"

    if echo "$logs" | grep -q "$NEGATIVE_LINE"; then
      log_fatal "FOUND negative signal: '$NEGATIVE_LINE' — Secret resolution likely failed at Pod boot"
    fi
    if echo "$logs" | grep -q "$EXPECT_LINE"; then
      log_info "FOUND positive signal: '$EXPECT_LINE'"
      return 0
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done

  log_fatal "Did not observe '$EXPECT_LINE' in logs of $pod within ${LOG_GREP_TIMEOUT}s — inspect: kubectl -n $K8S_NAMESPACE logs $pod --since=$LOG_SINCE"
}

main() {
  printf "validate-themory-wireup.sh — assert memoryHandler wired in loopruntime-cloud Pod\n\n"
  ensure_secret_present
  wait_for_pod_ready
  assert_memory_proxy_enabled
  printf "\n%b✓ memoryHandler wired — proxy ready for E2E smoke%b\n" "$C_GREEN" "$C_RESET"
  printf "Next: bash infra/scripts/local/smoke-themory-e2e.sh\n"
}

main "$@"
