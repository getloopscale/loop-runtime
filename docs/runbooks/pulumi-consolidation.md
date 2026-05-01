# Pulumi Consolidation Runbook (T3.1 + T3.2 + T3.3)

> Plan reference: `docs/plans/infra-normalize-multicell-plan.md` Phase 3.
> Source decision: `docs/decisions/MEET-INFRA-NORMALIZE-T3-pulumi-consolidation.md` (condition C10).
> Last updated: 2026-04-30.
> Audience: infra operator + change-governor + platform-architect (witness).

This runbook executes Phase 3 of the infra-normalize plan: archive + remove + delete-code for the 3 paralelas Pulumi dirs (`infra/pulumi-{infra,platform,control-plane}/`). Three commits, three days minimum spacing per condition C13.

---

## Pre-flight (DO NOT SKIP)

1. **3 governance approvals.** `MEET-INFRA-NORMALIZE-T3-pulumi-consolidation.md` shows APPROVED. All 29 conditions checked OR explicitly tracked as deferred with rationale.
2. **Archive bucket exists.** `aws s3api head-bucket --bucket loopzero-archive` returns 0. `aws s3api get-bucket-encryption --bucket loopzero-archive` reports SSE-KMS. Object Lock enabled, governance mode, 90d retention. Provisioned in `infra/pulumi/` BEFORE running this runbook (C4).
3. **CloudTrail logging.** Trail captures loopzero-archive bucket events (C8). Verified via `aws cloudtrail describe-trails`.
4. **Required env.** `PULUMI_CONFIG_PASSPHRASE` (fetched from mgmt-platform-secrets per C5), `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`.
5. **Pulumi version pinned.** `pulumi version` returns `3.140.0` per C17.
6. **Validation matrix GREEN.** `task validate:matrix` — 10/10 PASS.

If ANY pre-flight step fails: ABORT.

---

## T3.1 — Archive (commit 1 of 3)

```
# Dry-run first to inspect what will be archived
bash infra/scripts/pulumi-archive.sh --dry-run

# Live run
bash infra/scripts/pulumi-archive.sh
```

**Expected outcome.** `loopzero_pulumi_archives_total` increments for each (dir, stack) tuple. Archive files in `s3://loopzero-archive/pulumi-state/<dir>/<stack>/<ts>.json` with KMS encryption + private ACL.

**Failure modes:**
- exit 2: archive bucket missing/wrong ACL → fix bucket, retry.
- exit 3: live cloud resources detected → operator MUST verify per C18:
  - DigitalOcean: `doctl compute droplet list`, `doctl databases list`, `doctl kubernetes cluster list` cross-referenced against the stack's URN registry.
  - Cloudflare: zone/tunnel name cross-ref against `pulumi/Pulumi.dev.yaml` outputs.
  - AWS: `aws s3 ls`, `aws iam list-users`, etc — same cross-ref.
  If any unexpected live resource: the stack OWNS something we didn't expect → ABORT Phase 3 and escalate.
- exit 4: S3 upload failed → check AWS creds, retry.

**Commit 1.** After all stacks archived:
```
git add docs/audits/pulumi-state-inventory.md   # T0.2 output
git add docs/audits/pulumi-consolidation-log.md  # NEW per C15
# Note: scripts already committed in earlier commits; this commit is
# the inventory + audit log artefacts only.
```

---

## T3.2 — Stack rm (commit 2 of 3)

Wait minimum 24h after T3.1 archive (allows operator to cross-verify the inventory).

```
# Dry-run
bash infra/scripts/pulumi-orphan-rm.sh --dry-run

# Live run
bash infra/scripts/pulumi-orphan-rm.sh
```

**Expected outcome.** `pulumi stack ls` per paralelas dir returns 0 stacks. `loopzero_pulumi_stacks_total` gauge reflects post-rm count (canonical only).

**Failure modes:**
- exit 2: latest archive too old → re-run T3.1 first.
- exit 2: stack still has resources → operator must `pulumi destroy` first (separate decision).
- exit 3: `pulumi stack rm` returned non-zero → check Pulumi backend access; if state corruption: invoke restore (next section).

**Commit 2.** State has been mutated; document via append to audit log:
```
echo "T3.2 rm executed at $(date -u +%FT%TZ)" >> docs/audits/pulumi-consolidation-log.md
git add docs/audits/pulumi-consolidation-log.md
```

---

## T3.3 — Code delete (commit 3 of 3)

Wait minimum 24h after T3.2 stack rm. Run the **archive replay drill** during this window per C22 + C26.

### Archive replay drill (within 14 days of T3.2)

