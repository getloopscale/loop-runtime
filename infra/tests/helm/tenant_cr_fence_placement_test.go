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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	fenceName    = "loopzero-tenant-cr-shape"
	bootstrapDir = "../../helm/admission-policies"
	publishedDir = "../../../charts/loopzero-admission-policies"
)

// render runs `helm template` with the given values files, newest last.
func render(t *testing.T, chartDir string, valuesFiles ...string) string {
	t.Helper()
	args := []string{"template", "fence-probe", chartDir}
	for _, v := range valuesFiles {
		args = append(args, "-f", filepath.Join(chartDir, v))
	}
	out, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// The fence must render where the Tenant CRs live. `loopzero-crds` is installed by
// core.helmfile only; cell.helmfile does not reference it.
func TestTenantCrFence_RendersOnMgmt(t *testing.T) {
	got := render(t, bootstrapDir, "values.yaml", "values-mgmt.yaml")
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
	got := render(t, bootstrapDir, "values.yaml")
	if strings.Contains(got, fenceName) {
		t.Errorf("%s renders from the base values, which is what the CELL clusters receive — and "+
			"they install no `tenants` CRD (loopzero-crds appears 0 times in cell.helmfile). A policy "+
			"matching a resource that does not exist can never fire, and reads as coverage (B-077).",
			fenceName)
	}
}

// The two charts must agree on the default, per CLAUDE.md § Helm chart dual
// system. Divergence here is exactly the B-046 class: the published chart
// carried the template without the key that enables it.
func TestTenantCrFence_BothChartsAgreeOnTheDefault(t *testing.T) {
	bootstrap := strings.Contains(render(t, bootstrapDir, "values.yaml"), fenceName)
	published := strings.Contains(render(t, publishedDir, "values.yaml"), fenceName)
	if bootstrap != published {
		t.Errorf("default disagrees between the charts: bootstrap renders it = %v, published = %v. "+
			"Patching one without the other recreates B-046 — a fence that exists in dev and not "+
			"where it ships.", bootstrap, published)
	}
}

// Floor: a render that produced nothing would satisfy a `!Contains` assertion
// for the wrong reason. This is the `PASS — 0 routes` shape (B-040).
func TestTenantCrFence_RendersAreNotEmpty(t *testing.T) {
	for name, out := range map[string]string{
		"mgmt": render(t, bootstrapDir, "values.yaml", "values-mgmt.yaml"),
		"base": render(t, bootstrapDir, "values.yaml"),
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
	out := render(t, bootstrapDir, "values.yaml", "values-mgmt.yaml")

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
