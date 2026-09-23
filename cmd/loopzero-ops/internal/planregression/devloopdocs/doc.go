// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package devloopdocs guards that the documentation keeps the one-shot
// `task setup` and the `tilt up` dev loop distinct, and that `task setup`
// points operators at the dev loop.
//
// Test-only package: it reads repository files and has no production code.
package devloopdocs
