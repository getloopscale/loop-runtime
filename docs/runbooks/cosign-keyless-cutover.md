# Cosign Keyless Cutover Runbook

## Status

- **Plan:** `docs/plans/infra-sota-hardening-plan.md` T4.4
- **Meeting:** MEET-2026-05-03-063 (MEET-B)
- **ADR:** plan ADR D2 (keyless > key-based)
- **Track-1 (in-scope):** GitHub Actions builds (platform images + Helm charts)
- **Track-2 (DEFERRED to Sprint 9+):** in-cluster Argo Workflow user-app builds — requires private Fulcio or cluster OIDC infrastructure not currently deployed; see `api/internal/argo/cosign.go:93` for code-level confirmation.

## Pre-cutover validation

The cutover MUST NOT proceed unless ALL of the following are true:

### Coverage gate

```bash
bash scripts/cosign-coverage-audit.sh --since=$(date -u -d "7 days ago" +%Y-%m-%d) --threshold=100
```

Output must show:

- 100% of platform images built in the last 7 days have BOTH key-based AND keyless signatures.
- 100% of Helm charts published in the last 7 days have BOTH signatures.
- 0 images/charts in the window with only key-based signature.

### Webhook latency gate

```bash
# Query Prometheus on dev environment
curl -s "${PROM_URL}/api/v1/query?query=histogram_quantile(0.95,policy_controller_verify_duration_seconds_bucket)" \
  | jq -r '.data.result[0].value[1]'
```

Result must be ≤ 2 seconds (per MEET-B SRE condition).

### Admission test (k3d)

```bash
task k3d:up
helmfile sync --selector chart=admission-policies \
  --set clusterImagePolicyKeyless.enabled=true \
  --set clusterImagePolicyKeyless.mode=enforce
# Test: keyless-signed image should pass.
kubectl apply -f infra/tests/admission/keyless-signed-pod.yaml
# Expected: pod created.
# Test: unsigned image should be rejected.
kubectl apply -f infra/tests/admission/unsigned-pod.yaml
# Expected: admission webhook rejects with "no matching signature found"
```

## Cutover sequence

### Phase 1 — Flip policy to enforce (30 minutes window)

```bash
# 1. Update values overlay (production)
yq -i '.clusterImagePolicyKeyless.mode = "enforce"' \
   infra/helm/admission-policies/values.production.yaml

# 2. Apply via ArgoCD sync (or helmfile if not gitops-tracked yet)
argocd app sync admission-policies --prune
argocd app wait admission-policies --health

# 3. Monitor admission-controller logs for 30 minutes
kubectl logs -n cosign-system deployment/policy-controller-webhook --since=30m \
  | grep -E "denied|rejected"
# Expected: only known-unsigned legacy images rejected. Investigate any unexpected rejections.
```

If ANY production workload is unexpectedly rejected, IMMEDIATELY rollback:

```bash
yq -i '.clusterImagePolicyKeyless.mode = "warn"' \
   infra/helm/admission-policies/values.production.yaml
argocd app sync admission-policies --prune
```

### Phase 2 — Remove key-based authority (after 7 days enforce mode GREEN)

```bash
# 1. Set loopzero-images-warn (key-based) to disabled
yq -i '.clusterImagePolicy.enabled = false' \
   infra/helm/admission-policies/values.production.yaml

# 2. Remove cosign --key step from helm-charts.yml
# (manual edit — remove lines 263-275 of helm-charts.yml after this commit)

# 3. Remove cosign --key step from build-images.yml
# (manual edit — same pattern)

# 4. Apply
git commit -am "deploy(supply-chain): remove key-based cosign signing (cutover complete per T4.4)"
git push

# 5. Wait for ArgoCD sync
argocd app wait admission-policies --health
```

### Phase 3 — Revoke COSIGN_KEY secret (after Phase 2 GREEN for 24h)

```bash
# 1. Verify all builds in last 24h are keyless-only
bash scripts/cosign-coverage-audit.sh --since=$(date -u -d "1 day ago" +%Y-%m-%d) --keyless-only

# 2. Delete the secret in GitHub
gh secret delete COSIGN_KEY --repo getloopscale/loopzero
gh secret delete COSIGN_PASSWORD --repo getloopscale/loopzero

# 3. Document timestamp + Rekor coverage
# Append to this runbook:
#   ## Cutover completion
#   Date: <ISO timestamp>
#   Coverage: 100% keyless for X days
#   COSIGN_KEY revoked: YES
```

## Rollback plan

If verification regression detected at ANY phase:

1. **Phase 1 rollback:** flip `clusterImagePolicyKeyless.mode` back to `warn` (above).
2. **Phase 2 rollback:** revert the commit that removed `cosign --key`. Re-add COSIGN_KEY secret usage.
3. **Phase 3 rollback:** restore COSIGN_KEY secret from secrets manager. Re-issue secret if rotated.

For Phase 3 rollback, the COSIGN_KEY secret CANNOT be recovered if deleted — generate a new keypair, sign existing artifacts, update verification policies.

## Cutover completion

When this section is filled in, INF-G16 status moves PARTIAL → MET:

- **Date completed:** _________________
- **Keyless coverage at cutover:** _________________
- **Rekor entries verified:** ____ / total
- **Last key-based signature:** _________________
- **COSIGN_KEY revoked at:** _________________
- **Approver (security):** _________________
- **Approver (SRE):** _________________

## Track-2 deferred work (Sprint 9+)

Per MEET-2026-05-03-063 condition #6: in-cluster Argo Workflow keyless signing
(`api/internal/argo/cosign.go:cosignSignTemplate`) requires either:

1. **Private Fulcio deployment** in the build cluster, with custom CA root accepted by `policy-controller`.
2. **Spire deployment** providing SPIFFE workload identities consumable by Fulcio.
3. **Configuring Fulcio public to accept Kubernetes Service Account tokens** as OIDC issuer (not currently supported by sigstore.dev public Fulcio).

Open new change request when one of the above paths is selected. Do NOT begin Track-2 implementation without that meeting.

## References

- INF-G16 in `docs/technical/infra/sota-gates.md`
- Plan `docs/plans/infra-sota-hardening-plan.md` Phase 4
- Meeting `docs/meetings/records/MEET-2026-05-03-063.md`
- Cosign keyless: https://docs.sigstore.dev/cosign/signing/overview/
- OpenSSF Sigstore: https://openssf.org/blog/2024/02/16/scaling-up-supply-chain-security-implementing-sigstore-for-seamless-container-image-signing/
