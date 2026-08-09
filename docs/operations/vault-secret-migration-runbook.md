---
type: operational-runbook
title: Vault Secret Migration Runbook
---

# Vault Secret Migration Runbook

**Owner**: Platform Security
**Plan**: `.claude/knowledge-base/plans/2026-06-02-loopzero-api-vault-integration-plan.md` v1.8
**ADR**: `.claude/knowledge-base/decisions/2026-06-02-vault-secrets-sot.md` (D1-D12)
**SOT**: this file

## Scope

Operator-time procedures for migrating loopzero-api / loopzero-cloud / tenant-operator
secrets from env-var-as-SoT to Vault-as-SoT via ESO (D11). Includes:
provisioning, rotation, rollback, and break-glass.

Reflete D11 (ESO supersedes pkg/vault) + D12 (snapshot-agent for backups) +
v1.8 SDK sweep findings (3 follow-up improvements flagged outside scope).

## Architecture summary

```
┌─────────────────── loopzero-secrets cluster ────────────────────┐
│  OpenBao (Shamir 5/3 + Raft persistent + HTTPS)            │
│  ├── kv-v2 engine at /secret                               │
│  ├── kubernetes auth method                                │
│  │   ├── role `eso` → policy `eso-read` (recursive)        │
│  │   └── role `bao-snapshot` → policy `snapshot`           │
│  └── CronJob openbao-snapshot-agent → DO Spaces            │
└────────────────────────┬───────────────────────────────────┘
                         │ TokenReview cross-cluster
                         │ kubectl port-forward 8210 (local)
                         │ OR direct VPC peering (DOKS prod)
                         ▼
┌────────────────── loopzero-core cluster ──────────────────────┐
│  ESO operator (mgmt-side, NS external-secrets)             │
│  ├── ClusterSecretStore loopzero-secrets-secrets                 │
│  │   ├── server: https://host.k3d.internal:8210 (local)    │
│  │   ├── auth: kubernetes (role eso, SA eso-vault-reader)  │
│  │   └── caProvider: ConfigMap loopzero-secrets-ca               │
│  └── 13 ExternalSecret CRs (loopzero-external-secrets chart)   │
│      ├── loopzero-api group (10 targets, 16 secrets)           │
│      ├── loopzero-cloud group (2 targets, 3 secrets)           │
│      └── tenant-operator group (1 target, 7 keys consolidated)
│         ↓ ESO Login → KvGet per refreshInterval            │
│         ↓ creates/updates K8s Secrets em loopzero-system       │
│                                                             │
│  loopzero-api / loopzero-cloud / tenant-operator pods              │
│    ↑ env via secretKeyRef OR envFrom.secretRef             │
│    └── ZERO Go code change                                 │
└────────────────────────────────────────────────────────────┘
```

## Vault path convention (D6)

```
secret/data/loopzero/<env>/<binary>/<category>/<key>

where:
  <env>      = dev | drill | staging | prod | vault-local | standalone-multi
  <binary>   = api | cloud | operators
  <category> = secrets | config
  <key>      = kebab/slash path (e.g. gitea/token, minio/secret_key)

Each Vault entry stored as KV-v2 data: { "value": "<actual_secret>" }
(property "value" matches `remoteRef.property` in ExternalSecret template).
```

Shared secrets (cross-binary, D6 + Phase 3.3 vaultBinaryOverride): operators
that need `GITEA_TOKEN` reuse the path `loopzero/<env>/api/secrets/gitea/token`
via `vaultBinaryOverride: api`. Single rotation point.

## P1 — Initial provisioning (per env, one-time)

