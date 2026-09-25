// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package integrationsadapter holds the driven adapters of the integrations feature: the code that satisfies
// the GitHub installation lookups the integrations handler reads with persistence, Kubernetes or another feature's service. Only the
// composition roots import it; each adapter declares the port it satisfies at
// compile time (adapter-ports).
package integrationsadapter
