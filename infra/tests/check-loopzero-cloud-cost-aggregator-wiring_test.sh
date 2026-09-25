#!/usr/bin/env bash
# B-033 T1.1 hermetic gate — dev-public MUST publish LOOPZERO_PROMETHEUS_URL for cost aggregator.
#
# Root cause (BACKLOG.md § B-033):
#   LOOPZERO_PROMETHEUS_URL empty in the pod → the cost aggregator disables itself at boot
#   with `logger.Info("cost aggregator disabled")` → cost_counters stays empty → the
#   CostEnforcer middleware degrades to pass-through. The three cost metrics lose their cap.
#
# Contract asserted by this gate (hermetic — no live cluster):
#   A1  LOOPZERO_PROMETHEUS_URL is set in the loopzero-cloud ConfigMap the container mounts
#   A2  value equals http://prometheus-server.monitoring.svc.cluster.local:80 (D1 canonical)
#   A3  PARITY — value is byte-identical to the loopzero-api overlay's dataPlanePoller.prometheusUrl.
#       Absorbs EC-1 of the edge-case plan: editing one of the two files without the
#       other passes silently; A3 catches the drift by naming both values.
#
# WHAT THIS GATE DOES **NOT** PROVE — measured live on 2026-09-05, k3d-loopzero-core,
# with every assertion above PASSING:
#   - the rendered value is not the DEPLOYED value: `kubectl get deploy loopzero-cloud
#     -n loopzero-system` had 19 env vars and ZERO matching `PROM|COST` (Deployment
#     created 2026-08-10, pod started 2026-08-11, restarts=0 — the overlay change
#     of 2026-08-31 was never applied to the cluster);
#   - `select count(*) from cost_counters` returned **0**;
#   - and even with the aggregator enabled it would write nothing for two of the
#     three metrics: `loopzero_egress_bytes_total` and `loopzero_docr_pulls_total` — the
#     PromQL the aggregator runs (loopzero-cloud internal/billing/cost_aggregator.go)
#     — are emitted by NO repository (grepped across loopzero, loopzero-cloud, loopzero-engine,
#     loopzero-data-plane, loopzero-ops, loopzero-forge, loopzero-lens-reconciler, loopzero-workspace);
#     live Prometheus knows 48 `loopzero_*` series and none of the three.
# The gate is the first link of a four-link chain (overlay → deploy → aggregator →
# instrumentation). It asserts link 1. Its success line says so on purpose: a guard
# that names the whole chain while observing one link is how B-033 stayed invisible
# for three weeks after its "fix" landed in git.
#
# Sibling pattern (same class, same toolchain):
#   infra/tests/check-loopzero-cloud-rag-dashboard-wiring_test.sh
#   infra/tests/check-loopzero-cloud-lens-dashboard-wiring_test.sh

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "=== Rendering helmfile dev-public loopzero-cloud... ==="
LOOPZERO_HELMFILE_ENV=dev-public helmfile -e dev-public \
  -f infra/helmfile/core.helmfile.yaml.gotmpl \
  --selector name=loopzero-cloud \
  template > "$TMP/loopzero-cloud.yaml" 2> "$TMP/err.log" || {
    echo "FAIL — helmfile template errored (loopzero-cloud)"; cat "$TMP/err.log"; exit 1
  }

echo "=== Rendering helmfile dev-public loopzero-api... ==="
LOOPZERO_HELMFILE_ENV=dev-public helmfile -e dev-public \
  -f infra/helmfile/core.helmfile.yaml.gotmpl \
  --selector name=loopzero-api \
  template > "$TMP/loopzero-api.yaml" 2> "$TMP/err.log" || {
    echo "FAIL — helmfile template errored (loopzero-api)"; cat "$TMP/err.log"; exit 1
  }

fail=0
pass() { echo "PASS — $1"; }
fail_() { echo "FAIL — $1"; fail=$((fail + 1)); }

# --- A1/A2: LOOPZERO_PROMETHEUS_URL present on loopzero-cloud container with canonical value ---
URL=$(yq eval-all '(select(.kind == "ConfigMap" and .metadata.name == "loopzero-cloud")
  | .data.LOOPZERO_PROMETHEUS_URL) // ""' "$TMP/loopzero-cloud.yaml")

case "$URL" in
  "") fail_ "LOOPZERO_PROMETHEUS_URL absent from ConfigMap/loopzero-cloud — cost aggregator will boot-disable, cost_counters stays empty, and CostEnforcer degrades to pass-through (see BACKLOG.md § B-033)" ;;
  http://prometheus-server.monitoring.svc.cluster.local:80) pass "A1/A2 LOOPZERO_PROMETHEUS_URL=$URL (canonical value)" ;;
  *) fail_ "A1/A2 LOOPZERO_PROMETHEUS_URL='$URL' — expected 'http://prometheus-server.monitoring.svc.cluster.local:80' (D1: reuse the URL the loopzero-api overlay's readiness poller already consumes; two coordinates → silent drift)" ;;
esac

# --- A3 (parity — absorbs EC-1): loopzero-cloud URL == loopzero-api dataPlanePoller.prometheusUrl ---
# Without this assertion, editing ONE of the two files without the other passes
# silently: the two overlays point at different places and the cost aggregator breaks
# at runtime — the same class as the original B-033 defect.
#
# The loopzero-api chart turns its `dataPlanePoller.prometheusUrl` field into a variable
# with a different name (LOOPZERO_DATA_PLANE_POLLER_PROMETHEUS_URL) in the rendered
# manifest, so the most reliable comparison reads the literal value STRAIGHT from the
# dev-public overlay. The readiness poller moved from loopzero-app-operator to loopzero-api
# (docs/decisions.md, 2026-09-25), and the parity moved with it.
OP_OVERLAY="infra/helmfile/values/dev-public/loopzero-api.yaml.gotmpl"
if [ ! -f "$OP_OVERLAY" ]; then
  fail_ "A3 parity: $OP_OVERLAY does not exist — cannot compare with loopzero-cloud (plan D1 names that file as the canonical source)"
else
  OP_URL=$(grep -E '^\s*prometheusUrl:\s*"' "$OP_OVERLAY" | head -1 | sed -E 's/.*prometheusUrl:\s*"([^"]+)".*/\1/')
  if [ -z "$OP_URL" ]; then
    fail_ "A3 parity: prometheusUrl not found in $OP_OVERLAY — the D1 lockstep assertion has nothing to compare"
  elif [ "$URL" != "$OP_URL" ]; then
    fail_ "A3 parity DRIFT: loopzero-cloud.LOOPZERO_PROMETHEUS_URL='$URL' != loopzero-api.dataPlanePoller.prometheusUrl='$OP_URL'. Plan D1 reuses the SAME URL in both; editing one without the other is the failure mode this check exists to catch"
  else
    pass "A3 parity: loopzero-cloud.LOOPZERO_PROMETHEUS_URL == loopzero-api.dataPlanePoller.prometheusUrl (== $URL)"
  fi
fi

if [ "$fail" -eq 0 ]; then
  echo "OK — dev-public RENDER publishes LOOPZERO_PROMETHEUS_URL (link 1 of 4)."
  echo "     NOT asserted here: that the cluster runs this manifest, that the aggregator"
  echo "     started, or that anything emits the series it queries. See header + BACKLOG.md § B-033."
  exit 0
else
  echo "FAIL — $fail assertion(s) failed"; exit 1
fi
