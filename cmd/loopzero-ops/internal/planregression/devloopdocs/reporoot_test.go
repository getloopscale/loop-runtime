// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package devloopdocs

import "github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"

// repoRoot is the location-independent monorepo root (M9 Step 4 externalization;
// replaces the former hardcoded "../../../" offset that broke on relocation).
var repoRoot = qtestutil.MustRepoRoot()
