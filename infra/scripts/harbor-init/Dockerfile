# SPDX-License-Identifier: Apache-2.0
# Multi-stage build for harbor-init — idempotent post-deploy configuration.
# Produces a minimal distroless image (~5 MB).

FROM golang:1.25-bookworm AS builder
WORKDIR /src

# Copy module files first for caching.
COPY infra/scripts/go.mod infra/scripts/go.sum* ./infra/scripts/
RUN cd infra/scripts && go mod download 2>/dev/null || true

# Copy source.
COPY infra/scripts/harbor-init/ ./infra/scripts/harbor-init/

# Build static binary.
RUN cd infra/scripts && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /harbor-init ./harbor-init/

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /harbor-init /harbor-init
USER nonroot:nonroot
ENTRYPOINT ["/harbor-init"]
