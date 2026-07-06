// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"io"
	"log/slog"
)

// testLogger returns a discarding slog.Logger for release-context tests. M9
// Step 6: each services subpackage keeps its own copy of this trivial helper.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
