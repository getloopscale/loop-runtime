// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"io"
	"log/slog"
)

// testLogger returns a discarding slog.Logger for tests. M9 Step 6: the shared
// services-level test helper (the placement context kept its own copy in
// cross_cell_aggregator_test.go when it moved).
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
