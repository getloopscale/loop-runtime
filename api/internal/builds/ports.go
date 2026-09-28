// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/ports"
)

// AttestationStore is the minimal contract for reading attestation data.
//
// The only read is tenant-scoped on purpose: a build belongs to one tenant,
// and a build owned by another tenant must be indistinguishable from one that
// does not exist (ErrBuildNotFound, ADR-0014). There is deliberately no
// caller-trusted variant — the one that existed was what let
// GET /builds/{id}/attestation disclose any tenant's signing data.
type AttestationStore interface {
	GetBuildAttestationForTenant(ctx context.Context, tenantID, buildID string) (*ports.AttestationInfo, error)
}
