# Cluster Cleanup Runbook

**Audience:** platform operators, on-call.
**Scope:** k3d (`loopzero-mgmt`, `loopzero-build`, `loopzero-workload`) and DOKS (`do-sfo3-loopzero-*`).
**Risk class:** medium — uses `--type=merge` patches that bypass the operator's reconcileDelete. Always run `--dry-run` first.

---

## When to use

You should reach for this runbook when one or more of the following are true:

- `kubectl get ns` shows namespaces stuck in `Terminating` for >5 minutes (T0.1).
- `kubectl get tenants -A | grep Deleting` returns CRs older than 5 minutes (T0.2).
- `kubectl config get-contexts` lists `do-sfo3-*` contexts whose clusters were destroyed (T0.3).
- Pre-flight (`infra/tests/dogfood/preflight.sh`) reports T0.10 / T0.11 failures.

If the symptoms match, but the resources are **production tenants** (names matching `tn-*` rather than `loopzero-test-*` / `vap-test-*` / `test-*`), STOP. None of these scripts touch production tenants — the allowlist is enforced. Investigate the controller logs first.

---

## T0.1 — Namespaces stuck in Terminating

### Symptoms

```
$ kubectl --context k3d-loopzero-workload get ns | grep Terminating
loopzero-test-add-env-idem-production         Terminating  17h
loopzero-test-add-env-production              Terminating  17h
... (9 total)
```

The namespace's metadata.finalizers list is non-empty even though the workload is gone. Re-creating an Environment with the same name fails because the namespace already exists.

### Why this happens

The Environment controller's `reconcileDelete()` raced with an external delete (e.g., `kubectl delete ns ...` issued before the operator finished). Kubernetes will not garbage-collect the namespace until every entry in `metadata.finalizers` is cleared.

### Procedure

```bash
# 1) Inspect with dry-run first
CTX=k3d-loopzero-workload bash infra/tests/dogfood/scripts/cleanup-terminating-namespaces.sh --dry-run

# 2) Apply when the dry-run output looks correct (allowlist prefixes only)
CTX=k3d-loopzero-workload bash infra/tests/dogfood/scripts/cleanup-terminating-namespaces.sh

# 3) Verify
kubectl --context k3d-loopzero-workload get ns | grep Terminating || echo "clean"
```

### Allowlist (hardcoded — DO NOT widen without review)

- `loopzero-test-*`
- `build-test-*`
- `build-vap-test-*`
- `loopzero-vap-test-*`
- `loopzero-tn-e2etest-staging`

Any other namespace prints a refusal and exits non-zero.

### Patch shape (for reference)

The script uses `kubectl patch ns <name> --type=merge -p '{"metadata":{"finalizers":[]}}'`. We deliberately avoid `kubectl delete --force --grace-period=0` because that leaves orphans in etcd.

---

## T0.2 — Tenant / Environment CRs stuck in Deleting

### Symptoms

```
$ kubectl --context k3d-loopzero-mgmt -n loopzero-system get tenants
test-rm-env-idem        Deleting   12h
test-rm-env-last        Deleting   12h
vap-test-allowed        Deleting   12h
```

The tenant-controller logs show repeated reconcile attempts that never make progress.

### Procedure

```bash
# 1) Dry-run to confirm allowlist
CTX=k3d-loopzero-mgmt bash infra/tests/dogfood/scripts/cleanup-stuck-finalizers.sh --dry-run

# 2) Apply
CTX=k3d-loopzero-mgmt bash infra/tests/dogfood/scripts/cleanup-stuck-finalizers.sh

# 3) Verify
kubectl --context k3d-loopzero-mgmt -n loopzero-system get tenants
kubectl --context k3d-loopzero-mgmt -n loopzero-system get environments
```

### Why this happens (typical root causes)

1. The Environment CR was deleted out-of-band (e.g., direct `kubectl delete env`), so the Tenant's owner-ref-driven cleanup loops on `NotFound`.
2. Pulumi state for the tenant's cluster credentials is gone, so the controller's call to `cluster_credentials.GetByTenant()` returns nothing → controller cannot complete the finalizer.

If you observe these on production tenants (names matching `tn-*`), file an incident — the script will refuse them.

---

## T0.3 — Dangling DOKS contexts in kubeconfig

### Symptoms

```
$ kubectl config get-contexts | grep loopzero-
loopzero-build         do-sfo3-loopzero-infra-dev-sfo3-build       <- cluster destroyed?
loopzero-mgmt          do-sfo3-loopzero-infra-dev-sfo3-mgmt
loopzero-runtime       do-sfo3-loopzero-infra-dev-sfo3-runtime
```

`kubectl --context loopzero-build get ns` hangs / times out / returns connection refused.

### Procedure

The pre-flight script's T0.11 check probes each `do-sfo3-*` context with a 5-second `kubectl get ns kube-system` and reports stale ones at WARN level. To remove:

```bash
# 1) Identify dead contexts
for ctx in $(kubectl config get-contexts -o name | grep -E '^do-sfo3-loopzero|^loopzero-(mgmt|build|runtime)$'); do
  if ! timeout 5 kubectl --context "$ctx" get ns kube-system >/dev/null 2>&1; then
    echo "DEAD: $ctx"
  fi
done

# 2) Remove (one at a time — verify between each)
kubectl config delete-context <ctx-name>
kubectl config delete-cluster <cluster-name>
kubectl config delete-user    <user-name>
```

We deliberately surface this as WARN (not FAIL) in pre-flight: a stale context never causes incorrect behavior on its own — it only invites typos that target the wrong cluster. Operators should clean up between sprints.

---

## Idempotency contract

All three scripts (`cleanup-terminating-namespaces.sh`, `cleanup-stuck-finalizers.sh`, the manual `kubectl config delete-*` flow) are idempotent:

- Running them twice in a row produces the same end state.
- Resources outside the allowlist are skipped, never patched.
- Exit code 0 means "the cluster is now in the target state" (which may mean "nothing to do").

The pre-flight script (`infra/tests/dogfood/preflight.sh`) treats T0.10 / T0.11 as gating: it exits non-zero if there are zumbis in scope, so CI will refuse to start dogfood until cleanup runs.

---

## Cross-references

- `infra/tests/dogfood/scripts/cleanup-terminating-namespaces.sh`
- `infra/tests/dogfood/scripts/cleanup-stuck-finalizers.sh`
- `infra/tests/dogfood/preflight.sh` (T0.10, T0.11)
- `docs/runbooks/bootstrap-regression-matrix.md` entries 124, 125
- `docs/plans/infra-parity-plan.md` Phase 0 (T0.1, T0.2, T0.3)
