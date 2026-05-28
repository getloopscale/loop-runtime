# M07 Production Cutover Sequence

> **Authoritative cutover ordering for M07 Tenant Build Pipeline (cosign keyless + Sigstore + Rekor URL persistence) production deployment.**
>
> Source: T0.3 of plan `2026-05-28-meet-001-engineering-closure-plan.md` v1.1.
> Mandates: MEET-2026-05-28-001 mandatory action #23 (CR-11 fix).
>
> **Engineering side of MEET-001 must be GREEN before this sequence is executable** (15 actions per Plan A). Operator HARD STOPs in the sequence below are gated on their respective actions completing.

## Critical ordering invariant

**Migration MUST be applied BEFORE API redeploy.** Reason: `api/internal/db/release_store.go` INSERT unconditionally references `image_attestation_rekor_url` (migration 000019 column). If API rolls out first, every INSERT fails with `column does not exist`. This is BLOCKER CR-11 from MEET-2026-05-28-001.

## Pre-flight gate

Run `infra/scripts/production/m07-cutover-preflight.sh` BEFORE starting the sequence. The script validates 5 pre-conditions:

```bash
bash infra/scripts/production/m07-cutover-preflight.sh \
  --target-db="$DOKS_PG_CONN_STRING" \
  --target-env=prod \
  --api-tag="$LOOPZERO_API_CUTOVER_TAG" \
  --cosign-tag=v2.4.3-r0 \
  --chart-version=0.3.0
```

Exit 0 = safe to proceed. Exit 1 = ABORT, fix gaps, re-run preflight.

Pre-conditions checked:
1. Migration 000019 + 000020 applied to target DB (`schema_migrations.version >= 20`)
2. DOCR image `loopzero-cosign-signer:v2.4.3-r0` published
3. DOCR image `loopzero-api:<cutover-tag>` published
4. Chart `loopzero-build-cwts:0.3.0` published to OCI registry
5. TUF root present in Pulumi stack output (operator ceremony complete)

## Cutover sequence (7 steps)

### Step 1 — Apply DB migrations (operator + engineering)

**Pre-condition:** preflight gate PASSED.

```bash
bash infra/scripts/production/run-migrations.sh --target=$DOKS_PG_CONN_STRING --version=20
```

**Abort trigger:** `dirty=true` after apply OR `version != 20` post-apply. Run rollback runbook Step 4 (last resort).

**Validate:**
```bash
psql -c "SELECT version, dirty FROM schema_migrations ORDER BY version DESC LIMIT 1" $DOKS_PG_CONN_STRING
# Expect: version=20, dirty=f
```

### Step 2 — Publish OCI charts (engineering)

**Pre-condition:** Step 1 GREEN.

```bash
helm package charts/loopzero-build-cwts/ --version 0.3.0
helm push loopzero-build-cwts-0.3.0.tgz oci://${DOCR_REGISTRY}
```

**Abort trigger:** OCI push fails OR `helm show chart oci://${DOCR_REGISTRY}/loopzero-build-cwts --version 0.3.0` returns wrong version.

### Step 3 — Publish DOCR images (engineering)

**Pre-condition:** Step 2 GREEN.

```bash
# loopzero-cosign-signer with cosign v2.4.3
docker build -t ${DOCR_REGISTRY}/loopzero-cosign-signer:v2.4.3-r0 infra/images/cosign-signer/
docker push ${DOCR_REGISTRY}/loopzero-cosign-signer:v2.4.3-r0

# loopzero-api with M07 fixes
docker build -t ${DOCR_REGISTRY}/loopzero-api:$LOOPZERO_API_CUTOVER_TAG -f api/Dockerfile .
docker push ${DOCR_REGISTRY}/loopzero-api:$LOOPZERO_API_CUTOVER_TAG
```

**Abort trigger:** push fails OR image scan (Trivy CRITICAL/HIGH) reports issues.

### Step 4 — TUF ceremony + Pulumi sigstore.ts (OPERATOR HARD STOP)

**Pre-condition:** Step 3 GREEN. Engineering side of MEET-001 plan complete.

