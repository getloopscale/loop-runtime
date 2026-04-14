# loopzero-packs-runner: Analyzes source code and generates optimized Dockerfiles.
# Replaces hardcoded bash Dockerfile templates (MEET-2026-03-29-017).
FROM alpine:3.21.3

LABEL org.opencontainers.image.source="https://github.com/loopzero/loopzero"
LABEL org.opencontainers.image.title="loopzero-packs-runner"

RUN adduser -D -u 1000 loopzero
COPY loopzeropacks-generate /usr/local/bin/loopzeropacks-generate
RUN chmod +x /usr/local/bin/loopzeropacks-generate
USER 1000
