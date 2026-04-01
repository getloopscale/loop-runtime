# loopzero-clone-runner: Alpine image with mc (MinIO client) + git.
# Used by the loopzero-clone CWT to download source bundles from MinIO
# and unpack them via git clone (bundle format).
#
# SEC-004: mc binary baked at build time from official MinIO release.
# No runtime downloads — eliminates supply chain risk.
#
# Size: ~50MB (Alpine base + mc binary + git)
# Security: runs as non-root user 1000 (loopzero)
FROM alpine:3.21

RUN apk add --no-cache git ca-certificates \
    && wget -q "https://dl.min.io/client/mc/release/linux-amd64/mc" -O /usr/local/bin/mc \
    && chmod +x /usr/local/bin/mc \
    && adduser -D -u 1000 loopzero

USER 1000
ENTRYPOINT ["/bin/sh"]
