FROM golang:1.25-alpine AS builder
WORKDIR /workspace

# Disable Go workspace — build this module standalone.
ENV GOWORK=off

# Copy module definitions for dependency resolution.
# cloud/go.mod has `replace ../pkg` (monorepo sibling).
COPY pkg/go.mod ./pkg/go.mod
COPY cloud/go.mod cloud/go.sum ./cloud/

# Download dependencies (cached unless go.mod/go.sum change)
RUN cd cloud && go mod download

# Copy source code
COPY pkg/ ./pkg/
COPY cloud/ ./cloud/

# Build the binary
RUN cd cloud && CGO_ENABLED=0 go build -o /cloud-api ./cmd/main.go

# Runtime image
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
RUN adduser -D -u 1000 loopzero
COPY --from=builder /cloud-api /cloud-api
USER 1000
EXPOSE 13002
CMD ["/cloud-api"]
