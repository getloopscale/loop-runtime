---
type: runbook
title: "Runbook: LoopZeroCosignSignSlow"
---

# Runbook: LoopZeroCosignSignSlow

Alert from `prometheusrule-sigstore.yaml` (T4.2 of plan
`2026-05-28-meet-001-engineering-closure`). Fires when cosign sign
step P99 latency exceeds **60 seconds** for 10 minutes.

## What this means

The cosign `sign-image` step is the user-visible bottleneck of every
build. It calls Fulcio (cert issue) then Rekor (transparency log
record). A P99 over 60s means at least 1% of sign attempts are
exceeding the budget that downstream gates assume.

## First triage (5 minutes)

1. **Confirm the metric is real, not stale**:
   ```
   curl http://prometheus.monitoring/api/v1/query?query=histogram_quantile(0.99,rate(loopzero_cosign_sign_duration_seconds_bucket[10m]))
   ```
   If the value swings below 60s during triage, the alert was a
   transient — note it and move on.

2. **Split sign vs Fulcio**: the alert is on the END-TO-END sign step,
   but `loopzero_fulcio_cert_issuance_duration_seconds` isolates the Fulcio
   call. If Fulcio P99 is near sign P99, the bottleneck is cert
   issuance. Otherwise, it's Rekor record latency.

3. **Check the error counter**: a slow sign that's _also_ failing is
   a Sigstore brownout, not a latency drift.
   ```
   sum(rate(loopzero_cosign_sign_errors_total[5m])) by (component)
   ```

## Common causes

| Cause | Signal | Action |
|---|---|---|
| Fulcio cert issue slow | `loopzero_fulcio_cert_issuance_duration_seconds` P99 > 30s | Inspect Fulcio pod logs in `fulcio-system` for OIDC discovery retries; check kube-apiserver TLS handshake latency |
| Rekor record slow | sign P99 minus fulcio P99 > 30s | Inspect Rekor pod logs in `rekor-system`; check Trillian backend (Rekor's storage) for queue depth |
| Network path saturation | both Fulcio + Rekor slow, no errors | Inspect kube-router / Cilium metrics on the build cluster |
| Cosign client retries | `cosign sign` logs show 5xx retries | Check `sigstore-extras` NetworkPolicy (T3.1) — a misconfigured podSelector after a values change would manifest as initial connect failures + retries |

## Escalation

If P99 stays > 60s for 30 minutes with no clear component attribution,
page the platform on-call and treat as **Sigstore P1**. Builds may
still succeed but the operator team needs the SLO breach acknowledged.

## Related

- T4.2 plan section
- T3.1 NetworkPolicy hardening — recent changes to sigstore-extras
- `docs/operations/sigstore-rollback.md` (T4.5) for the emergency
  keyless→keyed reversion procedure
