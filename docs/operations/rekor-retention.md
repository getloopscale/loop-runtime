# Rekor Retention — Manual Archival Procedure

T4.6 of plan
`docs/plans/2026-05-28-meet-001-engineering-closure-plan.md`. CR-11
(MEET-2026-05-28-001 Production Design Review): the Rekor archival
script `infra/scripts/security/rekor-archive-old-entries.sh` ships as
a STUB because the Trillian admin API integration is a separate
operator workstream. This runbook is the manual procedure operators
follow until the API is wired.

## Why this exists

Rekor's transparency log is **append-only**. Entries cannot be
deleted without breaking the Merkle root chain. The "deletion" we
actually need is a 3-step process:

  1. Archive the entry payload to an immutable S3 bucket (so the
     evidence persists for auditors).
  2. Record the Merkle root inclusion proof at the time of archival
     (so future verifiers can prove the entry once existed).
  3. Garbage-collect the entry from Rekor's local store (Trillian
     admin API).

Steps 1 + 2 are operator-runnable today. Step 3 requires the Trillian
admin API integration deferred to the operator workstream.

## Storage budget

Per ADR `2026-05-27-T2.2-OFFLINE-TUF`:

| Resource | Allocation | Burn rate (est.) | Time to limit |
|---|---|---|---|
| Rekor PV | 140 GB | ~100 entries / build × ~50 builds/day × ~1.5 KB / entry ≈ 7.5 MB/day | **~50 years** at current rate |
| S3 archive bucket | unlimited | 100% of archived entries | n/a |
| Trillian backing store | included in Rekor PV | ~3× entry size | combined budget |

Reality: **manual archival is not urgent**. The script + runbook
exist so a future spike (e.g., 10× build volume) does not put the
operator in a position of having to invent the procedure on the day
the PV fills.

## When to invoke

Invoke manual archival when ANY of:

- Rekor PV usage > 80% (Prometheus alert
  `LoopZeroSigstorePVNearFull`, not yet shipped — track via
  `kubelet_volume_stats_used_bytes` in the meantime)
- An auditor requests a specific window be archived independent of
  the platform schedule
- Sigstore image upgrade requires a clean Rekor restart and you want
  to checkpoint old entries first

Otherwise the entries can stay where they are.

## Procedure

### Step 1 — Snapshot the Merkle root

```bash
# Rekor's public endpoint exposes the current root hash; the operator
# tucks this away in the audit trail for the archival event.
ROOT_HASH=$(curl -s "$REKOR_API_URL/api/v1/log" | jq -r .rootHash)
TREE_SIZE=$(curl -s "$REKOR_API_URL/api/v1/log" | jq -r .treeSize)
echo "{\"root_hash\": \"$ROOT_HASH\", \"tree_size\": $TREE_SIZE, \"captured_at\": \"$(date -u +%FT%TZ)\"}" \
  > /tmp/rekor-root-$(date -u +%Y%m%d).json
```

### Step 2 — Enumerate eligible entries

"Eligible" = older than `RETENTION_DAYS` (default 365). Rekor's API
exposes entries by UUID; iterate sorted by `integratedTime`.

```bash
RETENTION_DAYS=${RETENTION_DAYS:-365}
CUTOFF_TS=$(date -u -d "${RETENTION_DAYS} days ago" +%s)

# Iterate page-by-page. Page size 100 is Rekor default.
PAGE=0
while true; do
  RESP=$(curl -s "$REKOR_API_URL/api/v1/log/entries?page=$PAGE&limit=100")
  COUNT=$(jq '. | length' <<<"$RESP")
  [[ "$COUNT" == "0" ]] && break
  jq -r --argjson cutoff "$CUTOFF_TS" \
    '.[] | select(.integratedTime < $cutoff) | .uuid' \
    <<<"$RESP" >> /tmp/eligible-uuids.txt
  PAGE=$((PAGE + 1))
done

echo "Eligible: $(wc -l < /tmp/eligible-uuids.txt) entries"
```

### Step 3 — Archive each eligible entry to S3

```bash
S3_BUCKET=${S3_ARCHIVE_BUCKET:?required}

while read -r UUID; do
  PAYLOAD=$(curl -s "$REKOR_API_URL/api/v1/log/entries/$UUID")
  # Optional: include inclusion proof.
  PROOF=$(curl -s "$REKOR_API_URL/api/v1/log/entries/$UUID/proof")
  jq -n --argjson payload "$PAYLOAD" --argjson proof "$PROOF" \
    --arg uuid "$UUID" \
    '{uuid: $uuid, payload: $payload, proof: $proof}' \
    | aws s3 cp - "s3://$S3_BUCKET/rekor-archive/$UUID.json"
done < /tmp/eligible-uuids.txt
```

### Step 4 — Verify S3 archival, then garbage-collect locally

**This is the deferred step.** Without the Trillian admin API
integration, manual garbage collection requires:

  1. SSH to the Rekor pod (`kubectl exec -it rekor-0 -- /bin/sh`).
  2. Use the embedded `trillian` admin CLI to invoke `DeleteTree` or
     `UpdateTree` for the archived entries.

Per CR-11 disposition, the operator workstream will land
`rekor-archive-old-entries.sh` Step 4 once they confirm the Trillian
admin API access patterns. Until then, **stop after Step 3** —
entries remain queryable in Rekor but their evidence is archived
externally and cannot be lost to a Rekor PV outage.

### Step 5 — Audit log

Whatever subset of Steps 1-4 is performed, record a
`rekor.archival_run` audit_log entry (psql, manual):

```sql
INSERT INTO audit_log (tenant_id, action, actor, resource_type, resource_id, metadata)
VALUES ('system', 'rekor.archival_run', 'operator:<your name>', 'sigstore', 'rekor-archive',
        jsonb_build_object(
          'archived_count', :archived_count,
          'gc_step_completed', :gc_completed_bool,
          's3_bucket', :s3_bucket,
          'merkle_root_at_run', :root_hash
        ));
```

## Smoke check

After any archival run, sample 3 archived UUIDs from S3 and confirm
their `payload` matches a fresh `curl` from Rekor (entry should still
be query-able in the local store until Step 4 lands):

```bash
for UUID in $(jq -r '.uuid' /tmp/sample-3.json); do
  S3_PAYLOAD=$(aws s3 cp "s3://$S3_BUCKET/rekor-archive/$UUID.json" -)
  LIVE_PAYLOAD=$(curl -s "$REKOR_API_URL/api/v1/log/entries/$UUID")
  diff <(jq -c .payload <<<"$S3_PAYLOAD") <(jq -c . <<<"$LIVE_PAYLOAD")
done
```

Diff output empty = archive integrity confirmed.

## Related

- `infra/scripts/security/rekor-archive-old-entries.sh` (stub +
  `--manual-procedure-only` flag pointing here)
- `infra/tests/check-rekor-archive_test.sh` (hermetic test of the
  flag behavior)
- ADR `2026-05-27-T2.2-offline-tuf-ceremony.md` (append-only chain
  preservation rules)
- T2.1: write-once trigger on
  `image_attestation_rekor_url` — DB rows reference Rekor UUIDs; even
  after Step 4 garbage collection, the DB column still records what
  was once persisted (the S3 archive is the source of truth).
