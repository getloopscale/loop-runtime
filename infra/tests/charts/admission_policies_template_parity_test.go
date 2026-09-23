// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// infra/helm/admission-policies (installed by Helmfile L2 on core and on every
// cell) and charts/loopzero-admission-policies (published OCI, ArgoCD L3) are one
// chart shipped twice. Their VALUES differ per install path and may; their
// TEMPLATES are the policies themselves and must not.
//
// Measured before this test existed: 8 of 20 templates had diverged, and the one
// that mattered was the published vap-tenant-namespace.yaml, which lacked the
// exclude-system-actors exemption its bootstrap twin carried — the published
// policy could deadlock a Tenant's deletion (removing a finalizer is an UPDATE,
// and the policy matches UPDATE). The name-keyed chart-pairs registry cannot see
// this pair (the charts are named differently), and helm-drift compares file
// NAMES, so nothing noticed.
//
// A fix to one tree now fails here until it reaches the other.
func TestAdmissionPolicies_BootstrapAndPublishedShipIdenticalTemplates(t *testing.T) {
	boot := "../../helm/admission-policies/templates"
	pub := "../../../charts/loopzero-admission-policies/templates"

	read := func(dir string) map[string][]byte {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		files := map[string][]byte{}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", e.Name(), err)
			}
			files[e.Name()] = body
		}
		if len(files) == 0 {
			t.Fatalf("%s holds no templates — a comparison against nothing proves nothing", dir)
		}
		return files
	}

	b, p := read(boot), read(pub)
	names := map[string]bool{}
	for n := range b {
		names[n] = true
	}
	for n := range p {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	for _, n := range sorted {
		bb, inBoot := b[n]
		pb, inPub := p[n]
		switch {
		case !inBoot:
			t.Errorf("%s exists only in the published chart — add it to infra/helm/admission-policies/templates", n)
		case !inPub:
			t.Errorf("%s exists only in the bootstrap chart — add it to charts/loopzero-admission-policies/templates", n)
		case !bytes.Equal(bb, pb):
			t.Errorf("%s differs between the two trees — apply the same change to both", n)
		}
	}
}
