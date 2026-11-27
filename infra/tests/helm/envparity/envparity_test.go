//go:build helm_render

// Package envparity_test consolidates the per-chart env-parity guard that
// previously lived as 60 near-identical {dev,drill,prod}_test.go files (one
// triple per chart) into a single table-driven test.
//
// Refactor 2026-07-11 (infra/tests taxonomy): the 20 charts below have NO
// env-specific value stubs, so their per-env behavior is intentionally
// identical to defaults. AssertEnvParityIfNoStub documents that absence and
// FAILS LOUDLY the day someone adds a stub at
// infra/tests/helm/values/<env>/<chart>.yaml without also adding
// env-specific assertions — forcing a real per-env test instead of a silent
// guard. Behavior is byte-identical to the deleted files; only the file
// count changed (60 -> 1).
//
// No chart has env stubs today. The last ones were values/{dev,drill,prod}/loop-runtime-api.yaml,
// consumed by the render suite of the bootstrap chart infra/helm/loop-runtime-api; both went
// with that chart. The environment overlays are helmfile templates, which a plain
// helm render cannot read, so per-environment behaviour of an install is checked by
// rendering it through helmfile, not by stubs that are synced by hand.
//
// The "-published" entries pass their DIRECTORY name (loop-runtime-api-published,
// loop-console-published) as a distinct stub namespace, matching the original
// files — their stubs, if ever added, live at
// values/<env>/loop-runtime-<x>-published.yaml, NOT the bootstrap variant's stub.
package envparity_test

import (
	"testing"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
)

// parityCharts are the charts whose per-env render is intentionally identical
// to defaults (no env stub). One entry per former per-chart directory.
var parityCharts = []string{
	"chaos-mesh",
	"cloudflared",
	"cluster-issuer",
	"network-policies",
	"static-assets",
	"loop-runtime-admission-policies",
	"loop-runtime-api-published",
	"loop-runtime-app-operator",
	"loop-runtime-build-cwts",
	"loop-console-published",
	"loop-runtime-crds",
	"loop-console-dashboard",
	"loop-runtime-monitoring",
	"loop-runtime-platform",
	"loop-runtime-platform-operator",
	"loop-runtime-preview-operator",
	"loop-runtime-tenant-operator",
}

// parityEnvs mirrors the former dev_test.go / drill_test.go / prod_test.go triple.
var parityEnvs = []string{"dev", "drill", "prod"}

func TestEnvParity(t *testing.T) {
	for _, chart := range parityCharts {
		chart := chart
		for _, env := range parityEnvs {
			env := env
			t.Run(chart+"/"+env, func(t *testing.T) {
				t.Parallel()
				chartrender.AssertEnvParityIfNoStub(t, env, chart)
			})
		}
	}
}
