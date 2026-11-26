# Runbooks

Procedures that recover, rotate, upgrade or drain something. Setup and daily workflow are
in [`operations.md`](operations.md).

**Every step here states whether it is automated or manual.** A runbook that describes a
retired automation as if it still runs is worse than no runbook.

The full long-form versions — including the verbatim ceremony scripts — are in git at
`4f728eb7a:docs/operations/` and `4f728eb7a:docs/runbooks/`. Recover one with
`git show 4f728eb7a:docs/operations/<name>.md`.

---

# Secrets and identity

## OpenBao init ceremony (manual, air-gapped)

Generates the Shamir shards and captures the AWS KMS unseal config. **5 shards, quorum 3.**

**Pre-flight — block the ceremony if any fail:** operator machine can be air-gapped;
YubiKey FIDO2 ready for shard 3; LUKS volume mounted at `/mnt/openbao-shard-4`; an offline
printer available for shard 5.

Confirm the current state before starting: with the secrets cluster context, check the
OpenBao pods, port-forward `svc/openbao 8200:8200`, extract the CA from the `openbao-tls`
Secret, and run `bao status`.

1. **Air-gap the machine.** Disconnect WiFi and ethernet — shard material must never
   traverse a network.
2. **Capture init output to a RAMdisk** (`/dev/shm`), never to disk.
3. **`bao operator init`** with 5 shards / quorum 3. With AWS KMS auto-unseal configured
   (production), the flags differ — read the long form.
4. **Parse shards and root token**, asserting exactly 5 shards; write each to
   `/dev/shm/shard-N.b64`, then `chmod 600`.
5. **Compute the shard hashes** into `/dev/shm/shard-hashes.txt` — this is the verification
   basis for step 7.
6. **Distribute the shards** per the custody model below.
7. **Verify before shredding.** Re-read every shard from its storage, hash it, and diff
   against the recorded hash. **Any mismatch aborts the ceremony and redoes distribution.**
   Then have the sponsor retrieve 1+3+5 and the backup person 2+4, confirming all five are
   intact, and only then destroy the RAMdisk copies.

### Custodian map (locations only — never shard content)

| Shard | Storage | Custodian | Access |
|---|---|---|---|
| 1 | 1Password corporate vault | sponsor + 1 backup | login + 2FA |
| 2 | 1Password personal vault | sponsor | master password + 2FA hardware key |
| 3 | YubiKey FIDO2 | sponsor | physical key + PIN |
| 4 | LUKS-encrypted USB, residence safe | sponsor | physical USB + passphrase |
| 5 | paper, tamper-evident envelope, bank safe deposit box | sponsor + bank | appointment + signature |

**Any 3 of the 5 unseal or restore.** No specific combination is required.

## Break-glass recovery (manual — operator + sponsor only)

Recovers OpenBao when the normal AWS KMS auto-unseal path is unavailable. Triggers: KMS key
destroyed, AWS account suspended or in billing hold, AWS unreachable for hours, or a lost
Raft cluster needing a snapshot restore without KMS.

**Phase 1 — decision gate (5 min).** Confirm the `VaultSealedOrDown` alert is firing;
confirm the KMS key state in the AWS console; confirm no other path exists, including simply
waiting; record the decision in `audit_log` with `event_type='vault.breakglass.initiated'`.

**Phase 2 — retrieve 3 shards (15-45 min).** Prefer three *different* storage tiers to
maximise recovery probability; 1 + 2 + 3 is achievable in under 15 minutes when everyone has
laptop access.

**Phase 3 — unseal (10 min).** **Phase 4 — document in `audit_log` (5 min).**
**Phase 5 — restore normal operations (30-60 min).** **Phase 6 — post-incident review within
7 days.**

**Refuse break-glass** when the normal path is merely slow, when fewer than 3 shards can be
assembled, or when the decision gate has not been recorded.

**A quarterly drill is mandatory**, with a broader annual DR exercise.

## Vault / OpenBao supporting procedures

| Procedure | Shape |
|---|---|
| Transit cutover | the longest procedure in the set (600+ lines). Read the long form before starting. |
| Multi-cluster Kubernetes auth | one auth backend per cell cluster |
| AWS resources | the KMS key, its `protect: true` guard, and the backup bucket |
| Local secrets cluster | `task k3d:secrets:create` + `k3d:secrets:bootstrap`. Installing OpenBao into core is a **topology violation**: the `k3d:secrets:bootstrap-in-core` task was removed on 2026-08-12 (B-047) and the `openbao-bootstrap-in-core-task` gate fails on its existence, or on the same effect under another name. |
| Staging secrets cluster | `task staging:platform` runs `infra/staging/openbao.sh`: OpenBao on the `secrets` EKS cluster, unsealed by the `awskms` seal, initialised with recovery keys after each rebuild (init file on the host, `/var/lib/loopzero-staging/openbao-init.json`, mode 0600). Why a dedicated cluster: `docs/decisions/2026-11-25-tenant-secrets-on-openbao.md`. |
| Secret migration | moving existing secrets under Transit |

