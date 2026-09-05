---
type: operational-runbook
title: "Retired periodic jobs — what used to be scheduled, and who runs it now"
---

# Retired periodic jobs — what used to be scheduled, and who runs it now

> Registered by `B-147`. The chart `loopzero-ci-workflows` was retired on 2026-08-18
> (`B-019`) because every surface in it ran
> `registry.digitalocean.com/loopzero-system/loopzero-ops`, an image with no publication path in
> this tree. Four scheduled jobs went with it.
>
> They had already stopped working before the chart was deleted: the image had been
> absent from every registry since the DOCR was removed, so each firing produced a
> guaranteed red pod. Retiring the chart removed no capability. It made an absence
> visible that configuration had been dressing up as coverage.
>
> This file exists so that the absence stays visible. A job with no executor is
> recorded here as having no executor; it does not quietly cease to be anybody's
> problem.

## The four jobs

Re-measured 2026-09-05 in a clean worktree of `develop`.

| job | former cadence | command, as the retired chart declared it | resolves today? | executor now |
|---|---|---|---|---|
| `drillWatchdog` | `0 */6 * * *` | `task drill:watchdog` | yes | **operator**, while a drill stack is active |
| `weeklySoak` | `0 2 * * 0` | `task drill:l2-scale` | yes | **operator** |
| `coverageReport` | `0 9 * * 1` | `task quality:coverage:report` | **no — no such target** | **none — open** |
| `cosignDeadline` | `0 0 * * *` | `bash infra/scripts/security/check-cosign-cutover-deadline.sh` | script exists | **none — blocked** |

### `drillWatchdog` — operator, and the cron was the wrong shape

`task drill:watchdog` resolves. It is a safety net that destroys expired drill stacks,
and it only has anything to destroy while a drill stack is up. It needs
`DIGITALOCEAN_TOKEN`, `PULUMI_ACCESS_TOKEN` and `PULUMI_CONFIG_PASSPHRASE`
(`taskfiles/drill.yml`), and it destroys cloud infrastructure.

A fixed six-hourly cron therefore fired mostly against nothing, and held
stack-destroying credentials in order to do it. `taskfiles/drill.yml` has declared the
replacement since the GitHub Actions cadences were removed on 2026-05-23: the operator
runs it periodically while a stack is active. That declaration stands; this row records
that the cron is not coming back.

### `weeklySoak` — operator, and automating it is not an editing decision

`task drill:l2-scale` resolves. It provisions four DOKS clusters, runs a 50-concurrent
deploy test and tears the stack down: roughly 65 minutes and ~$0.35 of real spend per
run, behind a mandatory `drill:preflight` gate that requires four separate credentials.

Scheduling that unattended needs those credentials in a runner and an owner for the
spend. That is access and money, not a choice between alternatives, so it is not
delegated. `taskfiles/drill.yml` already records the cadence as an operator
responsibility. Note that the retired chart ran it weekly (`0 2 * * 0`) while
`taskfiles/drill.yml` describes the cadence it replaced as bi-weekly; the two never
agreed, and the operator cadence is the one that survived.

### `coverageReport` — the recorded command never existed

This job was broken twice over, and only one of the two reasons was the missing image.

The chart declared `task quality:coverage:report`. There is no such target. The target
that exists is **`quality:test:coverage:report`** (`taskfiles/quality.yml`), confirmed
by `task --list-all` on 2026-09-05. Rescheduling the job exactly as it was recorded
would fail even with a published runner.

It is also the only one of the four that could run unattended without credentials: the
target is hermetic and needs no live cluster. Where it should run is still open — see
"What is still open" below.

### `cosignDeadline` — blocked, and not on a scheduler

The script exists and is not the problem. Its input is.

`B-146` established that the ConfigMap it reads, `loopzero-system/loopzero-cosign-config`, has
never existed, and the decision recorded there is that it stays absent: provisioning it
makes the guard block immediately, because the block condition (deadline passed and
cosign still keyed) is already true. Whether to do the keyed→keyless cutover or amend
the deadline belongs to the cutover owner and is tracked as issue #348.

So this job has no executor and will not get one from a scheduling decision.
Re-measured 2026-09-05 against `k3d-loopzero-core`: `loopzero-cosign-config` is still
`NotFound`, and the guard exits **2** — the loud missing-input failure `B-146` intended.
Scheduling it today would produce a daily red run whose only content is that a held
decision is still held. It is recorded here as blocked rather than scheduled.

## What is still open

**Where `coverageReport` runs.** The three alternatives `B-147` names resolve like this:

- *in-cluster scheduler with a published runner* — needs a publication path for the
  `loopzero-ops` image, which this tree does not have. Not an editing decision.
- *external CI* — feasible in principle: the target is hermetic and
  `.github/workflows/` has no `schedule:` trigger today, so adding one is possible. But
  `.github/workflows/ci.yml` carries a written decision that these workflows are not
  this repository's quality gate, and the drill cadences were deliberately taken out of
  GitHub Actions on 2026-05-23. Putting a scheduled job back in contradicts both.
- *explicit retirement* — drops a coverage report that nothing else produces on a
  cadence.

None of the three is a free edit, so the choice is left to the sponsor rather than taken
as a side effect of this pass.

## What keeps this file honest

`infra/tests/scripts/retired_periodic_jobs_test.go` asserts that every `task X`
this file names resolves to a real target. That is the specific defect
`coverageReport` carried for months: a command recorded in configuration that looked
active and could never have run.
