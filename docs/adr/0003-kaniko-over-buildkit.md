# ADR 0003: Kaniko for container image builds

## Status
Accepted

## Context
Building container images in Kubernetes requires a tool that runs without Docker socket access. Options: Kaniko, BuildKit, Buildpacks.

## Decision
Use Kaniko (gcr.io/kaniko-project/executor). It runs as a regular container without privileged mode, builds Dockerfiles, and pushes directly to registry.

## Consequences
- No Docker socket required (security benefit)
- Runs as root inside its container (required for image layer manipulation)
- Dockerfile-based builds (familiar to developers)
- Cache layers stored in registry (MinIO S3-compatible)
- Internal term "Kaniko" must never appear in user-facing output — use "build" instead
