---
type: operational-runbook
title: Vault Cluster Local Runbook (loopzero-secrets k3d)
---

# Vault Cluster Local Runbook (loopzero-secrets k3d)

**Owner**: Platform Security
**SOT**: this file
**Companion**: [openbao-init-runbook.md](./openbao-init-runbook.md), [openbao-aws-resources-runbook.md](./openbao-aws-resources-runbook.md)
**Plan**: VK scope of sponsor-direct vault cluster isolation T3 (2026-06-02)

## Purpose

Local k3d cluster `loopzero-secrets` for validating vault cluster isolation topology
(per T3 decision 2026-06-02) BEFORE provisioning DOKS `loopzero-secrets-prod`. Same
Helm chart + Helmfile pattern that will run in production — bugs surfaced
locally save 5-7h of debug under prod pressure.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│  Host (laptop/dev machine)                                          │
│                                                                     │
│  Docker network: k3d-loopzero (shared)                                  │
│                                                                     │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐       │
│  │ loopzero-core    │  │ loopzero-build   │  │  loopzero-secrets (NEW)    │       │
│  │ kubeAPI 6443 │  │ kubeAPI 6445 │  │  kubeAPI 6447         │      │
│  │ 1srv+2agt    │  │ 1srv         │  │  1srv (single-node)  │       │
│  └──────────────┘  └──────────────┘  └──────────────────────┘       │
│         ▲                ▲                     ▲                    │
│         └────────────────┴─────────────────────┘                    │
│                          │                                          │
│                k3d-loopzero-registry:5000                               │
└─────────────────────────────────────────────────────────────────────┘
```

## Quick start (PRODLIKE workflow — 2026-06-02)

PRODLIKE refactor replaced devMode with proper prod-posture: Raft persistent
storage, HTTPS via cert-manager, Shamir 5/3 manual unseal. The bootstrap is
now a **3-phase sequence** instead of a single `task k3d:secrets:bootstrap`
(devMode was auto-unsealed; Shamir requires operator-time).

### Phase 1: provision infrastructure (idempotent)

```bash
task k3d:secrets:create     # 43s — k3d cluster
helmfile -f infra/helmfile/secrets.helmfile.yaml.gotmpl -e vault-local \
  --kube-context k3d-loopzero-secrets \
  --selector name=cert-manager sync
helmfile -f infra/helmfile/secrets.helmfile.yaml.gotmpl -e vault-local \
  --kube-context k3d-loopzero-secrets \
  --selector name=cluster-issuer sync
helmfile -f infra/helmfile/secrets.helmfile.yaml.gotmpl -e vault-local \
  --kube-context k3d-loopzero-secrets \
  --selector name=openbao-cert sync   # creates Secret openbao-tls
helmfile -f infra/helmfile/secrets.helmfile.yaml.gotmpl -e vault-local \
  --kube-context k3d-loopzero-secrets \
  --selector name=openbao sync         # pod Running but Sealed=true
```

### Phase 2: Shamir init + unseal (operator-time)

```bash
# Init: capture 5 shards + root token (ONE-TIME; idempotent only via storage state)
kubectl --context k3d-loopzero-secrets -n secrets-system exec openbao-0 -- sh -c \
  'BAO_ADDR=https://127.0.0.1:8200 BAO_SKIP_VERIFY=true bao operator init \
    -key-shares=5 -key-threshold=3 -format=json' > /tmp/openbao-init.json

# Unseal: 3 of 5 shards (quorum)
for i in 0 1 2; do
  SHARD=$(jq -r ".unseal_keys_b64[$i]" /tmp/openbao-init.json)
  kubectl --context k3d-loopzero-secrets -n secrets-system exec openbao-0 -- sh -c \
    "BAO_ADDR=https://127.0.0.1:8200 BAO_SKIP_VERIFY=true bao operator unseal $SHARD"
done

# Verify Sealed=false
kubectl --context k3d-loopzero-secrets -n secrets-system exec openbao-0 -- sh -c \
  'BAO_ADDR=https://127.0.0.1:8200 BAO_SKIP_VERIFY=true bao status' | grep Sealed
