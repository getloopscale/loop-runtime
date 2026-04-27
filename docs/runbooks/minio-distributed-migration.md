# Runbook: MinIO Standalone → Distributed Migration (CHG-002a / T2.3)

**Owners:** infra-engineer (primary), sre-engineer (incident commander), platform-security-engineer (credential rotation), gitops-engineer (build pipeline cutover)
**Status:** REQUIRED before T2.3 executes (D-3 SRE absolute veto: T2.3 without this runbook = blocked)
**Source:** ADR-0016 D-3 (blue/green with 48h overlap + atomic credential rotation)

---

## Why this runbook exists

MinIO standalone (1 pod, 1 PVC) is a SPOF for build artifacts (cache, bundles, logs). Pod kill = all 100+ tenants lose deploy capability until pod reschedules + PVC remounts (~30-90s typical, longer under node drain). MEET-2026-04-27-061 D-3 mandates blue/green migration with 48h overlap — strictly safer than in-place upgrade, which the upstream MinIO chart **does not support** (mode change destroys StatefulSet PVCs).

Target end-state: MinIO distributed mode with 4 drives across 4 pods, erasure code EC:2 (2 parity, 2 data) — survives loss of 2 drives simultaneously, recovery <2min per Gate 2a (CHG-002c).

## Pre-conditions (D-3 + D-7 enforcement)

1. CHG-001 closed (validation matrix 14/14)
2. CHG-002a opened with this runbook merged
3. Pulumi.dev.yaml `mgmtMaxNodes: 5` applied (T0.2 — distributed needs 4 nodes for anti-affinity)
4. `LOOPZERO_MAINTENANCE_MODE=1` set on loopzero-api before Phase D cutover (D-7 veto)
5. Backup of current MinIO data captured to DO Spaces:
   ```bash
   eval "$(grep -E '^[A-Z][A-Z_0-9]*=' /home/paulo/loopzero/.env | sed 's/^/export /')"
   KUBECONFIG=${HOME}/.kube/loopzero-loopzero-infra-dev-sfo3-mgmt.yaml \
     kubectl exec -n loopzero-system minio-0 -- \
       mc mirror --preserve --watch=false /data s3://loopzero-builds-backup-$(date +%Y%m%d)/
   ```

## Strategy: blue/green with 48h overlap

| Phase | Duration | Description |
|---|---|---|
| A — Deploy GREEN | 5min | `helm install minio-distributed` in NEW namespace `minio-distributed`, parallel to existing `minio` |
| B — Mirror data | 30-60min | `mc mirror` BLUE→GREEN for 3 buckets (`loopzero-artifacts`, `loopzero-builds`, `loopzero-logs`) |
| C — Validate GREEN | 10min | Quorum check, smoke upload+download, verify object counts match |
| D — Cutover | 5min downtime | MAINTENANCE_MODE on, drain workflows, final mirror, switch endpoint, MAINTENANCE_MODE off |
| E — 48h overlap | 48h passive | BLUE preserved as cold standby (read-only), GREEN serving all traffic |
| F — Decommission BLUE | 1h | Backup snapshot → `helm uninstall minio` → delete PVCs after 7-day grace |

## Phase A — Deploy GREEN (parallel to BLUE)

```bash
eval "$(grep -E '^[A-Z][A-Z_0-9]*=' /home/paulo/loopzero/.env | sed 's/^/export /')"
export PATH="$HOME/.pulumi/bin:$PATH"

# 1. Create dedicated namespace (network-policy isolated from minio-old).
KUBECONFIG=${HOME}/.kube/loopzero-loopzero-infra-dev-sfo3-mgmt.yaml \
  kubectl create namespace minio-distributed

# 2. Apply per-namespace pod anti-affinity (require 4 distinct nodes).
# Distribution rationale: pod kill = 1/4 drives down = EC:2 still survives.

# 3. Install via Helm using values-distributed.yaml.
helm upgrade --install minio-distributed bitnami/minio \
  --kube-context <mgmt-ctx> \
  -n minio-distributed \
  -f infra/helm/minio/values-distributed.yaml \
  --version 14.x.x \
  --wait --timeout 10m
```

**Expected output:** 4 pods `minio-distributed-{0,1,2,3}` Running on 4 distinct nodes.

