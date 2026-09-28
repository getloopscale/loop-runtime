// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package config

import (
	"os"
	"strings"
	"testing"
)

// B-022: `loopzero-cloud` is the engine's authenticating proxy. `r.Route("/engine")` strips the prefix,
// applies requireJWT and forwards to `loopzero-api`; when it goes down, `/engine/*` and `/v1/*` return 502
// TOGETHER, even with the engine healthy — measured on 2026-08-10.
//
// It was the only component with that coupling that NOBODY probed. Without an availability
// series, the B-022 bullet that demands "measured risk, not a drawing on the whiteboard" had no
// way to be answered — and the authentication-boundary decision it conditions stayed stuck.
//
// This test exists so the probe does not disappear silently: a removed target breaks nothing
// visibly, it only erases the data, and the missing data is invisible until someone needs it.
func TestProbeCoversLoopZeroCloud(t *testing.T) {
	const path = "../../helm/monitoring/manifests/probe-targets.yaml"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("probe-targets missing: %v", err)
	}
	s := string(b)

	if !strings.Contains(s, "loopzero-cloud.loopzero-system.svc") {
		t.Error("`loopzero-cloud` left the probe targets: without it there is no availability " +
			"series for the component whose outage also takes down the engine path (B-022)")
	}
	// The counterpart: the targets that already existed must not have been replaced by the new one.
	for _, target := range []string{
		"loopzero-api.loopzero-system.svc",
		"gitea-http.loopzero-system.svc",
		"traefik.loopzero-system.svc",
		"argocd-server.argocd.svc",
	} {
		if !strings.Contains(s, target) {
			t.Errorf("target %q disappeared from the probes", target)
		}
	}
}
