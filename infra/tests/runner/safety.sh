#!/usr/bin/env bash
# Safety guard — refuses to run load/chaos against production clusters.
#
# Every runner command sources this file before doing anything
# dangerous. The guard checks two things:
#
#   1. The current kubectl context name does NOT contain "prod".
#   2. The cluster's loopruntime-system namespace does NOT have the
#      runtime.getloopscale.com/environment=production label (double check).
#
# If either check fails, the script exits non-zero and prints a
# human-readable explanation. There is no --force flag by design —
# running chaos on production is never acceptable.

set -euo pipefail

loopruntime_safety_check() {
  local context
  context=$(kubectl config current-context 2>/dev/null || true)

  if [[ -z "$context" ]]; then
    echo "ERROR: no kubectl context set. Refusing to run tests." >&2
    return 1
  fi

  if echo "$context" | grep -qiE "prod|production"; then
    echo "╔══════════════════════════════════════════════════════╗" >&2
    echo "║  REFUSING to run tests: context '$context'" >&2
    echo "║  contains 'prod'. Chaos/load experiments are for " >&2
    echo "║  dev/staging only. NEVER run in production. " >&2
    echo "╚══════════════════════════════════════════════════════╝" >&2
    return 1
  fi

  # Double-check: read the namespace label from the API server.
  local env_label
  env_label=$(kubectl get ns loopruntime-system \
    -o jsonpath='{.metadata.labels.runtime\.getloopscale\.com/environment}' 2>/dev/null || true)

  if [[ "$env_label" == "production" ]]; then
    echo "╔══════════════════════════════════════════════════════╗" >&2
    echo "║  REFUSING to run tests: loopruntime-system namespace" >&2
    echo "║  is labeled runtime.getloopscale.com/environment=production." >&2
    echo "║  Chaos/load experiments are NEVER allowed here." >&2
    echo "╚══════════════════════════════════════════════════════╝" >&2
    return 1
  fi

  echo "✓ Safety check passed: context=$context, env=${env_label:-unset}"
  return 0
}

# Run the check if this script is invoked directly.
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  loopruntime_safety_check
fi
