// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"path/filepath"
	"strings"
	"testing"
)

// Readiness and liveness MUST NOT point at the same unconditional handler (B-135).
//
// `/api/v1/health` always answers 200 — right for LIVENESS ("the process is
// alive"). Using it for readiness too leaves the pod `Ready` with the database
// down, taking traffic and returning errors on 27 of the 36 surfaces. There is
// no observable state in which the instance is alive and OUT of the Service,
// which is the one thing readiness exists to do.
//
// The test asserts the SHAPE, not the exact path: the two must differ and
// readiness must point at a deep-check path. Pinning the literal would fail any
// route rename while nothing got worse.
func TestReadinessAndLivenessDoNotShareTheSameHandler(t *testing.T) {
	api := publishedChart(t, "loopruntime-api")
	// The chart is installed two ways: by ArgoCD with its defaults, and by the
	// Helmfile bootstrap with values-bootstrap.yaml. Both renders are checked,
	// because a values file can override a probe as easily as a template can.
	for _, install := range []struct {
		name   string
		values []string
	}{
		{"charts/loopruntime-api", nil},
		{"charts/loopruntime-api with values-bootstrap.yaml", []string{filepath.Join(api, "values-bootstrap.yaml")}},
	} {
		t.Run(install.name, func(t *testing.T) {
			out := renderChart(t, api, "t", nil, install.values...)
			liveness := probePath(out, "livenessProbe:")
			readiness := probePath(out, "readinessProbe:")
			// This was a Skip, and the `skip-link` gate was right to refuse it. A chart
			// that declares neither probe is not out of scope here: it is the WORST case
			// this test exists to catch. With no readiness, Kubernetes uses liveness to
			// route traffic — exactly B-135 in its extreme form.
			//
			// A Skip here made a missing probe indistinguishable from an approved chart.
			if liveness == "" || readiness == "" {
				t.Fatalf("the chart does not declare both probes (liveness=%q readiness=%q). With "+
					"no readiness of its own, an instance that cannot serve stays in the Service "+
					"(B-135).", liveness, readiness)
			}
			if liveness == readiness {
				t.Errorf("liveness and readiness point at the SAME path (%q). The liveness "+
					"handler answers 200 unconditionally, so readiness never takes an instance "+
					"that cannot serve out of the Service (B-135).", liveness)
			}
			if !strings.Contains(readiness, "ready") && !strings.Contains(readiness, "deep") {
				t.Errorf("readiness points at %q, which does not look like a deep check. It must "+
					"reflect whether THIS instance can serve.", readiness)
			}
		})
	}
}

// probePath returns the `path:` that follows the probe marker.
func probePath(rendered, marker string) string {
	i := strings.Index(rendered, marker)
	if i < 0 {
		return ""
	}
	window := rendered[i:]
	if len(window) > 400 {
		window = window[:400]
	}
	j := strings.Index(window, "path:")
	if j < 0 {
		return ""
	}
	rest := window[j+len("path:"):]
	if end := strings.IndexByte(rest, '\n'); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}
