//go:build helm_render

// Package loopzerodashboard_test — per-env parity test for chart "loopzero-dashboard"
// in env "prod".
//
// Plan 2026-05-20 v1.2 T3.1/T3.2 file-count compliance + scope-refinement
// pattern: regression guard for charts without helmfile env-specific values.
//
// Behavior:
//   - No env stub at infra/tests/helm/values/prod/loopzero-dashboard.yaml → PASS
//     (documents the absence of env divergence as intentional).
//   - Env stub appears later without env-specific assertions → FAIL loudly,
//     forcing future maintainer to assert env-specific invariants instead
//     of relying on this guard.
//
// To convert from guard to real env test: replace the body with explicit
// env-specific assertions (image registry / replicas / domain / etc.) and
// add a stub at the canonical EnvStubPath. See helm/loopzero-api/prod_test.go
// for the reference per-env test pattern.
package loopzerodashboard_test

import (
	"testing"

	"github.com/loopzero/loopzero/infra/tests/helm/helpers"
)

func TestEnvParity_prod(t *testing.T) {
	t.Parallel()
	helpers.AssertEnvParityIfNoStub(t, "prod", chartName)
}
