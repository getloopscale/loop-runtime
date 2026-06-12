#!/usr/bin/env bash
# c2-configmaps check-sprint category: Validate Kratos+Hydra+loopzero-cloud ConfigMaps
# Per T4.1 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
# SCAFFOLD — operator extends with additional probes as canonical state evolves.

set -uo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

# Single canonical probe (scaffold seed). Operator adds more progressively.
if kubectl --context=k3d-loopzero-mgmt -n loopzero-auth get cm ory-kratos-config ory-hydra >/dev/null 2>&1; then
  echo "PASS c2-configmaps (canonical probe OK)"
  exit 0
else
  echo "FAIL c2-configmaps (canonical probe failed)"
  exit 1
fi
