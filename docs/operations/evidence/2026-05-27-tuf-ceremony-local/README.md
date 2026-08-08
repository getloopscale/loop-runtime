---
type: evidence
title: "TUF Ceremony LOCAL Evidence — 2026-05-27"
---

# TUF Ceremony LOCAL Evidence — 2026-05-27

**Per ADR D5 do plan [`2026-05-27-M07-tenant-build-pipeline-to-3-85-cap-plan.md`](../../../plans/2026-05-27-M07-tenant-build-pipeline-to-3-85-cap-plan.md) v1.1**

LOCAL variant of TUF root signing ceremony — simulated quorum via openssl (NOT prod-grade YubiKey FIPS air-gapped). Generated during M2 Phase 6 execution.

## Content

- `root.json` (9514 bytes) — TUF root metadata, signed 3-of-5 quorum (parties 1, 2, 3)
- `party-{1..5}-pub.pem` (800 bytes each) — RSA-4096 public keys only (private keys NEVER committed; live em `/tmp/tuf-ceremony-local-2026-05-27/keys/` LOCAL only)

## Signatures verified

```bash
$ openssl dgst -sha256 -verify party-1-pub.pem -signature /tmp/sig1.bin /tmp/signed.bin
Verified OK
```

3 signatures total em `root.json` (parties 1, 2, 3 = 3-of-5 quorum satisfies root role threshold=3).

## K8s Secret created

```bash
$ kubectl --context k3d-loopzero-build get secret tuf-root-keys -n sigstore-system
NAME            TYPE     DATA   AGE
tuf-root-keys   Opaque   6      <recent>
```

6 keys: root.json + 5 party pubkeys.

## Local-only disclaimer (per ADR D5)

This ceremony is for **LOCAL validation only**. Prod ceremony requires:
- YubiKey FIPS hardware (not openssl)
- Air-gapped machine (full disk encryption, never networked)
- Multi-party in physical room (3-of-5 quorum quorum-verified)
- Per [`docs/operations/tuf-root-ceremony.md`](../../tuf-root-ceremony.md) 7-section procedure

This LOCAL ceremony validates the **chain of trust mechanism** (TUF root → K8s Secret → Pulumi component consumes → Sigstore stack boots) without prod-grade key management. Prod ceremony is documented but NOT executed locally.

## What this evidence supports

- M2 Phase 6 of plan derivado completed engineering-side
- K8s Secret `tuf-root-keys` exists em `sigstore-system` namespace (loopzero-build cluster)
- 3-of-5 quorum demonstrably satisfied (cryptographic signatures verifiable)
- Pulumi Sigstore component CAN consume this Secret reference (Phase 7 pre-requisite met)

## What this evidence does NOT support

- Prod activation (D1 ADR — DNS Local-only)
- Real-world chain of trust (LOCAL openssl keys ≠ FIPS-hardware-backed)
- Compliance audit acceptance (auditors require ceremony chain-of-custody video + multi-party affidavits)
