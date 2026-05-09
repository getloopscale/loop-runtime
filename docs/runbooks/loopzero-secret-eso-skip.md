# Runbook: LoopZeroSecretESOSyncSkipped

## Alert

`loopzero_secret_externalsecret_total{result="skip"} > 0` for 2m.

## Severity

Warning. Silent data-loss class — `loopzero secret set` returns 200 but the
ExternalSecret is never created on the workload cluster, so the tenant's
app starts without secrets and crashes at runtime.

## Quick check

```bash
# Was the API rebooted recently?
kubectl --context loopzero-mgmt -n loopzero-system describe pod -l app=loopzero-api | grep "RUNTIME_KUBE"

# Mount present?
kubectl --context loopzero-mgmt -n loopzero-system exec deploy/loopzero-api -- ls -la /etc/loopzero/runtime/kubeconfig

# Context resolves?
kubectl --context loopzero-mgmt -n loopzero-system exec deploy/loopzero-api -- \
  cat /etc/loopzero/runtime/kubeconfig | grep "^current-context"
```

Expected: `current-context: in-cluster` (per B34 fix).

## Root cause classes

1. `RUNTIME_KUBECONFIG` env var not mounted (Helm chart drift).
2. Context name mismatch (B34 class — F-11 wrote `runtime`, B34 expects `in-cluster`).
3. `loopzero-cluster-kubeconfigs` Secret missing or invalid in mgmt namespace.

## Recovery

1. Reapply Helm chart with correct `runtimeKubeconfigMount.enabled: true`.
2. Verify Secret exists: `kubectl get secret loopzero-cluster-kubeconfigs -n loopzero-system`.
3. Restart API: `kubectl rollout restart deploy/loopzero-api -n loopzero-system`.
4. Watch metric drop to 0: `kubectl exec deploy/loopzero-api -- wget -qO- http://localhost:9090/metrics | grep loopzero_secret_externalsecret_total`.

## References

- find_obs_006 / find_secret_skip_no_alert (deep-review-remediation Phase 5).
- B34 (CHG-2026-05-08-DOGFOOD-FIXES F-2) — runtime.kubeconfig context drift.
