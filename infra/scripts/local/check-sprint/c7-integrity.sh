#!/usr/bin/env bash
# c7-integrity check-sprint category: Validate Go AST gates
# Per T4.1 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
# SCAFFOLD — operator extends with additional probes as canonical state evolves.

set -uo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

# Single canonical probe (scaffold seed). Operator adds more progressively.
if (cd "$REPO_ROOT/cloud" && go test ./tests/unit/ -run 'TestRouteRegistration|TestHandlerErrorVisibility') >/dev/null 2>&1; then
  echo "PASS c7-integrity (canonical probe OK)"
  exit 0
else
  echo "FAIL c7-integrity (canonical probe failed)"
  exit 1
fi