```bash
# 1. Vault prerequisites (Phase 0 of openbao-adoption — done if PRODLIKE up)
task k3d:secrets:create
task k3d:secrets:bootstrap   # cert-manager + cluster-issuer + openbao-cert + openbao
# Operator-time Shamir init + unseal (per secrets-cluster-local-runbook P2/P3)
# kubectl create secret openbao-init-token --from-literal=BAO_TOKEN=$ROOT_TOKEN
helmfile -f infra/helmfile/secrets.helmfile.yaml.gotmpl -e vault-local \
  --kube-context k3d-loopzero-secrets --selector name=openbao-init sync

# 2-4. Canonical (recommended) — codified P1 steps 2-4 via vault-bootstrap-eso.sh:
bash infra/scripts/openbao/vault-bootstrap-eso.sh \
  --mgmt-context k3d-loopzero-core \
  --vault-context k3d-loopzero-secrets
# Applies SA + ClusterRoleBinding + extracts CA + creates ConfigMap +
# applies ClusterSecretStore + waits for Ready=True. Exit codes:
#   0=Ready, 2=apply failure, 3=Ready timeout.

# Manual fallback (legacy — use for debugging or non-standard envs):
# 2. Phase 3.1 — apply ClusterSecretStore + SA on mgmt cluster
kubectl --context k3d-loopzero-core apply \
  -f infra/manifests/cluster-secret-stores/eso-vault-reader-sa.yaml

# 3. Extract Vault CA cert + create ConfigMap on mgmt cluster
VAULT_CA=$(kubectl --context k3d-loopzero-secrets -n vault-system \
  get secret openbao-tls -o jsonpath='{.data.ca\.crt}' | base64 -d)
kubectl --context k3d-loopzero-core -n external-secrets create configmap loopzero-secrets-ca \
  --from-literal=ca.crt="$VAULT_CA" \
  --dry-run=client -o yaml | kubectl --context k3d-loopzero-core apply -f -

# 4. Apply ClusterSecretStore (depends on SA + ConfigMap)
kubectl --context k3d-loopzero-core apply \
  -f infra/manifests/cluster-secret-stores/loopzero-secrets-secrets.yaml

# 5. Seed initial secrets in Vault (one-time per env)
# Use seed-loopzero-secrets.sh script (T8.1 follow-up; for now manual):
ROOT_TOKEN=$(jq -r .root_token /tmp/openbao-init.json)
kubectl --context k3d-loopzero-secrets -n vault-system exec openbao-0 -- sh -c "
export BAO_ADDR=https://127.0.0.1:8200 BAO_SKIP_VERIFY=true BAO_TOKEN=$ROOT_TOKEN
bao kv put secret/loopzero/vault-local/api/secrets/gitea/token value=<actual>
bao kv put secret/loopzero/vault-local/api/secrets/minio/access_key value=<actual>
# ... (repeat per inventory in infra/helm/loopzero-external-secrets/values.yaml)
"

# 6. Enable Vault path in core.helmfile per env
# Edit infra/helmfile/environments/standalone-multi.yaml:
#   vaultEnabled: true
helmfile -f infra/helmfile/core.helmfile.yaml.gotmpl -e standalone-multi \
  --selector name=external-secrets sync
helmfile -f infra/helmfile/core.helmfile.yaml.gotmpl -e standalone-multi \
  --selector name=loopzero-external-secrets sync

# 7. Verify ExternalSecret status
kubectl --context k3d-loopzero-core -n loopzero-system get externalsecrets
# Each row should show STATUS=SecretSynced
```

## P2 — Secret rotation procedure

**Canonical (recommended):** use `rotate-secret.sh` (T8.1 EC-22 atomic rotation).
The script wraps all 4 steps below + cross-binary restart with rollout status wait.

```bash
export BAO_ADDR=https://127.0.0.1:8210 BAO_SKIP_VERIFY=true
export BAO_TOKEN=$(jq -r .root_token /tmp/openbao-init.json)
bash infra/scripts/openbao/rotate-secret.sh \
  --env vault-local \
  --path api/secrets/gitea/token \
  --value "$NEW_TOKEN"
# [STEP 1/4] bao kv put → [STEP 2/4] ESO force-refresh → [STEP 3/4] poll
# Ready → [STEP 4/4] rolling restart loopzero-api + loopzero-tenant-operator
# Exit 0 = complete; exit 2 = PARTIAL (Vault updated, restart failed)
```

