// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// B-077 / ADR 2026-08-13-tenant-cr-fence-placement.
//
// `loopzero-tenant-cr-shape` is a ValidatingAdmissionPolicy over `loopzero.io/v1 tenants`
// — the only fence the platform has on its own CRs, since it runs no admission
// webhook on them. Measured 2026-08-13: it rendered on the CELL clusters, which
// install no `tenants` CRD at all, and NOT on mgmt, where every Tenant lives.
//
// # WHY THIS IS ASSERTED BY RENDER
//
// The item's DoD asks for it in those words, and the reason is a defect this
// repository already paid for: slice-3 copied the policy template into the
// published chart without the value that enables it, and the drift gate stayed
// green because it compares FILE NAMES. A file can be present and the policy
// inert.
//
// A LIVE test would not settle it either, and the trap is worth naming: the k3d
// e2e kubeconfig is admin, a member of `system:masters` — the group this very
// policy exempts. Such a test passes because the apply was ALLOWED, not because
// the policy worked (CLAUDE.md § Common Mistakes).
package helm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
)

const (
	fenceName = "loopzero-tenant-cr-shape"
	// The one admission-policies chart. Helmfile installs it on core (with
	// values-mgmt.yaml) and on every cell (base values); ArgoCD consumes the
	// same chart. There used to be a bootstrap copy under infra/helm, and a test
	// here asserted the two agreed on the fence's default — the B-046 class, a
	// template copied without the value that enables it. One chart cannot
	// disagree with itself, so that comparison left with the copy.
	admissionChart = "loopzero-admission-policies"
)

// render renders the named chart with the given values files (relative to the
// chart), newest last, through the chartrender facade.
func render(t *testing.T, chartName string, valuesFiles ...string) string {
	t.Helper()
	chartDir, err := chartrender.ChartPath(chartName)
	if err != nil {
		t.Fatalf("resolve chart %q: %v", chartName, err)
	}
	opts, err := chartrender.NewOptions(chartDir, "")
	if err != nil {
		t.Fatalf("render options for %s: %v", chartDir, err)
	}
	for _, v := range valuesFiles {
		opts.ValuesFiles = append(opts.ValuesFiles, filepath.Join(chartDir, v))
	}
	return chartrender.RenderTemplate(t, opts, chartDir, "fence-probe", nil)
}

// The fence must render where the Tenant CRs live. `loopzero-crds` is installed by
// core.helmfile only; cell.helmfile does not reference it.
func TestTenantCrFence_RendersOnMgmt(t *testing.T) {
	got := render(t, admissionChart, "values.yaml", "values-mgmt.yaml")
	if !strings.Contains(got, fenceName) {
		t.Errorf("%s is ABSENT from the mgmt render. Every loopzero.io Tenant lives in loopzero-system on "+
			"mgmt, and this is the platform's only fence on its own CRs — there is no admission "+
			"webhook behind it (B-077).", fenceName)
	}
}

// And it must NOT render where they do not. An inert fence is worse than an
// absent one: it was read as coverage, and a slice-3 plan used that reading to
// accept a risk as small.
func TestTenantCrFence_DoesNotRenderOnCells(t *testing.T) {
	got := render(t, admissionChart, "values.yaml")
	if strings.Contains(got, fenceName) {
		t.Errorf("%s renders from the base values, which is what the CELL clusters receive — and "+
			"they install no `tenants` CRD (loopzero-crds appears 0 times in cell.helmfile). A policy "+
			"matching a resource that does not exist can never fire, and reads as coverage (B-077).",
			fenceName)
	}
}

// Floor: a render that produced nothing would satisfy a `!Contains` assertion
// for the wrong reason. This is the `PASS — 0 routes` shape (B-040).
func TestTenantCrFence_RendersAreNotEmpty(t *testing.T) {
	for name, out := range map[string]string{
		"mgmt": render(t, admissionChart, "values.yaml", "values-mgmt.yaml"),
		"base": render(t, admissionChart, "values.yaml"),
	} {
		if !strings.Contains(out, "kind: ValidatingAdmissionPolicy") {
			t.Errorf("%s render contains no ValidatingAdmissionPolicy at all; the absence assertion "+
				"above would pass for the wrong reason", name)
		}
	}
}

