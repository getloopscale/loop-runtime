---
type: runbook
title: "Runbook: LoopZeroCosignSignErrorRate"
---

# Runbook: LoopZeroCosignSignErrorRate

Alert from `prometheusrule-sigstore.yaml`. Fires when cosign sign
error rate exceeds **1%** for 10 minutes.

## What this means

More than 1 in 100 sign attempts are failing — builds are dropping
through to FAILED at the sign step. Every failed sign means an image
that cannot deploy: a Sigstore brownout that an operator cannot
ignore.

## First triage (5 minutes)

1. **Group by failing component** to root-cause:
   ```
   sum(rate(loopzero_cosign_sign_errors_total[5m])) by (component)
   ```
   Possible labels: `fulcio`, `rekor`, `ctlog`, `other`.

2. **Check the deploy denominator** (are sign attempts low + a few
   errors look like high rate?):
   ```
   sum(rate(loopzero_cosign_sign_total[5m]))
   ```
   If sign_total < 0.01/s (≈ < 1 sign per 2 minutes), the
   `clamp_min(..., 0.01)` in the alert expression keeps the rate
   real-valued — but the absolute error count may still be small.
   The alert assumes a steady stream of builds; near-zero traffic
   skews interpretation.

3. **Inspect Sigstore component pods**:
   ```
   kubectl get pods -n fulcio-system -o wide
   kubectl get pods -n rekor-system -o wide
   kubectl get pods -n ctlog-system -o wide
   ```
   Any non-Running pods? CrashLoop? OOMKill?

## Common causes

| Cause | Signal | Action |
|---|---|---|
| Fulcio down / CrashLoop | `loopzero_cosign_sign_errors_total{component="fulcio"}` is the spike | Check Fulcio pod logs; restart deployment; verify OIDC issuer trust chain via `fulcio-oidc-diagnose.sh` |
| Rekor down | `{component="rekor"}` is the spike | Check Rekor pod logs; verify Trillian backend reachable |
| CTLog issue | `{component="ctlog"}` is the spike | Check CTLog pod logs; verify Fulcio→CTLog network path |
| NetworkPolicy regression | All three components spike at once | Recent T3.1 chart redeploy? Check that pods have the `workflows.argoproj.io/workflow` label |
| Sigstore key rotation | Started suddenly post-cron | Check `cosign-cutover-deadline.sh` output; verify the active key matches the policy-controller `ClusterImagePolicy` |

## Escalation

If error rate stays > 1% for 30 minutes with a single component
attribution, page Sigstore SME. If multi-component, page platform
on-call: this looks like infrastructure (network, control plane).

## Rollback option

If the platform recently rolled the cosign keyless path forward
(T0.2 deadline window) and errors cluster around `fulcio`, consider
the **emergency keyless→keyed rollback** documented in
`docs/operations/sigstore-rollback.md` (T4.5 runbook).

## Related

- T4.2 plan section
- `docs/operations/sigstore-rollback.md` (T4.5)
- ADR `2026-05-28-cosign-v2.4.3-deadline.md`
