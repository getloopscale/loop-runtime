// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

// ---------------------------------------------------------------------------
// Image versions — single source of truth for all build container images.
// Update ONE constant to bump the version across all language strategies.
// ---------------------------------------------------------------------------

const (
	imageNode     = "node:20.11.1-alpine"
	imageGolang   = "golang:1.22-alpine"
	imagePython   = "python:3.12-alpine"
	imageAlpine   = "alpine:3.21.3"
	imageRuby     = "ruby:3.3-alpine"
	imageBuildKit = "moby/buildkit:v0.18.2-rootless"
	imageAWSCLI   = "amazon/aws-cli:2.22.35"

	// loopzero-cosign-signer: Pre-built Alpine image with cosign binary baked in.
	// SEC-006: No runtime downloads — cosign is checksum-verified at image build time.
	// Built from: infra/images/cosign-signer/Dockerfile — CI builds via build-images.yml.
	imageCosignSigner = "registry.digitalocean.com/loopzero-system/loopzero-cosign-signer:v0.1.0"

	// loopzero-syft-scanner: Pre-built Alpine image with syft binary baked in.
	// SEC-006: No runtime downloads — syft is checksum-verified at image build time.
	// Built from: infra/images/syft-scanner/Dockerfile — CI builds via build-images.yml.
	// Sprint 6 / T0.2 — used by sbom-{app} step to generate CycloneDX SBOMs.
	imageSyftScanner = "registry.digitalocean.com/loopzero-system/loopzero-syft-scanner:v0.1.0"

	// loopzero-packs-runner generates optimized Dockerfiles by analyzing source code.
	// Replaces hardcoded bash Dockerfile templates (MEET-2026-03-29-017).
	// Built from: loopzero-packs repo — CI builds via build-runner.yml.
	// build-mode-dispatch-and-dockerfile-security-plan T3.1 (2026-05-07):
	// pinned to v0.4.0 — first version with lenient user-Dockerfile contract
	// canonically documented. v0.2.0/v0.3.x had implicit-strict regression
	// that errored on user-supplied Dockerfile.
	// Local dev: override via LOOPZERO_PACKS_IMAGE env or use k3d-loopzero-registry:5000/loopzero-packs-runner:v0.4.0
	imageLoopZeroPacks = "registry.digitalocean.com/loopzero-system/loopzero-packs-runner:v0.4.0"

	// loopzero-dockerfile-lint: static analyzer for user-supplied Dockerfiles.
	// build-mode-dispatch-and-dockerfile-security-plan Phase 2 (2026-05-07).
	// Runs as a step in user-Dockerfile builds BEFORE BuildKit. Hard rejection
	// rules (FROM allow-list, RUN flag prohibitions, bind mount escape, reserved
	// env keys, seccomp/apparmor unconfined) cause build to Fail. Soft warnings
	// captured by Loki for audit.
	// Built from: tools/loopzero-dockerfile-lint/Dockerfile — CI builds via build-images.yml.
	// Local dev: override via LOOPZERO_DOCKERFILE_LINT_IMAGE env.
	imageDockerfileLint = "registry.digitalocean.com/loopzero-system/loopzero-dockerfile-lint:v0.1.0"
)

// ---------------------------------------------------------------------------
// Workflow defaults
// ---------------------------------------------------------------------------

const (
	workflowParallelism = 4
	serviceAccountName  = "loopzero-build-sa"
	entrypointName      = "build-pipeline"
	onExitDAGName       = "finalize"
)

// ---------------------------------------------------------------------------
// TTL values (seconds)
// ---------------------------------------------------------------------------

const (
	ttlAfterCompletion = 14400 // 4 hours — post-deploy debugging window
	ttlAfterSuccess    = 14400 // 4 hours
	ttlAfterFailure    = 86400 // 24 hours — incident investigation window
)

// ---------------------------------------------------------------------------
// Volume / Secret / Bucket names
// ---------------------------------------------------------------------------

const (
	volumeWorkspace      = "workspace"
	workspacePVCSize     = "5Gi"
	workspaceMountPath   = "/workspace"
	secretRegistryConfig = "buildkit-registry-config" //nolint:gosec // G101 false positive: Secret resource NAME, not credential value
	secretMinIOCreds      = "loopzero-minio-credentials" //nolint:gosec // G101 false positive: Secret resource NAME, not credential value
	secretCosignKey       = "cosign-signing-key"
	cosignKeyMountPath    = "/cosign"
	// Sigstore keyless mode: workflow-level projected SA token volume + mount.
	// Mounted at /var/run/secrets/sigstore/token (Sigstore standard path).
	// Added pod-level via buildVolumes() when CosignKeyless=true.
	sigstoreTokenVolumeName = "sigstore-token"
	sigstoreTokenMountPath  = "/var/run/secrets/sigstore" //nolint:gosec // G101 filesystem PATH, not credential value
	s3BucketStaticAssets  = "loopzero-static-assets"
)

// ---------------------------------------------------------------------------
// BuildKit
// ---------------------------------------------------------------------------

const (
	buildkitSocketPath     = "unix:///run/user/1000/buildkitd/buildkitd.sock"
	buildkitDaemonTimeout  = 60
	buildkitRegistryMount  = "/home/user/.docker"
)

// ---------------------------------------------------------------------------
// Security — rootless container IDs
// ---------------------------------------------------------------------------

const rootlessUID = int64(1000)

// ---------------------------------------------------------------------------
// Labels
// ---------------------------------------------------------------------------

const (
	labelTenant      = "loopzero.io/tenant"
	labelProject     = "loopzero.io/project"
	labelEnvironment = "loopzero.io/environment"
	labelBuildID     = "loopzero.io/build-id"
	labelLanguage    = "loopzero.io/language"
)