// The two policies that guard loopzero.io platform CRs must exempt the SAME platform
// identities. They match the same resources against the same actors, and a
// divergence means one of them denies a controller the other trusts.
//
// This test exists because that divergence WAS the state: `vap-protect-platform-
// deletes` exempted the operator SAs and `vap-tenant-cr-shape` did not, while the
// tenant operator writes Tenant CRs from four sites — including the Update that
// removes the finalizer. Enabling the fence with that gap would have made a
// Tenant permanently undeletable.
//
// The first enumeration missed those sites because they are `r.Update(ctx,
// tenant)` — no `&`, and one behind a helper. A grep-shaped check cannot see
// that; comparing the two policies to each other can.
func TestTenantCrFence_ExemptsTheSameIdentitiesAsItsSibling(t *testing.T) {
	out := render(t, admissionChart, "values.yaml", "values-mgmt.yaml")

	// Rendered together, so both policies are in one document set.
	for _, sa := range []string{
		"system:serviceaccount:loopzero-system:loopzero-api",
		"system:serviceaccount:loopzero-system:loopzero-tenant-operator",
	} {
		shape := policySection(t, out, fenceName)
		if !strings.Contains(shape, sa) {
			t.Errorf("%s does not exempt %q, but the tenant operator writes Tenant CRs from four "+
				"sites — one of them the Update that removes the finalizer. Denying it makes the "+
				"Tenant permanently undeletable (B-077).", fenceName, sa)
		}
	}
}

// policySection returns the YAML document declaring the named policy.
func policySection(t *testing.T, rendered, name string) string {
	t.Helper()
	for _, doc := range strings.Split(rendered, "\n---") {
		if strings.Contains(doc, "name: "+name) && strings.Contains(doc, "kind: ValidatingAdmissionPolicy") {
			return doc
		}
	}
	t.Fatalf("policy %q not found in the render; a check that inspected nothing verified nothing", name)
	return ""
}

// mgmtValuesStack reads the values files the helmfile actually layers onto the
// mgmt release, newest last.
//
// The tests above name the stack literally — `values.yaml`, `values-mgmt.yaml`.
// Today that matches the helmfile, and a review of the B-077 closure pointed out
// that nothing keeps it matching: add a third overlay in the helmfile and the
// tests keep rendering a two-file stack, green, measuring a composition that is
// not the deployed one.
//
// It is the same shape as the finding that produced this whole item — "the test
// measures the render but not WHERE the value is overridden" — one level up.
//
// This function does not replace the literals; it asserts they still ARE the
// stack. Rendering from a derived list would hide a divergence by absorbing it.
func mgmtValuesStack(t *testing.T) []string {
	t.Helper()
	const helmfilePath = "../../helmfile/core.helmfile.yaml.gotmpl"
	b, err := os.ReadFile(filepath.Clean(helmfilePath))
	if err != nil {
		t.Fatalf("reading %s: %v — the stack cannot be derived, and a literal nobody checks is the "+
			"drift this function exists to catch", helmfilePath, err)
	}

	lines := strings.Split(string(b), "\n")
	start := -1
	for i, l := range lines {
		if strings.Contains(l, "name: admission-policies-mgmt") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("no release named admission-policies-mgmt in %s — either it was renamed, in which "+
			"case these tests point at nothing, or the file moved", helmfilePath)
	}

	var stack []string
	inValues := false
	for _, l := range lines[start:] {
		trimmed := strings.TrimSpace(l)
		switch {
		case trimmed == "values:":
			inValues = true
		case inValues && strings.HasPrefix(trimmed, "- "):
			stack = append(stack, filepath.Base(strings.TrimPrefix(trimmed, "- ")))
		case inValues && trimmed != "" && !strings.HasPrefix(trimmed, "- "):
			return stack // the values block ended
		case strings.HasPrefix(trimmed, "- name: ") && inValues:
			return stack
		}
	}
	return stack
}

// TestTenantCrFence_TheValuesStackUnderTestIsTheDeployedOne keeps the literal
// stack in the tests above honest against the helmfile that deploys it.
func TestTenantCrFence_TheValuesStackUnderTestIsTheDeployedOne(t *testing.T) {
	want := []string{"values.yaml", "values-mgmt.yaml"}
	got := mgmtValuesStack(t)

	if len(got) != len(want) {
		t.Fatalf("the helmfile layers %d values file(s) onto admission-policies-mgmt (%v) and these "+
			"tests render %d (%v). Every assertion above is measuring a composition that is not "+
			"deployed — green, and about the wrong thing (B-077).", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("values file %d: helmfile layers %q, the tests render %q — order matters, the "+
				"later file overrides the earlier", i, got[i], want[i])
		}
	}
}
