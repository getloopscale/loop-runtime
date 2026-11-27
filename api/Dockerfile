FROM golang:1.26.11-alpine3.23@sha256:23f198931d4bd83b4f5a613bd75656257ae8bf832dcbc2db80e1050b5219ca2a AS builder
WORKDIR /workspace

ENV GOWORK=off
ENV GOPRIVATE=github.com/getloopscale/*

# loop-runtime-contracts is a PINNED published module (M5 version-pin cutover — api/go.mod
# requires github.com/getloopscale/contracts v0.6.0, NO replace). The private module
#
# FIXED 2026-08-08: this file authenticated for `getloopscale/` while api/go.mod
# started requesting `getloopscale/` in commit f813b367 (2026-08-06). The URL rewrite did not
# match the requested module, so git fetched loop-runtime-contracts WITHOUT authentication — and
# GitHub answers an unauthenticated request for a private repo with "not found", the same
# message as for a repo that does not exist. The loop-runtime-api image build broke on the first
# push to develop after the migration. Both orgs stay covered while the redirect exists.
# is fetched via a short-lived token mounted as a buildx secret (id=gomod_token); the
# token is NEVER written to an image layer. git is required for the module VCS fetch.
RUN apk add --no-cache git openssh-client

COPY pkg/go.mod ./pkg/go.mod
COPY operators/api/go.mod operators/api/go.sum ./operators/api/
COPY operators/api/v1 ./operators/api/v1
COPY api/go.mod api/go.sum ./api/

# `awk 1` and not `cp` when writing the key: OpenSSH requires a TRAILING newline in
# the PEM, and transport via `secret-envs` (an environment variable) strips it. Without
# that single byte the key arrives syntactically invalid and the error is
# `Load key: error in libcrypto` — which reads like a corrupted key, not like a
# missing terminator. Reproduced locally: `printf %s "$(cat key)"` produces exactly
# that error. `awk 1` re-emits every line with a terminator, normalizing to a single
# trailing newline whatever shape the key arrived in.
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=secret,id=gomod_token \
    --mount=type=secret,id=gomod_ssh_key \
    sh -c 'if [ -s /run/secrets/gomod_ssh_key ]; then \
      mkdir -p /root/.ssh && chmod 700 /root/.ssh; \
      awk 1 /run/secrets/gomod_ssh_key > /root/.ssh/gomod_key && chmod 600 /root/.ssh/gomod_key; \
      ssh-keyscan -t ed25519 github.com >> /root/.ssh/known_hosts 2>/dev/null; \
      printf "Host github.com\n  IdentityFile /root/.ssh/gomod_key\n  IdentitiesOnly yes\n  IdentityAgent none\n" > /root/.ssh/config; \
      git config --global url."git@github.com:getloopscale/".insteadOf "https://github.com/getloopscale/"; \
    elif [ -s /run/secrets/gomod_token ]; then \
      git config --global url."https://x-access-token:$(cat /run/secrets/gomod_token)@github.com/getloopscale/".insteadOf "https://github.com/getloopscale/"; \
    fi' \
    && cd api && { go mod download || { \
      echo "" >&2; \
      echo "FATAL: go mod download failed." >&2; \
      if [ ! -s /run/secrets/gomod_token ]; then \
        echo "  PROBABLE CAUSE: the gomod_token secret arrived EMPTY, so the URL rewrite above was" >&2; \
        echo "  SKIPPED (the if is silent by design) and git fetched loop-runtime-contracts WITHOUT authentication." >&2; \
        echo "  GitHub answers an unauthenticated request for a PRIVATE repo with Repository-not-found —" >&2; \
        echo "  the SAME message as for a repo that does not exist. That is why the error sends you looking" >&2; \
        echo "  for a repository that EXISTS, and why this build failed 10 times in a row without the cause showing." >&2; \
        echo "  ACTION: GOMOD_TOKEN needs a PAT with Contents:read on getloopscale/contracts" >&2; \
        echo "  (the module did NOT migrate together with this repository)." >&2; \
      else \
        echo "  The token is present and non-empty, so this is NOT a missing credential." >&2; \
        echo "  Check Contents:read on getloopscale/contracts and whether the PAT has expired." >&2; \
      fi; \
      exit 1; \
    }; }

COPY pkg/ ./pkg/
COPY api/ ./api/

# Binary provenance. Without these three the version endpoint reports the defaults
# declared in the code (`dev` / `unknown`) — honest, but useless: the /status panel
# showed "dev" as the production version, which demonstrates no version at all. CI
# passes them via the `build_args` of the build-publish reusable workflow.
#
# Defaults kept on purpose: a local build without args still compiles, and the
# endpoint reports `unknown` instead of a plausible-but-false sha, which an external
# verifier would read as "up to date".
ARG LOOP_RUNTIME_VERSION=dev
ARG LOOP_RUNTIME_COMMIT=unknown
ARG LOOP_RUNTIME_BUILD_DATE=unknown

RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    # B-053 — the path is copied from `api/internal/version/versionhttp/version.go`,
    # which is the single reference and MOVES TOGETHER with the code. These three lines
    # had two compounded errors: the org was `getloopscale` (the module is `getloopscale`)
    # and the package was the flat `internal/routes`, which the decomposition emptied.
    #
    # `-X` on a nonexistent symbol is a NO-OP that exits 0 — the linker does not warn —
    # so the build passed and the binary kept the default. Measured live on
    # 2026-08-16: the engine answered `{"version":"dev","built_at":"unknown"}`, and the
    # loop-console fleet status contract reads exactly those two values as the sentinel
    # for "the deploy layer did not inject the provenance".
    #
    # The `ldflag-symbols` gate resolves these paths against the go.mod files and the
    # AST, so the next package move fails the gate instead of breaking silently.
    LDFLAGS="-s -w \
      -X github.com/getloopscale/loop-runtime/api/internal/version/versionhttp.versionTag=${LOOP_RUNTIME_VERSION} \
      -X github.com/getloopscale/loop-runtime/api/internal/version/versionhttp.versionCommit=${LOOP_RUNTIME_COMMIT} \
      -X github.com/getloopscale/loop-runtime/api/internal/version/versionhttp.versionBuildDate=${LOOP_RUNTIME_BUILD_DATE}" \
    && cd api && CGO_ENABLED=0 go build -trimpath -ldflags="$LDFLAGS" -o /loop-runtime-api ./cmd/ \
    && CGO_ENABLED=0 go build -trimpath -ldflags="$LDFLAGS" -o /loop-runtime-controller ./cmd/controller/

# Build migrate from source (same Go toolchain) to avoid CVEs in pre-compiled binary.
FROM golang:1.26.11-alpine3.23@sha256:23f198931d4bd83b4f5a613bd75656257ae8bf832dcbc2db80e1050b5219ca2a AS migrate-builder
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go install -tags 'postgres' -trimpath -ldflags="-s -w" \
    github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1

FROM alpine:3.23@sha256:469c9ea410b9215eaee7976ba6c0639039b444be8e5d638f7abbada189971eb2

LABEL org.opencontainers.image.source="https://github.com/getloopscale/loop-runtime"
LABEL org.opencontainers.image.title="loop-runtime-api"
LABEL org.opencontainers.image.description="LoopRuntime PaaS API server"

RUN apk add --no-cache ca-certificates git && adduser -D -u 1000 loop-runtime
COPY --from=builder /loop-runtime-api /loop-runtime-api
COPY --from=builder /loop-runtime-controller /loop-runtime-controller
COPY --from=migrate-builder /go/bin/migrate /usr/local/bin/migrate
COPY --from=builder /workspace/api/internal/db/migrations/ /migrations/
USER 1000
EXPOSE 8080 9090 9091

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:8080/healthz || exit 1

CMD ["/loop-runtime-api"]
