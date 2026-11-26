// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import "github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/seed"

// init registers Wave 1 (K8s-Secret) + Wave 2 (HTTP-API port-forward) seeds.
// Registration order = execution order for `seed all` (deterministic, D3).
//
// A seed writes secret material, or data no helmfile can declare. It does not edit
// what helmfile renders: patch-kratos, hydra-urls, vault-cross-cluster-addr and
// loki-cross-cluster did, and their values are environment values now
// (infra/tests/charts/seeded_wiring_is_rendered_test.go, principle_violations#67).
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
	seedRegistry.Register(seed.NewAdminHydraClientSeed())
	// Wave 3 — complex + multi-cluster
	seedRegistry.Register(seed.NewVaultRoleIDSyncSeed())
	seedRegistry.Register(seed.NewNetworkingCrossClusterCorednsSeed())
	seedRegistry.Register(seed.NewClusterKubeconfigsSeed())
	seedRegistry.Register(seed.NewSecretsSeedDevSeed())
	seedRegistry.Register(seed.NewBrokerSeed())
	seedRegistry.Register(seed.NewStripeWebhookEndpointSeed())
	seedRegistry.Register(seed.NewGitHubWebhookFixtureSeed())
	seedRegistry.Register(seed.NewTenantFixtureSeed())
	seedRegistry.Register(seed.NewAdminLoopzeroDevSeed())
	// #470 — loopzero-cloud's data-plane admin keys, then the edge that depends on them.
	seedRegistry.Register(seed.NewDataPlaneAdminKeysSeed())
	seedRegistry.Register(seed.NewDataPlaneEdgeSeed())
}
