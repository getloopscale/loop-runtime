// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SRE-08.3 and SD-10.5 (system-design audit 2026-09-07).
//
// cwt-clone.yaml was the ONLY ClusterWorkflowTemplate with a retryStrategy, and
// its backoff is duration 5s / factor 2 with no jitter. Every clone retrying in
// a burst therefore retries at the same instants — 5s, 10s, 20s — and the herd
// stays synchronised.
//
// That matters here specifically: CLAUDE.md documents clone-repo exhausting its
// retry budget under roughly four or more concurrent builds within 30 seconds on
// a cold cluster, which is exactly the condition where synchronised retries make
// contention worse rather than better. maxConcurrentBuildsPerCell is 20.
//
// Argo's backoff has no jitter field, so the jitter has to be introduced by the
// step itself before the work starts.
func TestBuildTemplates_CloneRetryIsJittered(t *testing.T) {
	const clone = "../../../charts/loop-runtime-build-cwts/templates/cwt-clone.yaml"

	body, err := os.ReadFile(clone)
	if err != nil {
		t.Fatalf("read %s: %v", clone, err)
	}
	content := string(body)

	if !strings.Contains(content, "retryStrategy:") {
		t.Fatal("cwt-clone declares no retryStrategy at all")
	}

	// The herd is broken by randomising the start, since the backoff itself
	// cannot be randomised in Argo.
	jittered := strings.Contains(content, "RANDOM") || strings.Contains(content, "jitter")
	if !jittered {
		t.Error("clone retry backs off on fixed multiples with no jitter: a burst of clones " +
			"retries in lockstep at 5s/10s/20s, which is the documented failure mode under " +
			"concurrent build load")
	}
}

// SD-10.5. One retry policy across fourteen mapped hops. The image push is the
// other step that talks to an external system on the critical path of a deploy,
// and a transient registry error there fails the whole build with no attempt to
// recover.
func TestBuildTemplates_ExternalStepsDeclareRetry(t *testing.T) {
	dir := "../../../charts/loop-runtime-build-cwts/templates"
	entries, err := filepath.Glob(filepath.Join(dir, "cwt-*.yaml"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("no ClusterWorkflowTemplates under %s (err=%v)", dir, err)
	}

	var without []string
	for _, f := range entries {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if !strings.Contains(string(body), "retryStrategy:") {
			without = append(without, filepath.Base(f))
		}
	}

	if len(without) > 0 {
		t.Errorf("%d of %d ClusterWorkflowTemplates declare no retryStrategy:\n  %s\n\n"+
			"Each of these talks to an external system on the deploy critical path; a transient "+
			"error fails the build outright.",
			len(without), len(entries), strings.Join(without, "\n  "))
	}
}
