# Multi-Cell Alerts Runbook

> Plan reference: `docs/plans/infra-normalize-multicell-plan.md` Phase 5 T5.4 (D6 mandate).
> Source alerts: `infra/helm/monitoring/manifests/prometheusrule-multi-cell.yaml`.
> Last updated: 2026-04-30.

This runbook is the destination of every `runbook_url` annotation on the multi-cell alert rules. On-call follows the section matching the alertname. Each section is structured: **What** / **Why** / **Triage** / **Remediate** / **Escalate**.

---

## MultiCellRouterErrorRate

**What.** `DynamicRouter.Resolve()` is returning errors at > 0.01 decisions/sec for 5 minutes for cell `{{ $labels.cell }}`.

**Why.** Either:
1. A caller is passing an unknown `cell_id` (drift between `tenants.cell_id` and discovered cells).
2. A caller is passing empty `cell_id` (placement bug; should never happen because schema has DEFAULT 'cell-default').
3. A cell that was active recently disappeared from the router (CC CR deleted, status.connected flipped to false).

**Triage** (≤5 min):
1. `kubectl --context loopzero-mgmt get clustercredential -n loopzero-system` — list connected cells.
2. `psql -c "SELECT cell_id, COUNT(*) FROM tenants GROUP BY cell_id"` — list tenant pinning.
3. Compare: any `cell_id` in DB without matching CC = source of errors.

**Remediate**:
- If CC missing: recreate via `infra/helmfile/scripts/lib/clustercredential-create.sh` (planned T5.3b).
- If empty cell_id: grep API logs for `cell router: empty cell_id`; trace caller.
- If unknown cell_id: tenants drifted; either recreate cell or `UPDATE tenants SET cell_id='cell-default' WHERE cell_id='{stale}';` (only after capacity check on cell-default).

**Escalate.** SRE on-call + platform-architect if cell DB ↔ CC drift cannot be resolved in 30 min.

---

## CellUnhealthy

**What.** `loopzero_cell_health{cell="..."}` has been 0 for 2 minutes.

**Why.** Either CC `status.connected=false` OR active probe to cell API server failed.

**Triage**:
```
kubectl --context loopzero-mgmt get clustercredential -n loopzero-system <cell-name> -o yaml
# Check status.connected, status.lastCheckedAt, status.conditions
kubectl --context loopzero-mgmt logs -n loopzero-system deploy/loopzero-platform-operator | grep -i clustercredential
```

**Remediate**:
- Stale credentials: rotate per `docs/runbooks/cell-credentials-rotation.md` (planned T5.1).
- API server unreachable: confirm cell DOKS cluster up via `doctl kubernetes cluster get <name>`.
- Network partition: check VPC peering / firewall.

**Escalate.** If credential rotation does not restore: page DOKS provider escalation channel.

---

## ClusterCredentialDisconnected

**What.** `loopzero_cluster_credential_status{connected="false"} == 1` for cell `{{ $labels.cell }}` for 1 minute.

**Why.** ClusterCredential controller observed the backing cluster's API server is unreachable OR kubeconfig is invalid.

**Triage**:
```
kubectl --context loopzero-mgmt describe clustercredential -n loopzero-system <cell-name>
# Look at Events + Conditions
```

**Remediate**:
- Kubeconfig expired/rotated → update Secret referenced by CC, controller re-probes within 60s.
- Network reach lost → check VPC + DOKS firewall; restore connectivity.

**Escalate.** If repeated flapping (>3 transitions in 1h): page SRE; investigate underlying network/cluster instability.

---

## CellCapacityWarning

**What.** Cell `{{ $labels.cell }}` has > 800 pods (warn threshold) for 10 minutes.

**Why.** Tenants schedule pods proportional to deploys + replicas. Cell is approaching saturation.

**Triage**:
```
kubectl --context k3d-cell-X get pods -A | wc -l
psql -c "SELECT COUNT(*) FROM tenants WHERE cell_id='{cell-name}'"
```

**Remediate**:
- Plan: provision new cell (cell-N+1) via Pulumi.
- Stop new tenant pinning: `psql -c "UPDATE cells SET status='draining' WHERE cell_id='{cell-name}'"`.
- Existing tenants stay; new tenants → next available cell.

