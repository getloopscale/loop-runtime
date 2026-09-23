// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/buildplane/admission"
	"github.com/getloopscale/loopzero/api/internal/db"
)

// BuildStoreAdmissionQuerier bridges the db.BuildStore interface to the
// admission gate's ActiveBuildsQuerier contract. Roadmap 07.
//
// T1.2b (plan 2026-05-29-sqlc-migration v1.3): field type widened from
// concrete *PostgresBuildStore (now deleted) to db.BuildStore interface
// so BuildStoreSqlc + any future implementation drops in without churn.
// Only FindActiveBuilds is consumed here — interface stays minimal at
// the seam.
type BuildStoreAdmissionQuerier struct {
	Store db.BuildStore
}

func (q *BuildStoreAdmissionQuerier) ActiveBuilds(ctx context.Context, cellID string) (admission.ActiveBuildsSnapshot, error) {
	rows, err := q.Store.FindActiveBuilds(ctx, cellID)
	if err != nil {
		return admission.ActiveBuildsSnapshot{}, err
	}
	byTenant := make(map[string]int, len(rows))
	for _, b := range rows {
		byTenant[b.TenantID]++
	}
	return admission.ActiveBuildsSnapshot{
		Total:    len(rows),
		ByTenant: byTenant,
	}, nil
}

// The port this adapter satisfies, asserted at compile time.
var _ admission.ActiveBuildsQuerier = (*BuildStoreAdmissionQuerier)(nil)
