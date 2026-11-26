//go:build helm_render

// Package serviceaccountidentity_test asserts that the ServiceAccount identity each
// operator chart renders is a MEMBER of the delete-guard allowlist, at a release name
// the chart does not control (B-047).
//
// The coupling is the subject. charts/loopzero-common/templates/_helpers.tpl defaults the
// ServiceAccount name to loopzero-common.fullname, whose only non-override input is
// .Release.Name, while two places read that name as a literal string: the delete-guard
// ValidatingAdmissionPolicy, and OpenBao's boundServiceAccountNames. A derived name and
// a literal allowlist can agree only by coincidence, and that coincidence already broke
// once (B-096) — as a Forbidden inside a reconcile loop, never as a failed install,
// which is why it survived review.
//
// This asserts MEMBERSHIP rather than the literal string on purpose: a literal
// assertion would still pass if somebody removed an entry from the policy, and the
// coupling between a chart's identity and an allowlist in another tree is exactly what
// this test exists to make visible.
package serviceaccountidentity_test

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
	coreV1 "k8s.io/api/core/v1"
)

// controlRelease is deliberately outside loopzero-common.fullname's `contains` branch: at a
// release name containing the chart name the helper returns the release unchanged, which
// happens to be the allowlisted string, so a suite built on such a name is green against
// an unfixed tree.
const controlRelease = "zzqq"

// mgmtOverlay is the values file of the admission-policies install that runs the delete
// guard (core cluster). The exempt identities are no longer a literal in any template:
// they are `systemActorExemptions.platformServiceAccounts`, expanded by the
// loopzero-admission.systemActorExemptions helper, so only a render sees them — and
// rendering with the overlay the guard is actually installed with reads the list that
// is really enforced.
const mgmtOverlay = "charts/loopzero-admission-policies/values-mgmt.yaml"

// expectedAllowlistSize is the number of identities the policy carries today. It is a
// derivation guard, not an assertion about the policy's contents: if the parse breaks,
// this fails here and names the parse, instead of reporting every chart absent from an
// empty set — a failure mode that would read like a real finding.
const expectedAllowlistSize = 6

// allowlistedIdentities parses the fully-qualified usernames out of the rendered
// delete-guard policy.
func allowlistedIdentities(t *testing.T) map[string]bool {
	t.Helper()

	chartPath, err := chartrender.PublishedChartPath("loopzero-admission-policies")
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, mgmtOverlay)
	require.NoError(t, err)
	rendered := chartrender.RenderTemplate(t, opts, chartPath, "delete-guard",
		[]string{"templates/vap-protect-platform-deletes.yaml"})

	out := map[string]bool{}
	for _, m := range regexp.MustCompile(
		`system:serviceaccount:loopzero-system:([a-z0-9-]+)`).FindAllStringSubmatch(rendered, -1) {
		out[m[1]] = true
	}
	require.GreaterOrEqual(t, len(out), expectedAllowlistSize,
		"derivation broken: parsed %d identities from the rendered delete guard (%s) — fix the "+
			"parse before reading any result below as a finding", len(out), mgmtOverlay)
	return out
}

// charts includes loopzero-api as the POSITIVE control. It is pinned already
// (charts/loopzero-api/values.yaml), so it must pass BEFORE the change as well as after: a
// test whose every row is red at RED cannot distinguish a working assertion from one
// that always fails.
var charts = []string{
	"loopzero-api",
	"loopzero-app-operator",
	"loopzero-platform-operator",
	"loopzero-preview-operator",
	"loopzero-tenant-operator",
}

func TestServiceAccountIsAllowlistedAtAnyReleaseName(t *testing.T) {
	t.Parallel()
	exempt := allowlistedIdentities(t)

	for _, chart := range charts {
		t.Run(chart, func(t *testing.T) {
			t.Parallel()

			// PublishedChartPath: every chart in this list is a charts/ chart, and the
			// call says so instead of relying on a name resolving to one tree.
			chartPath, err := chartrender.PublishedChartPath(chart)
			require.NoError(t, err)

			opts, err := chartrender.NewOptions(chartPath, "")
			require.NoError(t, err)

			out := chartrender.RenderTemplate(t, opts, chartPath, controlRelease,
				[]string{"templates/serviceaccount.yaml"})

			var sa coreV1.ServiceAccount
			chartrender.MustUnmarshalK8SYamlByKind(t, out, "ServiceAccount", &sa)

			require.Equal(t, filepath.Base(chartPath), sa.Name,
				"release %q: the ServiceAccount name is derived from the release, not from "+
					"the chart", controlRelease)

			require.True(t, exempt[sa.Name],
				"release %q: %q is not in the delete-guard allowlist (%s) — the operator's "+
					"DELETEs are refused during reconcile, not at install",
				controlRelease, sa.Name, mgmtOverlay)
		})
	}
}
