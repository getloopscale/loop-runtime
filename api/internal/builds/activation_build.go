// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// activationBuildFrom projects a persisted build row onto the delivery
// contract's own type.
//
// This adapter is where the sink stopped importing persistence (B-079): the
// build context knows db.Build, deploycontracts does not, and the translation
// happens once here instead of leaking the row to the 38 production files, in
// 11 packages, that depend on the sink. (35-in-10 was the figure carried in
// this tree since 2026-08-12; re-measured 2026-09-04. It was also written
// here as "35 packages", which mixed the two units.) A nil row yields the zero value rather than panicking — the
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
