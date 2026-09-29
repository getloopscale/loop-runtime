# loopzero-cosign-signer: Alpine image with cosign baked in.
# Used by the sign-image Argo Workflow step to sign user-built container images.
#
# cosign is BUILT HERE FROM SOURCE (#441). The upstream release binary pins the Go
# toolchain and the module versions of its release day: v2.6.5 carried 38 fixable
# HIGH/CRITICAL findings (stdlib, x/crypto, x/net, grpc, fulcio, rekor, …). Building
# the verified tag commit with the current Go toolchain, and with each vulnerable
# module raised to the first release that fixes it, removes them. The pin is a source
# commit, checked before the build, and every module is verified against the Go
# checksum database — the guarantee the release checksum gave, one step earlier.
#
# Stays on the v2 line: 2026-05-28 (Ralph iter 3) v3 was tried and downgraded, because
# cosign v3 rejects the token client-side after Fulcio GetConfiguration without ever
# calling CreateSigningCertificate. v2 posts SIGSTORE_ID_TOKEN to /api/v2/signingCert.
#
# SEC-006: nothing is downloaded at runtime.
# Security: runs as non-root user 1000, no privilege escalation.
FROM mirror.gcr.io/library/golang:1.26.6-alpine3.23@sha256:e57c41c1d5864341031181b0db34b9a537bb5773eb6428e4e5bdaea0f9135406 AS cosign-build

# Tag v2.6.5 of github.com/sigstore/cosign. A tag can be moved; the commit cannot, so
# the build refuses any checkout that is not this commit.
ARG COSIGN_TAG=v2.6.5
ARG COSIGN_COMMIT=3e82f50a2839855693aacf7b3d0e7e2f30774cb4
# Each module raised to the first release that fixes the findings trivy reports for
# this tag (measured 2026-09-29). Exact versions keep the build reproducible;
# x/net and x/text are raised by what these require (grpc needs x/net >= 0.58), so
# pinning them lower is refused by Go. A pin is never
# below what another pin requires (grpc v1.83.2 needs otel >= v1.44.0, fulcio v1.8.6
# needs docker/cli >= v29.4.0): Go refuses the lower one.
ARG COSIGN_DEP_FIXES="\
github.com/docker/cli@v29.4.0+incompatible \
github.com/go-jose/go-jose/v4@v4.1.4 \
github.com/sigstore/fulcio@v1.8.6 \
github.com/sigstore/rekor@v1.5.2 \
go.opentelemetry.io/otel@v1.44.0 \
go.opentelemetry.io/otel/sdk@v1.44.0 \
golang.org/x/crypto@v0.55.0 \
golang.org/x/mod@v0.40.0 \
google.golang.org/grpc@v1.83.2"

RUN apk add --no-cache git \
    && git clone --depth 1 --branch "${COSIGN_TAG}" https://github.com/sigstore/cosign.git /src/cosign \
    && test "$(git -C /src/cosign rev-parse HEAD)" = "${COSIGN_COMMIT}"

WORKDIR /src/cosign
RUN go get ${COSIGN_DEP_FIXES} && go mod tidy
# No -X version injection (the ldflag-symbols gate only accepts symbols of this
# repository); the release and commit are carried by the OCI labels below.
RUN CGO_ENABLED=0 GOFLAGS=-mod=readonly go build -trimpath \
      -ldflags "-s -w" -o /out/cosign ./cmd/cosign \
    && /out/cosign version >/dev/null

FROM mirror.gcr.io/library/alpine:3.23@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0

ARG COSIGN_TAG=v2.6.5
ARG COSIGN_COMMIT=3e82f50a2839855693aacf7b3d0e7e2f30774cb4

LABEL org.opencontainers.image.source="https://github.com/getloopscale/loopzero" \
      org.opencontainers.image.title="loopzero-cosign-signer" \
      org.opencontainers.image.description="Cosign image signer for LoopZero build pipeline, built from sigstore/cosign@${COSIGN_COMMIT}" \
      io.loopzero.cosign.release="${COSIGN_TAG}" \
      io.loopzero.cosign.revision="${COSIGN_COMMIT}"

COPY --from=cosign-build /out/cosign /usr/local/bin/cosign

RUN apk add --no-cache ca-certificates \
    && adduser -D -u 1000 loopzero

USER 1000
ENTRYPOINT ["/bin/sh"]
