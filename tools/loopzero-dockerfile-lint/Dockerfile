# loopzero-dockerfile-lint — static analyzer for user-supplied Dockerfiles
# Plan: docs/plans/build-mode-dispatch-and-dockerfile-security-plan.md (T2.3)
#
# Runs as a step in the LoopZero build pipeline (Argo Workflow) BEFORE BuildKit.
# Validates user Dockerfiles against hard rejection rules (security) and
# emits warnings for soft issues. Exit 0 = pass, 1 = reject, 2 = usage error.

# T4.4 of deep-review-remediation-plan (closes find_infra_003 MEDIUM):
# pinned to specific patch matching api/operators/cloud Dockerfiles. Floating
# 1.25-alpine resolved to whatever latest patch GHCR served on each build,
# breaking SOURCE_DATE_EPOCH reproducibility.
FROM golang:1.26.1-alpine3.22 AS build

WORKDIR /src
COPY go.mod ./
COPY main.go ./

ENV GOWORK=off
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /loopzero-dockerfile-lint .

# Minimal runtime — aligned to fleet's alpine pin (was 3.20, fleet uses 3.22).
FROM alpine:3.22

LABEL org.opencontainers.image.source="https://github.com/getloopscale/loopzero"
LABEL org.opencontainers.image.title="loopzero-dockerfile-lint"
LABEL org.opencontainers.image.description="Static analyzer for user-supplied Dockerfiles in LoopZero build pipeline"

RUN apk add --no-cache ca-certificates && adduser -D -u 1000 loopzero
COPY --from=build /loopzero-dockerfile-lint /usr/local/bin/loopzero-dockerfile-lint
USER 1000

ENTRYPOINT ["/usr/local/bin/loopzero-dockerfile-lint"]
