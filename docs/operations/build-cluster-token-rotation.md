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

> **Which ServiceAccount:** the kubeconfig `loopzero-api` uses to **submit and
> watch** build Workflows is `loopzero-workflow-submitter` (bound to a Role with
> `workflows: [create, get, list, watch, delete]`). Do **NOT** mint
> `loopzero-build-sa` here — that SA is the workflow *executor* (bound only to
> `workflowtaskresults: [create, patch]`), so its token authenticates (no 401)
> but is **Forbidden (403)** to list/create Workflows. Minting the wrong SA
> turns a 401 into a silent 403 that looks like a fresh failure.

```bash
# 1. Mint a fresh 30d token in the BUILD cluster (not mgmt) for the SUBMITTER SA.
BUILD_TOKEN=$(kubectl --context k3d-loopzero-build -n argo create token loopzero-workflow-submitter --duration=720h)

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

# 3. Re-create the mgmt Secret with the new build.kubeconfig and EVERY other
#    kubeconfig key preserved. Preserve ALL non-build keys dynamically — the
#    Secret holds one key per cell (mgmt, runtime, runtime2, ...). Hard-coding
#    only mgmt+runtime here DROPS runtime2 and breaks cell-2 routing.
ARGS=(--from-file=build.kubeconfig=/tmp/build.kubeconfig)
OTHER_KEYS=$(kubectl --context k3d-loopzero-mgmt -n loopzero-system get secret loopzero-cluster-kubeconfigs \
  -o go-template='{{range $k,$v := .data}}{{$k}}{{"\n"}}{{end}}' | grep -v '^build\.kubeconfig$')
for KEY in $OTHER_KEYS; do
  kubectl --context k3d-loopzero-mgmt -n loopzero-system get secret loopzero-cluster-kubeconfigs \
    -o jsonpath="{.data.${KEY//./\\.}}" | base64 -d > "/tmp/$KEY"
  [ -s "/tmp/$KEY" ] || { echo "FATAL: $KEY empty — refusing to drop a cluster"; exit 1; }
  ARGS+=(--from-file="$KEY=/tmp/$KEY")
done

kubectl --context k3d-loopzero-mgmt -n loopzero-system create secret generic loopzero-cluster-kubeconfigs \
  "${ARGS[@]}" --dry-run=client -o yaml | kubectl --context k3d-loopzero-mgmt apply -f -

shred -u /tmp/*.kubeconfig 2>/dev/null || rm -f /tmp/*.kubeconfig

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
token life MUST exceed the rotation interval — otherwise the token dies
between runs. With a 30d (`720h`) token rotated every 7d the margin is
~23 days. **Never** mint a token whose TTL is *shorter* than the rotation
period (a 24h token rotated weekly is dead for 6 of every 7 days — this
is the exact bug that silently broke the deploy timeline).

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