```bash
KUBECONFIG=${HOME}/.kube/loopzero-loopzero-infra-dev-sfo3-mgmt.yaml \
  kubectl get pods -n minio-distributed -o wide
# Verify: each pod on a different node (anti-affinity working)

KUBECONFIG=${HOME}/.kube/loopzero-loopzero-infra-dev-sfo3-mgmt.yaml \
  kubectl exec -n minio-distributed minio-distributed-0 -- \
    mc admin info local
# Verify: "Drives: 4 Online, 0 Offline" + "Pool: 1, Set: 4 drives"
```

## Phase B — Mirror data (no downtime, BLUE keeps serving)

```bash
# Configure mc aliases on a transient pod inside the cluster (PG private network).
KUBECONFIG=${HOME}/.kube/loopzero-loopzero-infra-dev-sfo3-mgmt.yaml \
  kubectl run mc-mirror --rm -i --restart=Never --namespace=minio-distributed \
    --image=minio/mc:latest -- sh -c '
mc alias set blue http://minio.minio.svc.cluster.local:9000 $OLD_USER $OLD_PASS
mc alias set green http://minio-distributed.minio-distributed.svc.cluster.local:9000 $NEW_USER $NEW_PASS

# Create buckets on GREEN (idempotent).
for bucket in loopzero-artifacts loopzero-builds loopzero-logs; do
  mc mb --ignore-existing green/$bucket
done

# Mirror — preserve metadata, watch=false (one-shot).
for bucket in loopzero-artifacts loopzero-builds loopzero-logs; do
  echo "=== mirroring $bucket ==="
  mc mirror --preserve blue/$bucket green/$bucket
done

# Validate object counts match.
for bucket in loopzero-artifacts loopzero-builds loopzero-logs; do
  BLUE_CNT=$(mc ls --recursive blue/$bucket | wc -l)
  GREEN_CNT=$(mc ls --recursive green/$bucket | wc -l)
  echo "$bucket: blue=$BLUE_CNT green=$GREEN_CNT $([ $BLUE_CNT -eq $GREEN_CNT ] && echo OK || echo MISMATCH)"
done
'
```

If mirror fails mid-flight: `mc mirror --continue` resumes from last object. Bandwidth limit if needed: `--limit-upload 50MB`.

## Phase C — Validate GREEN

```bash
# 1. Quorum check (4 online, 0 offline, EC:2 active).
mc admin info green

# 2. Smoke upload + download via mc.
echo "smoke-test-$(date +%s)" > /tmp/smoke.txt
mc cp /tmp/smoke.txt green/loopzero-builds/_smoke/
mc cp green/loopzero-builds/_smoke/smoke.txt /tmp/smoke-back.txt
diff /tmp/smoke.txt /tmp/smoke-back.txt && echo "OK"
mc rm green/loopzero-builds/_smoke/smoke.txt

# 3. Failover test (CHG-002c chaos pre-validation).
KUBECONFIG=... kubectl scale statefulset minio-distributed -n minio-distributed --replicas=3
sleep 30  # Let cluster reconcile to 3/4 drives.
mc admin info green  # Expect: "3 Online, 1 Offline" — still serving (EC:2 = 2 parity).
mc cp /tmp/smoke.txt green/loopzero-builds/_smoke-degraded/
mc rm green/loopzero-builds/_smoke-degraded/smoke.txt
KUBECONFIG=... kubectl scale statefulset minio-distributed -n minio-distributed --replicas=4
sleep 60  # Let cluster heal back to 4/4.
```

## Phase D — Cutover (5min planned downtime)

```bash
# 1. MAINTENANCE_MODE on loopzero-api (D-7 veto enforcement).
KUBECONFIG=... kubectl set env deployment/loopzero-api -n loopzero-system \
  LOOPZERO_MAINTENANCE_MODE="MinIO migration in progress (~5min). Deploys queued."

# 2. Wait for active workflows to drain.
KUBECONFIG=... kubectl get workflows -A --field-selector status.phase=Running
# Loop until empty (or timeout 10min and force-cancel via argo).

# 3. Final incremental mirror (captures any writes during Phase B-C).
mc mirror --preserve --overwrite blue/loopzero-artifacts green/loopzero-artifacts
mc mirror --preserve --overwrite blue/loopzero-builds green/loopzero-builds
mc mirror --preserve --overwrite blue/loopzero-logs green/loopzero-logs

# 4. Switch endpoint via Pulumi config (preferred — auditable in state).
pulumi config set --stack dev loopzero-infra:minioEndpoint \
  "minio-distributed.minio-distributed.svc.cluster.local:9000"
pulumi up --stack dev --yes --skip-preview \
  --target 'urn:pulumi:dev::loopzero-infra::kubernetes:apps/v1:Deployment::loopzero-api' \
  --target 'urn:pulumi:dev::loopzero-infra::kubernetes:apps/v1:Deployment::loopzero-controller' \
  --target-dependents

# 5. Smoke deploy E2E to validate the new endpoint.
loopzero deploy --env staging --json | jq

# 6. MAINTENANCE_MODE off.
KUBECONFIG=... kubectl set env deployment/loopzero-api -n loopzero-system \
  LOOPZERO_MAINTENANCE_MODE-
KUBECONFIG=... kubectl rollout status deployment/loopzero-api -n loopzero-system
```

