# loop-edge: the Caddy that serves tenants' custom domains.
#
# It is stock Caddy plus one module, the Redis storage of github.com/pberkel/caddy-storage-redis.
# A fleet of Caddy instances shares its certificates through its storage, and the storage Caddy
# ships with is a directory. With this module the certificates and the locks that keep two
# instances from asking for the same certificate live in the platform's Valkey
# (docs/decisions/2026-11-26-customer-domains-at-the-edge.md).
#
# Both versions are exact. xcaddy resolves them through the Go module proxy and checks every
# module against the Go checksum database, so a tag moved after its first use fails the build.
# The module needs Caddy v2.11.1 or newer (its README, read on 2026-11-27).
FROM mirror.gcr.io/library/caddy:2.11.9-builder@sha256:cda07fe9d82728c7ac44d844306d435e98131423dc7b114f05a8a8ecfff94707 AS build

ARG CADDY_VERSION=v2.11.9
ARG STORAGE_REDIS_VERSION=v1.9.0
# Caddy v2.11.9 requires golang.org/x/net v0.60.0, which trivy reports for CVE-2026-78661
# (HIGH, fixed in v0.62.0; measured 2026-11-27). Drop this when a Caddy release requires
# v0.62.0 or newer.
ARG X_NET_VERSION=v0.62.0

# The builder image sets XCADDY_SETCAP=1, which makes xcaddy give the binary the
# cap_net_bind_service file capability. Caddy listens on unprivileged ports here, and a
# container that runs with no capability and no privilege escalation cannot start a binary
# that carries one.
RUN XCADDY_SETCAP=0 xcaddy build "${CADDY_VERSION}" \
      --with "github.com/pberkel/caddy-storage-redis@${STORAGE_REDIS_VERSION}" \
      --replace "golang.org/x/net=golang.org/x/net@${X_NET_VERSION}" \
      --output /out/caddy \
    && /out/caddy version \
    && /out/caddy list-modules | grep -q '^caddy.storage.redis$'

FROM mirror.gcr.io/library/alpine:3.23@sha256:469c9ea410b9215eaee7976ba6c0639039b444be8e5d638f7abbada189971eb2

ARG CADDY_VERSION=v2.11.9
ARG STORAGE_REDIS_VERSION=v1.9.0

LABEL org.opencontainers.image.source="https://github.com/getloopscale/loop-runtime" \
      org.opencontainers.image.title="loop-edge" \
      io.loop-runtime.caddy.version="${CADDY_VERSION}" \
      io.loop-runtime.caddy-storage-redis.version="${STORAGE_REDIS_VERSION}"

COPY --from=build /out/caddy /usr/bin/caddy

# Caddy listens on unprivileged ports (the chart sets http_port and https_port), so the
# binary needs no capability and the image no root.
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 1000 loop-edge

USER 1000
ENTRYPOINT ["/usr/bin/caddy"]
CMD ["run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
