// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"errors"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// cell_types.go — public struct + sentinel errors for the cell-
// aware placement primitives. T2.2 (Batch B #2) of plan
// 2026-05-29-sqlc-migration-plan v1.3.
//
// The 4 prod call sites (api/cmd/main.go:175,
// api/cmd/setup_stores.go:92, api/cmd/setup_server.go:231,
// placement.NewCellPinner consumer) all use struct-literal
// `&db.PostgresCellStore{DB: dbAdapter}` — preserved here.

// PostgresCellStore exposes cell-aware queries (PlacementEngine
// + admin pinner + cell metrics collector). T2.2 Batch B #2:
// DB field type preserved (db.DBTX); methods (cell_wrapper.go)
// dispatch through SqlcBridge to sqlc-generated queries.
type PostgresCellStore struct {
	DB db.DBTX
}

// Sentinel errors mirroring services.* so the cell_store doesn't import
// services (would cycle). The HTTP handler in admin_pin_cell.go matches
// against the strings, so the pinner service-side and db-side strings
// must stay in sync (test pinned in services/cell_pinner_test.go).
var (
	ErrCellPinnerTenantNotFound = errors.New("tenant not found")
	ErrCellPinnerCellNotFound   = errors.New("cell not found")
	ErrCellPinnerNotPinnable    = errors.New("cell not pinnable")
	ErrCellPinnerAtCapacity     = errors.New("cell at capacity")
)
