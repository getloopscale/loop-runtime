# Operator Topology Cutover Runbook

> Plan reference: `docs/plans/infra-normalize-multicell-plan.md` Phase 2.
> Source decision: `docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md` (condition C23).
> Last updated: 2026-04-30.
> Operators: SRE on-call + change-governor + executor (one human responsible for the actions; on-call observes).

This runbook executes the forward-only operator topology migration (ADR-0021 D1) per cluster. The cutover window is intentionally short (target ≤8 minutes per cluster end-to-end, hard limit ≤30 minutes); preparation and post-validation extend the total exercise to ~45-60 minutes.

The order is: dev clusters first, soak 24h, then production canary, then 100%. Per-cluster steps are below.

---

## Pre-flight (DO NOT SKIP)

Required state BEFORE running the cutover:

1. **Decision record up-to-date.** `docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md` shows APPROVED status. If conditions remain unchecked, ABORT and address them first.
2. **Validation matrix GREEN.** `task validate:matrix` — 10/10 PASS. If anything fails (especially `rbac-subset` or `helm:lint-split-operators`), ABORT.
3. **Rollback script tested in dev.** `bash infra/scripts/operator-cutover-rollback.sh <dev-ctx>` — must execute the script's pre-flight (steps 1-2) successfully against a dev cluster on the parent commit. (We do NOT execute the full rollback in dry-run because it would actually change the cluster; we test the parent-commit-resolution logic.)
4. **Charts deps built.** `task validate:helm-deps` ran clean within the last 24h.
5. **Active builds drain — Step ZERO (condition C23).** Confirm no in-flight CLI operations against the target cluster:
   ```
   kubectl --context <CTX> -n loopzero-system logs deploy/tenant-controller --tail 50
   # Look for "reconcile started" without matching "reconcile completed" within 30s
   #
   # If active reconcile in flight: WAIT for it to complete OR cancel
   # the upstream operation (loopzero deploy / loopzero platform install). Do
   # NOT proceed mid-reconcile — the cutover gap may leave the CR in
   # an intermediate state requiring manual recovery.
   ```
6. **Backup secrets.** `loopzero platform secrets backup` — captures tenant secrets BEFORE cutover, so if the worst happens secrets restore is trivial.
7. **Capture the current cutover commit SHA.** `git rev-parse HEAD` — this is what gets passed to the rollback script if needed.

If ANY pre-flight step fails: ABORT. Do not "just run it"; the rollback script depends on pre-flight invariants.

---

## Step 1 — Cutover commit lands on `develop`

The cutover PR (single commit; per ADR D1 forward-only) gets merged via `gh pr merge`. The PR body must reference:
- `MEET-INFRA-NORMALIZE-T3-operator-split.md` for decision context
- Conditions checklist (C1-C24) with current status
- This runbook URL

**This step happens on the developer machine, not the cluster.** No cluster-side action yet.

---

## Step 2 — Helmfile sync against the new state

Per cluster (one at a time; never parallel for production):

```
# Set context and environment
export KUBECONFIG=<path-to-cluster-kubeconfig>
ENV=<dev|prod|standalone-multi|standalone>
CTX=<cluster-context-name>

# Tail tenant-controller logs in another terminal
kubectl --context $CTX -n loopzero-system logs deploy/tenant-controller -f --tail 0 &

# Sync — this is the cutover
helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl -e $ENV --kube-context $CTX sync 2>&1 | tee /tmp/cutover-$CTX-$(date +%s).log
```

Expected duration: 3-5 minutes for the helmfile sync itself. Watch for:
- New deployments `loopzero-{tenant,app,preview,platform}-operator` appearing in `loopzero-system` ns.
- Old deployments `tenant-controller` + `loopzero-controller` scaling to 0 then deleting.
- Lease takeover in tenant-controller logs ("released lease ... acquired by ...").

If the sync hangs or fails:
- DO NOT panic — helmfile will roll back to the last good state on its own.
- Check the log file at `/tmp/cutover-$CTX-*.log` for the failing release.
- If recovery via helmfile is not possible: **execute rollback** (Step 6).

---

## Step 3 — Wait for new replicas to reach Ready

```
for op in loopzero-tenant-operator loopzero-app-operator loopzero-preview-operator loopzero-platform-operator; do
  kubectl --context $CTX -n loopzero-system wait deploy/$op --for=condition=Available --timeout=300s
done
```

This MUST succeed within 5 minutes. If any deploy stays Unavailable:
- Check pod events: `kubectl --context $CTX -n loopzero-system describe pod -l app.kubernetes.io/name=<op>`
- Common failure: ImagePullBackOff (pull-secret missing — confirm values overlay is correct per Invariant I-6).
- Common failure: CrashLoopBackOff in 1st replica due to leader-election lease still held by old monolith. Resolve by waiting another 30s; lease expires.

