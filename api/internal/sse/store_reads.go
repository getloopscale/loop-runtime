// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package sse

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

// Compile-time proof the production stores still provide every read.
var (
	_ interface {
		buildByID
	} = db.BuildStore(nil)
)
