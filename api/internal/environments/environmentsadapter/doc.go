// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package environmentsadapter holds the driven adapters of the environment feature: the code that satisfies
// the environment handlers' project lookup, dashboard KPI and preview-slot ports, the readiness check the environment ensurer polls, and the Prometheus source of the data-plane readiness poller, with persistence, Kubernetes, Prometheus or another feature's service. Only the
// composition roots import it; each adapter declares the port it satisfies at
// compile time (adapter-ports).
package environmentsadapter
