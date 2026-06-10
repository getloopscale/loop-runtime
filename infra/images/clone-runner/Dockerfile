# loopzero-clone-runner: Alpine image with mc (MinIO client) + git.
# Used by the loopzero-clone CWT to download source bundles from MinIO
# and unpack them via git clone (bundle format).
#
# SEC-004: mc binary baked at build time from official MinIO release.
# No runtime downloads — eliminates supply chain risk.
#
# Size: ~50MB (Alpine base + mc binary + git)
# Security: runs as non-root user 1000 (loopzero)
FROM mirror.gcr.io/library/alpine:3.21.6

LABEL org.opencontainers.image.source="https://github.com/loopzero/loopzero"
LABEL org.opencontainers.image.title="loopzero-clone-runner"

# mc binary version and checksum — update both when upgrading.
# Get checksum: wget -q <url> -O mc && sha256sum mc
ARG MC_VERSION=RELEASE.2025-08-13T08-35-41Z
ARG MC_SHA256=01f866e9c5f9b87c2b09116fa5d7c06695b106242d829a8bb32990c00312e891

RUN apk add --no-cache git ca-certificates \
    && wget -q "https://dl.min.io/client/mc/release/linux-amd64/archive/mc.${MC_VERSION}" \
       -O /usr/local/bin/mc \
    && echo "${MC_SHA256}  /usr/local/bin/mc" | sha256sum -c - \
    && chmod +x /usr/local/bin/mc \
    && adduser -D -u 1000 loopzero

USER 1000
ENTRYPOINT ["/bin/sh"]