## Service-auth secret rotation (manual, ~3 min, zero downtime)

Ed25519 keypair for the `loopzero-cloud` to engine proxy.

1. Generate a new keypair with `tools/gen-service-auth-ed25519`.
2. Publish the new **public** key alongside the current one — both are accepted.
3. Cut consumers over to the new **private** key.
4. **After a 24h soak**, drop the previous key.

Verify with a signed request before and after each step. Rollback is re-adding the previous
public key. **Anti-pattern:** dropping the old key in the same change that adds the new one —
that is a downtime window, not a rotation.

## Session token rotation (automated, with manual override)

The CLI's `ensureFreshToken` checks expiry before each call, refreshes through Hydra, and
persists the new token atomically. The API keeps a server-side cache.

Manual operator actions: force a refresh, or force-revoke **all** sessions for a user.

## Build cluster SA token rotation (manual)

**Symptom:** loopzero-api logs `argo step watcher: build cluster auth failed — rotate
loopzero-workflow-submitter token` — a 401/403 from the build cluster.

loopzero-api reaches the build cluster through `loopzero-cluster-kubeconfigs` (key
`build.kubeconfig`), with a token for `loopzero-workflow-submitter` in `argo`. Re-mint it, with
the cell-0 key, through the one minter those keys have:

```bash
CORE_CONTEXT=k3d-loopzero-core loopzero-ops seed cluster-kubeconfigs   # task k3d:heal runs the same
```

That is `kubectl -n argo create token loopzero-workflow-submitter --duration=720h`, rendered with
the build cluster's live CA. Do **NOT** mint `loopzero-build-sa` here: it is the workflow
executor, bound only to `workflowtaskresults`, so its token authenticates and is then
Forbidden to list workflows — a readable 401 traded for a silent 403. loopzero-api re-reads the
token on its next rejected call; no restart is needed.

**A `ClusterCredential` Secret** is a separate path. Symptom: a cached SA token stops working
*before* its `exp` claim — the SA UID changed because it was recreated during a rollout.

```bash
kubectl create token loopzero-system/default --duration=24h
# then patch the ClusterCredential Secret's data.kubeconfig
```

---

# Supply chain

## Cosign signing key rotation (manual, dual-sign window)

1. Generate the new keypair **offline**.
2. Store the new private key in an ESO Secret.
3. Add the new public key to the verification ConfigMap — **both keys now verify**.
4. **Dual-sign for 7 days.**
5. Cut over to the new key only.
6. Destroy the old private key.

A deadline check enforces the cutover so a dual-sign window cannot become permanent. Every
phase writes to the audit trail.

## Offline TUF root ceremony (manual, ~3h, 5 quorum members)

**Prerequisites:** 5 quorum members, per-member hardware, software staged on the air-gapped
machine, and a controlled physical environment.

**Phase 1 — preparation at T-7 days.** **Phase 2 — ceremony execution (~3h).**
**Phase 3 — Kubernetes setup (~30 min, operator only).** **Phase 4 — record the rotation
calendar.**

**Treat TUF root expiry as a scheduled outage**, because that is what it is.

## Keyed to keyless cutover

The M07 cutover sequence and its rollback runbook cover the move from keyed cosign to
keyless (Fulcio + Rekor). They were written as a pair — read the rollback before starting
the cutover.

---

# Platform lifecycle

## Kubernetes upgrade (DOKS) — manual, gated

**Pre-flight: a Pluto deprecated-API check must PASS before any upgrade.** Then run the
upgrade sequence. If Pluto warned and was ignored, or incidents follow, the rollback path is
in the long form.

## Cell drain (manual, SLO-bounded)

Moves every tenant off a cell so it can be retired.

1. Mark the cell as draining.
2. Inventory the tenants pinned to it.
3. Pin each tenant to the destination cell — `loopzero admin tenants pin` is the **only** path
   that moves a tenant; it updates the DB and the Tenant CR atomically and writes
   `audit_log`.
4. Monitor evacuation progress.
5. Confirm zero tenants remain on the drained cell.
6. Optionally transition to maintenance.
7. Retire when there is no plan to reactivate.

## Database migration safety and rollback

Migrations are **forward-only** (golang-migrate). A `DROP` requires explicit sign-off. There
is a PITR gate before migrating, a bounded RTO for rollback, and a verification log that
must be filled in.

