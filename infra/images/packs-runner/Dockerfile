# loopzero-packs-runner: Analyzes source code and generates optimized Dockerfiles.
# Replaces hardcoded bash Dockerfile templates (MEET-2026-03-29-017).
#
# CHG-2026-04-23-AUDIT-29-ext: clones the Go rewrite from
# github.com/getloopscale/loopzero-packs and builds the binary, instead of
# bundling a stale shell script. The Go version uses node:20-bookworm
# without `adduser -D -u 1000 loopzero`, fixing the node:alpine UID-1000
# collision that broke every Node deploy through the previous
# bash-based generator.

FROM mirror.gcr.io/library/golang:1.25-alpine AS build

RUN apk add --no-cache git
WORKDIR /src
# Pin to main for reproducibility of the current contract; bump to a
# tag once loopzero-packs cuts a stable release.
RUN git clone --depth 1 https://github.com/getloopscale/loopzero-packs.git . \
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
