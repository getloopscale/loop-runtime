# Gitea Admin Token Rotation Runbook

**Purpose:** Rotate the `GITEA_TOKEN` stored in Secret `loopzero-platform-tokens` without breaking tenant-controller / loopzero-api.

**Severity:** Operational. Failure manifests as `401 Unauthorized` from Gitea on EnsureRepo, opening the Gitea circuit breaker in tenant-controller, blocking tenant convergence.

## Critical Pitfall — `\n` in Secret value

`kubectl create secret --from-file=KEY=path` embeds the **trailing newline** of the file. When that Secret is consumed via `env.valueFrom.secretKeyRef`, Kubernetes injects the value as-is (with the `\n`). The `\n` then ends up in the HTTP `Authorization: token <hash>\n` header → Gitea rejects with `401`.

Bash command substitution (`$(... | base64 -d)`) strips trailing newlines automatically, which is why a smoke test from a debug pod can succeed while the controller fails — the bug is invisible without comparing pod env to the Secret bytes.

## Correct Rotation Procedure

```bash
KCFG=/home/paulo/.kube/loopzero-loopzero-infra-dev-sfo3-mgmt.yaml
NS=loopzero-system

# 1. Generate new token inside Gitea pod with FULL scopes.
#    write:organization is mandatory for org repo creation.
NEW_TOKEN_FILE=$(mktemp)
chmod 600 "$NEW_TOKEN_FILE"
kubectl --kubeconfig=$KCFG exec -n $NS deploy/gitea -- \
  gitea admin user generate-access-token \
  --username gitea_admin \
  --token-name "platform-$(date +%Y%m%d-%H%M)" \
  --scopes "write:organization,write:repository,write:user,write:admin" \
  | awk '/Access token was successfully created/{print $NF}' \
  > "$NEW_TOKEN_FILE"

# 2. Patch Secret WITHOUT trailing newline.
#    `printf '%s'` (NOT echo) avoids the \n that breaks env injection.
NEW_B64=$(printf '%s' "$(cat $NEW_TOKEN_FILE)" | base64 -w0)
kubectl --kubeconfig=$KCFG patch secret -n $NS loopzero-platform-tokens \
  --type=json \
  -p "[{\"op\":\"replace\",\"path\":\"/data/GITEA_TOKEN\",\"value\":\"$NEW_B64\"}]"

# 3. Verify length is exactly 40 chars (Gitea sha1 token, no \n).
kubectl --kubeconfig=$KCFG get secret -n $NS loopzero-platform-tokens \
  -o jsonpath='{.data.GITEA_TOKEN}' | base64 -d | wc -c
# expected: 40

# 4. Restart consumers so they reload Secret.
kubectl --kubeconfig=$KCFG rollout restart -n $NS deploy/tenant-controller deploy/loopzero-api
kubectl --kubeconfig=$KCFG rollout status -n $NS deploy/tenant-controller --timeout=2m
kubectl --kubeconfig=$KCFG rollout status -n $NS deploy/loopzero-api --timeout=2m

# 5. Smoke test — POST repo via ephemeral curl pod
#    (loopzero-api pod has no curl; gitea-redis pod is locked-down).
TOKEN=$(kubectl --kubeconfig=$KCFG get secret -n $NS loopzero-platform-tokens \
  -o jsonpath='{.data.GITEA_TOKEN}' | base64 -d)
kubectl --kubeconfig=$KCFG run smoke-curl-$$ --rm -i --restart=Never \
  --image=curlimages/curl:8.5.0 -n $NS -- sh -c \
  "curl -sS -i -H 'Authorization: token $TOKEN' -X POST \
   -H 'Content-Type: application/json' \
   -d '{\"name\":\"smoke-rotate-$$\",\"private\":true}' \
   http://gitea-http.loopzero-system.svc.cluster.local:3000/api/v1/orgs/loopzero/repos" \
  | head -3
# expected: HTTP/1.1 201 Created

# 6. Cleanup smoke repo + token file.
kubectl --kubeconfig=$KCFG run del-smoke-$$ --rm -i --restart=Never \
  --image=curlimages/curl:8.5.0 -n $NS -- sh -c \
  "curl -sS -X DELETE -H 'Authorization: token $TOKEN' \
   http://gitea-http.loopzero-system.svc.cluster.local:3000/api/v1/repos/loopzero/smoke-rotate-$$"
shred -u "$NEW_TOKEN_FILE"

# 7. Revoke old leaked tokens (if any) via Basic auth API.
#    Get gitea_admin password from initContainer env or from your secrets vault.
#    LIST tokens, identify old ones by name, DELETE by id.
```

## Verification — Circuit Breaker Closed

```bash
kubectl --kubeconfig=$KCFG logs -n $NS deploy/tenant-controller --since=1m \
  | grep -i 'circuit\|gitea' | tail -10
# expected: no "circuit breaker is open" entries; reconciles succeed.
```

## Failure Modes

| Symptom | Cause | Fix |
|---|---|---|
| `401 Unauthorized` from `gitea: get repo` | Token has trailing `\n` (used `kubectl create secret --from-file`) | Re-patch with `printf '%s' | base64 -w0` |
| `403 Forbidden` from `POST /orgs/loopzero/repos` | Token missing `write:organization` scope | Regenerate with full scopes per step 1 |
| Repos created in user namespace, not org | Token has only `write:repository` (user scope) | Add `write:organization` |
| Smoke 201 but controller still 401 | Controller not restarted after Secret rotation | `kubectl rollout restart deploy/tenant-controller` |

## Related

- `infra/pulumi/src/components/mgmt-tenant-controller.ts` — Secret consumer
- `operators/internal/gitea/` — circuit breaker logic
- ADR-0015 — adopt-or-create policy (pre-bootstrap)
