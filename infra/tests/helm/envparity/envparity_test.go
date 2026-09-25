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
// Charts WITH real per-env assertions keep their own dev/drill/prod files —
// currently only loopzero-api (infra/tests/helm/loopzero-api/), which has stubs at
// values/{dev,drill,prod}/loopzero-api.yaml and asserts registry/replicas/domain.
//
// The "-published" entries pass their DIRECTORY name (loopzero-api-published,
// loopzero-cloud-published) as a distinct stub namespace, matching the original
// files — their stubs, if ever added, live at
// values/<env>/loopzero-<x>-published.yaml, NOT the bootstrap variant's stub.
package envparity_test

import (
	"testing"

	"github.com/getloopscale/loopzero/infra/tests/helm/chartrender"
)

// parityCharts are the charts whose per-env render is intentionally identical
// to defaults (no env stub). One entry per former per-chart directory.
var parityCharts = []string{
	"chaos-mesh",
	"cloudflared",
	"cluster-issuer",
	"harbor",
	"network-policies",
	"static-assets",
	"loopzero-admission-policies",
	"loopzero-api-published",
	"loopzero-app-operator",
	"loopzero-build-cwts",
	"loopzero-cloud",
	"loopzero-cloud-published",
	"loopzero-crds",
	"loopzero-dashboard",
	"loopzero-monitoring",
	"loopzero-platform",
	"loopzero-platform-operator",
	"loopzero-preview-operator",
	"loopzero-tenant-operator",
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
