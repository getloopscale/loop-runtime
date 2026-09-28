// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release_test

import (
	"log/slog"
	"os"
)

// testLogger reports errors only, so a failing test still shows why.
var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
