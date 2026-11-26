// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package releaseadapter holds the driven adapters of the release feature: the code that satisfies
// the promotion and rollback handlers' service ports, the rollback release lookup, and the ArgoCD Application label read with persistence, Kubernetes or another feature's service. Only the
// composition roots import it; each adapter declares the port it satisfies at
// compile time (adapter-ports).
package releaseadapter
