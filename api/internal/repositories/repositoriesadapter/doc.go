// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package repositoriesadapter holds the driven adapters of the repositories feature: the code that satisfies
// the repositories handler's store and the GitHub webhook's repository lookups with persistence, Kubernetes or another feature's service. Only the
// composition roots import it; each adapter declares the port it satisfies at
// compile time (adapter-ports).
package repositoriesadapter
