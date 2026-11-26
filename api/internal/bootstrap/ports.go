// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package bootstrap is the platform-bootstrap feature: placing a new tenant in a
// cell and creating it there. Its handler lives in bootstraphttp.
package bootstrap

import "context"

// CellPlacementEngine assigns tenants to Cells.
// ADR-0008: Phase 0 = DefaultEngine (always "cell-0").
type CellPlacementEngine interface {
	AssignCell(ctx context.Context) (cellID string, err error)
	// AssignAndExecute picks a cell AND runs the callback in the same serialized
	// transaction. MEET-053 D-01: fixes TOCTOU between AssignCell and INSERT.
	AssignAndExecute(ctx context.Context, fn func(ctx context.Context, cellID string) error) error
}
