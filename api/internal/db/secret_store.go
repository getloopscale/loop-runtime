package db

import (
	"context"

	"github.com/loopzero/loopzero/api/internal/routes"
)

// EmptySecretStore is a Sprint 2 placeholder that always returns an empty list.
// Real secret storage will be implemented in Sprint 3 (secrets management).
type EmptySecretStore struct{}

// ListNames returns an empty list of secret metadata.
// This satisfies the routes.SecretStore interface for Sprint 2.
func (s *EmptySecretStore) ListNames(_ context.Context, _, _, _ string) ([]routes.SecretMetadata, error) {
	return []routes.SecretMetadata{}, nil
}
