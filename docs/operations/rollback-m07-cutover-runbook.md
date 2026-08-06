---
type: operational-runbook
title: M07 Cutover Rollback Runbook
---

# M07 Cutover Rollback Runbook

T4.5 of plan
[`docs/plans/2026-05-28-meet-001-engineering-closure-plan.md`](../plans/2026-05-28-meet-001-engineering-closure-plan.md) (CR-10).
Companion to `infra/scripts/production/m07-cutover-rollback.sh`.

Audience: operators triaging a failed M07 cutover (cosign keyed →
keyless via Sigstore + Pulumi sigstore.ts). The runbook covers the 4
production failure modes called out by SRE + Validation Planner +
Deploy Guardian during MEET-2026-05-28-001. Time budget for the
in-flight rollback (steps 0a + 0b + 1 + 2) is **≤ 15 minutes**.

## When to invoke

Invoke this runbook when ANY of the following observe in production:

| Trigger | Source signal |
|---|---|
| Sustained `LoopZeroCosignSignErrorRate` > 5% | Prometheus alert / `loopzero_cosign_sign_errors_total{component="fulcio"}` spike |
| Sustained `LoopZeroReleaseAttestationRekorURLNullRate` > 10% | Prometheus alert / `loopzero_release_attestation_rekor_url_null_total{result="null"}` spike |
| Fulcio cert issuance > 30s P99 for > 30 min | `loopzero_fulcio_cert_issuance_duration_seconds` |
| Operator drill failed any of the 4 gates below | Drill stack output |

Do NOT invoke for transient (< 10 min) brownouts that recover on
their own — those are tracked via the alert clearing, not rollback.

## Roles + sign-off

| Step | Required sign-off |
|---|---|
| Step 0a–0b (drain + history) | On-call operator |
| Step 1–2 (App-level rollback) | On-call operator + 1 peer |
| Step 3 (Sigstore stack removal) | Platform Lead |
| Step 4 (migration rollback) | **Sponsor (CTO) — explicit pre-release governance waiver** |

## Step 0a — Drain in-flight workflows

