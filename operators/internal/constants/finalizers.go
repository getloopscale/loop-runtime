// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package constants

// Finalizer names for each CRD's verify-then-remove cleanup pattern.
// Each controller uses exactly one finalizer — changing these strings
// requires a migration strategy for existing resources.
const (
	FinalizerTenant               = "loopzero.io/tenant-cleanup"
	FinalizerEnvironment          = "loopzero.io/environment-cleanup"
	FinalizerApplication          = "loopzero.io/app-cleanup"
	FinalizerExposedRoute         = "loopzero.io/route-cleanup"
	FinalizerClusterCredential    = "loopzero.io/clustercredential-cleanup"
	FinalizerGitRepositoryBinding = "platform.loopzero.io/repo-binding-cleanup"
	FinalizerPlatformBootstrap    = "loopzero.io/bootstrap-cleanup"
	FinalizerPreview              = "loopzero.io/preview-cleanup"
)