If after 5 minutes deploys still not Ready: **execute rollback** (Step 6).

---

## Step 4 — Validate reconcile activity resumed

After all 4 split operators reach Ready, run synthetic verification:

```
# Apply a no-op annotation to a Tenant CR; the reconcile loop should
# observe + emit a status update within ~30s.
TENANT=$(kubectl --context $CTX -n loopzero-system get tenants -o name | head -1)
kubectl --context $CTX -n loopzero-system annotate $TENANT \
  cutover-verification=$(date +%s) --overwrite

# Wait 60s, then check that tenant-operator logged a reconcile.
sleep 60
kubectl --context $CTX -n loopzero-system logs deploy/loopzero-tenant-operator --since=90s | grep -i reconcile | head -5
```

If no reconcile log line appears in 90s: **execute rollback** (Step 6).

---

## Step 5 — Soak (per environment)

After successful Steps 2-4:

| Environment | Soak before next env |
|---|---|
| `standalone` (k3d single) | 5 minutes — confirm no pod restart in tenant-operator |
| `standalone-multi` (k3d 3-cluster) | 30 minutes — exercise journey-01 dogfood |
| `dev` (DOKS shared dev) | 24 hours — covers daily build cycle |
| `prod` (DOKS production) | only after `dev` 24h soak GREEN; canary stages 4-5 from MEET-INFRA-NORMALIZE-T3-multi-cell apply (NOT relevant to operator split per se, but the soak observation is the same) |

During soak, alerts that may fire:
- `OperatorRestartRate24h` (NEW — see C15 below) — fires if any of the 4 split deployments restarts >5 times in 24h.
- `OperatorReconcileSlowdown` (existing per T2.1) — fires if reconcile p95 latency degrades >2× pre-cutover baseline.

If either fires during soak: ESCALATE; do not promote to next env.

---

## Step 6 — Rollback (if any of Steps 2-4 fails OR alerts fire during soak)

```
# Capture the cutover SHA
LOOPZERO_CUTOVER_COMMIT=$(git rev-parse HEAD)
export LOOPZERO_CUTOVER_COMMIT

# Run rollback
bash infra/scripts/operator-cutover-rollback.sh $CTX
```

The script will:
1. Spool the parent commit's helmfile to a temp dir.
2. helmfile sync against that (re-creates monolith releases).
3. Wait for monolith Ready.
4. Scale + delete the 4 split deployments.
5. Verify reconcile activity resumed.

Target: ≤5 minutes per condition C5. Observed: typically 3-4 minutes.

After rollback:
1. Open an incident channel.
2. Capture the cutover failure mode (logs from /tmp/cutover-*.log).
3. Update the cutover PR with a retro section.
4. The rollback git commit (reverse-patch per workflow.md line 33) gets a fresh PR — DO NOT use `git revert` or any other history-rewriting operation.

---

## Post-cutover (24h monitoring)

After the soak period passes for an environment:

1. Check `OperatorRestartRate24h` alert is silent for 24h.
2. Check `loopzero_operator_reconcile_duration_seconds` (per T2.1 obs plan) p95 trend is flat or improved.
3. Check `loopzero_cell_routing_decisions_total{decision="error"}` counter has not increased anomalously (multi-cell metrics from T5.4).
4. Confirm no manual `kubectl edit` on Tenant/Environment CRs in the last 24h (indicator of operator failing to reconcile, prompting human intervention).

If all GREEN: promote to next environment.

If any alert/anomaly: pause; investigate; rollback if needed.

---

## Cleanup (T2.5 / sprint 12 horizon)

After 30 days of stable runtime on the split topology with no restart-rate alert:

```
# Single PR, single review:
git rm -r infra/helm/tenant-controller infra/helm/loopzero-controller
# Update mgmt.helmfile.yaml.gotmpl to remove any residual references
```

This is condition C23's "deferred cleanup" (T2.5). Until that PR lands, the monolith chart files remain in tree as the rollback escape hatch.

---

## Cross-references

- `docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md` — conditions C1-C24
- `docs/decisions/operator-split-invariants.md` — I-1..I-8 (load-bearing for verification gates above)
- `docs/decisions/ADR-0021-infra-normalize-decisions.md` D1 — forward-only migration rationale
- `infra/scripts/operator-cutover-rollback.sh` — Step 6 implementation
- `infra/scripts/rbac-subset-check.sh` — pre-flight gate
- `infra/tests/parity/validate-matrix.sh` — pre-flight gate
- `.github/workflows/ci.yml` — `rbac-subset` job
- `.claude/rules/workflow.md` line 33 — forbidden git operations
