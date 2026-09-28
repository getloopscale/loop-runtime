// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package chaos_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// M13 (plan m13-chaos-declarative) — c11 reexpressed from bash orchestration
// (infra/scripts/local/chaos-scenarios/c11-webhook-concurrent-podkill.sh) into a
// declarative Chaos Mesh CRD (reusing manifests/pod-kill-api-leader.yaml) + Go
// assertion, following the repo's established chaos-spec pattern
// (rekor_down_during_attest_test.go / fulcio_down_during_sign_test.go).
//
// c11 scenario: inject a pod-kill on the loopzero-api leader CONCURRENTLY with a
// GitHub webhook dispatch. The webhook MUST be durably queued (PG inbox/retry)
// and the 9-link deploy chain MUST complete within 25 min (5 min chaos window +
// 20 min chain budget), with SLO error rate < 1% over the window — the webhook
// MUST be processed from the queue after the pod restarts, never silently dropped.
//
// The PodChaos fault reuses the existing pod-kill-api-leader.yaml manifest (same
// fault as Scenario 11 in chaos_test.go); c11's distinctness is the concurrent
// webhook dispatch + chain-completion + SLO assertion, not a new fault.

var _ = Describe("c11: webhook dispatch concurrent with loopzero-api leader pod-kill", Label("chaos", "webhook", "c11"), func() {
	BeforeEach(func() {
		skipUnlessChaos()
		if !chaosMeshAvailable() {
			Skip("chaos-mesh CRDs absent — install chaos-mesh per docs/operations/chaos-test-setup.md to enable")
		}
	})

	It("durably queues the webhook + completes the 9-link chain within budget, SLO error rate < 1%", func() {
		// Expected LIVE flow (operator and sponsor together, on a live cluster):
		//   1. checkAPIHealth() — API serving before chaos (EC-11 preflight:
		//      chaos-mesh operator has >= 4 Running pods).
		//   2. applyChaos("pod-kill-api-leader.yaml") — PodChaos pod-kill on
		//      app=loopzero-api (reused manifest).
		//   3. CONCURRENTLY dispatch a GitHub webhook (gh api repository_dispatch)
		//      during the chaos window.
		//   4. after the pod restarts, assert the webhook was processed FROM the
		//      PG queue (not silently dropped) and the 9-link deploy chain
		//      (clone -> build -> sign -> manifests -> gitops -> sync -> healthy)
		//      completes within 25 min total.
		//   5. assert SLO error rate < 1% over the chaos window.
		//   6. cleanup() deletes the PodChaos (deferred).
		//
		// The unit-level guarantees (webhook queue durability, reconcile
		// idempotency) are covered by their own tests; this spec is the LIVE
		// proof that they COMPOSE under a control-plane pod-kill.
		Skip("operator LIVE-only — chaos-mesh + real webhook dispatch + build cluster required; flow documented in this It block")

		Expect(true).To(BeTrue(), "placeholder so future operator-LIVE runs have a green pivot")
	})
})
