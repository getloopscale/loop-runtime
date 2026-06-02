# Vault Cluster Local Runbook (loopzero-vault k3d)

**Owner**: Platform Security
**SOT**: this file
**Companion**: [openbao-init-runbook.md](./openbao-init-runbook.md), [openbao-aws-resources-runbook.md](./openbao-aws-resources-runbook.md)
**Plan**: VK scope of sponsor-direct vault cluster isolation T3 (2026-06-02)

## Purpose

Local k3d cluster `loopzero-vault` for validating vault cluster isolation topology
(per T3 decision 2026-06-02) BEFORE provisioning DOKS `loopzero-vault-prod`. Same
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
│  │ loopzero-mgmt    │  │ loopzero-build   │  │  loopzero-vault (NEW)    │       │
│  │ kubeAPI 6443 │  │ kubeAPI 6445 │  │  kubeAPI 6447         │      │
│  │ 1srv+2agt    │  │ 1srv         │  │  1srv (single-node)  │       │
│  └──────────────┘  └──────────────┘  └──────────────────────┘       │
│         ▲                ▲                     ▲                    │
│         └────────────────┴─────────────────────┘                    │
│                          │                                          │
│                k3d-loopzero-registry:5000                               │
└─────────────────────────────────────────────────────────────────────┘
```

## Quick start

```bash
task k3d:vault:create     # 43s — bring cluster up
task k3d:vault:bootstrap  # 2-3min — install cert-manager + bank-vaults + openbao
task k3d:vault:status     # show pods + Vault CR + seal state
```

To destroy + start fresh:

```bash
task k3d:vault:destroy
task k3d:vault:create
task k3d:vault:bootstrap
```

## Releases deployed

| Release | Namespace | Source | Purpose |
|---------|-----------|--------|---------|
| cert-manager | cert-manager | jetstack v1.15.3 | TLS certificate issuance |
| cluster-issuer | cert-manager | infra/helm/cluster-issuer | self-signed ClusterIssuer |
| bank-vaults-operator | bank-vaults-operator | oci://ghcr.io/bank-vaults/helm-charts/vault-operator 1.24.0 | Controller reconciling Vault CRs |
| openbao | vault-system | infra/helm/openbao | Vault CR (devMode in local; HA Raft in prod) |

## Validation outcome (2026-06-02 LIVE session)

**Validated (✅)**:
- k3d cluster creation + networking
- Helmfile vault-local environment
- cert-manager + cluster-issuer installation
- Bank-Vaults operator pod Running
- Vault CRD discovery (`kubectl api-resources --api-group=vault.banzaicloud.com`)
- Vault CR Helm-applied with correct schema (devMode + inmemory storage)
- cert-manager Certificate emission
- ServiceAccount + RBAC for operator

**Pending (⏳ — operator-time bug investigation)**:
- Bank-Vaults operator 1.24.0 reconciler reports `vaults.vault.banzaicloud.com "openbao" not found`
  even though the CR exists with all proper labels + RBAC grants are correct.
  Suspected root cause: informer cache sync timing OR Bank-Vaults 1.24.0 upstream bug
  triggered when CRD is applied after the operator pod is already running.

  Workarounds to try when investigating:
  1. Restart operator AFTER CRD applied + Vault CR created (instead of before)
  2. Use Bank-Vaults 1.23.4 (previous stable) to see if regression
  3. Open issue at github.com/bank-vaults/vault-operator with the LIVE log dump

## Bugs caught LIVE (with fixes)

This validation surfaced 8 bugs in the F1.4-F1.9 implementation — 7 fixed
during this session, 1 remaining (operator-side). Each one would have
detonated in DOKS prod first sync, costing 30-60min debug under pressure.

| # | Bug | Fix | File |
|---|-----|-----|------|
| 1 | OCI chart path `bank-vaults/charts` (404) | `bank-vaults/helm-charts` | `infra/helm/bank-vaults-operator/Chart.yaml` |
| 2 | Helm dep build loop with alias | Removed `alias:` from dependency | `infra/helm/bank-vaults-operator/Chart.yaml` |
| 3 | Wrong image+tag (`bank-vaults-operator:1.24.0`) | `vault-operator:v1.24.0` | `infra/helmfile/vault.helmfile.yaml.gotmpl` |
| 4 | Helm v3 doesn't apply CRDs on upgrade | Manual `kubectl apply -f crds.yaml` on first run | (manual) |
| 5 | CRD `BaoConfiguration` doesn't exist | Switched to canonical `Vault` CRD | `infra/helm/openbao/templates/baoconfiguration.yaml` |
| 6 | Invalid K8s label with spaces/colons | Moved to annotations | `infra/helm/openbao/templates/_helpers.tpl` |
| 7 | VAP CEL syntax (`|` pipe + `default()`) | Use `has()` macro | `infra/helm/openbao/templates/vap-dev-mode.yaml` |
| 8 | Operator reconciler reports `not found` | TODO operator-time investigation | (upstream) |

## Cleanroom flow (when iterating)

```bash
# 1. Tear down
task k3d:vault:destroy

# 2. Rebuild deps (if Chart.yaml changed)
rm -rf infra/helm/bank-vaults-operator/charts/ infra/helm/bank-vaults-operator/Chart.lock

# 3. Provision fresh
task k3d:vault:create
task k3d:vault:bootstrap

# 4. First-time only: apply Bank-Vaults CRD (helm v3 doesn't on upgrades)
bunzip2 -c infra/helm/bank-vaults-operator/charts/vault-operator/charts/crds/files/crds.bz2 \
  | kubectl --context k3d-loopzero-vault apply -f -

# 5. Restart operator AFTER CRD applied (workaround for bug #8)
kubectl --context k3d-loopzero-vault rollout restart deployment \
  -n bank-vaults-operator bank-vaults-operator-vault-operator

# 6. Validate
task k3d:vault:status
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
  | kubectl --context k3d-loopzero-vault apply -f -
```

### Operator pod stuck on `vault.banzaicloud.com "openbao" not found`

Bug #8 (TODO operator-time). Workaround: restart operator pod AFTER Vault CR
exists (forces fresh informer cache sync):

```bash
kubectl --context k3d-loopzero-vault rollout restart deployment \
  -n bank-vaults-operator bank-vaults-operator-vault-operator
```

If still failing after restart, downgrade to Bank-Vaults 1.23.4 in Helmfile and
report upstream at github.com/bank-vaults/vault-operator/issues.

### Vault Pod not appearing despite Vault CR

Operator MUST reconcile the Vault CR to spawn the StatefulSet. If pod absent,
operator is failing (see bug #8 above).

## When this graduates to DOKS prod

This local validation gives confidence to provision DOKS `loopzero-vault-prod` with
the same Helmfile + values. Changes needed for prod:

1. Switch env `vault-local` → `prod` in `task k3d:vault:bootstrap` invocation
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
