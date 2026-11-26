// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package secrets is the secret-management feature: storing a tenant's secrets
// and delivering them to its workloads. Its handlers live in secretshttp.
package secrets

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// SecretStore abstracts secret persistence (DIP).
// Implementations MUST NOT expose secret values in list operations.
type SecretStore interface {
	ListNames(ctx context.Context, tenantID, projectID, env string) ([]ports.SecretMetadata, error)
	Set(ctx context.Context, tenantID, projectID, env, app, name string, value []byte) (created bool, err error)
	Delete(ctx context.Context, tenantID, projectID, env, app, name string) (deleted bool, err error)
}

// SecretK8sSync abstracts K8s Secret + ExternalSecret CR creation (DIP).
// ref.Project is the canonical M2 namespace segment (per
// pkg/naming.Namespace); the runtime ExternalSecret CR lives at
// loopzero-{tenant}-{project}-{env}.
type SecretK8sSync interface {
	CreateSecret(ctx context.Context, ref ports.SecretRef, value string) error
	DeleteSecret(ctx context.Context, ref ports.SecretRef) error
}
