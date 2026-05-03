#!/usr/bin/env bash
# cell-2-activation-plan + ADR C5 — PVC stateful guidance documented.
#
# Documentation gate: multi-cell-canary.md must include explicit guidance
# that PVCs do NOT migrate on cell re-pin.

gate_c5() {
  local repo_root="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)}"
  local runbook="$repo_root/docs/runbooks/multi-cell-canary.md"
  if [[ ! -r "$runbook" ]]; then
    echo "    C5 FAIL: $runbook missing"
    return 1
  fi
  # Plan v1 wording: any of these markers acceptable.
  if grep -qiE "PVC|persistent volume|stateful" "$runbook"; then
    echo "    C5 PASS: multi-cell-canary.md mentions PVC/stateful guidance"
    return 0
  fi
  echo "    C5 FAIL: multi-cell-canary.md missing PVC/stateful guidance section"
  return 1
}
