# loopzero-cosign-signer: Alpine image with cosign binary baked in.
# Used by the sign-image Argo Workflow step to sign user-built container images.
#
# SEC-006: cosign binary baked at build time with checksum verification.
# No runtime downloads — eliminates supply chain risk in the signing pipeline.
#
# Size: ~50MB (Alpine + cosign binary)
# Security: runs as non-root user 1000, no privilege escalation
FROM alpine:3.21.3

LABEL org.opencontainers.image.source="https://github.com/loopzero/loopzero"
LABEL org.opencontainers.image.title="loopzero-cosign-signer"
LABEL org.opencontainers.image.description="Cosign image signer for LoopZero build pipeline"

# cosign version and checksum — update both when upgrading.
# Get checksum: wget -q <url> -O cosign && sha256sum cosign
ARG COSIGN_VERSION=v3.0.6
ARG COSIGN_SHA256=c956e5dfcac53d52bcf058360d579472f0c1d2d9b69f55209e256fe7783f4c74

RUN apk add --no-cache ca-certificates \
    && wget -q "https://github.com/sigstore/cosign/releases/download/${COSIGN_VERSION}/cosign-linux-amd64" \
       -O /usr/local/bin/cosign \
    && echo "${COSIGN_SHA256}  /usr/local/bin/cosign" | sha256sum -c - \
    && chmod +x /usr/local/bin/cosign \
    && adduser -D -u 1000 loopzero

USER 1000
ENTRYPOINT ["/bin/sh"]
