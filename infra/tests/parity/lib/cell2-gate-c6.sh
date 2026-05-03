#!/usr/bin/env bash
# cell-2-activation-plan + ADR C6 — rollback independent of cell-2 reachability.
#
# Verifies design invariant: re-pin (DB UPDATE + CR MergePatch) does NOT
# require cell-2 cluster to be reachable. Implementation evidence:
#   - CellPinner.Pin (cell_pinner.go:117-133): mgmtKube patch is mgmt-side only
#   - admin_pin_cell handler does not contact cell-2

gate_c6() {
  local repo_root="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)}"
  local pinner="$repo_root/api/internal/services/cell_pinner.go"
  if [[ ! -r "$pinner" ]]; then
    echo "    C6 FAIL: $pinner missing"
    return 1
  fi
  # Defensive grep: pinner must not import any cell-2-specific client.
  if grep -qE "cell2Client|runtime2Client|workload2Client" "$pinner"; then
    echo "    C6 FAIL: cell_pinner.go references cell-2 specific client (rollback would depend on cell-2)"
    return 1
  fi
  # Pinner must use mgmt kube client (loopzero-system namespace).
  if ! grep -q 'mgmtNS:.*"loopzero-system"' "$pinner"; then
    echo "    C6 FAIL: cell_pinner.go missing mgmt-only patch path"
    return 1
  fi
  echo "    C6 PASS: cell_pinner mgmt-only patch path; rollback independent of cell-2"
  return 0
}
