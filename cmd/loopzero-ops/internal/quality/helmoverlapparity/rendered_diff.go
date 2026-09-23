// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmoverlapparity

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/helmrender"
	"gopkg.in/yaml.v3"
)

// M14 gap #3 (review fix): securityContext.runAsUser / runAsNonRoot are hardcoded
// in templates/deployment.yaml, ABSENT from values.yaml — so the values.yaml
// diff (content_diff.go) can never catch a runAsUser drift. This file closes
// that gap by comparing the RENDERED helm output (the plan's original
// renderFields intent), scoped to Deployment pod securityContexts. That IS the
// invariant loopzero-design.md names ("Both systems must share runAsUser: 1000").
//
// The compare is hermetic (diffRenderedSecurityContext takes rendered YAML
// strings); Check shells out to `helm template` for the real render (helm is a
// documented quality:gates prerequisite).

// renderHelm renders a chart dir as release "parity" through quality/helmrender,
// which keeps helm's stderr out of the manifest stream.
func renderHelm(chartDir string) (string, error) {
	return helmrender.Helm{Release: "parity"}.Render(chartDir, nil)
}

// deploymentPodSecurityContexts returns the set of pod-level securityContext
// signatures ("runAsUser=1000,runAsNonRoot=true") across every Deployment in the
// rendered YAML. Absence of a securityContext on a Deployment is represented as
// the sentinel "<none>" so a dropped non-root guarantee surfaces as drift.
// A document that fails to decode is an error: stopping there would compare
// the sets over a truncated stream.
func deploymentPodSecurityContexts(rendered string) (map[string]bool, error) {
	set := map[string]bool{}
	dec := yaml.NewDecoder(strings.NewReader(rendered))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			return set, nil
		} else if err != nil {
			return nil, fmt.Errorf("decode rendered manifest %d: %w", len(set)+1, err)
		}
		if doc == nil {
			continue
		}
		if kind, _ := doc["kind"].(string); kind != "Deployment" {
			continue
		}
		set[podSecurityContextSignature(doc)] = true
	}
}

// podSecurityContextSignature extracts spec.template.spec.securityContext of a
// Deployment as a stable string. "<none>" when absent.
func podSecurityContextSignature(deployment map[string]any) string {
	spec, _ := deployment["spec"].(map[string]any)
	tmpl, _ := spec["template"].(map[string]any)
	podSpec, _ := tmpl["spec"].(map[string]any)
	sc, ok := podSpec["securityContext"].(map[string]any)
	if !ok || len(sc) == 0 {
		return "<none>"
	}
	parts := make([]string, 0, len(invariantSecurityFields))
	present := false
	for _, k := range invariantSecurityFields {
		if v, has := sc[k]; has {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
			present = true
		}
	}
	if !present {
		return "<none>"
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// invariantSecurityFields are the securityContext keys the parity invariant
// covers: the non-root guarantee loopzero-design.md names ("Both systems must share
// runAsUser: 1000"). Ancillary fields (fsGroup, runAsGroup, seccompProfile) MAY
// legitimately differ between the bootstrap and published charts and are NOT
// part of the invariant — comparing them would flag benign differences.
var invariantSecurityFields = []string{"runAsUser", "runAsNonRoot"}

// diffRenderedSecurityContext compares the Deployment pod securityContext SETS
// of the two rendered chart copies. If the sets differ, the charts have diverged
// on a security-critical field the values.yaml diff cannot see — a Violation.
func diffRenderedSecurityContext(bootstrapRendered, publishedRendered, chartName string) []Violation {
	b, berr := deploymentPodSecurityContexts(bootstrapRendered)
	p, perr := deploymentPodSecurityContexts(publishedRendered)
	if err := errors.Join(berr, perr); err != nil {
		return []Violation{{Reason: fmt.Sprintf("rendered output of %q cannot be compared: %v", chartName, err)}}
	}
	if setsEqual(b, p) {
		return nil
	}
	return []Violation{{
		Reason: fmt.Sprintf("rendered securityContext drift in %q: bootstrap Deployments %v vs published %v",
			chartName, sortedKeys(b), sortedKeys(p)),
	}}
}

func setsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]bool) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
