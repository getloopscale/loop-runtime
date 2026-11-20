// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

// ---------------------------------------------------------------------------
// Image versions — single source of truth for all build container images.
// Update ONE constant to bump the version across all language strategies.
// ---------------------------------------------------------------------------

const (
	// Canonical 2026-06-16: Node 22.12.0 LTS is the minimum supported by
	// `loopzerokit` (LoopZeroKit's package preflight rejects Node < 22.12 with
	// "requires Node >= 22.12.0" because it uses AsyncLocalStorage + native
	// fetch + structured WHATWG streams optimizations that depend on Node 22
	// internals — older Node versions silently miscompile SSR streaming and
	// NODE_MODULE_VERSION mismatch breaks native bindings). Bumping to 22.12
	// to match the canonical LoopZeroKit framework requirement.
	// Canonical 2026-06-16: node:22.12.0 (full Debian bookworm) for glibc
	// compatibility with rollup native binaries (Alpine + musl breaks them
	// per npm bug #4828). Every `set -<flags>o pipefail` form — the one
	// shellgen.SetE emits and the ones scripts.go writes by hand — is sanitised
	// in wrapWithLoopZeroMarkers (loopzero_log.go) to run under /bin/sh -> dash on Debian.
	//
	// The previous statement covered only shellgen.SetE's literal form, and was
	// therefore false: scripts.go writes `set -uo pipefail` without the `e`, that
	// form escaped the sanitiser, and under dash the illegal option KILLS the
	// shell on line 1 with status 2. It stayed invisible while the status died in
	// the tee pipe (B-153).
	imageNode     = "node:22.12.0"
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
	// T4.1 (plan simplify-deploy-loopzerokit-only v2.0): imageLoopZeroPacks +
	// imageDockerfileLint removed — loopzero-packs generation (T2.1) and the
	// BYO-Dockerfile lint path (T2.2) are gone. The build uses canonical
	// platform Dockerfiles generated in-process (canonical_dockerfile.go).
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
	secretRegistryConfig = "buildkit-registry-config"   //nolint:gosec // G101 false positive: Secret resource NAME, not credential value
	secretMinIOCreds     = "loopzero-minio-credentials" //nolint:gosec // G101 false positive: Secret resource NAME, not credential value
	secretCosignKey      = "cosign-signing-key"
	cosignKeyMountPath   = "/cosign"
	// Sigstore keyless mode: workflow-level projected SA token volume + mount.
	// Mounted at /var/run/secrets/sigstore/token (Sigstore standard path).
	// Added pod-level via buildVolumes() when CosignKeyless=true.
	sigstoreTokenVolumeName = "sigstore-token"
	sigstoreTokenMountPath  = "/var/run/secrets/sigstore" //nolint:gosec // G101 filesystem PATH, not credential value
	s3BucketStaticAssets    = "loopzero-static-assets"
)

// ---------------------------------------------------------------------------
// BuildKit
// ---------------------------------------------------------------------------

const (
	buildkitSocketPath    = "unix:///run/user/1000/buildkitd/buildkitd.sock"
	buildkitDaemonTimeout = 60
	buildkitRegistryMount = "/home/user/.docker"
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
