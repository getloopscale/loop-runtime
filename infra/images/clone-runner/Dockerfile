# loopzero-clone-runner: Alpine image with mc (MinIO client) + git.
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
# Security: runs as non-root user 1000 (loopzero).
FROM mirror.gcr.io/library/golang:1.23.12-alpine3.21@sha256:4bb4be21ac98da06bc26437ee870c4973f8039f13e9a1a36971b4517632b0fc6 AS mc-build

# Tag RELEASE.2025-08-13T08-35-41Z of github.com/minio/mc. A tag can be moved; the
# commit cannot, so the build refuses any checkout that is not this commit.
ARG MC_TAG=RELEASE.2025-08-13T08-35-41Z
ARG MC_COMMIT=7394ce0dd2a80935aded936b09fa12cbb3cb8096

RUN apk add --no-cache git \
    && git clone --depth 1 --branch "${MC_TAG}" https://github.com/minio/mc.git /src/mc \
    && test "$(git -C /src/mc rev-parse HEAD)" = "${MC_COMMIT}"

WORKDIR /src/mc
RUN CGO_ENABLED=0 GOFLAGS=-mod=readonly go build -trimpath \
      -ldflags "-s -w -X github.com/minio/mc/cmd.ReleaseTag=${MC_TAG} -X github.com/minio/mc/cmd.CommitID=${MC_COMMIT}" \
      -o /out/mc . \
    && /out/mc --version

FROM mirror.gcr.io/library/alpine:3.21.6@sha256:c3f8e73fdb79deaebaa2037150150191b9dcbfba68b4a46d70103204c53f4709

LABEL org.opencontainers.image.source="https://github.com/getloopscale/loopzero"
LABEL org.opencontainers.image.title="loopzero-clone-runner"

# /usr/bin/mc is the path the MinIO chart's jobs call explicitly.
COPY --from=mc-build /out/mc /usr/bin/mc

RUN apk add --no-cache git ca-certificates \
    && adduser -D -u 1000 loopzero

USER 1000
ENTRYPOINT ["/bin/sh"]
