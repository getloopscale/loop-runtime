FROM golang:1.25-alpine AS builder
WORKDIR /workspace

# Disable Go workspace — build this module standalone.
# go.work references modules (loopzero-forge) not needed for the API binary.
ENV GOWORK=off

# Copy module definitions for dependency resolution.
# api/go.mod has `replace ../pkg` (monorepo sibling).
# loopzero-forge is now a published Go module (no local replace).
COPY pkg/go.mod ./pkg/go.mod
COPY api/go.mod api/go.sum ./api/

# Download dependencies (cached unless go.mod/go.sum change)
RUN cd api && go mod download

# Copy source code
COPY pkg/ ./pkg/
COPY api/ ./api/

# Build both binaries. The same image ships /loopzero-api (HTTP surface) and
# /loopzero-controller (background loops). The Kubernetes Deployment chooses
# which one to exec via the container `command:` field, so a single image
# artifact supports both workloads (roadmap 02).
RUN cd api && CGO_ENABLED=0 go build -o /loopzero-api ./cmd/ \
    && CGO_ENABLED=0 go build -o /loopzero-controller ./cmd/controller/

# Download migrate binary for init container usage
FROM alpine:3.21 AS migrate-dl
RUN apk add --no-cache curl && \
    curl -L https://github.com/golang-migrate/migrate/releases/download/v4.18.1/migrate.linux-amd64.tar.gz | tar xz && \
    mv migrate /migrate

# Runtime image
FROM alpine:3.21
RUN apk add --no-cache ca-certificates git
RUN adduser -D -u 1000 loopzero
COPY --from=builder /loopzero-api /loopzero-api
COPY --from=builder /loopzero-controller /loopzero-controller
COPY --from=migrate-dl /migrate /usr/local/bin/migrate
# Include migrations (used by both: init container mode + API reference)
COPY --from=builder /workspace/api/internal/db/migrations/ /migrations/
USER 1000
# 8080 = API HTTP, 9090 = API metrics, 9091 = controller HTTP (/healthz + /readyz + /metrics)
EXPOSE 8080 9090 9091
CMD ["/loopzero-api"]
