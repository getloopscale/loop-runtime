# Alert: LoopZeroCellDiscoveryEmpty

**Severity:** CRITICAL
**Fires when:** `loopzero_cell_discovery_count == 0` for 60s
**Impact:** ALL deploy, rollback, and status operations fail for ALL tenants.

## Diagnosis

1. Check ClusterCredential CRs:
   ```bash
   kubectl --context=$MGMT_CONTEXT get clustercredentials -n platform-system -o wide
   ```

2. If CRs exist but `Connected=False`:
   ```bash
   kubectl --context=$MGMT_CONTEXT describe clustercredential <name> -n platform-system
   # Check Conditions for error details
   ```

3. If zero CRs exist:
   - ClusterCredential was deleted or never created
   - Check Pulumi state: `cd infra/pulumi && npx pulumi stack export --stack dev | grep ClusterCredential`

4. Check kubeconfig Secret:
   ```bash
   kubectl --context=$MGMT_CONTEXT get secret runtime-cell-kubeconfig -n platform-system
   ```

5. Check DynamicRouter logs:
   ```bash
   kubectl --context=$MGMT_CONTEXT logs -n platform-system -l app=loopzero-api --tail=50 | grep "cell"
   ```

## Immediate Remediation

### Option 1: Rollback to single-cell mode
```bash
kubectl --context=$MGMT_CONTEXT set env deployment/loopzero-api -n platform-system LOOPZERO_SINGLE_CELL=true
# API restarts with StaticRouter — bypasses cell discovery entirely
```

### Option 2: Recreate ClusterCredential
```bash
# If CR was accidentally deleted, rerun Pulumi:
cd infra/pulumi && npx pulumi up --stack dev
# Or apply manually:
kubectl --context=$MGMT_CONTEXT apply -f docs/plans/multi-cell-default-roadmap.md  # CR YAML in Phase B
```

### Option 3: Fix kubeconfig Secret
```bash
# If DO kubeconfig expired, refresh:
doctl kubernetes cluster kubeconfig show <cluster-id> > /tmp/runtime-kubeconfig
kubectl --context=$MGMT_CONTEXT create secret generic runtime-cell-kubeconfig \
  -n platform-system --from-file=kubeconfig=/tmp/runtime-kubeconfig \
  --dry-run=client -o yaml | kubectl apply -f -
# Controller will detect Secret change within 5 minutes (or immediately with Secret watch)
```

## Post-Incident

- Verify alert resolves: `loopzero_cell_discovery_count > 0`
- Remove `LOOPZERO_SINGLE_CELL=true` if set: `kubectl set env deployment/loopzero-api -n platform-system LOOPZERO_SINGLE_CELL-`
- Run a test deploy to confirm end-to-end works

## Escalation

If none of the above resolves the issue:
1. Check K8s API server health on mgmt cluster
2. Check RBAC: loopzero-controller ServiceAccount must have `get`/`list` on `clustercredentials.platform.loopzero.io`
3. Check CRD installation: `kubectl get crd clustercredentials.platform.loopzero.io`

## References

- MEET-2026-04-18-053: Multi-Cell as Default
- ADR-0008: Cell-Based Architecture
- `api/internal/placement/dynamic_router.go`: DynamicRouter refresh logic
- `api/internal/placement/k8s_cell_store.go`: K8sCellStore reads CRDs
