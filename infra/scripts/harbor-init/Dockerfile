# SPDX-License-Identifier: Apache-2.0
# Multi-stage build for harbor-init — idempotent post-deploy configuration.
# Produces a minimal distroless image (~5 MB).

FROM golang:1.25.9-bookworm AS builder
WORKDIR /src

COPY infra/scripts/go.mod infra/scripts/go.sum* ./infra/scripts/
# infra/scripts/go.mod has `replace ../tests` — copy its go.mod to satisfy the replace
COPY infra/tests/go.mod infra/tests/go.sum* ./infra/tests/
RUN --mount=type=cache,target=/root/go/pkg/mod \
    cd infra/scripts && go mod download

COPY infra/scripts/harbor-init/ ./infra/scripts/harbor-init/

RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd infra/scripts && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /harbor-init ./harbor-init/

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.source="https://github.com/getloopscale/loopzero"
LABEL org.opencontainers.image.title="harbor-init"

COPY --from=builder /harbor-init /harbor-init
USER nonroot:nonroot
ENTRYPOINT ["/harbor-init"]
