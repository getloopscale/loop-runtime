---
name: sigstore-storage-model
description: Storage growth model + retention policy + PV resize procedure for self-hosted Sigstore (Fulcio + Rekor) per ADR-2026-05-27-T2.2-OFFLINE-TUF + plan T1.1.
type: operational-runbook
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: tenant-build-pipeline-gaps-closure plan T1.1
---

# Sigstore Self-Hosted — Storage Growth Model

> **Per ADR-2026-05-27-T2.2-OFFLINE-TUF + plan T1.1.** Storage planning for self-hosted Rekor transparency log + retention strategy.

## Growth Model

### Per-build storage cost

Each tenant build produces:
- **1 Rekor entry for cosign signature** (~1 KB)
- **1 Rekor entry for cosign attestation** (~1 KB, SLSA L3 SBOM linkage per plan T2.1)

**Per-build:** ~2 KB Rekor storage.

### Tenant scale projections

| Scenario | Tenants | Builds/tenant/day | Total/day | Annual growth |
|----------|---------|---------------------|-----------|---------------|
| Initial (Pre-GA) | 100 | 5 | 1 KB | ~365 KB |
| Beta (1k tenants) | 1,000 | 5 | 10 MB | ~3.5 GB |
| **Target (CTO Rule 2)** | **20,000** | **5** | **200 MB** | **~70 GB** |
| Stretch (50k stretch) | 50,000 | 5 | 500 MB | ~180 GB |

**Recommended PV size:** **140 GB** (2× target annual growth buffer = 1 year runway at 20k tenant target).

### Trillian backend (Rekor internal storage)

Rekor uses Trillian as its append-only log backend. Trillian backend = MySQL (sigstore-scaffolding default) provisioned inline as part of Helm release.

MySQL storage scaling matches Rekor entry count:
- 1 KB per entry × 2 entries per build → ~2 KB Trillian rows
- 20k tenants × 5 builds/day × 365 days = ~36.5M rows/year ≈ 70 GB

**MySQL PV:** match Rekor PV (140 GB).

## Retention Policy

### Append-only constraint

Rekor + Trillian are **append-only by design** — no native delete operation. Cryptographic guarantees rely on log immutability.

### Long-term: archive + delete

After ~365 days, entries unlikely needed for active verification (most images redeployed by then; old releases archived). Strategy:

1. **Daily snapshot** of Rekor entries older than 365d to S3 archive bucket
2. **Verify archive integrity** (compare hashes against Rekor public log root)
3. **Delete locally** via Trillian admin API (`tlserve --delete-tree`)
4. **Preserve transparency proof** by retaining Merkle tree root signatures in archive

### Archive script

Located: `infra/scripts/security/rekor-archive-old-entries.sh` (stub — operator implements detailed Trillian API calls post-Sigstore activation).

```bash
#!/usr/bin/env bash
# Daily cron: archive Rekor entries >365d to S3 + delete local
set -euo pipefail

REKOR_API="${REKOR_API_URL:-https://rekor.loopzero.example}"
S3_BUCKET="${S3_ARCHIVE_BUCKET:-loopzero-sigstore-archive}"
RETENTION_DAYS="${RETENTION_DAYS:-365}"
CUTOFF_TS=$(date -u -d "${RETENTION_DAYS} days ago" +%s)

# Query Rekor for entries before cutoff
rekor-cli search --rekor_server "$REKOR_API" \
  --max-result 10000 \
  --sha-prefix '' \
  > /tmp/rekor-entries.json

# Iterate + archive each to S3
jq -c '.[]' /tmp/rekor-entries.json | while read entry; do
  uuid=$(echo "$entry" | jq -r '.uuid')
  ts=$(echo "$entry" | jq -r '.integratedTime')

  if [ "$ts" -gt "$CUTOFF_TS" ]; then
    continue  # too new, skip
  fi

  # Upload to S3 (Trillian Merkle root + raw entry)
  aws s3 cp "/tmp/rekor-$uuid.json" "s3://$S3_BUCKET/$uuid.json"

  # Delete from local Rekor (admin API)
  # NOTE: this requires Rekor admin token; operator runbook handles
  rekor-cli delete --rekor_server "$REKOR_API" --uuid "$uuid"
done
```

## Alert thresholds

| Threshold | Severity | Action |
|-----------|----------|--------|
| PV usage > 80% | `warning` | Operator reviews growth rate; schedules archive run |
| PV usage > 95% | `critical` | Page operator; immediate action: expand PVC OR force-archive |

Alerts implemented in `infra/helm/admission-policies/templates/sigstore-storage-alerts.yaml` (T1.1).

## PV resize procedure

> **CAUTION:** PV resize requires PVC patch + StatefulSet rollout restart. Brief downtime (~30s) during pod restart. Schedule during off-peak window.

### Steps

1. **Pre-check:** confirm StorageClass `allowVolumeExpansion: true`
   ```bash
   kubectl get storageclass <name> -o jsonpath='{.allowVolumeExpansion}'
   ```

2. **Patch PVC**
   ```bash
   kubectl patch pvc rekor-data -n sigstore-system \
     --type='json' -p='[{"op": "replace", "path": "/spec/resources/requests/storage", "value": "280Gi"}]'
   ```

3. **Wait for ResizePending → ResizeInProgress → no condition (resized)**
   ```bash
   kubectl get pvc rekor-data -n sigstore-system -w
   ```

4. **Rollout restart StatefulSet** (some StorageClasses require pod restart to pick up new size)
   ```bash
   kubectl rollout restart statefulset rekor -n sigstore-system
   ```

5. **Verify** new size mounted:
   ```bash
   kubectl exec -n sigstore-system rekor-0 -- df -h /var/lib/rekor
   ```

### Downtime mitigation

If 0 downtime required:
- Provision new larger PV
- Use Velero (B11) to backup current Rekor MySQL → restore to new PV
- Switch StatefulSet to reference new PVC
- Decommission old PVC

This is more complex; recommended only if SLA mandates 0 downtime.

## Future considerations

- **Trillian Postgres backend** — Trillian roadmap item; would allow swap from MySQL to managed PG (DOKS managed PG)
- **Witness redundancy** — beyond Rekor cross-witness (per ADR-2026-05-27-T2.2-OFFLINE-TUF), consider 3rd-party witness (e.g., Mozilla's Binary Transparency)
- **Compression** — Rekor entries are JSON; gzip compression in S3 archive cuts storage ~5x

## Related

- ADR-2026-05-27-T2.2-OFFLINE-TUF: `.claude/knowledge-base/decisions/2026-05-27-T2.2-offline-tuf-ceremony.md`
- Plan T1.1: `docs/plans/2026-05-27-tenant-build-pipeline-gaps-closure-plan.md`
- Storage alerts: `infra/helm/admission-policies/templates/sigstore-storage-alerts.yaml`
- Archive script stub: `infra/scripts/security/rekor-archive-old-entries.sh`
- Sigstore scaffolding: https://github.com/sigstore/helm-charts
- Trillian admin guide: https://github.com/google/trillian
