// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package routeauthcoverage

import "github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"

// repoRoot is the location-independent monorepo root — the same helper the sibling
// gates use, so a relocation does not silently turn a real-tree assertion into a
// scan of nothing.
var repoRoot = qtestutil.MustRepoRoot()
