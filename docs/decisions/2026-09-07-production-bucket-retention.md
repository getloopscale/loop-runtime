# Declare bundle retention on the production object store, in the infrastructure that creates it

- **Status:** PROPOSED
- **Date:** 2026-09-07
- **Tier:** T2 (`pre-release-governance.md` lists Pulumi infrastructure changes as an unconditional trigger)
- **Source:** infrastructure system-design audit, finding SD-15.8

## Context

The audit first reported that source bundles were never expired. That was **false**, and the
refutation matters for reading this ADR: `infra/helm/minio/lifecycle-job.yaml` runs
`mc ilm rule add local/loopzero-builds --expire-days 30`, wired at `taskfiles/infra.yml:129-130`.

What survived the correction is narrower. That rule is applied to the **in-cluster MinIO**, by a
Taskfile `kubectl apply`. Production object storage is DigitalOcean Spaces, created at
`infra/pulumi-infra/src/providers/digitalocean/storage.ts`, and that bucket declared **versioning
and no lifecycle rule at all**. Production retention therefore depended on someone running a task
against a cluster that is not where the production objects live.

The builds bucket holds one bundle per commit at up to 500 MB (`SOURCE_MAX_BUNDLE_SIZE`). It is the
only store that grows with customer activity.

## Decision

`ObjectStorageSpec` gains an optional `expireAfterDays`. The DigitalOcean provider translates it
into a `lifecycleRules` expiration on the `SpacesBucket`. The **builds** bucket declares
`expireAfterDays: 30`, matching the in-cluster ILM rule so the two environments agree.

The **artifacts** bucket is deliberately left unexpired: it is the versioned bucket and holds things
a rollback may still need. A test asserts that absence so it stays a decision rather than an
oversight.

## Who is affected

- **This repository** — `infra/pulumi-infra` (contract, DO provider, `index.ts`, `components/infra.ts`).
- **`../loopzero-infra-modules` / `../loopzero-infra-live`** — the AWS re-platform targets. They are
  unaffected today: `infra/pulumi-infra/src/providers/aws/index.ts:43-45` throws `notImplemented`
  for `createDatabaseCluster`, and the AWS storage path is not the one in use. **When AWS storage is
  implemented it must carry the same expiry**, or the retention regresses silently at cutover.
- **Nobody outside this repo consumes `ObjectStorageSpec`** — it is an internal contract of the
  Pulumi program.

## What would break, and what would not

- **Additive.** `expireAfterDays` is optional; every existing call site that omits it renders
  exactly the bucket it rendered before. `tsc --noEmit` is clean.
- **What changes on the next `pulumi up`:** the builds bucket gains a lifecycle rule. Objects older
  than 30 days in that bucket become eligible for deletion by the provider.
- **The real risk, stated plainly:** if any rollback path reads a bundle older than 30 days, this
  deletes it. The in-cluster store has expired bundles at 30 days for as long as that ILM job has
  run, so the 30-day window is already the de-facto contract — this makes production match it rather
  than introducing it. If a longer window is needed, the number is the thing to change, in one place.

## Alternatives rejected

1. **Apply an ILM rule to Spaces from a Taskfile**, mirroring the MinIO approach. Rejected: it
   reproduces the defect the audit found — retention that exists only where someone runs a command,
   and nothing reconciles.
2. **Leave production unexpired and rely on the in-cluster rule.** Rejected: the in-cluster rule does
   not touch production objects. This is the status quo and it is the finding.
3. **A cold tier instead of expiry.** Rejected for now: DO Spaces offers no automatic tiering, so it
   would be an explicit transition rule with the same wiring question and more moving parts. Worth
   revisiting only if a retention longer than 30 days is decided.

## Verification

- `infra/pulumi-infra/src/__tests__/storage-lifecycle.test.ts` — 4 assertions, written RED first:
  the contract can express an expiry, the provider translates it, the builds bucket declares it, and
  the artifacts bucket does not.
- `npx tsc --noEmit` clean.

## Note on how this was authorised

`infra/pulumi-infra/` is **not** in `PRODUCTION_PATHS` of `.claude/hooks/governance-gate.py`, which
lists `infra/pulumi/` (the legacy tree). The gate therefore did not block this edit. That is a gap
in the gate rather than permission: the governance policy treats Pulumi changes as T2 regardless of
which directory they live in.

This ADR is what satisfies the tier, per `.claude/rules/autonomy-envelope.md` — "a structural
decision the contract wants recorded: write the ADR, and proceed on it", carrying who is affected,
what would break, and what was rejected.

**Recommended follow-up:** add `infra/pulumi-infra/` and `infra/pulumi-platform/` to
`PRODUCTION_PATHS`, so the gate covers what the policy already covers.
