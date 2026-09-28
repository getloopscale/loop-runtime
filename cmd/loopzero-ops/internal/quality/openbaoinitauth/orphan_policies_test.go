// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package openbaoinitauth

import (
	"strings"
	"testing"
)

const roleCarryingLoopZeroAPI = `    write_auth_role "k8s-core" "api" \
      "loopzero-api" \
      "loopzero-system" \
      "loopzero-api" \
      "24h"
`

func TestPolicyCarriedByARolePasses(t *testing.T) {
	r := `write_policy "loopzero-api" "/tmp/x.hcl"` + "\n" + roleCarryingLoopZeroAPI
	if f := orphanPolicyFailures(r); len(f) != 0 {
		t.Errorf("failures = %v; the policy is carried by the role", f)
	}
}

// The measured defect (B-098): two declared policies no role carried.
func TestOrphanPolicyIsNamed(t *testing.T) {
	r := `write_policy "loopzero-api" "/tmp/a.hcl"
write_policy "loopzero-api-read" "/tmp/b.hcl"` + "\n" + roleCarryingLoopZeroAPI
	f := orphanPolicyFailures(r)
	if len(f) != 1 || !strings.Contains(f[0], "loopzero-api-read") {
		t.Fatalf("failures = %v; the orphan must be NAMED — a gate that does not say which forces a hunt", f)
	}
	if strings.Contains(f[0], `loopzero-api,`) || strings.Contains(f[0], `: loopzero-api `) {
		t.Errorf("failures = %v; the carried policy must not be accused", f)
	}
}

// A role may carry several comma-separated policies.
func TestCommaSeparatedPolicyList(t *testing.T) {
	r := `write_policy "a" "/tmp/a.hcl"
write_policy "b" "/tmp/b.hcl"
    write_auth_role "k8s-core" "r" \
      "sa" \
      "ns" \
      "a,b" \
      "24h"
`
	if f := orphanPolicyFailures(r); len(f) != 0 {
		t.Errorf("failures = %v; both are in the role's list", f)
	}
}

// Green over nothing: with no policy reached, the gate verified nothing.
func TestRenderWithoutPolicyFails(t *testing.T) {
	if f := orphanPolicyFailures("echo nothing"); len(f) != 1 || !strings.Contains(f[0], "reached nothing") {
		t.Errorf("failures = %v; want the empty-scan failure", f)
	}
}

// If the helper signature changes, the parser would read the wrong argument and
// mark live policies as orphans. Reporting it is mandatory; guessing is not.
func TestShortSignatureIsReportedNotMisread(t *testing.T) {
	r := `write_policy "a" "/tmp/a.hcl"
    write_auth_role "k8s-core" "r" \
      "sa"
`
	found := false
	for _, x := range orphanPolicyFailures(r) {
		if strings.Contains(x, "helper signature changed") {
			found = true
		}
	}
	if !found {
		t.Error("a signature change must be reported, not absorbed")
	}
}
