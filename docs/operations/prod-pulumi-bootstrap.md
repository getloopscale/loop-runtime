---
type: operational-runbook
title: Pulumi Vault Provider Bootstrap Runbook
---

# Pulumi Vault Provider Bootstrap Runbook

> **T9.2-T9.5 of plan 2026-06-02-openbao-adoption v1.4**
>
> Migrates existing `secure: v1:...` inline encrypted entries in
> `Pulumi.*.yaml` stack files to Vault KV-v2 paths read via
> `@pulumi/vault.generic.getSecret`.

## Why migrate?

| Inline secrets (`secure: v1:`) | Vault KV reads |
|---|---|
| Encrypted with shared Pulumi passphrase | Encrypted by Vault (per-secret AES + KMS auto-unseal) |
| Rotation requires re-encrypt + commit | Rotation = `bao kv put` (no Pulumi state change) |
| No audit trail per read | Every read appears in Vault audit log |
| Passphrase shared OOB between team | K8s SA auth via projected token |
| Cannot revoke without rotating passphrase | Per-path policy denial |

## Prerequisites

1. **Vault stack active** — `task k3d:bootstrap-secrets-cluster` already ran (or
   prod equivalent). Vault unsealed + reachable from operator workstation.
2. **`@pulumi/vault` installed** — `cd infra/pulumi && npm install` picks up
   the v7.4.0 dep added in `0b876ad1`.
3. **Pulumi passphrase** — required to decrypt the existing `secure: v1:`
   values one last time before migration.

## Migration Procedure

### Step 1 — Inventory inline secrets

```bash
cd infra/pulumi
PULUMI_CONFIG_PASSPHRASE="<your-passphrase>" \
  pulumi config --stack dev --show-secrets > /tmp/pulumi-dev-secrets.txt
```

Repeat per stack (`dev`, `prod`, `staging`).

### Step 2 — Decide canonical Vault paths

Each Pulumi config key maps to a Vault path under
`secret/data/loopzero/<env>/iac/<resource>/<key>`:

| Pulumi config key | Vault path |
|---|---|
| `loopzero:cf-api-token` | `secret/data/loopzero/<env>/iac/cloudflare/api-token` |
| `loopzero:do-token` | `secret/data/loopzero/<env>/iac/digitalocean/token` |
| `loopzero:do-spaces-key` | `secret/data/loopzero/<env>/iac/digitalocean/spaces-access-key` |
| `loopzero:do-spaces-secret` | `secret/data/loopzero/<env>/iac/digitalocean/spaces-secret-key` |
| `loopzero:github-app-private-key` | `secret/data/loopzero/<env>/iac/github/app-private-key` |
| `loopzero:stripe-secret-key` | `secret/data/loopzero/<env>/iac/stripe/secret-key` |

### Step 3 — Write to Vault KV

```bash
for env in dev staging prod; do
  # Source the decoded secrets from Step 1
  # Iterate:
  bao kv put "secret/loopzero/${env}/iac/cloudflare/api-token" \
    value="<the-decoded-value>"
  bao kv put "secret/loopzero/${env}/iac/digitalocean/token" \
    value="<the-decoded-value>"
  # ...
done
```

The `infra/scripts/openbao/migrate-pulumi-secrets-to-vault.sh` orchestrates
this with `--dry-run` and `--env <stack>` flags (read-only by default).

### Step 4 — Configure Pulumi vault provider

```bash
pulumi config set --stack dev vault:address \
  https://openbao.secrets-system.svc.cluster.local:8200
pulumi config set --stack dev vault:token "$(bao print token)"
# Production: use Vault K8s auth instead of static token
# pulumi config set --stack prod vault:authLogin '{"path": "auth/k8s-mgmt/login", "parameters": {"role": "pulumi-ci", "jwt": "..."}}'
```

### Step 5 — Refactor Pulumi TS to read from Vault

Replace direct config.requireSecret calls with `VaultSecretReader.read()`
from `infra/pulumi/src/components/vault-iac.ts`:

```typescript
// BEFORE
const cfToken = config.requireSecret("cf-api-token");

// AFTER
import { VaultSecretReader } from "./components/vault-iac";
const vault = new VaultSecretReader();
const cfToken = vault.read(
  `secret/data/loopzero/${env}/iac/cloudflare/api-token`,
  "value",
);
```

### Step 6 — Verify with `pulumi preview`

```bash
PULUMI_CONFIG_PASSPHRASE="<your-passphrase>" \
  pulumi preview --stack dev
```

A preview that produces an IDENTICAL plan to before-migration confirms the
Vault reads return identical values to the inline `secure: v1:` entries.

### Step 7 — Delete inline entries

After preview matches:

```bash
pulumi config rm --stack dev cf-api-token
pulumi config rm --stack dev do-token
# ...
```

### Step 8 — Verify gate

```bash
/tmp/loopzero-ops quality no-pulumi-inline-secrets
# Expected: PASS (no `secure: v1:` literals remain in Pulumi.*.yaml)
```

## Rollback

If any preview disagrees with pre-migration state:

1. `git checkout infra/pulumi/Pulumi.<stack>.yaml`
2. Re-add the inline entries via `pulumi config set --secret`.
3. Comment the `VaultSecretReader.read()` calls + revert to `config.requireSecret`.
4. Open an incident — root-cause the Vault-vs-inline divergence (most
   common cause: typo in the Vault path or key sub-name).

## CI gate enforcement

After migration, the `no-pulumi-inline-secrets` quality gate is added to
`task quality:gates` (see `taskfiles/quality.yml`). Any future commit that
re-introduces `secure: v1:` entries in `Pulumi.*.yaml` will FAIL CI with a
suggestion pointing back to this runbook.

## Operator-time guidance

- **Stage cutover**: dev → staging → prod. Each stack independently. Do NOT
  cutover prod until dev has soaked for at least 7 days.
- **Audit log spike expected**: post-cutover, every `pulumi preview` /
  `pulumi up` generates Vault audit log entries (visible as
  `vault.audit.log_request_success`). The
  `VaultAuditLogStalled` alert understands this — silence is the anomaly.
- **Rotation drill**: with Vault as SoT, rotation is `bao kv put` + `pulumi up`.
  Verify rotation works end-to-end in dev before relying on prod.

## Related

- `infra/pulumi/src/components/vault-iac.ts` — `VaultSecretReader` impl.
- `cmd/loopzero-ops/internal/quality/nopulumiinlinesecrets` — enforcement gate (T9.6).
- `infra/scripts/openbao/migrate-pulumi-secrets-to-vault.sh` — orchestrator.
- Plan: [`.claude/knowledge-base/plans/2026-06-02-openbao-adoption-plan.md`](../plans/2026-06-02-openbao-adoption-plan.md).
