#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP / T2.x — mock kubectl for offline script testing.
# Captures kubectl invocations to /tmp/mock-kubectl.log and exits 0.
#
# Usage in tests:
#   PATH="$(dirname $0):$PATH" bash <real-script.sh>
#
# Then assert on /tmp/mock-kubectl.log contents.
set -euo pipefail
MOCK_LOG="${MOCK_KUBECTL_LOG:-/tmp/mock-kubectl.log}"

# Record the invocation
{
  echo "----"
  echo "args: $*"
  if [ -t 0 ] || [ ! -p /dev/stdin ]; then
    echo "stdin: <none>"
  else
    # Read up to 32K of stdin to avoid hangs on large pipes
    head -c 32768 -
    echo ""
  fi
} >> "$MOCK_LOG"

# Provide deterministic output for read-style commands
case "$*" in
  *"get secret"*"jsonpath"*)
    # Return empty — caller uses fallback
    echo ""
    ;;
  *"get sc local-path"*)
    echo "rancher.io/local-path"
    ;;
  *"get clusterimagepolicy"*)
    echo "clusterimagepolicy/loop-runtime-images-warn"
    ;;
  *"create namespace"*"--dry-run=client"*)
    echo "namespace/test-namespace created (mock)"
    ;;
  *"create secret"*"--dry-run=client"*)
    echo "secret/test created (mock)"
    ;;
  *"apply -f"*)
    echo "applied (mock)"
    ;;
  *"cluster-info"*)
    echo "Kubernetes control plane is running at https://mock"
    ;;
  *)
    echo "(mock kubectl) $*"
    ;;
esac

exit 0
