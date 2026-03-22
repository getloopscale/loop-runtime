FROM golang:1.25-alpine AS builder
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
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
RUN adduser -D -u 1000 loopzero
USER 1000
COPY --from=builder /tenant-controller /tenant-controller
EXPOSE 8081
ENTRYPOINT ["/tenant-controller"]
CMD ["--leader-elect=true"]