## Multi-cell canary

Staged rollout with **halt criteria fixed in advance per stage** — availability SLI, error
budget consumption, P99 latency divergence between cells, and cross-contamination of the
incumbent cell. Stages run dev 1h, dev 24h, prod single tenant 7d, then prod 10% / 50% /
100%, each with a 7-day soak.

When a halt criterion is met, **halt** — the criteria exist precisely so the decision is not
made under pressure.

## Local public dashboard (manual, dev only)

The edge serves `{$LOOPZERO_DASHBOARD_DIST:../loopzero-cloud/dashboard/dist}` — that expression is the
`root *` of the `app-dev` blocks in `infra/local/Caddyfile`, and the `dashboard-root-parity` gate
fails this runbook if the two diverge.

It was an absolute path under one person's home until 2026-09-16, which made the file work on
exactly one machine and published that machine's directory layout to anyone who cloned. The
default is now relative to this repository, and `LOOPZERO_DASHBOARD_DIST` overrides it for a checkout
laid out differently.

**The obvious path misleads:** the dashboard source moved to the sibling repo `../loopzero-cloud` at M4,
so a `dashboard/` under this repository is stale if it exists at all. Build in the sibling, serve the
`dist` it produces.

## PV rebind, CRD conversion, ArgoCD Application identity migration

Three narrow one-off procedures. Read the long form from git before running any of them.

---

# Alert response

## Delivery path

`LoopZeroCFTunnelDisconnected` (2m) — the tunnel is down. Check the CF dashboard, then the
tunnel pod.

## Tenant correctness — quiet and expensive

`LoopZeroMisplacedTenant` (5m) · `LoopZeroCellPinnerCRPatchDiverged` (2m) ·
`LoopZeroTenantDeletionStuck` (5m) · `LoopZeroSBOMGenerationStalled` (5m) ·
`LoopZeroWorkqueueTerminalWriteFailed` (2m).

These do not page loudly and cost the most when ignored — a misplaced tenant means routing
and the database disagree about where a customer lives.

## Capacity and saturation

| Alert | Fires on | First look |
|---|---|---|
| `TenantReconcileBacklog` (5m) | `workqueue_depth{name=~"tenant\|environment\|application\|exposure"} > 50` | operator logs — a backlog is usually one slow reconcile blocking a queue, not global load |
| `BuildFailureRateHigh` (5m) | >10% of builds FAILED over 5m | recent Argo Workflow failures; check whether one step (clone, sign, push) dominates |
| `LoopZeroBuildQueueSlow` (5m) | `loopzero_build_queue_depth > 5` | `maxConcurrentBuildsPerCell` is 20 — a depth of 5 is early, not saturated |

## Sigstore storage

`LoopZeroSigstorePVUsageHigh` (>80% for 30m) · `LoopZeroSigstorePVUsageCritical` (>95% for 5m).

Rekor and its Trillian backend only ever grow: the transparency log is append-only and
nothing in this repository archives or prunes it. The one remedy is more volume —
`kubectl -n sigstore-system get pvc`, then raise `spec.resources.requests.storage` on the
claim the alert names. That works only when its StorageClass has
`allowVolumeExpansion: true`; otherwise the data has to move to a larger volume (§ PV
rebind). At 100% Rekor stops accepting entries and every signed build stalls.

## Velero backup alerts

`LoopZeroVeleroServerDown` (`up{job="velero"} == 0` for 2m) · `LoopZeroVeleroBackupStale` (no
successful backup for >12h) · `LoopZeroVeleroBackupFailing` (any failure in 6h) ·
`LoopZeroVeleroBackupPartial` (completed with partial failures).

**A backup that has never been restored is a hypothesis.** Prove a restore rather than
trusting the success metric.

## Platform PostgreSQL alerts

The platform's own PostgreSQL is the CloudNativePG cluster `platform-postgres` in
`loopzero-data-plane` on the core cluster. It holds the databases of the data-plane services.
Start every one of these with the cluster's own view:

```bash
kubectl -n loopzero-data-plane get cluster platform-postgres -o jsonpath='{range .status.conditions[*]}{.type}={.status} {.reason} {.message}{"\n"}{end}'
kubectl -n loopzero-data-plane get pods,backup -l cnpg.io/cluster=platform-postgres
```

- `LoopZeroPlatformPostgresDown`: an instance is not running or not scraped. Read the pod's
  events and the `postgres` container's log. With one instance there is no failover: the
  operator restarts it on the same volume.
