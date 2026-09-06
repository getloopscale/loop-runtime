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
| `coverageReport` | `0 9 * * 1` | `task quality:coverage:report` | **no — no such target** | **retired**, and coverage is now measured on every push (see below) |
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

### `coverageReport` — retired, and the measurement it produced is now per-push

Decided 2026-09-06 by `B-179` between the three alternatives `B-147` names. The choice
is *explicit retirement*, and the reason is that coverage is already measured, more
often, and enforced.

`cmd/loopzero-ops/quality_registry.go:84` wires a quality gate named `test-coverage-report`.
It runs on every invocation of `task quality:gates` (`taskfiles/quality.yml:111`), which
`.claude/hooks/delivery-gate.sh` triggers before every `git push` on this machine and
which the post-merge `.github/workflows/ci.yml` invocation runs against `develop` and
`main`. The floor is 75% of production code and the current measurement is 85.33% in
about nine seconds (comment beside the wiring). So the measurement the weekly report was
going to produce is produced on every commit that leaves this repository, not once a
week, and a regression through the floor fails the push instead of appearing on Monday.

The on-demand form of the report remains: `task quality:test:coverage:report`
(`taskfiles/quality.yml:450`) generates the full profile from
`infra/tests/{charts,chaos,capacity,livetest}/...`, prints total coverage and leaves
`/tmp/loopzero-infra-tests-coverage.out` for an operator to inspect. Retiring the schedule
does not remove the report; it removes a scheduled email of a number that already fails
the push if it regresses.

The two alternatives were rejected on measurement, not preference:

- *in-cluster scheduler with a published runner* — needs a publication path for the
  `loopzero-ops` image, and this tree does not have one. `B-147` established that
  (`loopzero-ci-workflows` was retired for exactly this reason); nothing has changed since.
  This is not an editing decision.
- *external CI* — feasible in principle. `.github/workflows/` has no `schedule:` today
  and adding one is technically possible, but `.github/workflows/ci.yml` carries a
  written decision (2026-09-06 header) that these workflows are not this repository's
  quality gate, and that the gate is the local `delivery-gate.sh` running before every
  push. Adding a scheduled workflow whose purpose is to be a gate contradicts that
  decision. The drill cadences were deliberately taken out of GitHub Actions on
  2026-05-23 for the same reason.

What is lost by this decision: a weekly artefact separate from the per-push measurement,
in a form that would be emailed or archived. The per-push gate does not do that, and
nobody has asked for it since the schedule broke — verifiable because coverage has been
unavailable on any weekly cadence since the DOCR was removed and no ticket, comment or
CHANGELOG entry has requested it back. If someone does want the weekly artefact later,
the target still resolves and can be wired then; the decision recorded here is that no
one produces it as a scheduled job now.

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

## What keeps this file honest

`infra/tests/scripts/retired_periodic_jobs_test.go` asserts that every `task X`
this file names resolves to a real target. That is the specific defect
`coverageReport` carried for months: a command recorded in configuration that looked
active and could never have run.
