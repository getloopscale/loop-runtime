FROM golang:1.25.10-alpine3.22 AS builder
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

# Distroless static runtime (find_infra_009): no shell/apk/utilities to reduce
# post-compromise attack surface. Includes ca-certificates and runs as the
# built-in nonroot user (uid 65532). The binary is fully static (CGO_ENABLED=0).
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.source="https://github.com/loopzero/loopzero"
LABEL org.opencontainers.image.title="loopzero-cloud"
LABEL org.opencontainers.image.description="LoopZeroCloud service"

COPY --from=builder /cloud-api /cloud-api
USER nonroot:nonroot
EXPOSE 13002

# No Docker HEALTHCHECK: distroless has no shell/wget. Kubernetes liveness and
# readiness probes (httpGet /health on 13002) own health checking in production.

CMD ["/cloud-api"]
