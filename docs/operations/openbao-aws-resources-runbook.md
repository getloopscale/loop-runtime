---
type: operational-runbook
title: OpenBao AWS Resources Runbook
---

# OpenBao AWS Resources Runbook

**Owner**: Platform Security
**SOT**: this file
**Companion**: [openbao-init-runbook.md](./openbao-init-runbook.md), [break-glass-runbook.md](./break-glass-runbook.md)
**Provisioner**: `infra/scripts/openbao/provision-aws-resources.sh`
**ADR**: `.claude/knowledge-base/decisions/2026-06-02-openbao-adoption.md` (Q2 AWS KMS root-of-trust)

## Scope

OpenBao auto-unseal uses an **AWS KMS** CMK as HSM root-of-trust. Snapshots are
persisted to **DigitalOcean Spaces** (S3-compatible). Each environment
(`dev | staging | prod`) has its own isolated set of resources — KMS keys,
IAM users, and Spaces buckets are **never shared** across environments.

Per the **DOxAWS lifecycle independence** decision ratified 2026-06-02, the
AWS side is provisioned **out-of-band** via a shell script (not Pulumi). The
DigitalOcean side is provisioned via Pulumi (separate `loopzero-infra` stack per
env). This reflects FAANG pattern of separating CSP lifecycles when their
release cadence + blast radius differ.

## Resource inventory (per environment)

| Resource | Name | Region | Notes |
|----------|------|--------|-------|
| AWS KMS CMK | `alias/loopzero-openbao-<env>` | `us-east-1` | Symmetric, `ENCRYPT_DECRYPT`, rotation **enabled**, `protect = true` |
| AWS IAM user | `openbao-unseal-<env>` (path `/loopzero/`) | global | Resource-scoped inline policy `openbao-unseal-policy` |
| AWS IAM access key | 1 per user (auto-minted) | global | Only place secret is retrievable; cached in `~/.openbao-<env>-resources.env` |
| DO Spaces bucket | `loopzero-openbao-backups-<env>` | per env (see matrix) | Stores Raft snapshots + retention 7d/30d/12w |

### Region matrix

| ENV | Mgmt cluster region | DO Spaces bucket region | Rationale |
|-----|---------------------|--------------------------|-----------|
| dev | sfo3 | sfo3 | Co-located (low latency, $0 egress) |
| staging | nyc3 | nyc3 | Co-located |
| prod | nyc3 | **sfo3** (cross-region) | DR: survives full nyc3 outage |

## Output file format

`provision-aws-resources.sh` writes `$HOME/.openbao-<env>-resources.env`
(`chmod 600`). The file contains all credentials needed by Helmfile L2 to
configure the OpenBao chart:

```bash
OPENBAO_AWS_REGION=us-east-1
OPENBAO_AWS_KMS_KEY_ID=...
OPENBAO_AWS_KMS_KEY_ARN=arn:aws:kms:us-east-1:<account>:key/...
OPENBAO_AWS_ACCESS_KEY_ID=AKIA...
OPENBAO_AWS_SECRET_ACCESS_KEY=...
OPENBAO_BACKUP_BUCKET=loopzero-openbao-backups-<env>
OPENBAO_BACKUP_REGION=<sfo3|nyc3>
OPENBAO_BACKUP_ENDPOINT=https://<region>.digitaloceanspaces.com
OPENBAO_BACKUP_ACCESS_KEY_ID=...      # from .env SPACES_ACCESS_KEY_ID
OPENBAO_BACKUP_SECRET_ACCESS_KEY=...  # from .env SPACES_SECRET_ACCESS_KEY
```

**Never commit** this file. Path is in `$HOME`, not in the repo tree, so it
is naturally outside git.

## Procedures

### P1 — Initial provisioning (per env)

```bash
# 1. Verify pre-requisites
aws sts get-caller-identity        # must succeed with a user that can
                                    # iam:Create* + kms:CreateKey + s3:CreateBucket
test -f .env && grep -q '^SPACES_ACCESS_KEY_ID=' .env

# 2. Provision (idempotent — re-running = no-op if resources exist)
infra/scripts/openbao/provision-aws-resources.sh --env dev
infra/scripts/openbao/provision-aws-resources.sh --env staging
infra/scripts/openbao/provision-aws-resources.sh --env prod

# 3. Verify outputs
ls -la ~/.openbao-{dev,staging,prod}-resources.env
aws iam list-users --path-prefix /loopzero/
aws kms list-aliases --query 'Aliases[?starts_with(AliasName, `alias/loopzero-openbao-`)]'
```

