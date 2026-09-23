// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"fmt"
	"sort"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/helmrender"
	"gopkg.in/yaml.v3"
)

// Renderer turns a chart directory into the manifest stream it installs, under
// the values `sets` (each a `--set` expression; nil renders at chart defaults).
// It is quality/helmrender's seam, shared by every gate that renders charts.
//
// Values are on the one method rather than on a second interface: the RBAC
// comparison (B-001) renders the published chart once per entry of
// rbacValuesMatrix, and a separate ValuesRenderer would need a type assertion
// and a fallback that renders at defaults — so a caller could report "both
// flag states agree" about a renderer that ignored them.
type Renderer = helmrender.Renderer

// productionRenderer renders with a fixed release name, so two runs of the
// same chart produce byte-identical output; it is never compared across
// charts, because the two chart families use deliberately different naming
// conventions (see renderedKinds).
var productionRenderer Renderer = helmrender.Helm{Release: "drift-probe"}

// renderedKinds returns the set of resource KINDS a chart installs.
//
// # Why kinds, and not kind+name
//
// Measured 2026-08-16: comparing by kind+name gives ZERO resources in common for
// `loopzero-api` and `loopzero-cloud`, because the published charts prefix names with the
// release (`t-loopzero-api`) while the bootstrap charts hardcode them (`loopzero-api`).
// Those are deliberately different conventions, not drift — a gate built on the
// assumption that names match would report 100% drift on two of three pairs.
//
// The cost of kind granularity is declared, not hidden: two DIFFERENT ConfigMaps
// both count as "ConfigMap present". This gate answers "do the two sides install
// the same TYPES of resource?", not "the same resources". Going finer would mean
// normalising names across two intentional conventions — inventing a mapping —
// and an invented mapping inside a gate is the very defect this one exists to
// catch.
//
// # Why `helm.sh/hook: test` is excluded
//
// A resource annotated as a test hook is not installed by `helm install`; only
// `helm test` runs it. Counting it reports an asymmetry with no effect on what is
// actually running — and measured against the real charts, it is exactly the
// difference that showed up (`Pod` on the published side of both pairs).
func renderedKinds(r Renderer, chartDir string) (map[string]bool, error) {
	// nil values: the kind comparison renders at chart DEFAULTS, unchanged. Its
	// eleven approvals were measured under them, and moving its baseline while
	// adding a second comparison would make both findings unreadable.
	docs, err := renderDocs(r, chartDir, nil)
	if err != nil {
		return nil, err
	}
	kinds := map[string]bool{}
	for _, doc := range docs {
		kind, _ := doc["kind"].(string)
		if kind == "" {
			continue
		}
		kinds[kind] = true
	}
	return kinds, nil
}

// renderDocs renders a chart and returns the installable documents it produces:
// every YAML document that carries a kind and is not a Helm test hook.
//
// Split out of renderedKinds so a second comparison can read the SAME decoded
// documents rather than decoding the stream twice. Until B-001 the decode kept
// `doc["kind"]` and dropped the rest on the next iteration — the `rules` of every
// ClusterRole went out of scope four lines after being parsed, which is why the
// gate could not see a grant divergence.
func renderDocs(r Renderer, chartDir string, sets []string) ([]map[string]any, error) {
	raw, err := r.Render(chartDir, sets)
	if err != nil {
		// A render that FAILED and a chart that produces NOTHING are different
		// facts. Collapsing the first into the second would make the gate report
		// total drift as though it had measured it.
		return nil, err
	}

	var docs []map[string]any
	dec := yaml.NewDecoder(strings.NewReader(raw))
	for {
		var doc map[string]any
		derr := dec.Decode(&doc)
		if derr != nil {
			if derr.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("parsing rendered manifest of %s: %w", chartDir, derr)
		}
		// `helm template` emits stray `---` separators when a conditional block
		// renders nothing, so a nil document in the stream is normal.
		if doc == nil {
			continue
		}
		if _, ok := doc["kind"].(string); !ok {
			continue
		}
		if isTestHook(doc) {
			continue
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// isTestHook reports whether a rendered document is a Helm test hook.
func isTestHook(doc map[string]any) bool {
	meta, _ := doc["metadata"].(map[string]any)
	if meta == nil {
		return false
	}
	ann, _ := meta["annotations"].(map[string]any)
	if ann == nil {
		return false
	}
	hook, _ := ann["helm.sh/hook"].(string)
	return strings.Contains(hook, "test")
}

// diffKinds returns what only the published chart installs and what only the
// bootstrap chart installs, each ordered so two reports diff cleanly.
func diffKinds(published, bootstrap map[string]bool) (onlyPublished, onlyBootstrap []string) {
	for k := range published {
		if !bootstrap[k] {
			onlyPublished = append(onlyPublished, k)
		}
	}
	for k := range bootstrap {
		if !published[k] {
			onlyBootstrap = append(onlyBootstrap, k)
		}
	}
	sort.Strings(onlyPublished)
	sort.Strings(onlyBootstrap)
	return onlyPublished, onlyBootstrap
}
