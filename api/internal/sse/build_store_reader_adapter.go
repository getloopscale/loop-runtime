// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package sse

import (
	"context"
)

// BuildStoreReaderAdapter implements sse.BuildStoreReader using db.BuildStore.
// The SSE stream handler needs only GetByID returning sse.BuildInfo,
// while db.BuildStore returns the full db.Build struct.
type BuildStoreReaderAdapter struct {
	Store buildByID
}

func (a *BuildStoreReaderAdapter) GetByID(ctx context.Context, id string) (*BuildInfo, error) {
	build, err := a.Store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &BuildInfo{
		ID:       build.ID,
		TenantID: build.TenantID,
		Status:   build.Status,
	}, nil
}

// GetTenantForBuild satisfies the aggregator.BuildLookup contract used by
// PostgresDeployTimelineStore (plan T5.1 wiring). Returns the empty
// string when the build is not found OR an error occurs — the caller
// translates that into routes.ErrDeployNotFound (existence-leak guard).
func (a *BuildStoreReaderAdapter) GetTenantForBuild(ctx context.Context, buildID string) (string, error) {
	build, err := a.Store.GetByID(ctx, buildID)
	if err != nil {
		return "", err
	}
	if build == nil {
		return "", nil
	}
	return build.TenantID, nil
}

// The port this adapter satisfies, asserted at compile time.
//
// B-058: the package is called `adapters` and its admission criterion was "it is an adapter" —
// which refuses nothing. Measured on 2026-08-18: of the nine adapters, five NAMED their port in
// prose and only ONE verified it. Prose does not compile; this line does, and it is what lets
// the criterion say no.
var _ BuildStoreReader = (*BuildStoreReaderAdapter)(nil)