Per ADR-2026-05-27-T2.2-OFFLINE-TUF, operator MUST:
1. Perform offline TUF ceremony with key custody officer
2. Capture ceremony output as Pulumi stack config:
   ```bash
   pulumi config set --secret loopzero:tufPreMintedKeys "$(cat ceremony-output.json)"
   pulumi config set loopzero:fulcioHostname fulcio.loopzero.example
   pulumi config set loopzero:rekorHostname rekor.loopzero.example
   pulumi config set loopzero:oidcIssuer https://kubernetes.default.svc.cluster.local
   pulumi config set loopzero:rekorWitness https://rekor.sigstore.dev
   ```
3. Pulumi up:
   ```bash
   cd infra/pulumi-platform/
   pulumi up --stack prod --yes
   ```
4. Validate: `kubectl get pods -n fulcio-system -A | grep Running`

**Abort trigger:** Pulumi preview reveals unexpected drift OR ceremony output incomplete. **Do NOT proceed to Step 5 without all 4 sub-steps GREEN.**

### Step 5 — ArgoCD sync new chart + images (operator)

**Pre-condition:** Step 4 GREEN (Sigstore stack Running).

```bash
argocd app sync loopzero-build-cwts --revision $TARGET_REV
argocd app wait loopzero-build-cwts --health
argocd app sync loopzero-api --revision $TARGET_REV
argocd app wait loopzero-api --health
```

**Abort trigger:** ArgoCD app Degraded for >5min. Execute rollback per `docs/operations/rollback-m07-cutover-runbook.md`.

### Step 6 — Validate first build (engineering + operator)

**Pre-condition:** Step 5 GREEN.

Trigger test build from prod CLI:
```bash
$LOOPZERO deploy --env production --json
```

**Acceptance:**
- Workflow Phase=Succeeded
- DB release row populated: `image_signature` + `image_signed_at` + `image_attestation_rekor_url`
- `loopzero_release_attestation_rekor_url_null_total{result="populated"}` increments
- Zero CRITICAL alerts fire

**Abort trigger:** any assertion above fails. Execute rollback runbook.

### Step 7 — Flip admission policy to enforce (OPERATOR HARD STOP, +7d)

**Pre-condition:** Step 6 GREEN for ≥7d sustained (dual-signing GREEN streak).

```bash
helm upgrade admission-policies infra/helm/admission-policies/ \
  --set clusterImagePolicyKeyless.mode=enforce
```

**Abort trigger:** ANY build failure during the 7d window OR `loopzero_release_attestation_rekor_url_null_total{result="null"}` rate >1%.

## Pre-cutover gates (engineering side — must complete BEFORE Step 1)

These 15 engineering actions of Plan A (`2026-05-28-meet-001-engineering-closure-plan.md`) MUST be complete + LIVE-validated:

- T0.1 ADR-A LOCAL exception ✅
- T0.2 ADR-B cosign v2.4.3 deadline ✅
- T0.3 cutover ordering codified (this doc + preflight)
- T1.1 step-name SoT helpers
- T1.2 `--insecure-skip-verify` env gate
- T1.3 workflow_builder legacy branch
- T1.4 false-parity comment removed
- T1.5 anonymous-auth quality gate
- T2.1 migration 000020 (write-once trigger)
- T2.2 schema-mirror test
- T2.3 7 failure tests
- T3.1 NetworkPolicy podSelector hardening
- T4.1 8 metrics
- T4.2 3 alerts
- T4.3 Grafana dashboard
- T4.4 `loopzero admin release inspect` CLI
- T4.5 rollback runbook authored + LOCAL dry-run
- T4.6 Rekor retention documented

## Abort + rollback

If any step fails, execute `docs/operations/rollback-m07-cutover-runbook.md` (T4.5).

Time budgets (per SRE veto release condition):
- Steps 1-3 + 5: ≤ 15min rollback
- Steps 4 + 7: longer; coordinated operator workstream

## Related

- Plan: `docs/plans/2026-05-28-meet-001-engineering-closure-plan.md` v1.1
- Meeting: MEET-2026-05-28-001 (cutover REJECTED + 24 mandatory actions)
- Rollback: `docs/operations/rollback-m07-cutover-runbook.md` (T4.5, not yet authored)
- Preflight script: `infra/scripts/production/m07-cutover-preflight.sh`
