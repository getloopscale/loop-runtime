# Runbook: LoopZeroPolicyControllerDown

**Component:** sigstore policy-controller (Sprint 6 / T2.3)
**Severity:** critical
**Impact:**
- **Audit mode (D4 default, failurePolicy: Ignore):** admissions still succeed
  (fail-open) but image-signature verification is bypassed silently.
- **Deny mode (failurePolicy: Fail):** admissions in tenant namespaces FAIL
  CLOSED — no new pods can be scheduled.

## What this means

Prometheus has lost connectivity to the policy-controller webhook for at
least 5 minutes. The webhook is the gate that verifies cosign signatures
on tenant images.

In Audit mode this is a blind-spot but not user-visible. In Deny mode it
is a tenant-visible outage.

## Diagnosis

1. **Confirm the alert.**

   ```bash
   kubectl -n cosign-system get pods -l app.kubernetes.io/name=policy-controller
   kubectl -n cosign-system describe deploy policy-controller-webhook
   ```

2. **Look at recent events.**

   ```bash
   kubectl -n cosign-system get events --sort-by=.lastTimestamp | tail -30
   ```

3. **Verify metrics scrape.**

   ```bash
   kubectl -n cosign-system port-forward svc/policy-controller-webhook 8080:8080 &
   curl -s localhost:8080/metrics | head -20
   ```

4. **Check ServiceMonitor.**

   ```bash
   kubectl -n monitoring get servicemonitor -l loopzero.io/component=policy-controller
   ```

## Mitigation

- **Pod down (CrashLoopBackOff):** describe + logs:

  ```bash
  kubectl -n cosign-system logs -l app.kubernetes.io/name=policy-controller \
    --tail=100 --previous
  ```

  Common causes: missing `cosign-public-key` Secret, RBAC, OOMKill.

- **Network partition / scrape failure:** investigate Prometheus
  logs and the ServiceMonitor selector.

- **Audit mode safe-mode:** the webhook is failurePolicy: Ignore, so
  admissions succeed even with the controller down. There is no user
  impact while the controller is restored.

- **Deny mode emergency:** switch the failurePolicy to Ignore as a
  break-glass while restoring the controller (DO NOT skip post-incident
  review):

  ```bash
  kubectl edit clusterimagepolicy loopzero-verify-signatures
  # spec.validationActions: [Audit]   # restore from Deny
  ```

  Re-apply correct policy via `pulumi up --stack runtime` after
  controller is healthy.

- **Last-resort full restart:**

  ```bash
  kubectl -n cosign-system rollout restart deploy/policy-controller-webhook
  ```

## Validation after fix

- `up{namespace="cosign-system",job=~".*policy-controller.*"} == 1` for 5m.
- `LoopZeroPolicyControllerDown` alert clears.
- A test deploy successfully exercises the verification path:

  ```bash
  loopzero deploy --env staging
  # Inspect controller logs:
  kubectl -n cosign-system logs -l app.kubernetes.io/name=policy-controller --tail=20 | grep verif
  ```

## References

- Plan: `docs/plans/sprint6-security-completion-plan.md` §T2.3
- Decision: D4 — Audit mode first
- Code: `infra/pulumi/src/components/runtime-policy-controller.ts`
