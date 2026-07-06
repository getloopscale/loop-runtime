// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmoverlapparity

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// M14 gap #3 — content diff of the two overlapping chart copies.
//
// The version-parity check (Check) proves the published chart is not behind the
// bootstrap one, but says nothing about whether their SECURITY-CRITICAL content
// agrees. CLAUDE.md § Helm Chart Dual System enforces "when patching one, apply
// the same fix to the other" by convention only; this mechanizes it — the
// verify-only shape of kserve's verify-helm-helpers.sh (blueprint Q7), NOT
// generation (the two loopzero chart trees have distinct purposes and are not
// generated from one another).
//
// Only fields that MUST be byte-identical are compared. Fields that diverge
// BY DESIGN (bootstrap uses a local image, published an OCI ref) are in
// contentAllowlist.

// contentTargetFields are the dotted paths in values.yaml that MUST match
// across both chart copies.
//
// NOTE: securityContext.* is intentionally NOT here — on the real loopzero charts it
// is hardcoded in templates/deployment.yaml, ABSENT from values.yaml, so a
// values.yaml diff would be a permanent silent no-op (review finding). The
// securityContext invariant is enforced on RENDERED output instead
// (rendered_diff.go / diffRenderedSecurityContext). This list is the fields that
// genuinely live in values.yaml.
var contentTargetFields = []string{
	"resources.requests.cpu",
	"resources.requests.memory",
	"resources.limits.cpu",
	"resources.limits.memory",
}

// contentAllowlist are dotted paths that diverge by design and are NOT compared.
var contentAllowlist = map[string]bool{
	"image.repository": true,
	"image.tag":        true,
	"fullnameOverride": true,
}

// diffChartContent compares the target fields of two values.yaml files. A
// missing file on either side is reported as a Violation (EC-2) — never a
// panic, never a silent skip. Allowlisted fields are ignored.
func diffChartContent(bootstrapValues, publishedValues, chartName string) []Violation {
	bVals, bErr := readValues(bootstrapValues)
	pVals, pErr := readValues(publishedValues)

	// EC-2: asymmetry is the drift signal. If exactly ONE side has a values.yaml
	// and the other doesn't, the charts have diverged (a file was deleted/renamed
	// on one side) — that IS a violation. If BOTH sides lack values.yaml, there
	// is nothing to compare and no drift (some charts legitimately ship no
	// values.yaml); return clean rather than a false positive.
	bMissing, pMissing := os.IsNotExist(bErr), os.IsNotExist(pErr)
	if bMissing && pMissing {
		return nil
	}
	if bErr != nil || pErr != nil {
		reason := fmt.Sprintf("chart_missing_one_side: values.yaml present on only one side for %q", chartName)
		if bErr != nil {
			reason += fmt.Sprintf(" (bootstrap: %v)", bErr)
		}
		if pErr != nil {
			reason += fmt.Sprintf(" (published: %v)", pErr)
		}
		return []Violation{{
			BootstrapPath: bootstrapValues,
			PublishedPath: publishedValues,
			Reason:        reason,
		}}
	}

	var vs []Violation
	for _, field := range contentTargetFields {
		if contentAllowlist[field] {
			continue
		}
		bv := lookupDotted(bVals, field)
		pv := lookupDotted(pVals, field)
		if bv != pv {
			vs = append(vs, Violation{
				BootstrapPath:    bootstrapValues,
				PublishedPath:    publishedValues,
				BootstrapVersion: bv,
				PublishedVersion: pv,
				Reason:           fmt.Sprintf("content drift in %q at %s", chartName, field),
			})
		}
	}
	return vs
}

// readValues parses a values.yaml into a generic map.
func readValues(path string) (map[string]any, error) {
	// #nosec G304 -- path is composed of fixed RepoRoot + DefaultPairs entries.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// lookupDotted resolves a dotted path (e.g. "securityContext.runAsUser") in a
// nested map, returning "" when absent. A missing target field on both sides is
// equal ("" == "") and therefore not a drift; a field present on only one side
// surfaces as a mismatch ("value" != "").
func lookupDotted(m map[string]any, dotted string) string {
	cur := any(m)
	start := 0
	for i := 0; i <= len(dotted); i++ {
		if i == len(dotted) || dotted[i] == '.' {
			key := dotted[start:i]
			asMap, ok := cur.(map[string]any)
			if !ok {
				return ""
			}
			cur, ok = asMap[key]
			if !ok {
				return ""
			}
			start = i + 1
		}
	}
	if cur == nil {
		return ""
	}
	return fmt.Sprintf("%v", cur)
}
