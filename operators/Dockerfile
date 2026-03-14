FROM golang:1.25-alpine AS builder
WORKDIR /workspace

# Copy Go workspace definition
COPY go.work go.work.sum ./

# Copy all module definitions for dependency resolution
COPY pkg/go.mod ./pkg/go.mod
COPY api/go.mod api/go.sum ./api/
COPY operators/go.mod operators/go.sum ./operators/
COPY infra/tests/go.mod infra/tests/go.sum ./infra/tests/
COPY infra/scripts/go.mod ./infra/scripts/

# Download dependencies (cached unless go.mod/go.sum change)
RUN cd operators && go mod download

# Copy source code
COPY pkg/ ./pkg/
COPY operators/ ./operators/

# Build the binary
RUN cd operators && CGO_ENABLED=0 go build -o /tenant-controller ./cmd/main.go

# Runtime image
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
RUN adduser -D -u 1000 loopzero
USER 1000
COPY --from=builder /tenant-controller /tenant-controller
EXPOSE 8081
ENTRYPOINT ["/tenant-controller"]
CMD ["--leader-elect=false"]
