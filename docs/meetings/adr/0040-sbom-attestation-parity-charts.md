# ADR-0040 — SBOM attestation parity with container images Sprint 6

## Status

Accepted (async, pending human ratification) — 2026-05-05 (MEET-2026-05-05-CHARTS-SOTA-ASYNC-AMENDMENT)

## Context

Sprint 6 (CHG-2026-04-26-SPRINT6-IMPL) shipped cosign image signing + syft SBOM (CycloneDX) + policy-controller deny-by-default. Container images in LoopZero's pipeline carry:
- cosign keyless signature (Fulcio + Rekor)
- syft-generated CycloneDX SBOM
- cosign attestation (`--predicate sbom.json --type cyclonedx`)

Charts published to OCI registry today carry:
- cosign keyless + key-based dual-signing (operacional, MEET-063 condition #2)
- ❌ NO SBOM
- ❌ NO attestation

Asymmetric supply chain — charts são metadata "envelopes" of container images, but charts themselves don't have provenance.

Bitnami Secure Images publica SBOM in-toto + provenance attestation per chart. Sigstore project does the same.

## Decision

Each chart published to DOCR (`registry.digitalocean.com/loopzero-system/helm`) receives:

1. **SBOM**: `syft <chart>.tgz -o cyclonedx-json > sbom-<chart>.json`
2. **Attestation**: `cosign attest --predicate sbom-<chart>.json --type cyclonedx <REF>`
3. **Verification step**: `cosign verify-attestation --type cyclonedx <REF>` runs in CI post-sign, closes loop.
4. **Policy**: `charts/loopzero-admission-policies/templates/cluster-image-policy.yaml` (Sprint 9) optionally requires attestation for tenant-namespace deploys.

Implementation in Phase 5 (T5.1-T5.4) of charts-sota plan.

## Alternatives Considered

1. **Skip SBOM until post-launch.** REJECTED by ABSOLUTE platform-security-engineer veto: charts assinados sem SBOM = supply chain incompleto pre-GA.
2. **Use SPDX format.** REJECTED: CycloneDX is Sprint 6 standard; mantém paridade with images; tooling alignment.
3. **Sign + attest at install time (admission webhook regenerates).** REJECTED: defeats purpose of pre-published provenance; admission can verify only what was signed at publish.

## Conditions

- **CONDITION-1 (platform-security-engineer):** SBOM attestation (this ADR) MUST be implemented before GA. Não pode ficar para Post-Launch.

## Consequences

- **+** Charts ganham paridade supply chain com images.
- **+** CH-G19 NEW gate MET.
- **+** Habilita supply-chain validation downstream via `cosign verify-attestation`.
- **+** Sprint 9 admission policies podem requerer attestation.
- **−** ~30s adicional per chart in CI publish step.
- **−** syft binary (~50MB) instalado em CI runner (já é dep transitiva do Sprint 6 image pipeline).

## Related

- Plan: Phase 5 (T5.1-T5.4)
- Sprint 6 reference: CHG-2026-04-26-SPRINT6-IMPL
- Sigstore docs: https://docs.sigstore.dev/cosign/signing/other_types/
- Bitnami Secure Images: https://techdocs.broadcom.com/us/en/vmware-tanzu/bitnami-secure-images/