- `LoopZeroPlatformPostgresWALArchiveFailing` and `LoopZeroPlatformPostgresWALArchiveBacklog`:
  WAL is not reaching the bucket. Read the `plugin-barman-cloud` container's log of the
  primary. The usual causes are the IAM role (the service account's
  `eks.amazonaws.com/role-arn`, and the role's trust of `loopzero-data-plane:platform-postgres`)
  and the bucket. Until it is fixed the WAL volume fills and recovery cannot go past the
  last archived segment.
- `LoopZeroPlatformPostgresBackupStale` and `LoopZeroPlatformPostgresBackupFailed`: read
  `.status.error` of the latest `Backup`. Ask for one by hand with a `Backup` of
  `method: plugin` once the cause is fixed. A cluster that only just had backups turned on
  fails its first one, asked for before the instance restarted with the plugin; the next
  one succeeds.
- `LoopZeroPlatformPostgresVolumeFilling`: raise `platformPostgres.storage` or `walStorage` in
  the environment and deploy; the operator expands the volume when the StorageClass allows
  it. A WAL volume fills when the archive is failing: fix that first.
- `LoopZeroPlatformPostgresReplicaLagging`: a standby is behind. Look for a long transaction
  on the standby or a slow volume; a standby that never catches up is recreated by deleting
  its pod and volume claims.
- `LoopZeroPlatformPostgresSyncReplicaMissing`: fewer standbys than the quorum requires, so
  commits wait. Bring the standby back. Lowering `synchronous.dataDurability` to `preferred`
  resumes writes and gives up the guarantee that no acknowledged commit is lost.
- `LoopZeroPlatformPostgresTransactionIDAgeHigh`: vacuum is not freezing rows. Look for a
  transaction open for days (`pg_stat_activity`), an abandoned replication slot
  (`pg_replication_slots`) or a prepared transaction, then run `VACUUM (FREEZE)` on the
  database.
- `LoopZeroPlatformPostgresConnectionsNearLimit`: read `pg_stat_activity` grouped by
  `usename` and `state` to find the service holding connections.

## VAP delete guardrail

Protects platform resources on the core cluster from deletion.

**The failure mode to watch for:** a `DELETE` policy that stops exempting the cluster
garbage collector **deadlocks namespaces in `Terminating`**. The exempt identities are every
`system:serviceaccount:kube-system:*` account (the namespace controller and the garbage
collector among them), `system:kube-controller-manager`, the `system:masters` group, and the
platform ServiceAccounts listed in `systemActorExemptions.platformServiceAccounts` of the
admission-policies values. All of it is rendered by one helper,
`loopzero-admission.systemActorExemptions`, used by every policy that matches UPDATE or DELETE.

Two more traps, both of which make a test pass for the wrong reason:

- **A CEL expression over `object.` in a `DELETE` rule.** `object` is not populated on
  `DELETE` as it is on CREATE/UPDATE. The expression raises an evaluation error, and under
  `failurePolicy: Fail` that becomes a **general block**. Use `oldObject`, or `request.*` to
  decide by identity.
- **An empty exemption list in a negated `matchCondition`.** `!(x in [])` is **always true**,
  so the policy runs for every identity and denies everything, operators included. A minimum
  floor of identities is a gate assertion, not a convention.

**Testing an admission policy with the default e2e kubeconfig proves nothing** — k3d's
kubeconfig is admin and a member of `system:masters`, which is exempt, so the test passes
*because the delete was allowed*. Use `--as=system:serviceaccount:<ns>:<sa>` with a
non-exempt SA **and grant it RBAC**: without RBAC the `Forbidden` comes from RBAC and says
nothing about the policy. The RBAC message reads `cannot delete resource ... at the cluster
scope`; the policy's message names the policy.

Rollback is a values toggle — no rebuild.

## Image-digest admission warnings

`LoopZeroImageDigestWarnRatePresent` fires when the `loopzero-require-image-digest` policy, still in
warn mode, saw a pod admitted with an image `:tag` instead of `@sha256:<digest>`.

1. Find the workload: `kubectl get events -A --field-selector reason=FailedAdmission` or the
   API-server audit log for `loopzero-require-image-digest`.
2. A platform chart, sidecar or operator-rendered manifest using a tag is a defect in that
   source — pin it by digest there.
3. Only when the rate has stayed at zero for the 7-day soak may
   `vaps.requireImageDigestEnforce` be set to `true`; with violations still present the flip
   denies those pods.

Rollback is the same values toggle.

## Security and vault audit alerts

Separate runbooks covered delivery-security alerts and OpenBao audit alerts. Both are in git
at the SHA above.

## Related

- [`operations.md`](operations.md) — setup, tests, gates, credentials
- [`architecture.md`](architecture.md) — what the clusters contain