```

### Phase 3: provision token Secret + init Job

```bash
ROOT_TOKEN=$(jq -r .root_token /tmp/openbao-init.json)
kubectl --context k3d-loopzero-secrets -n secrets-system create secret generic \
  openbao-init-token --from-literal=BAO_TOKEN="$ROOT_TOKEN"

helmfile -f infra/helmfile/secrets.helmfile.yaml.gotmpl -e vault-local \
  --kube-context k3d-loopzero-secrets \
  --selector name=openbao-init sync
```

### Final validation

```bash
task k3d:secrets:status     # all pods + bao status
kubectl --context k3d-loopzero-secrets -n secrets-system exec openbao-0 -- sh -c \
  "BAO_ADDR=https://127.0.0.1:8200 BAO_SKIP_VERIFY=true BAO_TOKEN=$ROOT_TOKEN \
   bao kv put secret/test foo=bar && bao kv get secret/test"
```

### Cleanup + restart with same state

PVC `data-openbao-0` is persistent — `task k3d:secrets:destroy` deletes the
k3d cluster but the underlying Docker volume survives. After `create` +
helmfile sync, Vault comes back Sealed=true and requires unseal with the
same shards. **Save `/tmp/openbao-init.json` somewhere safe** if you want
to reuse state.

### Full reset (lose all state)

```bash
task k3d:secrets:destroy
docker volume prune -f   # WARNING: removes ALL unused Docker volumes
task k3d:secrets:create
# Re-run Phases 1-3 fresh (new shards, new root token)
```

## VK.5 — Cross-cluster auth (mgmt → vault) workflow

Validates that an SA in **loopzero-core** cluster can authenticate to OpenBao
running in **loopzero-secrets** cluster and read secrets per Vault policy. This
is the core test of T3 vault cluster isolation (LIVE-validated 2026-06-02).

### Pre-requisites

- loopzero-secrets cluster UP + PRODLIKE bootstrap done (Shamir unsealed,
  openbao-init Job Completed)
- loopzero-core cluster UP (`k3d cluster create --config infra/k3d/core-cluster.yaml`)
- `/tmp/openbao-init-output.json` available (Shamir shards + root token)

### Step 1: Provision reviewer SA in mgmt

```bash
kubectl --context k3d-loopzero-core apply -f - <<'EOF'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: loopzero-secrets-reviewer
  namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: loopzero-secrets-reviewer-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:auth-delegator
subjects:
  - kind: ServiceAccount
    name: loopzero-secrets-reviewer
    namespace: kube-system
EOF

REVIEWER_JWT=$(kubectl --context k3d-loopzero-core create token \
  loopzero-secrets-reviewer -n kube-system --duration=8760h)

# Extract mgmt CA cert
kubectl --context k3d-loopzero-core get cm -n kube-system kube-root-ca.crt \
  -o jsonpath='{.data.ca\.crt}' > /tmp/mgmt-ca.crt
```

### Step 2: Configure Vault auth/kubernetes pointing back to mgmt API

```bash
ROOT_TOKEN=$(jq -r .root_token /tmp/openbao-init-output.json)

# Find mgmt API URL reachable from vault pod (Docker bridge IP)
MGMT_API="https://$(docker inspect k3d-loopzero-core-server-0 \
  --format '{{ range .NetworkSettings.Networks }}{{ .IPAddress }}{{ end }}'):6443"

kubectl --context k3d-loopzero-secrets -n secrets-system exec openbao-0 -- sh -c "
export BAO_ADDR=https://127.0.0.1:8200
export BAO_SKIP_VERIFY=true
export BAO_TOKEN=$ROOT_TOKEN

cat > /tmp/mgmt-ca.crt <<'CAEOF'
$(cat /tmp/mgmt-ca.crt)
CAEOF

bao write auth/kubernetes/config \
  kubernetes_host=$MGMT_API \
  token_reviewer_jwt=$REVIEWER_JWT \
  kubernetes_ca_cert=@/tmp/mgmt-ca.crt \
  disable_iss_validation=true
