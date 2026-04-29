# LoopZero Platform — Secret Rotation Runbook

> CHG-2026-04-28-LAYERED-BOOTSTRAP iter 14 — operational runbook for the
> idempotent secrets management pattern.

## Architecture

```
┌────────────────────────────────────────────────────────────┐
│  Source of truth (per-secret type):                         │
│                                                              │
│  Pulumi L1 (cloud canon):                                   │
│   - DB_PASSWORD (via LOOPZERO_DB_PASSWORD env)                  │
│   - CF_TUNNEL_TOKEN (via LOOPZERO_CF_TUNNEL_TOKEN env)          │
│   - SPACES_KEY/SECRET (via LOOPZERO_SPACES_* env)               │
│                                                              │
│  K8s Secret (cluster canon, idempotent):                    │
│   - GITEA admin password + token                             │
│   - HARBOR admin password                                    │
│   - HARBOR-REDIS password                                    │
│   - COSIGN signing key + password                            │
│                                                              │
│  Backup (DR canon):                                          │
│   - ~/.loopzero/backups/secrets-{cluster}-{env}-{ts}.yaml        │
│   - chmod 600, retention 10 most recent                      │
└────────────────────────────────────────────────────────────┘
```

## Rotation Procedures

### 1) Rotate DB password (Pulumi-managed)

```bash
# 1. Update DO Managed PostgreSQL password (via doctl or DO console)
doctl databases user update <cluster-id> loopzero --new-password "<new-pwd>"

# 2. Update Pulumi config
cd infra/pulumi
pulumi config set --secret loopzero-infra:dbPassword "<new-pwd>"
pulumi up --stack dev --yes

# 3. Re-run platform install — generate-secrets.sh will write the new
#    password into loopzero-db-credentials.
LOOPZERO_DB_PASSWORD="<new-pwd>" loopzero platform install -f values.yaml --skip-infra

# 4. Restart consumer pods to pick up new credentials
kubectl rollout restart deploy/loopzero-api deploy/loopzero-controller deploy/tenant-controller -n loopzero-system
kubectl rollout restart deploy/ory-kratos -n loopzero-auth
kubectl rollout restart deploy/ory-hydra -n loopzero-auth
```

### 2) Rotate Gitea admin password

```bash
# 1. Backup current state (auto-backed every helmfile sync, but explicit is safer)
loopzero platform secrets backup -o ~/.loopzero/backups/pre-rotation-$(date +%Y%m%d).yaml

# 2. Generate new password and write to Secret directly
NEW_PWD=$(openssl rand -base64 32 | tr -d '/+=' | head -c 32)
kubectl create secret generic gitea-admin -n loopzero-system \
  --from-literal=password="$NEW_PWD" \
  --from-literal=token="$(kubectl get secret gitea-admin -n loopzero-system -o jsonpath='{.data.token}' | base64 -d)" \
  --dry-run=client -o yaml | kubectl apply -f -

# 3. Update Gitea internal user (via Gitea API)
GITEA_TOKEN=$(kubectl get secret gitea-admin -n loopzero-system -o jsonpath='{.data.token}' | base64 -d)
curl -X PATCH https://git.${DOMAIN}/api/v1/admin/users/loopzero-admin \
  -H "Authorization: token $GITEA_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"password\": \"$NEW_PWD\"}"

# 4. Restart Gitea to pick up changes
kubectl rollout restart deploy/gitea -n loopzero-system
```

### 3) Rotate Gitea token (separate from password)

```bash
# 1. Generate new token via Gitea API (using current admin password)
NEW_TOKEN=$(curl -X POST https://git.${DOMAIN}/api/v1/users/loopzero-admin/tokens \
  -u "REDACTED_SECRET_003" \
  -H "Content-Type: application/json" \
  -d '{"name":"loopzero-platform-rotated","scopes":["all"]}' | jq -r .sha1)

# 2. Update Secret (preserve B4 — no trailing newline)
kubectl create secret generic gitea-admin -n loopzero-system \
  --from-literal=password="$(kubectl get secret gitea-admin -n loopzero-system -o jsonpath='{.data.password}' | base64 -d)" \
  --from-file=token=<(printf '%s' "$NEW_TOKEN") \
  --dry-run=client -o yaml | kubectl apply -f -

# 3. Restart consumers
kubectl rollout restart deploy/loopzero-api deploy/tenant-controller -n loopzero-system

# 4. Revoke old token (after verifying new token works)
curl -X DELETE https://git.${DOMAIN}/api/v1/users/loopzero-admin/tokens/<old-token-id> \
  -u "REDACTED_SECRET_045"
```

### 4) Rotate cosign signing key

⚠️ **WARNING**: rotating cosign keys breaks signature verification for
**previously-signed images**. Plan a deprecation window.

```bash
# 1. Backup current key (kept for verification of older images)
kubectl get secret cosign-signing-key -n argo -o yaml \
  > ~/.loopzero/backups/cosign-key-archive-$(date +%Y%m%d).yaml
chmod 600 ~/.loopzero/backups/cosign-key-archive-*

# 2. Generate new keypair
COSIGN_PASSWORD="$(openssl rand -base64 32 | tr -d '/+=' | head -c 32)"
export COSIGN_PASSWORD
cd /tmp && cosign generate-key-pair --output-key-prefix cosign-rotated

# 3. Apply new Secret
kubectl create secret generic cosign-signing-key -n argo \
  --from-file=cosign.key=cosign-rotated.key \
  --from-file=cosign.pub=cosign-rotated.pub \
  --from-literal=cosign.password="$COSIGN_PASSWORD" \
  --dry-run=client -o yaml | kubectl apply -f -

# 4. Restart Argo Workflows so new builds pick up new key
kubectl rollout restart deploy/argo-workflows-server deploy/argo-workflows-workflow-controller -n argo

# 5. Update ClusterImagePolicy `loopzero-images-warn` to verify against the new
#    public key (or keep both keys during rotation window via static.action: pass)

# 6. Sign new images going forward; old images keep their old signature.
```