## Phase E — 48h overlap

BLUE remains in `minio` namespace. Read-only via NetworkPolicy (write blocked). All new traffic on GREEN. Monitor:
- `loopzero_minio_request_duration_seconds` p99 < 200ms (GREEN)
- `loopzero_build_failure_rate` no regression vs pre-migration baseline
- Zero `S3_ENDPOINT_UNREACHABLE` errors in loopzero-api logs

If anomaly detected: rollback by reverting the Pulumi config (Phase D step 4 reverse). BLUE still serves. Investigate, re-cutover later.

## Phase F — Decommission (after 48h clean window)

```bash
# 1. Final backup snapshot.
mc cp --recursive blue/loopzero-artifacts s3://loopzero-builds-backup-final/$(date +%Y%m%d)/loopzero-artifacts/
mc cp --recursive blue/loopzero-builds s3://loopzero-builds-backup-final/$(date +%Y%m%d)/loopzero-builds/

# 2. Helm uninstall BLUE.
helm uninstall minio -n minio --kube-context <mgmt-ctx>

# 3. Delete PVCs (Helm leaves them by default for safety).
KUBECONFIG=... kubectl delete pvc -n minio --all
# 7-day grace period before deleting backup buckets.
```

## Risk matrix

| Severity | Risk | Mitigation |
|---|---|---|
| HIGH | mc mirror partial — split brain reads | --watch=false (one-shot), final mirror in Phase D, MAINTENANCE_MODE drain |
| HIGH | EC:2 data loss if 2/4 drives die simultaneously during overlap | DO node anti-affinity + monitor pod count, alert on <4 |
| MEDIUM | Endpoint switch — stale loopzero-api pods cache old DNS | Rollout restart in Phase D step 4 (target-dependents includes loopzero-api) |
| MEDIUM | Quota exhaustion (4 PVCs new + 1 PVC old = 5 × 50Gi = 250Gi) | Verify DO block-storage quota pre-Phase A: `doctl compute volume list \| wc -l` < quota |
| LOW | Argo workflow build cache invalidation | Pre-warm buckets in Phase B + smoke deploy in Phase D step 5 |

## Rollback procedure (any phase)

| Phase | Rollback action |
|---|---|
| A | `helm uninstall minio-distributed -n minio-distributed && kubectl delete pvc -n minio-distributed --all` |
| B | `mc rm --recursive --force green/$bucket` (if data corruption suspected) + retry mirror |
| C | If GREEN validation fails: stay on BLUE, investigate, retry Phase B |
| D | `pulumi config set loopzero-infra:minioEndpoint <old-endpoint>` + pulumi up + rollout restart loopzero-api |
| E | Same as D — endpoint flip back to BLUE, GREEN preserved for next attempt |
| F | If decommission failure mid-flight: PVCs preserved by default; restore from backup snapshot to new namespace |

## Compliance

This runbook satisfies:
- D-3 (blue/green with 48h overlap): Phases A-F sequenced
- D-7 (MAINTENANCE_MODE pre-flight): Phase D step 1
- SRE veto: published BEFORE T2.3 opens
- Security veto (atomic credential rotation): each cluster (BLUE/GREEN) has independent rootUser+rootPassword via separate Secrets

## References

- ADR-0016 — Sprint 8 Phase 0-6 three-CHG split (D-3)
- MEET-2026-04-27-061 — meeting record (Stage 4 mandatory actions)
- `infra/helm/minio/values.yaml` — current standalone (will not be edited during migration)
- `infra/helm/minio/values-distributed.yaml` — GREEN deployment config (T2.3 deliverable)
- MinIO docs: https://min.io/docs/minio/kubernetes/upstream/operations/install-deploy-manage/migrate-fs-gateway.html
