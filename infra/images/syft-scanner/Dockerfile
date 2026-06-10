# loopzero-syft-scanner: Alpine image with syft binary baked in.
# Used by the sbom-{app} Argo Workflow step (Sprint 6 / T1.1) to scan
# signed container images and produce CycloneDX SBOMs.
#
# SEC-006: syft binary baked at build time with checksum verification.
# No runtime downloads — eliminates supply chain risk in the SBOM step.
#
# Output: CycloneDX JSON written to /tmp (caller uploads to S3).
# Size: ~50MB (Alpine + syft binary + aws-cli)
# Security: runs as non-root user 1000, no privilege escalation
FROM mirror.gcr.io/library/alpine:3.21.3

LABEL org.opencontainers.image.source="https://github.com/loopzero/loopzero"
LABEL org.opencontainers.image.title="loopzero-syft-scanner"
LABEL org.opencontainers.image.description="syft SBOM scanner for LoopZero build pipeline (Sprint 6)"

# syft version and checksum — update both when upgrading.
# Checksum source:
#   https://github.com/anchore/syft/releases/download/${SYFT_VERSION}/syft_${SYFT_VERSION_BARE}_checksums.txt
ARG SYFT_VERSION=v1.18.1
ARG SYFT_VERSION_BARE=1.18.1
ARG SYFT_SHA256=066c251652221e4d44fcc4d115ce3df33a91769da38c830a8533199db2f65aab

RUN apk add --no-cache ca-certificates aws-cli \
    && wget -q "https://github.com/anchore/syft/releases/download/${SYFT_VERSION}/syft_${SYFT_VERSION_BARE}_linux_amd64.tar.gz" \
       -O /tmp/syft.tar.gz \
    && echo "${SYFT_SHA256}  /tmp/syft.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/syft.tar.gz -C /tmp \
    && mv /tmp/syft /usr/local/bin/syft \
    && chmod +x /usr/local/bin/syft \
    && rm -f /tmp/syft.tar.gz /tmp/LICENSE /tmp/README.md \
    && adduser -D -u 1000 loopzero

USER 1000
ENTRYPOINT ["/bin/sh"]
