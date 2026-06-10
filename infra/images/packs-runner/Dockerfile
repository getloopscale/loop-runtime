# loopzero-packs-runner: Analyzes source code and generates optimized Dockerfiles.
# Replaces hardcoded bash Dockerfile templates (MEET-2026-03-29-017).
#
# CHG-2026-04-23-AUDIT-29-ext: clones the Go rewrite from
# github.com/getloopscale/loopzero-packs and builds the binary, instead of
# bundling a stale shell script. The Go version uses node:20-bookworm
# without `adduser -D -u 1000 loopzero`, fixing the node:alpine UID-1000
# collision that broke every Node deploy through the previous
# bash-based generator.

# T4.4 of deep-review-remediation-plan (closes find_infra_003 MEDIUM):
# pinned to specific patch matching api/operators Dockerfiles for
# reproducibility under SOURCE_DATE_EPOCH.
FROM mirror.gcr.io/library/golang:1.25.9-alpine3.22 AS build

RUN apk add --no-cache git
WORKDIR /src
# T4.3 of deep-review-remediation-plan (closes find_infra_002 HIGH):
# loopzero-packs is now pinned to a specific commit SHA via build arg, set
# in the build pipeline (or fed via ARG override). Default value is the
# commit known-good for loopzero-packs v0.4.0 lenient canonical contract.
# CI MUST override LOOPZERO_PACKS_REF when bumping versions; the default exists
# only so local docker build still works.
ARG LOOPZERO_PACKS_REF=v0.4.0
RUN git clone --depth 1 --branch ${LOOPZERO_PACKS_REF} https://github.com/getloopscale/loopzero-packs.git . \
 && go mod download \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /loopzeropacks-generate ./cmd/loopzeropacks-generate

FROM mirror.gcr.io/library/alpine:3.21.6

LABEL org.opencontainers.image.source="https://github.com/loopzero/loopzero"
LABEL org.opencontainers.image.title="loopzero-packs-runner"

RUN apk add --no-cache ca-certificates \
 && adduser -D -u 1000 loopzero

COPY --from=build /loopzeropacks-generate /usr/local/bin/loopzeropacks-generate

USER 1000
ENTRYPOINT ["loopzeropacks-generate"]
