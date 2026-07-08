// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import "os"

// osGetenv is a tiny indirection so tests can override env resolution via
// package-level var without importing os in every seed test file.
var osGetenv = os.Getenv
