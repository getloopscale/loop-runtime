# Cosign Signing Key Rotation Runbook

> **Plan reference:** T3.3 of `docs/plans/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md`
>
> **Closes:** M07 Build Pipeline HIGH gap — enforcement gate (`check-cosign-cutover-deadline.sh`) wired here. Script lives em `infra/scripts/security/check-cosign-cutover-deadline.sh`.

## Purpose

Procedimento for rotating cosign signing key (keyed mode, current state) AND for tracking cutover deadline to keyless mode (per ADR 2026-05-12 hard deadline T0+90d).

## Modes

| Mode | Identity | Trust root | Rotation cadence |
|---|---|---|---|
| **Keyed** (current) | Static cosign keypair stored em ESO Secret | Single public key in ConfigMap | Operator-driven, ≥ 90d |
| **Keyless** (target post-T2.2) | Workload OIDC token → Fulcio short-lived cert | Fulcio root CA + Rekor log | Auto on every build (~10min cert) |

## ConfigMap Schema (per EC-6 of edge-case-review)

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: loopzero-cosign-config
  namespace: loopzero-system
data:
  mode: "keyed"  # OR "keyless"
  cutover_deadline: "2026-08-10T00:00:00Z"  # ADR 2026-05-12 T0+90d
  public_key_active: "<public key PEM>"  # current
  public_key_previous: ""  # previous; non-empty during dual-sign phase
```

Owner: pulumi-platform (Fulcio component T2.2 sets+updates).

## Keyed Rotation Procedure

### Phase 1 — Generate new keypair (offline)

```bash
# On operator workstation (offline, air-gapped if HSM)
cosign generate-key-pair
# Generates: cosign.key (private) + cosign.pub (public)
# Encrypts cosign.key with prompted passphrase
```

### Phase 2 — Store new private key em ESO Secret

```bash
# Encode key
PRIVATE_KEY_BASE64=$(base64 < cosign.key)

# Upload to backing secret store (AWS Secrets Manager, HashiCorp Vault, etc.)
# ESO ExternalSecret picks up automatically

aws secretsmanager update-secret \
  --secret-id loopzero/cosign-signing-key-new \
  --secret-string "{\"private_key\":\"$PRIVATE_KEY_BASE64\",\"passphrase\":\"<prompt>\"}"
```

### Phase 3 — Add new public key to verification ConfigMap (dual-sign)

```bash
# Edit ConfigMap to include BOTH old + new public keys
kubectl edit cm -n loopzero-system loopzero-cosign-config

# Change:
#   public_key_active: <NEW PEM>
#   public_key_previous: <OLD PEM>
```

ClusterImagePolicy now accepts images signed by EITHER key (additive verification).

### Phase 4 — Dual-sign phase (7 days)

Workflow builds sign with BOTH keys during 7-day transition:
- Argo Workflow injects both Secrets
- cosign sign step iterates over both signing keys

```bash
# Verify dual-sign is happening
kubectl --context build logs <workflow-pod> -c cosign-signer | grep "signed with"
# Expected: "signed with key cosign-signing-key (current)" AND "signed with key cosign-signing-key-new (transitional)"
```

### Phase 5 — Cut over to new key only

After 7 days dual-sign + zero verification failures:

```bash
# Update ConfigMap: drop previous
kubectl edit cm -n loopzero-system loopzero-cosign-config

# Change:
#   public_key_active: <NEW PEM>
#   public_key_previous: ""
```

Workflow builds sign only with new key from this point.

### Phase 6 — Destroy old private key

```bash
aws secretsmanager delete-secret \
  --secret-id loopzero/cosign-signing-key \
  --recovery-window-in-days 0  # immediate (no recovery)

# Rename "new" → "current"
aws secretsmanager rename-secret \
  --from loopzero/cosign-signing-key-new \
  --to loopzero/cosign-signing-key
