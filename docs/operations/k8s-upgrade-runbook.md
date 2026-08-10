---
type: operational-runbook
title: Kubernetes Upgrade Runbook (DOKS)
---

# Kubernetes Upgrade Runbook (DOKS)

Operator-facing guide for upgrading the DOKS minor version (e.g., v1.32 → v1.33).
Adopted via plan [`docs/plans/completed/2026-05-22-shell-script-strategic-plan.md`](../plans/completed/2026-05-22-shell-script-strategic-plan.md) v1.3 Phase 7 T7.1.

---

## Pre-flight: Pluto deprecated API check

Before approving any DOKS minor upgrade, confirm that all current Helm charts
compile to manifests using APIs available in the target K8s version.

> **The automated gate no longer exists.** `.github/workflows/pluto-deprecated-apis.yml` and the
> `plutogate` Go check were both removed on 2026-05-23 with the GitHub Actions retirement — the gate
> only validated that workflow, so it went with it. **This check is now manual**, and running it is a
> hard pre-condition of any minor upgrade. Re-homing it as a `task quality:*` gate before the next
> DOKS minor bump is open work; the fixtures at `infra/tests/pluto/fixtures/` were kept for that.

### Manual run

```bash
# Render every chart, then scan the output for APIs removed in the target version.
task validate:helmfile-templates          # renders all releases
pluto detect-files -d /tmp/rendered --target-versions k8s=v1.33.0
```

Install Pluto from https://pluto.docs.fairwinds.com/ if it is not on PATH.

The gate fails CI if any rendered manifest contains:
- An API **REMOVED** in the target K8s version (FATAL)
- An API **DEPRECATED** with replacement available (warning surfaced via `only-show-removed: false`)

### Manual (operator-time before upgrade)

When planning a minor upgrade, bump `K8S_TARGET_VERSION` in the workflow file
to the **target** version and run via `workflow_dispatch`. Example:

```yaml
# .github/workflows/pluto-deprecated-apis.yml
env:
  K8S_TARGET_VERSION: v1.33.0  # was v1.32.0
```

Trigger manually:
```bash
gh workflow run pluto-deprecated-apis.yml --ref refactor/k8s-upgrade-v1.33
```

Read the workflow output:
- **PASS**: no removed/deprecated APIs detected — upgrade is API-safe
- **FAIL**: list of affected manifests + apiVersion to update. Address each
  (update chart templates to use replacement apiVersion) BEFORE proceeding
  with the upgrade.

---

## Upgrade sequence (after Pluto PASS)

1. **Pre-upgrade evidence:**
   - Pluto gate PASS for target version ✓
   - Backup snapshot of mgmt PG via Velero (verify via `/dogfood velero` skill)
   - Document current cluster versions: `doctl kubernetes cluster list`

2. **Upgrade cell-0 (canary cell):**
   ```bash
   doctl kubernetes cluster upgrade <core-cluster-id> --version v1.33-do.0
   doctl kubernetes cluster upgrade <build-cluster-id> --version v1.33-do.0
   ```
   Wait for upgrade complete (~10-20min per cluster).

3. **Validate health post-mgmt+build upgrade:**
   - `task doctor` PASS=N FAIL=0
   - All pods in mgmt namespace Running
   - ArgoCD repo creds + sync functional

4. **Smoke deploy:**
   - Test deploy against cell-0
   - Validate `loopzero deploy` → live URL responds 200
   - SSE rollout events flow correctly

5. **Upgrade additional cells (one at a time):**
   ```bash
   doctl kubernetes cluster upgrade <runtime-cluster-id> --version v1.33-do.0
   doctl kubernetes cluster upgrade <runtime-cell-1-id> --version v1.33-do.0
   ```

6. **Post-upgrade evidence:**
   - All cells on target version: `doctl kubernetes cluster list | grep -v <target>`
   - Update `K8S_TARGET_VERSION` env in pluto workflow to the new version (locks gate to current production)
   - Run `/dogfood full` end-to-end pass (12 journeys GREEN)

---

## Rollback (if Pluto warned but ignored, or post-upgrade incidents)

DOKS does **NOT** support minor-version rollback in-place. The recovery path:

1. **Hot-fix forward** if affected manifest still works (most cases):
   - Identify affected workload via ArgoCD UI Degraded state
   - Update chart template to use replacement apiVersion
   - Helmfile/ArgoCD sync — typically <5min recovery

2. **Cold restore** if upgrade is catastrophic (extremely rare):
   - Velero restore from pre-upgrade snapshot
   - Re-provision DOKS clusters at PREVIOUS version (Pulumi `--target` versioning)
   - Repoint DNS via Cloudflare Worker — see `docs/operations/dns-cutover-runbook.md`

---

## Reference

- Pluto upstream: https://pluto.docs.fairwinds.com/
- Workflow file: `.github/workflows/pluto-deprecated-apis.yml` — **deleted 2026-05-23**
- Hermetic tests: `cmd/loopzero-ops/internal/quality/plutogate/` — **deleted 2026-05-23** (the gate only validated the workflow above)
- Test fixtures: `infra/tests/pluto/fixtures/{deprecated,current}.yaml` — **retained**, ready for a re-homed gate
- Plan section: [`docs/plans/completed/2026-05-22-shell-script-strategic-plan.md`](../plans/completed/2026-05-22-shell-script-strategic-plan.md) Phase 7 T7.1
