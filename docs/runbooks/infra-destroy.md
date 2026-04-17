# Runbook: Infrastructure Destroy

**Context:** Safe controlled destroy of a LoopZero Pulumi stack using
`tools/infra-destroy-safe.sh`. Replaces ad-hoc `pulumi destroy` which is
dangerous given the known state drift on the dev stack (memory
`feedback_pulumi_state_drift`).

**Script:** `tools/infra-destroy-safe.sh` (executable, committed)

---

## When to use

- Tearing down dev stack to rebuild from scratch
- Clean-slate validation after architectural change
- End-of-test-campaign when no customer data exists
- Staging reset before fresh deployment

**DO NOT use:**
- On prod with live customer tenants
- Without meeting record for prod destroy
- When you want a partial cleanup (use `admin-evict-tenants.sh` or
  targeted `pulumi destroy --target ...` instead)

---

## Pre-flight (you do this manually, before running the script)

1. **Identify scope** — which stack? (`dev`, `staging`, or `prod`)
2. **Customer impact inventory** — list custom domains, users, tenants
   still active. Get their consent or confirm they are test-only.
3. **Announce maintenance window** (if non-test users exist)
4. **Capture a fresh PG managed backup via DO console** — don't rely
   on automatic daily backup; click "Create backup" in DO UI for safety.
5. **Export Pulumi state manually** — `pulumi stack export --stack $STACK > backup.json`
   as ground truth before running the script (script also does this,
   but redundancy is good).
6. **Confirm quotas** if you plan to recreate — DO droplet limits, VPC
   count, cluster count.

---

## Running the script

```bash
export DO_TOKEN="..."       # needed for orphan verification
export CF_API_TOKEN="..."   # needed for DNS zone snapshot
export PULUMI_CONFIG_PASSPHRASE="..."  # needed if stack encrypted

cd /home/paulo/loopzero
tools/infra-destroy-safe.sh --stack dev
```

The script walks you through 7 stages, **prompting for confirmation**
at step 5. Nothing is destroyed until you type the exact phrase
`DESTROY-{stack}` (e.g., `DESTROY-dev`).

### Progress indicators

Each step prints a header; backups go to `/tmp/loopzero-destroy-backups/<timestamp>/`:

```
════════════════════════════════════════
 STEP 1/7: PRE-FLIGHT
════════════════════════════════════════
 ...
 STEP 2/7: BACKUPS → /tmp/loopzero-destroy-backups/20260417-193000
 ...
 STEP 3/7: QUIESCE
 STEP 4/7: PREVIEW
 STEP 5/7: CONFIRMATION    ← you type DESTROY-{stack} here
 STEP 6/7: DESTROY
 STEP 7/7: ORPHAN CHECK
```

### Prod-specific gate

If `--stack prod`, the script requires an additional gate:

```
Type 'I-HAVE-MEETING-RECORD-AUTHORIZING-PROD-DESTROY' to continue:
```

Don't. Go get the meeting record first.

---

## What the script does NOT do automatically

- Does not delete CF zones (CF preserves zone even if Pulumi doesn't
  manage it). After destroy, manually clean up CF zones if permanent.
- Does not delete DOCR container registry (persisted across stacks
  typically). Verify in DO UI.
- Does not delete Pulumi stack metadata (`pulumi stack rm $STACK`).
  Run that only after you verify orphan check is clean.
- Does not cleanup `/tmp/loopzero-destroy-backups/...` — retain for ≥7 days.
- Does not kill the Ralph loop lock file (`.claude/ralph-loop.local.md`)
  — that's per-session state, not infra.

---

## Orphan cleanup (manual, post-script)

Script's step 7 reports any DO resources still tagged as the stack.
If any appear, run the commands it printed (examples):

```bash
# Cluster orphan (drift leftover)
doctl kubernetes cluster delete <id> --force --dangerous

# Managed DB orphan
doctl databases delete <id> --force

# Droplet orphan (from overflow/prewarm pools created via doctl)
doctl compute droplet delete <id> --force

# Load balancer orphan
doctl compute load-balancer delete <id> --force

# Volume orphan (PVC that Pulumi didn't track)
doctl compute volume delete <id> --force
```

---

## Recovery — if you regret the destroy

### Pulumi state recovery

```bash
pulumi stack import --file /tmp/loopzero-destroy-backups/<timestamp>/pulumi-state.json --stack $STACK
```

This only recovers the **state file**. Actual cloud resources are
gone; Pulumi will try to reconcile → `pulumi up` will recreate them if
their configs are still in `Pulumi.<stack>.yaml`.

### PG recovery

Managed PG has automatic daily backups + you created one manually in
pre-flight step 4. Via DO UI: "Restore from backup" — takes ~10min
for small DBs. New host, new connection string.

### Cloudflare zones

CF zones themselves are **not destroyed** by Pulumi. You only need to
re-add DNS records. Source of truth for records is in Pulumi source.

### K8s CRs

Phase 2a plan in `cell-architecture-phase2.md` has been re-applied via
`pulumi up` after infrastructure recreates. CRs are reconciled from
scratch; tenant CRs are re-created from PG rows if you restored PG.

---

## Cost note

Each destroy-and-recreate cycle has real cost:
- Time for `pulumi up` to create 3 DOKS + managed PG + Spaces + CF:
  ~20-30 min
- Time for platform bootstrap (ArgoCD + Traefik + Gitea + Harbor etc.):
  ~15 min
- Time for tenants to re-provision (if restoring from PG): minutes to hours
  depending on count
- **Wasted infra hours** during destroy → up window: ~$5-10

So roughly a 45-60min downtime + $5-10 for a full dev destroy-recreate.

---

## References

- Script: `tools/infra-destroy-safe.sh`
- Related: `tools/admin-evict-tenants.sh` (scoped cleanup, NOT infra destroy)
- Related: `docs/runbooks/tenant-bulk-evict.md` (tenant-level cleanup)
- Related: `docs/runbooks/runtime-capacity-bump.md` (recreate scenarios)
- Memory: `feedback_pulumi_state_drift` (explains why `pulumi destroy` can
  leave orphans on dev; script's step 7 is the safety net)
