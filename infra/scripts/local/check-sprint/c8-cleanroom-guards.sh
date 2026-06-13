#!/usr/bin/env bash
# c8-cleanroom-guards check-sprint category: Validate Tilt not stale
# Per T4.1 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
# SCAFFOLD — operator extends with additional probes as canonical state evolves.

set -uo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

# Single canonical probe (scaffold seed). Operator adds more progressively.
if pgrep -f "tilt up" >/dev/null >/dev/null 2>&1; then
  echo "PASS c8-cleanroom-guards (canonical probe OK)"
  exit 0
else
  echo "FAIL c8-cleanroom-guards (canonical probe failed)"
  exit 1
fi
