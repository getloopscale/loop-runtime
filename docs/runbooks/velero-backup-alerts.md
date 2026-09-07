# Runbook — Velero backup alerts

**Owner:** SRE · **Created:** 2026-09-07 · **Covers:** `LoopZeroVeleroBackupStale`,
`LoopZeroVeleroBackupFailing`, `LoopZeroVeleroServerDown`, `LoopZeroVeleroBackupPartial`

## Why this file exists, and what it is not

A 2026-09-07 system-design audit found these four alerts defined, unapplied, and — once applied —
with no document saying what to do when they fire. Backup failure is the failure nobody notices by
hand: nothing degrades, no user complains, and the first symptom is a restore that cannot happen.

**This runbook is assembled from artifacts already in the repository** — the alert expressions, the
schedule definitions, and the DR drill script — **not from operational experience of running this
system.** Where a step needed judgement nobody has written down, it says so instead of inventing a
procedure. Correct it from a real incident the first time one happens; that version will be better
than this one.

## What is backed up

`infra/helm/velero/values-schedules.yaml` defines **5 schedules**, each `0 */6 * * *` (every 6h)
with `ttl: 720h` (30 days), wired at `infra/helmfile/core.helmfile.yaml.gotmpl:346`:

| Schedule | Scope |
|---|---|
| `loopzero-crds-backup` | CRDs cross-namespace, by resource type (no `includedNamespaces` — deliberate) |
| `loopzero-argocd-backup` | ArgoCD namespace |
| `loopzero-gitea-backup` | Gitea — the GitOps repos ArgoCD syncs from |
| *(see the values file for the remaining two)* | |

All alerts filter on `schedule=~"loopzero-.*"`, so they only fire for these.

## `LoopZeroVeleroServerDown` — `up{job="velero"} == 0` for 2m

Nothing is being backed up while this is firing. It is the one to act on first, because the other
three cannot fire meaningfully when the server is down.

```bash
kubectl --context k3d-loopzero-core -n velero-system get pods -l app.kubernetes.io/name=velero
kubectl --context k3d-loopzero-core -n velero-system logs -l app.kubernetes.io/name=velero --tail=100
```

Common causes visible from the manifests: the backup storage location is unreachable (credentials or
endpoint), or the pod is not scheduled. Check the location before restarting anything:

```bash
kubectl --context k3d-loopzero-core -n velero-system get backupstoragelocation -o wide
```

A location in `Unavailable` means the object store is the problem, not Velero.

## `LoopZeroVeleroBackupStale` — no successful backup for >12h

The schedules run every 6h, so 12h means at least two consecutive misses.

```bash
kubectl --context k3d-loopzero-core -n velero-system get schedules
kubectl --context k3d-loopzero-core -n velero-system get backups --sort-by=.metadata.creationTimestamp | tail -10
```

Distinguish the two cases before acting: **the schedule did not fire** (no new Backup objects) is a
scheduling or controller problem; **backups fired and failed** is `LoopZeroVeleroBackupFailing`, and
that alert should also be firing — if it is not, the failure mode is the schedule itself.

## `LoopZeroVeleroBackupFailing` — any failure in 6h

```bash
kubectl --context k3d-loopzero-core -n velero-system describe backup <name>
```

The `describe` output carries the phase and the per-resource errors. Read them before retrying: a
backup failing on the same resource every cycle is a different problem from one failing on a
transient store error.

## `LoopZeroVeleroBackupPartial` — completed with partial failures

**This is the quiet one.** The backup exists and reports success at the top level while some
resources were not captured, so a restore from it silently omits them. Treat it as a failed backup
until you know which resources were skipped:

```bash
kubectl --context k3d-loopzero-core -n velero-system describe backup <name> | grep -A20 "Errors"
```

## Proving a restore actually works

`infra/scripts/drill/velero-dr-drill.sh` performs a full backup-and-restore cycle and is the only
thing in this repository that proves the backups are restorable rather than merely present.

```bash
bash infra/scripts/drill/velero-dr-drill.sh --dry-run              # preflight, no mutation
bash infra/scripts/drill/velero-dr-drill.sh --context <ctx> --timeout <secs>
```

It creates its own backup and restore (`velero backup create` / `velero restore create` at lines 278
and 318) and emits per-phase results.

**Run it after resolving any of the four alerts.** An alert that stops firing proves the alert
stopped firing; the drill is what proves the thing the alert exists to protect.

## What this runbook does not tell you

- **Which of the 5 schedules matters most for a given outage.** That depends on what you are
  restoring toward, and no document in this repository ranks them.
- **The RTO/RPO these backups are meant to meet.** 6h cadence and 30-day retention are declared;
  the objective they were chosen against is not written down anywhere.
- **Whether a partial failure is tolerable.** That is a judgement about the specific resources
  skipped, and it needs someone who knows what depends on them.

## Related

- Schedules: `infra/helm/velero/values-schedules.yaml`
- Alerts: `infra/helm/monitoring/manifests/prometheusrule-velero.yaml`
- DR drill: `infra/scripts/drill/velero-dr-drill.sh`, exercised by `infra/tests/e2e/journey_velero_dr_test.go`
- Audit: `system-design-output/final_report.md` — `SD-01.13`, `SRE-05.1`
