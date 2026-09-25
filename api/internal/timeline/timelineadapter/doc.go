// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package timelineadapter holds the driven adapters of the deploy-timeline feature: the code that satisfies
// the aggregator's event-stream read with persistence, Kubernetes or another feature's service. Only the
// composition roots import it; each adapter declares the port it satisfies at
// compile time (adapter-ports).
package timelineadapter
