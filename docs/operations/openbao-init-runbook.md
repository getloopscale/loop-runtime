---
type: operational-runbook
title: "Runbook: OpenBao Init Ceremony"
---

# Runbook: OpenBao Init Ceremony

> **Scope:** F1.8 of plan 2026-06-02-openbao-adoption v1.2.
> Operator-only procedure for first-time OpenBao Raft cluster init.
> Generates Shamir shards, captures AWS KMS unseal config, revokes
> root token. Runs ONCE per cluster lifecycle (re-running after init
> requires snapshot restore OR cluster destroy+recreate).
>
> **Authorization:** sponsor + 1 backup person witness (per Q13).
> Estimated time: ~30 min.
>
> **When this runbook is triggered:** este runbook é o conteúdo executável
> do **Sub-step 2.5** do mandate canonical
> ``/.claude/rules/cleanroom-up-live-test-mandate.md § Sub-step 2.5`` *(removido do versionamento)*.
> Disparado SEMPRE que `LOOPZERO_HELMFILE_ENV=dev-public` durante a chain
> Cleanroom canonical. Standalone-multi usa o shortcut
> `task k3d:secrets:bootstrap-in-core` (sem Shamir, sem sponsor JUNTOS) —
> consulte memory `check-openbao-bootstrap-in-core-task_test.sh` para
> trade-offs entre os dois paths.

## Pre-flight (block ceremony if any fail)

```
[ ] AWS account 776217504929 active + KMS key arn known
[ ] Pulumi outputs available: awsKmsKeyArn, openbaoBackup{BucketName,...}
[ ] `task setup` OR equivalent ran — k3d cluster healthy
[ ] OpenBao pods 3/3 Running but SEALED (status check below)
[ ] cert-manager Certificate openbao-tls Ready=True
[ ] No prior init.json residue on operator machine (rm -f /dev/shm/init.json*)
[ ] WiFi/network: prefer air-gap during shard distribution
[ ] 1Password Corporate Vault accessible by sponsor + backup person
[ ] YubiKey FIDO2 available + ready to receive shard 3
[ ] USB encrypted (LUKS) volume mounted at /mnt/openbao-shard-4
[ ] Paper printer available + tamper-evident envelope ready
[ ] Safe deposit box appointment scheduled (within 24h)
```

## Status check (run BEFORE ceremony to confirm state)

```bash
# From operator machine with mgmt cluster kubeconfig:
kubectl -n vault-system get pods -l app.kubernetes.io/name=openbao
# Expected: openbao-{0,1,2} Running 1/1 (or pending if size=1 in standalone)

# Port-forward + check seal status (expect Sealed: true, Initialized: false)
kubectl -n vault-system port-forward svc/openbao 8200:8200 &
export VAULT_ADDR=https://localhost:8200
export VAULT_CACERT=/tmp/openbao-ca.pem
kubectl -n vault-system get secret openbao-tls -o jsonpath='{.data.ca\.crt}' | base64 -d > /tmp/openbao-ca.pem
bao status
# Expected output BEFORE init:
#   Sealed:    true
#   Initialized: false
```

## Ceremony steps (12 + verification 7.5-7.9 per EC-05)

### Step 1 — Air-gap operator machine

Disconnect WiFi + ethernet. Goal: shard material never traverses
network during distribution. Re-connect after step 8.

### Step 2 — Capture init output to RAMdisk (not disk)

```bash
mkdir -p /dev/shm
chmod 700 /dev/shm
```

### Step 3 — Run init with 5-shard / 3-quorum

```bash
bao operator init \
  -key-shares=5 \
  -key-threshold=3 \
  -recovery-shares=0 \
  -recovery-threshold=0 \
  -format=json > /dev/shm/init.json
```

Note: when AWS KMS auto-unseal is configured (production), `bao operator
init` produces RECOVERY keys (not unseal keys). The 5/3 Shamir model
applies to recovery keys in that case. Same custody model.

### Step 4 — Parse shards + root token

```bash
python3 -c "
import json
with open('/dev/shm/init.json') as f:
    d = json.load(f)
keys = d.get('unseal_keys_b64') or d.get('recovery_keys_b64')
assert len(keys) == 5, 'Expected exactly 5 shards'
for i, k in enumerate(keys, start=1):
    with open(f'/dev/shm/shard-{i}.b64', 'w') as out:
        out.write(k + '\n')
print('Shards written: 1-5')
with open('/dev/shm/root_token', 'w') as out:
    out.write(d['root_token'] + '\n')
print('Root token written')
"
chmod 600 /dev/shm/shard-*.b64 /dev/shm/root_token
```

### Step 5 — Compute shard hashes (verification basis)

```bash
for i in 1 2 3 4 5; do
  sha256sum /dev/shm/shard-${i}.b64
done > /dev/shm/shard-hashes.txt
cat /dev/shm/shard-hashes.txt
# Capture this output in encrypted log for step 7.6 verification
```

### Step 6 — Distribute shards per Q13 custody model

Shard 1 → **1Password Corporate Vault** (sponsor + backup person have access):
```
1Password app → New Item (Secure Note)
  Title: "LoopZero OpenBao Shamir Shard 1 of 5"
  Content: <paste shard-1.b64>
  Tags: openbao, shamir, loopzero
Save.
```

Shard 2 → **Sponsor's 1Password personal vault** (2FA hardware key):
```
Sponsor's personal 1Password → New Item (Secure Note)
  Title: "LoopZero OpenBao Shamir Shard 2 of 5"
  Content: <paste shard-2.b64>
```

