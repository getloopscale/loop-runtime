// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build tools

package operators

// This file declares tool dependencies that are not directly imported by Go source
// but are required by the project (e.g., controller-runtime for operator development).
// See https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module

import (
	_ "sigs.k8s.io/controller-runtime"
)
