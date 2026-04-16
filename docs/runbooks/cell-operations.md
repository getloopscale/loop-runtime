# Runbook: Cell Operations

> ADR-0008: Cell-Based Architecture
> Each Cell is a self-contained workload unit (~5k apps max).
> This runbook covers all operational procedures for Cell lifecycle management.

---

## 1. Provision a New Cell

### Pre-requisites
- Pulumi CLI installed and authenticated
- DigitalOcean API token configured
- Access to the management cluster (`k3d-loopzero-mgmt` or DO mgmt)

### Steps

```bash
# 1. Add Cell config to Pulumi stack
pulumi config set --path 'loopzero:cells[1]' --secret '{
  "cellId": "cell-sfo3-02",
  "region": "sfo3",
  "nodeSize": "s-2vcpu-4gb",
  "nodeCount": 2,
  "baseDomain": "dev.loopzero.example"
}'

# 2. Preview changes
cd infra/pulumi && npx pulumi preview --stack dev

# 3. Apply
npx pulumi up --stack dev --yes

# 4. Verify cluster is ready
KUBECONFIG=$(pulumi stack output cell_sfo3_02_kubeconfig --show-secrets) kubectl get nodes

# 5. Create ClusterCredential CR on management cluster
kubectl apply -f - <<EOF
apiVersion: loopzero.io/v1
kind: ClusterCredential
metadata:
  name: cell-sfo3-02
spec:
  clusterName: cell-sfo3-02
  apiServer: https://<cluster-endpoint>:6443
  cellID: cell-sfo3-02
  secretRef:
    name: cell-sfo3-02-kubeconfig
    namespace: loopzero-system
  scope:
    argocd: true
    operators: true
EOF

# 6. Verify Cell health
kubectl get clustercredential cell-sfo3-02 -o jsonpath='{.status.connected}'
# Expected: true
```

### Verification
- `kubectl get nodes` on new Cell shows Ready nodes
- `kubectl get clustercredential cell-sfo3-02` shows `Connected: true`
- `loopzero_cell_tenant_count{cell_id="cell-sfo3-02"}` appears in Prometheus (value: 0)
- ArgoCD in new Cell is healthy: `kubectl -n argocd get pods` (all Running)

### Rollback
```bash
# If provisioning fails, destroy the Cell
npx pulumi destroy --stack dev --target cell-sfo3-02
kubectl delete clustercredential cell-sfo3-02
```

---

## 2. Migrate a Tenant Between Cells

### Pre-requisites
- Source Cell and target Cell both healthy
- Tenant has no in-flight deploys (`loopzero status` shows no PENDING/RUNNING builds)

### Steps

```bash
# 1. Mark tenant as migrating (prevents new deploys)
# API: PUT /internal/tenants/{tenantID}/migrate
curl -X PUT https://api.dev.loopzero.example/internal/tenants/tn-acme/migrate \
  -H "Authorization: Bearer $BOOTSTRAP_SECRET" \
  -d '{"target_cell_id": "cell-sfo3-02"}'

# 2. Wait for migration to complete
# The system will:
#   a. Sync all Gitea manifests to target Cell's ArgoCD
#   b. Wait for target Cell ArgoCD healthy
#   c. Update DNS/CF KV routing (tenant → new Cell)
#   d. Wait for DNS propagation
#   e. Remove tenant from source Cell's ArgoCD
#   f. Update tenant.cell_id in PostgreSQL
#   g. Mark tenant as active

# 3. Verify
loopzero status --cells  # Shows tenant count shifted
```

### Verification
- `loopzero deploy` works for the migrated tenant (targets new Cell)
- `loopzero logs` returns logs from the new Cell
- `loopzero rollback` works from the new Cell
- Old Cell shows 0 resources for this tenant

### Rollback
```bash
# If migration fails mid-way, the tenant stays in the source Cell
# Both Cells may serve traffic during migration (blue-green)
# To force rollback:
curl -X PUT https://api.dev.loopzero.example/internal/tenants/tn-acme/migrate \
  -H "Authorization: Bearer $BOOTSTRAP_SECRET" \
  -d '{"target_cell_id": "cell-default"}'  # back to original
```

---

## 3. Decommission a Cell

### Pre-requisites
- Cell has ZERO tenants (all migrated out)
- No in-flight deploys targeting this Cell

### Steps

```bash
# 1. Verify Cell is empty
loopzero status --cells
# cell-sfo3-02 should show 0 tenants, 0 apps

# 2. Delete ClusterCredential
kubectl delete clustercredential cell-sfo3-02

# 3. Remove from Pulumi config
pulumi config rm --path 'loopzero:cells[1]'

# 4. Destroy infrastructure
npx pulumi up --stack dev --yes
# This removes: DOKS cluster, ArgoCD, Traefik, cert-manager

# 5. Verify
loopzero status --cells  # Cell no longer listed
```

### Verification
- `loopzero status --cells` does not show the decommissioned Cell
- No orphaned resources in DigitalOcean console
- No alerts firing for missing Cell

### Rollback
```bash
# Re-provision using Step 1 of "Provision a New Cell"
```

---

## 4. Handle Cell Failure (Failover)

### Symptoms
- Alert: `LoopZeroCellUnhealthy` fires for a Cell
- `kubectl get nodes` shows NotReady
- Deploys to tenants in the failed Cell fail with timeout