The script reads the embedded rotation table to know which deployments + which
ExternalSecret CRs are affected by a given Vault path. Unknown paths fail-fast
(EC-22 typo guard). For PARTIAL exit codes, follow recovery hints in script
stderr output.

**Manual procedure** (legacy — use only for debugging or unsupported paths):

```bash
# Step 1: Update Vault (single SoT — Vault is canonical)
ROOT_TOKEN=$(jq -r .root_token /tmp/openbao-init.json)
kubectl --context k3d-loopzero-secrets -n vault-system exec openbao-0 -- sh -c "
export BAO_ADDR=https://127.0.0.1:8200 BAO_SKIP_VERIFY=true BAO_TOKEN=$ROOT_TOKEN
bao kv put secret/loopzero/<env>/api/secrets/gitea/token value=<new-token>
"

# Step 2: ESO picks up change within refreshInterval (1h default;
# 5m for high-rotation paths per values.yaml). Force immediate refresh:
kubectl --context k3d-loopzero-core -n loopzero-system annotate externalsecret gitea-admin \
  force-sync=$(date +%s) --overwrite

# Step 3: Verify K8s Secret updated
kubectl --context k3d-loopzero-core -n loopzero-system get secret gitea-admin \
  -o jsonpath='{.data.token}' | base64 -d
# (matches new value)

# Step 4 (CROSS-BINARY rotation per D2 EC-22 amendment): restart ALL
# binaries reading this path. GITEA_TOKEN used by loopzero-api AND tenant-operator.
kubectl --context k3d-loopzero-core -n loopzero-system rollout restart deployment loopzero-api
kubectl --context k3d-loopzero-core -n loopzero-system rollout restart deployment loopzero-tenant-operator
```

**Rotation table (per shared path → binaries to restart)**:

| Vault path | Binaries to restart | RefreshInterval |
|------------|---------------------|-----------------|
| `api/secrets/gitea/token` | loopzero-api + tenant-operator | 1h |
| `api/secrets/minio/access_key` + `/secret_key` | loopzero-api + tenant-operator | 1h |
| `api/secrets/service_auth/secret` (+ _previous) | loopzero-api | 5m |
| `api/secrets/admin/api_key` (+ _previous) | loopzero-api | 5m |
| `api/secrets/argo/callback_secret` | loopzero-api | 24h |
| `api/secrets/bootstrap_secret` | loopzero-api | 24h |
| `api/secrets/secret_encryption_key` | loopzero-api | 24h |
| `api/secrets/github/app_private_key` | loopzero-api | 1h |
| `api/secrets/github/webhook_secret` | loopzero-api | 1h |
| `api/secrets/cf/api_token` | loopzero-api | 1h |
| `api/secrets/stripe/secret_key` | loopzero-api | 6h |
| `cloud/secrets/service_auth/ed25519_private_key` | loopzero-cloud | 24h |
| `cloud/secrets/stripe/api_key` (+ webhook_secret) | loopzero-cloud | 6h |
| `operators/secrets/digitalocean/token` | tenant-operator | 1h |
| `operators/secrets/spaces/access_key_id` (+ secret_access_key) | tenant-operator | 1h |
| `operators/secrets/harbor/admin_password` | tenant-operator | 1h |

## P3 — Rollback (Vault → env var fallback)

If Vault becomes unavailable OR ESO sync fails persistently:

```bash
# Option A — Disable ESO + switch back to env var per Helm Deployment
helmfile -f infra/helmfile/core.helmfile.yaml.gotmpl -e <env> \
  --state-values-set vaultEnabled=false sync

# This stops ESO from updating Secrets; existing Secret values persist
# with last-known-good (creationPolicy: Orphan — Secrets have NO
# ownerReference to ExternalSecret CR, so K8s GC does NOT cascade-delete
# when chart uninstalled).
#
# ⚠️ LIVE-validated 2026-06-03 iter 20: creationPolicy=Owner (previous
# default) caused cascade-delete despite deletionPolicy=Retain because K8s
# garbage collector runs BEFORE ESO reconcile. Audit:
# `.claude/knowledge-base/audits/2026-06-03-vault-INT-LIVE-evidence.md`.
# Chart fixed in same iter — verify `kubectl get externalsecret -o yaml`
# shows `creationPolicy: Orphan` before relying on Retain semantics.

# Option B — Operator emergency env var override
# In Helm Deployment, add explicit env (overrides secretKeyRef if both set):
# infra/helm/loopzero-api/values.yaml or env vars file
GITEA_TOKEN=<emergency-token>
# Re-deploy: helmfile sync loopzero-api
```

