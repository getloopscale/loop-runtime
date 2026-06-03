# OpenBao Multi-Cluster Kubernetes Auth Runbook

> **T5.1-T5.4 of plan `2026-06-02-openbao-adoption v1.5`**
>
> Operator-time procedure for writing per-mount `auth/<X>/config` to Vault
> so each K8s cluster's SA tokens can authenticate against the right mount.
> The mounts themselves + per-binary roles are provisioned engineering-side
> via the `openbao-init` Helm chart (iter 32 LIVE evidence). This runbook
> covers the LIVE part requiring cluster credentials.

## When to use

Run this runbook ONCE per environment (`vault-local`, `dev`, `prod`) after:

1. Vault stack bootstrapped + unsealed
2. `openbao-init` Job completed successfully (provisions 6 auth mounts + 9 per-binary roles + 4 secrets engines)
3. Operator has credentials to query each target cluster's K8s API

Re-run for any of:
- New cluster added (e.g., `cell-3`)
- Vault re-initialized (lost prior config)
- K8s cluster CA rotated (need to update `kubernetes_ca_cert`)
- TokenReviewer SA recreated (need new JWT)

## Prerequisites

- `bao` CLI installed locally OR exec into `openbao-0` pod
- `kubectl` access to mgmt + build + cell-default + cell-1 + cell-2 (5 clusters in prod; LOCAL = single k3d cluster acting as all 5)
- `BAO_ADDR` + `BAO_TOKEN` (root token for first-run; restricted token thereafter)
- `jq` for parsing

## Auth mount inventory

Provisioned by `openbao-init`:

| Mount | Binds | Used by |
|---|---|---|
| `auth/kubernetes` | Legacy/backward compat | spki-extractor, bao-snapshot, tenant-controller-transit |
| `auth/k8s-mgmt` | mgmt cluster SAs | loopzero-api, loopzero-{tenant,app,preview,platform}-operator |
| `auth/k8s-build` | build cluster SAs | argo-workflows |
| `auth/k8s-cell-default` | cell-default workload SAs | external-secrets-broker |
| `auth/k8s-cell-1` | cell-1 workload SAs | external-secrets-broker |
| `auth/k8s-cell-2` | cell-2 workload SAs | external-secrets-broker |

Each mount needs `auth/<X>/config` with:
- `kubernetes_host` — that cluster's K8s API server URL (HTTPS)
- `kubernetes_ca_cert` — that cluster's CA bundle
- `token_reviewer_jwt` — a long-lived SA JWT with `system:auth-delegator` ClusterRole

Without `auth/<X>/config`, `bao login` calls to that mount return 400 — workloads cannot authenticate.

## Procedure (per cluster)

The pattern repeats 5× (or 6 if you also configure the legacy `auth/kubernetes` for backward compat).

### Step 1 — Identify the target cluster's K8s API server

```bash
TARGET_CONTEXT=k3d-loopzero-mgmt   # or k3d-loopzero-build, ...

K8S_HOST=$(kubectl --context "${TARGET_CONTEXT}" config view --minify \
  -o jsonpath='{.clusters[0].cluster.server}')
echo "kubernetes_host: ${K8S_HOST}"
```

In k3d local: `https://0.0.0.0:6443` (or similar). **PROD**: use the actual DOKS LB hostname (`https://<cluster-id>.k8s.ondigitalocean.com`).

### Step 2 — Fetch the target cluster's CA bundle

```bash
K8S_CA=$(kubectl --context "${TARGET_CONTEXT}" config view --raw --minify \
  -o jsonpath='{.clusters[0].cluster.certificate-authority-data}' | base64 -d)
echo "${K8S_CA}" | head -5
```

If `certificate-authority-data` is empty (DOKS sometimes uses `insecure-skip-tls-verify`), fetch from the cluster's `kube-root-ca.crt` ConfigMap:

```bash
K8S_CA=$(kubectl --context "${TARGET_CONTEXT}" -n default get cm kube-root-ca.crt \
  -o jsonpath='{.data.ca\.crt}')
```

### Step 3 — Create a TokenReviewer ServiceAccount on the target cluster

The SA's JWT will be used by Vault to call the cluster's TokenReview API. It needs the `system:auth-delegator` ClusterRole.

```bash
cat <<EOF | kubectl --context "${TARGET_CONTEXT}" apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: vault-tokenreviewer
  namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: vault-tokenreviewer-auth-delegator
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:auth-delegator
subjects:
  - kind: ServiceAccount
    name: vault-tokenreviewer
    namespace: kube-system
EOF
```

### Step 4 — Mint a long-lived JWT for the TokenReviewer SA

```bash
TOKEN_REVIEWER_JWT=$(kubectl --context "${TARGET_CONTEXT}" -n kube-system \
  create token vault-tokenreviewer --duration=8760h)
echo "TokenReviewer JWT minted (${#TOKEN_REVIEWER_JWT} chars)"
```

**Operator note:** 8760h = 1 year. Schedule a calendar reminder to rotate before expiry. After T7.x dynamic SA tokens are operator-time-enabled, this static JWT becomes the only long-lived credential remaining — keep it secret.

### Step 5 — Write the mount config to Vault