"
```

### Step 3: Create Vault role bound to SA + seed test data

```bash
kubectl --context k3d-loopzero-secrets -n secrets-system exec openbao-0 -- sh -c "
export BAO_ADDR=https://127.0.0.1:8200 BAO_SKIP_VERIFY=true BAO_TOKEN=$ROOT_TOKEN
bao write auth/kubernetes/role/test-reader \
  bound_service_account_names=test-vault-reader \
  bound_service_account_namespaces=default \
  policies=loopzero-api-read \
  ttl=24h
bao kv put secret/loopzero/test/hello message='Cross-cluster auth works!'
"
```

### Step 4: Provision test client in mgmt + start port-forward

```bash
# Extract Vault CA + create ConfigMap in mgmt
VAULT_CA=$(kubectl --context k3d-loopzero-secrets -n secrets-system \
  get secret openbao-tls -o jsonpath='{.data.ca\.crt}' | base64 -d)

kubectl --context k3d-loopzero-core apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata: {name: test-vault-reader, namespace: default}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: vault-ca, namespace: default}
data:
  ca.crt: |
$(echo "$VAULT_CA" | sed 's/^/    /')
EOF

# Start port-forward (Vault accessible on host:8210)
kubectl --context k3d-loopzero-secrets -n secrets-system port-forward openbao-0 \
  8210:8200 --address 0.0.0.0 >/tmp/pf.log 2>&1 &

# Create test client pod
kubectl --context k3d-loopzero-core apply -f - <<'EOF'
apiVersion: v1
kind: Pod
metadata: {name: test-vault-client, namespace: default}
spec:
  serviceAccountName: test-vault-reader
  restartPolicy: Never
  containers:
    - name: client
      image: quay.io/openbao/openbao:2.5.4
      command: ["sleep", "600"]
      env:
        - {name: BAO_ADDR, value: "https://host.k3d.internal:8210"}
        - {name: BAO_CACERT, value: /vault-ca/ca.crt}
      volumeMounts:
        - {name: vault-ca, mountPath: /vault-ca, readOnly: true}
  volumes:
    - {name: vault-ca, configMap: {name: vault-ca}}
EOF
```

### Step 5: E2E validation

```bash
kubectl --context k3d-loopzero-core exec test-vault-client -- sh -c '
SA_JWT=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)
VAULT_TOKEN=$(bao write -field=token auth/kubernetes/login \
  role=test-reader jwt=$SA_JWT)
