# Disaster Recovery — Velero Restore Procedure

> Plan reference: `docs/plans/velero-backup-dr-plan.md` Phase 4 / T4.1.
> Source decision: ADRs D1-D6 in the plan (Velero on mgmt only; DO Spaces /
> MinIO backend; 5 schedules; no PV restic).
> Last updated: 2026-05-06.
> Audience: SRE on-call + platform architect (witness for production
> restores). Pair-execute — single-operator restores are not allowed in
> prod per ADR-0023 incident protocol.

This runbook documents how to restore the LoopZero management cluster from
Velero backups after a destructive incident (etcd corruption, accidental
namespace drop, or full cluster loss). The legacy `LoopZeroGiteaBackupStale`
ORPHAN alert was retired in favor of `LoopZeroVeleroBackupStale` which fires
when ANY of the 5 scheduled scopes is more than 12 hours stale.

---

## Pre-conditions

Before starting, confirm:

1. **Velero CLI installed locally** (`velero version` returns
   `Client: 1.14.x`). Install via `brew install velero` or
   `https://velero.io/docs/main/basic-install/`.
2. **kubectl context set** to a freshly-provisioned mgmt cluster
   (Pulumi L1 reapplied, cert-manager + traefik already up, but the rest
   of the platform absent). Use `task setup` for k3d, `pulumi up` for
   cloud.
3. **DO Spaces credentials available** (`LOOPZERO_SPACES_KEY` /
   `LOOPZERO_SPACES_SECRET`) — same keys that drive the running platform.
4. **Bucket reachable** — `aws s3 ls s3://loopzero-{project}-{region}-velero-backups/`
   returns a `kopia/` or `backups/` prefix listing.
5. **Velero CR types pre-installed** — Velero install creates its own
   CRDs (`backupstoragelocations.velero.io`, `backups.velero.io`,
   `restores.velero.io`, etc.). The Helmfile install in Step 1 below
   takes care of this.

If any pre-condition fails, do not proceed — escalate per `docs/runbooks/
incident-response.md`.

---

## RTO budget

The plan targets **RTO < 30 minutes** end-to-end. Per-step estimate:

| Step | Action | Estimated time |
|------|--------|----------------|
| 1 | Install Velero on the new cluster | 3 min |
| 2 | Verify BSL connects | 1 min |
| 3 | List backups | 1 min |
| 4 | Restore CRDs | 5 min |
| 5 | Verify CRDs | 2 min |
| 6 | Restore ArgoCD | 5 min |
| 7 | Verify ArgoCD Apps | 2 min |
| 8 | Restore Secrets | 3 min |
| 9 | Verify platform health (`task doctor`) | 3 min |
| 10 | Smoke test (`loopzero deploy`) | 5 min |
| **Total** | | **~30 min** |

Bigger backups (full snapshot) cost more on Step 4 — budget 15 min
extra if restoring from `loopzero-full-backup`.

---

## Step 1 — Install Velero on the new cluster (3 min)

```
# Choose env file matching the target environment (dev | prod).
# Standalone uses MinIO; cloud restores almost never need standalone.
helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl \
  -e prod sync -l name=velero
```

The Helmfile sync also runs `generate-secrets.sh` which creates
`velero-system/velero-s3-credentials` (Secret with key `cloud` in AWS
INI format) before the chart starts. Wait until the velero pod reports
`Ready=true`:

```
kubectl --context <mgmt> -n velero-system rollout status deploy/velero
```

If the deploy fails to come up, check:
- `kubectl -n velero-system describe pod -l name=velero` for image
  pull / RBAC errors.
- `kubectl -n velero-system get bsl default -o yaml` — should reach
  `status.phase=Available` once the BSL controller talks to the bucket.

---

## Step 2 — Verify BackupStorageLocation connects (1 min)

```
velero --kubeconfig $KUBECONFIG backup-location get
```

Expected output:
```
NAME      PROVIDER   BUCKET/PREFIX                          PHASE       LAST VALIDATED  ACCESS MODE
default   aws        loopzero-infra-sfo3-velero-backups         Available   30s ago         ReadWrite
```

If `PHASE=Unavailable`, the credentials Secret is wrong or the bucket
ACL forbids the access key. Re-run `bash infra/helmfile/scripts/generate-secrets.sh prod mgmt`
and re-validate.

---

## Step 3 — List available backups (1 min)

```
velero backup get --selector velero.io/schedule-name=loopzero-crds-backup \
  --sort-by=.status.startTimestamp -o json | \
  jq -r '.items[-5:] | .[] | "\(.metadata.name)  \(.status.phase)  \(.status.startTimestamp)"'
```

Pick the most recent `Completed` backup — record the name (e.g.
`loopzero-crds-backup-20260506-060000`). Repeat for each schedule whose
data you need (`loopzero-argocd-backup`, `loopzero-secrets-backup`,
`loopzero-gitea-backup`).

---

## Step 4 — Restore CRDs first (5 min)

