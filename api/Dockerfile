FROM golang:1.26.1-alpine3.22 AS builder
WORKDIR /workspace

ENV GOWORK=off

# Sibling loopzero-contracts module: api/go.mod uses `replace => ../../loopzero-contracts`
# which from /workspace/api/ resolves to /loopzero-contracts. Provided via BuildKit
# named build context `loopzero-contracts` so the sibling repo is reachable inside
# the Docker build (per M5 decomposition; see docs/version-pin-cutover-runbook.md).
# Sponsor-direct-approval 2026-06-17 ("O sistema deve rodar no k3, continue").
COPY --from=loopzero-contracts . /loopzero-contracts

COPY pkg/go.mod ./pkg/go.mod
COPY operators/api/go.mod operators/api/go.sum ./operators/api/
COPY operators/api/v1 ./operators/api/v1
COPY api/go.mod api/go.sum ./api/

RUN --mount=type=cache,target=/root/go/pkg/mod \
    cd api && go mod download

COPY pkg/ ./pkg/
COPY api/ ./api/

RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd api && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /loopzero-api ./cmd/ \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /loopzero-controller ./cmd/controller/

# Build migrate from source (same Go toolchain) to avoid CVEs in pre-compiled binary.
FROM golang:1.26.1-alpine3.22 AS migrate-builder
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go install -tags 'postgres' -trimpath -ldflags="-s -w" \
    github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1

FROM alpine:3.22

LABEL org.opencontainers.image.source="https://github.com/getloopscale/loopzero"
LABEL org.opencontainers.image.title="loopzero-api"
LABEL org.opencontainers.image.description="LoopZero PaaS API server"

RUN apk add --no-cache ca-certificates git && adduser -D -u 1000 loopzero
COPY --from=builder /loopzero-api /loopzero-api
COPY --from=builder /loopzero-controller /loopzero-controller
COPY --from=migrate-builder /go/bin/migrate /usr/local/bin/migrate
COPY --from=builder /workspace/api/internal/db/migrations/ /migrations/
USER 1000
EXPOSE 8080 9090 9091

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:8080/healthz || exit 1

CMD ["/loopzero-api"]