**Why:** `helm uninstall sigstore-extras` mid-build leaks Rekor
entries (Rekor's transparency log is append-only). Drain workflow
queue first so subsequent steps run on a steady state.

```bash
KUBECONFIG=$(k3d kubeconfig write loopzero-build)  # LOCAL; prod uses operator kubeconfig
kubectl --kubeconfig="$KUBECONFIG" get workflows -n argo --no-headers \
  | awk '$2 == "Running" {print $1}' \
  | xargs -r -n1 argo --kubeconfig="$KUBECONFIG" terminate -n argo
```

Wait for the Workflow count to settle (≤ 60s).

## Step 0b — Discover the last-green revision

**Why (EC-8 of plan v1.1):** every rollback REQUIRES an explicit
discovery step before executing — operators must not memorize
"revision N − 1" because hot-patches between cutover and rollback can
shift the count.

```bash
argocd app history loopzero-api
argocd app history loopzero-build-cwts
```

Pick the LAST GREEN revision (i.e., the one BEFORE the cutover
commit). Note both: `LOOPZERO_API_REV` and `LOOPZERO_BUILD_CWTS_REV`.

## Step 1 — Roll back loopzero-api image

```bash
argocd app rollback loopzero-api "$LOOPZERO_API_REV"
argocd app wait loopzero-api --health --sync --timeout 300
```

**Abort trigger:** if `argocd app wait` times out, do NOT proceed.
The rollback is the LAST safe operation — anything after assumes a
healthy loopzero-api.

## Step 2 — Roll back loopzero-build-cwts (build pipeline workflow templates)

```bash
argocd app rollback loopzero-build-cwts "$LOOPZERO_BUILD_CWTS_REV"
argocd app wait loopzero-build-cwts --health --sync --timeout 300
```

After Step 2, **submit a probe build** to validate the chain:

```bash
loopzero deploy --tenant probe-tenant --project probe --env staging --wait
```

Probe build must reach `SUCCEEDED`. If not, escalate to Step 3.

## Step 3 — Sigstore stack removal + public Sigstore fallback

**Sign-off: Platform Lead.** This step removes the LOCAL/private
Sigstore stack and falls back to public sigstore.dev (Fulcio + Rekor).
After this step, signing latency may increase (public Sigstore is
geo-routed) but availability is higher.

```bash
helm uninstall sigstore-extras -n fulcio-system
helm uninstall fulcio -n fulcio-system
helm uninstall rekor -n rekor-system
helm uninstall ctlog -n ctlog-system

# Switch the Pulumi stack config back to public Sigstore
pulumi -s production config set loopzero:CosignFulcioURL https://fulcio.sigstore.dev
pulumi -s production config set loopzero:CosignRekorURL  https://rekor.sigstore.dev
pulumi -s production up --yes
```

Validate by re-running the Step 2 probe build.

## Step 4 — Migration rollback (LAST RESORT — sponsor sign-off required)

**Pre-release governance waiver required.** LoopZero migrations are
forward-only (CLAUDE.md). The triggers added by migration 000020
(T2.1) are write-once gates — they do NOT alter existing data. A
genuine rollback requires sponsor sign-off because it diverges from
the forward-only contract.

Only invoke if Step 3 did not restore service AND data corruption is
observed (e.g., `image_attestation_rekor_url` populated with an
attacker-controlled value mid-cutover — extremely unlikely given the
write-once trigger, but documented for completeness).

```bash
psql -c 'DROP TRIGGER releases_attestation_url_immutable ON releases;'
psql -c 'DROP TRIGGER releases_attestation_url_audit ON releases;'
psql -c 'DROP FUNCTION reject_attestation_url_mutation();'
psql -c 'DROP FUNCTION audit_attestation_url_set();'
```

Migration 000019's column (`image_attestation_rekor_url`) is nullable
and was added with no default — leaving it in place after Step 4 is
safe. Existing rows simply lose the write-once protection until a
future forward migration re-installs it.

## Abort triggers

If any of the following happen mid-rollback, **STOP and escalate to
the Platform Lead** (do not proceed to the next step):

- `argocd app rollback` returns an error other than timeout (e.g.,
  "revision not found" — the discovery step failed).
- `argocd app wait` times out for 5+ minutes even after retrying.
- A probe build hangs in `QUEUED` for > 5 minutes after rollback
  (build cluster may have been corrupted).
- Step 4 fails because the DROP statements report "trigger does not
  exist" — somebody else partially rolled back manually.

## LOCAL dry-run

Before any prod rollback, run the dry-run on a LOCAL k3d cleanroom
stack:

```bash
bash infra/scripts/production/m07-cutover-rollback.sh --dry-run \
  --target-rev "$LOOPZERO_API_REV" --build-rev "$LOOPZERO_BUILD_CWTS_REV"
```

The script will print every command it would execute without
mutating anything. The output is appended to
`.claude/knowledge-base/audits/2026-05-28-m07-rollback-local-dryrun-evidence.md`
for the audit trail.

## Post-rollback checklist

After rollback completes:

- [ ] `LoopZeroCosignSignErrorRate` drops below 1% within 15 min
- [ ] `LoopZeroReleaseAttestationRekorURLNullRate` drops within 15 min
- [ ] Probe build via `loopzero deploy` reaches `SUCCEEDED`
- [ ] `audit_log` contains `release.cutover_rollback` entry (see
      script's `--emit-audit` flag)
- [ ] Incident log entry: link this runbook + the revisions used +
      the time budget consumed (for SLO accountability)

## Related

- T2.1: migration 000020 write-once trigger (the thing we'd undo at Step 4)
- T4.1: metrics that signal whether rollback is needed
- T4.2: alerts that page on the rollback-trigger conditions
- T4.4: `loopzero admin release inspect` to verify rollback worked per release
- ADR `2026-05-27-T2.2-offline-tuf-ceremony.md`: production Sigstore provisioning
- ADR `2026-05-28-cosign-v2.4.3-deadline.md`: cosign client version deadline
