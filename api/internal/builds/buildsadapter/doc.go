// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package buildsadapter holds the driven adapters of the builds feature: the code that satisfies
// the build handlers' ports (orchestrator, list, stats, SBOM), the admission and step-watch ports, the Application CR the build ensures, and the GitHub webhook ports the build store and orchestrator answer with persistence, Kubernetes or another feature's service. Only the
// composition roots import it; each adapter declares the port it satisfies at
// compile time (adapter-ports).
package buildsadapter
