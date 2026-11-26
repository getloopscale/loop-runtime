// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package domainsadapter holds the driven adapters of the custom-domain feature: the code that satisfies
// the domain handlers' Cloudflare-zone and DNS TXT verification ports with persistence, Kubernetes or another feature's service. Only the
// composition roots import it; each adapter declares the port it satisfies at
// compile time (adapter-ports).
package domainsadapter
