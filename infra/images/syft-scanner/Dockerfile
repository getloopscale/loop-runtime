# loopzero-syft-scanner: Alpine image with syft baked in.
# Used by the sbom-{app} Argo Workflow step (Sprint 6 / T1.1) to scan
# signed container images and produce CycloneDX SBOMs.
#
# syft is BUILT HERE FROM SOURCE (#441). The upstream release binary is compiled with
# the Go toolchain of its release day, so even the newest one (v1.52.0) carried 11
# fixable HIGH findings in the Go standard library. Building the verified tag commit
# with the current Go toolchain removes them without waiting for an upstream release.
# The pin is a source commit, checked before the build, and the module graph is
# verified against the Go checksum database — the guarantee the release checksum gave.
#
# SEC-006: nothing is downloaded at runtime.
# Output: CycloneDX JSON written to /tmp (caller uploads to S3).
# Security: runs as non-root user 1000, no privilege escalation.
FROM mirror.gcr.io/library/golang:1.26.11-alpine3.23@sha256:23f198931d4bd83b4f5a613bd75656257ae8bf832dcbc2db80e1050b5219ca2a AS syft-build

# Tag v1.57.2 of github.com/anchore/syft. A tag can be moved; the commit cannot, so
# the build refuses any checkout that is not this commit.
ARG SYFT_TAG=v1.57.2
ARG SYFT_COMMIT=1a72047fd46c72e0a12ee77e24b23d2316753d08

RUN apk add --no-cache git \
    && git clone --depth 1 --branch "${SYFT_TAG}" https://github.com/anchore/syft.git /src/syft \
    && test "$(git -C /src/syft rev-parse HEAD)" = "${SYFT_COMMIT}"

WORKDIR /src/syft
# No -X version injection (the ldflag-symbols gate only accepts symbols of this
# repository); the release and commit are carried by the OCI labels below.
RUN CGO_ENABLED=0 GOFLAGS=-mod=readonly go build -trimpath \
      -ldflags "-s -w" -o /out/syft ./cmd/syft \
    && /out/syft --help >/dev/null

FROM mirror.gcr.io/library/alpine:3.23@sha256:469c9ea410b9215eaee7976ba6c0639039b444be8e5d638f7abbada189971eb2

ARG SYFT_TAG=v1.57.2
ARG SYFT_COMMIT=1a72047fd46c72e0a12ee77e24b23d2316753d08

LABEL org.opencontainers.image.source="https://github.com/getloopscale/loopzero" \
      org.opencontainers.image.title="loopzero-syft-scanner" \
      org.opencontainers.image.description="syft SBOM scanner for LoopZero build pipeline, built from anchore/syft@${SYFT_COMMIT}" \
      io.loopzero.syft.release="${SYFT_TAG}" \
      io.loopzero.syft.revision="${SYFT_COMMIT}"

COPY --from=syft-build /out/syft /usr/local/bin/syft

RUN apk add --no-cache ca-certificates aws-cli \
    && adduser -D -u 1000 loopzero

USER 1000
ENTRYPOINT ["/bin/sh"]
