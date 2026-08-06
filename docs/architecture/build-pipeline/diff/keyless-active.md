---
title: "Cosign Keyless Active — Build Pipeline Diff"
generated: { by: process:plan-t1.5-t2.2-design, at: 2026-05-27 }
name: keyless-active
description: PROPOSED — cosign keyless signing active (Fulcio + Rekor provisioned via Pulumi). T2.2 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap.
type: decision
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: plan T1.5 + T2.2 design
---

<!-- PROPOSED STATE — depends on operator T2.2 HARD STOP -->

# Cosign Keyless Active — Build Pipeline Diff

> **Plan reference:** T2.2 + T1.5 of [`docs/plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md`](../../../plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md)
>
> **Depends on:** Sponsor sign-off em `.claude/governance-gate.local.md` for Fulcio + Rekor provisioning. ADR 2026-05-12 hard deadline T0+90d (2026-08-10).

## AS-IS (current keyed mode)

```
loopzero deploy ──► Argo Workflow ──► build (BuildKit)
                                      │
                                      ▼
                              cosign sign --key <STATIC_KEY>
                                      │
                                      ▼
                              policy-controller verifies signature
                                  via static public key in ClusterImagePolicy
```

Verification ConfigMap: `loopzero-system/loopzero-cosign-config` with `mode: keyed`, `public_key_active: <PEM>`.

## PROPOSED (post-T2.2)

```
loopzero deploy ──► Argo Workflow ──► build (BuildKit)
                                      │
                                      ▼
                              cosign sign --identity-token <SA_OIDC>
                                      │
                                      ├─► Fulcio (T2.2 NEW)
                                      │     issues 10min cert for SA identity
                                      │
                                      └─► Rekor (T2.2 NEW)
                                            appends transparency log entry
                                      │
                                      ▼
                              policy-controller verifies via:
                                  Fulcio root CA + Rekor log entry
                                  + SA identity matches expected
```

Verification ConfigMap: `loopzero-system/loopzero-cosign-config` with `mode: keyless`, `public_key_active: ""` (no static key needed).

## What changes

| Component | AS-IS | PROPOSED |
|-----------|-------|----------|
| Signing identity | Static keypair stored in ESO Secret | SA OIDC token + 10min Fulcio-issued cert |
| Trust root | Single public key in ConfigMap | Fulcio root CA (deployed via T2.2) + Rekor log |
| Rotation cadence | Operator-driven, ≥ 90d | Auto every ~10min (cert lifetime) |
| Signing key compromise blast radius | Until next manual rotation (≤ 90d) | Until next cert issuance (≤ 10min) |
| ClusterImagePolicy match | `keyRef: {kms: ...}` | `keylessRef: {issuer: <Fulcio URL>, subject: <SA identity pattern>}` |

## Operator actions (HARD STOP)

1. Sponsor approves T2.2 via Change Governor meeting (T3)
2. Operator implements Pulumi Fulcio + Rekor components per [`production-canary-stage5-runbook.md`](../../../operations/production-canary-stage5-runbook.md) § cosign keyless
3. Operator runs `pulumi up --stack drill` validating provisioning
4. Operator runs `pulumi up --stack prod` (drill GREEN gate)
5. Operator flips ConfigMap `mode: keyed` → `mode: keyless`
6. Operator monitors `loopzero_cosign_signing_total{mode="keyless"}` for 7 days
7. Operator deprecates static key path em ESO + ClusterImagePolicy

## Related

- [`docs/operations/cosign-signing-key-rotation-runbook.md`](../../../operations/cosign-signing-key-rotation-runbook.md) (T3.3 — keyed rotation + cutover-deadline script)
- `infra/scripts/security/check-cosign-cutover-deadline.sh` (deadline enforcement)
- ADR 2026-05-12 (cosign keyless cutover commitment)
- `[[cosign-key-rotation]]` (concept page)
