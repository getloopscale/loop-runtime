#!/usr/bin/env bash
# Test pyramid validation per ADR D7.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T6.3 + EC-12.
#
# Pyramid ratios required:
#   unit ≥70%, integration ≤25%, e2e ≤5%
#
# EC-12: PR with `pyramid-defer` label allows one-PR override + auto-issue
#
# Env vars:
#   INVENTORY_FILE      — JSON from test-inventory.sh (default: infra/quality/test-inventory.json)
#   PR_LABELS           — comma-separated label list
#   UNIT_MIN_PCT        — minimum unit % (default: 70)
#   INTEGRATION_MAX_PCT — maximum integration % (default: 25)
#   E2E_MAX_PCT         — maximum e2e % (default: 5)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
INVENTORY_FILE="${INVENTORY_FILE:-$REPO_ROOT/infra/quality/test-inventory.json}"
PR_LABELS="${PR_LABELS:-}"
UNIT_MIN_PCT="${UNIT_MIN_PCT:-70}"
INTEGRATION_MAX_PCT="${INTEGRATION_MAX_PCT:-25}"
E2E_MAX_PCT="${E2E_MAX_PCT:-5}"

# EC-12: defer label
IFS=',' read -ra LABELS_ARR <<< "$PR_LABELS"
for lbl in "${LABELS_ARR[@]}"; do
  lbl="$(echo "$lbl" | tr -d ' ')"
  if [ "$lbl" = "pyramid-defer" ]; then
    echo "[pyramid] EC-12: 'pyramid-defer' label — PASS (auto-issue will be opened)"
    exit 0
  fi
done

if [ ! -f "$INVENTORY_FILE" ]; then
  echo "[pyramid] ERROR: inventory file not found: $INVENTORY_FILE" >&2
  echo "[pyramid] run 'bash infra/scripts/quality/test-inventory.sh' first" >&2
  exit 1
fi

python3 - "$INVENTORY_FILE" "$UNIT_MIN_PCT" "$INTEGRATION_MAX_PCT" "$E2E_MAX_PCT" <<'PY'
import json, sys

inv_path, unit_min, int_max, e2e_max = sys.argv[1:]
unit_min = float(unit_min)
int_max = float(int_max)
e2e_max = float(e2e_max)

with open(inv_path) as f:
    inv = json.load(f)

s = inv["summary"]
unit_pct = s["ratio_unit_pct"]
int_pct = s["ratio_integration_pct"]
e2e_pct = s["ratio_e2e_pct"]

violations = []
if unit_pct < unit_min:
    violations.append(f"unit {unit_pct:.1f}% < {unit_min:.0f}% required")
if int_pct > int_max:
    violations.append(f"integration {int_pct:.1f}% > {int_max:.0f}% max")
if e2e_pct > e2e_max:
    violations.append(f"e2e {e2e_pct:.1f}% > {e2e_max:.0f}% max")

if violations:
    print(f"[pyramid] FAIL: pyramid out of bounds:", file=sys.stderr)
    for v in violations:
        print(f"  {v}", file=sys.stderr)
    print("[pyramid] use 'pyramid-defer' label for one-PR override", file=sys.stderr)
    sys.exit(1)

print(f"[pyramid] PASS: unit={unit_pct:.1f}% integration={int_pct:.1f}% e2e={e2e_pct:.1f}%")
PY
