// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server_test

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/secrets/secretshttp"
)

// fakeSecretStore implements secrets.SecretStore for testing.
type fakeSecretStore struct {
	secrets []secretshttp.SecretMetadata
	err     error
}

func (f *fakeSecretStore) ListNames(ctx context.Context, tenantID, projectID, env string) ([]secretshttp.SecretMetadata, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.secrets, nil
}

func (f *fakeSecretStore) Set(_ context.Context, _, _, _, _, _ string, _ []byte) (bool, error) {
	return true, nil
}

func (f *fakeSecretStore) Delete(_ context.Context, _, _, _, _, _ string) (bool, error) {
	return true, nil
}