export BAO_TOKEN=$VAULT_TOKEN
bao kv get secret/loopzero/test/hello
'
# Expected output:
# Key        Value
# ---        -----
# message    Cross-cluster auth works!
```

### Bugs caught during VK.5 LIVE validation

| Bug | Fix |
|-----|-----|
| `--disable=servicelb` in secrets-cluster.yaml blocked NodePort routing | Re-enabled servicelb (Klipper) |
| Klipper servicelb has TLS-passthrough issue on NodePort | Workaround: kubectl port-forward (port 8210, since 8200 already taken by k3d serverlb) |
| Cert SAN missing `host.k3d.internal` | Added to openbao-cert dnsNames |
| Pod restart re-seals Vault → must re-unseal manually | Expected prod posture (idempotent: re-run unseal command with shards) |
| ConfigMap CA cert needs refresh when Certificate rotates | Re-apply ConfigMap after Certificate renewal |

### Pattern for production (loopzero-api)

This validates the contract LoopZero platform will use:
1. Replace SA `test-vault-reader` → real `loopzero-api` SA
2. Replace test role → `loopzero-api-read` / `loopzero-api-write` roles per service
3. Replace port-forward → DOKS LoadBalancer or VPC peering
4. CA cert from cert-manager already prod-pattern

Cross-cluster TokenReview is mature K8s API; no LoopZero-specific glue needed.

## Releases deployed

**BUG8.E pivot 2026-06-02**: Bank-Vaults removed (operator bug #8 — see below).
Pivoted to official `openbao/openbao` chart maintained by OpenBao org. Zero
operator layer; deploys OpenBao StatefulSet via Helm direct.

**INIT (F2+) 2026-06-02**: post-startup config via `infra/helm/openbao-init/`
chart Job (substitui BaoConfiguration declarative pattern que sumiu com BUG8.E).
Idempotent: re-runs são safe (skip-on-exists para engines/auth; policies write
sempre overwrites).

| Release | Namespace | Source | Purpose |
|---------|-----------|--------|---------|
| cert-manager | cert-manager | jetstack v1.15.3 | TLS certificate issuance (prod only) |
| cluster-issuer | cert-manager | infra/helm/cluster-issuer | self-signed ClusterIssuer |
| openbao | secrets-system | openbao/openbao v0.28.3 (appVersion v2.5.4) | OpenBao StatefulSet (devMode local; HA Raft + KMS unseal prod) |
| openbao-init | secrets-system | infra/helm/openbao-init v0.1.0 | Post-startup Job: secrets engines + auth methods + policies (idempotent) |

## Validation outcome (2026-06-02 LIVE session)

**Validated (✅) — post-pivot BUG8.E (2026-06-02)**:
- k3d cluster creation + networking
- Helmfile vault-local environment
- cert-manager + cluster-issuer installation
- OpenBao StatefulSet (openbao/openbao v0.28.3) — pod Running 1/1 in 25s
- Initialized=true, Sealed=false (dev-mode auto-unseal)
- kv-v2 engine functional (`bao kv put secret/test foo=bar` + get retorna data)
- Service ClusterIP routing port 8200
- Root token `loopzero-dev-vault-root` reconhecido

**BUG #8 Resolution (via PIVOT)**:
Investigation confirmed Bank-Vaults reconciler bug in BOTH v1.24.0 AND v1.23.4
downgrade — NOT a version regression. Root cause: controller-runtime informer
cache desync (operator receives reconcile event but `client.Get()` returns
NotFound; impersonating operator SA via kubectl GET returns the object —
so RBAC is fine, informer cache is the issue).

**Hypotheses tested and rejected**:
- Hypothesis A (CRD-first): apply CRD 90s before operator startup — still fails
- Hypothesis B (downgrade 1.23.4): same NotFound error with older version
- Hypothesis C-E (schema, RBAC, namespace filter): all eliminated via probes

**Pivot decision (BUG8.E)**: replaced Bank-Vaults with official `openbao/openbao`
chart from OpenBao org (LinuxFoundation/OpenSSF). Trade-offs:
- GAINS: zero operator layer (eliminates entire bug class); source canonical;
  alignment with upstream OpenBao roadmap; simpler lifecycle (Helm direct).
- LOSSES: BaoConfiguration CR for declarative engine/auth config — will be
  replaced by init Job running `bao secrets enable` + `bao auth enable`
  post-startup (standard OpenBao pattern).

For F2+ secrets engine config: see future PR adding `infra/helm/openbao-init/`
chart with Job that runs `bao` CLI post-startup. F1 scope considered complete
with kv-v2 functional (LIVE validated).

## Bugs caught LIVE (with fixes)

This validation surfaced 8 bugs in the F1.4-F1.9 implementation — 7 fixed
during this session, 1 remaining (operator-side). Each one would have
detonated in DOKS prod first sync, costing 30-60min debug under pressure.

| # | Bug | Fix | File |
|---|-----|-----|------|
| 1 | OCI chart path `bank-vaults/charts` (404) | `bank-vaults/helm-charts` | `infra/helm/bank-vaults-operator/Chart.yaml` |
| 2 | Helm dep build loop with alias | Removed `alias:` from dependency | `infra/helm/bank-vaults-operator/Chart.yaml` |
| 3 | Wrong image+tag (`bank-vaults-operator:1.24.0`) | `vault-operator:v1.24.0` | `infra/helmfile/secrets.helmfile.yaml.gotmpl` |
| 4 | Helm v3 doesn't apply CRDs on upgrade | Manual `kubectl apply -f crds.yaml` on first run | (manual) |
| 5 | CRD `BaoConfiguration` doesn't exist | Switched to canonical `Vault` CRD | `infra/helm/openbao/templates/baoconfiguration.yaml` |
| 6 | Invalid K8s label with spaces/colons | Moved to annotations | `infra/helm/openbao/templates/_helpers.tpl` |
| 7 | VAP CEL syntax (`|` pipe + `default()`) | Use `has()` macro | `infra/helm/openbao/templates/vap-dev-mode.yaml` |
| 8 | Operator reconciler reports `not found` | TODO operator-time investigation | (upstream) |

## Cleanroom flow (when iterating)

```bash
# 1. Tear down
task k3d:secrets:destroy

