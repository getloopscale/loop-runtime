# loopruntime-clone-runner: Alpine image with mc (MinIO client) + git.
#
# It is the platform's ONE image that carries mc: the clone CWT downloads source
# bundles with it, the cache CWTs save and restore build caches with it, and the
# MinIO chart's bucket/user jobs and the lifecycle job run their mc scripts in it.
#
# mc is BUILT HERE FROM SOURCE (#394). MinIO withdrew every binary channel of its
# community edition: dl.min.io answers 410, the quay.io/minio and Docker Hub
# minio/mc repositories no longer serve images, and github.com/minio/mc is
# distributed as source only. The pin is therefore a source commit, verified before
# the build, and the module graph is checked against the Go checksum database —
# the same guarantee the old binary sha256 gave, one step earlier in the chain.
#
# SEC-004 still holds: nothing is downloaded at runtime.
# Security: runs as non-root user 1000 (loopruntime).
FROM mirror.gcr.io/library/golang:1.26.11-alpine3.23@sha256:23f198931d4bd83b4f5a613bd75656257ae8bf832dcbc2db80e1050b5219ca2a AS mc-build

# Tag RELEASE.2025-08-13T08-35-41Z of github.com/minio/mc. A tag can be moved; the
# commit cannot, so the build refuses any checkout that is not this commit.
ARG MC_TAG=RELEASE.2025-08-13T08-35-41Z
ARG MC_COMMIT=7394ce0dd2a80935aded936b09fa12cbb3cb8096
# The source is archived, so no upstream release will raise these. Each module is
# raised to the first release that fixes the findings trivy reports for this tag
# (measured 2026-09-29, #441). Exact versions keep the build reproducible; x/net
# and x/text are raised by what these require (grpc needs x/net >= 0.58).
ARG MC_DEP_FIXES="\
github.com/klauspost/compress@v1.18.7 \
github.com/prometheus/prometheus@v0.311.3 \
golang.org/x/crypto@v0.58.0 \
golang.org/x/net@v0.62.0 \
google.golang.org/grpc@v1.83.2"

RUN apk add --no-cache git \
    && git clone --depth 1 --branch "${MC_TAG}" https://github.com/minio/mc.git /src/mc \
    && test "$(git -C /src/mc rev-parse HEAD)" = "${MC_COMMIT}"

WORKDIR /src/mc
RUN go get ${MC_DEP_FIXES} && go mod tidy
# No -X version injection (see infra/images/minio/Dockerfile): the release and
# commit are carried by the OCI labels below.
RUN CGO_ENABLED=0 GOFLAGS=-mod=readonly go build -trimpath \
      -ldflags "-s -w" -o /out/mc . \
    && /out/mc --version

FROM mirror.gcr.io/library/alpine:3.23@sha256:469c9ea410b9215eaee7976ba6c0639039b444be8e5d638f7abbada189971eb2

ARG MC_TAG=RELEASE.2025-08-13T08-35-41Z
ARG MC_COMMIT=7394ce0dd2a80935aded936b09fa12cbb3cb8096

LABEL org.opencontainers.image.source="https://github.com/getloopscale/loop-runtime" \
      org.opencontainers.image.title="loopruntime-clone-runner" \
      io.loopruntime.mc.release="${MC_TAG}" \
      io.loopruntime.mc.revision="${MC_COMMIT}"

# /usr/bin/mc is the path the MinIO chart's jobs call explicitly.
COPY --from=mc-build /out/mc /usr/bin/mc

RUN apk add --no-cache git ca-certificates \
    && adduser -D -u 1000 loopruntime

USER 1000
ENTRYPOINT ["/bin/sh"]
