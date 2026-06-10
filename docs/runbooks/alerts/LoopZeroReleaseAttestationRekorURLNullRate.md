# Runbook: LoopZeroReleaseAttestationRekorURLNullRate

Alert from `prometheusrule-sigstore.yaml`. Fires when more than **1%**
of releases over 10 minutes land with `image_attestation_rekor_url`
NULL (instead of a real Rekor entry URL).

## What this means

This alert is the SLI for the LIVE 2026-05-28 **3-layer Rekor URL bug
class** (CR-5 + CR-9 of MEET-2026-05-28-001). The bug shipped because:

  1. Migration 000019 added the column, struct + writer populated it,
     but the canonical SELECT statements + scanner silently dropped it.
  2. Even after fixing (1), the cosign attest output extraction in
     `cosign_attest.go` emits an "unknown" sentinel when no Rekor index
     can be parsed — and the sentinel was being persisted as-is.

T2.1 made the column write-once at the DB boundary. T2.2 catches
struct↔SELECT drift statically. T2.3 covers the runtime extractor.
This alert closes the **observability** loop: if any of those layers
quietly regresses in production, this alert pages within 10 minutes.

## First triage (5 minutes)

1. **Confirm the rate via metric**:
   ```
   rate(loopzero_release_attestation_rekor_url_null_total{result="null"}[5m])
   /
   (rate(loopzero_release_attestation_rekor_url_null_total{result="null"}[5m])
    + rate(loopzero_release_attestation_rekor_url_null_total{result="populated"}[5m]))
   ```

2. **Cross-check with attest failures**:
   ```
   rate(loopzero_cosign_attest_total{result="failure"}[5m])
   ```
   - **High attest failure** + **high null rate** → Sigstore brownout
     (Rekor down). See `LoopZeroCosignSignErrorRate` runbook for
     component triage. The null releases are correctly failing safe.
   - **Low attest failure** + **high null rate** → the **3-layer bug
     has regressed**. The attest step succeeds (no error), but the URL
     is not landing in the DB. This is a code-path regression.

3. **Inspect a recent failed release row**:
   ```sql
   SELECT id, build_id, image_signature IS NOT NULL AS signed,
          image_attestation_rekor_url
   FROM releases
   WHERE created_at > now() - interval '10 minutes'
     AND image_attestation_rekor_url IS NULL
   ORDER BY created_at DESC LIMIT 5;
   ```
   If `signed=true` but `image_attestation_rekor_url IS NULL` for
   releases where the attest step succeeded, the runtime extractor
   regressed.

## Common causes

| Cause | Signal | Action |
|---|---|---|
| Attest step failing (legitimate) | high `loopzero_cosign_attest_errors_total` | Triage as Sigstore brownout per `LoopZeroCosignSignErrorRate` runbook |
| Extractor regression (3-layer bug returns) | attest_total{success} is high, but null rate also high | Run `cd api && go test ./internal/argo/ -run TestRekorExtract` — assert all 7 pass. If they pass in isolation, run the schema-mirror suite (T2.2): `cd api && DATABASE_URL=... go test ./internal/db/ -run TestSchemaMirror -count=1` |
| Cosign output format change | sudden onset after Sigstore image upgrade | Inspect `attest-image-*` workflow node output params; cross-check against `cosign_attest.go` sed pipeline |
| Persistence-layer skew | mirror test passes but write fails | Inspect API logs for `release_store.go` errors; check the write-once trigger (migration 000020) is still installed |

## Verification after fix

Once you believe the regression is fixed:

  1. Run the schema-mirror suite locally (T2.2).
  2. Run the failure-test suite (T2.3): `cd api && go test ./internal/services/ -run TestEnrich` and `./internal/argo/ -run TestRekorExtract`.
  3. Watch the alert metric over 15 minutes — should drop back to near-zero.

## Escalation

If null rate stays > 1% for 30 minutes WITHOUT a corresponding spike
in `loopzero_cosign_attest_errors_total`, page the platform on-call:
this is a code-path regression and needs an engineer.

## Related

- T2.1: migration 000020 write-once trigger
- T2.2: schema-mirror CI test
- T2.3: 7 failure tests (LIVE bug class coverage)
- T4.1: metric definition (this counter)
