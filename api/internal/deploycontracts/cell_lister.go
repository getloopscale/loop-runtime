// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package deploycontracts

import "context"

// CellLister returns the list of active cell IDs.
// MEET-053 A-07: Used by BuildReconcileLoop to process all cells.
type CellLister interface {
	ListCellIDs(ctx context.Context) ([]string, error)
}
