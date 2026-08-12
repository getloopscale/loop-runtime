// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts_test

import (
	"os/exec"
	"strings"
	"testing"
)

// The published admission-policies chart must RENDER the same policy objects as
// the bootstrap one — not merely carry the same filenames.
//
// This exists because filename parity is not effect parity, and the difference
// is not academic. slice-3 copied `vap-tenant-cr-shape.yaml` into the published
// chart to close B-046, and that file's guard is
// `{{- if .Values.vaps.tenantCrShape }}` — a key the published values.yaml did
// not define. Measured at that moment: bootstrap rendered 3 occurrences,
// published rendered 0. The template was INERT, and `helm-drift` went green
// because it compares filename SETS.
//
// A visible gap had become an invisible one that passed a gate. This is the
// assertion that would have caught it, and it is deliberately about output.
func TestAdmissionPolicies_PublishedRendersWhatBootstrapRenders(t *testing.T) {
	render := func(chart string) string {
		out, err := exec.Command("helm", "template", "ap", chart).CombinedOutput()
		if err != nil {
			t.Fatalf("helm template %s: %v\n%s", chart, err, out)
		}
		return string(out)
	}

	boot := render("../../helm/admission-policies")
	pub := render("../../../charts/loopzero-admission-policies")

	// Compare the set of loopzero-* object names each chart emits. Names rather than
	// the whole render keeps the assertion stable against ordering and formatting
	// while still measuring effect instead of file presence.
	names := func(s string) map[string]bool {
		found := map[string]bool{}
		for _, line := range strings.Split(s, "\n") {
			l := strings.TrimSpace(line)
			if strings.HasPrefix(l, "name: loopzero-") {
				found[strings.TrimSpace(strings.TrimPrefix(l, "name:"))] = true
			}
		}
		return found
	}

	bootNames, pubNames := names(boot), names(pub)
	if len(bootNames) == 0 {
		t.Fatal("the bootstrap chart rendered no loopzero-* object — the extractor is broken, and a comparison against nothing proves nothing")
	}

	var missing []string
	for n := range bootNames {
		if !pubNames[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		t.Errorf("the published chart carries the template but does not RENDER: %v — a copied file whose value is never set is inert, and a filename-set gate cannot see it", missing)
	}
}