### P2 — Helmfile sync (after provisioning)

```bash
# Source env-specific resources file BEFORE invoking helmfile
set -a
source ~/.openbao-prod-resources.env
set +a

# Sync OpenBao only (avoid touching unrelated releases)
cd infra/helmfile
helmfile -e prod sync --selector name=openbao
helmfile -e prod sync --selector name=bank-vaults-operator
```

The Helmfile gotmpl ([`infra/helmfile/mgmt.helmfile.yaml.gotmpl`](../../infra/helmfile/mgmt.helmfile.yaml.gotmpl))
uses `requiredEnv` for all `OPENBAO_*` keys — if any is missing the sync
fails fast with a clear error.

### P3 — IAM access key rotation

Rotate the access key when:
- The previous key is suspected leaked
- 90-day rotation schedule reaches its deadline (set calendar reminder)
- Operator handover happens

```bash
# 1. Mint new key (deletes old + creates new in one step)
infra/scripts/openbao/provision-aws-resources.sh --env prod --rotate-key

# 2. Re-source the new .env file (it was overwritten with the new key)
set -a
source ~/.openbao-prod-resources.env
set +a

# 3. Trigger OpenBao to pick up the new credentials
#    (Helm sync re-renders the Secret + restarts the StatefulSet pods)
helmfile -e prod sync --selector name=openbao
```

**IMPORTANT**: `--rotate-key` is a destructive operation against the old
access key. There is a brief window (seconds) during which OpenBao pods
running with the old key may fail KMS calls. Schedule during a maintenance
window OR rolling-restart pods explicitly after sync.

### P4 — KMS key rotation (annual, manual)

AWS KMS supports **automatic** annual rotation (already enabled by the
script — `kms:enable-key-rotation`). Manual operator action is needed
**only** if you want to rotate **off-schedule** (e.g., compliance event,
suspected key leak — though KMS material is never exposed, the *access* to
it might be).

Off-schedule manual rotation procedure (operator-time, requires planning):

```bash
# 1. Create new KMS key (do NOT reuse alias yet)
NEW_KEY=$(aws kms create-key \
  --description "OpenBao Raft seal key (prod) — replacement YYYY-MM-DD" \
  --query 'KeyMetadata.KeyId' --output text)
aws kms enable-key-rotation --key-id "$NEW_KEY"

# 2. Decrypt the existing OpenBao seal blob with the OLD key, re-encrypt with NEW key.
#    OpenBao operator command for KMS seal rewrap:
kubectl -n vault-system exec openbao-0 -- bao operator seal-rewrap

# 3. Verify all pods are healthy + unsealed with new key
kubectl -n vault-system get pods -l app.kubernetes.io/name=openbao
kubectl -n vault-system exec openbao-0 -- bao status | grep -i sealed   # must be false

# 4. Swap alias to point at new key
aws kms update-alias --alias-name alias/loopzero-openbao-prod --target-key-id "$NEW_KEY"

# 5. Schedule deletion of the OLD key (NEVER deletes immediately;
#    AWS enforces 7-30d window).
aws kms schedule-key-deletion --key-id <OLD_KEY_ID> --pending-window-in-days 30
```

**Reversibility window**: 30 days. If a problem surfaces, run
`aws kms cancel-key-deletion --key-id <OLD_KEY_ID>` and re-rewrap.

### P5 — Destroy (per env)

`--destroy` deletes IAM user + Spaces bucket + output file. It **does not**
delete the KMS CMK (intentional — loss of CMK permanently invalidates all
snapshots encrypted with it).

```bash
infra/scripts/openbao/provision-aws-resources.sh --env dev --destroy
```

To also destroy the KMS key (irreversible after 7-30d):

```bash
KEY_ID=$(aws kms describe-key --key-id alias/loopzero-openbao-dev --query 'KeyMetadata.KeyId' --output text)
aws kms delete-alias --alias-name alias/loopzero-openbao-dev
aws kms schedule-key-deletion --key-id "$KEY_ID" --pending-window-in-days 7
```

**Before scheduling deletion**: confirm no production cluster still has
OpenBao pods that depend on this key. Once the deletion window expires,
recovery is **impossible** and all snapshots become permanently
undecryptable.

### P6 — Disaster recovery from snapshot

