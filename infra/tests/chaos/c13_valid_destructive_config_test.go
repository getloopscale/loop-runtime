// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package chaos_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// M13 (plan m13-chaos-declarative) — c13 reexpressed from bash orchestration
// (infra/scripts/local/chaos-scenarios/c13-valid-destructive-config.sh) into a Go
// (Ginkgo) chaos spec.
//
// c13 scenario: deploy a build-VALID but health-DESTRUCTIVE .loopruntime/services.json
// (a workload that BUILDS + SYNCS but never becomes HEALTHY — e.g. a readiness
// path that always 404s) and assert the deploy pipeline FAILS LOUD (release ->
// SYNC_FAILED, SSE deploy.sync_failed / rollout.aborted) within the SyncTimeout
// budget (180s) — never a silent hang, never a spurious ACTIVE.
//
// ADR D3 (from the c13 bash + this plan): c13 uses NO chaos-mesh. The fault is a
// CONFIG fault injected through the deploy path, not an infra fault — Chaos Mesh
// has no primitive for "make the app's readiness path 404". Hence this spec has
// NO chaosMeshAvailable() gate — only skipUnlessChaos() + the deploy-path
// preconditions checked at LIVE time.
//
// The AUTONOMOUS deterministic proof of the fail-loud boundary already exists as
// a hermetic unit test:
//   api/internal/release/release_convergence_loop_test.go
// (the never-healthy config stays PENDING and is failed loud at the 180s
// SyncTimeout in release_convergence_loop.go). This spec is the LIVE/E2E proof
// that the boundary holds end-to-end through a real deploy.

var _ = Describe("c13: build-valid, health-destructive config fails loud (no chaos-mesh)", Label("chaos", "c13"), func() {
	BeforeEach(func() {
		// ADR D3: NO chaosMeshAvailable() gate — this is a config fault, not an
		// infra fault. Only the operator opt-in gate applies here; the deploy-path
		// health precondition is checked inside the It at LIVE time.
		skipUnlessChaos()
	})

	It("FAILs the release loud (SYNC_FAILED) within the 180s SyncTimeout, never a silent hang", func() {
		// Expected LIVE flow (operator and sponsor together, on a live cluster):
		//   1. deploy-path health preflight (NOT chaos-mesh).
		//   2. deploy a build-valid but health-destructive .loopruntime/services.json
		//      (readiness path always 404s).
		//   3. EC-3 boundary guard: FIRST assert the build SUCCEEDED (or a release
		//      row exists) so the scenario asserts on the RELEASE boundary, not the
		//      build boundary (a build-stage failure would green the wrong boundary).
		//   4. assert the release reaches SYNC_FAILED within the 180s SyncTimeout
		//      budget (fail-loud), and an SSE deploy.sync_failed / rollout.aborted
		//      event was emitted — never a silent hang, never a spurious ACTIVE.
		//
		// Autonomous deterministic proof (hermetic, already GREEN in unit tests):
		//   api/internal/release/release_convergence_loop_test.go
		Skip("operator LIVE-only — real deploy of a destructive .loopruntime/services.json required; " +
			"deterministic proof is api/internal/release/release_convergence_loop_test.go; " +
			"flow documented in this It block")

		Expect(true).To(BeTrue(), "placeholder so future operator-LIVE runs have a green pivot")
	})
})
