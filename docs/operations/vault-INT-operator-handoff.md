---
type: operational-runbook
title: Vault Integration Operator Handoff Guide
---

# Vault Integration Operator Handoff Guide

Per Plan 2026-06-02-loopzero-api-vault-integration v1.8. Created iter 24 of Ralph loop session 2026-06-03.

## TL;DR

LoopZero migrated from env-var-as-secret to OpenBao Vault as canonical Source of Truth, using ESO (External Secrets Operator) for distribution. All 6 operational scripts, hermetic tests, runbook procedures, and LIVE validation evidence are complete for **standalone-multi level**. T9.2 (DOKS drill) and T9.3 (prod-like smoke) are operator-time gates.

## Authoritative References

- **Plan**: [`.claude/knowledge-base/plans/completed/2026-06-02-loopzero-api-vault-integration-plan.md`](../wiki/plans/completed/2026-06-02-loopzero-api-vault-integration-plan.md) v1.8
- **LIVE evidence**: [`.claude/knowledge-base/audits/2026-06-03-vault-INT-LIVE-evidence.md`](../wiki/audits/2026-06-03-vault-INT-LIVE-evidence.md)
- **Runbook**: [`vault-secret-migration-runbook.md`](vault-secret-migration-runbook.md) (P1-P6 procedures)
- **ADR**: [`.claude/knowledge-base/decisions/2026-06-02-vault-secrets-sot.md`](../wiki/decisions/2026-06-02-vault-secrets-sot.md) D1-D12

## Architecture (D11 ESO)

```
Vault (loopzero-vault cluster, Shamir 5/3 + Raft + HTTPS)
    ↑ ESO Kubernetes auth method (cross-cluster TokenReview)
ClusterSecretStore "loopzero-vault-secrets" (mgmt cluster, external-secrets NS)
    ↓ pull every refreshInterval (5m-24h per criticality)
13 ExternalSecret CRs (loopzero-system NS, creationPolicy: Orphan — iter 20 fix)
    ↓ creates/updates K8s Secrets
13 K8s Secrets (loopzero-system NS, last-known-good preserved on any failure)
    ↓ valueFrom.secretKeyRef OR envFrom.secretRef
Pod env vars (loopzero-api, loopzero-cloud, loopzero-tenant-operator)
```

## Canonical Scripts (operator-runnable)

All scripts live in `infra/scripts/openbao/`. Pre-flight requirements + flags documented in `--help` of each.

| Script | Purpose | Runbook section | Hermetic test |
|---|---|---|---|
| `vault-bootstrap-eso.sh` | Bootstrap ESO ClusterSecretStore + CA + SA | P1 steps 2-4 | `check-vault-bootstrap-eso_test.sh` (9/9) |
| `seed-loopzero-secrets.sh` | Idempotent per-env Vault seed from operator values file | P1 step 5 | `check-seed-loopzero-secrets_test.sh` (9/9) |
| `vault-sync-check.sh` | LIVE verification: enumera 13 ExternalSecret CRs + assert Ready=True | P1 step 7 + Phase 9 T9.1 | `check-vault-sync_test.sh` (8/8) |
| `rotate-secret.sh` | Atomic cross-binary rotation com ROTATION_TABLE embedded | P2 | `check-rotate-secret_test.sh` (10/10) |
| `enable-vault-backup.sh` | D12 snapshot-agent backup enable (DO Spaces) | P4 | `check-enable-vault-backup_test.sh` (10/10) |
| `restore-vault-snapshot.sh` | DR restore — **dry-run default + `--execute` required** | P5 | `check-restore-vault-snapshot_test.sh` (9/9) |

**Aggregate hermetic: 55 PASS** across 6 script test suites.

## Regression Tests (invariant gates)

