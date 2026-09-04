// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package build

import (
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/services/deploycontracts"
)

// activationBuildFrom projects a persisted build row onto the delivery
// contract's own type.
//
// This adapter is where the sink stopped importing persistence (B-079): the
// build context knows db.Build, deploycontracts does not, and the translation
// happens once here instead of leaking the row to all 35 packages that depend
// on the sink. A nil row yields the zero value rather than panicking — the
// reconcile loop must fail a build, not crash on one.
func activationBuildFrom(b *db.Build) deploycontracts.ActivationBuild {
	if b == nil {
		return deploycontracts.ActivationBuild{}
	}
	return deploycontracts.ActivationBuild{
		ID:            b.ID,
		TenantID:      b.TenantID,
		ProjectID:     b.ProjectID,
		Environment:   b.Environment,
		CreatedBy:     b.CreatedBy,
		CorrelationID: b.CorrelationID,
		CreatedAt:     b.CreatedAt,
	}
}
