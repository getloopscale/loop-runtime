# ADR-0018 Rollout Runbook — Tenant SoT to `loopzero-system`

> **Status:** Required gate per MEET-ADR-0018 condition C7
> **Owner:** sre / on-call (production), operator (self-hosted)
> **Audience:** anyone executing `task migrate:tenants` + operator upgrade + admission-policies upgrade
> **Estimated time:** 15-30 min (pre-flight + apply + verify)

## Mandatory deployment ORDER (C4)

This order is **NOT negotiable**. Reversing causes self-inflicted DoS (production-design-reviewer Failure Mode #1).

```
┌──────────────────────────┐
│ Step 1: T3.3 migration   │  Cleanup data drift in stale namespaces
│  task migrate:tenants    │  → exit 0 required before proceeding
│      -- --apply           │
└──────────────────────────┘
            │
            ▼
┌──────────────────────────┐
│ Step 2: Operator upgrade │  Cache scoping (C9) + const namespace (C1)
│  helmfile -e <env> sync  │  → operator binary now refuses to write
│   <operator chart>       │     Tenants/Environments outside loopzero-system
└──────────────────────────┘
            │
            ▼
┌──────────────────────────┐
│ Step 3: Apply T3.2 VAP   │  ValidatingAdmissionPolicy (C6 status excl.)
│  helmfile -e <env> sync  │  → ANY future CREATE/UPDATE outside
│   admission-policies     │     loopzero-system rejected at API server
└──────────────────────────┘
```

## Pre-flight checklist

Run **all** before Step 1:

- [ ] **Backup**: `kubectl get tenants,environments -A -o yaml > /tmp/pre-adr-0018-$(date +%s).yaml`
- [ ] **Inventory**: `kubectl get tenants,environments -A` — count CRs in non-`loopzero-system` namespaces. Zero = nothing to migrate; >0 = continue.
- [ ] **Customer notice (C8)**: 24h prior, sent to known self-hosted customer with Tenants in non-canonical namespaces.
- [ ] **Dry-run migration**: `bash infra/scripts/migrate-tenant-namespace.sh` (without `--apply`) — exit 0 expected, output shows `decide_winner` for each.
- [ ] **Verify operator chart version** (`infra/helm/tenant-controller/Chart.yaml`) bumped to include T3.1 commits.
- [ ] **Verify admission-policies chart** (`infra/helm/admission-policies/values.yaml`) has `vaps.tenantNamespace: false` initially (apply Step 3 explicitly).
- [ ] **On-call available**: pager confirmed; rollback procedure rehearsed mentally.
- [ ] **Health baseline**: `curl localhost:13001/api/v1/health/deep` → `status: ok`. Save output to `/tmp/baseline-health.json`.

## Step 1 — Apply T3.3 migration

```bash
# Dry-run first (always).
task migrate:tenants

# If dry-run output looks correct, apply:
task migrate:tenants -- --apply
```

**Expected**: `[migrate-tenant-namespace] migration complete.` + final exit 0.

**Abort if**:
- Exit code 2 → manual decision required (non-canonical CR is newer than canonical). Investigate before proceeding.
- Backup directory `~/.loopzero/backups/tenant-migration-*` not created → script crashed early.
- `kubectl get tenants -A` post-step shows ≥1 in non-`loopzero-system`.

**Verify**:
```bash
kubectl get tenants,environments -A
# Expected: only loopzero-system entries.

curl -s localhost:13001/api/v1/health/deep | jq .
# Expected: status: ok (T3.3 should not break health).
```

**Rollback Step 1** (if needed): the migration is destructive (delete in original namespace). Restore via the backup YAML written to `~/.loopzero/backups/tenant-migration-<timestamp>/`. Each Tenant/Environment YAML is preserved with `resourceVersion` stripped — re-apply with `kubectl apply -f`.

## Step 2 — Operator upgrade (T3.1)

```bash
# Production: GitOps controlled. Verify ArgoCD App tenant-controller picks up new chart.
kubectl --context k3d-loopzero-mgmt -n loopzero-system describe application tenant-controller -n argocd

# Self-hosted: manual helmfile.
helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl -e <env> sync -l name=tenant-controller
```

**Expected**: `tenant-controller` Pod restarts with new image; logs show:
```
"cache restricted to namespaces" namespaces=["loopzero-system"] kinds=[Tenant,Environment,...]
"misplaced tenant scan complete" misplaced=0
```

**Abort if**:
- `loopzero_misplaced_tenant_total{namespace="default"} > 0` → Step 1 did not fully clean. Re-run T3.3.
- Operator CrashLoopBackOff → roll back to previous image immediately.
- `kubectl get applications -n argocd` shows pre-existing tenant Apps becoming OutOfSync — operator scope changed without expected. Investigate before Step 3.

**Verify**:
```bash
kubectl logs -n loopzero-system -l app=tenant-controller --tail=50 | grep -i "cache\|misplaced"

# Healthy reconcile of canonical Tenant:
kubectl get tenant tn-e2etest -n loopzero-system -o jsonpath='{.status.phase}'
# Expected: Active
```

**Rollback Step 2** (if needed): `helm rollback tenant-controller <prev-revision>` or revert chart commit. Operator returns to cluster-wide watch — C9 protections lost but no destructive impact.

## Step 3 — Apply T3.2 VAP

```bash
# Toggle on in admission-policies chart values overlay:
helmfile -f infra/helmfile/runtime.helmfile.yaml.gotmpl -e <env> sync -l name=admission-policies

# Or directly with values flag:
helm --kube-context <ctx> upgrade admission-policies infra/helm/admission-policies \
  -n cosign-system \
  --set vaps.tenantNamespace=true \
  --reuse-values
```

**Expected**: VAP `loopzero-tenant-namespace-policy` + ValidatingAdmissionPolicyBinding present.
```bash
kubectl get validatingadmissionpolicy loopzero-tenant-namespace-policy
kubectl get validatingadmissionpolicybinding loopzero-tenant-namespace-policy-binding
```

**Smoke test**:
```bash
# Should succeed (canonical):
kubectl apply -f - <<EOF
apiVersion: loopzero.io/v1
kind: Tenant
metadata: { name: smoke-test, namespace: loopzero-system }
spec: { plan: starter, displayName: smoke }
EOF
kubectl delete tenant smoke-test -n loopzero-system

# Should FAIL with forbidden (VAP rejection):
kubectl apply -f - <<EOF
apiVersion: loopzero.io/v1
kind: Tenant
metadata: { name: smoke-rejected, namespace: default }
spec: { plan: starter, displayName: smoke }
EOF
# Expected: error: Tenant/Environment CRs must be created in loopzero-system (...)
```

**Abort if**:
- Status writes fail (operator stops reconciling) → C6 status subresource exclusion broken. Roll back VAP immediately.
- Existing Tenant updates blocked → indicates Step 1 left misplaced CRs. Roll back VAP, re-run T3.3.

**Rollback Step 3**: `helm upgrade admission-policies ... --set vaps.tenantNamespace=false`. VAP removed; system returns to T3.1-only protection (sufficient against operator-side regression but not against user `kubectl apply`).

## DR / Restore handling (C16)

If you restore the cluster from backup or run PITR:

1. **Before restoring**: review restored Tenant CRs. Any in non-`loopzero-system` namespaces will be **rejected** by VAP on first update.
2. **Restore procedure addendum**:
   - Restore backup.
   - **Run T3.3 migration immediately**: `task migrate:tenants -- --apply`.
   - **Then** allow operator to reconcile (operator may have been part of the same backup, will start with old binary if pre-T3.1).
3. **Document the post-restore migration step in any incident playbook that references this cluster.**

## Customer comms (C8)

For self-hosted customers with Tenant CRs in non-`loopzero-system` namespaces:

```
Subject: [LoopZero] Action required — namespace migration window <date>

Hi,

We are tightening the Tenant CR namespace boundary in our next release
(ADR-0018). Tenant + Environment CRs must live in `loopzero-system` going
forward; existing CRs in other namespaces will be migrated automatically
by `loopzero platform upgrade`.

Migration window: <date> 14:00-14:30 UTC. On-call: <pager>.
Pre-flight check:
  kubectl get tenants -A | grep -v loopzero-system

If non-empty, you can migrate proactively:
  task migrate:tenants
  task migrate:tenants -- --apply

Reply if you have questions or need assistance.
```

## Post-rollout monitoring (7-day window)

- [ ] Day 1, 3, 7: `loopzero_misplaced_tenant_total{namespace}` Prometheus query — expect 0 for all namespaces. >0 = investigate (something is creating Tenants outside `loopzero-system` — VAP should reject, indicates VAP misconfiguration or operator drift).
- [ ] Alert `MisplacedTenantDetected` configured (C5). Verify by intentionally creating a Tenant in `default` (admin-bypass — normal users blocked by VAP) — alert fires within 5 min, then delete the test CR.
- [ ] CI gate: `task validate:full` passes including new T-01..T-11 tests.

## Sign-off

Each rollout produces an entry in `docs/runbooks/rollout-log.md`:

```
- 2026-MM-DD HH:MM UTC <env> — ADR-0018 rolled out
  - T3.3 exit 0 ✓
  - operator upgraded: <chart_version> → <new_chart_version>
  - VAP applied; smoke tests passed (allow loopzero-system, reject default)
  - misplaced_tenant_total stable at 0 for 24h ✓
  - on-call: <name>
```

## References

- MEET-ADR-0018 decision record (binding)
- ADR-0018 v2 (this document's parent)
- `infra/scripts/migrate-tenant-namespace.sh` (T3.3 implementation)
- `infra/helm/admission-policies/values.yaml` (toggle reference)
- CLAUDE.md `pre-release-governance.md` (T2 trigger rules)