| Test | Catches | Source |
|---|---|---|
| `check-rotation-table-parity_test.sh` | rotate-secret ROTATION_TABLE vs canonical inventory drift | iter 17 |
| `check-loopzero-external-secrets-creation-policy_test.sh` | P0 regression: `creationPolicy: Owner` (causes cascade-delete) | iter 20 |
| `check-loopzero-external-secrets-chart-sanity_test.sh` | 8 chart invariants: 13 CRs + unique names + refresh + per-env + vaultBinaryOverride POS/NEG | iter 23 |

**Aggregate gates: 13 PASS** across 3 invariant test suites.

Wired em `task quality:gates` — runs on every CI invocation.

## LIVE Validation Status (Phase 9 T9.1)

| Critical path | Status | Iter |
|---|---|---|
| Full inventory 13/13 ExternalSecret CRs synced | ✅ | 19 |
| Cross-binary D6 shared paths (vaultBinaryOverride) | ✅ | 19 |
| Drift recovery <1s (ESO informer) | ✅ | 19 |
| Idempotency (re-run seed) | ✅ | 19 |
| **Rollback drill** (helm uninstall preserves Secrets) | ✅ post iter 20 fix | 20 |
| Rotation HAPPY PATH (4 STEPS GREEN) | ✅ | 21 |
| Rotation PARTIAL (single binary missing) | ✅ exit 2 + recovery hint | 18 |
| **Vault outage graceful degradation** | ✅ K8s preserved + Error status | 22 |

## P0 Bug Discovery + Fix (iter 20)

**Discovered via LIVE rollback drill**:

Pre-fix `creationPolicy: Owner` set ownerReference with `blockOwnerDeletion: true`. K8s garbage collector cascade-deleted ALL 13 Secrets when chart was uninstalled, BEFORE ESO could honor `deletionPolicy: Retain`. Operator running production rollback during incident → all Secrets deleted → pods CrashLoop → outage at worst possible time.

**Fix**: `creationPolicy: Owner → Orphan` (decouples lifecycle, no ownerReference). LIVE re-validated: 13/13 Secrets preserved after `helm uninstall`.

**Regression protection**: `check-loopzero-external-secrets-creation-policy_test.sh` (4/4 PASS) blocks any future Owner regression.

## Operator Workflow (Production Cutover Path)

### Phase 1: Provisioning per env (one-time)

```bash
# 1. Vault prerequisites (Shamir ceremony, operator-time)
task k3d:vault:create   # or DOKS loopzero-vault-prod provisioning via Pulumi
helmfile -f vault.helmfile.yaml.gotmpl -e <env> sync   # openbao + openbao-init

# 2-4. Bootstrap ESO ClusterSecretStore
export LOOPZERO_MGMT_CONTEXT=k3d-loopzero-mgmt    # or loopzero-mgmt-prod
export LOOPZERO_VAULT_CONTEXT=k3d-loopzero-vault  # or loopzero-vault-prod
bash infra/scripts/openbao/vault-bootstrap-eso.sh

# 5. Seed Vault with secrets (operator-provided values file, chmod 600)
export BAO_ADDR=https://vault.example:8210
export BAO_TOKEN=$(jq -r .root_token /tmp/openbao-init.json)
bash infra/scripts/openbao/seed-loopzero-secrets.sh \
  --env <env> \
  --secret-values-file ~/.loopzero-secrets-<env>.yaml

# 6. Enable Vault in env values + helmfile sync
# Edit infra/helmfile/environments/<env>.yaml: vaultEnabled: true
helmfile -f mgmt.helmfile.yaml.gotmpl -e <env> sync

# 7. Verify
bash infra/scripts/openbao/vault-sync-check.sh --threshold 13
# Expected: Verdict PASS (synced=13)
```

### Phase 2: Backup (P4)

```bash
# Edit infra/helmfile/environments/<env>.yaml: snapshotAgentEnabled: true
bash infra/scripts/openbao/enable-vault-backup.sh --env <env>
# Verifies CronJob registered + reports next-run schedule
```

### Phase 3: Rotation (P2)

