# Pulumi State Restore — URN-Rewrite Procedure

> Plan reference: `docs/plans/infra-normalize-multicell-plan.md` Phase 3.
> Source decision: `docs/decisions/MEET-INFRA-NORMALIZE-T3-pulumi-consolidation.md` (condition C16).
> Last updated: 2026-04-30.
> Audience: infra operator + platform-architect (witness for production restores).

This runbook documents how to restore an archived Pulumi state file (from `s3://loopzero-archive/pulumi-state/`) into a working Pulumi stack. The non-trivial step is **URN rewriting**: archives capture state at a specific project + stack identity, and restoring into a different identity requires editing the URNs.

Used by:
- T3.3 archive replay drill (C22 — every 14 days post-T3.2).
- C19 rollback (state-only restore within ≤15 min SLO).
- Forensics (post-incident diagnosis from a snapshot in time).

---

## Worked example: restore `pulumi-platform/dev` into the canonical project

Suppose the archive at `s3://loopzero-archive/pulumi-state/pulumi-platform/dev/20260430T120000Z.json` represents the dev stack of the deleted `loopzero-platform` project. Restoring into the canonical `loopzero-infra` project requires rewriting URNs from `urn:pulumi:dev::loopzero-platform::...` to `urn:pulumi:dev::loopzero-infra::...`.

### Step 1 — download

```
aws s3 cp \
  s3://loopzero-archive/pulumi-state/pulumi-platform/dev/20260430T120000Z.json \
  /tmp/restore-pulumi-platform-dev.json

# Verify integrity (snapshot SHA was captured in S3 metadata at
# archive time per condition C17).
aws s3api head-object \
  --bucket loopzero-archive \
  --key pulumi-state/pulumi-platform/dev/20260430T120000Z.json \
  --query 'Metadata."snapshot-sha256"' --output text
sha256sum /tmp/restore-pulumi-platform-dev.json
# Both values must match.
```

### Step 2 — URN rewrite

```
# Source project name (from archive)
SRC_PROJECT="loopzero-platform"
# Target project name (canonical)
TGT_PROJECT="loopzero-infra"

# Rewrite ALL URNs in the file.
jq --arg src "$SRC_PROJECT" --arg tgt "$TGT_PROJECT" '
  (.. | strings) |= (gsub("urn:pulumi:([^:]+)::" + $src + "::"; "urn:pulumi:\\1::" + $tgt + "::"))
' /tmp/restore-pulumi-platform-dev.json > /tmp/restore-rewritten.json

# Verify rewrite count > 0
diff <(jq -c . /tmp/restore-pulumi-platform-dev.json) \
     <(jq -c . /tmp/restore-rewritten.json) | wc -l
# Expect non-zero — every URN line should differ.
```

### Step 3 — initialize a target stack

```
cd infra/pulumi
# Pick a stack name that doesn't collide with existing canonical stacks.
RESTORE_STACK="restore-pulumi-platform-dev-$(date +%s)"
pulumi stack init "$RESTORE_STACK"
```

### Step 4 — import the rewritten state

```
pulumi stack select "$RESTORE_STACK"
pulumi stack import --file /tmp/restore-rewritten.json
```

If `pulumi stack import` errors with "URN mismatch" or "version mismatch":

- **URN mismatch**: jq rewrite missed a corner case. Inspect:
  ```
  pulumi stack import --file /tmp/restore-rewritten.json 2>&1 | grep -i urn | head -5
  ```
  Patch jq filter accordingly; some Pulumi versions emit URNs in unconventional places (e.g., inside `customTimeouts.create` or `propertyDependencies`).
- **Version mismatch**: archive was captured by a newer Pulumi than the local install (per C17 pin to 3.140.0). Either:
  - Use the matching Pulumi binary (recommended for production restores).
  - Manually downgrade the archive's `version` field at top-level JSON; document the change in the audit log (C15) and flag for platform-architect review (state semantics may differ).

### Step 5 — verify resource count

```
expected=$(aws s3api head-object \
  --bucket loopzero-archive \
  --key pulumi-state/pulumi-platform/dev/20260430T120000Z.json \
  --query 'Metadata.resources' --output text)

actual=$(pulumi stack --json | jq '.resources | length')

if [ "$expected" != "$actual" ]; then
  echo "FAIL: expected $expected resources, got $actual"
  exit 1
fi
```

Pass means the import is bit-faithful. The stack now has the SAME resource SET as the archived stack at archive time — but **resources are not actually live** (Pulumi state ≠ cloud reality after a destroy).

### Step 6 — decision: proceed to `pulumi up` or stop here?

**For replay drill (C22 / C26):** STOP HERE. Drill verified import semantics; do NOT `pulumi up`. Cleanup:
```
pulumi stack rm "$RESTORE_STACK" --yes
```

**For C19 rollback (real recovery):** if cloud resources still exist, `pulumi refresh` reconciles state with reality, then `pulumi up` if needed. If cloud resources were destroyed (the only reason to need a real restore), `pulumi up` re-provisions — costs $ and time.

---

## Common failure modes

### "Snapshot integrity check failed"

The S3 `snapshot-sha256` metadata doesn't match the actual file. Indicates:
- S3 corruption (rare but possible). Recover from a previous archive timestamp.
- Bucket write race during archive (C11 idempotency test should catch this; if it surfaces in production, file an incident).

### "Cannot import: stack already has resources"

You picked a target stack name that's already populated. Use a fresh name with timestamp suffix.

### "Archive contains plaintext secrets" (red flag)

The C9 plaintext-secret regex scan should run at archive time. If it surfaces during restore, that's a security incident — escalate to `platform-security-engineer` immediately. Do NOT proceed with the restore until the scan finding is reconciled.

---

## Cross-references

- `docs/runbooks/pulumi-consolidation.md` — invokes this runbook from T3.3 replay drill section
- `infra/scripts/pulumi-archive.sh` — captures snapshot-sha256 metadata (C17)
- `docs/decisions/MEET-INFRA-NORMALIZE-T3-pulumi-consolidation.md` C16 — this runbook is the deliverable
