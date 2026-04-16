# ADR 0003: Kaniko for container image builds

## Status
Superseded — actual implementation uses BuildKit (moby/buildkit:v0.18.2-rootless).

## Context
Building container images in Kubernetes requires a tool that runs without Docker socket access. Options: Kaniko, BuildKit, Buildpacks.

## Original Decision (Superseded)
Use Kaniko (gcr.io/kaniko-project/executor). It runs as a regular container without privileged mode, builds Dockerfiles, and pushes directly to registry.

## Actual Implementation
The platform uses **moby/buildkit:v0.18.2-rootless** instead of Kaniko. BuildKit was chosen during implementation because:
- Runs rootless (no root inside the container, unlike Kaniko which requires root for layer manipulation)
- Better build cache support with inline and registry-backed caching
- SLSA provenance and SBOM attestation built into BuildKit natively
- Faster builds in practice due to better parallelism in Dockerfile stage execution
- Active upstream maintenance and first-class Docker/OCI support

The original decision to avoid Docker socket access remains valid — BuildKit achieves the same goal with stronger security posture (rootless vs root).

## Original Consequences (Historical)
- No Docker socket required (security benefit)
- Runs as root inside its container (required for image layer manipulation)
- Dockerfile-based builds (familiar to developers)
- Cache layers stored in registry (MinIO S3-compatible)
- Internal term "Kaniko" must never appear in user-facing output — use "build" instead

## Current Consequences
- No Docker socket required (security benefit)
- Runs as non-root (rootless mode, stronger security than Kaniko)
- Dockerfile-based builds (familiar to developers)
- Registry-backed cache layers with mode=max for optimal layer reuse
- SLSA provenance and SBOM attestation generated at build time
- Internal term "BuildKit" must never appear in user-facing output — use "build" instead