```bash
bash infra/scripts/openbao/rotate-secret.sh \
  --env <env> \
  --path api/secrets/gitea/token \
  --value "$NEW_TOKEN"
# Atomic: bao write → ESO refresh → K8s Secret poll Ready → cross-binary
# rollout restart. Exit 0 [COMPLETE]; exit 2 [PARTIAL] com recovery hint.
```

### Phase 4: DR Restore (P5) — DESTRUCTIVE

```bash
# 1. List snapshots
export AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=...
bash infra/scripts/openbao/restore-vault-snapshot.sh --env <env> --list

# 2. Inspect (dry-run is DEFAULT)
bash infra/scripts/openbao/restore-vault-snapshot.sh \
  --env <env> --snapshot-key 2026-06-01T00-00-00Z.snap

# 3. Execute (REQUIRES explicit --execute)
bash infra/scripts/openbao/restore-vault-snapshot.sh \
  --env <env> --snapshot-key 2026-06-01T00-00-00Z.snap --execute

# Post-restore: vault will be RE-SEALED. Re-run Shamir unseal ceremony.
```

### Phase 5: Rollback (P3) — SAFE since iter 20 fix

```bash
# Disable Vault flow per env
helmfile -f mgmt.helmfile.yaml.gotmpl -e <env> \
  --state-values-set vaultEnabled=false sync
# K8s Secrets PRESERVED (Orphan policy + Retain). Pods continue via envFrom.
# Verified LIVE iter 20.
```

## What's NOT Done (Operator-Time)

| Item | Why operator-time | Estimate |
|---|---|---|
| T9.2 DOKS drill (ephemeral prod-parity) | Cost authorization required | ~$0.51/run, ~30min |
| T9.3 prod-like smoke | Requires DOKS loopzero-vault-prod cluster up | Operator decision |
| /dogfood full health score >=70 | Requires full loopzero-api + loopzero-cloud deployment | Standard dogfood procedure |
| Production cutover | Phase 9 declaration "complete" gate | Sponsor decision |

## Risk Posture

**Production-safe today (T9.1 evidence-backed)**:
- ✅ Rollback procedure safe (Orphan policy, LIVE-validated)
- ✅ Vault outage doesn't take down service (K8s Secret preserved + Status=Error)
- ✅ Drift recovery <1s (ESO informer + content-compare)
- ✅ Security invariants (no raw secret leak in any script dry-run)
- ✅ Cross-binary rotation atomic (PARTIAL semantics on failure)

**Operator-decision gates before prod cutover**:
- ⏳ T9.2 DOKS drill execution
- ⏳ T9.3 prod-like smoke
- ⏳ Per-env values file provisioning (Shamir shards, sensitive)
- ⏳ Monitoring + alerting setup (ESO Prometheus rules)

## Quick Sanity Check

Validate the entire toolchain is in place locally:

```bash
# 1. Hermetic tests pass
task quality:gates 2>&1 | grep -E "check-(seed-loopzero-secrets|rotate-secret|vault-sync|vault-bootstrap-eso|enable-vault-backup|restore-vault-snapshot|rotation-table-parity|loopzero-external-secrets-(creation-policy|chart-sanity))_test"

# 2. Scripts in place
ls infra/scripts/openbao/*.sh
# Expected: 6 .sh files (vault-bootstrap-eso, seed-loopzero-secrets, vault-sync-check, rotate-secret, enable-vault-backup, restore-vault-snapshot)

# 3. Chart renders
helm template loopzero-external-secrets infra/helm/loopzero-external-secrets --set envName=vault-local > /tmp/render.yaml
grep -c "^kind: ExternalSecret" /tmp/render.yaml
# Expected: 13

# 4. Helmfile per-env wires
grep -l "vaultEnabled" infra/helmfile/environments/*.yaml
# Expected: standalone-multi.yaml + dev.yaml + drill.yaml + prod.yaml
```

If all 4 checks pass, **the toolchain is operator-ready**.
