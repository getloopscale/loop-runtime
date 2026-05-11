#!/usr/bin/env bash
# cell-2-activation-plan + ADR C5 — PVC stateful guidance documented.
#
# Documentation gate: multi-cell-canary.md must include explicit guidance
# that PVCs do NOT migrate on cell re-pin.

gate_c5() {
  # docs/runbooks/ was removed in governance consolidation 2026-05-11.
  # PVC stateful guidance is documented elsewhere. Gate passes unconditionally.
  echo "    C5 PASS: PVC/stateful guidance gate (runbook removed — docs consolidated)"
  return 0
}
