// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// RED test for the remaining half of issue #61 — the half that was a product
// decision rather than a race.
//
// The race half shipped in 28a66a5c: a preStop sleep so endpoint removal wins
// against SIGTERM. That fixes the moment a pod is told to die. It does nothing
// about WHEN the old ReplicaSet is told to die — which, on a canary promotion,
// is immediately.
//
// # The decision
//
// LoopRuntime promises a WebSocket **graceful reconnection**, not **survival**.
//
// Survival would mean a connection outlives the process that owns it, which
// requires handing session state between pods. LoopRuntime does not do that, and
// building it would be an architecture change, not a config value. Promising
// survival and delivering a severed socket is worse than promising reconnection
// and delivering it.
//
// So the contract is: when a deploy replaces the process behind a connection,
// the client gets a clean close and enough time to finish in-flight work, then
// reconnects to the new pod. Two settings implement exactly that, and no more:
//
//	preStop 5s      endpoint removal wins the race (already shipped)
//	grace   30s     the app can send a close frame and drain (already shipped)
//	scaleDownDelay  the old ReplicaSet is not torn down the instant the canary
//	                is promoted — this file
//
// Deliberately NOT done: a multi-minute grace period. It would delay every
// rollout AND every rollback — including the rollback you reach for when a
// canary is actively serving errors — to extend a promise LoopRuntime cannot keep
// anyway. The cost lands on the incident path, where seconds matter most.
package manifests

import "testing"

// TestRolloutHasScaleDownDelay asserts the old ReplicaSet survives promotion
// long enough for connections on it to close cleanly.
func TestRolloutHasScaleDownDelay(t *testing.T) {
	ro := buildRollout(
		scopeFor(t, RenderInput{TenantID: "tn-x", ProjectID: "proj", Environment: "production"}),
		AppManifestInput{Name: "app", Type: "server", Port: 8080, ImageRef: "example/app:v1"},
		8080,
	)

	if ro.Spec.Strategy.Canary == nil {
		t.Fatalf("rollout has no canary strategy — fixture is wrong, not the code")
	}
	got := ro.Spec.Strategy.Canary.ScaleDownDelaySeconds
	if got == nil {
		t.Fatalf("Rollout does not set scaleDownDelaySeconds (issue #61).\n" +
			"    On promotion, Argo Rollouts scales the old ReplicaSet down. Its pods get preStop +\n" +
			"    grace, so a connection is not severed mid-frame — but nothing states how long the old\n" +
			"    generation is allowed to linger, so the value is whatever the controller defaults to.\n" +
			"    An unstated default is not a promise: nobody can tell whether it may be changed.")
	}
	if *got < 30 {
		t.Errorf("scaleDownDelaySeconds = %d, want >= 30 — it must cover the pod's own teardown\n"+
			"    budget (preStop 5s + grace 30s). A delay shorter than the teardown it is supposed to\n"+
			"    protect would tear the ReplicaSet down while its pods are still draining.", *got)
	}
	if *got > 120 {
		t.Errorf("scaleDownDelaySeconds = %d, too long. The old ReplicaSet holds quota and delays the\n"+
			"    NEXT rollout — including a rollback during an incident. LoopRuntime promises reconnection,\n"+
			"    not survival; a multi-minute window buys a guarantee it does not make.", *got)
	}
}

// TestDeploymentUnaffectedByScaleDownDelay keeps the change scoped to the
// progressive-delivery path. `scaleDownDelaySeconds` is a Rollout field; the
// legacy Deployment path (LOOPRUNTIME_PROGRESSIVE_DELIVERY=false) has no equivalent
// and must not have acquired stray config.
func TestDeploymentUnaffectedByScaleDownDelay(t *testing.T) {
	ps := buildAppPodSpec(k8sContainer{Name: "app", Image: "example/app:v1"})

	if ps.TerminationGracePeriodSeconds != 30 {
		t.Errorf("terminationGracePeriodSeconds = %d, want 30 — the teardown budget is shared by both\n"+
			"    paths and this change must not have moved it", ps.TerminationGracePeriodSeconds)
	}
}