Restoring CRDs first is critical. The LoopZero operators need the
`tenants.loopzero.io`, `environments.loopzero.io`, etc. type definitions plus
the actual CR instances before they can reconcile anything.

```
velero restore create restore-crds-$(date +%s) \
  --from-backup loopzero-crds-backup-<latest>
```

Wait for completion:
```
velero restore get restore-crds-* | head -3
# expect: PHASE=Completed
```

---

## Step 5 — Verify CRDs restored (2 min)

```
kubectl get tenants,environments,loopzeroapplications -A
kubectl get clustercredentials,exposedroutes,previewenvironments -A
```

Expected: same counts as the running prod cluster pre-incident. If
counts are short, inspect `velero restore describe restore-crds-...`
for items skipped because of cluster mismatch (CRD version drift).

---

## Step 6 — Restore ArgoCD (5 min)

```
velero restore create restore-argocd-$(date +%s) \
  --from-backup loopzero-argocd-backup-<latest>
```

ArgoCD Apps + AppProjects + repository Secrets land in `argocd`
namespace. The ArgoCD server must already be running (re-installed by
Helmfile in Step 1) — otherwise the Apps sit unprocessed until ArgoCD
boots.

---

## Step 7 — Verify ArgoCD Apps restored (2 min)

```
kubectl --context <mgmt> -n argocd get applications.argoproj.io
kubectl --context <mgmt> -n argocd get appprojects.argoproj.io
```

Expected: 1 Application per (tenant, environment) plus the
platform-bootstrap App (per ADR-0019). Apps may show `Unknown` /
`OutOfSync` immediately — wait 60s for the auto-sync loop, then
`argocd app sync <name>` if needed.

---

## Step 8 — Restore Secrets (3 min)

```
velero restore create restore-secrets-$(date +%s) \
  --from-backup loopzero-secrets-backup-<latest>
```

This restores cluster kubeconfigs, signing keys, registry pull
secrets in `loopzero-system`. Without these, the operators can't talk to
runtime clusters and the API can't sign images. This step is required
after Step 6 — ArgoCD Apps reference these Secrets.

**Security note (EC-7):** Secret backup objects are encrypted at rest
by DO Spaces but readable to anyone holding the Spaces API key.
Restore operations should be audited (record who ran the restore + a
correlation ID).

---

## Step 9 — Verify platform health (3 min)

```
task doctor
```

Expected: all 38 checks PASS (matches the pre-incident baseline).
Operator pods, Postgres connectivity, ArgoCD reachable, Gitea API
reachable. Track each FAIL back to the corresponding restore step.

---

## Step 10 — Smoke test (5 min)

The decisive validation: deploy a known-good fixture end-to-end.

```
loopzero project create restore-smoke --tenant <tenant>
loopzero deploy --project restore-smoke --env production --json | \
  jq -r '.build_id'
# Watch the pod come up:
kubectl --context <runtime> get pods -n loopzero-<tenant>-production -l \
  loopzero.io/project=restore-smoke -w
# Smoke probe:
curl -sk https://restore-smoke-<tenant>-production.<base-domain>/health
# Cleanup:
loopzero project delete restore-smoke --yes
```

If smoke passes, the cluster is recovered.

---

## Troubleshooting

### A. `Backup not found`

```
velero restore create ... \
  --from-backup loopzero-crds-backup-20260506-060000
# Error: backup "loopzero-crds-backup-20260506-060000" not found
```

**Cause:** The BSL hasn't synced backups from the bucket yet, or the
backup name has a typo.

**Fix:** Wait 1 minute for the next BSL sync (`backupSyncPeriod=1m` in
the chart values) and `velero backup get` again. If still missing,
verify the bucket directly with `aws s3 ls s3://...`.

### B. `Partially failed` restore

```
velero restore describe restore-crds-* | grep -A5 "Errors"
```

**Cause:** Cluster CRD version drift (the backup was taken on
`loopzero.io/v1alpha1`, current cluster has `loopzero.io/v1`). Or the
Application controller crashed mid-reconcile of restored CRs.

**Fix:** Re-run `loopzero-crds` install (Helmfile release `loopzero-crds` in
Wave -3) so the latest CRD versions are present before retrying the
restore.

### C. CRDs restored but operators crashloop

**Cause:** Operators were re-installed in Step 1 but cached the
empty cluster state from before the restore. They reconcile against
stale snapshots and panic on missing dependencies.

**Fix:** `kubectl rollout restart deploy -n loopzero-system -l
app.kubernetes.io/component=loopzero-operator`. The fresh pod loads
the restored CRs from etcd and reconciles cleanly.

---

## Cross-references

- Schedules: `infra/helm/velero/values-schedules.yaml` (T2.1)
- Alerts that surface RPO breach: `infra/helm/monitoring/manifests/prometheusrule-velero.yaml`
- DR drill (automated, runs in CI): `infra/scripts/velero-dr-drill.sh`
- CI workflow checking freshness: `.github/workflows/velero-backup-check.yml`
- Plan ADRs (D1-D6): `docs/plans/velero-backup-dr-plan.md`