```bash
# Determine which Vault auth mount this cluster maps to
case "${TARGET_CONTEXT}" in
  *loopzero-mgmt*)    MOUNT=k8s-mgmt ;;
  *loopzero-build*)   MOUNT=k8s-build ;;
  *cell-default*) MOUNT=k8s-cell-default ;;
  *cell-1*)       MOUNT=k8s-cell-1 ;;
  *cell-2*)       MOUNT=k8s-cell-2 ;;
  *) echo "ERR: cannot map ${TARGET_CONTEXT} to a Vault auth mount"; exit 1 ;;
esac

bao write "auth/${MOUNT}/config" \
  kubernetes_host="${K8S_HOST}" \
  kubernetes_ca_cert="${K8S_CA}" \
  token_reviewer_jwt="${TOKEN_REVIEWER_JWT}" \
  disable_iss_validation=true

echo "auth/${MOUNT}/config written"
```

**`disable_iss_validation=true`** is the default for K8s ≥ 1.21 (issuer claim changed format). Leave true unless you know what you're doing.

### Step 6 — Verify with a test login

Pick one of the per-binary roles that should work for this mount.

```bash
# Example for k8s-mgmt: loopzero-api role expects loopzero-api SA in loopzero-system NS
TEST_NS=loopzero-system
TEST_SA=loopzero-api
TEST_ROLE=loopzero-api

# Create the SA on the cluster (if not present)
kubectl --context "${TARGET_CONTEXT}" -n "${TEST_NS}" \
  create sa "${TEST_SA}" --dry-run=client -o yaml | \
  kubectl --context "${TARGET_CONTEXT}" apply -f -

# Mint a short-lived JWT for the test SA
TEST_JWT=$(kubectl --context "${TARGET_CONTEXT}" -n "${TEST_NS}" \
  create token "${TEST_SA}" --duration=60s)

# Attempt login against Vault
LOGIN_RESP=$(bao write -format=json "auth/${MOUNT}/login" \
  role="${TEST_ROLE}" \
  jwt="${TEST_JWT}")

if echo "${LOGIN_RESP}" | jq -e '.auth.client_token' >/dev/null; then
  echo "PASS: login succeeded — auth/${MOUNT}/config working"
else
  echo "FAIL: login response: ${LOGIN_RESP}"
fi
```

## Common errors

| Symptom | Cause | Fix |
|---|---|---|
| `400 missing client token` | Forgot to set BAO_TOKEN | export BAO_TOKEN |
| `400 kubernetes auth requires a configured config` | auth/<X>/config NOT written for this mount | Re-run Step 5 |
| `400 invalid issuer claim` | `disable_iss_validation` not set; K8s ≥ 1.21 | Add `disable_iss_validation=true` |
| `403 service account name not authorized` | SA in role's bound_service_account_names doesn't match | Inspect role: `bao read auth/<X>/role/<name>` |
| `500 namespace not in allowed_kubernetes_namespaces` | Role restricts NS; SA is in wrong NS | Either move SA OR update role's `bound_service_account_namespaces` |
| `403 jwt validation failed: token signature invalid` | `kubernetes_ca_cert` is wrong cluster's CA | Re-fetch from target cluster's `kube-root-ca.crt` |

## Post-config rotation drill

After all 5 mounts are configured, verify rotation works:

```bash
# Rotate tokenReviewer JWT (kube-system SA)
NEW_JWT=$(kubectl --context k3d-loopzero-mgmt -n kube-system \
  create token vault-tokenreviewer --duration=8760h)

# Update Vault config
bao write auth/k8s-mgmt/config \
  kubernetes_host="$(kubectl --context k3d-loopzero-mgmt config view --minify -o jsonpath='{.clusters[0].cluster.server}')" \
  kubernetes_ca_cert="$(kubectl --context k3d-loopzero-mgmt -n default get cm kube-root-ca.crt -o jsonpath='{.data.ca\.crt}')" \
  token_reviewer_jwt="${NEW_JWT}" \
  disable_iss_validation=true

# Verify login still works (Step 6 procedure)
```

## Audit trail

After running this runbook, capture state for audit:

```bash
bao auth list -detailed | tee ~/openbao-auth-mounts-$(date -u +%Y-%m-%d).txt
for mount in kubernetes k8s-mgmt k8s-build k8s-cell-default k8s-cell-1 k8s-cell-2; do
  echo "=== auth/${mount}/config ==="
  bao read "auth/${mount}/config"
  echo "=== auth/${mount}/role (list) ==="
  bao list "auth/${mount}/role" 2>/dev/null
done
```

Store the audit output in your operator-secure location (NOT Git — contains kubernetes_ca_cert).

## Related

- `infra/helm/openbao-init/values.yaml` — auth mounts + role declarations (engineering-side SoT)
- `infra/helm/openbao-init/templates/configmap.yaml` — write_auth_role helper invoked by init Job
- `infra/tests/check-openbao-init-auth-mounts_test.sh` — hermetic test asserts 19 invariants
- `infra/scripts/openbao/init-ceremony.sh` — Vault Shamir init (pre-requisite)
- Plan: `.claude/knowledge-base/plans/2026-06-02-openbao-adoption-plan.md`
- LIVE evidence iter 32: `.claude/knowledge-base/audits/2026-06-03-openbao-adoption-LIVE-evidence.md`
