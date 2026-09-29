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
FROM mirror.gcr.io/library/golang:1.26.6-alpine3.23@sha256:e57c41c1d5864341031181b0db34b9a537bb5773eb6428e4e5bdaea0f9135406 AS minio-build

# Tag RELEASE.2025-10-15T17-29-55Z of github.com/minio/minio — the last release
# before the repository was archived, and the first that carries the server's own
# security fix c1a49490c78e (sub-policy check), which the 2024-12-18 tag lacked (#441).
# A tag can be moved; the commit cannot, so the build refuses any checkout that is
# not this commit (a lightweight tag: this is the commit itself).
ARG MINIO_TAG=RELEASE.2025-10-15T17-29-55Z
ARG MINIO_COMMIT=9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a
# The source is archived, so no upstream release will raise these. Each module is
# raised to the first release that fixes the findings trivy reports for this tag
# (measured 2026-09-29, #441). Exact versions keep the build reproducible; x/net and
# x/text are raised by what these require (grpc needs x/net >= 0.58). A pin is
# never below what another pin requires (grpc v1.83.2 needs otel >= v1.44.0).
ARG MINIO_DEP_FIXES="\
github.com/apache/thrift@v0.24.0 \
github.com/buger/jsonparser@v1.1.2 \
github.com/go-jose/go-jose/v4@v4.1.4 \
github.com/prometheus/prometheus@v0.311.3 \
github.com/rabbitmq/amqp091-go@v1.13.0 \
go.opentelemetry.io/otel@v1.44.0 \
go.opentelemetry.io/otel/sdk@v1.44.0 \
golang.org/x/crypto@v0.55.0 \
google.golang.org/grpc@v1.83.2"

RUN apk add --no-cache git \
    && git clone --depth 1 --branch "${MINIO_TAG}" https://github.com/minio/minio.git /src/minio \
    && test "$(git -C /src/minio rev-parse HEAD)" = "${MINIO_COMMIT}"

WORKDIR /src/minio
# tidy -e: tidy also resolves the TESTS of dependencies, and a go-openapi test imports
# a package its own module no longer ships. The build never loads it; any package the
# build does need still fails `go build` below.
RUN go get ${MINIO_DEP_FIXES} && go mod tidy -e
# No -X version injection: the ldflag-symbols gate verifies every injection against
# a module of THIS repository, and upstream's version strings are informational only
# (user agent, a metric label, an admin message) — measured in cmd/ at this commit.
# The release and commit are carried by the OCI labels below instead.
RUN CGO_ENABLED=0 GOFLAGS=-mod=readonly go build -trimpath -tags kqueue \
      -ldflags "-s -w" -o /out/minio . \
    && /out/minio --version

FROM mirror.gcr.io/library/alpine:3.23@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0

ARG MINIO_TAG=RELEASE.2025-10-15T17-29-55Z
ARG MINIO_COMMIT=9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a

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