### 5) Rotate Harbor admin password

```bash
NEW_PWD=$(openssl rand -base64 32 | tr -d '/+=' | head -c 32)

# 1. Update Secret
kubectl create secret generic harbor-admin -n loopzero-system \
  --from-literal=password="$NEW_PWD" \
  --dry-run=client -o yaml | kubectl apply -f -

# 2. Update Harbor admin user via API
HARBOR_URL="https://harbor.${DOMAIN}"
curl -X PUT "$HARBOR_URL/api/v2.0/users/1/password" \
  -u "REDACTED_SECRET_004" \
  -H "Content-Type: application/json" \
  -d "{\"new_password\":\"$NEW_PWD\"}"

# 3. No restart needed — Harbor reads from Secret on each request
```

### 6) Rotate Cloudflare tunnel token

```bash
# 1. Get new tunnel token from Cloudflare
NEW_TOKEN=$(cloudflared tunnel token <tunnel-id>)

# 2. Update Pulumi config
cd infra/pulumi
pulumi config set --secret loopzero-infra:cfTunnelToken "$NEW_TOKEN"
pulumi up --stack dev --yes

# 3. Update K8s Secret
LOOPZERO_CF_TUNNEL_TOKEN="$NEW_TOKEN" \
  bash infra/helmfile/scripts/generate-runtime-secrets.sh dev runtime

# 4. Restart cloudflared pods
kubectl rollout restart deploy/cloudflared -n loopzero-system
```

## Disaster Recovery

### Scenario: `pulumi destroy` + `pulumi up` (recreated cluster)

After cluster recreation, K8s Secrets are gone. Restore from backup:

```bash
# 1. Find latest backup
ls -lt ~/.loopzero/backups/secrets-mgmt-* | head -1

# 2. Restore (CLI command added in iter 14)
loopzero platform secrets restore -f ~/.loopzero/backups/secrets-mgmt-dev-<ts>.yaml

# 3. Verify restored secrets
kubectl get secret -n loopzero-system loopzero-db-credentials gitea-admin harbor-admin
```

### Scenario: cluster compromised — rotate ALL secrets

```bash
# 1. Backup current state (forensics)
loopzero platform secrets backup -o ~/.loopzero/backups/pre-incident-$(date +%Y%m%d-%H%M).yaml

# 2. Force regenerate by DELETING current Secrets (only do this knowing impact)
for secret in gitea-admin harbor-admin harbor-redis-credentials; do
  kubectl delete secret $secret -n loopzero-system
done

# 3. Re-run install — generate-secrets.sh will create new randoms
LOOPZERO_DB_PASSWORD="<rotated>" \
  loopzero platform install -f values.yaml --skip-infra

# 4. Update consumers' admin users via their respective APIs (Gitea, Harbor, ...)
```

## Verification Procedures

### After any rotation, verify:

```bash
# LoopZero API health (uses DB credentials)
curl -sf https://api.${DOMAIN}/api/v1/health

# Gitea reachable + token works
GITEA_TOKEN=$(kubectl get secret gitea-admin -n loopzero-system -o jsonpath='{.data.token}' | base64 -d)
curl -sf -H "Authorization: token $GITEA_TOKEN" https://git.${DOMAIN}/api/v1/version

# Harbor admin reachable
HARBOR_PWD=$(kubectl get secret harbor-admin -n loopzero-system -o jsonpath='{.data.password}' | base64 -d)
curl -sf -u "admin:$HARBOR_PWD" https://harbor.${DOMAIN}/api/v2.0/health

# Cosign signing key valid (sigstore PEM format)
kubectl get secret cosign-signing-key -n argo -o jsonpath='{.data.cosign\.key}' \
  | base64 -d | head -1
# Expected: "-----BEGIN ENCRYPTED SIGSTORE PRIVATE KEY-----"
```

## Observability

```bash
# List recent backups
loopzero platform secrets list

# Manual backup before risky operation
loopzero platform secrets backup -o ~/.loopzero/backups/pre-upgrade-$(date +%Y%m%d).yaml

# Show retention
ls -lt ~/.loopzero/backups/ | head -15
```

## What's NOT in scope of this runbook

- **Vault-based dynamic credentials**: covered by hypothetical Phase 2 (ESO + Vault + KMS) — not implemented yet.
- **HSM-backed encryption**: requires Vault + DO Vault HSM or cloud KMS.
- **Audit log per secret read**: requires Vault audit engine or equivalent.

## References

- ADR D7 (in plan): "Secrets gerados pelo Helmfile via hooks"
- Iter 14: idempotency fix + auto-backup + CLI restore commands
- `infra/helmfile/scripts/generate-secrets.sh` — `get_or_generate()` pattern
- `cli/src/commands/platform.ts` — `platformSecretsBackup/Restore/List`
- `infra/helmfile/scripts/test_helpers/test-idempotency.sh` — regression protection
