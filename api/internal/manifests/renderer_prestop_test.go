// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// RED test for issue #61, endpoint-removal race half.
//
// #61 reports that an Argo Rollouts canary kills long-lived connections. Two
// distinct things are going on, and only one of them is a design question:
//
//  1. HOW LONG a WebSocket should be allowed to outlive a deploy — genuinely a
//     product decision (does LoopZero promise the connection survives, or that the
//     client reconnects?). Not settled here.
//
//  2. The pod being torn down WHILE STILL RECEIVING TRAFFIC — a race that harms
//     every rollout, WebSocket or not, and has one standard fix.
//
// This test covers (2). When a pod is deleted, Kubernetes does two things
// CONCURRENTLY: it sends SIGTERM to the container, and it removes the endpoint
// from Services. The second is asynchronous — kube-proxy and Traefik take a
// moment to observe it. In that window the app is already shutting down while
// new connections are still being routed to it.
//
// The canonical fix is a preStop hook that simply waits, so endpoint removal
// wins the race before the app is told to stop. `terminationGracePeriodSeconds`
// does NOT solve this: it bounds how long the app has AFTER SIGTERM, and the
// race happens before.
//
// Two constraints shape the implementation:
//
//   - `exec` is unusable. Tenant images are arbitrary, and this repo already
//     meets distroless bases with no /bin/sh (the crane image is one). A preStop that shells out fails on exactly the images that are
//     most hardened.
//   - The native `sleep` action (GA since k8s 1.30; the clusters run
//     v1.32.13 per infra/k3d/*.yaml) needs no shell and is the right tool.
package manifests

import "testing"

// TestPodSpecHasPreStopSleep is the #61 race-half RED.
//
// Asserted on the shared pod-spec builder so it holds for both the Rollout
// (progressive delivery) and the legacy Deployment — the race is identical.
func TestPodSpecHasPreStopSleep(t *testing.T) {
	ps := buildAppPodSpec(k8sContainer{Name: "app", Image: "example/app:v1"})

	if len(ps.Containers) == 0 {
		t.Fatalf("no containers in pod spec")
	}
	c := ps.Containers[0]

	if c.Lifecycle == nil {
		t.Fatalf("container has no `lifecycle` (issue #61).\n" +
			"    Without a preStop hook the pod receives SIGTERM while endpoint removal is still\n" +
			"    propagating, so new connections keep arriving at a process that is shutting down.\n" +
			"    terminationGracePeriodSeconds does not help — it bounds the time AFTER SIGTERM,\n" +
			"    and this race happens before it.")
	}
	if c.Lifecycle.PreStop == nil {
		t.Fatalf("lifecycle has no `preStop`")
	}
	if c.Lifecycle.PreStop.Sleep == nil {
		t.Fatalf("preStop has no native `sleep` action — an exec hook would need /bin/sh, which\n" +
			"    distroless tenant images do not have")
	}

	secs := c.Lifecycle.PreStop.Sleep.Seconds
	if secs <= 0 {
		t.Errorf("preStop sleep is %d seconds — must be positive to win the endpoint race", secs)
	}
	grace := ps.TerminationGracePeriodSeconds
	if grace > 0 && secs >= grace {
		t.Errorf("preStop sleep (%vs) is not shorter than terminationGracePeriodSeconds (%vs).\n"+
			"    The grace period covers the sleep AND the app's own shutdown; if the sleep consumes\n"+
			"    all of it, the app is SIGKILLed the instant it finally receives SIGTERM — strictly\n"+
			"    worse than having no hook at all.", secs, grace)
	}
}
