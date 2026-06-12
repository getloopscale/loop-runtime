#!/usr/bin/env bash
# c6-observability check-sprint category: Validate Promtail+Loki
# Per T4.1 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
# SCAFFOLD — operator extends with additional probes as canonical state evolves.

set -uo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

# Single canonical probe (scaffold seed). Operator adds more progressively.
if kubectl --context=k3d-loopzero-mgmt -n monitoring get pod -l app.kubernetes.io/name=promtail --field-selector=status.phase=Running >/dev/null 2>&1; then
  echo "PASS c6-observability (canonical probe OK)"
  exit 0
else
  echo "FAIL c6-observability (canonical probe failed)"
  exit 1
fi
