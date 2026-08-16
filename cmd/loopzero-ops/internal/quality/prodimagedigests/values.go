// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package prodimagedigests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// scanChartValues judges the VALUES layer of every chart that defers its image
// reference to the canonical helper.
//
// # The gap this closes, which the gate itself had written down
//
// The template scanner treats `{{ include "loopzero-common.image" . }}` as clean, and
// its comment explains why — the helper prefers `.Values.image.digest`. The same
// comment then says: *"the violation surfaces at the values.yaml level when
// digest is missing"*. That sentence names a check that was never written, and it
// was right: measured 2026-08-16, ALL SEVEN published charts use the helper, NONE
// declares a digest, and all seven declare `tag: dev`.
//
// So the helper fell back to the tag in every case, the effective default of
// every published chart was a mutable `loopzero-x:dev`, and the gate reported PASS
// over 66 files whose only three findings were Helm test pods.
//
// The charts do the right thing in the template layer and the wrong thing in the
// values layer. A gate that reads one layer cannot tell those apart.
//
// The companion sentence — *"at render-time the helper produces correct
// output"* — is true only GIVEN a digest. Without one the helper produces a tag,
// correctly, and the output is mutable.
func scanChartValues(chartsRoot string, out *Outcome) error {
	entries, err := os.ReadDir(chartsRoot)
	if err != nil {
		// A checkout without `charts/` is not a failure — same tolerance the
		// path walker already grants a missing scan path.
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read charts dir %s: %w", chartsRoot, err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		chartDir := filepath.Join(chartsRoot, e.Name())
		valuesPath := filepath.Join(chartDir, "values.yaml")

		raw, err := os.ReadFile(filepath.Clean(valuesPath))
		if err != nil {
			continue // a directory under charts/ without values.yaml is not a chart
		}

		// Only charts that DEFER to the helper are judged here. A chart writing
		// a literal `image:` is already covered by the template scanner, and
		// reporting it twice would give one defect two findings — which teaches
		// the reader to add wrong.
		if !chartDefersToHelper(chartDir) {
			continue
		}

		var doc struct {
			Image struct {
				Repository string `yaml:"repository"`
				Tag        string `yaml:"tag"`
				Digest     string `yaml:"digest"`
			} `yaml:"image"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("parse %s: %w", valuesPath, err)
		}

		// No image block at all: nothing is being published from here.
		if doc.Image.Repository == "" && doc.Image.Tag == "" && doc.Image.Digest == "" {
			continue
		}
		// An empty digest is not a declared one — `default ""` in the helper
		// treats it as absent, and accepting the key-present-but-empty form
		// would accept the appearance of the fix in place of the fix.
		if strings.TrimSpace(doc.Image.Digest) != "" {
			continue
		}

		out.Violations = append(out.Violations, fmt.Sprintf(
			"%s: image.digest ausente — o chart usa `loopzero-common.image`, que prefere digest e "+
				"cai para `tag: %s`; a referência efetiva do install padrão é mutável",
			valuesPath, orPlaceholder(doc.Image.Tag)))
	}
	return nil
}

// chartDefersToHelper reports whether any template in the chart resolves its
// image through the canonical helper.
func chartDefersToHelper(chartDir string) bool {
	found := false
	_ = filepath.WalkDir(filepath.Join(chartDir, "templates"),
		func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !isYAML(path) {
				return nil //nolint:nilerr // a missing templates dir is not a chart defect
			}
			b, rerr := os.ReadFile(filepath.Clean(path))
			if rerr != nil {
				return nil
			}
			if helperIncludeRe.Match(b) {
				found = true
			}
			return nil
		})
	return found
}

func orPlaceholder(tag string) string {
	if strings.TrimSpace(tag) == "" {
		return "(ausente — cai para .Chart.AppVersion)"
	}
	return tag
}
