# PV Rebind Runbook — M2 namespace migration with `--include-pvcs=true`

Operator playbook for the rare path where `migrate-legacy-namespaces.sh
--mode=adopt --include-pvcs=true --pv-rebind-strategy={retain|clone}` is
invoked on a tenant whose legacy 2-segment namespace holds stateful
workloads bound to PersistentVolumeClaims.

**Default path is `--include-pvcs=false` (PVCs skipped with WARNING).** Most
cleanroom-dev and early-stage prod tenants are stateless and never reach this
runbook. Per ADR D6 of plan `2026-06-09-m2-operator-canonical-fix` the
opt-in is explicit because PVCs bound to dynamically-provisioned PVs cannot
be moved between namespaces without out-of-band coordination — copying the
PVC manifest creates a *new* PVC that binds a *new* PV; the old PV becomes
orphan and is reaped when the legacy namespace is deleted, **destroying the
data**.

The script itself does NOT mutate PV bindings. It refuses execution unless
the operator explicitly selects a strategy, then runs the adopt path AROUND
the PVCs (resources other than PVCs are copied; PVCs are listed in the
WARNING for the operator to handle here).

---

## Strategy `retain` — preserve the underlying PV, re-bind to a new PVC

Best for production tenants whose data must survive the namespace move.

### Pre-flight

```bash
TENANT="<tn-XXXX>"
LEGACY_NS="loopzero-${TENANT}-<env>"               # 2-seg
CANONICAL_NS="loopzero-${TENANT}-<project>-<env>"  # 3-seg

# 1. List PVCs in the legacy namespace + their PVs
kubectl --context k3d-loopzero-workload -n "$LEGACY_NS" get pvc -o jsonpath='{range .items[*]}{.metadata.name}={.spec.volumeName}{"\n"}{end}'

# 2. Quiesce workloads writing to those PVCs (scale Deployments/Rollouts to 0)
kubectl --context k3d-loopzero-workload -n "$LEGACY_NS" scale deploy --all --replicas=0
kubectl --context k3d-loopzero-workload -n "$LEGACY_NS" wait --for=delete pod --all --timeout=120s

# 3. Snapshot PV state for rollback audit
kubectl --context k3d-loopzero-workload get pv -o yaml > "/tmp/pv-snapshot-${TENANT}-$(date +%s).yaml"
```

### The rebind dance (per PV)

```bash
PVC_NAME="<name>"
PV_NAME="$(kubectl --context k3d-loopzero-workload -n "$LEGACY_NS" get pvc "$PVC_NAME" -o jsonpath='{.spec.volumeName}')"

# Patch the PV's reclaim policy to Retain so it survives the PVC delete
kubectl --context k3d-loopzero-workload patch pv "$PV_NAME" --type=merge \
  -p '{"spec":{"persistentVolumeReclaimPolicy":"Retain"}}'

# Drop the old PVC; the PV survives in Released state
kubectl --context k3d-loopzero-workload -n "$LEGACY_NS" delete pvc "$PVC_NAME"

# Clear the claimRef so the PV is bindable again
kubectl --context k3d-loopzero-workload patch pv "$PV_NAME" --type=json \
  -p='[{"op": "remove", "path": "/spec/claimRef"}]'

# Create the new PVC in the canonical namespace, pinning it to the PV
cat <<EOF | kubectl --context k3d-loopzero-workload apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: $PVC_NAME
  namespace: $CANONICAL_NS
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: <restore-from-pv-snapshot>
  volumeName: $PV_NAME
EOF

# Verify the binding
kubectl --context k3d-loopzero-workload -n "$CANONICAL_NS" get pvc "$PVC_NAME" -o jsonpath='{.status.phase}'
# Expect: Bound
```

### After all PVCs are rebound

```bash
# Re-scale workloads in the CANONICAL namespace (they were re-created via
# the script's adopt path; just bring replicas back up)
kubectl --context k3d-loopzero-workload -n "$CANONICAL_NS" scale deploy --all --replicas=1
```

---

## Strategy `clone` — copy data to a new PV, retire the old one

Best when the underlying storage class supports VolumeSnapshot (or the
operator has an out-of-band backup mechanism). Slower but safer against
mis-rebind because the original PV is left untouched.

Outline (delegated to storage-class-specific tooling):

1. Take a VolumeSnapshot of each PVC in `$LEGACY_NS` (storage-class API).
2. Restore each snapshot into a NEW PVC in `$CANONICAL_NS`.
3. Verify checksums against the original.
4. Once verified, allow the legacy namespace delete (which retires the old
   PV per the cluster's default reclaim policy).

No verbatim commands here — every storage class has its own snapshot CRDs.
Consult `kubectl get storageclass` + the provider documentation.

---

## Rollback (both strategies)

If anything looks wrong AFTER the rebind but BEFORE the legacy namespace is
deleted:

```bash
# Restore the PV state from the snapshot
kubectl --context k3d-loopzero-workload apply -f "/tmp/pv-snapshot-${TENANT}-<timestamp>.yaml"

# Re-scale workloads in the LEGACY namespace
kubectl --context k3d-loopzero-workload -n "$LEGACY_NS" scale deploy --all --replicas=1
```

If the legacy namespace has already been deleted, recovery depends on
whether the PV was retained (recoverable via reclaim policy=Retain) or
reaped (recoverable only from external backup). Per the pre-flight, ALWAYS
snapshot before mutating PVs.

---

## When NOT to use this runbook

- Tenant has no PVCs → run `--include-pvcs=false` (default; PVCs WARNING
  is moot).
- Tenant is cleanroom-dev → run `--mode=delete` instead. Data is throwaway
  by definition of cleanroom.
- Storage-class supports cross-namespace PVC move natively (rare; CSI
  drivers vary) → consult provider docs.

## Cross-links

- Plan: `.claude/knowledge-base/plans/2026-06-09-m2-operator-canonical-fix-plan.md`
  ADR D6 + EC-6 + R3.
- Script: `infra/scripts/local/migrate-legacy-namespaces.sh` (`--include-pvcs=true`
  refuses without `--pv-rebind-strategy` → forces the operator here).
- Edge-case review: `.claude/knowledge-base/reviews/2026-06-09-m2-operator-canonical-fix-edge-cases-2026-06-09.md`
  EC-6 (PVC adopt-mode data loss).