# 2. Rebuild deps (if Chart.yaml changed)
rm -rf infra/helm/bank-vaults-operator/charts/ infra/helm/bank-vaults-operator/Chart.lock

# 3. Provision fresh
task k3d:secrets:create
task k3d:secrets:bootstrap

# 4. First-time only: apply Bank-Vaults CRD (helm v3 doesn't on upgrades)
bunzip2 -c infra/helm/bank-vaults-operator/charts/vault-operator/charts/crds/files/crds.bz2 \
  | kubectl --context k3d-loopzero-secrets apply -f -

# 5. Restart operator AFTER CRD applied (workaround for bug #8)
kubectl --context k3d-loopzero-secrets rollout restart deployment \
  -n bank-vaults-operator bank-vaults-operator-vault-operator

# 6. Validate
task k3d:secrets:status
```

## Troubleshooting

### "Error: An error occurred while checking for chart dependencies"

The wrapper chart approach was abandoned for `bank-vaults-operator` (see bug #2
in the table above). The Helmfile now installs the upstream OCI chart directly
with values inline. If this error reappears, check that no other code path
re-introduces a wrapper.

### "no matches for kind 'Vault' in version 'vault.banzaicloud.com/v1alpha1'"

The Bank-Vaults CRDs were not applied. Helm v3 only applies `crds/` on **first
install**, not on upgrades. Apply manually:

```bash
bunzip2 -c infra/helm/bank-vaults-operator/charts/vault-operator/charts/crds/files/crds.bz2 \
  | kubectl --context k3d-loopzero-secrets apply -f -
```

### Operator pod stuck on `vault.banzaicloud.com "openbao" not found`

Bug #8 (TODO operator-time). Workaround: restart operator pod AFTER Vault CR
exists (forces fresh informer cache sync):

```bash
kubectl --context k3d-loopzero-secrets rollout restart deployment \
  -n bank-vaults-operator bank-vaults-operator-vault-operator
```

If still failing after restart, downgrade to Bank-Vaults 1.23.4 in Helmfile and
report upstream at github.com/bank-vaults/vault-operator/issues.

### Vault Pod not appearing despite Vault CR

Operator MUST reconcile the Vault CR to spawn the StatefulSet. If pod absent,
operator is failing (see bug #8 above).

## When this graduates to DOKS prod

This local validation gives confidence to provision DOKS `loopzero-secrets-prod` with
the same Helmfile + values. Changes needed for prod:

1. Switch env `vault-local` → `prod` in `task k3d:secrets:bootstrap` invocation
2. Use `values-prod.yaml` (HA Raft + AWS KMS unseal) instead of standalone
3. Source `~/.openbao-prod-resources.env` before `helmfile sync` (per
   [openbao-aws-resources-runbook.md](./openbao-aws-resources-runbook.md))
4. Provision DOKS cluster via Pulumi (out of VK scope — separate T3 plan)
5. Wire mgmt cluster cross-cluster ClusterCredential to vault cluster
   (VK.5 deferred to follow-up sprint)

## Related

- [openbao-init-runbook.md](./openbao-init-runbook.md) — Shamir initialization (prod only)
- [openbao-aws-resources-runbook.md](./openbao-aws-resources-runbook.md) — AWS KMS + DO Spaces provisioning
- [break-glass-runbook.md](./break-glass-runbook.md) — Recovery without quorum
- `.claude/governance-gate.local.md` — VK scope approval
- ADR `2026-06-02-openbao-adoption.md` (Q1-Q16 architectural decisions)
