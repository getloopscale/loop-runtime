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
	imageAlpine   = "alpine:3.19.1"
	imageRuby     = "ruby:3.3-alpine"
	imageBuildKit = "moby/buildkit:v0.18.2-rootless"
	imageAWSCLI   = "amazon/aws-cli:2.22.35"

	// loopzero-packs-runner generates optimized Dockerfiles by analyzing source code.
	// Replaces hardcoded bash Dockerfile templates (MEET-2026-03-29-017).
	// Built from: loopzero-packs/Dockerfile.generate — CI builds via build-images.yml.
	// COMP-004 fix: pinned to v0.1.0 (was :latest — non-deterministic builds).
	// Local dev: override via LOOPZERO_PACKS_IMAGE env or use k3d-loopzero-registry:5000/loopzero-packs-runner:latest
	imageLoopZeroPacks = "registry.digitalocean.com/loopzero-infra-registry/loopzero-packs-runner:v0.2.0"
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
	secretRegistryConfig = "buildkit-registry-config"
	secretMinIOCreds     = "loopzero-minio-credentials"
	s3BucketStaticAssets = "loopzero-static-assets"
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
