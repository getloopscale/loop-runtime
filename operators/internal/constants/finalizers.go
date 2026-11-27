// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package constants

// Finalizer names for each CRD's verify-then-remove cleanup pattern.
// Each controller uses exactly one finalizer — changing these strings
// requires a migration strategy for existing resources.
const (
	FinalizerTenant               = "runtime.getloopscale.com/tenant-cleanup"
	FinalizerEnvironment          = "runtime.getloopscale.com/environment-cleanup"
	FinalizerApplication          = "runtime.getloopscale.com/app-cleanup"
	FinalizerExposedRoute         = "runtime.getloopscale.com/route-cleanup"
	FinalizerClusterCredential    = "runtime.getloopscale.com/clustercredential-cleanup"
	FinalizerGitRepositoryBinding = "platform.runtime.getloopscale.com/repo-binding-cleanup"
	FinalizerPlatformBootstrap    = "runtime.getloopscale.com/bootstrap-cleanup"
	FinalizerPreview              = "runtime.getloopscale.com/preview-cleanup"
)
