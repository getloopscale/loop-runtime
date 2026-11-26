// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package previewadapter holds the driven adapters of the preview feature: the code that satisfies
// the preview service's app lookup and PreviewEnvironment CR ports with persistence, Kubernetes or another feature's service. Only the
// composition roots import it; each adapter declares the port it satisfies at
// compile time (adapter-ports).
package previewadapter
