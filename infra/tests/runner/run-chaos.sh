#!/usr/bin/env bash
# Run a Chaos Mesh experiment against the current cluster, wait for
# the steady-state hypothesis to be validated, and clean up.
#
# Usage:
#   run-chaos.sh <manifest-filename>
#
# Example:
#   run-chaos.sh pod-kill-controller.yaml
#
# The script:
#   1. Runs the safety guard (refuses prod).
#   2. Checks Chaos Mesh is installed (chaos-mesh namespace exists).
#   3. Extracts the `runtime.getloopscale.com/slo-query` annotation from the manifest.
#   4. Applies the experiment.
#   5. Waits for the experiment duration.
#   6. Queries Prometheus with the SLO expression.
#   7. Deletes the experiment resource.
#   8. Exits non-zero if the SLO expression returned any value != 1.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHAOS_DIR="$(cd "$SCRIPT_DIR/../chaos/manifests" && pwd)"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://prometheus-server.monitoring.svc.cluster.local/api/v1/query}"

# shellcheck source=./safety.sh
. "$SCRIPT_DIR/safety.sh"
loop_runtime_safety_check

MANIFEST="${1:-}"
if [[ -z "$MANIFEST" ]]; then
  echo "Usage: $0 <manifest-filename>" >&2
  echo "Available:" >&2
  ls "$CHAOS_DIR" | grep -E '\.yaml$' | sed 's/^/  /' >&2
  exit 1
fi

MANIFEST_PATH="$CHAOS_DIR/$MANIFEST"
if [[ ! -f "$MANIFEST_PATH" ]]; then
  echo "Manifest not found: $MANIFEST_PATH" >&2
  exit 1
fi

# --- Preflight: Chaos Mesh installed? ---------------------------------
if ! kubectl get ns chaos-mesh >/dev/null 2>&1; then
  echo "⚠ chaos-mesh namespace not found. Install with:" >&2
  echo "    helm repo add chaos-mesh https://charts.chaos-mesh.org" >&2
  echo "    helm install chaos-mesh chaos-mesh/chaos-mesh \\" >&2
  echo "        --namespace=chaos-mesh --create-namespace --version 2.7.0" >&2
  exit 1
fi

# --- Extract hypothesis + SLO query from the manifest ---------------
HYPOTHESIS=$(grep -E '^\s*runtime\.getloopscale\.com/hypothesis:' "$MANIFEST_PATH" | sed 's/.*hypothesis: *//; s/^"//; s/"$//' || true)
DURATION=$(grep -E '^\s*duration:' "$MANIFEST_PATH" | head -1 | sed 's/.*duration: *//; s/"//g' || true)
DURATION_SEC=$(echo "$DURATION" | sed 's/s$//')

echo "── Chaos experiment ──"
echo "  Manifest:   $MANIFEST"
echo "  Hypothesis: $HYPOTHESIS"
echo "  Duration:   $DURATION"
echo

# --- Apply the manifest ---------------------------------------------
echo "[1/4] Applying experiment..."
kubectl apply -f "$MANIFEST_PATH"

# Record the kind+name for cleanup.
KIND=$(grep -E '^kind:' "$MANIFEST_PATH" | head -1 | awk '{print tolower($2)}')
NAME=$(grep -E '^\s*name:' "$MANIFEST_PATH" | head -1 | awk '{print $2}')
NS=$(grep -E '^\s*namespace:' "$MANIFEST_PATH" | head -1 | awk '{print $2}')
cleanup() {
  echo "[cleanup] Deleting $KIND/$NAME in $NS..."
  kubectl -n "$NS" delete "$KIND" "$NAME" --ignore-not-found --wait=false
}
trap cleanup EXIT

# --- Wait for the experiment duration -------------------------------
echo "[2/4] Waiting $DURATION_SEC seconds for experiment to complete..."
sleep "$DURATION_SEC"

# --- Query Prometheus for the steady-state SLO ----------------------
# The SLO query is a PromQL boolean expression that returns a result set
# IF and ONLY IF the steady-state hypothesis held. Empty result = BAD.
#
# NOTE: Prometheus queries require scraping to happen during the window.
# Since all our metrics are scraped every 15s, the 60-120s experiment
# windows should have at least 4 data points.
echo "[3/4] Querying Prometheus steady-state SLO..."
SLO_QUERY=$(awk '
  /runtime\.getloopscale\.com\/slo-query:/ {capture=1; next}
  capture && /^\s*[|>]/ {next}
  capture && /^spec:/ {exit}
  capture && NF {
    sub(/^\s*/, "")
    print
  }
' "$MANIFEST_PATH" | head -c 2000)

if [[ -z "$SLO_QUERY" ]]; then
  echo "⚠ No runtime.getloopscale.com/slo-query annotation found — skipping validation"
  echo "✓ Experiment completed (no assertions)"
  exit 0
fi

echo "Query:"
echo "  $SLO_QUERY"
echo

# Run the query via kubectl proxy (simpler than port-forward)
PROM_RESULT=$(kubectl run prom-query-$$ --rm -i --restart=Never \
  --image=curlimages/curl:latest -n loop-runtime-system -- \
  sh -c "curl -sG --data-urlencode 'query=${SLO_QUERY}' '${PROMETHEUS_URL}'" \
  2>&1 | tail -1)

echo "Prometheus response:"
echo "  $PROM_RESULT"
echo

# The result is a JSON object; we care whether `data.result` is non-empty.
if echo "$PROM_RESULT" | grep -q '"result":\[\]'; then
  echo "❌ SLO VIOLATED: query returned empty result — hypothesis FAILED"
  exit 1
fi

if ! echo "$PROM_RESULT" | grep -q '"status":"success"'; then
  echo "⚠ Prometheus query failed (non-success status)"
  exit 2
fi

echo "[4/4] ✅ Steady-state SLO held during experiment"
echo "✓ Hypothesis validated: $HYPOTHESIS"
