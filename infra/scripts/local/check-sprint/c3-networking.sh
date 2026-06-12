#!/usr/bin/env bash
# c3-networking check-sprint category: Validate cross-cluster DNS
# Per T4.1 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
# SCAFFOLD — operator extends with additional probes as canonical state evolves.

set -uo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

# Single canonical probe (scaffold seed). Operator adds more progressively.
if kubectl --context=k3d-loopzero-mgmt -n kube-system get cm coredns >/dev/null 2>&1; then
  echo "PASS c3-networking (canonical probe OK)"
  exit 0
else
  echo "FAIL c3-networking (canonical probe failed)"
  exit 1
fi
