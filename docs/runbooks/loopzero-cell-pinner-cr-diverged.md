# Runbook: LoopZeroCellPinnerCRPatchDiverged

## Alert

`loopzero_cell_pinner_cr_patch_fail_total > 0` for 2m.

## Severity

Warning. DB and Tenant CR diverged: `tenants.cell_id` says cell-X, but
Tenant CR `spec.cellID` says cell-Y. New EnvironmentReconciler reads the CR
and routes to cell-Y; the operator-intended placement (DB) is silently
ignored until the CR is patched.

## Quick check

```bash
# Compare DB vs CR for the affected tenant
psql "$DATABASE_URL" -c "SELECT id, cell_id FROM tenants WHERE id='<tenant_id>'"
kubectl --context loopzero-mgmt get tenant <tenant_id> -n loopzero-system -o jsonpath='{.spec.cellID}'
```

If the values differ, divergence is confirmed.

## Recovery

Re-run the pin operation (it's idempotent):

```bash
loopzero admin tenants pin --tenant <tenant_id> --cell <intended_cell>
```

If the CR patch fails again, check operator RBAC + connectivity:

```bash
kubectl --context loopzero-mgmt auth can-i patch tenants -n loopzero-system --as=system:serviceaccount:loopzero-system:loopzero-api
```

Manual fallback (last resort, audit-logged):

```bash
kubectl --context loopzero-mgmt -n loopzero-system patch tenant <tenant_id> \
  --type=merge -p '{"spec":{"cellID":"<intended_cell>"}}'
```

## References

- find_arch_005 (deep-review-remediation T1.5).
