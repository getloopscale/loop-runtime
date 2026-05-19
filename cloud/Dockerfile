FROM golang:1.25.9-alpine3.22 AS builder
WORKDIR /workspace

ENV GOWORK=off

COPY pkg/go.mod ./pkg/go.mod
COPY cloud/go.mod cloud/go.sum ./cloud/

RUN --mount=type=cache,target=/root/go/pkg/mod \
    cd cloud && go mod download

COPY pkg/ ./pkg/
COPY cloud/ ./cloud/

RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd cloud && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /cloud-api ./cmd/main.go

FROM alpine:3.22

LABEL org.opencontainers.image.source="https://github.com/loopzero/loopzero"
LABEL org.opencontainers.image.title="loopzero-cloud"
LABEL org.opencontainers.image.description="LoopZeroCloud service"

RUN apk add --no-cache ca-certificates && adduser -D -u 1000 loopzero
COPY --from=builder /cloud-api /cloud-api
USER 1000
EXPOSE 13002

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:13002/healthz || exit 1

CMD ["/cloud-api"]