```
# Pick one archived stack at random
random_stack=$(aws s3api list-objects-v2 --bucket loopzero-archive --prefix pulumi-state/ \
  --query 'Contents[?contains(Key, `pulumi-platform`)].Key | [-1]' --output text)

# Download
aws s3 cp "s3://loopzero-archive/${random_stack}" /tmp/replay-test.json

# Restore to a SCRATCH stack (not the canonical project)
cd infra/pulumi
pulumi stack init replay-test-$(date +%s)
pulumi stack import --file /tmp/replay-test.json
pulumi stack ls --json | jq '.[] | select(.name | startswith("replay-test"))'

# Verify the imported stack has the resource set we expect
pulumi stack --json | jq '.resources | length'
```

If the import fails OR resource count != expected (per pre-archive inventory): C26 says **retroactive REJECTED** — invoke C19 rollback (state restore from archive into a fresh canonical stack), retry, escalate.

### After drill GREEN

```
# rm the paralelas dirs + provision-homolog.sh + seed_e2e.go (per C2 + C7 + C28)
git rm -r infra/pulumi-infra infra/pulumi-platform infra/pulumi-control-plane infra/pulumi-shared
git rm infra/scripts/provision-homolog.sh
# C28: seed_e2e.go scope decision documented elsewhere; if homolog-only,
# delete; if generic, relocate.
```

**Commit 3.** Forward-only deletion. Per C29 the CHANGELOG entry uses product language only:
```
# CHANGELOG.md [Unreleased] under [Removed]:
- Pulumi state for 3 unused project directories archived to S3
  and removed (operational consolidation; no customer-facing impact).
```

Commit + push + open PR. Reviewers per repo policy.

---

## Rollback (any phase) — state restore

Per condition C19, state-only rollback SLO ≤ 15 min wall-clock.

```
# Identify which archive to restore
aws s3 ls s3://loopzero-archive/pulumi-state/<dir>/<stack>/ --recursive

# Pick the most recent
archive_ts="<YYYYMMDDTHHMMSSZ>"
aws s3 cp "s3://loopzero-archive/pulumi-state/<dir>/<stack>/${archive_ts}.json" /tmp/restore.json

# Restore into the canonical project (NOT the paralelas dir, which is gone).
# This requires URN rewriting — see docs/runbooks/pulumi-state-restore.md
# for the URN-rewrite procedure.
cd infra/pulumi
pulumi stack init "<stack-name>-restored"
pulumi stack import --file /tmp/restore.json
```

**Important.** Restoring state does NOT recreate cloud resources. If `pulumi destroy` already ran against the paralelas stack, the cloud resources are gone — `pulumi up` is needed to re-provision, which has its own cost + downtime semantics.

---

## Quarterly archive scan (post-Phase-3)

Per condition C12, `.github/workflows/pulumi-archive.yml` (NEW, deferred to a follow-up commit because the workflow needs the canonical project + AWS env populated) runs the archive script in `--archive-only` mode quarterly. Drift in any stack triggers SRE.

The metric `loopzero_pulumi_archives_total` should increment exactly once per quarter per stack. A stale value > 90d fires `PulumiArchiveStaleness` (medium severity).

---

## Cross-references

- `docs/decisions/MEET-INFRA-NORMALIZE-T3-pulumi-consolidation.md` — conditions C1-C29
- `docs/runbooks/pulumi-state-restore.md` — URN-rewrite restore procedure (C16)
- `docs/audits/pulumi-state-inventory.md` — T0.2 output (precondition for T3.1)
- `docs/audits/pulumi-consolidation-log.md` — append-only audit trail (C15)
- `infra/scripts/pulumi-archive.sh` — T3.1 implementation (C14)
- `infra/scripts/pulumi-orphan-rm.sh` — T3.2 implementation (C14)
- `infra/helm/monitoring/manifests/prometheusrule-pulumi-state.yaml` — C20 alerts
- `api/internal/metrics/metrics.go` — `loopzero_pulumi_archives_total` + `loopzero_pulumi_stacks_total` (C20)

---

## Section anchors (for runbook_url annotations)

### archive-staleness

The `PulumiArchiveStaleness` alert fires when `loopzero_pulumi_archives_total` has not incremented in >90 days. Triage:

1. Check `.github/workflows/pulumi-archive.yml` was triggered (Actions tab).
2. Check the workflow logs for archive errors.
3. Confirm AWS S3 access for the archive bucket: `aws s3 ls s3://loopzero-archive/pulumi-state/`.
4. If access works but no recent objects, manually invoke: `bash infra/scripts/pulumi-archive.sh --archive-only`.

### orphan-stack-detected

The `PulumiOrphanStackDetected` alert fires when stack count exceeds 4 (dev/prod/staging/homolog). Triage:

1. `cd infra/pulumi && pulumi stack ls --json | jq '.[].name'` — list current stacks.
2. Compare against canonical names.
3. For each unexpected stack:
   - If experimental: `pulumi stack rm <name> --yes` after confirmation with the operator who created it.
   - If paralelas regression (someone re-introduced a paralelas dir): immediate revert PR + escalate to platform-architect for root-cause review (process violation).
4. After cleanup, `loopzero_pulumi_stacks_total` should drop ≤ 4 within 1 collection cycle (10min).
