// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import "github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/seed"

// init registers the Wave 1 seeds (K8s-Secret class — CreateOrUpdate mechanic).
// Registration order = execution order for `seed all` (deterministic, D3).
func init() {
	seedRegistry.Register(seed.NewGitHubAppDevSeed())
	seedRegistry.Register(seed.NewStripeWebhookSecretSeed())
}
