# loopzero-cosign-signer: Alpine image with cosign binary baked in.
# Used by the sign-image Argo Workflow step to sign user-built container images.
#
# SEC-006: cosign binary baked at build time with checksum verification.
# No runtime downloads — eliminates supply chain risk in the signing pipeline.
#
# Size: ~50MB (Alpine + cosign binary)
# Security: runs as non-root user 1000, no privilege escalation
FROM mirror.gcr.io/library/alpine:3.21.3

LABEL org.opencontainers.image.source="https://github.com/getloopscale/loopzero"
LABEL org.opencontainers.image.title="loopzero-cosign-signer"
LABEL org.opencontainers.image.description="Cosign image signer for LoopZero build pipeline"

# cosign version and checksum — update both when upgrading.
# Get checksum: wget -q <url> -O cosign && sha256sum cosign
#
# 2026-05-28 Ralph iter 3: downgraded v3.0.6 → v2.6.3 because cosign v3
# does client-side token validation rejection after Fulcio GetConfiguration,
# without ever hitting CreateSigningCertificate. v2.6.3 uses simpler API:
# SIGSTORE_ID_TOKEN env + SIGSTORE_FULCIO_URL → POST /api/v2/signingCert.
# See: docs/plans/2026-05-28-M07-fulcio-cacert-keyless-LIVE-plan.md v1.2
ARG COSIGN_VERSION=v2.4.3
ARG COSIGN_SHA256=caaad125acef1cb81d58dcdc454a1e429d09a750d1e9e2b3ed1aed8964454708

RUN apk add --no-cache ca-certificates \
    && wget -q "https://github.com/sigstore/cosign/releases/download/${COSIGN_VERSION}/cosign-linux-amd64" \
       -O /usr/local/bin/cosign \
    && echo "${COSIGN_SHA256}  /usr/local/bin/cosign" | sha256sum -c - \
    && chmod +x /usr/local/bin/cosign \
    && adduser -D -u 1000 loopzero

USER 1000
ENTRYPOINT ["/bin/sh"]
