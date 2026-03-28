# Runbook: ArgoCD Token Regeneration

**Severity:** P0 when deploys fail with `ARGOCD_UNAUTHORIZED`
**Audience:** On-call engineer
**Last tested:** 2026-03-28 (DigitalOcean dev)

---

## Symptoms

- `loopzero deploy` fails with `DEPLOY_FAILED` or `ACTIVATION_FAILED`
- API deep health shows `"argocd": "auth_failed"` (not `"unreachable"`)
- Prometheus metric `loopzero_argocd_auth_failures_total` is incrementing
- API logs show: `argocd auth failure — not counted toward circuit breaker`
- Circuit breaker does NOT open (auth failures bypass it since MEET-013)

## Diagnosis

### Step 1: Verify it's an auth problem (not ArgoCD down)

```bash
# Check deep health
curl -sk https://api.dev.loopzero.example/api/v1/health/deep

# If "argocd": "auth_failed" → token problem (this runbook)
# If "argocd": "unreachable" → ArgoCD is down (different runbook)
```

### Step 2: Verify the token is invalid

```bash
# Get current token from Secret
CTX="do-sfo3-loopzero-infra-dev-sfo3-mgmt"
TOKEN=$(kubectl --context $CTX get secret loopzero-platform-tokens -n platform-system \
  -o jsonpath='{.data.ARGOCD_TOKEN}' | base64 -d)

# Check token format
echo "$TOKEN" | head -c 3
# Expected: "eyJ" (JWT prefix)
# If it's NOT a JWT: the token is an admin password, not an apiKey

# Test token directly
kubectl --context $CTX exec -n platform-system deploy/argocd-server -- \
  sh -c "wget -qO- --header='Authorization: Bearer $TOKEN' http://localhost:8080/api/v1/session/userinfo" 2>&1
# Expected: 200 with user info
# If 401: token is invalid
```

## Resolution

### Option A: Regenerate via argocd CLI (preferred)

```bash
CTX="do-sfo3-loopzero-infra-dev-sfo3-mgmt"

# 1. Get admin password
ADMIN_PASS=$(kubectl --context $CTX get secret argocd-initial-admin-secret \
  -n platform-system -o jsonpath='{.data.password}' | base64 -d)

# 2. Login as admin (inside the pod to avoid TLS issues)
kubectl --context $CTX exec -n platform-system deploy/argocd-server -- \
  sh -c "yes | argocd login localhost:8080 --insecure --plaintext --username admin --password '$ADMIN_PASS'"

# 3. Generate new apiKey token for loopzero-api
NEW_TOKEN=$(kubectl --context $CTX exec -n platform-system deploy/argocd-server -- \
  argocd account generate-token --account loopzero-api --server localhost:8080 --plaintext --insecure)

# 4. Verify it's a JWT
echo "$NEW_TOKEN" | head -c 3
# Must be "eyJ"

# 5. Update the Secret
GITEA_TOKEN=$(kubectl --context $CTX get secret loopzero-platform-tokens \
  -n platform-system -o jsonpath='{.data.GITEA_TOKEN}' | base64 -d)

kubectl --context $CTX create secret generic loopzero-platform-tokens \
  --namespace platform-system \
  --from-literal=ARGOCD_TOKEN="$NEW_TOKEN" \
  --from-literal=GITEA_TOKEN="$GITEA_TOKEN" \
  --dry-run=client -o yaml | kubectl --context $CTX apply -f -

# 6. Restart API to pick up new token
kubectl --context $CTX rollout restart deployment/loopzero-api -n platform-system
kubectl --context $CTX rollout status deployment/loopzero-api -n platform-system --timeout=120s

# 7. Verify
kubectl --context $CTX exec -n platform-system deploy/loopzero-api -c api -- \
  wget -qO- --timeout=5 http://localhost:8080/api/v1/health/deep
# Expected: "argocd": "connected"
```

### Option B: Regenerate via curl (if argocd CLI not available)

```bash
CTX="do-sfo3-loopzero-infra-dev-sfo3-mgmt"
ADMIN_PASS=$(kubectl --context $CTX get secret argocd-initial-admin-secret \
  -n platform-system -o jsonpath='{.data.password}' | base64 -d)

# Login as admin
ADMIN_SESSION=$(kubectl --context $CTX exec -n platform-system deploy/argocd-server -- \
  sh -c "wget -qO- --post-data='{\"username\":\"admin\",\"password\":\"$ADMIN_PASS\"}' \
  http://localhost:8080/api/v1/session" | python3 -c "import json,sys; print(json.load(sys.stdin)['token'])")

# Generate apiKey
NEW_TOKEN=$(kubectl --context $CTX exec -n platform-system deploy/argocd-server -- \
  sh -c "wget -qO- --header='Authorization: Bearer $ADMIN_SESSION' \
  --post-data='{\"name\":\"deploy-$(date +%s)\"}' \
  http://localhost:8080/api/v1/account/loopzero-api/token" | python3 -c "import json,sys; print(json.load(sys.stdin)['token'])")

# Then steps 4-7 from Option A
```

## Prevention

- The ArgoCD apiKey token does NOT expire by default
- Common causes of invalidation:
  - ArgoCD server restart with new `server.secretkey`
  - Manual token revocation
  - ArgoCD Helm chart upgrade that changes the signing key
  - `loopzero-api` account deleted or RBAC changed
- The bootstrap Job (`bootstrap.ts`) generates the token automatically on fresh deploys
- Monitor `loopzero_argocd_auth_failures_total` for early warning

## Verification

After resolution, verify end-to-end:

```bash
# 1. Deep health
curl -sk https://api.dev.loopzero.example/api/v1/health/deep
# Expected: all "connected"

# 2. Test deploy
loopzero deploy --json  # from a test project
# Expected: exit 0

# 3. Check metrics
kubectl exec -n platform-system deploy/loopzero-api -- \
  wget -qO- http://localhost:9090/metrics | grep loopzero_argocd
# Expected: circuit_state 0, auth_failures stable (not increasing)
```

## Escalation

If the `loopzero-api` account doesn't exist in ArgoCD:
```bash
# Check accounts
kubectl --context $CTX get configmap argocd-cm -n platform-system \
  -o jsonpath='{.data.accounts\.loopzero-api}'
# Should be: "apiKey"

# If empty, add it:
kubectl --context $CTX patch configmap argocd-cm -n platform-system \
  --type merge -p '{"data":{"accounts.loopzero-api":"apiKey"}}'

# Add RBAC:
kubectl --context $CTX patch configmap argocd-rbac-cm -n platform-system \
  --type merge -p '{"data":{"policy.csv":"p, loopzero-api, applications, sync, */*, allow\np, loopzero-api, applications, get, */*, allow\n"}}'

# Restart ArgoCD server to reload config
kubectl --context $CTX rollout restart deployment/argocd-server -n platform-system
```
