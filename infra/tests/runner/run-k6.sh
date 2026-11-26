#!/usr/bin/env bash
# Run a k6 script against the current cluster.
#
# Usage:
#   run-k6.sh <script> <seed_level> <seed_count>
#
# Example:
#   run-k6.sh scripts/l2-50-tenants.js l2 50
#
# The script:
#   1. Runs the safety guard (refuses prod).
#   2. Creates/updates the k6-scripts ConfigMap from infra/tests/performance/k6/scripts.
#   3. envsubst's the job-template with the chosen script + seed params.
#   4. Deletes any previous Job and applies the new one.
#   5. Streams the k6 logs until the Job reaches a terminal state.
#   6. Exits with the Job's success (0) or failure (non-zero) status.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
K6_DIR="$(cd "$SCRIPT_DIR/../performance/k6" && pwd)"

# shellcheck source=./safety.sh
. "$SCRIPT_DIR/safety.sh"
loop_runtime_safety_check

K6_SCRIPT="${1:-scripts/l1-10-tenants.js}"
K6_SEED_LEVEL="${2:-l1}"
K6_SEED_COUNT="${3:-10}"

NAMESPACE="loop-runtime-system"
JOB_NAME="k6-runner"

echo "── k6 runner ──"
echo "  Script:     $K6_SCRIPT"
echo "  Seed level: $K6_SEED_LEVEL"
echo "  Seed count: $K6_SEED_COUNT"
echo "  Namespace:  $NAMESPACE"
echo

# --- 1. Create/update the k6-scripts ConfigMap --------------------------
echo "[1/5] Creating k6-scripts ConfigMap from $K6_DIR/scripts + fixtures..."
kubectl -n "$NAMESPACE" create configmap k6-scripts \
  --from-file="$K6_DIR/scripts/lib/" \
  --from-file="$K6_DIR/scripts/" \
  --from-file="$K6_DIR/fixtures/" \
  --dry-run=client -o yaml | kubectl apply -f -

# --- 2. Delete previous Job (ignored if absent) ------------------------
echo "[2/5] Cleaning up previous run..."
kubectl -n "$NAMESPACE" delete job "$JOB_NAME" --ignore-not-found --wait=true

# --- 3. envsubst + apply the template ---------------------------------
echo "[3/5] Applying Job template..."
export K6_SCRIPT K6_SEED_LEVEL K6_SEED_COUNT
envsubst < "$K6_DIR/job-template.yaml" | kubectl apply -f -

# --- 4. Wait for the Job to start -------------------------------------
echo "[4/5] Waiting for Job to start..."
kubectl -n "$NAMESPACE" wait --for=condition=Ready pod \
  -l app.kubernetes.io/name=k6-runner --timeout=120s || true

# --- 5. Stream logs + wait for completion -----------------------------
echo "[5/5] Streaming logs (k6 container)..."
kubectl -n "$NAMESPACE" logs -f job/"$JOB_NAME" -c k6 || true

# Final status
STATUS=$(kubectl -n "$NAMESPACE" get job "$JOB_NAME" \
  -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' 2>/dev/null || true)
if [[ "$STATUS" == "True" ]]; then
  echo "✅ k6 run succeeded"
  exit 0
fi

FAILED=$(kubectl -n "$NAMESPACE" get job "$JOB_NAME" \
  -o jsonpath='{.status.conditions[?(@.type=="Failed")].status}' 2>/dev/null || true)
if [[ "$FAILED" == "True" ]]; then
  echo "❌ k6 run failed — check logs above"
  exit 1
fi

echo "⚠ k6 Job in unknown state"
exit 2