### Steps

```bash
# 1. Assess severity
kubectl --context cell-sfo3-02 get nodes
kubectl --context cell-sfo3-02 get pods -n argocd

# 2. If cluster is recoverable (node failure, not total loss):
# Wait for DigitalOcean auto-healing (replaces failed nodes)
# Monitor: kubectl get nodes --watch

# 3. If cluster is NOT recoverable (total loss):
# Option A: Re-provision the Cell (same config)
npx pulumi up --stack dev --yes

# Option B: Migrate tenants to other Cells
# For each tenant in the failed Cell:
curl -X PUT https://api.dev.loopzero.example/internal/tenants/{tenantID}/migrate \
  -H "Authorization: Bearer $BOOTSTRAP_SECRET" \
  -d '{"target_cell_id": "cell-default"}'
```

### Verification
- All tenants in the affected Cell can deploy after recovery/migration
- No data loss (PostgreSQL is shared, not per-Cell)
- Gitea repos intact (shared, not per-Cell)

---

## 5. Scale a Cell (Add Nodes)

### Steps

```bash
# 1. Update node count in Pulumi config
pulumi config set --path 'loopzero:cells[0].nodeCount' 4

# 2. Apply
npx pulumi up --stack dev --yes

# 3. Verify
kubectl --context cell-default get nodes
# Should show 4 Ready nodes
```

### Verification
- New nodes are Ready
- Pods are scheduled on new nodes
- No disruption to running workloads

---

## 6. Verify Health of All Cells

### CLI

```bash
# Operator view
loopzero status --cells

# Expected output:
# CELL            TENANTS  APPS   DEPLOY   CLUSTER
# cell-default    45       120    Healthy  Ready
# cell-sfo3-02    12       35     Healthy  Ready
```

### Prometheus Queries

```promql
# Tenant count per Cell
loopzero_cell_tenant_count

# Cell capacity usage (%)
loopzero_cell_tenant_count / 500 * 100

# ArgoCD sync latency per Cell (p95)
histogram_quantile(0.95, sum(rate(argocd_app_reconcile_duration_seconds_bucket[5m])) by (le, cluster))

# Releases pending per Cell
loopzero_releases_pending
```

### Grafana
- Dashboard: "Cell Health — ADR-0008"
- Panels: tenant count, capacity %, pod count, sync latency, sync failures

---

## 7. Query Cell Metrics

### Key Metrics

| Metric | Description | Alert |
|--------|-------------|-------|
| `loopzero_cell_tenant_count` | Tenants per Cell | Warning at 400, Critical at 500 |
| `loopzero_release_state_divergence` | PG vs K8s divergence per Cell | Critical at 1 for 5min |
| `loopzero_reconcile_loop_last_success_timestamp` | Loop heartbeat | Warning if stale > 2min |
| `argocd_app_reconcile_duration_seconds` | ArgoCD sync time | Warning at p95 > 30s |

---

## 8. Troubleshooting: Cell Provisioned But Not Receiving Deploys

### Symptoms
- New Cell exists in `loopzero status --cells`
- New tenants are NOT assigned to the new Cell
- `loopzero_cell_tenant_count{cell_id="new-cell"}` stays at 0

### Diagnosis

```bash
# 1. Check PlacementEngine configuration
# Is the Cell registered in ClusterCredential?
kubectl get clustercredential -o wide

# 2. Check Cell connectivity
kubectl get clustercredential <cell-id> -o jsonpath='{.status.connected}'
# Must be "true"

# 3. Check CF KV routing
# Is the Cell's ingress IP registered?
# Check via CF API or Worker logs

# 4. Check PlacementEngine logs
kubectl logs -l app=loopzero-api -n loopzero-system | grep "placement"
```

### Fix
- If `connected: false`: check kubeconfig secret, network connectivity
- If routing missing: re-run `PutCellRouting` via API
- If PlacementEngine not selecting: verify Cell labels match tenant requirements

---

## 9. Troubleshooting: Migration Stuck in `migrating` State

### Symptoms
- Tenant shows status `migrating` for > 30 minutes
- No progress in migration steps

### Diagnosis

```bash
# 1. Check which step is stuck
kubectl logs -l app=loopzero-api -n loopzero-system | grep "migrate" | grep "<tenant-id>"

# 2. Common causes:
# a. Target Cell ArgoCD not syncing — check ArgoCD health in target Cell
# b. DNS propagation stuck — check CF KV entry
# c. Source Cell ArgoCD cannot delete resources — check finalizers
```

### Fix

```bash
# Force complete the migration (skips remaining steps)
curl -X PUT https://api.dev.loopzero.example/internal/tenants/{tenantID}/migrate/force-complete \
  -H "Authorization: Bearer $BOOTSTRAP_SECRET"

# If that fails, manually fix:
# 1. Update tenant.cell_id in PG:
#    UPDATE tenants SET cell_id = 'target-cell' WHERE id = 'tenant-id';
# 2. Update CF KV routing
# 3. Clean up source Cell ArgoCD applications manually
```

### Prevention
- Ensure target Cell is healthy before starting migration
- Run migration during low-traffic periods
- Monitor `loopzero_tenant_migration_duration` metric
