FROM golang:1.26.1-alpine3.22 AS builder
WORKDIR /workspace

ENV GOWORK=off
ENV GOPRIVATE=github.com/getloopscale/*

# loopzero-contracts is a PINNED published module (M5 version-pin cutover — api/go.mod
# requires github.com/getloopscale/loopzero-contracts v0.3.0, NO replace). The private module
# is fetched via a short-lived token mounted as a buildx secret (id=gomod_token); the
# token is NEVER written to an image layer. git is required for the module VCS fetch.
RUN apk add --no-cache git

COPY pkg/go.mod ./pkg/go.mod
COPY operators/api/go.mod operators/api/go.sum ./operators/api/
COPY operators/api/v1 ./operators/api/v1
COPY api/go.mod api/go.sum ./api/

RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=secret,id=gomod_token \
    sh -c 'if [ -s /run/secrets/gomod_token ]; then \
      git config --global url."https://x-access-token:$(cat /run/secrets/gomod_token)@github.com/getloopscale/".insteadOf "https://github.com/getloopscale/"; \
    fi' \
    && cd api && go mod download

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