## P4 — Backup (D12 openbao-snapshot-agent)

**Canonical (recommended):** use `enable-vault-backup.sh` (Runbook P4 codification).

```bash
# Step 1 (manual) — enable in env values (vault-local default: false):
# Edit infra/helmfile/environments/<env>.yaml:
#   snapshotAgentEnabled: true

# Step 2-4 (codified) — Secret + helmfile sync + verify:
bash infra/scripts/openbao/enable-vault-backup.sh --env <env>
# Reads ~/.openbao-<env>-resources.env (provisioned via
# provision-aws-resources.sh) → creates Secret idempotent → helmfile sync
# openbao-snapshot-agent → verifies CronJob registered. Dry-run prints
# ONLY char counts (security invariant: never raw values).

# Inspect logs after first run:
kubectl --context k3d-loopzero-secrets -n vault-system logs \
  -l app=openbao-snapshot-agent --tail=50
```

**Manual procedure** (legacy — use for debugging or unsupported envs):

```bash
# Enable snapshot-agent in env values (vault-local default: false)
# Edit infra/helmfile/environments/<env>.yaml:
#   snapshotAgentEnabled: true

# Provision DO Spaces creds Secret (operator-time, from
# ~/.openbao-<env>-resources.env per SCRIPT.5)
kubectl --context k3d-loopzero-secrets -n vault-system create secret generic \
  openbao-snapshot-s3-creds \
  --from-literal=AWS_ACCESS_KEY_ID=$OPENBAO_BACKUP_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$OPENBAO_BACKUP_SECRET_ACCESS_KEY

# Sync
helmfile -f infra/helmfile/secrets.helmfile.yaml.gotmpl -e <env> \
  --selector name=openbao-snapshot-agent sync

# Verify
kubectl --context k3d-loopzero-secrets -n vault-system get cronjob openbao-snapshot
# Wait for first run
kubectl --context k3d-loopzero-secrets -n vault-system get pods | grep openbao-snapshot
# Inspect logs
kubectl --context k3d-loopzero-secrets -n vault-system logs <pod-name>
```

## P5 — Disaster recovery (restore from snapshot)

**Canonical (recommended):** use `restore-vault-snapshot.sh`. CRITICAL
safety pattern: **dry-run is DEFAULT**; `--execute` MUST be explicitly passed
for the destructive `bao operator raft snapshot restore`.

```bash
# Step 1 — List available snapshots:
export AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=...
bash infra/scripts/openbao/restore-vault-snapshot.sh --env vault-local --list

# Step 2 — Dry-run inspection (default — no destructive op):
bash infra/scripts/openbao/restore-vault-snapshot.sh \
  --env vault-local \
  --snapshot-key 2026-06-01T00-00-00Z.snap
# Prints planned actions for 4 steps; ZERO kubectl cp/exec calls.

# Step 3 — Actually restore (explicit --execute required):
bash infra/scripts/openbao/restore-vault-snapshot.sh \
  --env vault-local \
  --snapshot-key 2026-06-01T00-00-00Z.snap \
  --execute
# 3-second sleep window before destructive exec (Ctrl-C to abort).
```

**POST-RESTORE (MANDATORY operator-time):**
1. Vault will likely be RE-SEALED. Verify: `bao status`.
2. Re-run Shamir unseal ceremony per [`docs/operations/secrets-cluster-local-runbook.md`](./secrets-cluster-local-runbook.md) P3.
3. Verify KV data integrity: `bash infra/scripts/openbao/vault-sync-check.sh --threshold 10`.

