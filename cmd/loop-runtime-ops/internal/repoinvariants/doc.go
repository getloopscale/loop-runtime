// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package repoinvariants holds tests over the REPOSITORY rather than over
// loop-runtime-ops code: CODEOWNERS paths, workflow commit SHAs, .gitignore shadowing,
// Taskfile `cd` targets and rendering, the CI observer scripts, and the
// readers of infra/quality/.
//
// They lived in package main's test files, where they made loop-runtime-ops' own unit
// tests depend on the whole checkout and on external binaries (git, grep,
// python3, task). They are kept together here so `go test ./...` of this module
// still runs them, and so a reader can tell a repository invariant from a test
// of the code beside it.
package repoinvariants