**Escalate.** SRE + platform-architect for capacity expansion timeline.

---

## CellCapacityCritical

**What.** Cell `{{ $labels.cell }}` has > 1500 pods (critical threshold) for 5 minutes.

**Why.** Cell is at hard capacity; further pods may fail to schedule.

**Triage**: same as CellCapacityWarning.

**Remediate**:
- IMMEDIATELY set status='draining' (if not already).
- Page SRE for emergency capacity (provision new cell or scale existing nodes).
- Notify customers on this cell of potential degraded service via status page.

**Escalate.** This is a sev-1. Page on-call + IC + customer comms.

---

## CellDiscoveryEmpty

**What.** `loopzero_cell_discovery_count == 0` for 2 minutes — router sees zero cells.

**Why.** Either no CC CRs with `status.connected=true` exist, OR K8sCellStore lost K8s API access.

**Triage**:
```
kubectl --context loopzero-mgmt get clustercredential -n loopzero-system
# Should list ≥1 with status.connected=true
kubectl --context loopzero-mgmt logs -n loopzero-system deploy/loopzero-api | grep "cell router"
```

**Remediate**:
- If router previously had cells, it should have refused the empty refresh and kept the cached set (stale-data protection at `dynamic_router.go:100-106`). Logs say "refusing to clear N known cells". So this alert in principle only fires on a cold start.
- If cold start: the bootstrap step that creates CCs (planned T5.3b in `wire-clusters.sh` → `clustercredential-create.sh`) failed. Re-run the wiring script.
- If K8s API access lost: API pod's RBAC may be broken; confirm `loopzero-api` SA has `loopzero.io/clustercredentials` list/watch permissions.

**Escalate.** Sev-2. Page SRE + on-call.

---

## OrphanedTenants

**What.** `loopzero_orphaned_tenants > 0` for 5 minutes — tenants reference cells the router can't see.

**Why.** Tenant `cell_id` was set to some `cell-X` that has since been removed (CC deleted or `status.connected` flipped false).

**Triage**:
```
psql -c "
  SELECT tenants.id, tenants.cell_id
  FROM tenants
  WHERE tenants.cell_id NOT IN (
    SELECT spec_cellID  -- TODO: bridge from CC CR list
  )
"
```
(The SQL bridge requires a sync mechanism — current implementation: API queries the in-memory router map. If the orphan count is non-zero, expect at least one tenant rows whose cell_id ≠ any discovered cell.)

**Remediate**:
- Restore cell: recreate CC CR.
- OR migrate tenants: `UPDATE tenants SET cell_id='cell-default' WHERE cell_id='{stale-cell-id}';` (after capacity check).

**Escalate.** SRE if migration affects > 5 tenants.

---

## Pre-Stage-1 (canary T5.6) checks

Before flipping cell-2 to active for the synthetic tenant canary:

1. ✅ All metrics emit:
   ```
   curl localhost:9090/api/v1/query?query=loopzero_cell_discovery_count
   curl localhost:9090/api/v1/query?query=loopzero_cell_routing_decisions_total
   curl localhost:9090/api/v1/query?query=loopzero_cell_health
   curl localhost:9090/api/v1/query?query=loopzero_cluster_credential_status
   curl localhost:9090/api/v1/query?query=loopzero_cell_capacity_pods
   ```
   Each should return ≥1 series.
2. ✅ All alerts have rules loaded:
   ```
   kubectl --context loopzero-mgmt get prometheusrule -n monitoring loopzero-multi-cell-alerts -o yaml | grep alert: | wc -l
   # Expected: 7
   ```
3. ✅ Runbook URL annotations resolve (manual: open one URL, confirm 200).

---

## References

- `infra/helm/monitoring/manifests/prometheusrule-multi-cell.yaml` (source alerts)
- `api/internal/metrics/metrics.go` (CellRoutingDecisions, CellHealth, CellCapacityPods, ClusterCredentialStatus)
- `api/internal/placement/dynamic_router.go` (Resolve() + stale-data protection)
- `docs/decisions/MEET-INFRA-NORMALIZE-T3-multi-cell.md` (C13-C18 SRE conditions; this runbook is condition C18 deliverable)
- `docs/audits/multi-cell-source-audit.md` (action item A5: ClusterCredentialStatus metric — shipped)
