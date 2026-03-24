# PlatformBootstrap Recovery Runbook

**Last updated:** 2026-03-23
**Owner:** SRE
**Severity:** Platform-critical (blocks all tenant operations until resolved)
**SRE veto requirement:** This runbook must exist before the PlatformBootstrapController ships (MEET-2026-03-24-001, veto condition #2).

---

## Table of Contents

1. [Overview](#1-overview)
2. [Identify the Failed Phase](#2-identify-the-failed-phase)
3. [Per-Phase Manual Recovery](#3-per-phase-manual-recovery)
4. [Reset and Retry](#4-reset-and-retry)
5. [Common Failure Modes](#5-common-failure-modes)
6. [Escalation](#6-escalation)

---

## 1. Overview

The `PlatformBootstrapController` reconciles a single `PlatformBootstrap` CR (`main`) through a linear phase state machine:

```
Pending --> ConfiguringGitea --> ConfiguringArgoCD --> WaitingForAPI --> BootstrapComplete
   |              |                    |                    |
   +--------------+--------------------+--------------------+--> Failed
```

- **BootstrapComplete** is terminal. The controller will not re-reconcile a completed bootstrap.
- **Failed** requires manual intervention (delete+recreate or force-retry annotation).
- **Conditions** are observability signals, NOT control flow gates. The phase field drives transitions.

### Cluster Contexts

| Cluster | Context |
|---------|---------|
| mgmt | `do-sfo3-loopzero-infra-dev-sfo3-mgmt` |
| build | `do-sfo3-loopzero-infra-dev-sfo3-build` |
| runtime | `do-sfo3-loopzero-infra-dev-sfo3-runtime` |

All commands in this runbook target the **mgmt** cluster unless stated otherwise.

```bash
export KUBECONFIG=~/.kube/config
kubectl config use-context do-sfo3-loopzero-infra-dev-sfo3-mgmt
```

### Key Service DNS Names

| Service | DNS |
|---------|-----|
| Gitea HTTP | `gitea-http.platform-system.svc.cluster.local:3000` |
| ArgoCD Server | `argocd-server.argocd.svc.cluster.local:443` |
| LoopZero API | `loopzero-api.platform-system.svc:8080` |

---

## 2. Identify the Failed Phase

### Step 1: Read the CR status

```bash
kubectl get platformbootstrap main -n platform-system -o yaml
```

### Step 2: Check `status.phase`

| Phase Value | Meaning |
|-------------|---------|
| `Pending` | Controller has not started reconciliation |
| `ConfiguringGitea` | Stuck configuring Gitea (token, org, reachability) |
| `ConfiguringArgoCD` | Stuck configuring ArgoCD (JWT, repo credentials) |
| `WaitingForAPI` | Stuck waiting for API health or DB seed |
| `BootstrapComplete` | Successfully completed (no action needed) |
| `Failed` | Terminal failure, requires manual intervention |

### Step 3: Check `status.message`

The message field contains the specific error. Example:

```yaml
status:
  phase: Failed
  message: "ConfiguringGitea: failed to create admin token: connection refused"
  retryCount: 10
```

### Step 4: Check conditions

```bash
kubectl get platformbootstrap main -n platform-system -o jsonpath='{range .status.conditions[*]}{.type}{"\t"}{.status}{"\t"}{.reason}{"\t"}{.message}{"\n"}{end}'
```

Expected conditions and their meaning:

| Condition | True means | False means |
|-----------|-----------|-------------|
| `GiteaReachable` | Gitea HTTP API responds | Gitea is down or unreachable |
| `GiteaConfigured` | Org + token created | Org or token creation failed |
| `ArgoCDReachable` | ArgoCD API responds | ArgoCD is down or unreachable |
| `ArgoCDConfigured` | JWT created, repo creds registered | JWT or repo creds failed |
| `APIHealthy` | API health endpoint returns 200 | API is down or unhealthy |
| `DatabaseSeeded` | `POST /internal/bootstrap` returned 200 | Seed failed or API not ready |
| `BootstrapComplete` | All phases completed | Bootstrap incomplete |
| `TokenRotationRequired` | Token nearing 90-day expiry | Token is fresh |

### Step 5: Check controller logs

```bash
kubectl logs -n platform-system -l app=platform-bootstrap-controller --tail=100
```

Look for lines containing `ERROR`, `failed`, or the specific phase name.

---

## 3. Per-Phase Manual Recovery

### Phase: ConfiguringGitea

**What the controller does in this phase:**
1. Checks Gitea is reachable via HTTP
2. Creates organizations (`loopzero`, `loopzero-platform`)
3. Creates an admin API token (`platform-bootstrap-token`) with scopes `write:organization`, `write:repository`

#### Step 1: Verify Gitea pod is running

```bash
kubectl get pods -n platform-system -l app=gitea
```

**Expected output:**

```
NAME                     READY   STATUS    RESTARTS   AGE
gitea-0                  1/1     Running   0          10m
```

**If not running:** Check events and logs.

```bash
kubectl describe pod -n platform-system -l app=gitea
kubectl logs -n platform-system -l app=gitea --tail=50
```

#### Step 2: Verify Gitea is reachable via cluster DNS

```bash
kubectl run -n platform-system debug-gitea --rm -it --restart=Never --image=curlimages/curl:8.7.1 -- \
  curl -s -o /dev/null -w "%{http_code}" http://gitea-http.platform-system.svc.cluster.local:3000/api/v1/version
```

**Expected output:** `200`

**If not 200:** Gitea is not ready. Wait for the pod to become healthy, or check service/endpoint objects:

```bash
kubectl get svc gitea-http -n platform-system
kubectl get endpoints gitea-http -n platform-system
```

#### Step 3: Verify admin credentials Secret exists

```bash
kubectl get secret gitea-admin-credentials -n platform-system -o jsonpath='{.data}' | jq -r 'keys[]'
```

**Expected output:** Should contain `username` and `password` keys.

**If missing:** The Gitea Helm chart should create this secret. Check the Helm release:

```bash
helm list -n platform-system | grep gitea
helm get values gitea -n platform-system
```

#### Step 4: Verify organization exists

```bash
GITEA_USER=$(kubectl get secret gitea-admin-credentials -n platform-system -o jsonpath='{.data.username}' | base64 -d)
GITEA_PASS=$(kubectl get secret gitea-admin-credentials -n platform-system -o jsonpath='{.data.password}' | base64 -d)

kubectl run -n platform-system debug-gitea-org --rm -it --restart=Never --image=curlimages/curl:8.7.1 -- \
  curl -s -u "${GITEA_USER}:${GITEA_PASS}" \
  http://gitea-http.platform-system.svc.cluster.local:3000/api/v1/orgs/loopzero
```

**Expected output:** JSON with org details (HTTP 200).

**If 404 (org does not exist), create manually:**

```bash
kubectl run -n platform-system debug-gitea-create-org --rm -it --restart=Never --image=curlimages/curl:8.7.1 -- \
  curl -s -X POST -u "${GITEA_USER}:${GITEA_PASS}" \
  -H "Content-Type: application/json" \
  -d '{"username":"loopzero","visibility":"private"}' \
  http://gitea-http.platform-system.svc.cluster.local:3000/api/v1/orgs
```

Repeat for `loopzero-platform` if needed.

#### Step 5: Manual token creation if needed

List existing tokens:

```bash
kubectl run -n platform-system debug-gitea-tokens --rm -it --restart=Never --image=curlimages/curl:8.7.1 -- \
  curl -s -u "${GITEA_USER}:${GITEA_PASS}" \
  http://gitea-http.platform-system.svc.cluster.local:3000/api/v1/users/${GITEA_USER}/tokens
```

If `platform-bootstrap-token` does not exist, create it:

```bash
kubectl run -n platform-system debug-gitea-create-token --rm -it --restart=Never --image=curlimages/curl:8.7.1 -- \
  curl -s -X POST -u "${GITEA_USER}:${GITEA_PASS}" \
  -H "Content-Type: application/json" \
  -d '{"name":"platform-bootstrap-token","scopes":["write:organization","write:repository"]}' \
  http://gitea-http.platform-system.svc.cluster.local:3000/api/v1/users/${GITEA_USER}/tokens
```

**Save the returned token value.** It is only shown once. If the token already exists but you lost the value, delete it first and recreate:

```bash
# Delete existing token by ID (get ID from the list call above)
kubectl run -n platform-system debug-gitea-delete-token --rm -it --restart=Never --image=curlimages/curl:8.7.1 -- \
  curl -s -X DELETE -u "${GITEA_USER}:${GITEA_PASS}" \
  http://gitea-http.platform-system.svc.cluster.local:3000/api/v1/users/${GITEA_USER}/tokens/<TOKEN_ID>
```

---

### Phase: ConfiguringArgoCD

**What the controller does in this phase:**
1. Checks ArgoCD is reachable
2. Reads the admin password from `argocd-initial-admin-secret`
3. Generates a JWT via ArgoCD API
4. Creates repo credential Secrets for Gitea access

#### Step 1: Verify ArgoCD pods are running

```bash
kubectl get pods -n argocd
```

**Expected output:** All pods (`argocd-server`, `argocd-repo-server`, `argocd-application-controller`, `argocd-redis`) should be `Running` and `Ready`.

**If pods are not ready:**

```bash
kubectl describe pod -n argocd -l app.kubernetes.io/name=argocd-server
kubectl logs -n argocd -l app.kubernetes.io/name=argocd-server --tail=50
```

#### Step 2: Verify admin password Secret exists

```bash
kubectl get secret argocd-initial-admin-secret -n argocd -o jsonpath='{.data.password}' | base64 -d
```

**Expected output:** The admin password string.

**If the secret does not exist:** ArgoCD was installed without generating the initial admin secret, or it was deleted. Recreate it:

```bash
# Generate a new bcrypt password and create the secret
ARGOCD_PASS=$(openssl rand -base64 16)
ARGOCD_PASS_BCRYPT=$(htpasswd -bnBC 10 "" "${ARGOCD_PASS}" | tr -d ':\n' | sed 's/$2y/$2a/')

kubectl -n argocd patch secret argocd-secret -p "{\"stringData\": {\"admin.password\": \"${ARGOCD_PASS_BCRYPT}\", \"admin.passwordMtime\": \"$(date +%FT%T%Z)\"}}"

kubectl -n argocd create secret generic argocd-initial-admin-secret --from-literal=password="${ARGOCD_PASS}" --dry-run=client -o yaml | kubectl apply -f -

echo "New ArgoCD admin password: ${ARGOCD_PASS}"
```

#### Step 3: Manual JWT generation

```bash
ARGOCD_PASS=$(kubectl get secret argocd-initial-admin-secret -n argocd -o jsonpath='{.data.password}' | base64 -d)

kubectl run -n argocd debug-argocd-jwt --rm -it --restart=Never --image=curlimages/curl:8.7.1 -- \
  curl -s -k -X POST \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"admin\",\"password\":\"${ARGOCD_PASS}\"}" \
  https://argocd-server.argocd.svc.cluster.local/api/v1/session
```

**Expected output:** JSON containing `{"token":"<JWT>"}`.

**If connection refused:** ArgoCD server is not ready. Check pods (Step 1).

#### Step 4: Verify repo credentials are registered

```bash
kubectl get secrets -n argocd -l argocd.argoproj.io/secret-type=repo-creds
```

**Expected output:** At least one secret named `gitea-repo-creds` with the label.

**If missing, create manually:**

```bash
# Use the Gitea token from the ConfiguringGitea phase
kubectl create secret generic gitea-repo-creds -n argocd \
  --from-literal=type=git \
  --from-literal=url=http://gitea-http.platform-system.svc.cluster.local:3000 \
  --from-literal=username=<GITEA_ADMIN_USER> \
  --from-literal=password=<GITEA_TOKEN> \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl label secret gitea-repo-creds -n argocd argocd.argoproj.io/secret-type=repo-creds --overwrite
```

After creating repo credentials, restart the ArgoCD repo-server to pick them up:

```bash
kubectl rollout restart deployment argocd-repo-server -n argocd
kubectl rollout status deployment argocd-repo-server -n argocd --timeout=120s
```

---

### Phase: WaitingForAPI

**What the controller does in this phase:**
1. Checks API health endpoint: `GET /api/v1/health`
2. Calls DB seed endpoint: `POST /internal/bootstrap`
3. Waits for 200 response from both

#### Step 1: Verify API pods are running

```bash
kubectl get pods -n platform-system -l app=loopzero-api
```

**Expected output:** At least one pod `Running` and `Ready`.

**If not running:**

```bash
kubectl describe pod -n platform-system -l app=loopzero-api
kubectl logs -n platform-system -l app=loopzero-api --tail=50
```

Common causes: database connection refused, missing config, OOM.

#### Step 2: Check API health endpoint

```bash
kubectl run -n platform-system debug-api-health --rm -it --restart=Never --image=curlimages/curl:8.7.1 -- \
  curl -s -w "\nHTTP_CODE:%{http_code}\n" http://loopzero-api.platform-system.svc:8080/api/v1/health
```

**Expected output:** HTTP 200 with health status JSON.

**If not 200:** The API is unhealthy. Check:

```bash
# Database connectivity
kubectl logs -n platform-system -l app=loopzero-api --tail=50 | grep -i "database\|postgres\|connection"

# Database pod status
kubectl get pods -n platform-system -l app=postgresql
```

#### Step 3: Manual DB seed if endpoint fails

```bash
kubectl run -n platform-system debug-api-seed --rm -it --restart=Never --image=curlimages/curl:8.7.1 -- \
  curl -s -X POST -w "\nHTTP_CODE:%{http_code}\n" \
  http://loopzero-api.platform-system.svc:8080/internal/bootstrap
```

**Expected output:** HTTP 200 (seed completed or already seeded).

**If 500:** Check API logs for the specific error:

```bash
kubectl logs -n platform-system -l app=loopzero-api --tail=100 | grep -i "bootstrap\|seed\|migration"
```

#### Step 4: Verify DB records exist

Connect to the database and check for seeded records:

```bash
PG_POD=$(kubectl get pods -n platform-system -l app=postgresql -o jsonpath='{.items[0].metadata.name}')

kubectl exec -n platform-system ${PG_POD} -- psql -U loopzero -d loopzero -c "SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public';"
```

**Expected output:** Non-zero count of tables indicating migrations ran and seed data exists.

---

## 4. Reset and Retry

### Option A: Force retry via annotation

Add the force-retry annotation to trigger re-reconciliation without deleting the CR:

```bash
kubectl annotate platformbootstrap main -n platform-system \
  platform.loopzero.io/force-retry="true" --overwrite
```

The controller will clear the annotation, reset `retryCount`, and restart from the failed phase.

### Option B: Delete and recreate the CR

Use this when the CR is in a corrupted state or you need a clean start.

**Step 1:** Save the current spec (in case you need to reference it):

```bash
kubectl get platformbootstrap main -n platform-system -o yaml > /tmp/platformbootstrap-backup.yaml
```

**Step 2:** Delete the CR:

```bash
kubectl delete platformbootstrap main -n platform-system
```

**Step 3:** If the CR gets stuck in deletion (finalizer), remove the finalizer:

```bash
kubectl patch platformbootstrap main -n platform-system \
  --type=json -p='[{"op":"remove","path":"/metadata/finalizers"}]'
```

**Step 4:** Reapply the CR:

```bash
kubectl apply -f /tmp/platformbootstrap-backup.yaml
```

Or apply from the canonical source (Helm chart or git repo).

### Option C: Manual completion

If every component is already configured (Gitea orgs exist, ArgoCD creds registered, DB seeded) but the CR is stuck, you can verify everything manually using the steps in Section 3, then delete and recreate the CR. The controller uses check-then-act-idempotently, so it will skip already-completed actions and move to `BootstrapComplete`.

---

## 5. Common Failure Modes

### 5.1 Gitea not ready (slow boot)

**Symptom:** Phase stuck at `ConfiguringGitea`, condition `GiteaReachable` is False, message mentions connection refused or timeout.

**Cause:** Gitea pod takes longer than `timeouts.giteaReadySeconds` (default 120s) to start, often due to database initialization on first boot.

**Fix:**
1. Wait for Gitea pod to become Ready: `kubectl get pods -n platform-system -l app=gitea -w`
2. Once Ready, force retry (Section 4, Option A).

### 5.2 ArgoCD admin secret not found

**Symptom:** Phase stuck at `ConfiguringArgoCD`, message mentions `argocd-initial-admin-secret` not found.

**Cause:** ArgoCD was installed with `configs.secret.argocdServerAdminPassword` set and the initial secret was not created, or it was manually deleted.

**Fix:** Recreate the secret (Section 3, Phase ConfiguringArgoCD, Step 2).

### 5.3 API not deployed yet

**Symptom:** Phase stuck at `WaitingForAPI`, condition `APIHealthy` is False.

**Cause:** The LoopZero API deployment has not been applied yet, or it is waiting for its own dependencies (database, migrations).

**Fix:**
1. Check if the API deployment exists: `kubectl get deployment loopzero-api -n platform-system`
2. If it does not exist, deploy it first. The bootstrap controller cannot complete without the API.
3. If it exists but is not ready, check its logs and events.

### 5.4 Network policy blocking internal traffic

**Symptom:** All HTTP calls from the controller to Gitea/ArgoCD/API time out, but the target pods are healthy and reachable from other pods.

**Cause:** A NetworkPolicy is blocking egress from the controller pod or ingress to the target service.

**Fix:**

```bash
# Check network policies in platform-system
kubectl get networkpolicy -n platform-system

# Check network policies in argocd namespace
kubectl get networkpolicy -n argocd

# Verify the controller pod can reach the service
kubectl run -n platform-system debug-netpol --rm -it --restart=Never --image=curlimages/curl:8.7.1 -- \
  curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 \
  http://gitea-http.platform-system.svc.cluster.local:3000/api/v1/version
```

If blocked, review and update the NetworkPolicy to allow traffic from the controller to the target services.

### 5.5 Database connection refused

**Symptom:** API health check fails, API logs show `connection refused` to PostgreSQL.

**Cause:** PostgreSQL pod is not running, or the connection string is misconfigured.

**Fix:**

```bash
# Check PostgreSQL pod
kubectl get pods -n platform-system -l app=postgresql

# Check PostgreSQL service
kubectl get svc -n platform-system | grep postgres

# Check API database configuration
kubectl get configmap loopzero-api-config -n platform-system -o yaml
kubectl get secret loopzero-api-db-credentials -n platform-system -o jsonpath='{.data}' | jq -r 'keys[]'
```

### 5.6 Token already exists with different scopes (Gitea 422)

**Symptom:** Phase stuck at `ConfiguringGitea`, message mentions 422 Unprocessable Entity.

**Cause:** A token with name `platform-bootstrap-token` already exists but the controller cannot determine if it has the correct scopes. Normally the controller treats 422/409 as success (convergence-based), but if the existing token lacks required scopes, subsequent operations will fail with 403.

**Fix:** Delete the existing token and force retry (Section 3, Phase ConfiguringGitea, Step 5).

### 5.7 Total bootstrap timeout exceeded

**Symptom:** Phase is `Failed`, message mentions `totalBootstrapSeconds` exceeded.

**Cause:** The entire bootstrap took longer than `spec.timeouts.totalBootstrapSeconds` (default 600s).

**Fix:**
1. Identify which phase was last active from `status.message`.
2. Fix the underlying issue using the per-phase recovery steps.
3. Force retry (Section 4, Option A).

---

## 6. Escalation

### When to escalate

- The controller has been retried 3+ times with the same error after manual investigation.
- The failure involves credential corruption (Secrets exist but contain invalid data).
- The failure requires changes to the controller code (a code bug, not an environment issue).
- Database is in a partially-seeded state and you are unsure which records are missing.
- The CR is stuck in a state not documented in this runbook.

### What information to collect before escalating

Collect all of the following and attach to the escalation ticket:

```bash
# 1. Full CR status
kubectl get platformbootstrap main -n platform-system -o yaml > /tmp/escalation-bootstrap-cr.yaml

# 2. Controller logs (last 500 lines)
kubectl logs -n platform-system -l app=platform-bootstrap-controller --tail=500 > /tmp/escalation-controller-logs.txt

# 3. Events in platform-system namespace
kubectl get events -n platform-system --sort-by=.lastTimestamp > /tmp/escalation-events.txt

# 4. Pod status of all dependencies
kubectl get pods -n platform-system -o wide > /tmp/escalation-pods-platform.txt
kubectl get pods -n argocd -o wide > /tmp/escalation-pods-argocd.txt

# 5. Secrets existence check (DO NOT dump secret values)
kubectl get secrets -n platform-system --show-labels > /tmp/escalation-secrets-platform.txt
kubectl get secrets -n argocd -l argocd.argoproj.io/secret-type --show-labels > /tmp/escalation-secrets-argocd.txt

# 6. Cluster context confirmation
kubectl config current-context > /tmp/escalation-context.txt
kubectl cluster-info >> /tmp/escalation-context.txt
```

### Escalation contacts

| Severity | Contact | Response time |
|----------|---------|---------------|
| P1 (platform down, no tenants can operate) | On-call SRE + operator-engineer | 30 min |
| P2 (bootstrap blocked, workaround exists) | operator-engineer | 4 hours |
| P3 (non-blocking, informational) | operator-engineer via ticket | Next business day |