Shard 3 → **YubiKey FIDO2 OATH-HOTP slot** OR YubiKey resident credential:
```
ykman oath accounts add --secret <shard-3.b64> "LoopZeroOpenBaoShard3"
# OR alternative: write to encrypted USB key kept with YubiKey
```

Shard 4 → **USB encrypted (LUKS) at residence safe**:
```bash
echo "<shard-4.b64>" > /mnt/openbao-shard-4/shard-4.b64
sync
umount /mnt/openbao-shard-4
# Disconnect USB + store in residence fireproof safe
```

Shard 5 → **Paper print + tamper-evident envelope → safe deposit box (bank)**:
```bash
# Print shard 5 (use offline printer; not network-connected)
cat /dev/shm/shard-5.b64 | lp -d offline-printer
# Manually seal in tamper-evident envelope (e.g., 3M security bag)
# Schedule safe deposit box deposit within 24h
```

### Step 7.5 — Hash verification (EC-05)

For EACH of the 5 storage locations:

```bash
# Retrieve shard from storage
# Compute SHA-256 hash
echo "<retrieved-shard>" | sha256sum

# Compare against shard-hashes.txt
diff <(echo "<retrieved-hash>") <(grep "shard-${i}.b64" /dev/shm/shard-hashes.txt | awk '{print $1}')
```

Each shard's hash MUST match. If ANY mismatch, abort + redo distribution.

### Step 7.6 — Retrieve-verify cycle

Have sponsor retrieve shards 1+3+5 (one from each tier) BEFORE shredding.
Have backup person retrieve shards 2+4. Confirm all 5 are intact.

### Step 7.7 — Unseal-test in throwaway cluster

```bash
# Spin throwaway k3d cluster (5 min)
k3d cluster create openbao-shard-test
# Deploy OpenBao single-node + restore snapshot to test recovery
# (Detailed procedure in docs/operations/openbao-disaster-recovery.md)
# Attempt:
bao operator unseal <shard-1.b64>
bao operator unseal <shard-2.b64>
bao operator unseal <shard-3.b64>
bao status
# Expected: Sealed: false (proves 3 shards work)
```

### Step 7.8 — Confirm bao status post-test

If step 7.7 successful + bao status returns Sealed: false → proceed.
If failed → STOP, redo distribution, re-test.

### Step 7.9 — Document successful verification

```bash
# Append to audit log
psql -h $LOOPZERO_DB_HOST -U loopzero -d loopzero -c "
  INSERT INTO audit_log (event_type, actor, payload, timestamp)
  VALUES ('vault.shamir.ceremony.verified',
          '$(whoami)',
          '{\"shards\": 5, \"quorum\": 3, \"witness\": \"<backup-person-email>\"}',
          NOW());
"
```

### Step 8 — Shred RAMdisk (irreversible)

```bash
shred -uvz /dev/shm/shard-*.b64
shred -uvz /dev/shm/init.json
shred -uvz /dev/shm/shard-hashes.txt
# KEEP /dev/shm/root_token for step 9
```

### Step 9 — Login with root token + revoke

```bash
bao login -method=token "$(cat /dev/shm/root_token)"
shred -uvz /dev/shm/root_token

# Confirm OpenBao is operational + engines enabled
bao secrets list
# Expected: secret/ (kv-v2) listed (per F1.4 baoconfig)

# Revoke root token (per Q15 — OIDC humans replace root post-bootstrap)
bao token revoke -self
```

### Step 10 — Reconnect network

After shred + root revoke complete.

### Step 11 — Schedule first quarterly drill

```bash
# Calendar entry: 90 days from today
# Drill restores snapshot to throwaway k3d, unseals with 3 random shards
# from different storages, validates bao status = unsealed.
```

### Step 12 — Audit entry

```bash
# Append final ceremony complete entry
psql -h $LOOPZERO_DB_HOST -U loopzero -d loopzero -c "
  INSERT INTO audit_log (event_type, actor, payload, timestamp)
  VALUES ('vault.shamir.ceremony.complete',
          '$(whoami)',
          '{\"shards\": 5, \"quorum\": 3, \"witness\": \"<backup-person>\",
            \"verification_passed\": true,
            \"root_token_revoked\": true,
            \"next_drill\": \"$(date -d \"+90 days\" -I)\"}',
          NOW());
"
```

## Post-ceremony checklist

```
[ ] All 5 shards confirmed in their 5 storage locations
[ ] Quarterly drill calendar entry created
[ ] Audit log entries committed
[ ] /dev/shm contents shredded (verify: ls /dev/shm/ | grep -E "shard|init|root" → empty)
[ ] Network reconnected
[ ] bao status from operator workstation: Initialized=true, Sealed=false (steady state)
[ ] First Prometheus metric `vault_core_unsealed{instance=*}` ALL = 1
[ ] cert-manager Certificate openbao-tls Status=Ready
```

## Recovery procedure references

- AWS KMS unreachable + need to unseal → [`docs/operations/break-glass-runbook.md`](./break-glass-runbook.md)
- Snapshot restore (cluster loss) → `docs/operations/openbao-disaster-recovery.md` (F1.10+)
- Shard rotation (operator change) → run `bao operator rekey` per Q13

## References

- Plan: [`docs/plans/2026-06-02-openbao-adoption-plan.md`](../plans/2026-06-02-openbao-adoption-plan.md) F1.8
- ADR: `.claude/knowledge-base/decisions/2026-06-02-openbao-adoption.md` (Q13)
- MEET-ASYNC: `MEET-ASYNC-AMENDMENT-2026-06-02-001-openbao-f1-execution.md`
- EC-05: edge review `.claude/knowledge-base/plans/2026-06-02-openbao-adoption-edge-review.md`
