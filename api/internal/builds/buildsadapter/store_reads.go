// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package buildsadapter

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// The build and release stores declare 17 and 15 methods; each adapter here
// uses one to three of them. These are the reads the adapters in this package
// depend on, declared from the consumer's side (ISP): a test fake implements
// what the adapter calls, and an adapter that is only given a tenant-scoped read
// cannot reach the unscoped one.

// buildByID is the UNSCOPED build read. Only adapters that already resolve
// the tenant elsewhere (or that are not reachable from a request) take it.
type buildByID interface {
	GetByID(ctx context.Context, id string) (*db.Build, error)
}

// tenantBuildByID is the tenant-scoped build read.
type tenantBuildByID interface {
	GetByIDForTenant(ctx context.Context, tenantID, id string) (*db.Build, error)
}

type buildApps interface {
	GetBuildApps(ctx context.Context, buildID string) ([]db.BuildAppSpec, error)
}

type envBuilds interface {
	ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]db.Build, int, error)
	ListEnvsForProject(ctx context.Context, tenantID, projectName string) ([]string, error)
}

type activeBuilds interface {
	FindActiveBuilds(ctx context.Context, cellID string) ([]db.Build, error)
}

// Compile-time proof the production stores still provide every read.
var (
	_ interface {
		buildByID
		tenantBuildByID
		buildApps
		envBuilds
		activeBuilds
	} = db.BuildStore(nil)
)
