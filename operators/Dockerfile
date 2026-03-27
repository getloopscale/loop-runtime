FROM golang:1.25.3-alpine3.21 AS builder
WORKDIR /workspace

# Disable Go workspace — build this module standalone.
# go.work references modules (loopzero-forge) not needed for the operator binary.
ENV GOWORK=off

# Copy module definitions for dependency resolution
COPY pkg/go.mod ./pkg/go.mod
COPY operators/go.mod operators/go.sum ./operators/

# Download dependencies (cached unless go.mod/go.sum change)
RUN cd operators && go mod download

# Copy source code
COPY pkg/ ./pkg/
COPY operators/ ./operators/

# Build the binary
RUN cd operators && CGO_ENABLED=0 go build -o /tenant-controller ./cmd/main.go

# Runtime image
FROM alpine:3.21.3

LABEL org.opencontainers.image.source="https://github.com/loopzero/loopzero"
LABEL org.opencontainers.image.title="loopzero-operator"
LABEL org.opencontainers.image.description="LoopZero PaaS Kubernetes operator"

RUN apk add --no-cache ca-certificates
RUN adduser -D -u 1000 loopzero
USER 1000
COPY --from=builder /tenant-controller /usr/local/bin/tenant-controller
EXPOSE 8080 8081

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost:8081/healthz || exit 1

ENTRYPOINT ["tenant-controller"]
CMD ["--leader-elect=true"]
