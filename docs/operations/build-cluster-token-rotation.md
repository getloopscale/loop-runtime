# Operator runbook — Build cluster SA token rotation

## When this fires

Grafana alert `rate(loopzero_argo_watcher_token_expired_total[5m]) > 0` pages on-call.

This means `argo_step_watcher` (in `loopzero-api`) received a 401/403 from
the **build** cluster API server. The most likely cause is that the
ServiceAccount token used by `loopzero-cluster-kubeconfigs.build.kubeconfig`
expired. Without rotation, the dashboard's per-step deploy timeline
will silently stop updating for any build that runs after the alert.

## Symptom check

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system logs -l app=loopzero-api --tail=200 \
  | grep "argo step watcher: build cluster auth failed"
```

You should see entries with `status_code=401` or `403`. Each entry
corresponds to one failed reconnect attempt.

## Rotate the token

```bash
# 1. Mint a fresh 24h token in the BUILD cluster (not mgmt).
BUILD_TOKEN=$(kubectl --context k3d-loopzero-build -n argo create token loopzero-build-sa --duration=24h)

# 2. Rebuild the kubeconfig blob.
BUILD_CLUSTER_SERVER=$(docker inspect k3d-loopzero-build-server-0 \
  -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'):6443

cat > /tmp/build.kubeconfig <<EOF
apiVersion: v1
kind: Config
clusters:
- name: in-cluster
  cluster:
    server: https://$BUILD_CLUSTER_SERVER
    insecure-skip-tls-verify: true
contexts:
- name: in-cluster
  context: {cluster: in-cluster, user: in-cluster}
current-context: in-cluster
users:
- name: in-cluster
  user: {token: $BUILD_TOKEN}
EOF
chmod 600 /tmp/build.kubeconfig

# 3. Re-create the mgmt Secret with the new build.kubeconfig and the
#    existing mgmt/runtime kubeconfigs preserved.
MGMT=$(kubectl --context k3d-loopzero-mgmt -n loopzero-system get secret loopzero-cluster-kubeconfigs -o jsonpath='{.data.mgmt\.kubeconfig}' | base64 -d)
RT=$(kubectl --context k3d-loopzero-mgmt -n loopzero-system get secret loopzero-cluster-kubeconfigs -o jsonpath='{.data.runtime\.kubeconfig}' | base64 -d)
echo "$MGMT" > /tmp/mgmt.kubeconfig
echo "$RT" > /tmp/runtime.kubeconfig

kubectl --context k3d-loopzero-mgmt -n loopzero-system create secret generic loopzero-cluster-kubeconfigs \
  --from-file=build.kubeconfig=/tmp/build.kubeconfig \
  --from-file=mgmt.kubeconfig=/tmp/mgmt.kubeconfig \
  --from-file=runtime.kubeconfig=/tmp/runtime.kubeconfig \
  --dry-run=client -o yaml | kubectl --context k3d-loopzero-mgmt apply -f -

shred -u /tmp/build.kubeconfig /tmp/mgmt.kubeconfig /tmp/runtime.kubeconfig

# 4. Force loopzero-api to reload the kubeconfig.
kubectl --context k3d-loopzero-mgmt -n loopzero-system rollout restart deploy/loopzero-api
```

## Validation

After the rollout completes:

```bash
POD=$(kubectl --context k3d-loopzero-mgmt -n loopzero-system get pods -l app=loopzero-api \
  -o jsonpath='{.items[0].metadata.name}')

kubectl --context k3d-loopzero-mgmt -n loopzero-system logs $POD | \
  grep -E "argo step watcher: (started|terminal cache loaded)"
```

Both lines should appear with no `build cluster auth failed` in the
last 5 minutes.

## Prevention

Add a weekly cron that runs the rotation script above unattended. The
token life is 24h; rotating every 7d gives a 6-day margin.

```bash
# /etc/cron.d/loopzero-build-sa-rotate
0 4 * * 1 root /usr/local/bin/loopzero-build-sa-rotate.sh
```

The wrapper script wraps the rotate-token + recreate-secret + rollout
commands above and writes its own audit log.

## Related

- Plan J-17 T1.1.bis (the watcher itself)
- Memory `reference_github_app_credentials_chain_2026_06_06` (one of
  the LIVE J-09 dogfood debugs that surfaced the original 24h TTL)
- Counter `loopzero_argo_watcher_token_expired_total`
