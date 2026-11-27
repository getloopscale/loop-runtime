// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package source provides abstractions for the source code storage backend.
package source

import (
	"context"
	"io"
)

// PushResult holds the result of a PushBundle operation.
type PushResult struct {
	// SourceID is the git SHA (for git bundles) or "ch-{hash}" (for tar.gz).
	SourceID string
	// GitBundlePath is the path to a git bundle file for S3 upload.
	// Non-empty only for tar.gz sources (where the original wasn't a git bundle).
	// The caller must clean up the parent directory when done.
	GitBundlePath string
}

// SourceStore abstracts the Git storage backend (Gitea in production).
type SourceStore interface {
	// PushBundle pushes a source bundle (git bundle or tar.gz) to the tenant's project repository.
	// For tar.gz sources, it also creates a git bundle for S3 upload (returned in GitBundlePath).
	// For git bundle sources, GitBundlePath is empty (the original bundle is reused for S3).
	PushBundle(ctx context.Context, tenantID, projectID string, bundle io.Reader) (PushResult, error)
}
