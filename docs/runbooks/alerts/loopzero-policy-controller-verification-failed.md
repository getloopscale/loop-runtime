# Runbook: LoopZeroPolicyControllerVerification{FailuresPresent,FailureRateHigh}

**Component:** sigstore policy-controller (Sprint 6 / T2.3)
**Severity:** info (any failures, audit mode), warning (>5% rate / 10m)
**Impact:**
- **Audit mode (D4 default):** failures are logged but admissions succeed.
- **Deny mode (post-audit):** failures BLOCK pod admission.

## What this means

The policy-controller attempted to verify the cosign signature on a tenant
image and the verification failed. Possible causes:

1. The image was not signed (legacy build pre-Sprint 6 or signing step failure).
2. The cosign public key on the runtime cluster has rotated/drifted from
   the private key on the build cluster.
3. The image manifest was modified after signing (registry tampering).
4. The signature manifest was deleted from the registry.

## Diagnosis

1. **Identify the offending images.** The audit log records the image
   reference and the failure reason:

   ```bash
   kubectl -n cosign-system logs -l app.kubernetes.io/name=policy-controller \
     --tail=200 | grep -E "audit|fail|reject"
   ```

2. **Verify the build pipeline produced a signature.** For a specific build:

   ```bash
   loopzero deploy attestation <build-id>           # via CLI
   curl -s ${LOOPZERO_API}/api/v1/builds/<id>/attestation | jq .signed
   ```

3. **Check key alignment.** The public key on the runtime cluster MUST be
   the cosign public-key counterpart of the build cluster's private key:

   ```bash
   # On runtime cluster:
   kubectl -n cosign-system get secret cosign-public-key -o jsonpath='{.data.cosign\.pub}' \
     | base64 -d | sha256sum

   # On build cluster (local copy of the private key):
   cosign public-key --key cosign.key | sha256sum
   ```

   The two SHA256s must match.

4. **Manual verification reproduces the failure.**

   ```bash
   cosign verify --key cosign.pub registry.digitalocean.com/loopzero-infra-registry/<image>@sha256:<digest>
   ```

## Mitigation

- **Legacy unsigned image (pre-Sprint 6):** redeploy via `loopzero deploy` —
  the new build will be signed. Audit mode admits the legacy image until
  redeploy.

- **Key drift:** rotate the cosign keypair (build + runtime) atomically:

  ```bash
  # 1. Generate new keypair in a controlled environment.
  cosign generate-key-pair
  # 2. Update Pulumi config:
  pulumi config set --secret cosignPrivateKeyB64 "$(base64 -w0 cosign.key)"
  pulumi config set cosignPublicKeyB64 "$(cosign public-key --key cosign.key | base64 -w0)"
  # 3. Apply to BOTH stacks:
  pulumi up --stack build
  pulumi up --stack runtime
  # 4. Re-sign in-flight images via re-deploy.
  ```

- **Tampered manifest:** an image with mismatched digest indicates a
  registry compromise. STOP — do not redeploy. Investigate the registry
  audit log and rotate registry credentials.

## When to flip Audit → Deny (D4)

The plan's D4 calls for two weeks of clean Audit logs before flipping
the policy to Deny. Verify with:

```promql
sum(increase(policy_controller_image_signature_verifications_total{result="failure"}[14d])) == 0
```

Then update `infra/pulumi/src/components/runtime-policy-controller.ts`:

- `failurePolicy: "Ignore"` → `"Fail"`
- `validationActions: ["Audit"]` → remove (default is Deny)

Run `pulumi up --stack runtime`.

## Validation after fix

- Verification failure rate drops to 0 over 1h window.
- No new `LoopZeroPolicyControllerVerificationFailureRateHigh` alerts.
- Sample tenant deploy succeeds end-to-end.

## References

- Plan: `docs/plans/sprint6-security-completion-plan.md` §T2.3, §D4
- Code: `infra/pulumi/src/components/runtime-policy-controller.ts`
