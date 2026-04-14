# loopzero-clone-runner: Alpine image with mc (MinIO client) + git.
# Used by the loopzero-clone CWT to download source bundles from MinIO
# and unpack them via git clone (bundle format).
#
# SEC-004: mc binary baked at build time from official MinIO release.
# No runtime downloads — eliminates supply chain risk.
#
# Size: ~50MB (Alpine base + mc binary + git)
# Security: runs as non-root user 1000 (loopzero)
FROM alpine:3.21.3

LABEL org.opencontainers.image.source="https://github.com/loopzero/loopzero"
LABEL org.opencontainers.image.title="loopzero-clone-runner"

# mc binary version and checksum — update both when upgrading.
# Get checksum: wget -q <url> -O mc && sha256sum mc
ARG MC_VERSION=RELEASE.2025-04-08T15-05-49Z
ARG MC_SHA256=9e39b86bd737a1d0d10207f285cbab2be1f01f714c705c67dff8abb4a5eb9fe0

RUN apk add --no-cache git ca-certificates \
    && wget -q "https://dl.min.io/client/mc/release/linux-amd64/archive/mc.${MC_VERSION}" \
       -O /usr/local/bin/mc \
    && echo "${MC_SHA256}  /usr/local/bin/mc" | sha256sum -c - \
    && chmod +x /usr/local/bin/mc \
    && adduser -D -u 1000 loopzero

USER 1000
ENTRYPOINT ["/bin/sh"]
