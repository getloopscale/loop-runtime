// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package source provides abstractions for the source code storage backend.
package source

import (
	"context"
	"io"
)

// SourceStore abstracts the Git storage backend (Gitea in production).
type SourceStore interface {
	// PushBundle pushes a source bundle (git bundle or tar.gz) to the tenant's project repository.
	// Returns the source identifier (git SHA or "ch-{content-hash}") and optionally
	// the path to a git bundle file for S3 upload (empty if the original was already a git bundle).
	PushBundle(ctx context.Context, tenantID, projectID string, bundle io.Reader) (sourceID string, err error)

	// CreateGitBundleFromRepo creates a git bundle from the Gitea repo for S3 upload.
	// Used when the original source was tar.gz — the CWT expects a git bundle in S3.
	// Returns the path to the temporary git bundle file (caller must clean up).
	CreateGitBundleFromRepo(ctx context.Context, tenantID, projectID string) (bundlePath string, err error)
}
