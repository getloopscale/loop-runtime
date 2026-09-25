// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package placementadapter holds the driven adapters of the placement feature: the code that satisfies
// the tenant evictor's database and Kubernetes ports, the cell pinner's Tenant CR patch, and the eviction port of placementhttp with persistence, Kubernetes or another feature's service. Only the
// composition roots import it; each adapter declares the port it satisfies at
// compile time (adapter-ports).
package placementadapter