**Manual procedure** (legacy — use for debugging or non-standard envs):

```bash
# 1. Identify snapshot
aws --endpoint-url https://sfo3.digitaloceanspaces.com s3 ls \
  s3://loopzero-openbao-backups-<env>/ | sort -r | head -5

# 2. Download
aws --endpoint-url https://sfo3.digitaloceanspaces.com s3 cp \
  s3://loopzero-openbao-backups-<env>/<latest>.snap /tmp/

# 3. Restore (per secrets-cluster-local-runbook P6)
kubectl cp /tmp/<latest>.snap openbao-0:/tmp/snapshot.snap
kubectl exec openbao-0 -- bao operator raft snapshot restore /tmp/snapshot.snap

# Vault now has data state of <latest> snapshot. ESO will re-sync
# K8s Secrets within refreshInterval.
```

## P6 — Break-glass (Vault root token)

If Vault sealed + need urgent secret access:

```bash
# 1. Unseal Vault (3 of 5 Shamir shards — distributed custody)
kubectl exec openbao-0 -- bao operator unseal <shard1>
kubectl exec openbao-0 -- bao operator unseal <shard2>
kubectl exec openbao-0 -- bao operator unseal <shard3>

# 2. Use root token directly (logged for audit)
ROOT_TOKEN=$(jq -r .root_token /tmp/openbao-init.json)
kubectl exec openbao-0 -- bao login token=$ROOT_TOKEN

# 3. Read needed secret manually
kubectl exec openbao-0 -- bao kv get secret/loopzero/<env>/api/secrets/gitea/token

# 4. POST-INCIDENT: rotate root token + redistribute shards
kubectl exec openbao-0 -- bao token revoke -self
# Re-run Shamir init ceremony per docs/operations/openbao-init-runbook.md
```

## Inventory (canonical)

ALL secret targets + paths defined em
[`infra/helm/loopzero-external-secrets/values.yaml`](../../infra/helm/loopzero-external-secrets/values.yaml).
Operator updates inventory → helmfile sync → all ExternalSecret CRs reconcile.

**13 ExternalSecret targets** covering **26 unique secrets** distributed across:
- loopzero-api binary: 10 targets, 16 secrets
- loopzero-cloud binary: 2 targets, 3 secrets
- tenant-operator binary: 1 target (consolidated), 7 keys (4 specific + 3 shared with loopzero-api via D6 vaultBinaryOverride)

## SDK follow-ups (out-of-scope this plan — flagged v1.8)

These improvements identified during v1.8 SDK sweep, deferred to separate plans:

| SDK | Domain | Recommendation |
|-----|--------|----------------|
| `stripe-go` v76+ | cloud/billing | Replace `os.Getenv("STRIPE_PRICE_*")` + own HTTP with stripe-go SDK. Cleaner billing webhook handling + native price ID validation. |
| `godo` v1.x | tenant-operator | Replace direct DigitalOcean API HTTP with godo SDK. Better error handling, retries, rate limiting. |
| `trust-manager` (cert-manager subproject) | cert distribution | Distribute openbao-tls CA via Bundle CR instead of manual ConfigMap copy on each cluster. Auto-update on cert rotation. |

Each merits its own focused plan with TDD + cross-validation when prioritized.

## References

- `decisions/2026-06-02-openbao-adoption.md` (parent ADR Q1-Q16)
- `decisions/2026-06-02-vault-secrets-sot.md` (D1-D12)
- `plans/2026-06-02-loopzero-api-vault-integration-plan.md` v1.8
- [`docs/operations/secrets-cluster-local-runbook.md`](./secrets-cluster-local-runbook.md) (Vault cluster setup + Shamir)
- [`docs/operations/openbao-aws-resources-runbook.md`](./openbao-aws-resources-runbook.md) (AWS KMS + DO Spaces)
- [`docs/operations/openbao-init-runbook.md`](./openbao-init-runbook.md) (Shamir init ceremony)
- [`docs/operations/break-glass-runbook.md`](./break-glass-runbook.md) (recovery without quorum)
