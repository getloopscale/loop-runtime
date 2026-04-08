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

# Build the binary
RUN cd api && CGO_ENABLED=0 go build -o /loopzero-api ./cmd/

# Runtime image
FROM alpine:3.21
RUN apk add --no-cache ca-certificates git
RUN adduser -D -u 1000 loopzero
COPY --from=builder /loopzero-api /loopzero-api
# Include migrations for init container (golang-migrate reads from /migrations/)
COPY --from=builder /workspace/api/internal/db/migrations/ /migrations/
USER 1000
EXPOSE 3001
CMD ["/loopzero-api"]
