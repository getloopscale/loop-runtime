// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statusadapter

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// The build and release stores declare 17 and 15 methods; each adapter here
// uses one to three of them. These are the reads the adapters in this package
// depend on, declared from the consumer's side (ISP): a test fake implements
// what the adapter calls, and an adapter that is only given a tenant-scoped read
// cannot reach the unscoped one.

type projectBuilds interface {
	ListByProject(ctx context.Context, tenantID, projectID string, limit, offset int) ([]db.Build, error)
}

type activeReleaseByEnv interface {
	GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*db.Release, error)
}

// Compile-time proof the production stores still provide every read.
var (
	_ interface {
		projectBuilds
	} = db.BuildStore(nil)
	_ interface {
		activeReleaseByEnv
	} = db.ReleaseStore(nil)
)
