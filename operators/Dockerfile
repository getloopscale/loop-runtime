FROM golang:1.25.9-alpine3.22 AS builder
WORKDIR /workspace

ENV GOWORK=off

COPY pkg/go.mod ./pkg/go.mod
COPY operators/go.mod operators/go.sum ./operators/

RUN --mount=type=cache,target=/root/go/pkg/mod \
    cd operators && go mod download

COPY pkg/ ./pkg/
COPY operators/ ./operators/

RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd operators && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /loopzero-tenant-controller ./cmd/main.go

FROM alpine:3.22

LABEL org.opencontainers.image.source="https://github.com/loopzero/loopzero"
LABEL org.opencontainers.image.title="loopzero-tenant-controller"
LABEL org.opencontainers.image.description="LoopZero PaaS Kubernetes operator"

RUN apk add --no-cache ca-certificates && adduser -D -u 1000 loopzero
USER 1000
COPY --from=builder /loopzero-tenant-controller /usr/local/bin/loopzero-tenant-controller
EXPOSE 8080 8081

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost:8081/healthz || exit 1

ENTRYPOINT ["loopzero-tenant-controller"]
CMD ["--leader-elect=true"]
