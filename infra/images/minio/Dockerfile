# loopzero-minio: the MinIO server, built from source (#394).
#
# MinIO withdrew every binary channel of its community edition: dl.min.io answers
# 410, quay.io/minio/minio and Docker Hub minio/minio no longer serve images, and
# github.com/minio/minio was archived on 2026-04-24 as "source only, no longer
# maintained". The chart we install (minio/minio 5.4.0) still defaults to the quay
# image, so a cold start cannot pull its object store at all.
#
# This is a BRIDGE, and it is stated as one: the source is archived, so no security
# fix will ever be published for it. Replacing the local/self-host object store with
# a maintained S3-compatible implementation is tracked separately (#394).
#
# The layout matches upstream's Dockerfile.release at the same commit — the chart
# calls /usr/bin/docker-entrypoint.sh and expects `minio` on PATH — with the
# download-and-minisign step replaced by a build from a verified source commit.
FROM mirror.gcr.io/library/golang:1.23.12-alpine3.21@sha256:4bb4be21ac98da06bc26437ee870c4973f8039f13e9a1a36971b4517632b0fc6 AS minio-build

# Tag RELEASE.2024-12-18T13-15-44Z of github.com/minio/minio — the version the
# chart pins. A tag can be moved; the commit cannot, so the build refuses any
# checkout that is not this commit (the tag OBJECT is e1f26f7c; this is the commit
# it points to).
ARG MINIO_TAG=RELEASE.2024-12-18T13-15-44Z
ARG MINIO_COMMIT=16f8cf1c52f0a77eeb8f7565aaf7f7df12454583
# The release time the tag encodes, in the form upstream's gen-ldflags writes.
ARG MINIO_VERSION=2024-12-18T13:15:44Z

RUN apk add --no-cache git \
    && git clone --depth 1 --branch "${MINIO_TAG}" https://github.com/minio/minio.git /src/minio \
    && test "$(git -C /src/minio rev-parse HEAD)" = "${MINIO_COMMIT}"

WORKDIR /src/minio
RUN CGO_ENABLED=0 GOFLAGS=-mod=readonly go build -trimpath -tags kqueue \
      -ldflags "-s -w \
        -X github.com/minio/minio/cmd.Version=${MINIO_VERSION} \
        -X github.com/minio/minio/cmd.ReleaseTag=${MINIO_TAG} \
        -X github.com/minio/minio/cmd.CommitID=${MINIO_COMMIT} \
        -X github.com/minio/minio/cmd.ShortCommitID=${MINIO_COMMIT%${MINIO_COMMIT#????????????}}" \
      -o /out/minio . \
    && /out/minio --version

FROM mirror.gcr.io/library/alpine:3.21.6@sha256:c3f8e73fdb79deaebaa2037150150191b9dcbfba68b4a46d70103204c53f4709

ARG MINIO_TAG=RELEASE.2024-12-18T13-15-44Z
ARG MINIO_COMMIT=16f8cf1c52f0a77eeb8f7565aaf7f7df12454583

LABEL org.opencontainers.image.source="https://github.com/getloopscale/loopzero" \
      org.opencontainers.image.title="loopzero-minio" \
      org.opencontainers.image.version="${MINIO_TAG}" \
      org.opencontainers.image.revision="${MINIO_COMMIT}" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.description="MinIO server built from github.com/minio/minio@${MINIO_COMMIT}"

ENV MINIO_ACCESS_KEY_FILE=access_key \
    MINIO_SECRET_KEY_FILE=secret_key \
    MINIO_ROOT_USER_FILE=access_key \
    MINIO_ROOT_PASSWORD_FILE=secret_key \
    MINIO_KMS_SECRET_KEY_FILE=kms_master_key \
    MINIO_CONFIG_ENV_FILE=config.env \
    MC_CONFIG_DIR=/tmp/.mc

RUN apk add --no-cache ca-certificates

COPY --from=minio-build /out/minio /usr/bin/minio
COPY --from=minio-build /src/minio/dockerscripts/docker-entrypoint.sh /usr/bin/docker-entrypoint.sh
# AGPL-3.0: the licence and credits travel with the binary.
COPY --from=minio-build /src/minio/LICENSE /licenses/LICENSE
COPY --from=minio-build /src/minio/CREDITS /licenses/CREDITS

EXPOSE 9000
VOLUME ["/data"]
ENTRYPOINT ["/usr/bin/docker-entrypoint.sh"]
CMD ["minio"]
