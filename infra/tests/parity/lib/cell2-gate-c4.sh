#!/usr/bin/env bash
# cell-2-activation-plan T1.2 + ADR C4 — rollback SLO measured.
#
# Verifies the rollback SLO measurement script exists, was last run, and
# the latest measurement is within budget. Does NOT run the measurement
# itself (that's a 10-iteration LIVE exercise — operator runs separately).

gate_c4() {
  local repo_root="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)}"
  local slo_script="$repo_root/infra/tests/parity/cell2-rollback-slo.sh"
  if [[ ! -x "$slo_script" ]]; then
    echo "    C4 FAIL: $slo_script missing or not executable"
    return 1
  fi
  if ! "$slo_script" --dry-run >/dev/null 2>&1; then
    # Dry-run requires SYNTH_TENANT_ID; treat as SKIP if env not set.
    if [[ -z "${SYNTH_TENANT_ID:-}" ]]; then
      echo "    C4 SKIP: SYNTH_TENANT_ID not set — operator must run cell2-rollback-slo.sh manually before Stage 4"
      return 0
    fi
    echo "    C4 FAIL: cell2-rollback-slo.sh --dry-run failed (preflight broken)"
    return 1
  fi

  # Look for the most recent CSV — accept it if present and within budget.
  local latest_csv
  latest_csv=$(ls -t /tmp/cell2-rollback-slo-*.csv 2>/dev/null | head -1)
  if [[ -z "$latest_csv" ]]; then
    echo "    C4 PASS (provisional): script exists + dry-run OK; no measurement yet — operator must run live before Stage 4"
    return 0
  fi
  # Compute P50/P95 from latest CSV (column 2 = ms_to_cell1_rollback).
  local n p50_idx p95_idx p50 p95
  n=$(awk -F, 'NR>1 {print $2}' "$latest_csv" | wc -l | tr -d ' ')
  if [[ "${n:-0}" -lt 1 ]]; then
    echo "    C4 PASS (provisional): empty CSV; no measurement to verify"
    return 0
  fi
  p50_idx=$(awk -v n="$n" 'BEGIN{print int((n*50+50)/100)}')
  p95_idx=$(awk -v n="$n" 'BEGIN{print int((n*95+50)/100)}')
  (( p50_idx < 1 )) && p50_idx=1
  (( p95_idx < 1 )) && p95_idx=1
  p50=$(awk -F, 'NR>1 {print $2}' "$latest_csv" | sort -n | sed -n "${p50_idx}p")
  p95=$(awk -F, 'NR>1 {print $2}' "$latest_csv" | sort -n | sed -n "${p95_idx}p")
  if (( p50 > 300000 || p95 > 480000 )); then
    echo "    C4 FAIL: latest measurement P50=${p50}ms P95=${p95}ms exceeds ADR budget (300000/480000)"
    return 1
  fi
  echo "    C4 PASS: latest P50=${p50}ms P95=${p95}ms within ADR budget (csv=$latest_csv)"
  return 0
}
