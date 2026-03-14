// Package source provides abstractions for the source code storage backend.
package source

import (
	"context"
	"io"
)

// SourceStore abstracts the Git storage backend (Gitea in production).
type SourceStore interface {
	// PushBundle pushes a git bundle to the tenant's project repository.
	// Returns the commit SHA from the pushed bundle.
	PushBundle(ctx context.Context, tenantID, projectID string, bundle io.Reader) (commitSHA string, err error)
}
