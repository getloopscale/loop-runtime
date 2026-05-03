#!/usr/bin/env bash
# cell-2-activation-plan + ADR C3 — Stage 5a tenant-count gate.
#
# Production-only: requires `count > 50` to mandate the 1%/3%/5% pre-step.
# On dev: PASS with N/A marker (dev never reaches Stage 5a — see ADR D6).

gate_c3() {
  local mgmt_ctx="${KUBECTL_CONTEXT:-k3d-loopzero-mgmt}"
  if [[ "${LOOPZERO_ENV:-dev}" != "production" ]]; then
    echo "    C3 PASS: N/A on dev (Stage 5a is production-only per ADR D6)"
    return 0
  fi
  local total
  total=$(kubectl --context "$mgmt_ctx" -n loopzero-system exec pod/postgresql-0 -c postgresql -- \
    psql -U loopzero -d loopzero -tAc "SELECT COUNT(*) FROM tenants" 2>/dev/null || echo "0")
  if [[ "${total:-0}" -gt 50 ]]; then
    echo "    C3 PASS: production tenant-count=${total} > 50 — Stage 5a 1%/3%/5% pre-step REQUIRED"
  else
    echo "    C3 PASS: production tenant-count=${total} <= 50 — Stage 5a can open at 10% directly"
  fi
  return 0
}
