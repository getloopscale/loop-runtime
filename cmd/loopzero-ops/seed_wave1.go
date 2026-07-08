// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import "github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/seed"

// init registers Wave 1 (K8s-Secret) + Wave 2 (HTTP-API port-forward) seeds.
// Registration order = execution order for `seed all` (deterministic, D3).
func init() {
	// Wave 1 — K8s-Secret CreateOrUpdate mechanic
	seedRegistry.Register(seed.NewGitHubAppDevSeed())
	seedRegistry.Register(seed.NewStripeWebhookSecretSeed())
	seedRegistry.Register(seed.NewStripeAPIKeysSeed())
	seedRegistry.Register(seed.NewArgoCDGiteaCredsSeed())
	seedRegistry.Register(seed.NewMinioCanonicalSeed())
	// Wave 2 — HTTP-API (port-forward-based)
	seedRegistry.Register(seed.NewArgoCDTokenSeed())
	seedRegistry.Register(seed.NewGiteaTokenSeed())
	seedRegistry.Register(seed.NewHydraURLsSeed())
	seedRegistry.Register(seed.NewAdminHydraClientSeed())
	// Wave 3 — complex + multi-cluster
	seedRegistry.Register(seed.NewVaultCrossClusterAddrSeed())
}
