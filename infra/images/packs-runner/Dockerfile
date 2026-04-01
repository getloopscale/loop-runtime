# loopzero-packs-runner: Analyzes source code and generates optimized Dockerfiles.
# Replaces hardcoded bash Dockerfile templates (MEET-2026-03-29-017).
#
# Usage: loopzeropacks-generate --source /workspace --app-path . --app-name myapp --output /workspace/Dockerfile
#
# Behavior:
#   - If user-provided Dockerfile exists at app-path, copies it to output (skip generation)
#   - Detects monorepo via workspaces in root package.json
#   - Detects framework: express, fastify, nextjs, node, go
#   - Generates Dockerfile optimized for the detected framework
#
# Size: ~5MB (Alpine + shell script)
# Security: runs as non-root user 1000 (loopzero)
FROM alpine:3.21
RUN adduser -D -u 1000 loopzero
COPY loopzeropacks-generate /usr/local/bin/loopzeropacks-generate
RUN chmod +x /usr/local/bin/loopzeropacks-generate
USER 1000