Triggered when: OpenBao quorum is permanently lost (e.g., simultaneous
failure of all 3 nodes + their PVs), but the KMS key + Spaces snapshots
are intact.

```bash
# 1. Identify the latest healthy snapshot
aws --endpoint-url https://sfo3.digitaloceanspaces.com \
    s3 ls s3://loopzero-openbao-backups-prod/snapshots/ \
    | sort -r | head -5

# 2. Download it locally
aws --endpoint-url https://sfo3.digitaloceanspaces.com \
    s3 cp s3://loopzero-openbao-backups-prod/snapshots/<TIMESTAMP>.snap /tmp/

# 3. Recreate fresh 3-node OpenBao cluster (Helmfile sync)
helmfile -e prod sync --selector name=openbao

# 4. Restore snapshot
kubectl -n vault-system cp /tmp/<TIMESTAMP>.snap openbao-0:/tmp/snapshot.snap
kubectl -n vault-system exec openbao-0 -- \
    bao operator raft snapshot restore /tmp/snapshot.snap

# 5. Validate: list a known secret to confirm decryption works end-to-end
kubectl -n vault-system exec openbao-0 -- bao kv list secret/
```

Cross-reference: [break-glass-runbook.md](./break-glass-runbook.md) for
Shamir-shard recovery if the snapshot itself is also lost.

## Failure-mode summary

| Scenario | Symptoms | Recovery |
|----------|----------|----------|
| IAM access key leaked | Suspicious CloudTrail events | P3 rotation |
| IAM access key expired (90d) | OpenBao logs `InvalidClientTokenId` | P3 rotation |
| KMS key permanently lost | `bao operator unseal` returns `KMSInvalidStateException` | **Unrecoverable**; restore from offline-stored Shamir shards per break-glass runbook |
| Spaces bucket deleted | Snapshot cron logs `NoSuchBucket` | Re-run `provision-aws-resources.sh --env <ENV>` (idempotent — recreates only the bucket) |
| Quorum lost, snapshots intact | OpenBao API 503 | P6 disaster recovery |
| Quorum lost, snapshots lost | OpenBao API 503 + S3 ListObjects empty | Break-glass: rebuild OpenBao + manual re-seed of secrets |

## Cost notes (2026-06-02 baseline)

| Resource | Quantity | Unit cost | Monthly |
|----------|----------|-----------|---------|
| AWS KMS CMK | 3 (dev+staging+prod) | $1.00 each | **$3.00** |
| AWS KMS API calls | < 1M/mo expected (unseal on pod start + snapshot encrypt 24×30 = 720) | $0.03/10k | < $0.01 |
| DO Spaces bucket | 3 | $5/mo includes 250GB | **$15.00** (charged only if any single bucket > 250GB) |
| AWS IAM | 3 users | $0 | $0 |
| **Total expected** | | | **$3-18/mo** depending on snapshot volume |

KMS cost is irreducible (HSM-backed key). DO Spaces cost is the only
variable; with snapshot retention of 7d hourly + 30d daily + 12w weekly
(per Q12 of ADR), expected disk use < 100MB per env even at scale.

## Anti-patterns

- **DO NOT** `aws iam delete-user` without first deleting the user's access
  keys + inline policies (AWS refuses).
- **DO NOT** `aws s3 rb` (without `--force`) on a non-empty bucket (AWS
  refuses). The script handles this via `--force`.
- **DO NOT** schedule KMS key deletion as a "cleanup" reflex. The
  deletion window (7-30d) is the **only** reversal opportunity. Always
  verify zero dependencies first via `aws kms list-grants` +
  CloudTrail `LastUsedDate`.
- **DO NOT** commit `~/.openbao-*-resources.env` to git, paste it in
  Slack, or share via email. If exposed: immediately run P3 rotation +
  audit CloudTrail for unauthorized KMS calls.
- **DO NOT** edit `provision-aws-resources.sh` to share KMS keys across
  envs. Per-env isolation is the security invariant — a compromised dev
  key must not affect prod snapshots.

## Related

- [openbao-init-runbook.md](./openbao-init-runbook.md) — Shamir initialization ceremony
- [break-glass-runbook.md](./break-glass-runbook.md) — Recovery without quorum
- ADR `2026-06-02-openbao-adoption.md` (Q2 AWS KMS choice rationale)
- Helmfile wiring: [`infra/helmfile/mgmt.helmfile.yaml.gotmpl`](../../infra/helmfile/mgmt.helmfile.yaml.gotmpl) (openbao release `set:` stanza)
