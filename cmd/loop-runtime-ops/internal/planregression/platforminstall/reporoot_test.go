// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package platforminstall

import "github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"

// repoRoot is the location-independent monorepo root (M9 Step 4 externalization;
// replaces the former hardcoded "../../../" offset that broke on relocation).
var repoRoot = qtestutil.MustRepoRoot()
