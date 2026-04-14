FROM golang:1.25.3-alpine3.21 AS builder
WORKDIR /workspace

ENV GOWORK=off

COPY pkg/go.mod ./pkg/go.mod
COPY api/go.mod api/go.sum ./api/

RUN --mount=type=cache,target=/root/go/pkg/mod \
    cd api && go mod download

COPY pkg/ ./pkg/
COPY api/ ./api/

RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd api && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /loopzero-api ./cmd/ \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /loopzero-controller ./cmd/controller/

FROM alpine:3.21 AS migrate-dl
RUN apk add --no-cache curl && \
    curl -L https://github.com/golang-migrate/migrate/releases/download/v4.18.1/migrate.linux-amd64.tar.gz | tar xz && \
    mv migrate /migrate

FROM alpine:3.21.3

LABEL org.opencontainers.image.source="https://github.com/loopzero/loopzero"
LABEL org.opencontainers.image.title="loopzero-api"
LABEL org.opencontainers.image.description="LoopZero PaaS API server"

RUN apk add --no-cache ca-certificates git && adduser -D -u 1000 loopzero
COPY --from=builder /loopzero-api /loopzero-api
COPY --from=builder /loopzero-controller /loopzero-controller
COPY --from=migrate-dl /migrate /usr/local/bin/migrate
COPY --from=builder /workspace/api/internal/db/migrations/ /migrations/
USER 1000
EXPOSE 8080 9090 9091

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:8080/healthz || exit 1

CMD ["/loopzero-api"]
