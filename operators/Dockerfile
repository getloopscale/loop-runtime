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

# T3.7 / INFRA-007: distroless runtime — no shell, no package manager,
# no busybox. K8s livenessProbe + readinessProbe in
# charts/loopzero-tenant-controller/templates/deployment.yaml:85-93 replace
# the Dockerfile HEALTHCHECK (distroless cannot exec wget).
# `:nonroot` tag runs as UID 65532, drops the manual adduser step.
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.source="https://github.com/loopzero/loopzero"
LABEL org.opencontainers.image.title="loopzero-tenant-controller"
LABEL org.opencontainers.image.description="LoopZero PaaS Kubernetes operator"

COPY --from=builder /loopzero-tenant-controller /usr/local/bin/loopzero-tenant-controller
EXPOSE 8080 8081

ENTRYPOINT ["loopzero-tenant-controller"]
CMD ["--leader-elect=true"]
