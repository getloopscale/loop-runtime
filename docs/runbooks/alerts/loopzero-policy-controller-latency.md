# Runbook: LoopZeroPolicyControllerWebhookLatency{High,Critical}

**Component:** sigstore policy-controller (Sprint 6 / T2.3)
**Severity:** warning (p99 > 500ms / 10m), critical (p99 > 1s / 5m)
**Impact:** Pod admission in tenant namespaces is delayed. New deploys, scale events, and replica restarts wait for webhook responses.

## What this means

The cosign image-signature verification webhook sits on the admission path
for any pod created in a tenant namespace (`loopzero.io/managed-by=environment-controller`).
Every Pod creation fires a synchronous webhook call to the policy-controller.
When the webhook is slow, every tenant deployment is slow.

## Diagnosis

1. **Confirm the alert is real.** Look at the Prometheus query directly:

   ```promql
   histogram_quantile(0.99,
     sum by (le) (rate(policy_controller_webhook_duration_seconds_bucket[5m]))
   )
   ```

2. **Check the policy-controller pods.**

   ```bash
   kubectl -n cosign-system get pods -l app.kubernetes.io/name=policy-controller
   kubectl -n cosign-system top pods
   ```

   Look for: CPU throttling, OOMKills, restart count > 0.

3. **Check the audit log.** A flood of unverified images can saturate the
   verification path:

   ```bash
   kubectl -n cosign-system logs -l app.kubernetes.io/name=policy-controller --tail=200 | grep -E "audit|verif"
   ```

4. **Check the registry latency.** policy-controller fetches signature
   manifests from the registry on every cold-cache verification:

   ```bash
   # From any node:
   curl -sI -o /dev/null -w "%{time_total}s\n" \
     https://registry.digitalocean.com/v2/
   ```

## Mitigation

- **Ephemeral spike (load):** scale the policy-controller deployment.

  ```bash
  kubectl -n cosign-system scale deploy policy-controller-webhook --replicas=3
  ```

- **Sustained registry slowness:** investigate the registry itself
  (DOCR / Harbor) — see runbook `loopzero-registry-latency.md`.

- **policy-controller crash loop:** describe the pod to see the failure
  reason. If due to RBAC or the public-key Secret missing, redeploy with
  `pulumi up --stack <env>` after fixing config.

- **Last-resort (Audit mode only):** disable the policy temporarily
  during an incident:

  ```bash
  kubectl delete clusterimagepolicy loopzero-verify-signatures
  ```

  Re-create with `pulumi up` after the incident.

## Validation after fix

- p99 webhook latency back below 500ms for 5 minutes.
- No PolicyControllerDown alert.
- New tenant deploys complete in normal time (< 5 min).

## References

- Plan: `docs/plans/sprint6-security-completion-plan.md` §T2.3
- Decision: MEET-2026-03-30-022 §Distributed Systems
- Code: `infra/pulumi/src/components/runtime-policy-controller.ts`
