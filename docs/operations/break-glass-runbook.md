# Runbook: OpenBao Break-Glass Recovery

> **CLASSIFIED — operator + sponsor only**
>
> Procedure to recover OpenBao when normal auto-unseal path (AWS KMS)
> is unavailable. Per Q13 of plan 2026-06-02-openbao-adoption v1.2:
> 5/3 Shamir distributed heterogenous custody.
>
> **Scenarios that trigger this runbook:**
> 1. AWS KMS key destroyed (catastrophic — EC-02 protect:true defeated)
> 2. AWS account suspended OR billing issue
> 3. AWS network unreachable for hours
> 4. OpenBao Raft cluster lost + snapshot restore needed without KMS

## Custodian map (locations only; shard content NOT here)

| Shard # | Storage | Custodian | Access procedure |
|---|---|---|---|
| 1 | 1Password Corporate Vault | Sponsor + 1 backup person | Login + 2FA |
| 2 | 1Password personal vault | Sponsor (sole access) | Sponsor's master pw + 2FA hardware key |
| 3 | YubiKey FIDO2 (OATH-HOTP slot OR resident credential) | Sponsor | Physical YubiKey + PIN |
| 4 | USB encrypted (LUKS) at sponsor residence safe | Sponsor | Physical USB + LUKS passphrase |
| 5 | Paper + tamper-evident envelope at bank safe deposit box | Sponsor + bank | Safe deposit box appointment + signature |

**Quorum:** 3 of 5 shards required to unseal OR restore. Any 3 work
(no specific combination required).

## Break-glass procedure

### Phase 1 — Decision gate (5 min)

Confirm break-glass is necessary:

```
[ ] Normal AWS KMS unseal failing (check Prometheus alert OpenBaoSealStatus)
[ ] AWS Console: KMS key state confirmed (scheduled deletion? account
    suspended? region outage?)
[ ] No other path to recovery (e.g., wait for AWS to come back)
[ ] Decision: proceed with Shamir recovery
[ ] Document decision in audit_log with `event_type='vault.breakglass.initiated'`
```

### Phase 2 — Retrieve 3 shards (15-45 min depending on travel)

Sponsor + 1 backup person assemble. Goal: 3 different storage tiers
to maximize recovery probability.

**Recommended retrieval combination:**
- Shard 1 (1Password corporate) — sponsor or backup retrieves
- Shard 2 (sponsor personal 1Password) — sponsor retrieves
- Shard 3 (YubiKey) — sponsor retrieves

This combo can be done in <15 min if all parties have laptop access.

**If sponsor unavailable + emergency:**
- Shard 1 (corporate 1Password — backup person)
- Shard 5 (safe deposit box — backup person if listed on box)
- Shard 4 (residence USB — emergency only; requires backup person physical access)

### Phase 3 — Execute unseal (10 min)

Air-gap operator machine (disconnect network).

```bash
# Spin up OpenBao Raft cluster (assume restored from snapshot OR
# fresh init followed by snapshot import via separate procedure)
kubectl -n vault-system port-forward svc/openbao 8200:8200 &
export VAULT_ADDR=https://localhost:8200
export VAULT_CACERT=/tmp/openbao-ca.pem

# Confirm sealed state
bao status
# Expected: Sealed: true

# Apply 3 shards (one at a time)
bao operator unseal "<shard-1>"
bao operator unseal "<shard-2>"
bao operator unseal "<shard-3>"

# Confirm
bao status
# Expected: Sealed: false
```

### Phase 4 — Document in audit_log (5 min)

```bash
psql -h $LOOPZERO_DB_HOST -U loopzero -d loopzero -c "
  INSERT INTO audit_log (event_type, actor, payload, timestamp)
  VALUES ('vault.breakglass.complete',
          '$(whoami)',
          '{\"shards_used\": [1,2,3],
            \"reason\": \"<aws-kms-loss|aws-suspended|network|raft-loss>\",
            \"witness\": \"<backup-person>\",
            \"recovery_window_minutes\": <total>}',
          NOW());
"
```

### Phase 5 — Restore normal operations (30-60 min)

Once unsealed, restore normal AWS KMS auto-unseal path:

1. Rotate AWS KMS key if compromise suspected (`pulumi up --refresh`)
2. Re-issue access key for `openbao-unseal` IAM user
3. Update Helmfile L2 wiring (pulumi-output.sh re-runs)
4. `bao operator rekey` to generate NEW Shamir shards (old shards
   should be considered exposed if any non-owner accessed during
   break-glass)
5. Redistribute new shards per Q13 custody model (re-run
   `docs/operations/openbao-init-runbook.md` steps 6-7.9)

### Phase 6 — Post-incident review (within 7 days)

- Operator writes incident report
- Sponsor + backup person sign off
- Update Risk Register if new failure mode discovered
- Test recovery procedure improvements in next quarterly drill

## When to refuse break-glass

If sponsor + backup person both unavailable AND custodial paths require
their authorization (e.g., 1Password corporate vault, safe deposit box):

- **DO NOT bypass.** The 3-quorum design protects against single-person
  compromise. Bypassing defeats it.
- Wait for sponsor return. If genuinely catastrophic (e.g., sponsor
  incapacitated permanently), escalate to legal counsel for next-of-kin
  procedure (defined in business continuity plan, outside this runbook).

## Quarterly drill (Q13 mandatory)

Schedule: every 90 days.

```
Drill procedure (~30 min):
1. Operator spins throwaway k3d cluster
2. Restore last successful prod snapshot to k3d
3. Retrieve 3 shards from 3 different storage tiers
4. Attempt unseal — should succeed
5. Verify shard hashes match init manifest
6. Audit log entry `vault.breakglass.drill.complete`
7. Destroy throwaway k3d cluster
```

Drill failure = incident. Operator investigates + corrects within 7 days.

## Annual full DR (broader scope)

Once per year:
- Simulate AWS KMS catastrophic loss (cordon AWS account)
- Execute full Phase 1-5 procedure
- Validate all paths actually work
- Document any gaps + fix before next annual

## References

- Plan: `docs/plans/2026-06-02-openbao-adoption-plan.md` F1.8 + Q13
- ADR: `.claude/knowledge-base/decisions/2026-06-02-openbao-adoption.md` (Q13)
- Init runbook: `docs/operations/openbao-init-runbook.md`
- Disaster recovery: `docs/operations/openbao-disaster-recovery.md` (F1.10+)

## CLASSIFIED notice

This document lists **storage location categories** (which tier, who
has access) but NOT specific addresses (residence street, bank branch,
1Password vault names) — those live in sponsor's encrypted offline
notes + business continuity plan. Document scope: enough to execute
recovery; not enough for an attacker with this file alone to locate shards.