```

Old keypair is destroyed. Operator should also:
- Delete local `cosign.key` after secret store upload confirmed
- Destroy any laptop/HSM copies
- Update audit_log

## Cutover Deadline Enforcement (check-cosign-cutover-deadline.sh)

**Script:** `infra/scripts/security/check-cosign-cutover-deadline.sh`

**Behavior:**
```bash
# Reads ADR-2026-05-12 deadline + current mode from ConfigMap
# Returns:
#   exit 0 if (date < deadline) OR (mode == keyless)
#   exit 1 if (date > deadline) AND (mode == keyed)  ← blocks builds
```

**Emergency override (per EC-7 edge-case-review):**
```bash
bash infra/scripts/security/check-cosign-cutover-deadline.sh \
  --emergency-override="incident-IR-2026-08-10-cosign-rollback"
# Effects:
#   - Emits WARN log
#   - INSERT INTO audit_log (action='cosign_deadline_override', reason=...)
#   - exit 0
# Operator MUST request post-hoc Change Governor review within 24h
```

## Audit Trail

Every rotation phase writes audit_log:
- `action: cosign_key_rotate_start | cosign_key_rotate_dual_sign | cosign_key_rotate_cutover | cosign_key_rotate_destroy`
- `actor: <operator>` (X-Service-Auth)
- `metadata: {phase, old_key_id, new_key_id, mode}`

Query rotation history:
```sql
SELECT timestamp, action, actor, metadata
FROM audit_log
WHERE action LIKE 'cosign_key_%'
ORDER BY timestamp DESC LIMIT 20;
```

## Verification After Rotation

```bash
# 1. New build signs successfully
loopzero deploy --project testapp --env staging
# Wait for build complete, check cosign signature on resulting image

# 2. Verify signature with new key
NEW_PUBLIC_KEY=$(kubectl get cm -n loopzero-system loopzero-cosign-config -o jsonpath='{.data.public_key_active}')
echo "$NEW_PUBLIC_KEY" > /tmp/cosign.pub
cosign verify --key /tmp/cosign.pub <registry>/<image>:<tag>
# Expected: Verification PASS

# 3. policy-controller accepts admission of new image
kubectl run testpod --image=<registry>/<image>:<tag> --restart=Never -n <namespace>
# Expected: pod Running (admission allowed)
```

## Failure Modes

| Sintoma | Causa | Recovery |
|---|---|---|
| New builds fail to sign | New private key not propagated to BuildKit pods | Restart `argo-workflows` namespace; verify ESO sync |
| New builds sign but verification fails | ConfigMap public_key_active not updated | `kubectl edit cm loopzero-cosign-config` |
| Old image admission denied post-cutover | public_key_previous removed too early (some images still signed only with old) | Re-add to ConfigMap; extend dual-sign window |
| Emergency override needed but script blocks | Use `--emergency-override="<reason>"` per EC-7 | Open Change Governor review within 24h |
| Cutover deadline passed AND keyless not ready | Phase 2 (Fulcio install) delayed | Use emergency override; expedite T2.2 |

## Keyless Migration (T2.2 dependency)

Once T2.2 of plan is APPROVED + Fulcio + Rekor provisioned:

1. Update ConfigMap `mode: keyless`
2. Workflow builds switch to `cosign sign --identity-token <SA OIDC>` path
3. ClusterImagePolicy switches matching criteria from key to identity
4. After 30 days zero verification failures, deprecate keyed mode entirely
5. `check-cosign-cutover-deadline.sh` reports mode=keyless → exit 0 always

## Related

- ADR 2026-05-12 (cosign keyless cutover, T0+90d deadline)
- ADR 2026-05-13 (Ed25519 sponsor-direct approval pattern)
- `infra/scripts/security/check-cosign-cutover-deadline.sh` (T3.3 deliverable)
- T2.2 of this plan (Pulumi Fulcio + Rekor component)
- `[[cosign-key-rotation]]`
